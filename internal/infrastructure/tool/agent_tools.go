package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// SpawnFunc creates an independent agent loop and runs a task, returning the result.
type SpawnFunc func(ctx context.Context, task string) (string, error)

// SpawnAgentTool creates a sub-agent for independent task execution.
type SpawnAgentTool struct {
	spawnFn SpawnFunc
}

// NewSpawnAgentTool creates a spawn tool with a factory function.
func NewSpawnAgentTool(fn SpawnFunc) *SpawnAgentTool {
	return &SpawnAgentTool{spawnFn: fn}
}

// SetSpawnFunc sets the factory function (used for lazy init from builder).
func (t *SpawnAgentTool) SetSpawnFunc(fn SpawnFunc) {
	t.spawnFn = fn
}

func (t *SpawnAgentTool) Name() string        { return "spawn_agent" }
func (t *SpawnAgentTool) Description() string { return prompttext.ToolSpawnAgent }

func (t *SpawnAgentTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "Detailed task description for the sub-agent"},
			"context": map[string]any{"type": "string", "description": "Additional context to pass"},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnAgentTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	task, _ := args["task"].(string)
	if task == "" {
		return dtool.ToolResult{Output: "Error: 'task' is required"}, nil
	}

	extraCtx, _ := args["context"].(string)
	if extraCtx != "" {
		task = task + "\n\nContext:\n" + extraCtx
	}

	if t.spawnFn == nil {
		return dtool.ToolResult{Output: "Error: spawn agent not available (factory not configured)"}, nil
	}

	// 5 minute timeout for sub-agent
	subCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := t.spawnFn(subCtx, task)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Sub-agent failed: %v", err)}, nil
	}

	return dtool.ToolResult{Output: fmt.Sprintf("[Sub-agent completed]\n%s", result)}, nil
}

// ForgeTool constructs, executes, and validates structured task environments.
type ForgeTool struct {
	sandboxRoot string
}

func NewForgeTool(sandboxRoot string) *ForgeTool {
	return &ForgeTool{sandboxRoot: sandboxRoot}
}

func (t *ForgeTool) Name() string        { return "forge" }
func (t *ForgeTool) Description() string { return prompttext.ToolForge }

func (t *ForgeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"setup", "assert", "diagnose", "cleanup"},
				"description": "Forge action",
			},
			"forge_id": map[string]any{"type": "string", "description": "Forge ID (from setup)"},
			"files": map[string]any{
				"type":        "object",
				"description": "Files to create (action=setup): path→content mapping",
			},
			"commands": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Setup commands to run (action=setup)",
			},
			"file_exists": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Paths that must exist (action=assert)",
			},
			"file_contains": map[string]any{
				"type":        "object",
				"description": "Path→substring checks (action=assert)",
			},
			"shell_check": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Commands that must exit 0 (action=assert)",
			},
			"failure": map[string]any{
				"type":        "string",
				"description": "Failure description (action=diagnose)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ForgeTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)

	switch action {
	case "setup":
		return t.doSetup(ctx, args)
	case "assert":
		return t.doAssert(ctx, args)
	case "diagnose":
		return t.doDiagnose(ctx, args)
	case "cleanup":
		return t.doCleanup(ctx, args)
	default:
		return dtool.ToolResult{Output: "Error: action must be one of: setup, assert, diagnose, cleanup"}, nil
	}
}

func (t *ForgeTool) doSetup(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	forgeID := uuid.New().String()[:8]
	sandboxPath := filepath.Join(t.sandboxRoot, forgeID)

	if err := os.MkdirAll(sandboxPath, 0755); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating sandbox: %v", err)}, nil
	}

	// Create files
	if files, ok := args["files"].(map[string]any); ok {
		for path, content := range files {
			fullPath := filepath.Join(sandboxPath, path)
			os.MkdirAll(filepath.Dir(fullPath), 0755)
			contentStr, _ := content.(string)
			os.WriteFile(fullPath, []byte(contentStr), 0644)
		}
	}

	// Run setup commands
	if cmds, ok := args["commands"].([]any); ok {
		for _, cmd := range cmds {
			cmdStr, _ := cmd.(string)
			if cmdStr == "" {
				continue
			}
			execCmd := exec.CommandContext(ctx, "bash", "-lc", cmdStr)
			execCmd.Dir = sandboxPath
			if output, err := execCmd.CombinedOutput(); err != nil {
				return dtool.ToolResult{Output: fmt.Sprintf("Setup command failed: %s\nOutput: %s\nError: %v", cmdStr, string(output), err)}, nil
			}
		}
	}

	return dtool.ToolResult{Output: fmt.Sprintf(`{"forge_id": "%s", "sandbox_path": "%s"}`, forgeID, sandboxPath)}, nil
}

func (t *ForgeTool) doAssert(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	forgeID, _ := args["forge_id"].(string)
	if forgeID == "" {
		return dtool.ToolResult{Output: "Error: 'forge_id' is required"}, nil
	}
	sandboxPath := filepath.Join(t.sandboxRoot, forgeID)

	total, passed, failed := 0, 0, 0
	var details []string

	// File existence checks
	if paths, ok := args["file_exists"].([]any); ok {
		for _, p := range paths {
			path, _ := p.(string)
			total++
			fullPath := filepath.Join(sandboxPath, path)
			if _, err := os.Stat(fullPath); err == nil {
				passed++
				details = append(details, fmt.Sprintf("✅ exists: %s", path))
			} else {
				failed++
				details = append(details, fmt.Sprintf("❌ missing: %s", path))
			}
		}
	}

	// File content checks
	if contains, ok := args["file_contains"].(map[string]any); ok {
		for path, substr := range contains {
			total++
			fullPath := filepath.Join(sandboxPath, path)
			data, err := os.ReadFile(fullPath)
			subStr, _ := substr.(string)
			if err == nil && strings.Contains(string(data), subStr) {
				passed++
				details = append(details, fmt.Sprintf("✅ contains: %s → '%s'", path, subStr))
			} else {
				failed++
				details = append(details, fmt.Sprintf("❌ missing content: %s → '%s'", path, subStr))
			}
		}
	}

	// Shell command checks
	if cmds, ok := args["shell_check"].([]any); ok {
		for _, cmd := range cmds {
			cmdStr, _ := cmd.(string)
			total++
			execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			execCmd := exec.CommandContext(execCtx, "bash", "-lc", cmdStr)
			execCmd.Dir = sandboxPath
			if err := execCmd.Run(); err == nil {
				passed++
				details = append(details, fmt.Sprintf("✅ cmd ok: %s", cmdStr))
			} else {
				failed++
				details = append(details, fmt.Sprintf("❌ cmd failed: %s", cmdStr))
			}
			cancel()
		}
	}

	result := fmt.Sprintf(`{"total": %d, "passed": %d, "failed": %d, "details": [`, total, passed, failed)
	for i, d := range details {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf(`"%s"`, strings.ReplaceAll(d, `"`, `\"`))
	}
	result += "]}"
	return dtool.ToolResult{Output: result}, nil
}

func (t *ForgeTool) doDiagnose(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	failure, _ := args["failure"].(string)
	if failure == "" {
		return dtool.ToolResult{Output: "Error: 'failure' description is required"}, nil
	}

	// Simple heuristic diagnosis
	category := "code_bug"
	autoFixable := true
	suggestion := "Review and fix the code"

	lowerFailure := strings.ToLower(failure)
	switch {
	case strings.Contains(lowerFailure, "not found") || strings.Contains(lowerFailure, "no such file"):
		category = "missing_dep"
		suggestion = "Install the missing dependency"
	case strings.Contains(lowerFailure, "permission denied") || strings.Contains(lowerFailure, "sudo"):
		category = "env_issue"
		autoFixable = false
		suggestion = "Requires elevated permissions — ask user"
	case strings.Contains(lowerFailure, "api key") || strings.Contains(lowerFailure, "token") || strings.Contains(lowerFailure, "cookie"):
		category = "unresolvable"
		autoFixable = false
		suggestion = "Requires user-provided credentials"
	}

	return dtool.ToolResult{Output: fmt.Sprintf(`{"category": "%s", "auto_fixable": %t, "suggestion": "%s"}`, category, autoFixable, suggestion)}, nil
}

func (t *ForgeTool) doCleanup(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	forgeID, _ := args["forge_id"].(string)
	if forgeID == "" {
		return dtool.ToolResult{Output: "Error: 'forge_id' is required"}, nil
	}
	sandboxPath := filepath.Join(t.sandboxRoot, forgeID)
	if err := os.RemoveAll(sandboxPath); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error cleaning up: %v", err)}, nil
	}
	return dtool.ToolResult{Output: "OK"}, nil
}
