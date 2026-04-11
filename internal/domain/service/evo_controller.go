// Package service — Evo evaluation orchestration integrated into the graph flow.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// fireHooks invokes PostRunHooks asynchronously after the graph reaches terminal completion.
func (a *AgentLoop) fireHooks(ctx context.Context, steps int) {
	if a.deps.Hooks == nil {
		return
	}
	a.mu.Lock()
	finalContent := ""
	userMsg := a.latestMeaningfulUserMessageLocked()
	if len(a.history) > 0 {
		finalContent = a.history[len(a.history)-1].Content
	}
	historySnapshot := make([]model.Message, len(a.history))
	copy(historySnapshot, a.history)
	a.mu.Unlock()

	intelligence := a.consumeIntelligenceSnapshot()
	a.deps.Hooks.OnRunComplete(ctx, RunInfo{
		SessionID:    a.SessionID(),
		UserMessage:  userMsg,
		Steps:        steps,
		Mode:         a.options.Mode,
		FinalContent: finalContent,
		History:      historySnapshot,
		Delta:        a.deps.Delta,
		Intelligence: intelligence,
	})
}

func (a *AgentLoop) latestMeaningfulUserMessage() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.latestMeaningfulUserMessageLocked()
}

func (a *AgentLoop) latestMeaningfulUserMessageLocked() string {
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "user" && a.history[i].Content != "" {
			return a.history[i].Content
		}
	}
	return ""
}

func (a *AgentLoop) shouldRunEvoEvaluation() bool {
	return a.Mode().EvoEnabled && a.deps.EvoEvaluator != nil && a.traceCollector != nil
}

func (a *AgentLoop) evaluateCurrentRun(ctx context.Context, previous graphruntime.EvaluationState) (bool, error) {
	if !a.shouldRunEvoEvaluation() {
		return false, nil
	}

	userMsg := a.latestMeaningfulUserMessage()
	evalState, repairState, actionableRepair, err := a.runInlineEvaluation(ctx, userMsg, previous)
	if evalState.Valid {
		a.setEvaluationDecision(evalState)
	}
	a.setRepairDecision(repairState)
	if err != nil {
		return false, err
	}
	return actionableRepair, nil
}

func (a *AgentLoop) runInlineEvaluation(ctx context.Context, userMsg string, previous graphruntime.EvaluationState) (graphruntime.EvaluationState, graphruntime.RepairState, bool, error) {
	traceID, traceJSON, shouldEvaluate, err := a.flushTraceForEvaluation(userMsg)
	if err != nil {
		return graphruntime.EvaluationState{}, graphruntime.RepairState{}, false, err
	}
	if !shouldEvaluate {
		return graphruntime.EvaluationState{}, a.intelligenceSnapshot().Repair, false, nil
	}

	var evoCtx *EvalContext
	if previous.Valid && !previous.Passed {
		var failures strings.Builder
		fmt.Fprintf(&failures, "Previous score: %.1f, error_type: %s\n", previous.Score, previous.ErrorType)
		for _, issue := range previous.Issues {
			fmt.Fprintf(&failures, "- [%s] %s\n", issue.Severity, issue.Description)
		}
		evoCtx = &EvalContext{
			PreviousFailures: failures.String(),
			PreviousEval:     &previous,
		}
	}

	a.pushEvo("evo_eval", map[string]any{"type": "evo_eval", "text": "evaluating...", "session_id": a.SessionID()})
	evalResult, err := a.deps.EvoEvaluator.Evaluate(ctx, a.SessionID(), traceID, userMsg, traceJSON, "", evoCtx)
	if err != nil {
		return graphruntime.EvaluationState{}, graphruntime.RepairState{}, false, err
	}

	current := a.intelligenceSnapshot()
	needsRepair := evaluationNeedsRepair(*evalResult)
	if !needsRepair {
		repair := graphruntime.RepairState{}
		if current.Repair.Attempted && current.Repair.Strategy != "" {
			repair = current.Repair
			repair.Success = true
		}
		a.pushEvo("evo_eval", map[string]any{"type": "evo_eval", "text": fmt.Sprintf("passed (score=%.1f)", evalResult.Score), "session_id": a.SessionID()})
		return *evalResult, repair, false, nil
	}

	if a.deps.EvoRepairRouter == nil {
		return *evalResult, graphruntime.RepairState{}, false, nil
	}

	canRepair, reason := a.deps.EvoRepairRouter.CanRepair(a.SessionID())
	if !canRepair {
		repair := graphruntime.RepairState{
			Allowed:     false,
			BlockReason: reason,
			Description: "circuit breaker: " + reason,
		}
		a.pushEvo("evo_repair", map[string]any{"type": "evo_repair", "text": repair.Description, "session_id": a.SessionID()})
		return *evalResult, repair, false, nil
	}

	plan := a.deps.EvoRepairRouter.Route(evalResult)
	a.pushEvo("evo_repair", map[string]any{"type": "evo_repair", "text": plan.Description, "session_id": a.SessionID()})
	return *evalResult, plan, true, nil
}

func (a *AgentLoop) flushTraceForEvaluation(userMsg string) (uint, string, bool, error) {
	if a.traceCollector == nil {
		return 0, "", false, nil
	}

	traceID, err := a.traceCollector.Flush(a.SessionID(), 0, userMsg)
	if err != nil {
		return 0, "", false, fmt.Errorf("trace flush failed: %w", err)
	}

	var traceJSON string
	if a.deps.EvoStore != nil && traceID > 0 {
		if trace, err := a.deps.EvoStore.GetTraceByID(traceID); err == nil {
			traceJSON = trace.Steps
		}
	}
	if traceJSON == "" || traceJSON == "[]" {
		return traceID, "", false, nil
	}

	metaTools := map[string]bool{"task_boundary": true, "notify_user": true, "task_plan": true}
	if countEffectiveSteps(traceJSON, metaTools) < 2 {
		return traceID, "", false, nil
	}
	return traceID, traceJSON, true, nil
}

func evaluationNeedsRepair(eval graphruntime.EvaluationState) bool {
	if !eval.Passed {
		return true
	}
	for _, issue := range eval.Issues {
		if issue.Severity != "info" {
			return true
		}
	}
	return false
}

func (a *AgentLoop) continueWithRepair(rs *runState) graphruntime.NodeResult {
	intelligence := a.intelligenceSnapshot()
	repair := intelligence.Repair
	if !repair.Allowed || repair.Strategy == "" {
		return a.transitionTo(StateIdle, graphRouteComplete)
	}

	repair.Attempted = true
	a.setRepairDecision(repair)
	if a.deps.EvoRepairRouter != nil {
		_ = a.deps.EvoRepairRouter.RecordRepair(a.SessionID(), 0, repair, false, 0, 0, 0)
	}
	a.pushEvo("auto_wake_start", map[string]string{"type": "auto_wake_start", "session_id": a.SessionID()})
	if a.deps.Delta != nil {
		a.deps.Delta.OnAutoWakeStart()
	}
	if repair.Ephemeral != "" {
		a.InjectEphemeral(repair.Ephemeral)
	}
	rs.setStepCount(0)
	rs.setRetryCount(0)
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.history = append(a.history, a.buildUserMessage(""))
	}()
	return a.transitionTo(StatePrepare, graphRoutePrepare)
}

// pushEvo sends an evo event via WS push.
func (a *AgentLoop) pushEvo(eventType string, data any) {
	if a.deps.EventPusher != nil {
		a.deps.EventPusher(a.SessionID(), eventType, data)
	}
}

func (a *AgentLoop) evaluationContextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

func (a *AgentLoop) repairContextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 120*time.Second)
}

func (a *AgentLoop) logInlineEvaluationFailure(err error) {
	if err != nil {
		slog.Info(fmt.Sprintf("[evo] inline evaluation failed: %v", err))
	}
}
