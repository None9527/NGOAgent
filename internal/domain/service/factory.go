// Package service — factory.go provides the LoopFactory for creating
// independent AgentLoop instances with unified lifecycle management.
//
// All agent execution contexts (chat, subagent, forge) use
// LoopFactory.Create() instead of directly calling NewAgentLoop().
package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// ────────────────────────────────────────────
// RunID generation: sid:channel:shortid
// ────────────────────────────────────────────

// MakeRunID creates a hierarchical run ID: parentSID:channel:shortUUID.
// For top-level chat sessions, parentSID is the session UUID itself.
func MakeRunID(parentSID, channel string) string {
	short := uuid.New().String()[:8]
	return fmt.Sprintf("%s:%s:%s", parentSID, channel, short)
}

// ────────────────────────────────────────────
// AgentRun: a tracked execution context
// ────────────────────────────────────────────

// AgentRun wraps an AgentLoop with lifecycle metadata.
type AgentRun struct {
	ID        string       // e.g. "c4c76450:sub:d4e5f6"
	ParentID  string       // empty for top-level runs
	Channel   AgentChannel // chat/subagent/forge
	Loop      *AgentLoop
	StartedAt time.Time
	cancel    context.CancelFunc
}

// ────────────────────────────────────────────
// LoopFactory: centralized loop lifecycle
// ────────────────────────────────────────────

// LoopFactory creates and tracks independent AgentLoop instances.
// All consumers (chat, subagent, forge) go through this factory.
type LoopFactory struct {
	baseDeps      Deps // Shared infrastructure (LLM, tools, brain, etc.)
	mu            sync.Mutex
	active        map[string]*AgentRun // All active runs by ID
	maxConcurrent int                  // Global concurrency limit
	sem           chan struct{}        // Concurrency semaphore
}

// NewLoopFactory creates a factory with shared deps and concurrency limit.
func NewLoopFactory(deps Deps, maxConcurrent int) *LoopFactory {
	if maxConcurrent <= 0 {
		maxConcurrent = 8 // Default: 8 concurrent agent runs
	}
	return &LoopFactory{
		baseDeps:      deps,
		active:        make(map[string]*AgentRun),
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// Create spawns a new AgentRun with an independent AgentLoop.
// The loop shares infra deps but has its own history, state, and mutex.
// For subagent channels, orchestration tools are disabled based on the AgentDefinition.
// If def is nil, a default disallowed list is used for backward compatibility.
func (f *LoopFactory) Create(parentSID string, ch AgentChannel, def *model.AgentDefinition) *AgentRun {
	runID := MakeRunID(parentSID, ch.Name())

	// Clone deps with channel-specific delta sink
	deps := f.baseDeps
	deps.Delta = ch.DeltaSink()

	// For sub-agents: clone registry with tools disabled per AgentDefinition.
	if ch.Name() == "subagent" {
		disallowed := []string{
			"task_boundary", "notify_user", "task_plan",
			"spawn_agent", "save_memory", "view_media",
		}
		if def != nil && len(def.DisallowedTools) > 0 {
			disallowed = def.DisallowedTools
		}
		type withClone interface {
			CloneWithDisabled([]string) any
		}
		if c, ok := deps.ToolExec.(withClone); ok {
			if te, ok := c.CloneWithDisabled(disallowed).(ToolExecutor); ok {
				deps.ToolExec = te
			}
		}
	}

	loop := NewAgentLoop(deps)

	// Set subagent mode so prompt engine uses AssembleSubagent
	if ch.Name() == "subagent" {
		loop.options.Mode = "subagent"
		// Store agent definition for prompt assembly
		if def != nil {
			loop.options.AgentType = def.AgentType
			loop.options.AgentDef = def
		}
	}

	run := &AgentRun{
		ID:        runID,
		ParentID:  parentSID,
		Channel:   ch,
		Loop:      loop,
		StartedAt: time.Now(),
	}

	f.mu.Lock()
	f.active[runID] = run
	f.mu.Unlock()

	return run
}

// RunAsync starts an AgentRun in a goroutine with concurrency control.
// Returns the run ID immediately (non-blocking).
// When the run completes, it calls channel.OnComplete and unregisters.
func (f *LoopFactory) RunAsync(ctx context.Context, run *AgentRun, message string) string {
	runCtx, cancel := context.WithCancel(ctx)
	run.cancel = cancel

	go func() {
		defer func() {
			f.mu.Lock()
			delete(f.active, run.ID)
			f.mu.Unlock()
		}()

		// Acquire semaphore slot
		select {
		case f.sem <- struct{}{}:
			defer func() { <-f.sem }()
		case <-runCtx.Done():
			run.Channel.OnComplete(run.ID, "", runCtx.Err())
			return
		}

		err := run.Loop.Run(runCtx, message)

		// P2 #22: Worker Transcript — persist subagent history to DB for debugging/audit.
		// Uses run.ID as sessionID so transcripts are queryable by parent session.
		if run.Loop.deps.HistoryStore != nil {
			run.Loop.persistTranscript(run.ID)
		}

		result := ""
		if collector, ok := run.Channel.DeltaSink().(*OutputCollector); ok {
			result = collector.StructuredResult()
		}
		run.Channel.OnComplete(run.ID, result, err)
		cancel()
	}()

	return run.ID
}

// RunSync starts an AgentRun and blocks until completion.
// Used for channels that need synchronous results (e.g., chat via SSE).
func (f *LoopFactory) RunSync(ctx context.Context, run *AgentRun, message string) error {
	runCtx, cancel := context.WithCancel(ctx)
	run.cancel = cancel
	defer func() {
		cancel()
		f.mu.Lock()
		delete(f.active, run.ID)
		f.mu.Unlock()
	}()

	// Acquire semaphore slot
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-runCtx.Done():
		return runCtx.Err()
	}

	return run.Loop.Run(runCtx, message)
}

// Stop cancels a specific run and cascades to all its children.
func (f *LoopFactory) Stop(runID string) {
	f.mu.Lock()
	run, ok := f.active[runID]
	// Collect children to stop
	var childIDs []string
	for id, r := range f.active {
		if r.ParentID == runID {
			childIDs = append(childIDs, id)
		}
	}
	f.mu.Unlock()

	if ok && run.cancel != nil {
		slog.Info(fmt.Sprintf("[factory] Stopping run %s (%s)", runID, run.Channel.Name()))
		run.cancel()
	}

	// Cascade stop to children
	for _, childID := range childIDs {
		f.Stop(childID)
	}
}

// StopAll cancels all active runs.
func (f *LoopFactory) StopAll() {
	f.mu.Lock()
	ids := make([]string, 0, len(f.active))
	for id := range f.active {
		ids = append(ids, id)
	}
	f.mu.Unlock()

	for _, id := range ids {
		f.Stop(id)
	}
}

// Active returns the number of active runs.
func (f *LoopFactory) Active() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.active)
}

// ListRuns returns metadata about all active runs.
func (f *LoopFactory) ListRuns() []ActiveRunInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	runs := make([]ActiveRunInfo, 0, len(f.active))
	for _, r := range f.active {
		runs = append(runs, ActiveRunInfo{
			ID:       r.ID,
			ParentID: r.ParentID,
			Channel:  r.Channel.Name(),
			Age:      time.Since(r.StartedAt),
		})
	}
	return runs
}

// ActiveRunInfo is a snapshot of an active run for listing/monitoring.
type ActiveRunInfo struct {
	ID       string
	ParentID string
	Channel  string
	Age      time.Duration
}
