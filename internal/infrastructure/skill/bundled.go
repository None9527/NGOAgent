package skill

import (
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
)

// ─── P1 #49: Bundled Skills — built-in recovery strategies ─────────────

// RegisterBundled adds built-in skills to the manager.
// These are not loaded from disk — they're hardcoded recovery strategies
// that the agent can use when it detects common failure patterns.
func (m *Manager) RegisterBundled() {
	bundled := []*entity.Skill{
		bundledLoopBreaker(),
		bundledStuckRecovery(),
		bundledDebugHelper(),
	}
	for _, s := range bundled {
		if _, exists := m.skills[s.Name]; !exists {
			m.skills[s.Name] = s
		}
	}
}

// bundledLoopBreaker helps the agent break out of repetitive action loops.
func bundledLoopBreaker() *entity.Skill {
	return &entity.Skill{
		ID:          "_loop_breaker",
		Name:        "_loop_breaker",
		Description: "Detect and break repetitive action loops (e.g., same edit failing repeatedly)",
		Type:        "workflow",
		Weight:      "light",
		Triggers:    []string{},
		Enabled:     true,
		EvoStatus:   "evolved",
		InstalledAt: time.Now(),
		Content: `---
name: _loop_breaker
description: Break repetitive action loops
---

# Loop Breaker

You are stuck in a repetitive loop. STOP and follow these steps:

1. **Identify the loop**: What action are you repeating? What error keeps occurring?
2. **Root cause**: Why does the action keep failing? Common causes:
   - Wrong file path or content target
   - Missing dependency or import
   - Wrong approach entirely
3. **Break the loop**: Try ONE of these strategies:
   - **Read first**: Use read_file to see the actual current file state
   - **Different approach**: If edit_file keeps failing, try write_file with full content
   - **Step back**: Run the tests/build to understand the real error
   - **Ask the user**: If you've tried 3+ approaches, ask for clarification

RULE: After 3 consecutive failures of the same tool on the same file, you MUST switch strategies.`,
	}
}

// bundledStuckRecovery helps when the agent doesn't know what to do next.
func bundledStuckRecovery() *entity.Skill {
	return &entity.Skill{
		ID:          "_stuck_recovery",
		Name:        "_stuck_recovery",
		Description: "Recovery strategies when the agent is stuck or uncertain",
		Type:        "workflow",
		Weight:      "light",
		Triggers:    []string{},
		Enabled:     true,
		EvoStatus:   "evolved",
		InstalledAt: time.Now(),
		Content: `---
name: _stuck_recovery
description: Recovery when stuck or uncertain
---

# Stuck Recovery

If you're unsure how to proceed, follow this checklist:

1. **Re-read the user's request**: What exactly did they ask for?
2. **Check what you know**: What files have you read? What did you learn?
3. **Gather more context**:
   - Run 'find . -name "*.go" | head -20' to understand project structure
   - Read key files: go.mod, package.json, README.md
   - Check git log -5 for recent changes
4. **Start small**: Make the smallest possible change first, verify it works
5. **Report blockers**: If you need information you can't find, tell the user

RULE: Never produce empty or placeholder implementations. Always write complete, working code.`,
	}
}

// bundledDebugHelper provides structured debugging approach.
func bundledDebugHelper() *entity.Skill {
	return &entity.Skill{
		ID:          "_debug_helper",
		Name:        "_debug_helper",
		Description: "Structured approach for debugging errors and test failures",
		Type:        "workflow",
		Weight:      "light",
		Triggers:    []string{},
		Enabled:     true,
		EvoStatus:   "evolved",
		InstalledAt: time.Now(),
		Content: `---
name: _debug_helper
description: Structured debugging for errors
---

# Debug Helper

When you encounter an error, follow this systematic approach:

1. **Read the error**: Copy the EXACT error message. Don't paraphrase.
2. **Locate the source**: Find the file and line number mentioned in the error.
3. **Read context**: Read 20 lines around the error location.
4. **Categorize**:
   - Syntax error → fix the syntax
   - Import error → check go.mod or package name
   - Type error → check function signatures and interfaces
   - Runtime error → add logging, check nil values
5. **Fix and verify**: Make ONE change, then re-run the failing command.

RULE: Never make multiple unrelated fixes at once. Fix one error, verify, then fix the next.`,
	}
}
