// Package skill — workflow.go provides the WorkflowRunner for pipeline-type skills.
//
// A pipeline skill has a workflow.yaml that defines a sequence of steps.
// The runner executes steps sequentially with code-enforced ordering,
// hard gates (required steps), retry limits, and variable passing.
//
// This solves the problem where LLM-driven skills "work around" hard
// constraints (e.g. skipping mandatory reference images).
package skill

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"gopkg.in/yaml.v3"
)

// WorkflowDef is the parsed workflow.yaml structure.
//
// Mode controls how the agent interacts with this workflow:
//   - "pipeline" (default): WorkflowRunner takes over the agent loop, executes
//     steps in strict code-enforced order. LLM cannot skip steps.
//   - "agent": Workflow is injected as a structured hint only. The LLM agent
//     runs freely and uses the steps as a reference plan, enabling dynamic
//     decision-making, adaptive searching, and iterative execution.
type WorkflowDef struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Trigger     string         `yaml:"trigger"`
	Mode        string         `yaml:"mode"` // "pipeline" (default) | "agent"
	Steps       []WorkflowStep `yaml:"steps"`
}

// IsAgentMode returns true when the workflow delegates execution to the LLM
// agent loop (hint injection) rather than the code-enforced WorkflowRunner.
func (d *WorkflowDef) IsAgentMode() bool {
	return strings.EqualFold(d.Mode, "agent")
}

// WorkflowStep defines a single step in the workflow.
type WorkflowStep struct {
	ID       string         `yaml:"id"`
	Mode     string         `yaml:"mode"`     // "tool" | "llm" | "command"
	Tool     string         `yaml:"tool"`     // tool name (when mode=tool)
	Args     map[string]any `yaml:"args"`     // tool arguments (supports {{var}} templates)
	Prompt   string         `yaml:"prompt"`   // LLM prompt (when mode=llm)
	Command  string         `yaml:"command"`  // shell command (when mode=command)
	Required bool           `yaml:"required"` // hard gate: failure = abort entire workflow
	Retry    int            `yaml:"retry"`    // max retry count (0 = no retry)
	SaveAs   string         `yaml:"save_as"`  // variable name to store result
}

// StepResult holds the outcome of a single step execution.
type StepResult struct {
	StepID  string
	Output  string
	Success bool
	Error   string
}

// WorkflowResult holds the final outcome of the workflow.
type WorkflowResult struct {
	Status  string            // "completed" | "failed"
	FailAt  string            // step ID that failed (if any)
	Reason  string            // failure reason
	Steps   []StepResult      // results of each step
	Context map[string]string // accumulated variable context
}

// WorkflowRunner executes pipeline-type skills.
type WorkflowRunner struct {
	toolExec ToolExecutorFunc
	llmCall  LLMCallFunc
}

// ToolExecutorFunc executes a tool by name and returns the output.
type ToolExecutorFunc func(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error)

// LLMCallFunc performs a single LLM completion (non-streaming, no tools).
type LLMCallFunc func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// NewWorkflowRunner creates a runner with injected tool and LLM dependencies.
func NewWorkflowRunner(toolExec ToolExecutorFunc, llmCall LLMCallFunc) *WorkflowRunner {
	return &WorkflowRunner{toolExec: toolExec, llmCall: llmCall}
}

// LoadWorkflow parses a workflow.yaml from a skill directory.
func LoadWorkflow(skillDir string) (*WorkflowDef, error) {
	path := filepath.Join(skillDir, "workflow.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	var def WorkflowDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	// Default step IDs from index
	for i := range def.Steps {
		if def.Steps[i].ID == "" {
			def.Steps[i].ID = fmt.Sprintf("step_%d", i+1)
		}
		if def.Steps[i].SaveAs == "" {
			def.Steps[i].SaveAs = def.Steps[i].ID
		}
	}
	return &def, nil
}

// HasWorkflow checks if a skill directory contains a workflow.yaml.
func HasWorkflow(skillDir string) bool {
	_, err := os.Stat(filepath.Join(skillDir, "workflow.yaml"))
	return err == nil
}

// AgentHintFromWorkflow builds an ephemeral hint for agent-mode workflows.
// Instead of taking over execution, this hint is injected into the LLM context
// so the agent understands the recommended steps while retaining full autonomy
// to adapt, re-order, retry, or add extra searches as needed.
func AgentHintFromWorkflow(name, skillPath string, def *WorkflowDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🧭 Skill: %s [agent mode — you control execution]\n", name))
	b.WriteString(fmt.Sprintf("Full guide: skill(name=\"%s\")\n", name))
	if def.Description != "" {
		b.WriteString(fmt.Sprintf("Description: %s\n", def.Description))
	}
	b.WriteString("\nRecommended steps (adapt freely — you are NOT bound to this order):\n")
	for i, step := range def.Steps {
		req := ""
		if step.Required {
			req = " [required]"
		}
		desc := step.ID
		switch step.Mode {
		case "tool":
			desc = fmt.Sprintf("use %s", step.Tool)
		case "llm":
			// Truncate prompt to first line as summary
			if lines := strings.SplitN(strings.TrimSpace(step.Prompt), "\n", 2); len(lines) > 0 {
				desc = strings.TrimSpace(lines[0])
				if len(desc) > 80 {
					desc = desc[:80] + "…"
				}
			}
		case "command":
			desc = "run shell command"
		}
		b.WriteString(fmt.Sprintf("  %d. [%s] %s%s\n", i+1, step.ID, desc, req))
	}
	b.WriteString("\nYou may search multiple times, skip optional steps, or add extra steps as needed.")
	return b.String()
}

// Run executes the workflow with the given input variables.
func (r *WorkflowRunner) Run(ctx context.Context, def *WorkflowDef, input map[string]string) WorkflowResult {
	vars := make(map[string]string)
	for k, v := range input {
		vars[k] = v
	}

	result := WorkflowResult{
		Status:  "completed",
		Context: vars,
	}

	total := len(def.Steps)
	for i, step := range def.Steps {
		slog.Info(fmt.Sprintf("[workflow] %s: step %d/%d [%s] mode=%s required=%v",
			def.Name, i+1, total, step.ID, step.Mode, step.Required))

		stepResult := r.execStep(ctx, step, vars)
		result.Steps = append(result.Steps, stepResult)

		if stepResult.Success {
			// Save result for next steps
			vars[step.SaveAs] = stepResult.Output
			slog.Info(fmt.Sprintf("[workflow] %s: step [%s] succeeded (%d chars)",
				def.Name, step.ID, len(stepResult.Output)))
			continue
		}

		// Step failed
		if step.Required {
			result.Status = "failed"
			result.FailAt = step.ID
			result.Reason = fmt.Sprintf("必需步骤 [%s] (%d/%d) 失败: %s", step.ID, i+1, total, stepResult.Error)
			slog.Info(fmt.Sprintf("[workflow] %s: ABORT at required step [%s]: %s",
				def.Name, step.ID, stepResult.Error))
			return result
		}

		// Optional step failed — log and continue
		slog.Info(fmt.Sprintf("[workflow] %s: optional step [%s] failed, continuing: %s",
			def.Name, step.ID, stepResult.Error))
		vars[step.SaveAs] = "" // empty for downstream
	}

	return result
}

// execStep executes a single step with retry logic.
func (r *WorkflowRunner) execStep(ctx context.Context, step WorkflowStep, vars map[string]string) StepResult {
	maxAttempts := 1 + step.Retry
	var lastErr string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			slog.Info(fmt.Sprintf("[workflow] step [%s] retry %d/%d", step.ID, attempt-1, step.Retry))
		}

		output, err := r.execOnce(ctx, step, vars, attempt)
		if err == nil && output != "" {
			return StepResult{StepID: step.ID, Output: output, Success: true}
		}

		if err != nil {
			lastErr = err.Error()
		} else {
			lastErr = "empty output"
		}
	}

	return StepResult{StepID: step.ID, Success: false, Error: lastErr}
}

// execOnce performs a single execution attempt for a step.
func (r *WorkflowRunner) execOnce(ctx context.Context, step WorkflowStep, vars map[string]string, _ int) (string, error) {
	switch step.Mode {
	case "tool":
		return r.execTool(ctx, step, vars)
	case "llm":
		return r.execLLM(ctx, step, vars)
	case "command":
		return r.execCommand(ctx, step, vars)
	default:
		// Auto-detect: has Tool → tool mode, has Prompt → llm mode, has Command → command mode
		if step.Tool != "" {
			return r.execTool(ctx, step, vars)
		}
		if step.Prompt != "" {
			return r.execLLM(ctx, step, vars)
		}
		if step.Command != "" {
			return r.execCommand(ctx, step, vars)
		}
		return "", fmt.Errorf("step [%s] has no mode, tool, prompt, or command", step.ID)
	}
}

func (r *WorkflowRunner) execTool(ctx context.Context, step WorkflowStep, vars map[string]string) (string, error) {
	// Resolve template variables in args
	resolvedArgs := make(map[string]any)
	for k, v := range step.Args {
		if s, ok := v.(string); ok {
			resolvedArgs[k] = resolveTemplate(s, vars)
		} else {
			resolvedArgs[k] = v
		}
	}

	result, err := r.toolExec(ctx, step.Tool, resolvedArgs)
	if err != nil {
		return "", fmt.Errorf("tool %s: %w", step.Tool, err)
	}
	if strings.HasPrefix(result.Output, "Error:") {
		return "", fmt.Errorf("tool %s returned error: %s", step.Tool, result.Output)
	}
	return result.Output, nil
}

func (r *WorkflowRunner) execLLM(ctx context.Context, step WorkflowStep, vars map[string]string) (string, error) {
	prompt := resolveTemplate(step.Prompt, vars)

	systemPrompt := "You are a workflow step executor. Complete the task precisely. Output ONLY the requested result, nothing else."
	output, err := r.llmCall(ctx, systemPrompt, prompt)
	if err != nil {
		return "", fmt.Errorf("llm step: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func (r *WorkflowRunner) execCommand(ctx context.Context, step WorkflowStep, vars map[string]string) (string, error) {
	cmd := resolveTemplate(step.Command, vars)

	// Use run_command tool for sandboxed execution
	result, err := r.toolExec(ctx, "run_command", map[string]any{
		"command": cmd,
	})
	if err != nil {
		return "", fmt.Errorf("command: %w", err)
	}
	return result.Output, nil
}

// resolveTemplate replaces {{var_name}} with values from the context map.
var templatePattern = regexp.MustCompile(`\{\{(\w+(?:\.\w+)?)\}\}`)

func resolveTemplate(template string, vars map[string]string) string {
	return templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		key := match[2 : len(match)-2] // strip {{ and }}
		if val, ok := vars[key]; ok {
			return val
		}
		return match // leave unresolved
	})
}

// FormatResult produces a human-readable summary of the workflow result.
func (wr WorkflowResult) FormatResult() string {
	var b strings.Builder
	if wr.Status == "completed" {
		b.WriteString("✅ Workflow 完成\n\n")
	} else {
		b.WriteString(fmt.Sprintf("❌ Workflow 失败: %s\n\n", wr.Reason))
	}

	b.WriteString("### 执行步骤\n")
	for i, step := range wr.Steps {
		status := "✅"
		if !step.Success {
			status = "❌"
		}
		b.WriteString(fmt.Sprintf("%d. %s [%s]", i+1, status, step.StepID))
		if !step.Success && step.Error != "" {
			b.WriteString(fmt.Sprintf(" — %s", step.Error))
		}
		b.WriteString("\n")
	}

	// Include last successful output if completed
	if wr.Status == "completed" && len(wr.Steps) > 0 {
		lastStep := wr.Steps[len(wr.Steps)-1]
		if lastStep.Output != "" && len(lastStep.Output) < 500 {
			b.WriteString(fmt.Sprintf("\n### 最终输出\n%s\n", lastStep.Output))
		}
	}

	return b.String()
}

// QuickLLMCall creates an LLMCallFunc from an LLM provider.
// This is a convenience factory for building the WorkflowRunner from a Router.
func QuickLLMCall(router interface {
	ResolveWithFallback(model string) (llm.Provider, string, error)
	CurrentModel() string
}) LLMCallFunc {
	return func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
		provider, model, err := router.ResolveWithFallback("")
		if err != nil {
			return "", fmt.Errorf("resolve LLM: %w", err)
		}

		req := &llm.Request{
			Model: model,
			Messages: []llm.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
			Temperature: 0.3, // Low temperature for deterministic workflow steps
			Stream:      false,
		}

		ch := make(chan llm.StreamChunk, 64)
		resp, err := provider.GenerateStream(ctx, req, ch)
		// Drain channel
		for range ch {
		}
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", fmt.Errorf("nil response from LLM")
		}
		return resp.Content, nil
	}
}
