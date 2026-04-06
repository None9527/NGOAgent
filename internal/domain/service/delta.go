package service

import (
	"encoding/json"
	"strings"
)

// Delta is a default DeltaSink implementation that records events.
// Server layer creates its own protocol-specific sink (SSE, gRPC, etc.)
type Delta struct {
	OnTextFunc            func(string)
	OnReasoningFunc       func(string)
	OnToolStartFunc       func(string, string, map[string]any)
	OnToolResultFunc      func(string, string, string, error)
	OnProgressFunc        func(taskName, status, summary, mode string)
	OnPlanReviewFunc      func(message string, paths []string)
	OnApprovalRequestFunc func(approvalID, toolName string, args map[string]any, reason string)
	OnTitleUpdateFunc     func(sessionID, title string)
	OnCompleteFunc        func()
	OnErrorFunc           func(error)
	OnAutoWakeStartFunc   func()
	EmitFunc              func(DeltaEvent) // Generic event emitter (evo events, etc.)
}

func (d *Delta) OnText(text string) {
	if d.OnTextFunc != nil {
		d.OnTextFunc(text)
	}
}

func (d *Delta) OnReasoning(text string) {
	if d.OnReasoningFunc != nil {
		d.OnReasoningFunc(text)
	}
}

func (d *Delta) OnToolStart(callID string, name string, args map[string]any) {
	if d.OnToolStartFunc != nil {
		d.OnToolStartFunc(callID, name, args)
	}
}

func (d *Delta) OnToolResult(callID string, name string, output string, err error) {
	if d.OnToolResultFunc != nil {
		d.OnToolResultFunc(callID, name, output, err)
	}
}

func (d *Delta) OnProgress(taskName, status, summary, mode string) {
	if d.OnProgressFunc != nil {
		d.OnProgressFunc(taskName, status, summary, mode)
	}
}

func (d *Delta) OnPlanReview(message string, paths []string) {
	if d.OnPlanReviewFunc != nil {
		d.OnPlanReviewFunc(message, paths)
	}
}

func (d *Delta) OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string) {
	if d.OnApprovalRequestFunc != nil {
		d.OnApprovalRequestFunc(approvalID, toolName, args, reason)
	}
}

func (d *Delta) OnTitleUpdate(sessionID, title string) {
	if d.OnTitleUpdateFunc != nil {
		d.OnTitleUpdateFunc(sessionID, title)
	}
}

func (d *Delta) OnComplete() {
	if d.OnCompleteFunc != nil {
		d.OnCompleteFunc()
	}
}

func (d *Delta) OnError(err error) {
	if d.OnErrorFunc != nil {
		d.OnErrorFunc(err)
	}
}

func (d *Delta) OnAutoWakeStart() {
	if d.OnAutoWakeStartFunc != nil {
		d.OnAutoWakeStartFunc()
	}
}

func (d *Delta) Emit(event DeltaEvent) {
	if d.EmitFunc != nil {
		d.EmitFunc(event)
	}
}

// OutputCollector is a DeltaSink that accumulates text output AND tool events.
// Used by spawn_agent to collect sub-agent results with structured tool info.
type OutputCollector struct {
	buf       strings.Builder
	events    []ToolEvent
	pending   map[string]pendingTool // callID → start info
	StepPush  func(toolName string)  // optional: push current step to parent (for SubagentDock)
	toolCount int                    // total tools executed (for step counting)
}

// ToolEvent records a single tool invocation by the sub-agent.
type ToolEvent struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Output string         `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type pendingTool struct {
	name string
	args map[string]any
}

func (c *OutputCollector) OnText(text string) { c.buf.WriteString(text) }
func (c *OutputCollector) OnReasoning(string) {}
func (c *OutputCollector) OnToolStart(callID string, name string, args map[string]any) {
	if c.pending == nil {
		c.pending = make(map[string]pendingTool)
	}
	c.pending[callID] = pendingTool{name: name, args: args}
	c.toolCount++
	// Push step info to parent so SubagentDock shows current activity
	if c.StepPush != nil {
		c.StepPush(name)
	}
}
func (c *OutputCollector) OnToolResult(callID string, name string, output string, err error) {
	ev := ToolEvent{Name: name, Output: output}
	if p, ok := c.pending[callID]; ok {
		ev.Args = p.args
		delete(c.pending, callID)
	}
	if err != nil {
		ev.Error = err.Error()
	}
	// Truncate long output for structured view
	if len(ev.Output) > 500 {
		ev.Output = ev.Output[:500] + "..."
	}
	c.events = append(c.events, ev)
}
func (c *OutputCollector) OnProgress(string, string, string, string)                {}
func (c *OutputCollector) OnPlanReview(string, []string)                            {}
func (c *OutputCollector) OnApprovalRequest(string, string, map[string]any, string) {}
func (c *OutputCollector) OnTitleUpdate(string, string)                             {}
func (c *OutputCollector) OnAutoWakeStart()                                         {}
func (c *OutputCollector) OnComplete()                                              {}
func (c *OutputCollector) OnError(error)                                            {}
func (c *OutputCollector) Emit(DeltaEvent)                                          {}

// Result returns the accumulated text (backward compat).
func (c *OutputCollector) Result() string { return c.buf.String() }

// StructuredResult returns JSON with text + tool_events for rich rendering.
func (c *OutputCollector) StructuredResult() string {
	if len(c.events) == 0 {
		return c.buf.String()
	}
	type structured struct {
		Text       string      `json:"text"`
		ToolEvents []ToolEvent `json:"tool_events"`
	}
	data := structured{Text: c.buf.String(), ToolEvents: c.events}
	b, err := json.Marshal(data)
	if err != nil {
		return c.buf.String()
	}
	return string(b)
}
