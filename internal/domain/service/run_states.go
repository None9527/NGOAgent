// Package service — node-level execution handlers for the graph-backed agent loop.
//
// These methods retain the existing prepare/generate/tool/guard/compact/done
// node behaviors, but they are now invoked exclusively through the graph
// runtime adapter instead of a standalone loop state machine.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// runState holds per-run mutable state shared across graph node handlers.
type runState struct {
	opts RunOptions
	exec *graphruntime.ExecutionState
}

func (rs *runState) execution() *graphruntime.ExecutionState {
	if rs == nil {
		return nil
	}
	if rs.exec == nil {
		rs.exec = &graphruntime.ExecutionState{}
		if rs.opts.MaxTokens > 0 {
			rs.exec.MaxTokens = rs.opts.MaxTokens
		}
	}
	return rs.exec
}

func (rs *runState) stepCount() int {
	if exec := rs.execution(); exec != nil {
		return exec.TurnSteps
	}
	return 0
}

func (rs *runState) setStepCount(v int) {
	if exec := rs.execution(); exec != nil {
		exec.TurnSteps = v
	}
}

func (rs *runState) incStep() int {
	next := rs.stepCount() + 1
	rs.setStepCount(next)
	return next
}

func (rs *runState) maxTokens() int {
	if exec := rs.execution(); exec != nil {
		if exec.MaxTokens == 0 {
			exec.MaxTokens = rs.opts.MaxTokens
		}
		return exec.MaxTokens
	}
	return 0
}

func (rs *runState) setMaxTokens(v int) {
	rs.opts.MaxTokens = v
	if exec := rs.execution(); exec != nil {
		exec.MaxTokens = v
	}
}

func (rs *runState) excludedProviderList() []string {
	if exec := rs.execution(); exec != nil {
		return append([]string(nil), exec.ExcludedProviders...)
	}
	return nil
}

func (rs *runState) addExcludedProvider(v string) {
	if exec := rs.execution(); exec != nil {
		exec.ExcludedProviders = append(exec.ExcludedProviders, v)
	}
}

func (rs *runState) retryCount() int {
	if exec := rs.execution(); exec != nil {
		return exec.Retry.Count
	}
	return 0
}

func (rs *runState) setRetryCount(v int) {
	if exec := rs.execution(); exec != nil {
		exec.Retry.Count = v
	}
}

func (rs *runState) incRetry() int {
	next := rs.retryCount() + 1
	rs.setRetryCount(next)
	return next
}

func (rs *runState) lastProvider() string {
	if exec := rs.execution(); exec != nil {
		return exec.Retry.LastProvider
	}
	return ""
}

func (rs *runState) setLastProvider(v string) {
	if exec := rs.execution(); exec != nil {
		exec.Retry.LastProvider = v
	}
}

// ───────────────────────────────────────────
// StateGenerate handler (was 133 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleGenerate(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	// P0-D #3: Microcompact — clear old digested tool results before LLM call
	a.microCompact()

	opts := rs.opts
	opts.MaxTokens = rs.maxTokens()
	resp, provName, err := a.doGenerate(ctx, opts, rs.excludedProviderList())
	rs.setLastProvider(provName)

	if err != nil {
		return a.handleGenerateError(ctx, rs, err)
	}
	rs.setRetryCount(0)

	// P1 #26: Max Output Recovery — auto-continue when LLM truncates output
	if resp.StopReason == "length" && len(resp.ToolCalls) == 0 {
		var cont int
		func() { a.mu.Lock(); defer a.mu.Unlock(); a.outputContinuations++; cont = a.outputContinuations }()

		if cont <= 3 {
			slog.Info(fmt.Sprintf("[max-output] continuation %d/3 — output truncated, auto-resuming", cont))
			a.AppendMessage(llm.Message{
				Role:      "assistant",
				Content:   resp.Content,
				Reasoning: resp.Reasoning,
			})
			a.AppendMessage(llm.Message{
				Role:    "user",
				Content: "Your previous output was truncated due to length. Continue EXACTLY from where you left off. Do NOT repeat any content. Do NOT add preamble.",
			})
			return a.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
		slog.Info(fmt.Sprintf("[max-output] exceeded 3 continuations, stopping"))
		a.deps.Delta.OnText("\n\n[Output continuation limit reached (3/3)]\n")
	}
	// Reset continuation counter on non-truncated output
	if resp.StopReason != "length" {
		func() { a.mu.Lock(); defer a.mu.Unlock(); a.outputContinuations = 0 }()
	}

	a.AppendMessage(llm.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Reasoning: resp.Reasoning,
	})

	// BehaviorGuard check
	verdict := a.guard.Check(resp.Content, len(resp.ToolCalls), rs.stepCount())
	switch verdict.Action {
	case "terminate":
		a.deps.Delta.OnText("\n\n[" + verdict.Message + "]")
		return a.transitionTo(StateDone, graphRouteDone), nil
	case "warn":
		a.InjectEphemeral(verdict.Message)
	}

	if len(resp.ToolCalls) == 0 {
		if a.traceCollector != nil && resp.Content != "" {
			a.traceCollector.RecordFinalResponse(resp.Content)
		}
		if a.Mode().SelfReview && resp.Content != "" {
			return graphruntime.NodeResult{
				RouteKey:         graphRouteReflect,
				ObservedState:    "reflect",
				OutputSchemaName: graphReflectionSchema,
			}, nil
		}
		return a.transitionTo(StateDone, graphRouteDone), nil
	}
	return a.transitionTo(StateToolExec, graphRouteToolExec), nil
}

// handleGenerateError handles all LLM error variants with retry/failover/fatal logic.
func (a *AgentLoop) handleGenerateError(ctx context.Context, rs *runState, err error) (graphruntime.NodeResult, error) {
	llmErr, ok := err.(*llm.LLMError)
	if !ok {
		a.deps.Delta.OnError(err)
		return a.finishWith(StateError, graphruntime.NodeStatusFatal), err
	}

	switch llmErr.Level {
	case llm.ErrorTransient, llm.ErrorOverload:
		// P0-A #4: background tasks skip retries
		if llmErr.IsBackground {
			slog.Info(fmt.Sprintf("[retry] background task %s — skipping retry", llmErr.Level))
			a.deps.Delta.OnError(err)
			return a.finishWith(StateError, graphruntime.NodeStatusFatal), err
		}
		base, maxR := llm.BackoffConfig(llmErr.Level)
		if rs.retryCount() < maxR {
			nextRetry := rs.incRetry()
			backoff := llm.BackoffWithJitter(base, nextRetry-1)
			slog.Info(fmt.Sprintf("[retry] %s attempt %d/%d, backoff %v: %s",
				llmErr.Level, nextRetry, maxR, backoff, llmErr.Code))
			a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
			select {
			case <-ctx.Done():
				return a.finishWith(StateError, graphruntime.NodeStatusFatal), ctx.Err()
			case <-time.After(backoff):
			}
			return a.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
		// Exhausted retries → failover to next provider
		if rs.lastProvider() != "" {
			slog.Info(fmt.Sprintf("[failover] exhausted %d max retries for %s on %s, marking as forced-failover",
				maxR, llmErr.Level, rs.lastProvider()))
			rs.addExcludedProvider(rs.lastProvider())
			rs.setRetryCount(0)
			return a.transitionTo(StateGenerate, graphRouteGenerate), nil
		}

	case llm.ErrorContextOverflow:
		// P1 #38: compact first, then forceTruncate
		if rs.retryCount() < 2 {
			nextRetry := rs.incRetry()
			if nextRetry == 1 {
				slog.Info("[retry] context overflow → compacting then retry")
				rs.setMaxTokens(rs.maxTokens() / 2)
				return a.transitionTo(StateCompact, graphRouteCompact), nil
			} else {
				slog.Info("[retry] context overflow after compact → forceTruncate(6)")
				a.forceTruncate(6)
				a.deps.Delta.OnText("\n\n[Context too large — force-truncated to last 6 messages]\n")
				return a.transitionTo(StateGenerate, graphRouteGenerate), nil
			}
		}

	case llm.ErrorBilling:
		slog.Info(fmt.Sprintf("[error] billing/quota exhausted: %s", llmErr.Message))
		a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		a.deps.Delta.OnError(err)
		return a.finishWith(StateFatal, graphruntime.NodeStatusFatal), err

	case llm.ErrorFatal:
		a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		a.deps.Delta.OnError(err)
		return a.finishWith(StateFatal, graphruntime.NodeStatusFatal), err
	}

	a.deps.Delta.OnError(err)
	return a.finishWith(StateError, graphruntime.NodeStatusFatal), err
}

// ───────────────────────────────────────────
// StateToolExec handler (was 38 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleToolExec(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	var lastMsg llm.Message
	func() { a.mu.Lock(); defer a.mu.Unlock(); lastMsg = a.history[len(a.history)-1] }()

	// P2: Mixed batch splitting
	if len(lastMsg.ToolCalls) > 1 {
		readOnly, write := splitToolCalls(lastMsg.ToolCalls)
		if len(readOnly) > 0 && len(write) == 0 {
			a.execToolsConcurrent(ctx, lastMsg.ToolCalls)
		} else if len(readOnly) > 1 && len(write) > 0 {
			a.execToolsConcurrent(ctx, readOnly)
			if a.execToolsSerial(ctx, write) {
				return a.transitionTo(StateDone, graphRouteDone), nil
			}
		} else {
			if a.execToolsSerial(ctx, lastMsg.ToolCalls) {
				return a.transitionTo(StateDone, graphRouteDone), nil
			}
		}
	} else {
		if a.execToolsSerial(ctx, lastMsg.ToolCalls) {
			return a.transitionTo(StateDone, graphRouteDone), nil
		}
	}

	// Check yield signal from terminal tools
	var shouldStop bool
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		shouldStop = a.task.YieldRequested
		a.task.YieldRequested = false
	}()
	if shouldStop {
		if a.shouldRouteSpawn(lastMsg.ToolCalls) {
			return graphruntime.NodeResult{
				RouteKey:      graphRouteSpawn,
				ObservedState: "spawn",
			}, nil
		}
		return a.transitionTo(StateDone, graphRouteDone), nil
	}

	return a.transitionTo(StateGuardCheck, graphRouteGuardCheck), nil
}

func (a *AgentLoop) shouldRouteSpawn(calls []llm.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	barrier := a.activeBarrierSnapshot()
	if barrier == nil || barrier.PendingCount == 0 {
		return false
	}
	for _, tc := range calls {
		if tc.Function.Name == "spawn_agent" || tc.Function.Name == "skill" {
			return true
		}
	}
	return false
}

// ───────────────────────────────────────────
// StateGuardCheck handler (was 21 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleGuardCheck(rs *runState) (graphruntime.NodeResult, error) {
	rs.incStep()

	tokenEstimate := a.estimateTokens()
	policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
	usage := float64(tokenEstimate) / float64(policy.ContextWindow)

	// Three-level context defense
	if usage > 0.95 {
		a.forceTruncate(8)
		a.InjectEphemeral(prompttext.EphCompactionNotice)
		return a.transitionTo(StateGenerate, graphRouteGenerate), nil
	} else if usage > 0.70 {
		return a.transitionTo(StateCompact, graphRouteCompact), nil
	} else {
		return a.transitionTo(StateGenerate, graphRouteGenerate), nil
	}
}

// ───────────────────────────────────────────
// StateCompact handler (was 18 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleCompact(ctx context.Context) (graphruntime.NodeResult, error) {
	// P1-A #24: Try tool-heavy compression first
	if a.toolHeavyCompact() {
		newEst := a.estimateTokens()
		newPolicy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
		newUsage := float64(newEst) / float64(newPolicy.ContextWindow)
		if newUsage <= 0.70 {
			a.InjectEphemeral(prompttext.EphCompactionNotice)
			return a.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
	}
	a.doCompact(ctx)
	a.InjectEphemeral(prompttext.EphCompactionNotice)
	a.persistFullHistory()
	return a.transitionTo(StateGenerate, graphRouteGenerate), nil
}

// ───────────────────────────────────────────
// StateDone handler (was 33 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleDone(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	// Snapshot file edit history for this message turn
	if a.deps.FileHistory != nil && a.deps.FileHistory.HasPendingEdits() {
		msgID := fmt.Sprintf("%s_step%d", a.SessionID(), rs.stepCount())
		a.deps.FileHistory.Snapshot(msgID)
	}
	a.persistHistory()

	// Pending Wake tail-check (subagent orchestration)
	if a.pendingWake.Load() {
		slog.Info(fmt.Sprintf("[loop] pendingWake detected, routing through merge node"))
		return graphruntime.NodeResult{
			RouteKey:      graphRouteMerge,
			ObservedState: "merge",
		}, nil
	}

	if a.shouldRunEvoEvaluation() {
		return graphruntime.NodeResult{
			RouteKey:      graphRouteEvaluate,
			ObservedState: "evaluate",
		}, nil
	}
	return a.transitionTo(StateIdle, graphRouteComplete), nil
}

func (a *AgentLoop) handleEvaluate(_ context.Context, _ *runState) (graphruntime.NodeResult, error) {
	evalCtx, cancel := a.evaluationContextTimeout()
	defer cancel()

	previous := a.intelligenceSnapshot().Evaluation
	shouldRepair, err := a.evaluateCurrentRun(evalCtx, previous)
	if err != nil {
		a.logInlineEvaluationFailure(err)
		return graphruntime.NodeResult{
			RouteKey:      graphRouteComplete,
			ObservedState: "evaluate",
		}, nil
	}
	if shouldRepair {
		return graphruntime.NodeResult{
			RouteKey:         graphRouteRepair,
			ObservedState:    "evaluate",
			OutputSchemaName: graphEvaluationSchema,
		}, nil
	}
	return graphruntime.NodeResult{
		RouteKey:         graphRouteComplete,
		ObservedState:    "evaluate",
		OutputSchemaName: graphEvaluationSchema,
	}, nil
}

func (a *AgentLoop) handleRepair(_ context.Context, rs *runState) (graphruntime.NodeResult, error) {
	result := a.continueWithRepair(rs)
	result.ObservedState = "repair"
	return result, nil
}

func (a *AgentLoop) handleComplete(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	// Completion side effects belong to the terminal complete node only.
	if a.deps.Delta != nil {
		a.deps.Delta.OnComplete()
	}
	go a.fireHooks(ctx, rs.stepCount())
	// P3 I2: Session is idle — start background pre-indexing.
	a.dream.OnIdle()
	return a.finishWith(StateIdle, graphruntime.NodeStatusComplete), nil
}
