// Package service — node-level execution handlers for the graph-backed agent loop.
//
// These methods retain the existing prepare/generate/tool/guard/compact/done
// transition logic, but they are now invoked exclusively through the graph
// runtime adapter instead of a standalone FSM executor.
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
	opts              RunOptions
	steps             int
	retries           int
	excludedProviders []string
	lastProvName      string
}

// ───────────────────────────────────────────
// StateGenerate handler (was 133 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleGenerate(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	// P0-D #3: Microcompact — clear old digested tool results before LLM call
	a.microCompact()

	resp, provName, err := a.doGenerate(ctx, rs.opts, rs.excludedProviders)
	rs.lastProvName = provName

	if err != nil {
		return a.handleGenerateError(ctx, rs, err)
	}
	rs.retries = 0

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
			a.setPhase(StateGenerate)
			return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
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
	verdict := a.guard.Check(resp.Content, len(resp.ToolCalls), rs.steps)
	switch verdict.Action {
	case "terminate":
		a.setPhase(StateDone)
		a.deps.Delta.OnText("\n\n[" + verdict.Message + "]")
		return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
	case "warn":
		a.InjectEphemeral(verdict.Message)
	}

	if len(resp.ToolCalls) == 0 {
		if a.traceCollector != nil && resp.Content != "" {
			a.traceCollector.RecordFinalResponse(resp.Content)
		}
		a.setPhase(StateDone)
		return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
	}
	a.setPhase(StateToolExec)
	return graphruntime.NodeResult{RouteKey: graphRouteToolExec}, nil
}

// handleGenerateError handles all LLM error variants with retry/failover/fatal logic.
func (a *AgentLoop) handleGenerateError(ctx context.Context, rs *runState, err error) (graphruntime.NodeResult, error) {
	a.setPhase(StateError)

	llmErr, ok := err.(*llm.LLMError)
	if !ok {
		a.deps.Delta.OnError(err)
		return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, err
	}

	switch llmErr.Level {
	case llm.ErrorTransient, llm.ErrorOverload:
		// P0-A #4: background tasks skip retries
		if llmErr.IsBackground {
			slog.Info(fmt.Sprintf("[retry] background task %s — skipping retry", llmErr.Level))
			a.deps.Delta.OnError(err)
			return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, err
		}
		base, maxR := llm.BackoffConfig(llmErr.Level)
		if rs.retries < maxR {
			rs.retries++
			backoff := llm.BackoffWithJitter(base, rs.retries-1)
			slog.Info(fmt.Sprintf("[retry] %s attempt %d/%d, backoff %v: %s",
				llmErr.Level, rs.retries, maxR, backoff, llmErr.Code))
			a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
			select {
			case <-ctx.Done():
				return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, ctx.Err()
			case <-time.After(backoff):
			}
			a.setPhase(StateGenerate)
			return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
		}
		// Exhausted retries → failover to next provider
		if rs.lastProvName != "" {
			slog.Info(fmt.Sprintf("[failover] exhausted %d max retries for %s on %s, marking as forced-failover",
				maxR, llmErr.Level, rs.lastProvName))
			rs.excludedProviders = append(rs.excludedProviders, rs.lastProvName)
			rs.retries = 0
			a.setPhase(StateGenerate)
			return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
		}

	case llm.ErrorContextOverflow:
		// P1 #38: compact first, then forceTruncate
		if rs.retries < 2 {
			rs.retries++
			if rs.retries == 1 {
				slog.Info("[retry] context overflow → compacting then retry")
				rs.opts.MaxTokens = rs.opts.MaxTokens / 2
				a.setPhase(StateCompact)
				return graphruntime.NodeResult{RouteKey: graphRouteCompact}, nil
			} else {
				slog.Info("[retry] context overflow after compact → forceTruncate(6)")
				a.forceTruncate(6)
				a.deps.Delta.OnText("\n\n[Context too large — force-truncated to last 6 messages]\n")
				a.setPhase(StateGenerate)
				return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
			}
		}

	case llm.ErrorBilling:
		slog.Info(fmt.Sprintf("[error] billing/quota exhausted: %s", llmErr.Message))
		a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		a.setPhase(StateFatal)
		a.deps.Delta.OnError(err)
		return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, err

	case llm.ErrorFatal:
		a.deps.Delta.OnText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		a.setPhase(StateFatal)
		a.deps.Delta.OnError(err)
		return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, err
	}

	a.deps.Delta.OnError(err)
	return graphruntime.NodeResult{Status: graphruntime.NodeStatusFatal}, err
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
				return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
			}
		} else {
			if a.execToolsSerial(ctx, lastMsg.ToolCalls) {
				return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
			}
		}
	} else {
		if a.execToolsSerial(ctx, lastMsg.ToolCalls) {
			return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
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
		a.setPhase(StateDone)
		return graphruntime.NodeResult{RouteKey: graphRouteDone}, nil
	}

	a.setPhase(StateGuardCheck)
	return graphruntime.NodeResult{RouteKey: graphRouteGuardCheck}, nil
}

// ───────────────────────────────────────────
// StateGuardCheck handler (was 21 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleGuardCheck(rs *runState) (graphruntime.NodeResult, error) {
	rs.steps++

	tokenEstimate := a.estimateTokens()
	policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
	usage := float64(tokenEstimate) / float64(policy.ContextWindow)

	// Three-level context defense
	if usage > 0.95 {
		a.forceTruncate(8)
		a.InjectEphemeral(prompttext.EphCompactionNotice)
		a.setPhase(StateGenerate)
		return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
	} else if usage > 0.70 {
		a.setPhase(StateCompact)
		return graphruntime.NodeResult{RouteKey: graphRouteCompact}, nil
	} else {
		a.setPhase(StateGenerate)
		return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
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
			a.setPhase(StateGenerate)
			return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
		}
	}
	a.doCompact(ctx)
	a.InjectEphemeral(prompttext.EphCompactionNotice)
	a.persistFullHistory()
	a.setPhase(StateGenerate)
	return graphruntime.NodeResult{RouteKey: graphRouteGenerate}, nil
}

// ───────────────────────────────────────────
// StateDone handler (was 33 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleDone(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	// Snapshot file edit history for this message turn
	if a.deps.FileHistory != nil && a.deps.FileHistory.HasPendingEdits() {
		msgID := fmt.Sprintf("%s_step%d", a.SessionID(), rs.steps)
		a.deps.FileHistory.Snapshot(msgID)
	}
	a.state = StateIdle
	a.persistHistory()

	// OnComplete FIRST: release frontend immediately
	a.deps.Delta.OnComplete()
	// Hooks run async: must NOT block runInner return
	go a.fireHooks(ctx, rs.steps)

	// Pending Wake tail-check (subagent orchestration)
	if a.pendingWake.CompareAndSwap(true, false) {
		slog.Info(fmt.Sprintf("[loop] pendingWake detected, auto-continuing for subagent results"))
		rs.steps = 0
		rs.retries = 0
		if a.deps.Delta != nil {
			a.deps.Delta.OnAutoWakeStart()
		}
		func() { a.mu.Lock(); defer a.mu.Unlock(); a.history = append(a.history, a.buildUserMessage("")) }()
		a.setPhase(StatePrepare)
		return graphruntime.NodeResult{RouteKey: graphRoutePrepare}, nil
	}
	// P3 I2: Session is idle — start background pre-indexing
	a.dream.OnIdle()
	return graphruntime.NodeResult{Status: graphruntime.NodeStatusComplete}, nil
}
