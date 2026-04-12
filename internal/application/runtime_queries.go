package application

import (
	"context"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (a *RuntimeQueries) ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	if a.runtimeStore == nil {
		return nil, nil
	}
	snaps, err := a.runtimeStore.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return a.runtimeSnapshotsToInfo(snaps), nil
}

func (a *RuntimeQueries) ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return filterPendingRuns(runs), nil
}

func (a *RuntimeQueries) ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return filterPendingDecisionRuns(filterPendingRuns(runs)), nil
}

func (a *RuntimeQueries) PendingDecision(ctx context.Context, sessionID, runID string) (*apitype.RuntimeRunInfo, error) {
	runs, err := a.ListPendingDecisions(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if runID == "" {
		if len(runs) == 0 {
			return nil, nil
		}
		run := runs[0]
		return &run, nil
	}
	for _, run := range runs {
		if run.RunID == runID {
			matched := run
			return &matched, nil
		}
	}
	return nil, nil
}

func (a *RuntimeQueries) ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error) {
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return apitype.OrchestrationGraphInfo{SessionID: sessionID}, err
	}
	var caps []apitype.CapabilityInfo
	if a.discovery != nil {
		caps = projectCapabilities(a.discovery.ListCapabilities(ctx))
	}
	return buildRuntimeGraph(sessionID, runs, caps), nil
}

func (a *RuntimeQueries) ListRuntimeRunsByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) ([]apitype.RuntimeRunInfo, error) {
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return filterRuntimeRunsByEvent(runs, eventType, trigger, barrierID), nil
}

func (a *RuntimeQueries) ListRuntimeGraphByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) (apitype.OrchestrationGraphInfo, error) {
	runs, err := a.ListRuntimeRunsByEvent(ctx, sessionID, eventType, trigger, barrierID)
	if err != nil {
		return apitype.OrchestrationGraphInfo{SessionID: sessionID}, err
	}
	var caps []apitype.CapabilityInfo
	if a.discovery != nil {
		caps = projectCapabilities(a.discovery.ListCapabilities(ctx))
	}
	return buildRuntimeGraph(sessionID, runs, caps), nil
}

func (a *RuntimeQueries) ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error) {
	if a.runtimeStore == nil {
		return nil, nil
	}
	snaps, err := a.runtimeStore.ListByParentRun(ctx, parentRunID)
	if err != nil {
		return nil, err
	}
	return a.runtimeSnapshotsToInfo(snaps), nil
}

func filterPendingRuns(runs []apitype.RuntimeRunInfo) []apitype.RuntimeRunInfo {
	pending := make([]apitype.RuntimeRunInfo, 0, len(runs))
	for _, run := range runs {
		if run.Status == "wait" || run.WaitReason != "" {
			pending = append(pending, run)
		}
	}
	return pending
}

func filterPendingDecisionRuns(runs []apitype.RuntimeRunInfo) []apitype.RuntimeRunInfo {
	decisions := make([]apitype.RuntimeRunInfo, 0, len(runs))
	for _, run := range runs {
		if run.PendingDecision != nil {
			decisions = append(decisions, run)
		}
	}
	return decisions
}

func filterRuntimeRunsByEvent(runs []apitype.RuntimeRunInfo, eventType, trigger, barrierID string) []apitype.RuntimeRunInfo {
	eventType = strings.TrimSpace(eventType)
	trigger = strings.TrimSpace(trigger)
	barrierID = strings.TrimSpace(barrierID)
	if eventType == "" && trigger == "" && barrierID == "" {
		return runs
	}

	filtered := make([]apitype.RuntimeRunInfo, 0, len(runs))
	for _, run := range runs {
		if runtimeRunMatchesEvent(run, eventType, trigger, barrierID) {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func runtimeRunMatchesEvent(run apitype.RuntimeRunInfo, eventType, trigger, barrierID string) bool {
	for _, event := range run.Events {
		if eventType != "" && event.Type != eventType {
			continue
		}
		if trigger != "" && event.Trigger != trigger {
			continue
		}
		if barrierID != "" && event.BarrierID != barrierID {
			continue
		}
		return true
	}
	return false
}

func projectCapabilities(caps []service.ToolCapability) []apitype.CapabilityInfo {
	if len(caps) == 0 {
		return nil
	}
	result := make([]apitype.CapabilityInfo, len(caps))
	for i, c := range caps {
		result[i] = apitype.CapabilityInfo{
			Name:        c.Name,
			Description: c.Description,
			Category:    c.Category,
			Source:      c.Source,
			InputSchema: c.InputSchema, // Keep input schema for A2A exposure
			Tags:        c.Tags,
			Version:     c.Version,
		}
	}
	return result
}
