package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
)

// TraceStep is a structured snapshot of a single agent action (tool call or reasoning).
// Designed for RL training data: captures full action + observation + reasoning chain.
type TraceStep struct {
	Index     int            `json:"i"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	Output    string         `json:"out"`
	Error     string         `json:"err,omitempty"`
	Duration  int            `json:"ms"`
	Reasoning string         `json:"reasoning,omitempty"` // LLM thinking/reasoning before this action
}

// TraceCollectorHook implements ToolHook to collect structured execution traces.
// Records each tool call's name, FULL arguments, FULL output, duration, and reasoning.
// Thread-safe: BeforeTool returns a step index, AfterTool uses it for correct matching.
type TraceCollectorHook struct {
	mu        sync.Mutex
	steps     []TraceStep
	startAt   map[int]time.Time
	evoStore  *persistence.EvoStore
	tokensIn  int
	tokensOut int
	model     string
	// pendingReasoning holds the LLM's reasoning text to attach to the next tool call.
	pendingReasoning string
	// finalResponse stores the last assistant text for RL training completeness.
	finalResponse string
}

// NewTraceCollectorHook creates a trace collector.
func NewTraceCollectorHook(store *persistence.EvoStore) *TraceCollectorHook {
	return &TraceCollectorHook{
		evoStore: store,
		startAt:  make(map[int]time.Time),
	}
}

// RecordReasoning stores the LLM's reasoning output to attach to the next tool call.
func (h *TraceCollectorHook) RecordReasoning(reasoning string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pendingReasoning = reasoning
}

// RecordTokens accumulates token usage for the current run.
func (h *TraceCollectorHook) RecordTokens(tokensIn, tokensOut int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokensIn += tokensIn
	h.tokensOut += tokensOut
}

// SetModel records the model name used in this run.
func (h *TraceCollectorHook) SetModel(model string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.model = model
}

// RecordFinalResponse stores the agent's final text response for RL training.
func (h *TraceCollectorHook) RecordFinalResponse(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finalResponse = text
}

// BeforeTool records the tool invocation start time and FULL arguments.
// Returns the allocated step index for concurrent-safe AfterTool matching.
func (h *TraceCollectorHook) BeforeTool(_ context.Context, name string, args map[string]any) (int, map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := len(h.steps)
	h.startAt[idx] = time.Now()

	// Store full args — critical for RL training (code content, file patches, etc.)
	cleanArgs := sanitizeArgs(args, 50*1024)

	step := TraceStep{
		Index: idx,
		Tool:  name,
		Args:  cleanArgs,
	}

	// Attach pending reasoning to this tool call
	if h.pendingReasoning != "" {
		step.Reasoning = h.pendingReasoning
		h.pendingReasoning = ""
	}

	h.steps = append(h.steps, step)

	return idx, args, false // Return index for AfterTool matching
}

// AfterTool records the FULL tool output and duration.
// Uses stepIdx from BeforeTool for concurrent-safe matching.
func (h *TraceCollectorHook) AfterTool(_ context.Context, stepIdx int, name string, output string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if stepIdx < 0 || stepIdx >= len(h.steps) {
		return
	}

	step := &h.steps[stepIdx]
	// Store full output — critical for RL training (agent observation).
	// Cap at 50KB to prevent extreme DB bloat from binary/dump outputs.
	step.Output = capStr(output, 50*1024)
	if err != nil {
		step.Error = err.Error()
	}
	if start, ok := h.startAt[stepIdx]; ok {
		step.Duration = int(time.Since(start).Milliseconds())
		delete(h.startAt, stepIdx)
	}
}

// Flush serializes all collected steps into an EvoTrace and persists it.
// Returns the created trace ID. Resets the collector for the next run.
func (h *TraceCollectorHook) Flush(sessionID string, runIndex int, userMessage string) (uint, error) {
	h.mu.Lock()
	steps := make([]TraceStep, len(h.steps))
	copy(steps, h.steps)
	tokIn := h.tokensIn
	tokOut := h.tokensOut
	model := h.model
	finalResp := h.finalResponse
	var totalDuration int
	for _, s := range steps {
		totalDuration += s.Duration
	}
	// Reset for next run
	h.steps = h.steps[:0]
	h.startAt = make(map[int]time.Time)
	h.tokensIn = 0
	h.tokensOut = 0
	h.model = ""
	h.pendingReasoning = ""
	h.finalResponse = ""
	h.mu.Unlock()

	stepsJSON, _ := json.Marshal(steps)

	trace := &persistence.EvoTrace{
		SessionID:     sessionID,
		RunIndex:      runIndex,
		Steps:         string(stepsJSON),
		TokensIn:      tokIn,
		TokensOut:     tokOut,
		Duration:      totalDuration,
		Model:         model,
		UserMessage:   userMessage,
		FinalResponse: finalResp,
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
	h.tokensIn = 0
	h.tokensOut = 0
	h.model = ""
	h.pendingReasoning = ""
	h.finalResponse = ""
}

// sanitizeArgs creates a copy of args, capping extremely large string values.
func sanitizeArgs(args map[string]any, maxBytes int) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = capStr(s, maxBytes)
		} else {
			out[k] = v
		}
	}
	return out
}

// capStr caps a string at max runes, appending a truncation notice if exceeded.
func capStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "\n[truncated at " + itoa(max) + " chars]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
