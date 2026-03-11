package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// SendMessageTool saves notification messages to brain for later review.
type SendMessageTool struct {
	brainDir string
}

// NewSendMessageTool creates a send message tool with brain directory.
func NewSendMessageTool(brainDir string) *SendMessageTool {
	return &SendMessageTool{brainDir: brainDir}
}

func (t *SendMessageTool) Name() string        { return "send_message" }
func (t *SendMessageTool) Description() string { return prompttext.ToolSendMessage }

func (t *SendMessageTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel":  map[string]any{"type": "string", "description": "Target channel: 'log' (default), 'alert', 'report'"},
			"message":  map[string]any{"type": "string", "description": "Message content"},
			"priority": map[string]any{"type": "string", "description": "Priority: 'low', 'normal' (default), 'high'"},
		},
		"required": []string{"message"},
	}
}

func (t *SendMessageTool) Execute(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	message, _ := args["message"].(string)
	if message == "" {
		return dtool.ToolResult{Output: "Error: 'message' is required"}, nil
	}

	channel := "log"
	if ch, ok := args["channel"].(string); ok && ch != "" {
		channel = ch
	}
	priority := "normal"
	if p, ok := args["priority"].(string); ok && p != "" {
		priority = p
	}

	// Save to brain/notifications/
	notifDir := filepath.Join(t.brainDir, "notifications")
	os.MkdirAll(notifDir, 0755)

	entry := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"channel":   channel,
		"priority":  priority,
		"message":   message,
	}

	data, _ := json.MarshalIndent(entry, "", "  ")
	filename := fmt.Sprintf("%s_%s.json", time.Now().Format("20060102_150405"), channel)
	filePath := filepath.Join(notifDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error saving message: %v", err)}, nil
	}

	return dtool.ToolResult{Output: fmt.Sprintf("Message sent to '%s' channel (priority: %s)", channel, priority)}, nil
}
