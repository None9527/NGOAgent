package application

import (
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (a *RuntimeQueries) runtimeSnapshotsToInfo(snaps []*graphruntime.RunSnapshot) []apitype.RuntimeRunInfo {
	out := make([]apitype.RuntimeRunInfo, 0, len(snaps))
	for _, snap := range snaps {
		if snap == nil {
			continue
		}
		out = append(out, runtimeSnapshotToInfo(snap))
	}
	return out
}

func runtimeSnapshotToInfo(snap *graphruntime.RunSnapshot) apitype.RuntimeRunInfo {
	info := apitype.RuntimeRunInfo{
		RunID:           snap.RunID,
		ParentRunID:     snap.TurnState.Orchestration.ParentRunID,
		Status:          string(snap.Status),
		CurrentNode:     snap.Cursor.CurrentNode,
		CurrentRoute:    snap.Cursor.RouteKey,
		WaitReason:      string(snap.ExecutionState.WaitReason),
		UpdatedAt:       snap.UpdatedAt.UTC().Format(time.RFC3339),
		PendingMerge:    snap.TurnState.Orchestration.PendingMerge,
		LastWakeSource:  snap.TurnState.Orchestration.LastWakeSource,
		ChildRunIDs:     append([]string(nil), snap.TurnState.Orchestration.ChildRunIDs...),
		PendingDecision: pendingDecisionInfo(snap),
		LastDecision:    lastDecisionInfo(snap),
		Ingress:         runtimeIngressInfo(snap.TurnState.Orchestration.Ingress),
	}
	if len(snap.TurnState.Orchestration.Handoffs) > 0 {
		info.Handoffs = make([]apitype.RuntimeHandoffInfo, 0, len(snap.TurnState.Orchestration.Handoffs))
		for _, handoff := range snap.TurnState.Orchestration.Handoffs {
			info.Handoffs = append(info.Handoffs, apitype.RuntimeHandoffInfo{
				TargetRunID: handoff.TargetRunID,
				TargetNode:  handoff.TargetNode,
				Kind:        handoff.Kind,
				PayloadJSON: handoff.PayloadJSON,
			})
		}
	}
	if len(snap.TurnState.Orchestration.Events) > 0 {
		info.Events = make([]apitype.RuntimeEventInfo, 0, len(snap.TurnState.Orchestration.Events))
		for _, event := range snap.TurnState.Orchestration.Events {
			eventAt := ""
			if !event.At.IsZero() {
				eventAt = event.At.UTC().Format(time.RFC3339)
			}
			info.Events = append(info.Events, apitype.RuntimeEventInfo{
				Type:         event.Type,
				Kind:         event.Kind,
				Source:       event.Source,
				Trigger:      event.Trigger,
				DecisionKind: event.DecisionKind,
				Decision:     event.Decision,
				RunID:        event.RunID,
				SourceRun:    event.SourceRun,
				BarrierID:    event.BarrierID,
				At:           eventAt,
				Summary:      event.Summary,
				PayloadJSON:  event.PayloadJSON,
			})
		}
	}
	return info
}

func pendingDecisionInfo(snap *graphruntime.RunSnapshot) *apitype.RuntimeDecisionInfo {
	contract := service.PendingDecisionFromSnapshot(snap)
	if contract == nil {
		return nil
	}
	return decisionContractInfo(contract)
}

func lastDecisionInfo(snap *graphruntime.RunSnapshot) *apitype.RuntimeDecisionInfo {
	contract := service.DecisionFromSnapshot(snap)
	if contract == nil {
		return nil
	}
	return decisionContractInfo(contract)
}

func decisionContractInfo(contract *graphruntime.DecisionContractState) *apitype.RuntimeDecisionInfo {
	if contract == nil {
		return nil
	}
	info := &apitype.RuntimeDecisionInfo{
		Kind:         string(contract.Kind),
		Schema:       contract.SchemaName,
		Decision:     contract.Decision,
		Reason:       contract.Reason,
		Feedback:     contract.Feedback,
		ResumeAction: contract.ResumeAction,
	}
	if !contract.AppliedAt.IsZero() {
		info.AppliedAt = contract.AppliedAt.UTC().Format(time.RFC3339)
	}
	return info
}

func runtimeIngressInfo(in graphruntime.IngressState) *apitype.RuntimeIngressInfo {
	if in == (graphruntime.IngressState{}) {
		return nil
	}
	info := &apitype.RuntimeIngressInfo{
		Kind:         in.Kind,
		Source:       in.Source,
		Trigger:      in.Trigger,
		RunID:        in.RunID,
		DecisionKind: in.DecisionKind,
		Decision:     in.Decision,
	}
	if !in.At.IsZero() {
		info.At = in.At.UTC().Format(time.RFC3339)
	}
	info.Category = ingressCategory(*info)
	info.Phase = ingressPhase(*info)
	return info
}
