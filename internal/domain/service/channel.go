// Package service — channel.go defines the AgentChannel protocol.
// Each channel type (chat, subagent, forge) implements this interface,
// controlling how an AgentLoop receives input, produces output, and handles completion.
package service

import "log"

// AgentChannel defines the protocol for a specific agent execution context.
// Different channels have different output sinks, tool policies, and completion behaviors.
type AgentChannel interface {
	// Name returns the channel type identifier: "chat", "subagent", "forge".
	Name() string

	// DeltaSink returns the output sink for this channel.
	// Chat → SSE writer, Subagent → OutputCollector.
	DeltaSink() DeltaSink

	// OnComplete is called when the agent run finishes.
	// Subagent channels use this to announce results back to the parent.
	OnComplete(runID string, result string, err error)
}

// ────────────────────────────────────────────
// ChatChannel: primary user interaction
// ────────────────────────────────────────────

// ChatChannel handles direct user ↔ agent chat via SSE streaming.
type ChatChannel struct {
	sink DeltaSink
}

func NewChatChannel(sink DeltaSink) *ChatChannel {
	return &ChatChannel{sink: sink}
}

func (c *ChatChannel) Name() string          { return "chat" }
func (c *ChatChannel) DeltaSink() DeltaSink  { return c.sink }
func (c *ChatChannel) OnComplete(string, string, error) {} // Chat streams directly, no announce needed

// ────────────────────────────────────────────
// SubagentChannel: child agent with announce-back
// ────────────────────────────────────────────

// SubagentChannel runs a child agent task and announces results to the parent.
type SubagentChannel struct {
	collector  *OutputCollector
	announceFn func(runID string, result string) // Posts result to parent session
}

// NewSubagentChannel creates a subagent channel with an announce callback.
// announceFn is called when the subagent completes, injecting results into the parent.
func NewSubagentChannel(announceFn func(runID string, result string)) *SubagentChannel {
	return &SubagentChannel{
		collector:  &OutputCollector{},
		announceFn: announceFn,
	}
}

func (c *SubagentChannel) Name() string          { return "subagent" }
func (c *SubagentChannel) DeltaSink() DeltaSink  { return c.collector }
func (c *SubagentChannel) Collector() *OutputCollector { return c.collector }
func (c *SubagentChannel) OnComplete(runID string, _ string, err error) {
	result := c.collector.Result()
	if err != nil {
		result = "Sub-agent error: " + err.Error() + "\n" + result
	}
	if c.announceFn != nil {
		c.announceFn(runID, result)
	}
}

// Result returns the accumulated output (for synchronous callers).
func (c *SubagentChannel) Result() string { return c.collector.Result() }

// ────────────────────────────────────────────
// ForgeChannel: structured test environments
// ────────────────────────────────────────────

// ForgeChannel handles forge (test/eval) execution contexts.
type ForgeChannel struct {
	sink DeltaSink
}

func NewForgeChannel(sink DeltaSink) *ForgeChannel {
	return &ForgeChannel{sink: sink}
}

func (c *ForgeChannel) Name() string          { return "forge" }
func (c *ForgeChannel) DeltaSink() DeltaSink  { return c.sink }
func (c *ForgeChannel) OnComplete(string, string, error) {}

// ────────────────────────────────────────────
// LogSink: simple DeltaSink that logs output
// ────────────────────────────────────────────

// LogSink is a DeltaSink that writes to log.Printf.
type LogSink struct {
	Prefix string
}

func (s *LogSink) OnText(text string) {
	if text != "" {
		log.Printf("%s text: %s", s.Prefix, text)
	}
}
func (s *LogSink) OnReasoning(string)                        {}
func (s *LogSink) OnToolStart(string, string, map[string]any)        {}
func (s *LogSink) OnToolResult(string, string, string, error)        {}
func (s *LogSink) OnProgress(string, string, string, string) {}
func (s *LogSink) OnPlanReview(string, []string)              {}
func (s *LogSink) OnApprovalRequest(string, string, map[string]any, string) {}
func (s *LogSink) OnTitleUpdate(string, string) {}
func (s *LogSink) OnAutoWakeStart()              {}
func (s *LogSink) OnComplete() {}
func (s *LogSink) OnError(err error) {
	if err != nil {
		log.Printf("%s error: %v", s.Prefix, err)
	}
}
