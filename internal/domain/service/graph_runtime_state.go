package service

import (
	"encoding/json"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

func (a *graphLoopAdapter) syncToGraphState(state *graphruntime.TurnState, exec *graphruntime.ExecutionState) {
	if state == nil {
		return
	}

	a.loop.mu.Lock()
	defer a.loop.mu.Unlock()

	state.Mode = a.loop.options.Mode
	state.Ephemerals = append([]string(nil), a.loop.ephemerals...)
	state.CurrentPlan = a.loop.task.BoundarySummary
	state.Task = graphruntime.TaskState{
		Name:             a.loop.task.BoundaryTaskName,
		Status:           a.loop.task.BoundaryStatus,
		Summary:          a.loop.task.BoundarySummary,
		YieldRequested:   a.loop.task.YieldRequested,
		BoundaryTaskName: a.loop.task.BoundaryTaskName,
		BoundaryStatus:   a.loop.task.BoundaryStatus,
		BoundarySummary:  a.loop.task.BoundarySummary,
		StepsSinceUpdate: a.loop.task.StepsSinceUpdate,
	}
	state.Compact = graphruntime.CompactState{
		CompactCount:        a.loop.compactCount,
		OutputContinuations: a.loop.outputContinuations,
		HistoryDirty:        a.loop.historyDirty,
	}
	state.Reflection = graphruntime.ReflectionState{
		Required: a.loop.mode.SelfReview,
	}

	if len(a.loop.history) > 0 {
		last := a.loop.history[len(a.loop.history)-1]
		state.LastLLMResponse = graphruntime.LLMResponseState{
			Content:    last.Content,
			Reasoning:  last.Reasoning,
			StopReason: "",
			Provider:   a.rs.lastProvName,
		}
		state.Attachments = attachmentPaths(last.Attachments)
		state.ToolCalls = mapToolCalls(last.ToolCalls)
	}

	if exec != nil {
		exec.PendingWake = a.loop.pendingWake.Load()
		exec.Continuation = graphruntime.ContinuationState{Count: a.loop.outputContinuations}
		exec.Retry = graphruntime.RetryState{
			Count:        a.rs.retries,
			LastProvider: a.rs.lastProvName,
		}
		if a.loop.deps.Security != nil {
			pending := a.loop.deps.Security.ListPendingApprovals()
			if len(pending) > 0 {
				exec.PendingApproval = &graphruntime.ApprovalState{
					ID:          pending[0].ID,
					ToolName:    pending[0].ToolName,
					Reason:      pending[0].Reason,
					RequestedAt: pending[0].Requested,
				}
			} else {
				exec.PendingApproval = nil
			}
		}
		if a.loop.barrier != nil {
			barrier := a.loop.barrier.Snapshot()
			exec.PendingBarrier = &barrier
		} else {
			exec.PendingBarrier = nil
		}
	}
}

func (a *graphLoopAdapter) syncFromGraphState(state *graphruntime.TurnState, exec *graphruntime.ExecutionState) {
	if state == nil {
		return
	}

	a.loop.mu.Lock()
	defer a.loop.mu.Unlock()

	a.loop.options.Mode = state.Mode
	a.loop.ephemerals = append([]string(nil), state.Ephemerals...)
	a.loop.task.BoundaryTaskName = state.Task.BoundaryTaskName
	a.loop.task.BoundaryStatus = state.Task.BoundaryStatus
	a.loop.task.BoundarySummary = state.Task.BoundarySummary
	a.loop.task.YieldRequested = state.Task.YieldRequested
	a.loop.task.StepsSinceUpdate = state.Task.StepsSinceUpdate
	a.loop.compactCount = state.Compact.CompactCount
	a.loop.outputContinuations = state.Compact.OutputContinuations
	a.loop.historyDirty = state.Compact.HistoryDirty

	a.rs.opts.Mode = state.Mode
	if exec != nil {
		a.rs.retries = exec.Retry.Count
		a.rs.lastProvName = exec.Retry.LastProvider
		if exec.PendingWake {
			a.loop.pendingWake.Store(true)
		}
	}
}

func attachmentPaths(atts []llm.Attachment) []string {
	paths := make([]string, 0, len(atts))
	for _, att := range atts {
		paths = append(paths, att.Path)
	}
	return paths
}

func mapToolCalls(calls []llm.ToolCall) []graphruntime.ToolCallState {
	out := make([]graphruntime.ToolCallState, 0, len(calls))
	for _, call := range calls {
		var args map[string]any
		if call.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		out = append(out, graphruntime.ToolCallState{
			ID:   call.ID,
			Name: call.Function.Name,
			Args: args,
		})
	}
	return out
}
