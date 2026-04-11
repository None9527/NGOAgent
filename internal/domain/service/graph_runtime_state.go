package service

import (
	"encoding/json"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

func (a *graphLoopAdapter) syncToGraphState(state *graphruntime.TurnState, exec *graphruntime.ExecutionState) {
	if state == nil {
		return
	}

	currentStructured := state.StructuredOutput
	currentIntelligence := state.Intelligence
	a.loop.mu.Lock()
	defer a.loop.mu.Unlock()
	a.rs.exec = exec
	boundarySummary := a.loop.task.Summary

	state.Mode = a.rs.opts.Mode
	state.Ephemerals = append([]string(nil), a.loop.ephemerals...)
	state.PendingMedia = cloneMediaItems(a.loop.pendingMedia)
	state.Task = graphruntime.TaskState{
		YieldRequested:   a.loop.task.YieldRequested,
		Name:             a.loop.task.Name,
		Mode:             a.loop.task.Mode,
		Status:           a.loop.task.Status,
		Summary:          boundarySummary,
		StepsSinceUpdate: a.loop.task.StepsSinceUpdate,
		PlanModified:     a.loop.task.PlanModified,
		CurrentStep:      a.loop.task.CurrentStep,
		ArtifactLastStep: cloneIntMap(a.loop.task.ArtifactLastStep),
		SkillLoaded:      a.loop.task.SkillLoaded,
		SkillPath:        a.loop.task.SkillPath,
	}
	state.Compact = graphruntime.CompactState{
		CompactCount:        a.loop.compactCount,
		OutputContinuations: a.loop.outputContinuations,
		HistoryDirty:        a.loop.historyDirty,
	}
	state.Reflection = graphruntime.ReflectionState{
		LastReview: state.Reflection.LastReview,
		Required:   a.loop.mode.SelfReview,
	}
	state.Intelligence = cloneIntelligenceState(a.loop.intelligence)
	state.Orchestration = cloneOrchestrationState(a.loop.orchestration)
	state.LastLLMResponse = graphruntime.LLMResponseState{}
	state.Attachments = nil
	state.ToolCalls = nil
	state.ToolResults = nil
	state.OutputDraft = ""
	state.StructuredOutput = graphruntime.StructuredOutputState{}
	state.ForceNextTool = a.loop.guard.PeekForceToolName()
	state.ActiveSkills = cloneStringMap(a.loop.activeSkills)
	if a.loop.barrier != nil {
		barrier := a.loop.barrier.Snapshot()
		state.Orchestration.ActiveBarrier = &barrier
		state.Orchestration.PendingMerge = a.loop.pendingWake.Load()
		state.Orchestration.LastWakeSource = "barrier"
	} else if exec != nil && exec.PendingBarrier != nil {
		barrier := *exec.PendingBarrier
		barrier.Members = append([]graphruntime.BarrierMemberState(nil), exec.PendingBarrier.Members...)
		state.Orchestration.ActiveBarrier = &barrier
		state.Orchestration.PendingMerge = exec.PendingWake
		if state.Orchestration.LastWakeSource == "" && exec.PendingWake {
			state.Orchestration.LastWakeSource = "barrier"
		}
	}

	if len(a.loop.history) > 0 {
		assistantIdx := len(a.loop.history) - 1
		trailingToolStart := len(a.loop.history)
		for assistantIdx >= 0 && a.loop.history[assistantIdx].Role == "tool" {
			trailingToolStart = assistantIdx
			assistantIdx--
		}
		if assistantIdx >= 0 && a.loop.history[assistantIdx].Role == "assistant" {
			last := a.loop.history[assistantIdx]
			state.LastLLMResponse = graphruntime.LLMResponseState{
				Content:    last.Content,
				Reasoning:  last.Reasoning,
				StopReason: "",
				Provider:   a.rs.lastProvider(),
			}
			state.Attachments = attachmentPaths(last.Attachments)
			state.ToolCalls = mapToolCalls(last.ToolCalls)
			state.OutputDraft = last.Content
			state.StructuredOutput = graphruntime.StructuredOutputState{
				SchemaName: execOutputSchemaName(exec),
				RawJSON:    structuredOutputRaw(execOutputSchemaName(exec), last.Content),
				Valid:      structuredOutputRaw(execOutputSchemaName(exec), last.Content) != "",
			}
			if trailingToolStart < len(a.loop.history) {
				state.ToolResults = mapToolResults(a.loop.history[trailingToolStart:], last.ToolCalls)
			}
		}
	}
	if state.StructuredOutput == (graphruntime.StructuredOutputState{}) && currentStructured.Valid {
		state.StructuredOutput = currentStructured
	}
	if intelligenceStateEmpty(state.Intelligence) && !intelligenceStateEmpty(currentIntelligence) {
		state.Intelligence = cloneIntelligenceState(currentIntelligence)
	}

	if exec != nil {
		exec.PendingWake = a.loop.pendingWake.Load()
		exec.Continuation = graphruntime.ContinuationState{Count: a.loop.outputContinuations}
		if a.loop.deps.Security != nil {
			if pending := latestApprovalSnapshot(a.loop.deps.Security.ListPendingApprovals()); pending != nil {
				exec.PendingApproval = &graphruntime.ApprovalState{
					ID:          pending.ID,
					ToolName:    pending.ToolName,
					Args:        cloneMap(pending.Args),
					Reason:      pending.Reason,
					RequestedAt: pending.Requested,
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

func execOutputSchemaName(exec *graphruntime.ExecutionState) string {
	if exec == nil {
		return ""
	}
	return exec.OutputSchemaName
}

func structuredOutputRaw(schemaName, content string) string {
	if schemaName == "" || !json.Valid([]byte(content)) {
		return ""
	}
	return content
}

func (a *graphLoopAdapter) syncFromGraphState(state *graphruntime.TurnState, exec *graphruntime.ExecutionState) {
	if state == nil {
		return
	}

	a.loop.mu.Lock()
	defer a.loop.mu.Unlock()

	a.loop.options.Mode = state.Mode
	a.loop.ephemerals = append([]string(nil), state.Ephemerals...)
	a.loop.pendingMedia = cloneMediaItems(state.PendingMedia)
	a.loop.task.Name = state.Task.Name
	a.loop.task.Mode = state.Task.Mode
	a.loop.task.Status = state.Task.Status
	a.loop.task.Summary = state.Task.Summary
	a.loop.task.YieldRequested = state.Task.YieldRequested
	a.loop.task.StepsSinceUpdate = state.Task.StepsSinceUpdate
	a.loop.task.PlanModified = state.Task.PlanModified
	a.loop.task.CurrentStep = state.Task.CurrentStep
	a.loop.task.ArtifactLastStep = cloneIntMap(state.Task.ArtifactLastStep)
	if a.loop.task.ArtifactLastStep == nil {
		a.loop.task.ArtifactLastStep = make(map[string]int)
	}
	a.loop.task.SkillLoaded = state.Task.SkillLoaded
	a.loop.task.SkillPath = state.Task.SkillPath
	a.loop.compactCount = state.Compact.CompactCount
	a.loop.outputContinuations = state.Compact.OutputContinuations
	a.loop.historyDirty = state.Compact.HistoryDirty
	a.loop.activeSkills = cloneStringMap(state.ActiveSkills)
	a.loop.guard.SetForceToolName(state.ForceNextTool)
	a.loop.intelligence = cloneIntelligenceState(state.Intelligence)
	a.loop.orchestration = cloneOrchestrationState(state.Orchestration)

	a.rs.opts.Mode = state.Mode
	a.rs.exec = exec
	if exec != nil {
		if exec.MaxTokens == 0 && a.rs.opts.MaxTokens > 0 {
			exec.MaxTokens = a.rs.opts.MaxTokens
		}
		a.loop.pendingWake.Store(exec.PendingWake)
		if a.loop.deps.Security != nil &&
			exec.Status == graphruntime.NodeStatusWait &&
			exec.WaitReason == graphruntime.WaitReasonApproval &&
			exec.PendingApproval != nil {
			a.loop.deps.Security.RestorePendingApproval(ApprovalSnapshot{
				ID:        exec.PendingApproval.ID,
				ToolName:  exec.PendingApproval.ToolName,
				Args:      cloneMap(exec.PendingApproval.Args),
				Reason:    exec.PendingApproval.Reason,
				Requested: exec.PendingApproval.RequestedAt,
			})
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

func mapToolResults(msgs []llm.Message, calls []llm.ToolCall) []graphruntime.ToolResultState {
	callNames := make(map[string]string, len(calls))
	for _, call := range calls {
		callNames[call.ID] = call.Function.Name
	}

	out := make([]graphruntime.ToolResultState, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role != "tool" {
			continue
		}
		result := graphruntime.ToolResultState{
			CallID: msg.ToolCallID,
			Name:   callNames[msg.ToolCallID],
			Output: msg.Content,
		}
		if strings.HasPrefix(msg.Content, "Error: ") {
			result.Error = strings.TrimPrefix(msg.Content, "Error: ")
		}
		out = append(out, result)
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMediaItems(in []map[string]string) []map[string]string {
	if in == nil {
		return nil
	}
	out := make([]map[string]string, 0, len(in))
	for _, item := range in {
		clone := make(map[string]string, len(item))
		for k, v := range item {
			clone[k] = v
		}
		out = append(out, clone)
	}
	return out
}

func latestApprovalSnapshot(pending []ApprovalSnapshot) *ApprovalSnapshot {
	if len(pending) == 0 {
		return nil
	}
	latest := pending[0]
	for _, candidate := range pending[1:] {
		if candidate.Requested.After(latest.Requested) {
			latest = candidate
		}
	}
	return &latest
}
