package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ScriptTool bridges an executable skill into the tool registry.
// It wraps skill scripts (bash/python) as agent-callable tools.
type ScriptTool struct {
	skill *entity.Skill
}

// NewScriptTool creates a tool from a skill.
func NewScriptTool(skill *entity.Skill) *ScriptTool {
	return &ScriptTool{skill: skill}
}

func (t *ScriptTool) Name() string {
	return "skill_" + t.skill.Name
}

func (t *ScriptTool) Description() string {
	return fmt.Sprintf("Execute skill: %s — %s", t.skill.Name, t.skill.Description)
}

func (t *ScriptTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{
				"type":        "string",
				"description": "Arguments to pass to the skill script",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Optional stdin input",
			},
		},
	}
}

func (t *ScriptTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	scriptArgs, _ := args["args"].(string)
	input, _ := args["input"].(string)

	// Detect script runner based on file extension
	scriptPath := t.skill.Path + "/run.sh"
	runner := "bash"

	// Check for Python script (os.Stat checks file existence; LookPath only searches $PATH)
	if _, err := os.Stat(scriptPath); err != nil {
		pyPath := t.skill.Path + "/run.py"
		if _, err := os.Stat(pyPath); err == nil {
			scriptPath = pyPath
			runner = "python3"
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(execCtx, runner, scriptPath)
	if scriptArgs != "" {
		cmd = exec.CommandContext(execCtx, runner, scriptPath, scriptArgs)
	}
	cmd.Dir = t.skill.Path

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Skill %s failed: %v\nOutput: %s", t.skill.Name, err, string(output))}, nil
	}

	return dtool.ToolResult{Output: string(output)}, nil
}
