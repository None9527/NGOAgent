package service

import (
	"log"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// sanitizeMessages fixes orphan tool/assistant blocks in the message history.
//
// Two cases are handled:
//  1. Orphan tool_calls: assistant message with ToolCalls but no subsequent tool result → strip ToolCalls.
//  2. Orphan tool results: tool role message whose ToolCallID has no matching assistant tool_call → remove.
//
// This can happen after context compaction, pruning, or error recovery.
// Ported from gateway/internal/domain/service/sanitize.go.
func sanitizeMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return messages
	}

	// Pass 1: collect all tool_call IDs from assistant messages
	callIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					callIDs[tc.ID] = true
				}
			}
		}
	}

	// Pass 2: collect all tool result IDs
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			resultIDs[msg.ToolCallID] = true
		}
	}

	// Pass 3: rebuild — fix orphans in both directions
	result := make([]llm.Message, 0, len(messages))
	stripped := 0
	for _, msg := range messages {
		switch {
		case msg.Role == "tool" && msg.ToolCallID != "":
			// Orphan tool result: no matching assistant tool_call → drop
			if !callIDs[msg.ToolCallID] {
				stripped++
				continue
			}
			result = append(result, msg)

		case msg.Role == "assistant" && len(msg.ToolCalls) > 0:
			// Orphan tool_calls: strip tool_calls that have no matching result
			hasOrphan := false
			for _, tc := range msg.ToolCalls {
				if !resultIDs[tc.ID] {
					hasOrphan = true
					break
				}
			}
			if hasOrphan {
				// Strip ALL tool calls if any are orphaned (safe: text content preserved)
				cleaned := msg
				cleaned.ToolCalls = nil
				// Only keep if there's actual text content
				if cleaned.Content != "" {
					result = append(result, cleaned)
				}
				stripped++
			} else {
				result = append(result, msg)
			}

		default:
			result = append(result, msg)
		}
	}

	if stripped > 0 {
		log.Printf("[sanitize] fixed %d orphan messages (%d→%d)", stripped, len(messages), len(result))
	}
	return result
}

// enforceTurnOrdering merges consecutive same-role messages to ensure
// strict user→assistant alternation. Required by some LLM APIs.
// System and tool messages are passed through unchanged.
func enforceTurnOrdering(messages []llm.Message) []llm.Message {
	if len(messages) <= 1 {
		return messages
	}

	result := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" || msg.Role == "tool" {
			result = append(result, msg)
			continue
		}

		// Never merge assistant messages with tool_calls — they must stay paired
		// with their subsequent tool result messages for API schema compliance.
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			result = append(result, msg)
			continue
		}

		// If last non-system/tool message has same role AND no tool_calls, merge content
		if len(result) > 0 && result[len(result)-1].Role == msg.Role &&
			len(result[len(result)-1].ToolCalls) == 0 {
			last := &result[len(result)-1]
			if msg.Content != "" {
				if last.Content != "" {
					last.Content += "\n" + msg.Content
				} else {
					last.Content = msg.Content
				}
			}
			continue
		}

		result = append(result, msg)
	}

	return result
}
