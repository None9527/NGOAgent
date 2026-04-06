package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
)

// RunCommandTool executes shell commands via the sandbox.
type RunCommandTool struct {
	sandbox *sandbox.Manager
}

func NewRunCommandTool(sb *sandbox.Manager) *RunCommandTool {
	return &RunCommandTool{sandbox: sb}
}

func (t *RunCommandTool) Name() string { return "run_command" }
func (t *RunCommandTool) Description() string {
	return `Execute a shell command. Set background=true for long-running processes (servers, builds).
- cwd: persists between calls automatically
- wait_ms_before_async: wait before auto-backgrounding (use 500 for slow cmds like npm install, go build)
- Output >50KB is truncated (head + tail)`
}

func (t *RunCommandTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string", "description": "Shell command to execute. Use && to chain commands, not ;", "minLength": 1},
			"cwd":        map[string]any{"type": "string", "description": "Working directory (absolute path). Defaults to persisted cwd."},
			"timeout_ms": map[string]any{"type": "integer", "description": "Timeout in ms. Default 30000. Use 120000 for builds.", "default": 30000},
			"background": map[string]any{"type": "boolean", "description": "Run in background, returns command_id. Default false.", "default": false},
			"detach":     map[string]any{"type": "boolean", "description": "Launch as detached service (survives agent restart). Use for persistent servers like uvicorn, npm run dev. No output tracking.", "default": false},
			"wait_ms_before_async": map[string]any{
				"type":        "integer",
				"description": "Wait this many ms for sync completion; if not done, auto-background and return command_id. Use 500 for potentially slow commands.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *RunCommandTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	command, _ := args["command"].(string)
	cwd, _ := args["cwd"].(string)
	background, _ := args["background"].(bool)
	detach, _ := args["detach"].(bool)
	timeoutMs := 30000.0
	waitMsBeforeAsync := 0.0

	if v, ok := args["timeout_ms"].(float64); ok && v > 0 {
		timeoutMs = v
	}
	if v, ok := args["wait_ms_before_async"].(float64); ok && v > 0 {
		waitMsBeforeAsync = v
	}

	if command == "" {
		return dtool.ToolResult{Output: "Error: 'command' is required"}, nil
	}

	// Detached mode: fully independent service, survives agent restart
	if detach {
		pid, err := t.sandbox.RunDetached(command, cwd)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error launching detached service: %v", err)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Service launched (detached).\nPID: %d\nNote: not tracked by agent. Use 'kill %d' to stop.", pid, pid)}, nil
	}

	// Explicit background mode
	if background {
		id := uuid.New().String()[:8]
		if err := t.sandbox.RunBackground(ctx, id, command, cwd); err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error starting background command: %v", err)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Background command started. ID: %s\nUse command_status to check output.", id)}, nil
	}

	// Hybrid sync/async mode: wait N ms, then auto-background if still running
	if waitMsBeforeAsync > 0 {
		return t.executeHybrid(ctx, command, cwd, waitMsBeforeAsync)
	}

	// Standard synchronous execution
	timeout := time.Duration(timeoutMs) * time.Millisecond
	result, err := t.sandbox.Run(ctx, command, cwd, timeout)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	return t.formatResult(result, timeoutMs), nil
}

// executeHybrid starts a command in the background, waits up to waitMs for it
// to complete. If done, returns result directly. Otherwise returns the command_id.
func (t *RunCommandTool) executeHybrid(ctx context.Context, command, cwd string, waitMs float64) (dtool.ToolResult, error) {
	id := uuid.New().String()[:8]
	if err := t.sandbox.RunBackground(ctx, id, command, cwd); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error starting command: %v", err)}, nil
	}

	// Wait for completion within the window
	waitDur := time.Duration(waitMs) * time.Millisecond
	result, err := t.sandbox.GetStatus(id, 0)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	// If already done (very fast command)
	if result.ExitCode >= 0 {
		return t.formatResult(result, waitMs), nil
	}

	// Wait the remaining time (context-aware, can be cancelled)
	select {
	case <-time.After(waitDur):
	case <-ctx.Done():
		return dtool.ToolResult{Output: fmt.Sprintf("Command cancelled. ID: %s\nUse command_status to check output.", id)}, nil
	}
	result, err = t.sandbox.GetStatus(id, 0)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	// Completed within window
	if result.ExitCode >= 0 {
		return t.formatResult(result, waitMs), nil
	}

	// Still running — report as backgrounded
	return dtool.ToolResult{
		Output: fmt.Sprintf("Command still running after %dms. Moved to background.\nCommand ID: %s\nUse command_status to check output.", int(waitMs), id),
	}, nil
}

func (t *RunCommandTool) formatResult(result *sandbox.Result, timeoutMs float64) dtool.ToolResult {
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
	return dtool.ToolResult{Output: output}
}
