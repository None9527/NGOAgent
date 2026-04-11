package service

import (
	"encoding/json"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

func cloneOrchestrationState(in graphruntime.OrchestrationState) graphruntime.OrchestrationState {
	out := in
	out.ChildRunIDs = append([]string(nil), in.ChildRunIDs...)
	out.Handoffs = append([]graphruntime.HandoffState(nil), in.Handoffs...)
	out.Events = append([]graphruntime.OrchestrationEventState(nil), in.Events...)
	if in.ActiveBarrier != nil {
		barrier := *in.ActiveBarrier
		barrier.Members = append([]graphruntime.BarrierMemberState(nil), in.ActiveBarrier.Members...)
		out.ActiveBarrier = &barrier
	}
	return out
}

func (a *AgentLoop) orchestrationSnapshot() graphruntime.OrchestrationState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneOrchestrationState(a.orchestration)
}

func (a *AgentLoop) setOrchestrationState(state graphruntime.OrchestrationState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orchestration = cloneOrchestrationState(state)
}

func (a *AgentLoop) recordOrchestrationEvent(event graphruntime.OrchestrationEventState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orchestration.Events = append(a.orchestration.Events, event)
	if len(a.orchestration.Events) > 32 {
		a.orchestration.Events = append([]graphruntime.OrchestrationEventState(nil), a.orchestration.Events[len(a.orchestration.Events)-32:]...)
	}
}

func (a *AgentLoop) recordBarrierProgress(runID, barrierID, summary string) {
	a.recordOrchestrationEvent(graphruntime.OrchestrationEventState{
		Type:      "barrier.member_completed",
		RunID:     runID,
		BarrierID: barrierID,
		At:        time.Now().UTC(),
		Summary:   summary,
	})
}

func (a *AgentLoop) recordBarrierFinalized(barrierID, summary string) {
	a.recordOrchestrationEvent(graphruntime.OrchestrationEventState{
		Type:      "barrier.finalized",
		BarrierID: barrierID,
		At:        time.Now().UTC(),
		Summary:   summary,
	})
}

func (a *AgentLoop) recordBarrierTimeout(barrierID, summary string) {
	a.recordOrchestrationEvent(graphruntime.OrchestrationEventState{
		Type:      "barrier.timeout",
		BarrierID: barrierID,
		At:        time.Now().UTC(),
		Summary:   summary,
	})
}

func (a *AgentLoop) BindParentRun(parentRunID string) {
	if parentRunID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orchestration.ParentRunID = parentRunID
}

func (a *AgentLoop) RegisterSpawnedChild(parentRunID, childRunID, taskName, agentType string) {
	if childRunID == "" {
		return
	}
	payload := map[string]string{
		"task_name":  taskName,
		"agent_type": agentType,
		"parent_run": parentRunID,
		"child_run":  childRunID,
	}
	raw, _ := json.Marshal(payload)

	a.mu.Lock()
	defer a.mu.Unlock()
	if parentRunID != "" {
		a.orchestration.ParentRunID = parentRunID
	}
	for _, existing := range a.orchestration.ChildRunIDs {
		if existing == childRunID {
			return
		}
	}
	a.orchestration.ChildRunIDs = append(a.orchestration.ChildRunIDs, childRunID)
	a.orchestration.Handoffs = append(a.orchestration.Handoffs, graphruntime.HandoffState{
		TargetRunID: childRunID,
		PayloadJSON: string(raw),
		Kind:        "subagent_task",
	})
	a.orchestration.Events = append(a.orchestration.Events, graphruntime.OrchestrationEventState{
		Type:      "child.spawned",
		RunID:     childRunID,
		SourceRun: parentRunID,
		At:        time.Now().UTC(),
		Summary:   taskName,
	})
	if len(a.orchestration.Events) > 32 {
		a.orchestration.Events = append([]graphruntime.OrchestrationEventState(nil), a.orchestration.Events[len(a.orchestration.Events)-32:]...)
	}
	if len(a.orchestration.Handoffs) > 32 {
		a.orchestration.Handoffs = append([]graphruntime.HandoffState(nil), a.orchestration.Handoffs[len(a.orchestration.Handoffs)-32:]...)
	}
}
