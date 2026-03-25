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
	return t.skill.Name
}

func (t *ScriptTool) Description() string {
	desc := fmt.Sprintf("Execute skill: %s — %s", t.skill.Name, t.skill.Description)
	if t.skill.Command != "" {
		desc += fmt.Sprintf("\nUsage example: run.sh %s", t.skill.Command)
	}
	return desc
}

func (t *ScriptTool) Schema() map[string]any {
	usageDesc := "CLI arguments (subcommand + flags), e.g.: generate \"prompt\""
	if t.skill.Command != "" {
		usageDesc = fmt.Sprintf("CLI arguments, e.g.: %s", t.skill.Command)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{
				"type":        "string",
				"description": usageDesc,
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

	// Detect script entry point
	scriptPath := t.skill.Path + "/run.sh"
	if _, err := os.Stat(scriptPath); err != nil {
		pyPath := t.skill.Path + "/run.py"
		if _, err := os.Stat(pyPath); err == nil {
			scriptPath = pyPath
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Use bash -c for proper shell argument splitting.
	// Without this, exec.Command passes the entire args string as $1,
	// breaking CLI subcommand parsing (e.g. "generate 'prompt'" → one arg).
	shellCmd := scriptPath
	if scriptArgs != "" {
		shellCmd = scriptPath + " " + scriptArgs
	}
	cmd := exec.CommandContext(execCtx, "bash", "-c", shellCmd)
	cmd.Dir = t.skill.Path

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// On failure, include usage hint so LLM can self-correct
		usageHint := ""
		if t.skill.Command != "" {
			usageHint = fmt.Sprintf("\n\nUsage reference: run.sh %s", t.skill.Command)
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Skill %s failed: %v\nOutput: %s%s",
			t.skill.Name, err, string(output), usageHint)}, nil
	}

	return dtool.ToolResult{Output: string(output)}, nil
}
