package graphruntime

import (
	"context"
	"fmt"
	"testing"
)

type testNode struct {
	name string
	kind NodeKind
	run  func(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error)
}

func (n testNode) Name() string   { return n.name }
func (n testNode) Kind() NodeKind { return n.kind }
func (n testNode) Execute(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error) {
	return n.run(ctx, rt, state)
}

func TestRuntimeRunsLinearGraph(t *testing.T) {
	store := NewInMemorySnapshotStore()
	graph := GraphDefinition{
		ID:        "linear",
		Version:   "v1",
		EntryNode: "prepare",
		Nodes: map[string]Node{
			"prepare": testNode{
				name: "prepare",
				kind: NodeKindPrepare,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.Ephemerals = append(state.Ephemerals, "prepared")
					return NodeResult{RouteKey: "ok"}, nil
				},
			},
			"done": testNode{
				name: "done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "finished"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "prepare", To: "done", Condition: "ok", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	err = rt.Run(context.Background(), RunRequest{
		RunID:   "run-1",
		Session: &SessionState{SessionID: "session-1"},
		Turn:    TurnState{RunID: "run-1", UserMessage: "hello"},
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.Status != NodeStatusComplete {
		t.Fatalf("unexpected status: %s", snap.Status)
	}
	if snap.TurnState.OutputDraft != "finished" {
		t.Fatalf("unexpected output draft: %q", snap.TurnState.OutputDraft)
	}
}

func TestRuntimeWaitAndResume(t *testing.T) {
	store := NewInMemorySnapshotStore()
	approved := false

	graph := GraphDefinition{
		ID:        "approval-flow",
		Version:   "v1",
		EntryNode: "generate",
		Nodes: map[string]Node{
			"generate": testNode{
				name: "generate",
				kind: NodeKindGenerate,
				run: func(_ context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error) {
					if !approved {
						rt.Execution.PendingApproval = &ApprovalState{
							ID:       "ap-1",
							ToolName: "write_file",
							Reason:   "needs confirmation",
						}
						state.CurrentPlan = "awaiting approval"
						return NodeResult{
							Status:          NodeStatusWait,
							RouteKey:        "approved",
							NeedsCheckpoint: true,
							WaitReason:      WaitReasonApproval,
						}, nil
					}
					rt.Execution.PendingApproval = nil
					state.CurrentPlan = "approved"
					return NodeResult{RouteKey: "approved"}, nil
				},
			},
			"done": testNode{
				name: "done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "resumed"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "generate", To: "done", Condition: "approved", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-2",
		Session: &SessionState{SessionID: "session-2"},
		Turn:    TurnState{RunID: "run-2", UserMessage: "change file"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-2")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap == nil || snap.Status != NodeStatusWait {
		t.Fatalf("expected wait snapshot, got %#v", snap)
	}
	if snap.ExecutionState.PendingApproval == nil {
		t.Fatal("expected pending approval in snapshot")
	}

	approved = true
	if err := rt.Resume(context.Background(), "run-2"); err != nil {
		t.Fatalf("Resume error: %v", err)
	}

	snap, err = store.LoadLatest(context.Background(), "run-2")
	if err != nil {
		t.Fatalf("LoadLatest error after resume: %v", err)
	}
	if snap.Status != NodeStatusComplete {
		t.Fatalf("expected complete after resume, got %s", snap.Status)
	}
	if snap.TurnState.OutputDraft != "resumed" {
		t.Fatalf("unexpected output after resume: %q", snap.TurnState.OutputDraft)
	}
}

func TestRuntimeErrorsWhenNoRouteMatches(t *testing.T) {
	graph := GraphDefinition{
		ID:        "broken",
		Version:   "v1",
		EntryNode: "start",
		Nodes: map[string]Node{
			"start": testNode{
				name: "start",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					return NodeResult{RouteKey: "missing"}, nil
				},
			},
		},
	}

	rt, err := NewRuntime(graph, NewInMemorySnapshotStore())
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	err = rt.Run(context.Background(), RunRequest{
		RunID:   "run-3",
		Session: &SessionState{SessionID: "session-3"},
		Turn:    TurnState{RunID: "run-3"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got, want := err.Error(), `no edge from node "start" for route "missing"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestGraphValidateRejectsUnknownNodes(t *testing.T) {
	graph := GraphDefinition{
		ID:        "bad",
		Version:   "v1",
		EntryNode: "start",
		Nodes: map[string]Node{
			"start": testNode{
				name: "start",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					return NodeResult{}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "start", To: "missing", Condition: "ok"},
		},
	}
	err := graph.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), `edge to unknown node "missing"`; got != want {
		t.Fatalf("unexpected validation error: got %q want %q", got, want)
	}
}

func TestRuntimeStepLimit(t *testing.T) {
	graph := GraphDefinition{
		ID:        "loop",
		Version:   "v1",
		EntryNode: "loop",
		Nodes: map[string]Node{
			"loop": testNode{
				name: "loop",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = fmt.Sprintf("step-%d", len(state.Ephemerals))
					state.Ephemerals = append(state.Ephemerals, "x")
					return NodeResult{RouteKey: "again"}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "loop", To: "loop", Condition: "again", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, NewInMemorySnapshotStore())
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}
	rt.maxSteps = 3

	err = rt.Run(context.Background(), RunRequest{
		RunID:   "run-4",
		Session: &SessionState{SessionID: "session-4"},
		Turn:    TurnState{RunID: "run-4"},
	})
	if err == nil {
		t.Fatal("expected step limit error")
	}
}
