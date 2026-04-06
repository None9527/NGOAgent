package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// DiaryAppender abstracts the diary store (avoids import cycle with memory package).
type DiaryAppender interface {
	Append(entry DiaryEntry) error
}

// DiaryEntry mirrors memory.DiaryEntry for use in the service layer.
type DiaryEntry struct {
	Time      time.Time
	SessionID string
	Task      string
	ToolCount int
	Steps     int
	Result    string
}

// DiaryHook records a diary entry after each agent run completes.
// Implements PostRunHook interface.
type DiaryHook struct {
	diary DiaryAppender
}

// NewDiaryHook creates a diary recording hook.
func NewDiaryHook(diary DiaryAppender) *DiaryHook {
	return &DiaryHook{diary: diary}
}

// OnRunComplete extracts a task summary from the run info and appends a diary entry.
func (h *DiaryHook) OnRunComplete(ctx context.Context, info RunInfo) {
	if h.diary == nil || info.SessionID == "" {
		return
	}

	// Skip very short interactions (pure Q&A with no tool usage)
	if info.Steps < 1 {
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Info(fmt.Sprintf("[hook] panic in DiaryHook: %v", r))
			}
		}()

		task := extractTaskSummary(info)
		result := "✅ 完成"
		if info.FinalContent != "" && strings.Contains(info.FinalContent, "Error") {
			result = "⚠️ 有错误"
		}

		entry := DiaryEntry{
			Time:      time.Now(),
			SessionID: info.SessionID,
			Task:      task,
			ToolCount: info.ToolCalls,
			Steps:     info.Steps,
			Result:    result,
		}

		if err := h.diary.Append(entry); err != nil {
			slog.Info(fmt.Sprintf("[hook] diary append failed: %v", err))
		} else {
			slog.Info(fmt.Sprintf("[hook] diary entry added: session=%s task=%q", info.SessionID, task))
		}
	}()
}

// extractTaskSummary derives a short task description from RunInfo.
func extractTaskSummary(info RunInfo) string {
	// Use the user's first message as task description (truncated)
	msg := info.UserMessage
	if msg == "" {
		msg = "unknown task"
	}
	// Truncate to ~80 chars
	runes := []rune(msg)
	if len(runes) > 80 {
		msg = string(runes[:80]) + "..."
	}
	// Single line
	msg = strings.ReplaceAll(msg, "\n", " ")
	return fmt.Sprintf("%s (steps=%d, tools=%d)", msg, info.Steps, info.ToolCalls)
}
