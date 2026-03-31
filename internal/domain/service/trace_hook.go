package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
)

// TraceStep is a structured snapshot of a single tool call.
type TraceStep struct {
	Index    int            `json:"i"`
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
	Output   string         `json:"out"`
	Error    string         `json:"err,omitempty"`
	Duration int            `json:"ms"`
}

// TraceCollectorHook implements ToolHook to collect structured execution traces.
// It records each tool call's name, arguments, output (truncated), and duration.
type TraceCollectorHook struct {
	mu       sync.Mutex
	steps    []TraceStep
	startAt  map[int]time.Time
	evoStore *persistence.EvoStore
}

// NewTraceCollectorHook creates a trace collector.
func NewTraceCollectorHook(store *persistence.EvoStore) *TraceCollectorHook {
	return &TraceCollectorHook{
		evoStore: store,
		startAt:  make(map[int]time.Time),
	}
}

// BeforeTool records the tool invocation start time and arguments.
func (h *TraceCollectorHook) BeforeTool(_ context.Context, name string, args map[string]any) (map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := len(h.steps)
	h.startAt[idx] = time.Now()

	// Pre-allocate the step slot with args (truncate large values)
	cleanArgs := truncateArgs(args)
	h.steps = append(h.steps, TraceStep{
		Index: idx,
		Tool:  name,
		Args:  cleanArgs,
	})

	return args, false // Never skip
}

// AfterTool records the tool output and duration.
func (h *TraceCollectorHook) AfterTool(_ context.Context, name string, output string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := len(h.steps) - 1
	if idx < 0 {
		return
	}

	step := &h.steps[idx]
	step.Output = truncateStr(output, 300)
	if err != nil {
		step.Error = err.Error()
	}
	if start, ok := h.startAt[idx]; ok {
		step.Duration = int(time.Since(start).Milliseconds())
		delete(h.startAt, idx)
	}
}

// Flush serializes all collected steps into an EvoTrace and persists it.
// Returns the created trace ID. Resets the collector for the next run.
func (h *TraceCollectorHook) Flush(sessionID string, runIndex int) (uint, error) {
	h.mu.Lock()
	steps := make([]TraceStep, len(h.steps))
	copy(steps, h.steps)
	h.steps = h.steps[:0]
	h.startAt = make(map[int]time.Time)
	h.mu.Unlock()

	stepsJSON, _ := json.Marshal(steps)

	trace := &persistence.EvoTrace{
		SessionID: sessionID,
		RunIndex:  runIndex,
		Steps:     string(stepsJSON),
	}

	if h.evoStore == nil {
		return 0, nil
	}
	if err := h.evoStore.SaveTrace(trace); err != nil {
		return 0, err
	}
	return trace.ID, nil
}

// Steps returns the currently collected steps (for inspection without flushing).
func (h *TraceCollectorHook) Steps() []TraceStep {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]TraceStep, len(h.steps))
	copy(out, h.steps)
	return out
}

// Reset clears collected steps without persisting.
func (h *TraceCollectorHook) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps = h.steps[:0]
	h.startAt = make(map[int]time.Time)
}

// truncateArgs creates a copy of args with large string values truncated.
func truncateArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = truncateStr(s, 200)
		} else {
			out[k] = v
		}
	}
	return out
}

// truncateStr truncates a string to max characters.
func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
