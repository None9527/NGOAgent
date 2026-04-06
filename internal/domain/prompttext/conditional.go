// Package prompttext — conditional prompt generation (Sprint 1-1).
// Provides ToolContext-aware dynamic prompt builders that reduce
// irrelevant token injection by ~30%.
package prompttext

import (
	"fmt"
	"strings"
)

// Base tool descriptions used only as seeds for dynamic generation.
// These are NOT exported — callers use the Dynamic() functions.
const toolRunCommandBase = `Execute a shell command. Set background=true for long-running processes (servers, builds).
- cwd: persists between calls automatically
- wait_ms_before_async: wait before auto-backgrounding (use 500 for slow cmds like npm install, go build)
- Output >50KB is truncated (head + tail)`

// ToolContext captures runtime environment capabilities.
// Detected once at builder init time, reused for all tool descriptions.
type ToolContext struct {
	HasGit     bool // Is the workspace a git repo?
	HasSandbox bool // Is sandbox.Manager active?
	SkillCount int  // Number of registered skills
	HasBrain   bool // Is brain directory configured?
}

// ── Conditional Sections (injected only when relevant) ──────────────

const gitCommitSection = `

# Git operations
When committing changes, follow these principles:
- NEVER update the git config
- NEVER run destructive git commands (push --force, reset --hard) unless the user explicitly requests
- NEVER skip hooks (--no-verify) unless the user explicitly requests
- Prefer creating NEW commits rather than amending. If a pre-commit hook fails, fix the issue and create a NEW commit.
- When staging files, prefer adding specific files by name rather than "git add -A"
- NEVER commit changes unless the user explicitly asks you to
- Use gh command via run_command for GitHub-related tasks (issues, PRs, checks)
- Pass commit messages via HEREDOC for proper formatting: git commit -m "$(cat <<'EOF'
  Message here
  EOF
  )"`

const sandboxSection = `

# Command sandbox
Commands run in a sandbox that controls filesystem and network access.
- Use $TMPDIR for temporary files (auto-set to sandbox-writable dir)
- Do NOT use /tmp directly — use $TMPDIR instead
- If a command fails due to sandbox restrictions, retry outside sandbox or adjust approach`

// ToolRunCommandDynamic generates a context-aware run_command description.
// Omits git/sandbox sections when those capabilities are absent.
func ToolRunCommandDynamic(ctx ToolContext) string {
	base := toolRunCommandBase // Start with the base constant

	if ctx.HasGit {
		base += gitCommitSection
	}
	if ctx.HasSandbox {
		base += sandboxSection
	}
	return base
}

// ToolSpawnAgentDynamic generates an enhanced spawn_agent description
// with coordinator decision matrix and prompt-writing guidance.
func ToolSpawnAgentDynamic(ctx ToolContext) string {
	enhanced := ToolSpawnAgent + `

## Writing the prompt
Brief the agent like a smart colleague who just walked into the room — it hasn't seen this conversation.
- Explain what you're trying to accomplish and why.
- Describe what you've already learned or ruled out.
- Give enough context that the agent can make judgment calls.
- Include file paths, line numbers, and specific details.

**Never delegate understanding.** Don't write "based on your findings, fix the bug."
Write prompts that prove you understood: include file paths, line numbers, what specifically to change.

## When NOT to use spawn_agent
- Reading a specific file: use read_file directly
- Searching for code: use grep_search directly
- Simple single-file edits: do it yourself
- Tasks that take <30 seconds: faster to do directly`

	if ctx.HasBrain {
		enhanced += `

## Scratchpad
Workers in the same session share a scratchpad directory for intermediate results.
The scratchpad path is automatically injected into worker task descriptions.
Use it for: research notes, findings, partial results that other workers need.`
	}
	return enhanced
}

// ── Skill Listing Budget Control (Sprint 1-2) ────────────────────

const (
	DefaultSkillCharBudget = 60000 // Heavy skills inject full SKILL.md (~10KB each × 4)
	MaxListingDescChars    = 200   // Per-skill description cap
	MinDescLength          = 20    // Below this → names-only fallback
)

// SkillEntry represents a skill for listing purposes.
type SkillEntry struct {
	Name        string
	Description string
	Type        string // "executable", "pipeline", "workflow"
	Weight      string // "light", "heavy" (auto-derived)
	Path        string
	Content     string // SKILL.md body (heavy skills only, auto-injected)
	WhenToUse   string // trigger condition from frontmatter
	Context     string // "inline" | "fork"
	Args        string // parameter hint
}

// FormatSkillsWithBudget applies three-level degradation:
// Level 1: Full descriptions (within budget)
// Level 2: Truncated descriptions (each ≤ MaxListingDescChars)
// Level 3: Names only (extreme case)
func FormatSkillsWithBudget(skills []SkillEntry, budget int) string {
	if len(skills) == 0 {
		return ""
	}
	if budget <= 0 {
		budget = DefaultSkillCharBudget
	}

	// Level 1: Try full descriptions
	full := formatSkillsFull(skills)
	if len(full) <= budget {
		return full
	}

	// Level 2: Truncated descriptions
	perSkillBudget := (budget - len("Available skills:\n")) / len(skills)
	if perSkillBudget >= MinDescLength+10 { // 10 chars for "- name: "
		return formatSkillsTruncated(skills, perSkillBudget)
	}

	// Level 3: Names only
	return formatSkillsNamesOnly(skills)
}

func formatSkillsFull(skills []SkillEntry) string {
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, s := range skills {
		b.WriteString(formatOneSkill(s, 0))
	}
	return b.String()
}

func formatSkillsTruncated(skills []SkillEntry, maxDescChars int) string {
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, s := range skills {
		b.WriteString(formatOneSkill(s, maxDescChars))
	}
	return b.String()
}

func formatSkillsNamesOnly(skills []SkillEntry) string {
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, s := range skills {
		b.WriteString(fmt.Sprintf("- %s\n", s.Name))
	}
	return b.String()
}

// formatOneSkill formats a single skill entry for system prompt listing.
// All skills are now invoked via skill(name="X") — no more read_file.
// maxDesc=0 means no truncation.
func formatOneSkill(s SkillEntry, maxDesc int) string {
	desc := s.Description
	if maxDesc > 0 && len(desc) > maxDesc {
		desc = desc[:maxDesc-1] + "…"
	}

	var b strings.Builder

	// Executable skills still have their own dedicated tools
	if s.Type == "executable" {
		b.WriteString(fmt.Sprintf("- %s [tool]: %s\n", s.Name, desc))
		return b.String()
	}

	// All other skills use skill() tool
	b.WriteString(fmt.Sprintf("- %s [skill]: %s\n", s.Name, desc))
	if s.WhenToUse != "" {
		b.WriteString(fmt.Sprintf("  ↳ trigger: %s\n", s.WhenToUse))
	}
	mode := "inline"
	if s.Context == "fork" {
		mode = "fork (sub-agent)"
	}
	if s.Args != "" {
		b.WriteString(fmt.Sprintf("  ↳ invoke: skill(name=\"%s\", args=\"%s\") [%s]\n", s.Name, s.Args, mode))
	} else {
		b.WriteString(fmt.Sprintf("  ↳ invoke: skill(name=\"%s\") [%s]\n", s.Name, mode))
	}
	return b.String()
}
