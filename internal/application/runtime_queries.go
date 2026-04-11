package application

import (
	"context"

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
	return buildRuntimeGraph(sessionID, runs), nil
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
