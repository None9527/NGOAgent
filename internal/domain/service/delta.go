package service

import "strings"

// Delta is a default DeltaSink implementation that records events.
// Server layer creates its own protocol-specific sink (SSE, gRPC, etc.)
type Delta struct {
	OnTextFunc       func(string)
	OnReasoningFunc  func(string)
	OnToolStartFunc  func(string, map[string]any)
	OnToolResultFunc func(string, string, error)
	OnProgressFunc   func(taskName, status, summary, mode string)
	OnApprovalRequestFunc func(approvalID, toolName string, args map[string]any, reason string)
	OnCompleteFunc   func()
	OnErrorFunc      func(error)
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

func (d *Delta) OnToolStart(name string, args map[string]any) {
	if d.OnToolStartFunc != nil {
		d.OnToolStartFunc(name, args)
	}
}

func (d *Delta) OnToolResult(name string, output string, err error) {
	if d.OnToolResultFunc != nil {
		d.OnToolResultFunc(name, output, err)
	}
}

func (d *Delta) OnProgress(taskName, status, summary, mode string) {
	if d.OnProgressFunc != nil {
		d.OnProgressFunc(taskName, status, summary, mode)
	}
}

func (d *Delta) OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string) {
	if d.OnApprovalRequestFunc != nil {
		d.OnApprovalRequestFunc(approvalID, toolName, args, reason)
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

// OutputCollector is a DeltaSink that accumulates text output.
// Used by spawn_agent to collect sub-agent results without SSE streaming.
type OutputCollector struct {
	buf strings.Builder
}

func (c *OutputCollector) OnText(text string)                        { c.buf.WriteString(text) }
func (c *OutputCollector) OnReasoning(string)                        {}
func (c *OutputCollector) OnToolStart(string, map[string]any)        {}
func (c *OutputCollector) OnToolResult(string, string, error)        {}
func (c *OutputCollector) OnProgress(string, string, string, string) {}
func (c *OutputCollector) OnApprovalRequest(string, string, map[string]any, string) {}
func (c *OutputCollector) OnComplete()                               {}
func (c *OutputCollector) OnError(error)                             {}
func (c *OutputCollector) Result() string                            { return c.buf.String() }
