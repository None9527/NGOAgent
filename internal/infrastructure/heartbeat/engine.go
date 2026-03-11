// Package heartbeat provides periodic background task execution.
// The heartbeat engine runs a lightweight agent loop on heartbeat.md tasks
// at configured intervals, using a restricted tool allowlist.
package heartbeat

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// AgentRunner is the interface for running the agent on a message.
type AgentRunner interface {
	Run(ctx context.Context, message string) error
}

// Engine runs periodic heartbeat tasks.
type Engine struct {
	cfg      *config.HeartbeatConfig
	runner   AgentRunner
	loadTask func() string // Loads heartbeat.md content
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
}

// NewEngine creates a heartbeat engine.
func NewEngine(cfg *config.HeartbeatConfig, runner AgentRunner, loadTask func() string) *Engine {
	return &Engine{
		cfg:      cfg,
		runner:   runner,
		loadTask: loadTask,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the heartbeat tick loop.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running || !e.cfg.Enabled {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	log.Printf("[heartbeat] Started with interval %v", e.cfg.Interval)

	go e.loop(ctx)
}

// Stop terminates the heartbeat loop.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	close(e.stopCh)
	e.running = false
	log.Println("[heartbeat] Stopped")
}

func (e *Engine) loop(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) tick(ctx context.Context) {
	tasks := e.loadTask()
	if tasks == "" {
		return
	}

	tickCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	log.Println("[heartbeat] Running tick...")
	if err := e.runner.Run(tickCtx, tasks); err != nil {
		log.Printf("[heartbeat] Tick error: %v", err)
	}
}

// IsRunning returns whether the heartbeat is active.
func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}
