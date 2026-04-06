package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
)

// NotifyUserTool presents a message to the user and optionally yields control.
// This is the protocol-level mechanism for plan-then-stop workflow.
// When blocked_on_user=true, the tool returns SignalYield which causes
// the agent loop to terminate (mirrors Anti's TERMINAL_STEP_TYPE mechanism).
type NotifyUserTool struct{}

func NewNotifyUserTool() *NotifyUserTool {
	return &NotifyUserTool{}
}

func (t *NotifyUserTool) Name() string { return "notify_user" }
func (t *NotifyUserTool) Description() string {
	return `Present a message to the user.
- message: Concise notice or question.
- blocked_on_user: If true, agent PAUSES for response.
- paths_to_review: Optional paths for review.`
}

func (t *NotifyUserTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "The message content to show the user. Keep concise.",
			},
			"paths_to_review": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Absolute file paths for the user to review (e.g. plan.md).",
			},
			"blocked_on_user": map[string]any{
				"type":        "boolean",
				"description": "If true, the agent pauses and waits for user response before continuing.",
			},
		},
		"required": []string{"message"},
	}
}

func (t *NotifyUserTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	message, _ := args["message"].(string)
	blocked, _ := args["blocked_on_user"].(bool)

	if message == "" {
		return dtool.ToolResult{Output: "Error: 'message' is required"}, nil
	}

	// Collect paths_to_review
	var paths []string
	if raw, ok := args["paths_to_review"].([]any); ok {
		for _, p := range raw {
			if s, ok := p.(string); ok {
				paths = append(paths, s)
			}
		}
	}

	// Save the notification to brain for persistence
	brainDir := brain.BrainDirFromContext(ctx)
	if brainDir != "" {
		notif := map[string]any{
			"message":         message,
			"paths_to_review": paths,
			"blocked_on_user": blocked,
		}
		data, _ := json.MarshalIndent(notif, "", "  ")
		notifPath := filepath.Join(brainDir, "last_notification.json")
		os.WriteFile(notifPath, data, 0644)
	}

	// Build output text for LLM context
	output := "Message delivered to user."
	if blocked {
		output += " Waiting for their response. Do NOT make any more tool calls."
	}

	// If blocked_on_user, return SignalYield to terminate the loop
	if blocked {
		return dtool.ToolResult{
			Output: output,
			Signal: dtool.SignalYield,
			Payload: map[string]any{
				"message":         message,
				"paths_to_review": paths,
				"blocked_on_user": true,
			},
		}, nil
	}

	// Non-blocking: just deliver the message, loop continues
	return dtool.ToolResult{
		Output: fmt.Sprintf("Message delivered: %s", message),
	}, nil
}
