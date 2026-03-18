// Package prompt assembles the 15-section system prompt with budget-based pruning.
package prompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
)

// Section represents one numbered section of the system prompt.
type Section struct {
	Order    int    // 1-15
	Name     string // e.g., "Identity"
	Content  string
	Priority int // 0=required, 1=high, 2=medium, 3=low (for pruning)
}

// SkillInfo is skill metadata provided by the skill manager.
type SkillInfo struct {
	Name        string
	Description string
	Type        string
	Content     string
	Command     string
	Path        string
}

// Deps contains all data needed to assemble the system prompt.
type Deps struct {
	UserRules      string
	ProjectContext string
	SkillInfos     []SkillInfo
	ConvSummary    string   // Semantically-retrieved KI summaries
	PreferenceKI   string   // Preference-tagged KI summaries (always injected)
	ToolDescs      []ToolDesc
	Ephemeral      []string
	FocusFile      string
	TokenBudget    int
	Mode           string // chat
	Runtime        string // Pre-built runtime info (OS/time/model/workspace)
}

// ToolDesc is a tool name + description pair for the tooling section.
type ToolDesc struct {
	Name        string
	Description string
}

// Engine assembles the system prompt from sections.
type Engine struct {
	homeDir string // ~/.ngoagent/
}

// NewEngine creates a prompt engine.
func NewEngine() *Engine {
	return &Engine{}
}

// NewEngineWithHome creates a prompt engine with a home directory for file discovery.
func NewEngineWithHome(homeDir string) *Engine {
	return &Engine{homeDir: homeDir}
}

// DiscoverUserRules loads user rules from global and project-level files.
func (e *Engine) DiscoverUserRules(workspaceDir string) (string, error) {
	if e.homeDir == "" {
		return "", nil
	}
	d := NewDiscovery(e.homeDir, workspaceDir)
	return d.LoadUserRules(), nil
}

// Assemble builds the complete system prompt.
// Returns the system prompt string and estimated token count.
func (e *Engine) Assemble(deps Deps) (string, int) {
	sections := e.buildSections(deps)
	return e.prune(sections, deps.TokenBudget)
}

// buildSections creates the first-principles ordered sections.
// Layout: Head(Identity→CoreBehavior→Safety→PreferenceKI→UserRules) → Mid(Tooling→Skills→ToolProtocol→ToolCalling→ProjectCtx) → Tail(ResponseFormat→SemanticKI→Runtime→Focus→Ephemeral)
// Mid follows natural dependency: WHAT I can do → HOW to work
func (e *Engine) buildSections(deps Deps) []Section {
	sections := []Section{
		// ═══ Head Peak: Identity + Behavior + Constraints (HIGH attention) ═══
		{Order: 1, Name: "Identity", Content: prompttext.Identity, Priority: 0},
		{Order: 2, Name: "CoreBehavior", Content: prompttext.CoreBehavior, Priority: 0},
		{Order: 3, Name: "OutputCapabilities", Content: prompttext.OutputCapabilities, Priority: 1},
		{Order: 4, Name: "Safety", Content: prompttext.Safety, Priority: 0},
		{Order: 5, Name: "PreferenceKI", Content: e.buildPreferenceKI(deps.PreferenceKI), Priority: 0},
		{Order: 6, Name: "UserRules", Content: e.buildUserRules(deps.UserRules), Priority: 1},
		// ═══ Mid Valley: Capability inventory FIRST, then protocol (natural dependency) ═══
		{Order: 7, Name: "Tooling", Content: e.buildTooling(deps.ToolDescs), Priority: 0},
		{Order: 8, Name: "Skills", Content: e.buildSkills(deps.SkillInfos), Priority: 1},
		{Order: 9, Name: "ToolProtocol", Content: prompttext.ToolProtocol, Priority: 0},
		{Order: 10, Name: "ToolCalling", Content: prompttext.ToolCalling, Priority: 0},
		{Order: 11, Name: "ProjectContext", Content: e.buildProjectContext(deps.ProjectContext), Priority: 2},
		{Order: 12, Name: "Variants", Content: "", Priority: 3},
		// ═══ Tail Peak: Output format + Task Knowledge + Live Context (HIGH attention) ═══
		{Order: 13, Name: "ResponseFormat", Content: prompttext.ResponseFormat, Priority: 0},
		{Order: 14, Name: "SemanticKI", Content: e.buildSemanticKI(deps.ConvSummary), Priority: 0},
		{Order: 15, Name: "Runtime", Content: e.buildRuntime(deps), Priority: 1},
		{Order: 16, Name: "Focus", Content: e.buildFocus(deps.FocusFile), Priority: 2},
		{Order: 17, Name: "Ephemeral", Content: e.buildEphemeral(deps.Ephemeral), Priority: 0},
	}
	return sections
}

func (e *Engine) buildRuntime(deps Deps) string {
	if deps.Runtime != "" {
		return deps.Runtime
	}
	return fmt.Sprintf("Mode: %s", deps.Mode)
}

func (e *Engine) buildTooling(descs []ToolDesc) string {
	if len(descs) == 0 {
		return ""
	}
	// Minimal summary — full descriptions are in function calling schema.
	// Only include critical usage notes that the schema can't express.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("You have %d tools available. Key usage notes:\n", len(descs)))
	b.WriteString("- Prefer purpose-built tools over run_command (edit_file > sed, grep_search > grep)\n")
	b.WriteString("- run_command: set background=true for long-running processes (servers, builds)\n")
	b.WriteString("- run_command: working directory PERSISTS between calls\n")
	b.WriteString("- task_plan: NEVER use write_file for plan.md/task.md/walkthrough.md\n")
	return b.String()
}

func (e *Engine) buildSkills(infos []SkillInfo) string {
	if len(infos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("You can use specialized 'skills' to help you with complex tasks.\n")
	b.WriteString("IMPORTANT: You MUST use `read_file` on a skill's SKILL.md BEFORE using it. ")
	b.WriteString("SKILL.md contains critical domain knowledge, execution commands, and rules. ")
	b.WriteString("NEVER guess commands or skip reading SKILL.md.\n\n")
	b.WriteString("Available skills:\n")
	for _, s := range infos {
		skillMd := s.Path + "/SKILL.md"
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", s.Name, skillMd, s.Description))
	}
	return b.String()
}

func (e *Engine) buildUserRules(rules string) string {
	if rules == "" {
		return ""
	}
	return "<user_rules>\n" + rules + "\n</user_rules>"
}

func (e *Engine) buildProjectContext(ctx string) string {
	if ctx == "" {
		return ""
	}
	return "<project_context>\n" + ctx + "\n</project_context>"
}

func (e *Engine) buildPreferenceKI(preferenceKI string) string {
	if preferenceKI == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<preference_knowledge>\n")
	b.WriteString("⚠️ CRITICAL: 以下是用户强制偏好，所有输出必须遵守。\n\n")
	b.WriteString(preferenceKI)
	b.WriteString("</preference_knowledge>")
	return b.String()
}

func (e *Engine) buildSemanticKI(semanticKI string) string {
	if semanticKI == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<semantic_knowledge>\n")
	b.WriteString("以下知识条目与当前任务相关，执行操作前请检查。\n\n")
	b.WriteString(semanticKI)
	b.WriteString("</semantic_knowledge>")
	return b.String()
}


func (e *Engine) buildFocus(path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("Focus file: %s", path)
}

func (e *Engine) buildEphemeral(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString("<EPHEMERAL_MESSAGE>\n")
		b.WriteString(msg)
		b.WriteString("\n</EPHEMERAL_MESSAGE>\n\n")
	}
	return b.String()
}

// prune applies 4-level budget-based pruning.
// Level 0 (Normal): <50% → no pruning
// Level 1 (Elevated): 50-70% → truncate long sections
// Level 2 (Tight): 70-85% → drop Skills → Memory → Variants → Knowledge → ProjectContext
// Level 3 (Critical): >85% → only Identity+Guidelines+Tooling+Runtime+Focus+Ephemeral
func (e *Engine) prune(sections []Section, budget int) (string, int) {
	if budget <= 0 {
		budget = 32000 // Default budget in tokens (~128K chars)
	}

	// Estimate: ~4 chars per token
	charBudget := budget * 4

	// Sort by order
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Order < sections[j].Order
	})

	// Calculate total size
	totalChars := 0
	for _, s := range sections {
		totalChars += len(s.Content)
	}

	pct := float64(totalChars) / float64(charBudget) * 100

	var pruneLevel int
	switch {
	case pct < 50:
		pruneLevel = 0
	case pct < 70:
		pruneLevel = 1
	case pct < 85:
		pruneLevel = 2
	default:
		pruneLevel = 3
	}

	// Apply pruning
	var b strings.Builder
	kept := 0
	for _, s := range sections {
		if s.Content == "" {
			continue
		}
		if s.Priority > 3-pruneLevel && s.Priority > 0 {
			continue // Drop low-priority sections
		}
		content := s.Content
		if pruneLevel >= 1 && len(content) > 2000 && s.Priority > 0 {
			content = content[:2000] + "\n... (truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
		kept += len(content)
	}

	// Rough token estimate
	tokens := kept / 4
	return b.String(), tokens
}
