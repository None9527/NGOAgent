package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// HealthChecker periodically probes LLM providers to track availability.
// Used by Router.ResolveWithFallback to skip unhealthy providers.
type HealthChecker struct {
	mu       sync.RWMutex
	status   map[string]providerHealth
	interval time.Duration
	stopCh   chan struct{}
}

type providerHealth struct {
	Healthy     bool
	LastPing    time.Time
	ConsecFails int
	LastError   string
}

const (
	healthFailThreshold = 3  // consecutive failures to mark unhealthy
	defaultPingInterval = 60 // seconds
)

// NewHealthChecker creates a health checker with the given ping interval.
// interval=0 uses the default (60s).
func NewHealthChecker(intervalSec int) *HealthChecker {
	if intervalSec <= 0 {
		intervalSec = defaultPingInterval
	}
	return &HealthChecker{
		status:   make(map[string]providerHealth),
		interval: time.Duration(intervalSec) * time.Second,
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic health probing for all providers.
// Blocks until stop is called or the stop channel is closed.
// Call in a goroutine.
func (hc *HealthChecker) Start(providers map[string]Provider) {
	// Initial probe for all providers
	hc.probeAll(providers)

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.probeAll(providers)
		case <-hc.stopCh:
			return
		}
	}
}

// Stop terminates the health checker.
func (hc *HealthChecker) Stop() {
	select {
	case <-hc.stopCh:
	default:
		close(hc.stopCh)
	}
}

// IsHealthy returns whether a provider is considered healthy.
// Unknown providers are assumed healthy (optimistic).
func (hc *HealthChecker) IsHealthy(provName string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	h, ok := hc.status[provName]
	if !ok {
		return true // unknown = assume healthy
	}
	return h.Healthy
}

// Status returns a snapshot of all provider health states.
func (hc *HealthChecker) Status() map[string]providerHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	out := make(map[string]providerHealth, len(hc.status))
	for k, v := range hc.status {
		out[k] = v
	}
	return out
}

// probeAll pings every registered provider with a minimal completion request.
func (hc *HealthChecker) probeAll(providers map[string]Provider) {
	for name, prov := range providers {
		go hc.probeOne(name, prov)
	}
}

// probeOne sends a minimal request to test provider connectivity.
func (hc *HealthChecker) probeOne(name string, prov Provider) {
	models := prov.Models()
	if len(models) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &Request{
		Model:       models[0],
		Messages:    []Message{{Role: "user", Content: "ping"}},
		MaxTokens:   1,
		Temperature: 0,
		Stream:      true,
	}

	ch := make(chan StreamChunk, 8)
	_, err := prov.GenerateStream(ctx, req, ch)
	// Drain to prevent leak
	for range ch {
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	h := hc.status[name]
	if err != nil {
		h.ConsecFails++
		h.LastError = err.Error()
		if h.ConsecFails >= healthFailThreshold {
			if h.Healthy {
				slog.Info(fmt.Sprintf("[health] Provider %s marked UNHEALTHY after %d consecutive failures: %v",
					name, h.ConsecFails, err))
			}
			h.Healthy = false
		}
	} else {
		if !h.Healthy && h.ConsecFails > 0 {
			slog.Info(fmt.Sprintf("[health] Provider %s recovered (was %d fails)", name, h.ConsecFails))
		}
		h.Healthy = true
		h.ConsecFails = 0
		h.LastPing = time.Now()
		h.LastError = ""
	}
	hc.status[name] = h
}
