// Package application provides adapter types that bridge domain interfaces
// to infrastructure implementations, preventing import cycles.
package application

import (
	"context"
	"log"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
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
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Reasoning:  m.Reasoning,
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
			Role:       r.Role,
			Content:    r.Content,
			ToolCalls:  r.ToolCalls,
			ToolCallID: r.ToolCallID,
			Reasoning:  r.Reasoning,
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
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Reasoning:  m.Reasoning,
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
			log.Printf("[bootstrap] MarkReady failed: %v", err)
		} else {
			log.Printf("[bootstrap] System marked as ready after first conversation (session=%s)", info.SessionID)
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
