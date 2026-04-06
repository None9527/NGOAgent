// Package service — Evo evaluation controller.
// Extracted from run.go (Sprint 1-2): manages async evo evaluation and repair dispatch.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// fireHooks invokes all PostRunHooks asynchronously.
func (a *AgentLoop) fireHooks(ctx context.Context, steps int) {
	if a.deps.Hooks == nil {
		return
	}
	a.mu.Lock()
	finalContent := ""
	userMsg := ""
	for _, m := range a.history {
		if m.Role == "user" && m.Content != "" {
			userMsg = m.Content
			break
		}
	}
	if len(a.history) > 0 {
		finalContent = a.history[len(a.history)-1].Content
	}
	// Snapshot history for async hooks (KI distillation)
	historySnapshot := make([]model.Message, len(a.history))
	copy(historySnapshot, a.history)

	// Capture evo state and clear it
	evoEval := a.evoLastEval
	evoPlan := a.evoLastPlan
	evoSuccess := a.evoRepairSuccess
	a.evoLastEval = nil
	a.evoLastPlan = nil
	a.evoRepairSuccess = false
	a.mu.Unlock()

	a.deps.Hooks.OnRunComplete(ctx, RunInfo{
		SessionID:        a.SessionID(),
		UserMessage:      userMsg,
		Steps:            steps,
		Mode:             a.options.Mode,
		FinalContent:     finalContent,
		History:          historySnapshot,
		Delta:            a.deps.Delta,
		EvoEval:          evoEval,
		EvoRepairSuccess: evoSuccess,
		EvoRepairPlan:    evoPlan,
	})

	// ── Evo Mode: async evaluation (dual-process) ──
	// Runs AFTER hooks complete, in the same goroutine (already async from main loop).
	// Main loop has already released runMu → user can send new messages.
	if a.Mode().EvoEnabled && a.deps.EvoEvaluator != nil && a.traceCollector != nil {
		a.runEvoEval(ctx, userMsg)
	}
}

// runEvoEval performs async evo evaluation + repair after the main loop completes.
// Called from fireHooks goroutine — does NOT hold runMu.
// Uses independent context (not runCtx) to survive user's next message.
func (a *AgentLoop) runEvoEval(_ context.Context, userMsg string) {
	// Independent context: main loop is done, don't inherit cancellation
	evalCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Flush trace → persist to DB (includes full args, outputs, tokens, model, and user task)
	traceID, err := a.traceCollector.Flush(a.SessionID(), 0, userMsg)
	if err != nil {
		slog.Info(fmt.Sprintf("[evo] trace flush failed: %v", err))
		return
	}

	// 2. Read back flushed trace JSON from DB
	var traceJSON string
	if a.deps.EvoStore != nil && traceID > 0 {
		if trace, err := a.deps.EvoStore.GetTraceByID(traceID); err == nil {
			traceJSON = trace.Steps
		}
	}
	if traceJSON == "" || traceJSON == "[]" {
		slog.Info(fmt.Sprintf("[evo] skipping evaluation: no tool calls recorded (traceID=%d)", traceID))
		return
	}

	// 2.5 Filter: skip if trace only has meta-tools (no substantive work)
	metaTools := map[string]bool{"task_boundary": true, "notify_user": true, "task_plan": true}
	effectiveSteps := countEffectiveSteps(traceJSON, metaTools)
	if effectiveSteps < 2 {
		slog.Info(fmt.Sprintf("[evo] skipping evaluation: only %d effective tool calls (traceID=%d)", effectiveSteps, traceID))
		return
	}

	// 3. Build evaluation context from previous rounds
	var evoCtx *EvalContext
	a.mu.Lock()
	lastEval := a.evoLastEval
	a.mu.Unlock()
	if lastEval != nil && !lastEval.Passed {
		var failures strings.Builder
		fmt.Fprintf(&failures, "Previous score: %.1f, error_type: %s\n", lastEval.Score, lastEval.ErrorType)
		for _, issue := range lastEval.Issues {
			fmt.Fprintf(&failures, "- [%s] %s\n", issue.Severity, issue.Description)
		}
		evoCtx = &EvalContext{
			PreviousFailures: failures.String(),
			PreviousEval:     lastEval,
		}
	}

	// 4. Evaluate — push status via WS (SSE handler already exited after OnComplete)
	a.pushEvo("evo_eval", map[string]any{"type": "evo_eval", "text": "evaluating...", "session_id": a.SessionID()})
	evalResult, err := a.deps.EvoEvaluator.Evaluate(
		evalCtx, a.SessionID(), 0, userMsg, traceJSON, "", evoCtx,
	)
	if err != nil {
		slog.Info(fmt.Sprintf("[evo] evaluation failed: %v", err))
		return
	}

	// 4. Decide: repair needed?
	// Repair triggers when:
	//   (a) score < threshold (evalResult.Passed == false), OR
	//   (b) score >= threshold but has actionable issues (severity != "info")
	needsRepair := !evalResult.Passed
	if evalResult.Passed && len(evalResult.Issues) > 0 {
		for _, issue := range evalResult.Issues {
			if issue.Severity != "info" {
				needsRepair = true
				break
			}
		}
	}

	if !needsRepair {
		slog.Info(fmt.Sprintf("[evo] evaluation passed: score=%.2f issues=%d (traceID=%d)", evalResult.Score, len(evalResult.Issues), traceID))
		a.pushEvo("evo_eval", map[string]any{"type": "evo_eval", "text": fmt.Sprintf("passed (score=%.1f)", evalResult.Score), "session_id": a.SessionID()})
		return
	}

	// 5. Route repair
	slog.Info(fmt.Sprintf("[evo] needs repair: score=%.2f issues=%v", evalResult.Score, evalResult.Issues))
	if a.deps.EvoRepairRouter == nil {
		return
	}

	canRepair, reason := a.deps.EvoRepairRouter.CanRepair(a.SessionID())
	if !canRepair {
		slog.Info(fmt.Sprintf("[evo] circuit breaker tripped: %s", reason))
		a.pushEvo("evo_repair", map[string]any{"type": "evo_repair", "text": "circuit breaker: " + reason, "session_id": a.SessionID()})
		return
	}

	plan := a.deps.EvoRepairRouter.Route(evalResult)
	slog.Info(fmt.Sprintf("[evo] repair: strategy=%s desc=%s", plan.Strategy, plan.Description))
	a.pushEvo("evo_repair", map[string]any{"type": "evo_repair", "text": plan.Description, "session_id": a.SessionID()})

	// 6. Store evo context for next round's hooks
	a.mu.Lock()
	a.evoLastEval = evalResult
	a.evoLastPlan = &plan
	a.mu.Unlock()

	// 7. Inject repair instructions + re-run (acquires runMu)
	// Signal frontend: new round starting via WS push
	a.pushEvo("auto_wake_start", map[string]string{"type": "auto_wake_start", "session_id": a.SessionID()})
	a.InjectEphemeral(plan.Ephemeral)
	repairCtx, repairCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer repairCancel()
	if err := a.Run(repairCtx, ""); err != nil {
		slog.Info(fmt.Sprintf("[evo] repair re-run failed: %v", err))
	}
}

// pushEvo sends an evo event via WS push (survives SSE handler exit).
func (a *AgentLoop) pushEvo(eventType string, data any) {
	if a.deps.EventPusher != nil {
		a.deps.EventPusher(a.SessionID(), eventType, data)
	}
}
