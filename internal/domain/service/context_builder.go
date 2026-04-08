// Package service — context_builder.go implements the L1/L2/L3 context
// passing engine for subagent spawning.
//
// Context levels:
//
//	L1 (task):    Task + scratchDir only. ~200 tokens.
//	L2 (summary): Task + extractParentIntent summary. ~500 tokens. Default.
//	L3 (history): Task + recent N rounds of parent conversation. ~2000+ tokens.
package service

import (
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// BuildSubagentContext constructs the full task string to inject into a subagent.
// It enriches the raw task with parent context at the level specified by AgentDefinition.
func BuildSubagentContext(task string, parentHistory []llm.Message, def *model.AgentDefinition) string {
	if def == nil {
		return task // L1 equivalent for undefined agents
	}

	switch def.ContextLevel {
	case model.ContextWithHistory:
		return buildL3Context(task, parentHistory, 6)
	case model.ContextWithSummary:
		return buildL2Context(task, parentHistory)
	default: // ContextTaskOnly or empty
		return task
	}
}

// buildL2Context attaches a distilled parent intent summary to the task.
// Token cost: ~500 (summary is extracted, not raw history copied).
func buildL2Context(task string, history []llm.Message) string {
	summary := extractParentIntent(history, 4)
	if summary == "" {
		return task
	}
	var sb strings.Builder
	sb.WriteString("<parent_context>\n")
	sb.WriteString(summary)
	sb.WriteString("\n</parent_context>\n\n")
	sb.WriteString(task)
	return sb.String()
}

// buildL3Context attaches recent parent conversation rounds to the task.
// Token cost: ~2000+ depending on maxRounds.
func buildL3Context(task string, history []llm.Message, maxRounds int) string {
	excerpt := formatRecentHistory(history, maxRounds)
	if excerpt == "" {
		return task
	}
	var sb strings.Builder
	sb.WriteString("<parent_conversation_excerpt>\n")
	sb.WriteString(excerpt)
	sb.WriteString("\n</parent_conversation_excerpt>\n\n")
	sb.WriteString(task)
	return sb.String()
}

// extractParentIntent distills relevant context from the most recent parent messages.
// It extracts the last N user messages and any assistant planning text.
// Skips tool call messages to keep the summary token-efficient.
func extractParentIntent(history []llm.Message, recentN int) string {
	if len(history) == 0 {
		return ""
	}

	var parts []string
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < recentN; i-- {
		msg := history[i]
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				parts = append([]string{fmt.Sprintf("User: %s", truncateStr(msg.Content, 300))}, parts...)
				seen++
			}
		case "assistant":
			if msg.Content != "" && seen < recentN {
				parts = append([]string{fmt.Sprintf("Assistant (intent): %s", truncateStr(msg.Content, 200))}, parts...)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// formatRecentHistory returns a formatted excerpt of the last N rounds (user+assistant pairs).
// Skips tool messages. Used for L3 context.
func formatRecentHistory(history []llm.Message, maxRounds int) string {
	if len(history) == 0 {
		return ""
	}

	var lines []string
	roundsFound := 0

	for i := len(history) - 1; i >= 0 && roundsFound < maxRounds; i-- {
		msg := history[i]
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				lines = append([]string{fmt.Sprintf("[User]\n%s", truncateStr(msg.Content, 500))}, lines...)
				roundsFound++
			}
		case "assistant":
			if msg.Content != "" {
				lines = append([]string{fmt.Sprintf("[Assistant]\n%s", truncateStr(msg.Content, 400))}, lines...)
			}
		// Skip role=="tool" — raw tool outputs are too noisy for subagent context
		}
	}

	return strings.Join(lines, "\n\n")
}

// truncateStr shortens s to maxLen with "..." suffix if needed.
func truncateStr(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
