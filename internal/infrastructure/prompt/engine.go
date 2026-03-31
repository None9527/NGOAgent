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
	Order    int    // 1-18
	Name     string // e.g., "Identity"
	Content  string
	Priority int // 0=required, 1=high, 2=medium, 3=low (for pruning)
}

// EffectivePriority returns the runtime-adjusted priority based on current step.
// KnowledgeIndex becomes less critical after early turns (agent already read KIs).
// Focus becomes more critical in later steps (task narrowing).
func (s *Section) EffectivePriority(step int) int {
	base := s.Priority
	switch s.Name {
	case "KnowledgeIndex":
		if step > 5 {
			base++ // Deprioritize after agent should have read KIs
		}
	case "Focus":
		if step > 10 {
			base-- // Prioritize as task narrows
		}
	case "SemanticMemory":
		if step > 15 {
			base++ // Less relevant in deep sessions
		}
	}
	if base < 0 {
		base = 0
	}
	if base > 3 {
		base = 3
	}
	return base
}

// SkillInfo is skill metadata provided by the skill manager.
type SkillInfo struct {
	Name        string
	Description string
	Type        string
	Weight      string
	Content     string
	Command     string
	Path        string
}

// Deps contains all data needed to assemble the system prompt.
type Deps struct {
	UserRules      string
	ProjectContext string
	SkillInfos     []SkillInfo
	ConvSummary    string   // KI index for prompt injection
	MemoryContent  string   // Vector memory retrieval for prompt injection
	ToolDescs      []ToolDesc
	Ephemeral      []string
	FocusFile      string
	TokenBudget    int
	Mode           string // chat
	Runtime        string // Pre-built runtime info (OS/time/model/workspace)
	CurrentStep    int    // Current agent step for dynamic section priority
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
	return e.prune(sections, deps.TokenBudget, deps.CurrentStep)
}

// AssembleSubagent builds a streamlined prompt for sub-agent workers.
// Keeps full execution capability but removes planning ceremony, user interaction,
// skills, KI, memory, and user rules. ~25% of parent prompt size.
func (e *Engine) AssembleSubagent(deps Deps) (string, int) {
	sections := []Section{
		{Order: 1, Name: "Identity", Content: prompttext.SubAgentIdentity, Priority: 0},
		{Order: 2, Name: "CoreBehavior", Content: prompttext.SubAgentBehavior, Priority: 0},
		{Order: 3, Name: "Safety", Content: prompttext.Safety, Priority: 0},
		{Order: 4, Name: "Tooling", Content: e.buildTooling(deps.ToolDescs), Priority: 0},
		{Order: 5, Name: "ToolCalling", Content: prompttext.ToolCalling, Priority: 0},
		{Order: 6, Name: "Runtime", Content: e.buildRuntime(deps), Priority: 1},
		{Order: 7, Name: "Ephemeral", Content: e.buildEphemeral(deps.Ephemeral), Priority: 0},
	}
	return e.prune(sections, deps.TokenBudget, deps.CurrentStep)
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
		{Order: 5, Name: "UserRules", Content: e.buildUserRules(deps.UserRules), Priority: 1},
		// ═══ Mid Valley: Capability inventory FIRST, then protocol (natural dependency) ═══
		{Order: 7, Name: "Tooling", Content: e.buildTooling(deps.ToolDescs), Priority: 0},
		{Order: 8, Name: "Skills", Content: e.buildSkills(deps.SkillInfos), Priority: 1},
		{Order: 9, Name: "ToolProtocol", Content: prompttext.ToolProtocol, Priority: 0},
		{Order: 10, Name: "ToolCalling", Content: prompttext.ToolCalling, Priority: 0},
		{Order: 11, Name: "ProjectContext", Content: e.buildProjectContext(deps.ProjectContext), Priority: 2},
		{Order: 12, Name: "Variants", Content: "", Priority: 3},
		// ═══ Tail Peak: Output format + Task Knowledge + Live Context (HIGH attention) ═══
		{Order: 13, Name: "ResponseFormat", Content: prompttext.ResponseFormat, Priority: 0},
		{Order: 14, Name: "KnowledgeIndex", Content: e.buildKnowledgeIndex(deps.ConvSummary), Priority: 0},
		{Order: 15, Name: "SemanticMemory", Content: e.buildSemanticMemory(deps.MemoryContent), Priority: 2},
		{Order: 16, Name: "Runtime", Content: e.buildRuntime(deps), Priority: 1},
		{Order: 17, Name: "Focus", Content: e.buildFocus(deps.FocusFile), Priority: 2},
		{Order: 18, Name: "Ephemeral", Content: e.buildEphemeral(deps.Ephemeral), Priority: 0},
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
	b.WriteString("Available skills:\n")
	for _, s := range infos {
		switch {
		case (s.Type == "executable" || s.Type == "hybrid") && s.Weight == "light":
			// Light executable: registered as direct ScriptTool
			b.WriteString(fmt.Sprintf("- %s [tool]: %s\n", s.Name, s.Description))
		case s.Weight == "heavy":
			// Heavy: trigger-inject auto-hints + run_command execution
			b.WriteString(fmt.Sprintf("- %s [run_command]: %s. Entry: %s/run.sh. For full guide: read_file(path='%s/SKILL.md')\n",
				s.Name, s.Description, s.Path, s.Path))
		default:
			// Workflow: read SKILL.md for guide
			b.WriteString(fmt.Sprintf("- %s [guide]: %s. Read: %s/SKILL.md\n", s.Name, s.Description, s.Path))
		}
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

func (e *Engine) buildKnowledgeIndex(kiIndex string) string {
	if kiIndex == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<knowledge_items>\n")
	b.WriteString("你的知识库索引。每个条目下的 📄 路径是完整知识文件，用 read_file 读取即可获取全部内容。\n\n")
	b.WriteString(kiIndex)
	b.WriteString("</knowledge_items>")
	return b.String()
}

func (e *Engine) buildSemanticMemory(memContent string) string {
	if memContent == "" {
		return ""
	}
	return "<semantic_memory>\nThe following are fragments from previous conversations that may be relevant to the current context:\n\n" + memContent + "</semantic_memory>"
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

// prune applies 4-level budget-based pruning with progressive truncation.
// Level 0 (Normal): <50% → no pruning
// Level 1 (Elevated): 50-70% → truncate Priority≥2 sections to 50%
// Level 2 (Tight): 70-85% → drop Priority≥2, truncate Priority=1 to 1000 chars
// Level 3 (Critical): >85% → only Priority=0 + UserRules kept
func (e *Engine) prune(sections []Section, budget int, step int) (string, int) {
	if budget <= 0 {
		budget = 32000 // Default budget in tokens (~128K chars)
	}

	// CJK-aware char budget: scan content to estimate chars-per-token ratio
	charBudget := e.estimateCharBudget(sections, budget)

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

	// Apply pruning with dynamic priority
	var b strings.Builder
	kept := 0
	for _, s := range sections {
		if s.Content == "" {
			continue
		}
		effPriority := s.EffectivePriority(step)
		// UserRules never dropped — contains user's highest-priority constraints
		if effPriority > 3-pruneLevel && effPriority > 0 && s.Name != "UserRules" {
			continue // Drop low-priority sections
		}
		content := s.Content
		// Progressive truncation instead of all-or-nothing
		if pruneLevel >= 2 && effPriority >= 1 && len(content) > 1000 {
			content = content[:1000] + "\n... (truncated)"
		} else if pruneLevel >= 1 && effPriority >= 2 && len(content) > 2000 {
			// Level 1: truncate medium-priority sections to 50%
			half := len(content) / 2
			if half > 2000 {
				half = 2000
			}
			content = content[:half] + "\n... (truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
		kept += len(content)
	}

	// CJK-aware token estimate for the kept content
	tokens := estimateTokensFromChars(b.String())
	return b.String(), tokens
}

// estimateCharBudget computes an appropriate char budget based on content CJK ratio.
// Pure English ≈ 4 chars/token, pure CJK ≈ 1.5 chars/token, mixed ≈ weighted blend.
func (e *Engine) estimateCharBudget(sections []Section, tokenBudget int) int {
	// Sample content to detect CJK ratio (scan first 2000 chars for speed)
	var sampled int
	var cjkCount int
	const sampleLimit = 2000

	for _, s := range sections {
		for _, r := range s.Content {
			if sampled >= sampleLimit {
				break
			}
			sampled++
			if r >= 0x2E80 { // CJK Radicals Supplement and beyond
				cjkCount++
			}
		}
		if sampled >= sampleLimit {
			break
		}
	}

	if sampled == 0 {
		return tokenBudget * 4
	}

	cjkRatio := float64(cjkCount) / float64(sampled)
	// Blend: CJK part → 1.5 chars/token, ASCII part → 4.0 chars/token
	charsPerToken := cjkRatio*1.5 + (1-cjkRatio)*4.0
	return int(float64(tokenBudget) * charsPerToken)
}

// estimateTokensFromChars estimates token count with CJK awareness.
func estimateTokensFromChars(s string) int {
	var tokens float64
	for _, r := range s {
		if r >= 0x2E80 {
			tokens += 1.5 // CJK ≈ 1.5 tokens per char
		} else {
			tokens += 0.25 // ASCII ≈ 0.25 tokens per char
		}
	}
	return int(tokens)
}
