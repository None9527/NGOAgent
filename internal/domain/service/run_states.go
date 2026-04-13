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

func (a *AgentLoop) guardCheck(response string, toolCalls int, step int) GuardVerdict {
	return a.guard.Check(response, toolCalls, step)
}

func (a *AgentLoop) recordFinalResponse(content string) {
	if a.traceCollector != nil && content != "" {
		a.traceCollector.RecordFinalResponse(content)
	}
}

func (a *AgentLoop) selfReviewEnabled() bool {
	return a.Mode().SelfReview
}

func (a *AgentLoop) appendMessage(msg llm.Message) {
	a.AppendMessage(msg)
}

func (a *AgentLoop) incrementOutputContinuation() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outputContinuations++
	return a.outputContinuations
}

func (a *AgentLoop) resetOutputContinuations() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outputContinuations = 0
}

func (a *AgentLoop) emitText(text string) {
	if a.deps.Delta != nil && text != "" {
		a.deps.Delta.OnText(text)
	}
}

func (a *AgentLoop) emitError(err error) {
	if a.deps.Delta != nil && err != nil {
		a.deps.Delta.OnError(err)
	}
}

// handleError handles all LLM error variants with retry/failover/fatal logic.
func (s generateNodeService) handleError(ctx context.Context, rs *runState, err error) (graphruntime.NodeResult, error) {
	llmErr, ok := err.(*llm.LLMError)
	if !ok {
		s.runtime.emitError(err)
		return s.runtime.finishWith(StateError, graphruntime.NodeStatusFatal), err
	}

	switch llmErr.Level {
	case llm.ErrorTransient, llm.ErrorOverload:
		// P0-A #4: background tasks skip retries
		if llmErr.IsBackground {
			slog.Info(fmt.Sprintf("[retry] background task %s — skipping retry", llmErr.Level))
			s.runtime.emitError(err)
			return s.runtime.finishWith(StateError, graphruntime.NodeStatusFatal), err
		}
		base, maxR := llm.BackoffConfig(llmErr.Level)
		if rs.retryCount() < maxR {
			nextRetry := rs.incRetry()
			backoff := llm.BackoffWithJitter(base, nextRetry-1)
			slog.Info(fmt.Sprintf("[retry] %s attempt %d/%d, backoff %v: %s",
				llmErr.Level, nextRetry, maxR, backoff, llmErr.Code))
			s.runtime.emitText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
			select {
			case <-ctx.Done():
				return s.runtime.finishWith(StateError, graphruntime.NodeStatusFatal), ctx.Err()
			case <-time.After(backoff):
			}
			return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
		// Exhausted retries → failover to next provider
		if rs.lastProvider() != "" {
			slog.Info(fmt.Sprintf("[failover] exhausted %d max retries for %s on %s, marking as forced-failover",
				maxR, llmErr.Level, rs.lastProvider()))
			rs.addExcludedProvider(rs.lastProvider())
			rs.setRetryCount(0)
			return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
		}

	case llm.ErrorContextOverflow:
		// P1 #38: compact first, then forceTruncate
		if rs.retryCount() < 2 {
			nextRetry := rs.incRetry()
			if nextRetry == 1 {
				slog.Info("[retry] context overflow → compacting then retry")
				rs.setMaxTokens(rs.maxTokens() / 2)
				return s.runtime.transitionTo(StateCompact, graphRouteCompact), nil
			} else {
				slog.Info("[retry] context overflow after compact → forceTruncate(6)")
				s.runtime.forceTruncate(6)
				s.runtime.emitText("\n\n[Context too large — force-truncated to last 6 messages]\n")
				return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
			}
		}

	case llm.ErrorBilling:
		slog.Info(fmt.Sprintf("[error] billing/quota exhausted: %s", llmErr.Message))
		s.runtime.emitText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		s.runtime.emitError(err)
		return s.runtime.finishWith(StateFatal, graphruntime.NodeStatusFatal), err

	case llm.ErrorFatal:
		s.runtime.emitText(fmt.Sprintf("\n\n[%s]\n", llmErr.Level.UserMessage()))
		s.runtime.emitError(err)
		return s.runtime.finishWith(StateFatal, graphruntime.NodeStatusFatal), err
	}

	s.runtime.emitError(err)
	return s.runtime.finishWith(StateError, graphruntime.NodeStatusFatal), err
}

// ───────────────────────────────────────────
// StateToolExec handler (was 38 lines inline)
// ───────────────────────────────────────────

func (a *AgentLoop) handleToolExec(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	return toolExecNodeService{runtime: a}.Execute(ctx, rs)
}

func (a *AgentLoop) lastHistoryMessage() llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.history) == 0 {
		return llm.Message{}
	}
	return a.history[len(a.history)-1]
}

func (a *AgentLoop) consumeYieldRequested() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	shouldStop := a.task.YieldRequested
	a.task.YieldRequested = false
	return shouldStop
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

func (a *AgentLoop) snapshotPendingFileEdits(step int) {
	if a.deps.FileHistory != nil && a.deps.FileHistory.HasPendingEdits() {
		msgID := fmt.Sprintf("%s_step%d", a.SessionID(), step)
		a.deps.FileHistory.Snapshot(msgID)
	}
}

func (a *AgentLoop) hasPendingWake() bool {
	return a.pendingWake.Load()
}

func (a *AgentLoop) consumePendingWake() bool {
	return a.pendingWake.Swap(false)
}

func (a *AgentLoop) emitAutoWakeStart() {
	if a.deps.Delta != nil {
		a.deps.Delta.OnAutoWakeStart()
	}
}

func (a *AgentLoop) appendEmptyUserMessage() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append(a.history, a.buildUserMessage(""))
}

func (a *AgentLoop) planReviewEmitter() planReviewEmitter {
	return a.deps.Delta
}

func (a *AgentLoop) emitComplete() {
	if a.deps.Delta != nil {
		a.deps.Delta.OnComplete()
	}
}

func (a *AgentLoop) markDreamIdle() {
	a.dream.OnIdle()
}
