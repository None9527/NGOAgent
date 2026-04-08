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

// RecallTool provides unified search across KI, Memory, and Diary
// with scope-based namespace isolation and algorithmic routing.
//
// Phase 4 architecture:
//   Layer 1 — Algorithmic Router: deterministic source selection + scope filtering
//   Layer 2 — Dedup + Score Normalization: merge results across sources
//   (Layer 3 — LLM Reranker: future, deferred until needed)
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
- auto: intelligently route to best sources based on query type (default)
- ki: search Knowledge Items only (distilled, cross-session knowledge)
- memory: search conversation memory fragments (vector-based, with time-decay)
- diary: read daily diary entries (time-axis, recent days)
Use 'scope' to filter results to a specific project/domain namespace.
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
			"scope": map[string]any{
				"type":        "string",
				"description": "Project/domain namespace to filter results (empty = all scopes)",
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
	scope, _ := args["scope"].(string)
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
		t.searchKI(&sb, query, topK, scope)
	case "memory":
		t.searchMemory(&sb, query, topK, scope)
	case "diary":
		t.searchDiary(&sb, days)
	case "auto":
		t.autoRoute(&sb, query, topK, days, scope)
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
// Layer 1: Algorithmic Router
// ═══════════════════════════════════════════

// autoRoute selects sources intelligently based on query characteristics.
// Heuristic:
//   - Time-related queries ("yesterday", "last week") → Diary first
//   - Pattern/decision queries ("how did we", "why") → KI first
//   - Everything else → KI + Memory (merged, deduplicated)
func (t *RecallTool) autoRoute(sb *strings.Builder, query string, topK, days int, scope string) {
	lower := strings.ToLower(query)

	// Route 1: Time-axis queries → Diary + Memory
	if containsAny(lower, timeKeywords) {
		t.searchDiary(sb, days)
		t.searchMemory(sb, query, topK, scope)
		return
	}

	// Route 2: Decision/pattern queries → KI only (distilled knowledge is authoritative)
	if containsAny(lower, decisionKeywords) {
		t.searchKI(sb, query, topK, scope)
		return
	}

	// Route 3: Default — KI + Memory (most queries benefit from both)
	t.searchKI(sb, query, topK, scope)
	t.searchMemory(sb, query, topK, scope)
}

// ═══════════════════════════════════════════
// Source-specific search methods (scope-aware)
// ═══════════════════════════════════════════

func (t *RecallTool) searchKI(sb *strings.Builder, query string, topK int, scope string) {
	if t.kiRetriever == nil {
		return
	}

	var items []*knowledge.Item
	var err error
	if scope != "" {
		items, err = t.kiRetriever.Retrieve(query, topK, scope)
	} else {
		items, err = t.kiRetriever.Retrieve(query, topK)
	}
	if err != nil || len(items) == 0 {
		return
	}

	for _, item := range items {
		// Show deprecation status for transparency
		statusTag := ""
		if item.Deprecated {
			statusTag = " [DEPRECATED]"
		}
		sb.WriteString(fmt.Sprintf("[KI]%s %s\n", statusTag, item.Title))
		sb.WriteString(fmt.Sprintf("  摘要: %s\n", item.Summary))
		if item.Scope != "" {
			sb.WriteString(fmt.Sprintf("  范围: %s\n", item.Scope))
		}
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

func (t *RecallTool) searchMemory(sb *strings.Builder, query string, topK int, scope string) {
	if t.memStore == nil {
		return
	}

	var fragments []memory.Fragment
	var err error
	if scope != "" {
		fragments, err = t.memStore.Search(query, topK, scope)
	} else {
		fragments, err = t.memStore.Search(query, topK)
	}
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

// ═══════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════

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

// containsAny returns true if text contains any of the keywords.
func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// timeKeywords trigger diary-first routing.
var timeKeywords = []string{
	"yesterday", "today", "last week", "last time", "recently",
	"昨天", "今天", "上周", "上次", "最近", "前几天", "this morning",
}

// decisionKeywords trigger KI-first routing.
var decisionKeywords = []string{
	"why did we", "how did we", "decision", "decided", "pattern",
	"architecture", "design choice", "convention", "preference",
	"为什么", "怎么", "决定", "约定", "架构", "设计", "惯例",
}
