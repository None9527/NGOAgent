package service

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// ──────────────────────────────────────────────
// sanitizeMessages tests
// ──────────────────────────────────────────────

func TestSanitize_EmptySlice(t *testing.T) {
	result := sanitizeMessages(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestSanitize_NoOrphans(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []model.ToolCall{{ID: "tc1", Type: "function"}}},
		{Role: "tool", ToolCallID: "tc1", Content: "result"},
		{Role: "assistant", Content: "done"},
	}
	result := sanitizeMessages(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
}

func TestSanitize_OrphanToolResult_Removed(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", ToolCallID: "orphan_tc", Content: "no matching call"},
		{Role: "assistant", Content: "world"},
	}
	result := sanitizeMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (orphan tool removed), got %d", len(result))
	}
	if result[0].Role != "user" || result[1].Role != "assistant" {
		t.Errorf("expected user+assistant, got %s+%s", result[0].Role, result[1].Role)
	}
}

func TestSanitize_OrphanToolCalls_Stripped(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "thinking", ToolCalls: []model.ToolCall{{ID: "tc_no_result", Type: "function"}}},
		// No tool result for tc_no_result
	}
	result := sanitizeMessages(msgs)
	// Assistant message should remain (has content) but with ToolCalls stripped
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if len(result[1].ToolCalls) != 0 {
		t.Errorf("expected ToolCalls stripped, got %d", len(result[1].ToolCalls))
	}
	if result[1].Content != "thinking" {
		t.Errorf("expected content preserved, got %q", result[1].Content)
	}
}

func TestSanitize_OrphanToolCalls_NoContent_Removed(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "tc_orphan", Type: "function"}}},
		// No content, no result → entire message dropped
	}
	result := sanitizeMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message (empty assistant dropped), got %d", len(result))
	}
}

// ──────────────────────────────────────────────
// enforceTurnOrdering tests
// ──────────────────────────────────────────────

func TestEnforceTurnOrdering_Empty(t *testing.T) {
	result := enforceTurnOrdering(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestEnforceTurnOrdering_SingleMessage(t *testing.T) {
	msgs := []model.Message{{Role: "user", Content: "hello"}}
	result := enforceTurnOrdering(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestEnforceTurnOrdering_ConsecutiveUser_Merged(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "part 1"},
		{Role: "user", Content: "part 2"},
	}
	result := enforceTurnOrdering(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(result))
	}
	if result[0].Content != "part 1\npart 2" {
		t.Errorf("expected merged content, got %q", result[0].Content)
	}
}

func TestEnforceTurnOrdering_AssistantWithToolCalls_NotMerged(t *testing.T) {
	msgs := []model.Message{
		{Role: "assistant", Content: "first", ToolCalls: []model.ToolCall{{ID: "tc1"}}},
		{Role: "tool", ToolCallID: "tc1", Content: "result"},
		{Role: "assistant", Content: "second"},
	}
	result := enforceTurnOrdering(msgs)
	// All 3 should remain — assistant with tool_calls must not be merged
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

func TestEnforceTurnOrdering_SystemPassThrough(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result := enforceTurnOrdering(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("system message should pass through, got role=%s", result[0].Role)
	}
}

func TestEnforceTurnOrdering_ConsecutiveAssistant_Merged(t *testing.T) {
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "part A"},
		{Role: "assistant", Content: "part B"},
	}
	result := enforceTurnOrdering(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 (user + merged assistant), got %d", len(result))
	}
	if result[1].Content != "part A\npart B" {
		t.Errorf("expected merged assistant content, got %q", result[1].Content)
	}
}
