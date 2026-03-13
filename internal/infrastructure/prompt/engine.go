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
	Path        string
}

// Deps contains all data needed to assemble the system prompt.
type Deps struct {
	UserRules      string
	ProjectContext string
	SkillInfos     []SkillInfo
	MemoryContent  string
	ConvSummary    string
	ToolDescs      []ToolDesc
	Ephemeral      []string
	FocusFile      string
	TokenBudget    int
	Mode           string // chat / heartbeat
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

// buildSections creates all 15 sections.
func (e *Engine) buildSections(deps Deps) []Section {
	sections := []Section{
		// ═══ Top: Required (never pruned) ═══
		{Order: 1, Name: "Identity", Content: prompttext.Identity, Priority: 0},
		{Order: 2, Name: "Guidelines", Content: prompttext.Guidelines, Priority: 0},
		{Order: 3, Name: "ToolCalling", Content: prompttext.ToolCalling, Priority: 0},
		{Order: 4, Name: "Safety", Content: prompttext.Safety, Priority: 0},
		// ═══ Mid: Prunable in order ═══
		{Order: 5, Name: "Runtime", Content: e.buildRuntime(deps), Priority: 1},
		{Order: 6, Name: "Tooling", Content: e.buildTooling(deps.ToolDescs), Priority: 0},
		{Order: 7, Name: "Skills", Content: e.buildSkills(deps.SkillInfos), Priority: 3},
		{Order: 8, Name: "UserRules", Content: e.buildUserRules(deps.UserRules), Priority: 1},
		{Order: 9, Name: "ProjectContext", Content: e.buildProjectContext(deps.ProjectContext), Priority: 2},
		{Order: 10, Name: "Memory", Content: e.buildMemory(deps.MemoryContent), Priority: 3},
		{Order: 11, Name: "Knowledge", Content: e.buildKnowledge(deps.ConvSummary), Priority: 2},
		// Section 12 removed: ConvSummary is already injected in Section 11 (Knowledge)
		// ═══ Bottom: Required (U-shape) ═══
		{Order: 13, Name: "Variants", Content: "", Priority: 3}, // Prompt variants (loaded from file)
		{Order: 14, Name: "Focus", Content: e.buildFocus(deps.FocusFile), Priority: 2},
		{Order: 15, Name: "Ephemeral", Content: e.buildEphemeral(deps.Ephemeral), Priority: 0},
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
	var b strings.Builder
	b.WriteString("# Available Tools\n\n")
	for _, td := range descs {
		b.WriteString(fmt.Sprintf("## %s\n%s\n\n", td.Name, td.Description))
	}
	return b.String()
}

func (e *Engine) buildSkills(infos []SkillInfo) string {
	if len(infos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("You can use specialized 'skills' to help you with complex tasks.\n")
	b.WriteString("If a skill seems relevant, use `read_file` on its SKILL.md to get full instructions.\n\n")
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

func (e *Engine) buildMemory(mem string) string {
	if mem == "" {
		return ""
	}
	return "<memory>\n" + mem + "\n</memory>"
}

func (e *Engine) buildKnowledge(summary string) string {
	if summary == "" {
		return ""
	}
	return "<knowledge>\n" + summary + "\n</knowledge>"
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
