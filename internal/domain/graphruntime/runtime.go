package graphruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
)

type Runtime struct {
	graph    GraphDefinition
	snapshot SnapshotStore
	maxSteps int
}

func NewRuntime(graph GraphDefinition, snapshot SnapshotStore) (*Runtime, error) {
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return &Runtime{
		graph:    graph,
		snapshot: snapshot,
		maxSteps: 256,
	}, nil
}

func (r *Runtime) Run(ctx context.Context, req RunRequest) error {
	if req.RunID == "" {
		return fmt.Errorf("run id is required")
	}
	if req.Session == nil {
		return fmt.Errorf("session state is required")
	}

	execState := ExecutionState{
		Cursor: ExecutionCursor{
			GraphID:      r.graph.ID,
			GraphVersion: r.graph.Version,
			CurrentNode:  r.graph.EntryNode,
			Step:         0,
		},
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    NodeStatusContinue,
	}
	if req.ResumeAt != nil {
		execState.Cursor = *req.ResumeAt
	}

	rt := &RuntimeContext{
		Graph:         r.graph,
		Session:       cloneSession(req.Session),
		Execution:     &execState,
		SnapshotStore: r.snapshot,
		Values:        map[string]any{},
	}

	turn := cloneTurn(req.Turn)
	return r.execute(ctx, rt, &turn)
}

func (r *Runtime) Resume(ctx context.Context, runID string) error {
	if r.snapshot == nil {
		return fmt.Errorf("snapshot store not configured")
	}
	snap, err := r.snapshot.LoadLatest(ctx, runID)
	if err != nil {
		return err
	}
	if snap == nil {
		return fmt.Errorf("run snapshot not found: %s", runID)
	}
	if snap.GraphID != r.graph.ID || snap.GraphVersion != r.graph.Version {
		return fmt.Errorf("snapshot graph mismatch: snapshot=%s@%s runtime=%s@%s",
			snap.GraphID, snap.GraphVersion, r.graph.ID, r.graph.Version)
	}

	session := &SessionState{SessionID: snap.SessionID}
	rt := &RuntimeContext{
		Graph:         r.graph,
		Session:       session,
		Execution:     cloneExecution(snap.ExecutionState),
		SnapshotStore: r.snapshot,
		Values:        map[string]any{},
	}
	turn := cloneTurn(snap.TurnState)
	return r.execute(ctx, rt, &turn)
}

func (r *Runtime) execute(ctx context.Context, rt *RuntimeContext, state *TurnState) error {
	if rt.Execution == nil {
		return fmt.Errorf("execution state is required")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if rt.Execution.Cursor.Step >= r.maxSteps {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = "runtime step limit exceeded"
			_ = r.saveSnapshot(ctx, rt, state)
			return errors.New(rt.Execution.LastError)
		}

		nodeName := rt.Execution.Cursor.CurrentNode
		node, ok := r.graph.Nodes[nodeName]
		if !ok {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = fmt.Sprintf("node not found: %s", nodeName)
			_ = r.saveSnapshot(ctx, rt, state)
			return errors.New(rt.Execution.LastError)
		}

		result, err := node.Execute(ctx, rt, state)
		if err != nil {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = err.Error()
			rt.Execution.UpdatedAt = time.Now()
			_ = r.saveSnapshot(ctx, rt, state)
			return err
		}

		result = result.normalize()
		rt.Execution.Status = result.Status
		rt.Execution.Cursor.RouteKey = result.RouteKey
		rt.Execution.UpdatedAt = time.Now()

		switch result.Status {
		case NodeStatusWait:
			if result.NeedsCheckpoint || r.snapshot != nil {
				if err := r.saveSnapshot(ctx, rt, state); err != nil {
					return err
				}
			}
			return nil
		case NodeStatusComplete:
			if err := r.saveSnapshot(ctx, rt, state); err != nil {
				return err
			}
			return nil
		case NodeStatusFatal:
			if err := r.saveSnapshot(ctx, rt, state); err != nil {
				return err
			}
			return fmt.Errorf("graph runtime terminated at node %s", nodeName)
		}

		next, err := r.nextNode(nodeName, result.RouteKey)
		if err != nil {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = err.Error()
			if saveErr := r.saveSnapshot(ctx, rt, state); saveErr != nil {
				return saveErr
			}
			return err
		}

		rt.Execution.Cursor.Step++
		rt.Execution.Cursor.CurrentNode = next

		if result.NeedsCheckpoint {
			if err := r.saveSnapshot(ctx, rt, state); err != nil {
				return err
			}
		}
	}
}

func (r *Runtime) nextNode(from, routeKey string) (string, error) {
	var matches []Edge
	for _, e := range r.graph.Edges {
		if e.From != from {
			continue
		}
		if e.Condition == routeKey || (routeKey == "" && e.Condition == "") {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no edge from node %q for route %q", from, routeKey)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Priority == matches[j].Priority {
			return slices.Compare([]string{matches[i].To}, []string{matches[j].To}) < 0
		}
		return matches[i].Priority < matches[j].Priority
	})
	return matches[0].To, nil
}

func (r *Runtime) saveSnapshot(ctx context.Context, rt *RuntimeContext, state *TurnState) error {
	if r.snapshot == nil {
		return nil
	}
	now := time.Now()
	snap := &RunSnapshot{
		RunID:          state.RunID,
		SessionID:      rt.Session.SessionID,
		GraphID:        r.graph.ID,
		GraphVersion:   r.graph.Version,
		Status:         rt.Execution.Status,
		Cursor:         rt.Execution.Cursor,
		TurnState:      cloneTurn(*state),
		ExecutionState: *cloneExecution(*rt.Execution),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return r.snapshot.Save(ctx, snap)
}

func cloneSession(in *SessionState) *SessionState {
	if in == nil {
		return &SessionState{}
	}
	out := *in
	out.ConversationTurns = append([]string(nil), in.ConversationTurns...)
	out.ActiveOverlays = append([]string(nil), in.ActiveOverlays...)
	out.MemoryPointers = append([]string(nil), in.MemoryPointers...)
	if in.SessionMetadata != nil {
		out.SessionMetadata = make(map[string]string, len(in.SessionMetadata))
		for k, v := range in.SessionMetadata {
			out.SessionMetadata[k] = v
		}
	}
	if in.TokenUsageSummary != nil {
		out.TokenUsageSummary = make(map[string]int64, len(in.TokenUsageSummary))
		for k, v := range in.TokenUsageSummary {
			out.TokenUsageSummary[k] = v
		}
	}
	return &out
}

func cloneTurn(in TurnState) TurnState {
	out := in
	out.Attachments = append([]string(nil), in.Attachments...)
	out.Ephemerals = append([]string(nil), in.Ephemerals...)
	out.ToolCalls = append([]ToolCallState(nil), in.ToolCalls...)
	out.ToolResults = append([]ToolResultState(nil), in.ToolResults...)
	return out
}

func cloneExecution(in ExecutionState) *ExecutionState {
	out := in
	if in.PendingApproval != nil {
		approval := *in.PendingApproval
		out.PendingApproval = &approval
	}
	if in.PendingBarrier != nil {
		barrier := *in.PendingBarrier
		barrier.Members = append([]BarrierMemberState(nil), in.PendingBarrier.Members...)
		out.PendingBarrier = &barrier
	}
	return &out
}
