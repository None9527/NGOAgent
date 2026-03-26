package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
)

// doPrepare detects ephemeral injection needs.
// Implements Anti's multi-layer ephemeral injection system with a cap
// to prevent context bloat (max 4 ephemerals per turn).
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
	boundaryName := a.boundaryTaskName
	boundaryMode := a.boundaryMode
	boundaryStatus := a.boundaryStatus
	boundarySummary := a.boundarySummary
	planMod := a.planModified
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

	// === Layer 1: Planning mode base template (skip in agentic — agent self-manages) ===
	if isPlanning && a.PlanMode() != "agentic" {
		a.InjectEphemeral(prompttext.EphPlanningMode)
	}

	// === Layer 1b: Agentic mode — autonomous decision-making ===
	if a.PlanMode() == "agentic" {
		a.InjectEphemeral(
			"🤖 [AGENTIC MODE] You are operating in fully autonomous mode.\n" +
				"You have complete decision-making authority:\n" +
				"- For complex, multi-step, or risky tasks: CREATE a plan first (use task_boundary + implementation_plan), then self-review and execute.\n" +
				"- For simple, single-step tasks: proceed directly without planning.\n" +
				"- You do NOT need user approval for plans — review them yourself and proceed.\n" +
				"- Prioritize thoroughness and correctness over speed.\n" +
				"- For tasks with 3+ independent components, use spawn_agent to parallelize.\n" +
				"Make your own judgment call on whether planning is needed.")
		// Team coordination protocol for sub-agent management
		a.InjectEphemeral(prompttext.TeamLeadPrompt)
	}

	a.mu.Lock()
	steps := a.stepsSinceUpdate
	a.mu.Unlock()

	// === Layer 2: Active task boundary reminder (frequency gated: every 3 steps) ===
	if boundaryName != "" {
		if steps == 0 || steps%3 == 0 {
			msg := prompttext.Render(prompttext.EphActiveTaskReminder, map[string]any{
				"TaskName": boundaryName,
				"Status":   boundaryStatus,
				"Summary":  boundarySummary,
				"Mode":     boundaryMode,
			})
			a.InjectEphemeral(msg)
		}
	}

	// === Layer 2b: Boundary frequency nudge (Anti's num_steps pattern) ===
	if ssb := a.guard.StepsSinceBoundary(); ssb >= 5 {
		a.InjectEphemeral(fmt.Sprintf(
			"<ephemeral_message>You have made %d tool calls without updating task progress. "+
				"Call task_boundary to report your current status when you reach a natural pause point.</ephemeral_message>", ssb))
	}

	// === Layer 3a: Artifact staleness reminder (skip when context tight) ===
	if !contextTight {
		a.mu.Lock()
		curStep := a.currentStep
		a.mu.Unlock()
		if a.deps.Brain != nil {
			checks := map[string]int{
				"task.md": 8,  // 8 steps without touching → remind
				"plan.md": 15, // plan is less frequently updated
			}
			for name, threshold := range checks {
				a.mu.Lock()
				lastStep, tracked := a.artifactLastStep[name]
				a.mu.Unlock()
				if !tracked {
					continue
				}
				if gap := curStep - lastStep; gap >= threshold {
					a.InjectEphemeral(fmt.Sprintf(
						"You have not updated %s in %d steps. Review and update it if needed.", name, gap))
				}
			}
		}
	}

	// === Layer 3b: Planning mode + no plan.md → force reminder ===
	if isPlanning && !planExists {
		a.InjectEphemeral(prompttext.EphPlanningNoPlanReminder)
	}

	// === Layer 3c: Plan modified but not reviewed by user ===
	if planMod && boundaryMode == "planning" {
		a.InjectEphemeral(prompttext.EphPlanModifiedReminder)
	}

	// === Layer 3d: Mode transitions (entering/exiting planning) ===
	a.mu.Lock()
	prevMode := a.previousMode
	a.mu.Unlock()
	if boundaryMode != "" && prevMode != "" && boundaryMode != prevMode {
		if boundaryMode == "planning" {
			a.InjectEphemeral(prompttext.EphEnteringPlanningMode)
		} else if prevMode == "planning" {
			a.InjectEphemeral(prompttext.EphExitingPlanningMode)
		}
		// Mode switch artifact existence check
		if boundaryMode == "execution" && a.deps.Brain != nil {
			if _, err := a.deps.Brain.Read("task.md"); err != nil {
				a.InjectEphemeral("You switched to EXECUTION mode but task.md doesn't exist. " +
					"Create it via task_plan(action=create, type=task) IMMEDIATELY.")
			}
		}
	}

	// Context usage warning
	if pct > 60 {
		msg := prompttext.Render(prompttext.EphContextStatus, map[string]any{
			"Percent": pct,
			"Used":    tokenEst,
			"Total":   policy.ContextWindow,
		})
		a.InjectEphemeral(msg)
	}

	// === Layer 4: Skill trigger auto-injection (heavy skills, skip when context tight) ===
	if !contextTight && a.deps.SkillMgr != nil && lastMsg != "" && a.currentStep < 2 {
		matches := a.deps.SkillMgr.MatchTriggers(lastMsg)
		for _, m := range matches {
			usage := m.Skill.Command
			if usage == "" {
				usage = "<subcommand> [args]"
			}
			a.InjectEphemeral(fmt.Sprintf(
				"\tSkill available: %s\n"+
					"Entry: %s/run.sh\n"+
					"Quick usage via run_command: cd %s && ./run.sh %s\n"+
					"For full guide: use_skill(name='%s')",
				m.Skill.Name, m.Skill.Path,
				m.Skill.Path, usage,
				m.Skill.Name,
			))
		}
	}

	// L2 Progressive Disclosure: inject skill instruction after SKILL.md read
	a.mu.Lock()
	skillName := a.skillLoaded
	a.skillLoaded = "" // one-shot: clear after injection
	a.skillPath = ""
	a.mu.Unlock()
	if skillName != "" {
		msg := prompttext.Render(prompttext.EphSkillInstruction, map[string]any{
			"SkillName": skillName,
		})
		a.InjectEphemeral(msg)
	}

	// === Layer 4: KI index re-injection (every 8 steps, skip when context tight) ===
	if !contextTight && a.deps.KIStore != nil && steps > 0 && steps%8 == 0 {
		kiIndex := a.deps.KIStore.GenerateKIIndex()
		if kiIndex != "" {
			a.InjectEphemeral("<knowledge_reminder>\n你有以下知识可用，需要时用 read_file 查看完整内容：\n" + kiIndex + "</knowledge_reminder>")
		}
	}
}

// shouldInjectPlanning checks if planning mode should be triggered.
func (a *AgentLoop) shouldInjectPlanning(userMessage string) bool {
	planMode := a.PlanMode()
	if planMode == "plan" {
		return true
	}
	if strings.Contains(userMessage, "/plan") {
		return true
	}
	return false
}
