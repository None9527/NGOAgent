package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// doPrepare detects ephemeral injection needs.
// Uses the Ephemeral Budget System to prevent context bloat:
// candidates are collected with priority and dimension tags, then
// SelectWithBudget picks the best set within the token budget.
func (a *AgentLoop) doPrepare(_ context.Context) graphruntime.PlanningState {
	// Sub-agents skip all planning/boundary/agentic injections
	if a.options.Mode == "subagent" {
		return graphruntime.PlanningState{}
	}

	a.mu.Lock()
	lastMsg := ""
	if len(a.history) > 0 {
		lastMsg = a.history[len(a.history)-1].Content
	}
	boundaryName := a.task.Name
	boundaryMode := a.task.Mode
	boundaryStatus := a.task.Status
	boundarySummary := a.task.Summary
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
	mode := a.Mode()

	// Context usage — computed once, reused for gating below
	tokenEst := a.estimateTokens()
	policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
	pct := int(float64(tokenEst) / float64(policy.ContextWindow) * 100)
	contextTight := pct > 80 // Skip low-priority ephemerals when context is tight
	planning := graphruntime.PlanningState{
		Required:       isPlanning || mode.ForcePlan,
		BoundaryMode:   boundaryMode,
		PlanExists:     planExists,
		TaskExists:     taskMdExists,
		ContextTight:   contextTight,
		ReviewRequired: !a.Mode().SelfReview,
	}
	if mode.ForcePlan {
		planning.Trigger = "mode_force_plan"
	}
	if isPlanning {
		planning.Trigger = "user_plan_request"
	}
	if !planExists {
		planning.MissingArtifacts = append(planning.MissingArtifacts, "plan.md")
	}
	if boundaryMode == "execution" && !taskMdExists {
		planning.MissingArtifacts = append(planning.MissingArtifacts, "task.md")
	}

	// Collect candidates instead of direct injection
	var candidates []EphemeralCandidate

	// === Layer 1: Planning mode base template (inject when forced or user-triggered) ===
	slog.Info(fmt.Sprintf("[prepare] mode=%s ForcePlan=%v isPlanning=%v", mode.String(), mode.ForcePlan, isPlanning))
	if isPlanning || mode.ForcePlan {
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.EphPlanningMode, Priority: 0, Dimension: "planning",
		})
		slog.Info(fmt.Sprintf("[prepare] ✅ Planning mode ephemeral INJECTED (len=%d)", len(prompttext.EphPlanningMode)))
	}

	// === Layer 1b: Self-review mode — autonomous decision-making (agentic/agentic+evo) ===
	// NOTE: Dimension "agentic" is separate from "planning" so both survive SelectWithBudget.
	// EphAgenticMode overrides per-turn behavior: no user approval needed for plans.
	if a.Mode().SelfReview {
		candidates = append(candidates, EphemeralCandidate{
			Content:  prompttext.EphAgenticMode,
			Priority: 0, Dimension: "agentic",
		})
		// Team coordination protocol for sub-agent management
		candidates = append(candidates, EphemeralCandidate{
			Content: prompttext.TeamLeadPrompt, Priority: 1, Dimension: "team",
		})
		// P3 I1: 4-Phase execution hint (starts after first tool call, avoids noise on step 0)
		if a.task.CurrentStep > 1 {
			if phaseHint := a.phaseDetector.PhaseEphemeral(); phaseHint != "" {
				candidates = append(candidates, EphemeralCandidate{
					Content: phaseHint, Priority: 2, Dimension: "phase",
				})
			}
		}
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
			// Entering planning: use "transition" dimension so it doesn't conflict with EphPlanningMode.
			// EphPlanningMode (Dimension "planning") already covers the full protocol.
			// This is just a lightweight transition signal.
			candidates = append(candidates, EphemeralCandidate{
				Content:  "Mode transition: you are now entering planning mode. Follow the planning workflow detailed in the system prompt.",
				Priority: 2, Dimension: "transition",
			})
		} else if prevMode == "planning" {
			candidates = append(candidates, EphemeralCandidate{
				Content: prompttext.EphExitingPlanningMode, Priority: 1, Dimension: "transition",
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

	// === Layer 3e: Token usage self-awareness (Sprint 2-3) ===
	// Inject at 60% to let agent proactively manage output length
	if pct > 60 && pct <= 75 {
		candidates = append(candidates, EphemeralCandidate{
			Content: fmt.Sprintf(
				"<context_usage>Context: %d/%d tokens (%.0f%%). "+
					"Getting full — keep responses concise, avoid unnecessary tool output.</context_usage>",
				tokenEst, policy.ContextWindow, float64(pct)),
			Priority: 2, Dimension: "meta",
		})
	}

	// Context usage warning (existing — fires at 75%+)
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

	// === Layer 3f: Scratchpad directory (Sprint 3-1) ===
	// Inject scratchpad path so agent knows where to write shared artifacts
	if a.deps.Brain != nil {
		scratchDir := a.deps.Brain.BaseDir() + "/scratchpad"
		// Only inject once (first 2 steps) or when scratchpad has content
		if a.task.CurrentStep < 2 {
			candidates = append(candidates, EphemeralCandidate{
				Content: fmt.Sprintf(
					"<scratchpad>Shared workspace: %s\n"+
						"Workers in this session can read/write here for intermediate results, "+
						"research notes, and cross-worker knowledge sharing.</scratchpad>",
					scratchDir),
				Priority: 3, Dimension: "scratchpad",
			})
		}
	}

	// === Layer 4: Skill injection removed — skills now invoked via dedicated skill() tool ===

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
	a.setPlanningDecision(planning)
	return planning
}

// shouldInjectPlanning checks if planning mode should be triggered.
func (a *AgentLoop) shouldInjectPlanning(userMessage string) bool {
	if a.Mode().ForcePlan {
		return true
	}
	// Exact command match: avoid false positives from "explain", "floorplan", etc.
	msg := strings.TrimSpace(userMessage)
	if msg == "/plan" || strings.HasPrefix(msg, "/plan ") {
		return true
	}
	return false
}
