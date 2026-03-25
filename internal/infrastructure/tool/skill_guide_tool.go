package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// SkillManager is the interface needed by UseSkillTool for skill lookup.
type SkillManager interface {
	Get(name string) (*entity.Skill, bool)
	List() []*entity.Skill
}

// UseSkillTool is a unified tool for activating any skill.
// Instead of registering 46 individual tools, this single tool
// lets the LLM load any skill's SKILL.md by name.
// Also serves as fallback when LLM calls a skill name directly.
type UseSkillTool struct {
	skillMgr SkillManager
}

// NewUseSkillTool creates the unified skill activation tool.
func NewUseSkillTool(mgr SkillManager) *UseSkillTool {
	return &UseSkillTool{skillMgr: mgr}
}

func (t *UseSkillTool) Name() string {
	return "use_skill"
}

func (t *UseSkillTool) Description() string {
	return "Activate a skill by name. Returns the skill's full guide (SKILL.md) " +
		"so you can follow its instructions. Use this instead of manually reading SKILL.md files."
}

func (t *UseSkillTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the skill to activate (e.g., 'brainstorming', 'frontend-design')",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "Optional: the topic or context for this skill",
			},
		},
		"required": []string{"name"},
	}
}

func (t *UseSkillTool) IsReadOnly() bool   { return true }
func (t *UseSkillTool) IsDestructive() bool { return false }

func (t *UseSkillTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	name, _ := args["name"].(string)
	topic, _ := args["topic"].(string)

	if name == "" {
		// List available skills
		skills := t.skillMgr.List()
		var b strings.Builder
		b.WriteString("Available skills:\n")
		for _, s := range skills {
			b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
		}
		return dtool.ToolResult{Output: b.String()}, nil
	}

	return t.LoadSkill(ctx, name, topic)
}

// LoadSkill reads and returns a skill's SKILL.md content.
// Exported so the registry fallback can call it directly.
func (t *UseSkillTool) LoadSkill(_ context.Context, name, topic string) (dtool.ToolResult, error) {
	skill, ok := t.skillMgr.Get(name)
	if !ok {
		// Try fuzzy match
		skills := t.skillMgr.List()
		var candidates []string
		for _, s := range skills {
			if strings.Contains(strings.ToLower(s.Name), strings.ToLower(name)) {
				candidates = append(candidates, s.Name)
			}
		}
		if len(candidates) == 1 {
			skill, _ = t.skillMgr.Get(candidates[0])
		} else if len(candidates) > 1 {
			return dtool.ToolResult{Output: fmt.Sprintf("Multiple skills match '%s': %s", name, strings.Join(candidates, ", "))}, nil
		} else {
			return dtool.ToolResult{Output: fmt.Sprintf("Skill '%s' not found. Use use_skill() without name to list all.", name)}, nil
		}
	}

	skillFile := filepath.Join(skill.Path, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading skill %s: %v", name, err)}, nil
	}

	// Executable skills are registered as direct tools — redirect LLM
	if skill.Type == "executable" || skill.Type == "hybrid" {
		usage := skill.Command
		if usage == "" {
			usage = "generate \"prompt\""
		}
		redirect := fmt.Sprintf(
			"⚡ Skill '%s' is a direct tool. Call it directly instead of use_skill:\n\n"+
				"  Tool: %s\n  Args: %s\n\n"+
				"Example: %s(args='%s')\n\n"+
				"Do NOT use use_skill for this. Call the tool directly with args.",
			skill.Name, skill.Name, usage, skill.Name, usage,
		)
		// Also append a brief from SKILL.md so LLM knows available subcommands
		if topic != "" {
			redirect += fmt.Sprintf("\n\nTopic requested: %s", topic)
		}
		return dtool.ToolResult{Output: redirect}, nil
	}

	output := string(content)
	if topic != "" {
		output = fmt.Sprintf("## Activated Skill: %s\n## Topic: %s\n\n%s", skill.Name, topic, output)
	} else {
		output = fmt.Sprintf("## Activated Skill: %s\n\n%s", skill.Name, output)
	}

	return dtool.ToolResult{Output: output}, nil
}
