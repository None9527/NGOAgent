package graphruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
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
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        NodeStatusContinue,
		ObservedState: r.graph.EntryNode,
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
	applyIngressMetadata(ctx, &turn)
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
	applyIngressMetadata(ctx, &turn)
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
			r.logEvent(state, rt, "runtime.step_limit", slog.String("error", rt.Execution.LastError))
			_ = r.saveSnapshot(ctx, rt, state)
			return errors.New(rt.Execution.LastError)
		}

		nodeName := rt.Execution.Cursor.CurrentNode
		node, ok := r.graph.Nodes[nodeName]
		if !ok {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = fmt.Sprintf("node not found: %s", nodeName)
			r.logEvent(state, rt, "runtime.node_missing", slog.String("error", rt.Execution.LastError))
			_ = r.saveSnapshot(ctx, rt, state)
			return errors.New(rt.Execution.LastError)
		}

		r.logEvent(state, rt, "runtime.node_start")
		result, err := node.Execute(ctx, rt, state)
		if err != nil {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = err.Error()
			rt.Execution.UpdatedAt = time.Now()
			r.logEvent(state, rt, "runtime.node_error", slog.String("error", err.Error()))
			_ = r.saveSnapshot(ctx, rt, state)
			return err
		}

		result = result.normalize()
		if err := validateWaitResult(result); err != nil {
			rt.Execution.Status = NodeStatusFatal
			rt.Execution.LastError = err.Error()
			rt.Execution.UpdatedAt = time.Now()
			r.logEvent(state, rt, "runtime.node_error", slog.String("error", err.Error()))
			_ = r.saveSnapshot(ctx, rt, state)
			return err
		}
		rt.Execution.Status = result.Status
		rt.Execution.OutputSchemaName = result.OutputSchemaName
		if result.Status == NodeStatusWait {
			rt.Execution.WaitReason = result.WaitReason
		} else {
			rt.Execution.WaitReason = WaitReasonNone
		}
		rt.Execution.Cursor.RouteKey = result.RouteKey
		rt.Execution.UpdatedAt = time.Now()
		if result.ObservedState != "" {
			rt.Execution.ObservedState = result.ObservedState
		}
		if state != nil {
			state.StructuredOutput = structuredOutputSnapshot(result.OutputSchemaName, state.OutputDraft, state.StructuredOutput)
		}
		r.logEvent(state, rt, "runtime.node_result",
			slog.String("result_status", string(result.Status)),
			slog.String("result_route", result.RouteKey),
			slog.String("observed_state", result.ObservedState),
			slog.String("wait_reason", string(result.WaitReason)),
			slog.String("output_schema", result.OutputSchemaName),
			slog.Bool("needs_checkpoint", result.NeedsCheckpoint),
		)

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
			r.logEvent(state, rt, "runtime.transition_error", slog.String("error", err.Error()))
			if saveErr := r.saveSnapshot(ctx, rt, state); saveErr != nil {
				return saveErr
			}
			return err
		}

		rt.Execution.Cursor.Step++
		rt.Execution.Cursor.CurrentNode = next
		r.logEvent(state, rt, "runtime.transition", slog.String("next_node", next))

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
	r.logEvent(state, rt, "runtime.snapshot_save_start", slog.Bool("checkpoint", true))
	if err := r.snapshot.Save(ctx, snap); err != nil {
		r.logEvent(state, rt, "runtime.snapshot_save_error", slog.String("error", err.Error()), slog.Bool("checkpoint", true))
		return err
	}
	r.logEvent(state, rt, "runtime.snapshot_save_done", slog.Bool("checkpoint", true))
	return nil
}

func (r *Runtime) logEvent(state *TurnState, rt *RuntimeContext, msg string, extra ...any) {
	attrs := []any{
		slog.String("graph_id", r.graph.ID),
		slog.String("graph_version", r.graph.Version),
	}
	if state != nil {
		attrs = append(attrs, slog.String("run_id", state.RunID))
	}
	if rt != nil {
		if rt.Session != nil {
			attrs = append(attrs, slog.String("session_id", rt.Session.SessionID))
		}
		if rt.Execution != nil {
			attrs = append(attrs,
				slog.String("node", rt.Execution.Cursor.CurrentNode),
				slog.Int("step", rt.Execution.Cursor.Step),
				slog.String("route", rt.Execution.Cursor.RouteKey),
				slog.String("status", string(rt.Execution.Status)),
				slog.String("observed_state", rt.Execution.ObservedState),
			)
		}
	}
	attrs = append(attrs, extra...)
	slog.Info(msg, attrs...)
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
	out.PendingMedia = clonePendingMedia(in.PendingMedia)
	out.ToolCalls = append([]ToolCallState(nil), in.ToolCalls...)
	out.ToolResults = append([]ToolResultState(nil), in.ToolResults...)
	if in.Task.ArtifactLastStep != nil {
		out.Task.ArtifactLastStep = make(map[string]int, len(in.Task.ArtifactLastStep))
		for k, v := range in.Task.ArtifactLastStep {
			out.Task.ArtifactLastStep[k] = v
		}
	}
	if in.ActiveSkills != nil {
		out.ActiveSkills = make(map[string]string, len(in.ActiveSkills))
		for k, v := range in.ActiveSkills {
			out.ActiveSkills[k] = v
		}
	}
	out.Intelligence.Evaluation.Issues = append([]EvaluationIssueState(nil), in.Intelligence.Evaluation.Issues...)
	out.Orchestration.ChildRunIDs = append([]string(nil), in.Orchestration.ChildRunIDs...)
	out.Orchestration.Handoffs = append([]HandoffState(nil), in.Orchestration.Handoffs...)
	out.Orchestration.Events = append([]OrchestrationEventState(nil), in.Orchestration.Events...)
	if in.Orchestration.ActiveBarrier != nil {
		barrier := *in.Orchestration.ActiveBarrier
		barrier.Members = append([]BarrierMemberState(nil), in.Orchestration.ActiveBarrier.Members...)
		out.Orchestration.ActiveBarrier = &barrier
	}
	return out
}

func applyIngressMetadata(ctx context.Context, state *TurnState) {
	if state == nil {
		return
	}
	meta := ctxutil.RuntimeIngressFromContext(ctx)
	if meta == (ctxutil.RuntimeIngressMetadata{}) {
		return
	}
	state.Orchestration.Ingress = IngressState{
		Kind:         meta.Kind,
		Source:       meta.Source,
		Trigger:      meta.Trigger,
		RunID:        meta.RunID,
		DecisionKind: meta.DecisionKind,
		Decision:     meta.Decision,
		At:           time.Now().UTC(),
	}
	if event := TriggerEventFromIngress("", state.RunID, state.Orchestration.Ingress); event != nil {
		payloadJSON := ""
		if len(event.Payload) > 0 {
			if raw, err := json.Marshal(event.Payload); err == nil {
				payloadJSON = string(raw)
			}
		}
		state.Orchestration.Events = append(state.Orchestration.Events, OrchestrationEventState{
			Type:         "trigger.received",
			Kind:         string(event.Kind),
			Source:       event.Source,
			Trigger:      event.Trigger,
			DecisionKind: state.Orchestration.Ingress.DecisionKind,
			Decision:     state.Orchestration.Ingress.Decision,
			RunID:        event.RunID,
			At:           event.At,
			Summary:      triggerEventSummary(*event),
			SourceRun:    "",
			PayloadJSON:  payloadJSON,
		})
		if len(state.Orchestration.Events) > 32 {
			state.Orchestration.Events = append([]OrchestrationEventState(nil), state.Orchestration.Events[len(state.Orchestration.Events)-32:]...)
		}
	}
}

func triggerEventSummary(event TriggerEvent) string {
	parts := []string{string(event.Kind)}
	if strings.TrimSpace(event.Trigger) != "" {
		parts = append(parts, strings.TrimSpace(event.Trigger))
	}
	if decisionKind, ok := event.Payload["decision_kind"].(string); ok && strings.TrimSpace(decisionKind) != "" {
		parts = append(parts, strings.TrimSpace(decisionKind))
	}
	return strings.Join(parts, ":")
}

func clonePendingMedia(in []map[string]string) []map[string]string {
	if in == nil {
		return nil
	}
	out := make([]map[string]string, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		cp := make(map[string]string, len(item))
		for k, v := range item {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func cloneExecution(in ExecutionState) *ExecutionState {
	out := in
	out.ExcludedProviders = append([]string(nil), in.ExcludedProviders...)
	if in.PendingApproval != nil {
		approval := *in.PendingApproval
		if in.PendingApproval.Args != nil {
			approval.Args = make(map[string]any, len(in.PendingApproval.Args))
			for k, v := range in.PendingApproval.Args {
				approval.Args[k] = v
			}
		}
		out.PendingApproval = &approval
	}
	if in.PendingBarrier != nil {
		barrier := *in.PendingBarrier
		barrier.Members = append([]BarrierMemberState(nil), in.PendingBarrier.Members...)
		out.PendingBarrier = &barrier
	}
	return &out
}

func validateWaitResult(result NodeResult) error {
	if result.Status != NodeStatusWait {
		return nil
	}
	switch result.WaitReason {
	case WaitReasonApproval, WaitReasonBarrier, WaitReasonUserInput:
		return nil
	case WaitReasonNone:
		return fmt.Errorf("wait status requires wait reason")
	case WaitReasonExternal:
		return fmt.Errorf("unsupported wait reason %q", result.WaitReason)
	default:
		return fmt.Errorf("unknown wait reason %q", result.WaitReason)
	}
}

func structuredOutputSnapshot(schemaName, outputDraft string, current StructuredOutputState) StructuredOutputState {
	out := StructuredOutputState{SchemaName: schemaName}
	if schemaName == "" || outputDraft == "" || !json.Valid([]byte(outputDraft)) {
		if current.SchemaName == schemaName && current.Valid && current.RawJSON != "" {
			return current
		}
		return out
	}
	out.RawJSON = outputDraft
	out.Valid = true
	return out
}
