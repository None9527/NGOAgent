package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
)

func TestSaveMemoryToolWritesToKIStore(t *testing.T) {
	store := knowledge.NewStore(t.TempDir())
	saveMemory := NewSaveMemoryTool(store, nil, 0)

	if saveMemory.Name() != "save_memory" {
		t.Fatalf("expected public tool name save_memory, got %q", saveMemory.Name())
	}

	result, err := saveMemory.Execute(context.Background(), map[string]any{
		"key":     "user_prefers_strict_tests",
		"content": "User prefers rigorous agent E2E tests over shallow QA checks.",
		"tags":    []any{"preference", "testing"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Output, "Knowledge saved: user_prefers_strict_tests") {
		t.Fatalf("unexpected output: %q", result.Output)
	}

	items, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one KI, got %d", len(items))
	}
	if items[0].Title != "user_prefers_strict_tests" {
		t.Fatalf("expected title from key, got %q", items[0].Title)
	}
	if !strings.Contains(items[0].Content, "rigorous agent E2E") {
		t.Fatalf("expected KI content to be persisted, got %q", items[0].Content)
	}
}

func TestSaveKnowledgeToolRemainsLegacyAlias(t *testing.T) {
	store := knowledge.NewStore(t.TempDir())
	saveKnowledge := NewSaveKnowledgeTool(store, nil, 0)

	if saveKnowledge.Name() != "save_knowledge" {
		t.Fatalf("expected legacy tool name save_knowledge, got %q", saveKnowledge.Name())
	}
	if !strings.Contains(saveKnowledge.Description(), "Deprecated alias for save_memory") {
		t.Fatalf("legacy alias description should point to save_memory: %q", saveKnowledge.Description())
	}

	result, err := saveKnowledge.Execute(context.Background(), map[string]any{
		"title":   "legacy_title_arg",
		"content": "Legacy save_knowledge callers still write to KI.",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Output, "Knowledge saved: legacy_title_arg") {
		t.Fatalf("unexpected output: %q", result.Output)
	}

	items, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 || items[0].Title != "legacy_title_arg" {
		t.Fatalf("expected legacy title alias to become KI key, got %#v", items)
	}
}
