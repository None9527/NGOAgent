package tool

import (
	"context"
	"fmt"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// CommandStatusTool gets the status and output of a background command.
type CommandStatusTool struct {
	sandbox *sandbox.Manager
}

func NewCommandStatusTool(sb *sandbox.Manager) *CommandStatusTool {
	return &CommandStatusTool{sandbox: sb}
}

func (t *CommandStatusTool) Name() string        { return "command_status" }
func (t *CommandStatusTool) Description() string { return prompttext.ToolCommandStatus }

func (t *CommandStatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command_id":   map[string]any{"type": "string", "description": "Background command ID"},
			"wait_seconds": map[string]any{"type": "integer", "description": "Wait for completion (default: 0)"},
			"output_chars": map[string]any{"type": "integer", "description": "Max output characters to return. When set, returns only new output since last check (incremental). Keep small to save tokens."},
		},
		"required": []string{"command_id"},
	}
}

func (t *CommandStatusTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	id, _ := args["command_id"].(string)
	waitSec := 0
	maxChars := 0
	if v, ok := args["wait_seconds"].(float64); ok {
		waitSec = int(v)
	}
	if v, ok := args["output_chars"].(float64); ok && v > 0 {
		maxChars = int(v)
	}

	if id == "" {
		return dtool.ToolResult{Output: "Error: 'command_id' is required"}, nil
	}

	result, err := t.sandbox.GetStatusWithLimit(id, waitSec, maxChars)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	status := "running"
	if result.ExitCode >= 0 {
		status = "done"
	}

	output := fmt.Sprintf("Status: %s\nExit code: %d\nDuration: %v\n\n", status, result.ExitCode, result.Duration)
	if result.Stdout != "" {
		output += "--- stdout ---\n" + result.Stdout + "\n"
	}
	if result.Stderr != "" {
		output += "--- stderr ---\n" + result.Stderr + "\n"
	}
	return dtool.ToolResult{Output: output}, nil
}
