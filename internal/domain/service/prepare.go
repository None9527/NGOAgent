package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
)

// doPrepare detects ephemeral injection needs.
// Uses the Ephemeral Budget System to prevent context bloat:
// candidates are collected with priority and dimension tags, then
// SelectWithBudget picks the best set within the token budget.
func (a *AgentLoop) doPrepare(_ context.Context) {
	// Sub-agents skip all planning/boundary/agentic injections
	if a.options.Mode == "subagent" {
		return
	}

	a.mu.Lock()
	lastMsg := ""
	if len(a.history) > 0 {
		lastMsg = a.history[len(a.history)-1].Content
	}
	boundaryName := a.task.BoundaryTaskName
	boundaryMode := a.task.BoundaryMode
	boundaryStatus := a.task.BoundaryStatus
	boundarySummary := a.task.BoundarySummary
	planMod := a.task.PlanModified
	a.mu.Unlock()

	isPlanning := a.shouldInjectPlanning(lastMsg)

	// Sync planning state to Guard for step-level enforcement
	// Cache Brain.Read results — reused in Layer 3b below
	planExists := false
	taskMdExists := false
	if a.deps.Brain != nil {
		if _, err := a.deps.Brain.Read("plan.md"); err == nil {
			planExists = true
		}
		if _, err := a.deps.Brain.Read("task.md"); err == nil {
			taskMdExists = true
		}
	}
	a.guard.SetModeState(isPlanning, planExists, taskMdExists, boundaryMode)

	// Context usage — computed once, reused for gating below
	tokenEst := a.estimateTokens()
	policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
	pct := int(float64(tokenEst) / float64(policy.ContextWindow) * 100)
	contextTight := pct > 80 // Skip low-priority ephemerals when context is tight

	// Collect candidates instead of direct injection
	var candidates []EphemeralCandidate

	// === Layer 1: Planning mode base template (skip in agentic — agent self-manages) ===
	if isPlanning && a.PlanMode() != "agentic" {
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.EphPlanningMode, Priority: 0, Dimension: "planning",
		})
	}

	// === Layer 1b: Agentic mode — autonomous decision-making ===
	if a.PlanMode() == "agentic" {
		candidates = append(candidates, EphemeralCandidate{
			Content: "🤖 [AGENTIC MODE] You are operating in fully autonomous mode.\n" +
				"You have complete decision-making authority:\n" +
				"- For complex, multi-step, or risky tasks: CREATE a plan first (use task_boundary + implementation_plan), then self-review and execute.\n" +
				"- For simple, single-step tasks: proceed directly without planning.\n" +
				"- You do NOT need user approval for plans — review them yourself and proceed.\n" +
				"- Prioritize thoroughness and correctness over speed.\n" +
				"- For tasks with 3+ independent components, use spawn_agent to parallelize.\n" +
				"Make your own judgment call on whether planning is needed.",
			Priority: 0, Dimension: "planning",
		})
		// Team coordination protocol for sub-agent management
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.TeamLeadPrompt, Priority: 1, Dimension: "team",
		})
	}

	a.mu.Lock()
	steps := a.task.StepsSinceUpdate
	a.mu.Unlock()

	// === Layer 2: Active task boundary reminder (frequency gated: every 3 steps) ===
	if boundaryName != "" {
		if steps > 0 && steps%3 == 0 {
			msg := prompttext.Render(prompttext.EphActiveTaskReminder, map[string]any{
				"TaskName": boundaryName,
				"Status":   boundaryStatus,
				"Summary":  boundarySummary,
				"Mode":     boundaryMode,
			})
			candidates = append(candidates, EphemeralCandidate{
				Content: msg, Priority: 1, Dimension: "context",
			})
		}
	}

	// === Layer 2b: Boundary frequency nudge (Anti's num_steps pattern) ===
	if ssb := a.guard.StepsSinceBoundary(); ssb >= 5 {
		candidates = append(candidates, EphemeralCandidate{
			Content: fmt.Sprintf(
				"<ephemeral_message>You have made %d tool calls without updating task progress. "+
					"Call task_boundary to report your current status when you reach a natural pause point.</ephemeral_message>", ssb),
			Priority: 2, Dimension: "context",
		})
	}

	// === Layer 3a: Artifact staleness reminder (skip when context tight) ===
	if !contextTight {
		a.mu.Lock()
		curStep := a.task.CurrentStep
		a.mu.Unlock()
		if a.deps.Brain != nil {
			checks := map[string]int{
				"task.md": 8,  // 8 steps without touching → remind
				"plan.md": 15, // plan is less frequently updated
			}
			var staleItems []string
			for name, threshold := range checks {
				a.mu.Lock()
				lastStep, tracked := a.task.ArtifactLastStep[name]
				a.mu.Unlock()
				if !tracked {
					continue
				}
				if gap := curStep - lastStep; gap >= threshold {
					staleItems = append(staleItems, fmt.Sprintf("%s (%d steps ago)", name, gap))
				}
			}
			if len(staleItems) > 0 {
				candidates = append(candidates, EphemeralCandidate{
					Content:  fmt.Sprintf("Stale artifacts: %s. Review and update if needed.", strings.Join(staleItems, ", ")),
					Priority: 3, Dimension: "context",
				})
			}
		}
	}

	// === Layer 3b: Planning mode + no plan.md → force reminder ===
	if isPlanning && !planExists {
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.EphPlanningNoPlanReminder, Priority: 1, Dimension: "planning",
		})
	}

	// === Layer 3c: Plan modified but not reviewed by user ===
	if planMod && boundaryMode == "planning" {
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.EphPlanModifiedReminder, Priority: 2, Dimension: "planning",
		})
	}

	// === Layer 3d: Mode transitions (entering/exiting planning) ===
	a.mu.Lock()
	prevMode := a.task.PreviousMode
	a.mu.Unlock()
	if boundaryMode != "" && prevMode != "" && boundaryMode != prevMode {
		if boundaryMode == "planning" {
			candidates = append(candidates, EphemeralCandidate{
				Content: prompttext.EphEnteringPlanningMode, Priority: 1, Dimension: "planning",
			})
		} else if prevMode == "planning" {
			candidates = append(candidates, EphemeralCandidate{
				Content: prompttext.EphExitingPlanningMode, Priority: 1, Dimension: "planning",
			})
		}
		// Mode switch artifact existence check
		if boundaryMode == "execution" && a.deps.Brain != nil {
			if _, err := a.deps.Brain.Read("task.md"); err != nil {
				candidates = append(candidates, EphemeralCandidate{
					Content:  "You switched to EXECUTION mode but task.md doesn't exist. Create it via task_plan(action=create, type=task) IMMEDIATELY.",
					Priority: 0, Dimension: "planning",
				})
			}
		}
	}

	// Context usage warning
	if pct > 75 {
		msg := prompttext.Render(prompttext.EphContextStatus, map[string]any{
			"Percent": pct,
			"Used":    tokenEst,
			"Total":   policy.ContextWindow,
		})
		candidates = append(candidates, EphemeralCandidate{
			Content: msg, Priority: 2, Dimension: "context",
		})
	}

	// === Layer 4: Skill trigger auto-injection (heavy skills, skip when context tight) ===
	if !contextTight && a.deps.SkillMgr != nil && lastMsg != "" && a.task.CurrentStep < 2 {
		matches := a.deps.SkillMgr.MatchTriggers(lastMsg)
		for _, m := range matches {
			usage := m.Skill.Command
			if usage == "" {
				usage = "<subcommand> [args]"
			}
			hint := fmt.Sprintf(
				"\tSkill available: %s\n"+
					"Entry: %s/run.sh\n"+
					"Quick usage via run_command: cd %s && ./run.sh %s\n"+
					"For full guide: read_file(path='%s/SKILL.md')",
				m.Skill.Name, m.Skill.Path,
				m.Skill.Path, usage,
				m.Skill.Path,
			)
			if len(m.Skill.Rules) > 0 {
				hint += "\nRULES (MUST follow):"
				for _, r := range m.Skill.Rules {
					hint += "\n- " + r
				}
			}
			candidates = append(candidates, EphemeralCandidate{
				Content: hint, Priority: 2, Dimension: "skill",
			})
		}
	}

	// L2 Progressive Disclosure: inject skill instruction after SKILL.md read
	a.mu.Lock()
	skillName, _ := a.task.ConsumeSkill()
	a.mu.Unlock()
	if skillName != "" {
		msg := prompttext.Render(prompttext.EphSkillInstruction, map[string]any{
			"SkillName": skillName,
		})
		candidates = append(candidates, EphemeralCandidate{
			Content: msg, Priority: 1, Dimension: "skill",
		})
	}

	// === Layer 4: KI index re-injection (every 8 steps, skip when context tight) ===
	if !contextTight && a.deps.KIStore != nil && steps > 0 && steps%8 == 0 {
		kiIndex := a.deps.KIStore.GenerateKIIndex()
		if kiIndex != "" {
			candidates = append(candidates, EphemeralCandidate{
				Content:  "<knowledge_reminder>\n你有以下知识可用，需要时用 read_file 查看完整内容：\n" + kiIndex + "</knowledge_reminder>",
				Priority: 3, Dimension: "ki",
			})
		}
	}

	// Budget-based selection: deduplicate by dimension, sort by priority, fit within budget
	budget := maxEphemeralBudget
	if contextTight {
		budget = maxEphemeralBudget / 2 // Halve budget when context is tight
	}
	selected := SelectWithBudget(candidates, budget)
	for _, msg := range selected {
		a.InjectEphemeral(msg)
	}
}

// shouldInjectPlanning checks if planning mode should be triggered.
func (a *AgentLoop) shouldInjectPlanning(userMessage string) bool {
	planMode := a.PlanMode()
	if planMode == "plan" {
		return true
	}
	// Exact command match: avoid false positives from "explain", "floorplan", etc.
	msg := strings.TrimSpace(userMessage)
	if msg == "/plan" || strings.HasPrefix(msg, "/plan ") {
		return true
	}
	return false
}
