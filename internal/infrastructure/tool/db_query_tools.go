package tool

import (
	"context"
	"fmt"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
)

// ═══════════════════════════════════════════
// Phase 5: Agent Database Query Tools
// Give the agent direct SQL-level access to its own history and evolution data.
// ═══════════════════════════════════════════

// ReadHistoryTool lets the agent query its own persisted conversation history.
type ReadHistoryTool struct {
	historyStore *persistence.HistoryStore
}

// NewReadHistoryTool creates a history query tool.
func NewReadHistoryTool(hs *persistence.HistoryStore) *ReadHistoryTool {
	return &ReadHistoryTool{historyStore: hs}
}

func (t *ReadHistoryTool) Name() string { return "read_history" }
func (t *ReadHistoryTool) Description() string {
	return `Query persisted conversation history from the database.
Returns messages from a specific session, ordered by time.
Use 'limit' to control how many recent messages to retrieve.
Use this to recover context from past sessions or review what happened in a conversation.`
}

func (t *ReadHistoryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Session ID to query history from",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max number of recent messages to return (default: 20)",
			},
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "user", "assistant", "tool"},
				"description": "Filter by message role (default: all)",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *ReadHistoryTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	if t.historyStore == nil {
		return dtool.TextResult("Error: history store not available")
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return dtool.TextResult("Error: 'session_id' is required")
	}

	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	roleFilter, _ := args["role"].(string)
	if roleFilter == "" {
		roleFilter = "all"
	}

	msgs, err := t.historyStore.LoadSessionRecent(sessionID, limit)
	if err != nil {
		return dtool.TextResult(fmt.Sprintf("Error loading history: %v", err))
	}

	if len(msgs) == 0 {
		return dtool.TextResult(fmt.Sprintf("No messages found for session %s", sessionID))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Session %s — %d messages:\n\n", sessionID, len(msgs)))

	shown := 0
	for _, msg := range msgs {
		if roleFilter != "all" && msg.Role != roleFilter {
			continue
		}

		timeStr := msg.CreatedAt.Format("15:04:05")
		content := msg.Content
		if len([]rune(content)) > 300 {
			content = string([]rune(content)[:300]) + "..."
		}

		sb.WriteString(fmt.Sprintf("[%s] %s:\n%s\n\n", timeStr, msg.Role, content))
		shown++
	}

	if shown == 0 {
		return dtool.TextResult(fmt.Sprintf("No %s messages found in session %s", roleFilter, sessionID))
	}

	return dtool.TextResult(sb.String())
}

// ═══════════════════════════════════════════
// ReadEvoTraceTool — Agent evolution analytics
// ═══════════════════════════════════════════

// ReadEvoTraceTool lets the agent query its own execution traces and evaluations.
type ReadEvoTraceTool struct {
	evoStore *persistence.EvoStore
}

// NewReadEvoTraceTool creates an evo trace query tool.
func NewReadEvoTraceTool(es *persistence.EvoStore) *ReadEvoTraceTool {
	return &ReadEvoTraceTool{evoStore: es}
}

func (t *ReadEvoTraceTool) Name() string { return "read_evo_trace" }
func (t *ReadEvoTraceTool) Description() string {
	return `Query agent evolution traces and evaluations from the database.
Returns execution traces, scores, and repair history for self-improvement analysis.
Use this to analyze past performance, identify error patterns, and review successful repair strategies.`
}

func (t *ReadEvoTraceTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Session ID to query traces from (optional: if empty, returns recent traces)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max number of recent traces to return (default: 10)",
			},
			"include_eval": map[string]any{
				"type":        "boolean",
				"description": "Include evaluation results (default: true)",
			},
		},
	}
}

func (t *ReadEvoTraceTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	if t.evoStore == nil {
		return dtool.TextResult("Error: evo store not available")
	}

	sessionID, _ := args["session_id"].(string)
	limit := 10
	includeEval := true

	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if v, ok := args["include_eval"].(bool); ok {
		includeEval = v
	}

	var sb strings.Builder

	if sessionID != "" {
		// Query specific session
		traces, err := t.evoStore.GetTraces(sessionID)
		if err != nil {
			return dtool.TextResult(fmt.Sprintf("Error loading traces: %v", err))
		}
		if len(traces) == 0 {
			return dtool.TextResult(fmt.Sprintf("No traces found for session %s", sessionID))
		}

		sb.WriteString(fmt.Sprintf("Session %s — %d traces:\n\n", sessionID, len(traces)))
		for _, trace := range traces {
			writeTrace(&sb, trace)
		}

		if includeEval {
			evals, err := t.evoStore.GetEvaluations(sessionID)
			if err == nil && len(evals) > 0 {
				sb.WriteString("\n--- Evaluations ---\n\n")
				for _, eval := range evals {
					sb.WriteString(fmt.Sprintf("  Score: %.2f | Passed: %v | Error: %s\n",
						eval.Score, eval.Passed, eval.ErrorType))
					if eval.Feedback != "" {
						sb.WriteString(fmt.Sprintf("  Feedback: %s\n", eval.Feedback))
					}
				}
			}
		}
	} else {
		// Query recent traces across all sessions
		traces, err := t.evoStore.GetRecentTraces(limit)
		if err != nil {
			return dtool.TextResult(fmt.Sprintf("Error loading recent traces: %v", err))
		}
		if len(traces) == 0 {
			return dtool.TextResult("No recent traces found")
		}

		sb.WriteString(fmt.Sprintf("Recent %d traces:\n\n", len(traces)))
		for _, trace := range traces {
			writeTrace(&sb, trace)
		}
	}

	return dtool.TextResult(sb.String())
}

// writeTrace formats a single EvoTrace for display.
func writeTrace(sb *strings.Builder, trace persistence.EvoTrace) {
	sb.WriteString(fmt.Sprintf("Run #%d [%s] model=%s tokens=%d/%d duration=%dms\n",
		trace.RunIndex, trace.CreatedAt.Format("2006-01-02 15:04"),
		trace.Model, trace.TokensIn, trace.TokensOut, trace.Duration))
	if trace.UserMessage != "" {
		msg := trace.UserMessage
		if len([]rune(msg)) > 100 {
			msg = string([]rune(msg)[:100]) + "..."
		}
		sb.WriteString(fmt.Sprintf("  Task: %s\n", msg))
	}
	if trace.Summary != "" {
		sb.WriteString(fmt.Sprintf("  Summary: %s\n", trace.Summary))
	}
	sb.WriteString("\n")
}
