package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

// SkillTool is the dedicated tool for invoking skills.
// Replaces the read_file side-effect path with an explicit tool call.
//
// Execution modes (auto-derived from skill metadata):
//   - inline:     returns skill content as tool_result, agent executes in-context
//   - fork:       auto-spawns sub-agent via SpawnFunc, skill content as task
//   - pipeline:   delegates to WorkflowRunner for code-enforced step execution
//   - executable: delegates to existing ScriptTool (registered separately)
type SkillTool struct {
	skillMgr *skill.Manager
	spawnFn  SpawnFunc             // reuses spawn_agent's SpawnFunc
	runner   *skill.WorkflowRunner // for pipeline execution
}

// NewSkillTool creates a skill tool backed by the skill manager.
func NewSkillTool(mgr *skill.Manager) *SkillTool {
	return &SkillTool{skillMgr: mgr}
}

// SetSpawnFunc wires the sub-agent factory (same as spawn_agent uses).
func (t *SkillTool) SetSpawnFunc(fn SpawnFunc) {
	t.spawnFn = fn
}

// SetWorkflowRunner wires the pipeline execution engine.
func (t *SkillTool) SetWorkflowRunner(r *skill.WorkflowRunner) {
	t.runner = r
}

func (t *SkillTool) Name() string { return "skill" }

func (t *SkillTool) Description() string {
	count := 0
	if t.skillMgr != nil {
		count = len(t.skillMgr.List())
	}
	return fmt.Sprintf("Execute a registered skill by name. %d skills available. "+
		"Use this tool instead of reading SKILL.md directly.", count)
}

func (t *SkillTool) Schema() map[string]any {
	// Build enum of available skill names for schema validation
	var names []string
	if t.skillMgr != nil {
		for _, s := range t.skillMgr.List() {
			names = append(names, s.Name)
		}
	}
	nameSchema := map[string]any{
		"type":        "string",
		"description": "Skill name (as shown in the skill listing)",
	}
	if len(names) > 0 {
		nameSchema["enum"] = names
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": nameSchema,
			"args": map[string]any{
				"type":        "string",
				"description": "Optional arguments or user request context for the skill",
			},
		},
		"required": []string{"name"},
	}
}

func (t *SkillTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	name := MustString(args, "name")
	skillArgs := MustString(args, "args")

	if name == "" {
		return dtool.ToolResult{Output: "Error: 'name' is required"}, nil
	}

	if t.skillMgr == nil {
		return dtool.ToolResult{Output: "Error: skill manager not available"}, nil
	}

	s, ok := t.skillMgr.Get(name)
	if !ok {
		// Fuzzy match: suggest closest name
		available := t.listNames()
		return dtool.ToolResult{Output: fmt.Sprintf(
			"Skill '%s' not found.\nAvailable skills: %s", name, available)}, nil
	}

	execMode := deriveExecMode(s)

	switch execMode {
	case "pipeline":
		return t.executePipeline(ctx, s, skillArgs)
	case "executable":
		return t.executeExecutable(s)
	case "fork":
		return t.executeFork(ctx, s, skillArgs)
	default: // "inline"
		return t.executeInline(s, skillArgs)
	}
}

// executeInline returns skill content as tool_result for in-context execution.
// Emits SignalSkillLoaded for compression protection — content survives compaction.
func (t *SkillTool) executeInline(s *entity.Skill, args string) (dtool.ToolResult, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<skill_execution name=\"%s\">\n", s.Name))
	sb.WriteString("EXECUTION PROTOCOL:\n")
	sb.WriteString("1. Follow steps STRICTLY IN ORDER.\n")
	sb.WriteString("2. Full tool access — use whatever is needed.\n")
	sb.WriteString("3. Complete ALL steps before reporting.\n")
	sb.WriteString("4. If a step fails, report failure — do NOT improvise.\n\n")
	if args != "" {
		sb.WriteString(fmt.Sprintf("User request: %s\n\n", args))
	}
	sb.WriteString(s.Content)
	sb.WriteString("\n</skill_execution>")
	output := sb.String()
	return dtool.SkillLoadedResult(output, s.Name, s.Path, s.Content)
}

// executeFork auto-spawns a sub-agent with skill content as task.
func (t *SkillTool) executeFork(ctx context.Context, s *entity.Skill, args string) (dtool.ToolResult, error) {
	if t.spawnFn == nil {
		// Fallback to inline if spawn not available
		return t.executeInline(s, args)
	}

	// Build task: skill content + user args
	var task strings.Builder
	if args != "" {
		task.WriteString(fmt.Sprintf("User request: %s\n\n", args))
	}
	task.WriteString("Skill instructions:\n")
	task.WriteString(s.Content)

	taskName := "skill:" + s.Name
	runID, err := t.spawnFn(ctx, task.String(), taskName)
	if err != nil {
		// Fallback to inline on spawn failure
		return t.executeInline(s, args)
	}

	return dtool.SpawnYieldResult(fmt.Sprintf(
		"[Skill '%s' forked to sub-agent → %s]\n"+
			"⏸ Executing in independent sub-agent. Results arrive via auto-wake.\n"+
			"DO NOT poll — barrier handles completion automatically.",
		s.Name, runID))
}

// executePipeline runs a workflow.yaml via WorkflowRunner.
func (t *SkillTool) executePipeline(ctx context.Context, s *entity.Skill, args string) (dtool.ToolResult, error) {
	def, err := skill.LoadWorkflow(s.Path)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Failed to load workflow: %v", err)}, nil
	}

	// Agent-mode workflows: return hint instead of running pipeline
	if def.IsAgentMode() {
		hint := skill.AgentHintFromWorkflow(s.Name, s.Path, def)
		return dtool.ToolResult{Output: hint}, nil
	}

	// Pipeline mode: code-enforced execution
	if t.runner == nil {
		// No runner wired — return content for manual execution
		return t.executeInline(s, args)
	}

	input := map[string]string{}
	if args != "" {
		input["args"] = args
	}
	result := t.runner.Run(ctx, def, input)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Pipeline '%s': %s\n", s.Name, result.Status))
	if result.FailAt != "" {
		sb.WriteString(fmt.Sprintf("Failed at: %s — %s\n", result.FailAt, result.Reason))
	}
	for _, step := range result.Steps {
		icon := "✅"
		if !step.Success {
			icon = "❌"
		}
		sb.WriteString(fmt.Sprintf("  %s %s: %s\n", icon, step.StepID, truncate(step.Output, 200)))
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

// executeExecutable tells the agent to use the dedicated ScriptTool.
func (t *SkillTool) executeExecutable(s *entity.Skill) (dtool.ToolResult, error) {
	return dtool.ToolResult{Output: fmt.Sprintf(
		"Skill '%s' is an executable script. Call it directly as a tool:\n"+
			"  %s(args=\"...\")\n"+
			"Description: %s",
		s.Name, s.Name, s.Description)}, nil
}

// deriveExecMode determines the execution mode from skill metadata.
func deriveExecMode(s *entity.Skill) string {
	if s.Type == "pipeline" {
		return "pipeline"
	}
	if s.Type == "executable" {
		return "executable"
	}
	if s.Context == "fork" {
		return "fork"
	}
	return "inline"
}

// listNames returns a comma-separated list of available skill names.
func (t *SkillTool) listNames() string {
	skills := t.skillMgr.List()
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
