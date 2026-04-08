package service

import (
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// RunState tracks the independent per-run execution state.
// Separated from AgentLoop to support concurrent sessions.
type RunState struct {
	mu           sync.Mutex
	SessionID    string
	State        State
	Step         int
	ToolCalls    int
	TokensUsed   int
	StartedAt    time.Time
	LastToolName string
	Error        error
}

// NewRunState creates a fresh run state.
func NewRunState(sessionID string) *RunState {
	return &RunState{
		SessionID: sessionID,
		State:     StateIdle,
		StartedAt: time.Now(),
	}
}

// Transition updates the observed run phase.
func (rs *RunState) Transition(to State) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.State = to
	return true
}

// IsTerminal returns true if the run is in a terminal state.
func (rs *RunState) IsTerminal() bool {
	return rs.State == StateDone || rs.State == StateFatal
}

// ═══════════════════════════════════════════
// DeltaEvent — typed event stream
// ═══════════════════════════════════════════

// DeltaType classifies delta events.
type DeltaType int

const (
	DeltaText       DeltaType = iota // Assistant text chunk
	DeltaReasoning                   // Thinking/reasoning text
	DeltaToolStart                   // Tool call started
	DeltaToolResult                  // Tool call completed
	DeltaState                       // State transition
	DeltaError                       // Error occurred
	DeltaApproval                    // Approval requested
	DeltaComplete                    // Turn completed
	DeltaEvoEval                     // Evo: evaluation result
	DeltaEvoRepair                   // Evo: repair attempt
)

// DeltaEvent is a typed streaming event.
type DeltaEvent struct {
	Type     DeltaType      `json:"type"`
	Text     string         `json:"text,omitempty"`
	ToolCall *ToolCallDelta `json:"tool_call,omitempty"`
	State    string         `json:"state,omitempty"`
	Error    string         `json:"error,omitempty"`
	Approval *ApprovalDelta `json:"approval,omitempty"`
}

// ToolCallDelta describes a tool call event.
type ToolCallDelta struct {
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Output string         `json:"output,omitempty"`
	Err    string         `json:"error,omitempty"`
}

// ApprovalDelta describes an approval request event.
type ApprovalDelta struct {
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	Reason   string         `json:"reason"`
}

// DeltaAdapter wraps the old callback-style Delta as a typed DeltaSink emitter.
type DeltaAdapter struct {
	inner *Delta
}

// NewDeltaAdapter creates an adapter.
func NewDeltaAdapter(d *Delta) *DeltaAdapter {
	return &DeltaAdapter{inner: d}
}

// Emit routes typed events to the appropriate callback function.
func (da *DeltaAdapter) Emit(event DeltaEvent) {
	switch event.Type {
	case DeltaText:
		da.inner.OnText(event.Text)
	case DeltaReasoning:
		da.inner.OnReasoning(event.Text)
	case DeltaToolStart:
		if event.ToolCall != nil {
			da.inner.OnToolStart(event.ToolCall.ID, event.ToolCall.Name, event.ToolCall.Args)
		}
	case DeltaToolResult:
		if event.ToolCall != nil {
			var err error
			if event.ToolCall.Err != "" {
				err = &toolError{msg: event.ToolCall.Err}
			}
			da.inner.OnToolResult(event.ToolCall.ID, event.ToolCall.Name, event.ToolCall.Output, err)
		}
	case DeltaError:
		if event.Error != "" {
			da.inner.OnError(&toolError{msg: event.Error})
		}
	case DeltaComplete:
		da.inner.OnComplete()
	}
}

// HistoryEntry wraps an LLM message for run state tracking.
type HistoryEntry struct {
	model.Message
	Step int
}

type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }
