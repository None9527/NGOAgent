// Package prompt assembles the 15-section system prompt with budget-based pruning.
package prompt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/profile"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
)

// Section represents one numbered section of the system prompt.
type Section struct {
	Order     int    // 1-18
	Name      string // e.g., "Identity"
	Content   string
	Priority  int  // 0=required, 1=high, 2=medium, 3=low (for pruning)
	Cacheable bool // content is stable across turns; eligible for provider cache
	CacheTier int  // 0=dynamic, 1=core(immutable), 2=session(stable)
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
	WhenToUse   string
	Context     string
	Args        string
}

// Deps contains all data needed to assemble the system prompt.
type Deps struct {
	UserRules      string
	ProjectContext string
	SkillInfos     []SkillInfo
	ConvSummary    string // KI index for prompt injection
	MemoryContent  string // Vector memory retrieval for prompt injection
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
	homeDir     string                    // ~/.ngoagent/
	registry    *Registry                 // Section factory registry
	allOverlays []profile.BehaviorOverlay // All registered overlays
	active      []profile.BehaviorOverlay // Currently active overlays
}

// defaultOverlays returns the standard set of behavior overlays.
func defaultOverlays() []profile.BehaviorOverlay {
	return []profile.BehaviorOverlay{
		&profile.CodingOverlay{},
		&profile.ResearchOverlay{},
	}
}

// NewEngine creates a prompt engine with the default section registry.
// Default active overlays: CodingOverlay (backward compatible).
func NewEngine() *Engine {
	all := defaultOverlays()
	e := &Engine{registry: NewRegistry(), allOverlays: all, active: all[:1]}
	e.RegisterDefaults()
	return e
}

// NewEngineWithHome creates a prompt engine with a home directory for file discovery.
func NewEngineWithHome(homeDir string) *Engine {
	all := defaultOverlays()
	e := &Engine{homeDir: homeDir, registry: NewRegistry(), allOverlays: all, active: all[:1]}
	e.RegisterDefaults()
	return e
}

// Registry exposes the section registry for external extension / testing.
func (e *Engine) Registry() *Registry { return e.registry }

// ActivateOverlays detects which overlays should be active based on user message and workspace.
// Call this at the start of each agent turn to dynamically compose behavior.
func (e *Engine) ActivateOverlays(userMessage string, workspaceFiles []string) {
	e.active = profile.ActiveOverlays(e.allOverlays, userMessage, workspaceFiles)
	e.RegisterDefaults()
}

// SetOverlays explicitly sets the active overlays (for testing or /profile command).
func (e *Engine) SetOverlays(overlays []profile.BehaviorOverlay) {
	e.active = overlays
	e.RegisterDefaults()
}

// ActiveProfile returns the names of all active overlays (for logging).
func (e *Engine) ActiveProfile() string {
	return profile.ActiveNames(e.active)
}

// DiscoverUserRules loads user rules from global and project-level files.
func (e *Engine) DiscoverUserRules(workspaceDir string) (string, error) {
	if e.homeDir == "" {
		return "", nil
	}
	d := NewDiscovery(e.homeDir, workspaceDir)
	return d.LoadUserRules(), nil
}

// CacheSegment represents one cache-breakpoint segment of the assembled prompt.
// DashScope supports up to 4 cache_control markers per request.
type CacheSegment struct {
	Content   string // Assembled section text
	Tokens    int    // Estimated token count
	Cacheable bool   // Should this segment get cache_control marker?
	Tier      int    // CacheTierCore or CacheTierSession
}

// AssembleResult carries the cache-split prompt assembly output.
// Segments are ordered: [CoreStatic, SessionStatic, Dynamic].
// When provider caching is unavailable, join all segments.
type AssembleResult struct {
	Segments    []CacheSegment // Ordered segments (up to 3)
	Static      string         // Convenience: CoreStatic + SessionStatic concatenated
	Dynamic     string         // Per-request sections
	TokensTotal int            // Estimated total tokens
	TokenStatic int            // Estimated tokens for all static segments
}

// Assemble builds the complete system prompt (all sections merged).
// Returns the system prompt string and estimated token count.
func (e *Engine) Assemble(deps Deps) (string, int) {
	sections := e.buildSections(deps)
	return e.prune(sections, deps.TokenBudget, deps.CurrentStep)
}

// AssembleSplit builds the system prompt split into tiered cache segments.
// Returns up to 3 segments: CoreStatic (immutable), SessionStatic (per-session),
// Dynamic (per-request). Each cacheable segment gets its own cache_control marker.
func (e *Engine) AssembleSplit(deps Deps) AssembleResult {
	sections := e.buildSections(deps)

	var coreSecs, sessionSecs, dynamicSecs []Section
	for _, s := range sections {
		switch s.CacheTier {
		case CacheTierCore:
			coreSecs = append(coreSecs, s)
		case CacheTierSession:
			sessionSecs = append(sessionSecs, s)
		default:
			dynamicSecs = append(dynamicSecs, s)
		}
	}

	budget := deps.TokenBudget

	coreStr, coreTok := e.prune(coreSecs, budget, deps.CurrentStep)
	budget -= coreTok

	sessionStr, sessionTok := e.prune(sessionSecs, budget, deps.CurrentStep)
	budget -= sessionTok

	dynamicStr, dynamicTok := e.prune(dynamicSecs, budget, deps.CurrentStep)

	var segments []CacheSegment
	if coreStr != "" {
		segments = append(segments, CacheSegment{
			Content: coreStr, Tokens: coreTok, Cacheable: true, Tier: CacheTierCore,
		})
	}
	if sessionStr != "" {
		segments = append(segments, CacheSegment{
			Content: sessionStr, Tokens: sessionTok, Cacheable: true, Tier: CacheTierSession,
		})
	}
	if dynamicStr != "" {
		segments = append(segments, CacheSegment{
			Content: dynamicStr, Tokens: dynamicTok, Cacheable: false, Tier: CacheTierDynamic,
		})
	}

	staticTok := coreTok + sessionTok
	return AssembleResult{
		Segments:    segments,
		Static:      strings.TrimSpace(coreStr + "\n\n" + sessionStr),
		Dynamic:     dynamicStr,
		TokensTotal: coreTok + sessionTok + dynamicTok,
		TokenStatic: staticTok,
	}
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

// RegisterDefaults registers all canonical sections in order.
// CacheTier 1 (Core): immutable framework-level content.
// CacheTier 2 (Session): stable per-session, may change across sessions.
// CacheTier 0 (Dynamic): per-request content.
// External callers can call Registry().Register() to add or override sections.
func (e *Engine) RegisterDefaults() {
	r := e.registry

	// ═══ Head: Identity + Behavior + Constraints ═══ (CORE — immutable)
	// Architecture: Omni (universal base) + Σ(active overlays)
	// Multiple overlays can be active simultaneously for composable behavior.
	active := e.active // Captured by closures below
	r.Register(SectionMeta{Name: "Identity", Order: 1, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section {
			return Section{Content: profile.ComposeIdentity(active)}
		},
	})
	r.Register(SectionMeta{Name: "CoreBehavior", Order: 2, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section {
			return Section{Content: profile.OmniBehavior}
		},
	})
	r.Register(SectionMeta{Name: "DoingTasks", Order: 3, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section {
			return Section{Content: profile.ComposeGuidelines(active)}
		},
	})
	r.Register(SectionMeta{Name: "ToneAndStyle", Order: 4, Priority: 1, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section {
			return Section{Content: profile.ComposeTone(active)}
		},
	})
	r.Register(SectionMeta{Name: "OutputCapabilities", Order: 5, Priority: 1, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section { return Section{Content: prompttext.OutputCapabilities} },
	})
	r.Register(SectionMeta{Name: "Safety", Order: 6, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section { return Section{Content: prompttext.Safety} },
	})

	// ═══ Mid: Session-stable content ═══ (SESSION — changes across sessions)
	r.Register(SectionMeta{Name: "UserRules", Order: 7, Priority: 1, CacheTier: CacheTierSession,
		Factory: func(d Deps) Section { return Section{Content: e.buildUserRules(d.UserRules)} },
	})
	r.Register(SectionMeta{Name: "Tooling", Order: 8, Priority: 0, CacheTier: CacheTierSession,
		Factory: func(d Deps) Section { return Section{Content: e.buildTooling(d.ToolDescs)} },
	})
	r.Register(SectionMeta{Name: "Skills", Order: 9, Priority: 1, CacheTier: CacheTierSession,
		Factory: func(d Deps) Section { return Section{Content: e.buildSkills(d.SkillInfos)} },
	})
	r.Register(SectionMeta{Name: "ToolProtocol", Order: 10, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section { return Section{Content: prompttext.ToolProtocol} },
	})
	r.Register(SectionMeta{Name: "ToolCalling", Order: 11, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section { return Section{Content: prompttext.ToolCalling} },
	})
	r.Register(SectionMeta{Name: "ProjectContext", Order: 12, Priority: 2, CacheTier: CacheTierSession,
		Factory: func(d Deps) Section { return Section{Content: e.buildProjectContext(d.ProjectContext)} },
	})
	r.Register(SectionMeta{Name: "Variants", Order: 13, Priority: 3, CacheTier: CacheTierSession,
		Factory: func(d Deps) Section { return Section{Content: ""} },
	})
	r.Register(SectionMeta{Name: "ResponseFormat", Order: 14, Priority: 0, CacheTier: CacheTierCore,
		Factory: func(d Deps) Section { return Section{Content: prompttext.ResponseFormat} },
	})

	// ═══ Tail: Per-request dynamic content ═══ (DYNAMIC)
	r.Register(SectionMeta{Name: "KnowledgeIndex", Order: 15, Priority: 1, CacheTier: CacheTierDynamic,
		Factory: func(d Deps) Section { return Section{Content: e.buildKnowledgeIndex(d.ConvSummary)} },
	})
	r.Register(SectionMeta{Name: "SemanticMemory", Order: 16, Priority: 2, CacheTier: CacheTierDynamic,
		Factory: func(d Deps) Section { return Section{Content: e.buildSemanticMemory(d.MemoryContent)} },
	})
	r.Register(SectionMeta{Name: "Runtime", Order: 17, Priority: 1, CacheTier: CacheTierDynamic,
		Factory: func(d Deps) Section { return Section{Content: e.buildRuntime(d)} },
	})
	r.Register(SectionMeta{Name: "Focus", Order: 18, Priority: 2, CacheTier: CacheTierDynamic,
		Factory: func(d Deps) Section { return Section{Content: e.buildFocus(d.FocusFile)} },
	})
	r.Register(SectionMeta{Name: "Ephemeral", Order: 19, Priority: 0, CacheTier: CacheTierDynamic,
		Factory: func(d Deps) Section { return Section{Content: e.buildEphemeral(d.Ephemeral)} },
	})
}

// buildSections materializes sections from the registry.
func (e *Engine) buildSections(deps Deps) []Section {
	return e.registry.Build(deps)
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
	b.WriteString("- Prefer purpose-built tools over run_command (edit_file > sed, grep_search > grep, tree > ls -R, diff_files > diff)\n")
	b.WriteString("- run_command: set background=true for long-running processes (servers, builds)\n")
	b.WriteString("- run_command: working directory PERSISTS between calls\n")
	b.WriteString("- task_plan: NEVER use write_file for plan.md/task.md/walkthrough.md\n")
	b.WriteString("- http_fetch: for localhost/internal APIs and simple public endpoints; use web_fetch for Cloudflare-protected or JS-heavy sites\n")
	return b.String()
}

func (e *Engine) buildSkills(infos []SkillInfo) string {
	if len(infos) == 0 {
		return ""
	}
	// Convert SkillInfo → prompttext.SkillEntry for budget-controlled formatting
	entries := make([]prompttext.SkillEntry, len(infos))
	for i, s := range infos {
		entries[i] = prompttext.SkillEntry{
			Name:        s.Name,
			Description: s.Description,
			Type:        s.Type,
			Weight:      s.Weight,
			Path:        s.Path,
			Content:     s.Content,
			WhenToUse:   s.WhenToUse,
			Context:     s.Context,
			Args:        s.Args,
		}
	}
	return prompttext.FormatSkillsWithBudget(entries, prompttext.DefaultSkillCharBudget)
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
	// M2: <verified_knowledge> — explicitly high-trust, LLM should prefer this over working_memory
	b.WriteString("<verified_knowledge>\n")
	b.WriteString("以下是经过整理和验证的知识库（高可信度）。每个条目的 📄 路径是完整知识文件，用 read_file 读取完整内容。\n\n")
	b.WriteString(kiIndex)
	b.WriteString("</verified_knowledge>")
	return b.String()
}

func (e *Engine) buildSemanticMemory(memContent string) string {
	if memContent == "" {
		return ""
	}
	// M2: <working_memory> — explicitly low-trust, verify before using
	// CC-inspired: MEMORY_DRIFT_CAVEAT + TRUSTING_RECALL_SECTION
	return "<working_memory>\n" +
		"以下是本会话的工作记忆片段（自动保存，未经整理，低可信度）。\n" +
		"⚠️ 与 verified_knowledge 冲突时，以 verified_knowledge 为准。\n" +
		"涉及文件路径、函数名、API 时，使用前必须通过工具验证其仍然存在。\n\n" +
		memContent + "</working_memory>"
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
