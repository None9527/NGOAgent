// Package application provides adapter types that bridge domain interfaces
// to infrastructure implementations, preventing import cycles.
package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

// historyAdapter bridges domain.HistoryPersister → infrastructure.HistoryStore
// to avoid import cycle (domain cannot import persistence).
type historyAdapter struct {
	store *persistence.HistoryStore
}

func (a *historyAdapter) SaveAll(sessionID string, msgs []service.HistoryExport) error {
	rows := make([]persistence.HistoryMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = persistence.HistoryMessage{
			Role:        m.Role,
			Content:     m.Content,
			ToolCalls:   m.ToolCalls,
			ToolCallID:  m.ToolCallID,
			Reasoning:   m.Reasoning,
			Attachments: m.Attachments,
		}
	}
	return a.store.SaveAll(sessionID, rows)
}

func (a *historyAdapter) LoadAll(sessionID string) ([]service.HistoryExport, error) {
	rows, err := a.store.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}
	exports := make([]service.HistoryExport, len(rows))
	for i, r := range rows {
		exports[i] = service.HistoryExport{
			Role:        r.Role,
			Content:     r.Content,
			ToolCalls:   r.ToolCalls,
			ToolCallID:  r.ToolCallID,
			Reasoning:   r.Reasoning,
			Attachments: r.Attachments,
		}
	}
	return exports, nil
}

func (a *historyAdapter) DeleteSession(sessionID string) error {
	return a.store.DeleteSession(sessionID)
}

func (a *historyAdapter) AppendAll(sessionID string, msgs []service.HistoryExport) error {
	rows := make([]persistence.HistoryMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = persistence.HistoryMessage{
			Role:        m.Role,
			Content:     m.Content,
			ToolCalls:   m.ToolCalls,
			ToolCallID:  m.ToolCallID,
			Reasoning:   m.Reasoning,
			Attachments: m.Attachments,
		}
	}
	return a.store.AppendBatch(sessionID, rows)
}

// sessionRepoAdapter bridges domain.SessionRepo → *persistence.Repository.
type sessionRepoAdapter struct {
	repo *persistence.Repository
	loc  *time.Location
}

func (a *sessionRepoAdapter) CreateConversation(channel, title string) (string, error) {
	conv, err := a.repo.CreateConversation(channel, title)
	if err != nil {
		return "", err
	}
	return conv.ID, nil
}

func (a *sessionRepoAdapter) ListConversations(limit, offset int) ([]service.ConversationInfo, error) {
	convs, err := a.repo.ListConversations(limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]service.ConversationInfo, len(convs))
	for i, c := range convs {
		result[i] = service.ConversationInfo{
			ID:        c.ID,
			Title:     c.Title,
			Channel:   c.Channel,
			CreatedAt: c.CreatedAt.In(a.loc).Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: c.UpdatedAt.In(a.loc).Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return result, nil
}

func (a *sessionRepoAdapter) UpdateTitle(id, title string) error {
	return a.repo.UpdateConversationTitle(id, title)
}

func (a *sessionRepoAdapter) Touch(id string) error {
	return a.repo.TouchConversation(id)
}

func (a *sessionRepoAdapter) DeleteConversation(id string) error {
	return a.repo.DeleteConversation(id)
}

// toolRegistryAdapter bridges domain.ToolRegistry → *tool.Registry.
type toolRegistryAdapter struct {
	reg *tool.Registry
}

func (a *toolRegistryAdapter) List() []service.ToolInfo {
	infos := a.reg.List()
	result := make([]service.ToolInfo, len(infos))
	for i, ti := range infos {
		result[i] = service.ToolInfo{Name: ti.Name, Enabled: ti.Enabled}
	}
	return result
}

func (a *toolRegistryAdapter) Enable(name string) error  { return a.reg.Enable(name) }
func (a *toolRegistryAdapter) Disable(name string) error { return a.reg.Disable(name) }

// bootstrapReadyHook marks the system as "ready" after the first successful conversation.
type bootstrapReadyHook struct{}

func (h *bootstrapReadyHook) OnRunComplete(_ context.Context, info service.RunInfo) {
	if info.Steps > 0 {
		if err := config.MarkReady(); err != nil {
			slog.Info(fmt.Sprintf("[bootstrap] MarkReady failed: %v", err))
		} else {
			slog.Info(fmt.Sprintf("[bootstrap] System marked as ready after first conversation (session=%s)", info.SessionID))
		}
	}
}

// diaryAdapter bridges service.DiaryAppender → memory.DiaryStore
// to avoid import cycle (service cannot import memory).
type diaryAdapter struct {
	store *memory.DiaryStore
}

func (a *diaryAdapter) Append(entry service.DiaryEntry) error {
	return a.store.Append(memory.DiaryEntry{
		Time:      entry.Time,
		SessionID: entry.SessionID,
		Task:      entry.Task,
		ToolCount: entry.ToolCount,
		Steps:     entry.Steps,
		Result:    entry.Result,
	})
}

// securityAdapter bridges domain.SecurityChecker → *security.Hook.
// Converts infrastructure types (Decision, PendingApproval) to domain equivalents.
type securityAdapter struct {
	hook *security.Hook
}

func (a *securityAdapter) BeforeToolCall(ctx context.Context, toolName string, args map[string]any) (service.SecurityDecision, string) {
	decision, reason := a.hook.BeforeToolCall(ctx, toolName, args)
	return service.SecurityDecision(decision), reason // same underlying int
}

func (a *securityAdapter) AfterToolCall(ctx context.Context, toolName string, result string, err error) {
	a.hook.AfterToolCall(ctx, toolName, result, err)
}

func (a *securityAdapter) RequestApproval(toolName string, args map[string]any, reason string) *service.ApprovalTicket {
	pending := a.hook.RequestApproval(toolName, args, reason)
	return &service.ApprovalTicket{
		ID:     pending.ID,
		Result: pending.Result,
	}
}

func (a *securityAdapter) ListPendingApprovals() []service.ApprovalSnapshot {
	pending := a.hook.ListPending()
	out := make([]service.ApprovalSnapshot, 0, len(pending))
	for _, p := range pending {
		out = append(out, service.ApprovalSnapshot{
			ID:        p.ID,
			ToolName:  p.ToolName,
			Args:      p.Args,
			Reason:    p.Reason,
			Requested: p.Created,
		})
	}
	return out
}

func (a *securityAdapter) CleanupPending(approvalID string) {
	a.hook.CleanupPending(approvalID)
}
