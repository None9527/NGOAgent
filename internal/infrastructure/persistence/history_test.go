package persistence

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryStore_SaveLoadUsesNormalizedMessages(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := NewHistoryStore(db)
	now := time.Now().UTC().Round(time.Second)

	msgs := []HistoryMessage{
		{
			Role:        "user",
			Content:     "hello",
			CreatedAt:   now,
			Attachments: `[{"type":"image","path":"/tmp/a.png","mime_type":"image/png","name":"a.png"}]`,
		},
		{
			Role:       "assistant",
			Content:    "working",
			Reasoning:  "think",
			ToolCalls:  `[{"id":"call-1","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.go\"}"}}]`,
			CreatedAt:  now.Add(time.Second),
			ToolCallID: "",
		},
	}
	if err := store.SaveAll("session-history", msgs); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	var normalizedCount int64
	if err := db.Model(&MessageRecord{}).Where("conversation_id = ?", "session-history").Count(&normalizedCount).Error; err != nil {
		t.Fatalf("count normalized messages: %v", err)
	}
	if normalizedCount != 2 {
		t.Fatalf("expected 2 normalized messages, got %d", normalizedCount)
	}

	loaded, err := store.LoadSession("session-history")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded messages, got %d", len(loaded))
	}
	if loaded[1].Reasoning != "think" || loaded[1].ToolCalls == "" {
		t.Fatalf("expected normalized assistant payloads, got %#v", loaded[1])
	}
	if loaded[0].Attachments == "" {
		t.Fatalf("expected normalized attachments, got %#v", loaded[0])
	}
}

func TestHistoryStore_ToolResultCompletesNormalizedToolCall(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "history-tool-result.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := NewHistoryStore(db)
	now := time.Now().UTC().Round(time.Second)

	msgs := []HistoryMessage{
		{
			Role:      "assistant",
			Content:   "I'll read it.",
			ToolCalls: `[{"id":"call-read","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}]`,
			CreatedAt: now,
		},
		{
			Role:       "tool",
			Content:    "module github.com/ngoclaw/ngoagent",
			ToolCallID: "call-read",
			CreatedAt:  now.Add(time.Second),
		},
	}
	if err := store.SaveAll("session-tool-result", msgs); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	var toolCall MessageToolCallRecord
	if err := db.First(&toolCall, "tool_call_id = ?", "call-read").Error; err != nil {
		t.Fatalf("load normalized tool call: %v", err)
	}
	if toolCall.Status != "completed" {
		t.Fatalf("expected completed tool call, got %q", toolCall.Status)
	}
	if toolCall.ResultJSON == "" {
		t.Fatal("expected normalized tool result JSON")
	}
}

func TestHistoryStore_LoadSession_UsesNormalizedTablesOnly(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "history-backfill.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store := NewHistoryStore(db)
	now := time.Now().UTC().Round(time.Second)
	if err := store.Save("normalized-session", &HistoryMessage{
		Role:        "assistant",
		Content:     "normalized",
		ToolCalls:   `[{"id":"call-2","type":"function","function":{"name":"edit_file","arguments":"{\"path\":\"normalized.go\"}"}}]`,
		Attachments: `[{"type":"file","path":"/tmp/normalized.txt","mime_type":"text/plain","name":"normalized.txt"}]`,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.LoadSession("normalized-session")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Content != "normalized" {
		t.Fatalf("unexpected loaded normalized message: %#v", loaded)
	}

	var normalizedCount int64
	if err := db.Model(&MessageRecord{}).Where("conversation_id = ?", "normalized-session").Count(&normalizedCount).Error; err != nil {
		t.Fatalf("count normalized messages: %v", err)
	}
	if normalizedCount != 1 {
		t.Fatalf("expected normalized message row, got %d", normalizedCount)
	}
	if db.Migrator().HasTable("history_messages") {
		t.Fatal("legacy history_messages table should not exist")
	}
}

func TestHistoryStore_AppendBatchAndDeleteSessionSyncNormalizedTables(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "history-append.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := NewHistoryStore(db)

	if err := store.AppendBatch("append-session", []HistoryMessage{
		{Role: "user", Content: "first", CreatedAt: time.Now().UTC()},
		{Role: "assistant", Content: "second", CreatedAt: time.Now().UTC().Add(time.Second)},
	}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	recent, err := store.LoadSessionRecent("append-session", 1)
	if err != nil {
		t.Fatalf("LoadSessionRecent: %v", err)
	}
	if len(recent) != 1 || recent[0].Content != "second" {
		t.Fatalf("unexpected recent messages: %#v", recent)
	}

	if err := store.DeleteSession("append-session"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	var count int64
	if err := db.Model(&MessageRecord{}).Where("conversation_id = ?", "append-session").Count(&count).Error; err != nil {
		t.Fatalf("count normalized messages after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected normalized messages deleted, got %d", count)
	}
}
