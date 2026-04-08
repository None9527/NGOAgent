package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
)

// SpawnFunc creates an independent agent loop and runs a task, returning the result.
// agentType selects the AgentDefinition from the registry ("general", "researcher", etc.)
type SpawnFunc func(ctx context.Context, task, taskName, agentType string) (string, error)

// SpawnAgentTool creates a sub-agent for independent task execution.
type SpawnAgentTool struct {
	spawnFn       SpawnFunc
	EventPusher   func(sessionID, eventType string, data any) // wire by server for SSE progress push
	ToolCtx       prompttext.ToolContext                      // Runtime environment context for dynamic description
	ScratchDir    string                                      // Session-scoped scratchpad directory (Sprint 3-1)
	AgentTypeEnum []string                                    // Available agent types from registry (for schema enum)
}

// NewSpawnAgentTool creates a spawn tool with a factory function.
func NewSpawnAgentTool(fn SpawnFunc) *SpawnAgentTool {
	return &SpawnAgentTool{spawnFn: fn}
}

// SetSpawnFunc sets the factory function (used for lazy init from builder).
func (t *SpawnAgentTool) SetSpawnFunc(fn SpawnFunc) {
	t.spawnFn = fn
}

// GetSpawnFunc returns the factory function for sharing with other tools (e.g. SkillTool).
func (t *SpawnAgentTool) GetSpawnFunc() SpawnFunc {
	return t.spawnFn
}

// SetEventPusher wires the event-push function so progress events reach the parent SSE stream.
func (t *SpawnAgentTool) SetEventPusher(fn func(sessionID, eventType string, data any)) {
	t.EventPusher = fn
}

// SetAgentTypes populates the agent_type enum from the registry.
func (t *SpawnAgentTool) SetAgentTypes(types []string) {
	t.AgentTypeEnum = types
}

func (t *SpawnAgentTool) Name() string        { return "spawn_agent" }
func (t *SpawnAgentTool) Description() string { return prompttext.ToolSpawnAgentDynamic(t.ToolCtx) }

func (t *SpawnAgentTool) Schema() map[string]any {
	// Build agent_type property with enum if available
	agentTypeProp := map[string]any{
		"type":        "string",
		"description": "Type of agent to spawn. Each type has its own tool set, context level, and timeout.",
	}
	if len(t.AgentTypeEnum) > 0 {
		enumSlice := make([]any, len(t.AgentTypeEnum))
		for i, v := range t.AgentTypeEnum {
			enumSlice[i] = v
		}
		agentTypeProp["enum"] = enumSlice
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":       map[string]any{"type": "string", "description": "Detailed task description for the sub-agent"},
			"task_name":  map[string]any{"type": "string", "description": "Short human-readable name (e.g. 'Fix Auth Tests')"},
			"agent_type": agentTypeProp,
			"context":    map[string]any{"type": "string", "description": "Additional context to pass"},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnAgentTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	task, _ := args["task"].(string)
	if task == "" {
		return dtool.ToolResult{Output: "Error: 'task' is required"}, nil
	}

	taskName, _ := args["task_name"].(string)
	if taskName == "" {
		taskName = "sub-agent"
	}

	agentType, _ := args["agent_type"].(string)
	if agentType == "" {
		agentType = "general" // default
	}

	extraCtx, _ := args["context"].(string)
	if extraCtx != "" {
		task = task + "\n\nContext:\n" + extraCtx
	}

	// Auto-inject workspace directory
	if cwd, err := os.Getwd(); err == nil {
		task = fmt.Sprintf("Working directory: %s\n\n", cwd) + task
	}

	// Auto-inject scratchpad path for cross-worker knowledge sharing
	if t.ScratchDir != "" {
		task = fmt.Sprintf("Scratchpad directory: %s (read/write intermediate results here for other workers)\n\n", t.ScratchDir) + task
		_ = os.MkdirAll(t.ScratchDir, 0755)
	}

	if t.spawnFn == nil {
		return dtool.ToolResult{Output: "Error: spawn agent not available (factory not configured)"}, nil
	}

	fmt.Printf("[spawn] Starting sub-agent: %s (type=%s)\n", taskName, agentType)

	// Non-blocking: RunAsync returns runID immediately
	runID, err := t.spawnFn(ctx, task, taskName, agentType)
	if err != nil {
		fmt.Printf("[spawn] Sub-agent '%s' failed to start: %v\n", taskName, err)
		return dtool.ToolResult{Output: fmt.Sprintf("Sub-agent '%s' failed to start: %v", taskName, err)}, nil
	}

	fmt.Printf("[spawn] Sub-agent '%s' (type=%s) spawned → %s\n", taskName, agentType, runID)
	return dtool.SpawnYieldResult(fmt.Sprintf(
		"[Sub-agent '%s' (type=%s) spawned → %s]\n"+
			"⏸ Async task running. Parent loop will pause automatically.\n"+
			"Results arrive via auto-wake when ALL sub-agents complete.\n"+
			"DO NOT poll this ID with command_status.",
		taskName, agentType, runID))
}

// EvoTool constructs, executes, and validates structured task environments.
type EvoTool struct {
	sandboxRoot string
	sandbox     *sandbox.Manager // P0-6: route commands through security pipeline
}

func NewEvoTool(sandboxRoot string, sb *sandbox.Manager) *EvoTool {
	return &EvoTool{sandboxRoot: sandboxRoot, sandbox: sb}
}

func (t *EvoTool) Name() string        { return "evo" }
func (t *EvoTool) Description() string { return prompttext.ToolEvo }

func (t *EvoTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"setup", "assert", "diagnose", "cleanup"},
				"description": "Evo action",
			},
			"evo_id": map[string]any{"type": "string", "description": "Evo ID (from setup)"},
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

func (t *EvoTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
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

func (t *EvoTool) doSetup(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	evoID := uuid.New().String()[:8]
	sandboxPath := filepath.Join(t.sandboxRoot, evoID)

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

	// Run setup commands via sandbox (P0-6: security pipeline)
	if cmds, ok := args["commands"].([]any); ok {
		for _, cmd := range cmds {
			cmdStr, _ := cmd.(string)
			if cmdStr == "" {
				continue
			}
			result, err := t.sandbox.Run(ctx, cmdStr, sandboxPath, 60*time.Second)
			if err != nil {
				return dtool.ToolResult{Output: fmt.Sprintf("Setup command failed: %s\nError: %v", cmdStr, err)}, nil
			}
			if result.ExitCode != 0 {
				output := result.Stdout
				if result.Stderr != "" {
					output += "\n" + result.Stderr
				}
				return dtool.ToolResult{Output: fmt.Sprintf("Setup command failed: %s\nOutput: %s\nExit code: %d", cmdStr, output, result.ExitCode)}, nil
			}
		}
	}

	return dtool.ToolResult{Output: fmt.Sprintf(`{"evo_id": "%s", "sandbox_path": "%s"}`, evoID, sandboxPath)}, nil
}

func (t *EvoTool) doAssert(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	evoID, _ := args["evo_id"].(string)
	if evoID == "" {
		return dtool.ToolResult{Output: "Error: 'evo_id' is required"}, nil
	}
	sandboxPath := filepath.Join(t.sandboxRoot, evoID)

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

	// Shell command checks (P0-6: via sandbox)
	if cmds, ok := args["shell_check"].([]any); ok {
		for _, cmd := range cmds {
			cmdStr, _ := cmd.(string)
			total++
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			result, err := t.sandbox.Run(checkCtx, cmdStr, sandboxPath, 30*time.Second)
			cancel()
			if err == nil && result.ExitCode == 0 {
				passed++
				details = append(details, fmt.Sprintf("✅ cmd ok: %s", cmdStr))
			} else {
				failed++
				details = append(details, fmt.Sprintf("❌ cmd failed: %s", cmdStr))
			}
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

func (t *EvoTool) doDiagnose(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
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

func (t *EvoTool) doCleanup(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	evoID, _ := args["evo_id"].(string)
	if evoID == "" {
		return dtool.ToolResult{Output: "Error: 'evo_id' is required"}, nil
	}
	sandboxPath := filepath.Join(t.sandboxRoot, evoID)
	if err := os.RemoveAll(sandboxPath); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error cleaning up: %v", err)}, nil
	}
	return dtool.ToolResult{Output: "OK"}, nil
}
