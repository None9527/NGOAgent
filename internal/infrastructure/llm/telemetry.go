// Package llm — structured API telemetry collector (P3 J3).
// Replaces scattered log/print calls with structured TelemetryEvent records.
// Uses a fixed ring buffer (1000 events) with P50/P95/P99 latency stats.
package llm

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// TelemetryEvent records a single LLM API call outcome.
type TelemetryEvent struct {
	Timestamp   time.Time `json:"ts"`
	SessionID   string    `json:"session_id,omitempty"`
	Model       string    `json:"model"`
	Provider    string    `json:"provider"`
	PromptTok   int       `json:"prompt_tok"`
	CompleteTok int       `json:"complete_tok"`
	CachedTok   int       `json:"cached_tok,omitempty"` // DashScope: prompt tokens served from cache
	LatencyMs   int64     `json:"latency_ms"`
	Error       string    `json:"error,omitempty"`
}

// TelemetryStats provides aggregated metrics over a set of events.
type TelemetryStats struct {
	Count          int
	SuccessRate    float64 // 0.0 - 1.0
	P50Ms          int64
	P95Ms          int64
	P99Ms          int64
	AvgPromptTok   float64
	AvgCompleteTok float64
	ByModel        map[string]int // model → call count
	ErrorRate      float64
}

const telemetryRingSize = 1000

// TelemetryCollector accumulates LLM API events in a ring buffer.
// Thread-safe; hooks are called synchronously (keep them fast).
type TelemetryCollector struct {
	mu    sync.Mutex
	ring  [telemetryRingSize]TelemetryEvent
	head  int // next write position
	count int // total events recorded (capped at ring size for access)
	total int // total ever recorded (for stats context)
	hooks []func(TelemetryEvent)
}

// NewTelemetryCollector creates a ready-to-use collector.
func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{}
}

// AddHook registers a callback invoked on every recorded event.
// Use for external sinks (log file, Prometheus, etc.). Keep hooks fast.
func (tc *TelemetryCollector) AddHook(fn func(TelemetryEvent)) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.hooks = append(tc.hooks, fn)
}

// Record stores a TelemetryEvent in the ring buffer and calls hooks.
func (tc *TelemetryCollector) Record(evt TelemetryEvent) {
	tc.mu.Lock()
	tc.ring[tc.head] = evt
	tc.head = (tc.head + 1) % telemetryRingSize
	if tc.count < telemetryRingSize {
		tc.count++
	}
	tc.total++
	hooks := make([]func(TelemetryEvent), len(tc.hooks))
	copy(hooks, tc.hooks)
	tc.mu.Unlock()

	for _, h := range hooks {
		h(evt)
	}
}

// Recent returns the last n events (most recent last), up to ring size.
func (tc *TelemetryCollector) Recent(n int) []TelemetryEvent {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if n > tc.count {
		n = tc.count
	}
	if n == 0 {
		return nil
	}

	out := make([]TelemetryEvent, n)
	// Walk backward from head - 1
	for i := 0; i < n; i++ {
		idx := (tc.head - 1 - i + telemetryRingSize) % telemetryRingSize
		out[n-1-i] = tc.ring[idx]
	}
	return out
}

// Stats computes aggregated metrics over the last n events.
// n=0 means all available (up to ring size).
func (tc *TelemetryCollector) Stats(n int) TelemetryStats {
	events := tc.Recent(n)
	if len(events) == 0 {
		return TelemetryStats{}
	}

	var latencies []int64
	var promptToks, completeToks []int
	byModel := make(map[string]int)
	errCount := 0

	for _, e := range events {
		latencies = append(latencies, e.LatencyMs)
		promptToks = append(promptToks, e.PromptTok)
		completeToks = append(completeToks, e.CompleteTok)
		byModel[e.Model]++
		if e.Error != "" {
			errCount++
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	n = len(latencies)
	p := func(pct float64) int64 {
		idx := int(float64(n-1) * pct)
		return latencies[idx]
	}

	var sumP, sumC int
	for i := range promptToks {
		sumP += promptToks[i]
		sumC += completeToks[i]
	}

	return TelemetryStats{
		Count:          n,
		SuccessRate:    float64(n-errCount) / float64(n),
		P50Ms:          p(0.50),
		P95Ms:          p(0.95),
		P99Ms:          p(0.99),
		AvgPromptTok:   float64(sumP) / float64(n),
		AvgCompleteTok: float64(sumC) / float64(n),
		ByModel:        byModel,
		ErrorRate:      float64(errCount) / float64(n),
	}
}

// Format returns a human-readable summary for slash commands.
func (s TelemetryStats) Format() string {
	if s.Count == 0 {
		return "No telemetry data collected yet."
	}
	out := fmt.Sprintf(
		"📊 LLM Telemetry (%d calls)\n"+
			"  Latency  P50: %dms  P95: %dms  P99: %dms\n"+
			"  Tokens   Avg prompt: %.0f  Avg complete: %.0f\n"+
			"  Success: %.1f%%  Error: %.1f%%\n"+
			"  By model:\n",
		s.Count,
		s.P50Ms, s.P95Ms, s.P99Ms,
		s.AvgPromptTok, s.AvgCompleteTok,
		s.SuccessRate*100, s.ErrorRate*100,
	)
	for model, count := range s.ByModel {
		out += fmt.Sprintf("    %-40s %d calls\n", model, count)
	}
	return out
}

// GlobalTelemetry is the process-wide singleton collector.
// Initialized in builder; nil-safe (Record/Stats no-op if nil).
var GlobalTelemetry *TelemetryCollector
