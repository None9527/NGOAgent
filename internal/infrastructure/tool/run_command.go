package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// RunCommandTool executes shell commands via the sandbox.
type RunCommandTool struct {
	sandbox *sandbox.Manager
}

func NewRunCommandTool(sb *sandbox.Manager) *RunCommandTool {
	return &RunCommandTool{sandbox: sb}
}

func (t *RunCommandTool) Name() string        { return "run_command" }
func (t *RunCommandTool) Description() string { return prompttext.ToolRunCommand }

func (t *RunCommandTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string", "description": "Shell command to execute"},
			"cwd":        map[string]any{"type": "string", "description": "Working directory (optional)"},
			"timeout_ms": map[string]any{"type": "integer", "description": "Timeout in milliseconds (default: 30000)"},
			"background": map[string]any{"type": "boolean", "description": "Run in background (default: false)"},
		},
		"required": []string{"command"},
	}
}

func (t *RunCommandTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	command, _ := args["command"].(string)
	cwd, _ := args["cwd"].(string)
	background, _ := args["background"].(bool)
	timeoutMs := 30000.0

	if v, ok := args["timeout_ms"].(float64); ok && v > 0 {
		timeoutMs = v
	}

	if command == "" {
		return dtool.ToolResult{Output: "Error: 'command' is required"}, nil
	}

	if background {
		id := uuid.New().String()[:8]
		if err := t.sandbox.RunBackground(ctx, id, command, cwd); err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error starting background command: %v", err)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Background command started. ID: %s\nUse command_status to check output.", id)}, nil
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	result, err := t.sandbox.Run(ctx, command, cwd, timeout)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	var output string
	if result.Stdout != "" {
		output = result.Stdout
	}
	if result.Stderr != "" {
		if output != "" {
			output += "\n--- stderr ---\n"
		}
		output += result.Stderr
	}
	if output == "" {
		output = "(no output)"
	}

	if result.TimedOut {
		output += fmt.Sprintf("\n\n[Command timed out after %dms]", int(timeoutMs))
	}
	if result.ExitCode != 0 {
		output += fmt.Sprintf("\n\n[Exit code: %d]", result.ExitCode)
	}
	return dtool.ToolResult{Output: output}, nil
}
