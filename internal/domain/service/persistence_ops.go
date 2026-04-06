// Package service — History persistence operations.
// Extracted from run.go (Sprint 1-3): incremental and full history persistence to DB.
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// serializeAttachments converts model.Attachment slice to JSON string for DB storage.
func serializeAttachments(atts []model.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	b, _ := json.Marshal(atts)
	return string(b)
}

// msgToExport converts an in-memory model.Message to a persistable HistoryExport.
func msgToExport(m model.Message) HistoryExport {
	var tcStr string
	if len(m.ToolCalls) > 0 {
		tc, _ := json.Marshal(m.ToolCalls)
		tcStr = string(tc)
	}
	return HistoryExport{
		Role:        m.Role,
		Content:     m.Content,
		ToolCalls:   tcStr,
		ToolCallID:  m.ToolCallID,
		Reasoning:   m.Reasoning,
		Attachments: serializeAttachments(m.Attachments),
	}
}

// persistHistory incrementally writes new messages to storage.
// Only appends messages added since the last persist (tracked by persistedCount).
func (a *AgentLoop) persistHistory() {
	if a.deps.HistoryStore == nil {
		return
	}
	sid := a.SessionID()
	if sid == "" {
		return
	}
	a.mu.Lock()
	baseline := a.persistedCount
	if baseline >= len(a.history) {
		a.mu.Unlock()
		return // nothing new
	}
	newMsgs := a.history[baseline:]
	exports := make([]HistoryExport, len(newMsgs))
	for i, m := range newMsgs {
		exports[i] = msgToExport(m)
	}
	a.persistedCount = len(a.history)
	a.mu.Unlock()
	if err := a.deps.HistoryStore.AppendAll(sid, exports); err != nil {
		slog.Info(fmt.Sprintf("[history] incremental persist failed: %v", err))
		// Roll back persistedCount so failed messages will be retried
		a.mu.Lock()
		a.persistedCount = baseline
		a.mu.Unlock()
	}
}

// persistFullHistory does a destructive full replace of the DB history.
// Called ONLY after doCompact/forceTruncate which intentionally restructure the history.
func (a *AgentLoop) persistFullHistory() {
	if a.deps.HistoryStore == nil {
		return
	}
	sid := a.SessionID()
	if sid == "" {
		return
	}
	a.mu.Lock()
	exports := make([]HistoryExport, len(a.history))
	for i, m := range a.history {
		exports[i] = msgToExport(m)
	}
	a.persistedCount = len(a.history)
	a.mu.Unlock()
	if err := a.deps.HistoryStore.SaveAll(sid, exports); err != nil {
		slog.Info(fmt.Sprintf("[history] full persist failed: %v", err))
	}
}

// persistTranscript saves the full history of a subagent run to the DB.
// Uses the provided runID as the session key (e.g. "parentSID:sub:shortUUID"),
// making transcripts queryable by parent session for debugging and audit.
func (a *AgentLoop) persistTranscript(runID string) {
	if a.deps.HistoryStore == nil || runID == "" {
		return
	}
	a.mu.Lock()
	exports := make([]HistoryExport, len(a.history))
	for i, m := range a.history {
		exports[i] = msgToExport(m)
	}
	a.mu.Unlock()
	if err := a.deps.HistoryStore.SaveAll(runID, exports); err != nil {
		slog.Info(fmt.Sprintf("[transcript] subagent persist failed for %s: %v", runID, err))
	} else {
		slog.Info(fmt.Sprintf("[transcript] saved %d messages for subagent %s", len(exports), runID))
	}
}
