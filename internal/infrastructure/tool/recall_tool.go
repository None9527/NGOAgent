package tool

import (
	"context"
	"fmt"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
)

// RecallTool provides unified search across KI, Memory, and Diary.
// It replaces the need for separate read_knowledge and memory_search tools.
type RecallTool struct {
	kiRetriever *knowledge.Retriever // optional: KI semantic search
	memStore    *memory.Store        // optional: vector memory fragments
	diaryStore  *memory.DiaryStore   // optional: daily diary timeline
}

// NewRecallTool creates a recall tool with one or more knowledge sources.
// All parameters are optional — nil sources are simply skipped during search.
func NewRecallTool(ki *knowledge.Retriever, mem *memory.Store, diary *memory.DiaryStore) *RecallTool {
	return &RecallTool{
		kiRetriever: ki,
		memStore:    mem,
		diaryStore:  diary,
	}
}

func (t *RecallTool) Name() string { return "recall" }
func (t *RecallTool) Description() string {
	return `Search across knowledge, memory, and diary for relevant context.
Sources:
- auto: search all sources and merge results (default)
- ki: search Knowledge Items only (distilled, cross-session knowledge)
- memory: search conversation memory fragments (vector-based, with time-decay)
- diary: read daily diary entries (time-axis, recent days)
Use this when you need to recall past knowledge, decisions, patterns, or user preferences.`
}

func (t *RecallTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query describing what you want to recall",
			},
			"source": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "ki", "memory", "diary"},
				"description": "Knowledge source to search (default: auto)",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Max results per source (default: 5)",
			},
			"days": map[string]any{
				"type":        "integer",
				"description": "For diary source: number of recent days to read (default: 7)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *RecallTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	query, _ := args["query"].(string)
	source, _ := args["source"].(string)
	topK := 5
	days := 7

	if query == "" {
		return dtool.TextResult("Error: 'query' is required")
	}
	if source == "" {
		source = "auto"
	}
	if v, ok := args["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}
	if v, ok := args["days"].(float64); ok && v > 0 {
		days = int(v)
	}

	var sb strings.Builder

	switch source {
	case "ki":
		t.searchKI(&sb, query, topK)
	case "memory":
		t.searchMemory(&sb, query, topK)
	case "diary":
		t.searchDiary(&sb, days)
	case "auto":
		t.searchKI(&sb, query, topK)
		t.searchMemory(&sb, query, topK)
		t.searchDiary(&sb, days)
	default:
		return dtool.TextResult(fmt.Sprintf("Error: unknown source %q (use: auto, ki, memory, diary)", source))
	}

	result := sb.String()
	if result == "" {
		return dtool.TextResult("No relevant results found.")
	}
	return dtool.TextResult(result)
}

// ═══════════════════════════════════════════
// Source-specific search methods
// ═══════════════════════════════════════════

func (t *RecallTool) searchKI(sb *strings.Builder, query string, topK int) {
	if t.kiRetriever == nil {
		return
	}

	items, err := t.kiRetriever.Retrieve(query, topK)
	if err != nil || len(items) == 0 {
		return
	}

	for _, item := range items {
		sb.WriteString(fmt.Sprintf("[KI] %s\n", item.Title))
		sb.WriteString(fmt.Sprintf("  摘要: %s\n", item.Summary))
		if item.Content != "" {
			content := item.Content
			if len([]rune(content)) > 500 {
				content = string([]rune(content)[:500]) + "..."
			}
			sb.WriteString(fmt.Sprintf("  内容: %s\n", content))
		}
		sb.WriteString("\n")
	}
}

func (t *RecallTool) searchMemory(sb *strings.Builder, query string, topK int) {
	if t.memStore == nil {
		return
	}

	fragments, err := t.memStore.Search(query, topK)
	if err != nil || len(fragments) == 0 {
		return
	}

	for _, f := range fragments {
		age := formatAge(f.CreatedAt)
		sb.WriteString(fmt.Sprintf("[Memory] (%.2f, %s ago)\n", f.Score, age))
		sb.WriteString(fmt.Sprintf("  %s\n\n", f.Content))
	}
}

func (t *RecallTool) searchDiary(sb *strings.Builder, days int) {
	if t.diaryStore == nil {
		return
	}

	content := t.diaryStore.ReadRecent(days)
	if content == "" {
		return
	}

	sb.WriteString("[Diary] Recent entries:\n")
	sb.WriteString(content)
	sb.WriteString("\n")
}

// formatAge returns a human-readable age string.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
}
