package service

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// AuditHook logs tool usage and tracks basic execution stats.
// Implements ToolHook for before/after tool lifecycle events.
type AuditHook struct {
	totalCalls  atomic.Int64
	totalErrors atomic.Int64
}

// NewAuditHook creates an audit hook for tool execution logging.
func NewAuditHook() *AuditHook {
	return &AuditHook{}
}

func (h *AuditHook) BeforeTool(ctx context.Context, name string, args map[string]any) (map[string]any, bool) {
	log.Printf("[audit] tool_start: %s", name)
	return args, false
}

func (h *AuditHook) AfterTool(ctx context.Context, name string, output string, err error) {
	h.totalCalls.Add(1)
	if err != nil {
		h.totalErrors.Add(1)
		log.Printf("[audit] tool_error: %s err=%v (total=%d errors=%d)",
			name, err, h.totalCalls.Load(), h.totalErrors.Load())
		return
	}
	outLen := len(output)
	if outLen > 200 {
		outLen = 200
	}
	_ = outLen
	log.Printf("[audit] tool_done: %s output=%d bytes (total=%d)",
		name, len(output), h.totalCalls.Load())
}

// Stats returns current audit counters.
func (h *AuditHook) Stats() (calls, errors int64) {
	return h.totalCalls.Load(), h.totalErrors.Load()
}

// CompactAuditHook logs compaction events.
// Implements CompactHook for before/after compact lifecycle events.
type CompactAuditHook struct{}

func NewCompactAuditHook() *CompactAuditHook { return &CompactAuditHook{} }

func (h *CompactAuditHook) BeforeCompact(ctx context.Context, history []llm.Message) {
	log.Printf("[audit] compact_start: %d messages", len(history))
}

func (h *CompactAuditHook) AfterCompact(ctx context.Context, compacted []llm.Message) {
	log.Printf("[audit] compact_done: %d messages remaining", len(compacted))
}

