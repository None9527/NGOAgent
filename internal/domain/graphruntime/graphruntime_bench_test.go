package graphruntime

import (
	"context"
	"fmt"
	"testing"
)

// benchNode is a lightweight test node for bench cases.
type benchNode struct {
	name string
	kind NodeKind
	run  func(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error)
}

func (n benchNode) Name() string   { return n.name }
func (n benchNode) Kind() NodeKind { return n.kind }
func (n benchNode) Execute(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error) {
	return n.run(ctx, rt, state)
}

// ---------------------------------------------------------------------------
// B1: Diamond Branching — same source node, two conditional edges, select by route key
// ---------------------------------------------------------------------------

func TestBenchDiamondBranching(t *testing.T) {
	store := NewInMemorySnapshotStore()
	graph := GraphDefinition{
		ID:        "diamond",
		Version:   "v1",
		EntryNode: "decide",
		Nodes: map[string]Node{
			"decide": benchNode{
				name: "decide",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if state.UserMessage == "left" {
						return NodeResult{RouteKey: "left"}, nil
					}
					return NodeResult{RouteKey: "right"}, nil
				},
			},
			"left_done": benchNode{
				name: "left_done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "went_left"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
			"right_done": benchNode{
				name: "right_done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "went_right"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "decide", To: "left_done", Condition: "left", Priority: 1},
			{From: "decide", To: "right_done", Condition: "right", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	for _, tc := range []struct {
		msg  string
		want string
	}{
		{"left", "went_left"},
		{"right", "went_right"},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			runID := "diamond-" + tc.msg
			if err := rt.Run(context.Background(), RunRequest{
				RunID:   runID,
				Session: &SessionState{SessionID: "s-diamond"},
				Turn:    TurnState{RunID: runID, UserMessage: tc.msg},
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			snap, _ := store.LoadLatest(context.Background(), runID)
			if snap == nil || snap.Status != NodeStatusComplete {
				t.Fatalf("expected complete, got %#v", snap)
			}
			if snap.TurnState.OutputDraft != tc.want {
				t.Fatalf("expected output %q, got %q", tc.want, snap.TurnState.OutputDraft)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B2: Edge Priority Resolution — multiple edges same from/condition, lowest priority wins
// ---------------------------------------------------------------------------

func TestBenchEdgePriorityResolution(t *testing.T) {
	store := NewInMemorySnapshotStore()
	graph := GraphDefinition{
		ID:        "priority",
		Version:   "v1",
		EntryNode: "start",
		Nodes: map[string]Node{
			"start": benchNode{
				name: "start",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					return NodeResult{RouteKey: "next"}, nil
				},
			},
			"high_priority": benchNode{
				name: "high_priority",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "high_priority_won"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
			"low_priority": benchNode{
				name: "low_priority",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "low_priority_won"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "start", To: "low_priority", Condition: "next", Priority: 99},
			{From: "start", To: "high_priority", Condition: "next", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-priority",
		Session: &SessionState{SessionID: "s-priority"},
		Turn:    TurnState{RunID: "run-priority"},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snap, _ := store.LoadLatest(context.Background(), "run-priority")
	if snap == nil || snap.TurnState.OutputDraft != "high_priority_won" {
		t.Fatalf("expected high_priority_won, got %#v", snap)
	}
}

// ---------------------------------------------------------------------------
// B3: Deep NodePatch Stack — multiple nodes append history + ephemerals via Patch
// ---------------------------------------------------------------------------

func TestBenchDeepNodePatchStack(t *testing.T) {
	store := NewInMemorySnapshotStore()
	nodeCount := 4

	nodes := map[string]Node{}
	edges := []Edge{}
	for i := 0; i < nodeCount; i++ {
		name := fmt.Sprintf("step_%d", i)
		nextRoute := "next"
		isLast := i == nodeCount-1
		idx := i
		nodes[name] = benchNode{
			name: name,
			kind: NodeKindCustom,
			run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
				if isLast {
					return NodeResult{
						Status: NodeStatusComplete,
						Patch: NodePatch{
							AppendHistory:    []ConversationMessageState{{Role: "assistant", Content: fmt.Sprintf("answer_%d", idx)}},
							AppendEphemerals: []string{fmt.Sprintf("eph_%d", idx)},
						},
					}, nil
				}
				return NodeResult{
					RouteKey: nextRoute,
					Patch: NodePatch{
						AppendHistory:    []ConversationMessageState{{Role: "assistant", Content: fmt.Sprintf("answer_%d", idx)}},
						AppendEphemerals: []string{fmt.Sprintf("eph_%d", idx)},
					},
				}, nil
			},
		}
		if i < nodeCount-1 {
			edges = append(edges, Edge{
				From:      name,
				To:        fmt.Sprintf("step_%d", i+1),
				Condition: "next",
				Priority:  1,
			})
		}
	}

	graph := GraphDefinition{
		ID:        "deep-patch",
		Version:   "v1",
		EntryNode: "step_0",
		Nodes:     nodes,
		Edges:     edges,
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-deep-patch",
		Session: &SessionState{SessionID: "s-deep-patch"},
		Turn:    TurnState{RunID: "run-deep-patch"},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	snap, _ := store.LoadLatest(context.Background(), "run-deep-patch")
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if len(snap.TurnState.History) != nodeCount {
		t.Fatalf("expected %d history entries, got %d", nodeCount, len(snap.TurnState.History))
	}
	if len(snap.TurnState.Ephemerals) != nodeCount {
		t.Fatalf("expected %d ephemerals, got %d", nodeCount, len(snap.TurnState.Ephemerals))
	}
	for i := 0; i < nodeCount; i++ {
		want := fmt.Sprintf("answer_%d", i)
		if snap.TurnState.History[i].Content != want {
			t.Fatalf("history[%d]: expected %q, got %q", i, want, snap.TurnState.History[i].Content)
		}
		wantEph := fmt.Sprintf("eph_%d", i)
		if snap.TurnState.Ephemerals[i] != wantEph {
			t.Fatalf("ephemeral[%d]: expected %q, got %q", i, wantEph, snap.TurnState.Ephemerals[i])
		}
	}
}

// ---------------------------------------------------------------------------
// B4: Multi-Checkpoint Consistency — 5 nodes each with NeedsCheckpoint
// ---------------------------------------------------------------------------

func TestBenchMultiCheckpointConsistency(t *testing.T) {
	store := NewInMemorySnapshotStore()
	checkpointCount := 5
	observed := make([]int, 0)

	nodes := map[string]Node{}
	edges := []Edge{}
	for i := 0; i < checkpointCount; i++ {
		name := fmt.Sprintf("cp_%d", i)
		isLast := i == checkpointCount-1
		idx := i
		nodes[name] = benchNode{
			name: name,
			kind: NodeKindCustom,
			run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
				state.Ephemerals = append(state.Ephemerals, fmt.Sprintf("checkpoint_%d", idx))
				observed = append(observed, idx)
				if isLast {
					return NodeResult{Status: NodeStatusComplete, NeedsCheckpoint: true}, nil
				}
				return NodeResult{RouteKey: "next", NeedsCheckpoint: true}, nil
			},
		}
		if i < checkpointCount-1 {
			edges = append(edges, Edge{From: name, To: fmt.Sprintf("cp_%d", i+1), Condition: "next", Priority: 1})
		}
	}

	graph := GraphDefinition{
		ID:        "multi-cp",
		Version:   "v1",
		EntryNode: "cp_0",
		Nodes:     nodes,
		Edges:     edges,
	}
	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-multi-cp",
		Session: &SessionState{SessionID: "s-multi-cp"},
		Turn:    TurnState{RunID: "run-multi-cp"},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	snap, _ := store.LoadLatest(context.Background(), "run-multi-cp")
	if snap == nil || snap.Status != NodeStatusComplete {
		t.Fatalf("expected complete, got %#v", snap)
	}
	if len(snap.TurnState.Ephemerals) != checkpointCount {
		t.Fatalf("expected %d ephemerals in final snapshot, got %d", checkpointCount, len(snap.TurnState.Ephemerals))
	}
	if len(observed) != checkpointCount {
		t.Fatalf("expected %d nodes executed, got %d", checkpointCount, len(observed))
	}
}

// ---------------------------------------------------------------------------
// B5: Context Cancel During Execution
// ---------------------------------------------------------------------------

func TestBenchContextCancelDuringExec(t *testing.T) {
	store := NewInMemorySnapshotStore()
	step := 0
	ctx, cancel := context.WithCancel(context.Background())

	graph := GraphDefinition{
		ID:        "cancel-test",
		Version:   "v1",
		EntryNode: "loop",
		Nodes: map[string]Node{
			"loop": benchNode{
				name: "loop",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					step++
					state.Ephemerals = append(state.Ephemerals, fmt.Sprintf("step_%d", step))
					if step >= 3 {
						cancel()
					}
					return NodeResult{RouteKey: "again"}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "loop", To: "loop", Condition: "again", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	err = rt.Run(ctx, RunRequest{
		RunID:   "run-cancel",
		Session: &SessionState{SessionID: "s-cancel"},
		Turn:    TurnState{RunID: "run-cancel"},
	})
	if err == nil {
		t.Fatal("expected context cancel error")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if step < 3 {
		t.Fatalf("expected at least 3 steps before cancel, got %d", step)
	}
}

// ---------------------------------------------------------------------------
// B6: Resume At Middle Node — skip initial nodes via ResumeAt cursor
// ---------------------------------------------------------------------------

func TestBenchResumeAtMiddleNode(t *testing.T) {
	store := NewInMemorySnapshotStore()
	nodesVisited := []string{}

	graph := GraphDefinition{
		ID:        "resume-mid",
		Version:   "v1",
		EntryNode: "step_a",
		Nodes: map[string]Node{
			"step_a": benchNode{
				name: "step_a",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					nodesVisited = append(nodesVisited, "step_a")
					return NodeResult{RouteKey: "next"}, nil
				},
			},
			"step_b": benchNode{
				name: "step_b",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					nodesVisited = append(nodesVisited, "step_b")
					return NodeResult{RouteKey: "next"}, nil
				},
			},
			"step_c": benchNode{
				name: "step_c",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					nodesVisited = append(nodesVisited, "step_c")
					state.OutputDraft = "from_middle"
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "step_a", To: "step_b", Condition: "next", Priority: 1},
			{From: "step_b", To: "step_c", Condition: "next", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-resume-mid",
		Session: &SessionState{SessionID: "s-resume-mid"},
		Turn:    TurnState{RunID: "run-resume-mid"},
		ResumeAt: &ExecutionCursor{
			GraphID:      "resume-mid",
			GraphVersion: "v1",
			CurrentNode:  "step_b",
			Step:         1,
		},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(nodesVisited) != 2 {
		t.Fatalf("expected 2 nodes visited (step_b, step_c), got %v", nodesVisited)
	}
	if nodesVisited[0] != "step_b" || nodesVisited[1] != "step_c" {
		t.Fatalf("expected [step_b, step_c], got %v", nodesVisited)
	}
	snap, _ := store.LoadLatest(context.Background(), "run-resume-mid")
	if snap == nil || snap.TurnState.OutputDraft != "from_middle" {
		t.Fatalf("unexpected output: %#v", snap)
	}
}

// ---------------------------------------------------------------------------
// B7: Fatal Node Halts Graph
// ---------------------------------------------------------------------------

func TestBenchFatalNodeHaltsGraph(t *testing.T) {
	store := NewInMemorySnapshotStore()
	secondNodeCalled := false

	graph := GraphDefinition{
		ID:        "fatal-halt",
		Version:   "v1",
		EntryNode: "fatal_node",
		Nodes: map[string]Node{
			"fatal_node": benchNode{
				name: "fatal_node",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					return NodeResult{
						Status:         NodeStatusFatal,
						ObservedState:  "critical_failure",
						NeedsCheckpoint: true,
					}, nil
				},
			},
			"unreachable": benchNode{
				name: "unreachable",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					secondNodeCalled = true
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "fatal_node", To: "unreachable", Condition: "", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.Run(context.Background(), RunRequest{
		RunID:   "run-fatal",
		Session: &SessionState{SessionID: "s-fatal"},
		Turn:    TurnState{RunID: "run-fatal"},
	})
	if err == nil {
		t.Fatal("expected error from fatal node")
	}
	if secondNodeCalled {
		t.Fatal("unreachable node should not be called after fatal")
	}
	snap, _ := store.LoadLatest(context.Background(), "run-fatal")
	if snap == nil || snap.Status != NodeStatusFatal {
		t.Fatalf("expected fatal snapshot, got %#v", snap)
	}
}

// ---------------------------------------------------------------------------
// B8: Node Returns Error — non-NodeResult error propagation
// ---------------------------------------------------------------------------

func TestBenchNodeReturnsError(t *testing.T) {
	store := NewInMemorySnapshotStore()

	graph := GraphDefinition{
		ID:        "node-error",
		Version:   "v1",
		EntryNode: "broken",
		Nodes: map[string]Node{
			"broken": benchNode{
				name: "broken",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
					return NodeResult{}, fmt.Errorf("node panicked: out of memory")
				},
			},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.Run(context.Background(), RunRequest{
		RunID:   "run-node-error",
		Session: &SessionState{SessionID: "s-node-error"},
		Turn:    TurnState{RunID: "run-node-error"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "node panicked: out of memory" {
		t.Fatalf("expected original error, got %v", err)
	}
	snap, _ := store.LoadLatest(context.Background(), "run-node-error")
	if snap == nil || snap.Status != NodeStatusFatal {
		t.Fatalf("expected fatal snapshot, got %#v", snap)
	}
	if snap.ExecutionState.LastError != "node panicked: out of memory" {
		t.Fatalf("expected last error in snapshot, got %q", snap.ExecutionState.LastError)
	}
}

// ---------------------------------------------------------------------------
// B9: Empty Graph Validation — boundary conditions
// ---------------------------------------------------------------------------

func TestBenchEmptyGraphValidation(t *testing.T) {
	tests := []struct {
		name    string
		graph   GraphDefinition
		wantErr string
	}{
		{
			name:    "empty_id",
			graph:   GraphDefinition{Version: "v1", EntryNode: "a", Nodes: map[string]Node{"a": benchNode{name: "a"}}},
			wantErr: "graph id is required",
		},
		{
			name:    "empty_version",
			graph:   GraphDefinition{ID: "g", EntryNode: "a", Nodes: map[string]Node{"a": benchNode{name: "a"}}},
			wantErr: "graph version is required",
		},
		{
			name:    "empty_entry",
			graph:   GraphDefinition{ID: "g", Version: "v1", Nodes: map[string]Node{"a": benchNode{name: "a"}}},
			wantErr: "graph entry node is required",
		},
		{
			name:    "no_nodes",
			graph:   GraphDefinition{ID: "g", Version: "v1", EntryNode: "missing"},
			wantErr: "graph must define at least one node",
		},
		{
			name: "entry_not_in_nodes",
			graph: GraphDefinition{
				ID: "g", Version: "v1", EntryNode: "missing",
				Nodes: map[string]Node{"other": benchNode{name: "other"}},
			},
			wantErr: `entry node "missing" not found`,
		},
		{
			name: "edge_from_unknown",
			graph: GraphDefinition{
				ID: "g", Version: "v1", EntryNode: "a",
				Nodes: map[string]Node{"a": benchNode{name: "a"}},
				Edges: []Edge{{From: "unknown", To: "a"}},
			},
			wantErr: `edge from unknown node "unknown"`,
		},
		{
			name: "edge_to_unknown",
			graph: GraphDefinition{
				ID: "g", Version: "v1", EntryNode: "a",
				Nodes: map[string]Node{"a": benchNode{name: "a"}},
				Edges: []Edge{{From: "a", To: "unknown"}},
			},
			wantErr: `edge to unknown node "unknown"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.graph.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("expected %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B10: Self-Loop With Checkpoint — node loops back to itself, checkpoints each time
// ---------------------------------------------------------------------------

func TestBenchSelfLoopWithCheckpoint(t *testing.T) {
	store := NewInMemorySnapshotStore()
	iteration := 0

	graph := GraphDefinition{
		ID:        "self-loop-cp",
		Version:   "v1",
		EntryNode: "looper",
		Nodes: map[string]Node{
			"looper": benchNode{
				name: "looper",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					iteration++
					state.Ephemerals = append(state.Ephemerals, fmt.Sprintf("iter_%d", iteration))
					if iteration >= 3 {
						state.OutputDraft = fmt.Sprintf("completed_at_%d", iteration)
						return NodeResult{Status: NodeStatusComplete, NeedsCheckpoint: true}, nil
					}
					return NodeResult{RouteKey: "loop", NeedsCheckpoint: true}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "looper", To: "looper", Condition: "loop", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-self-loop",
		Session: &SessionState{SessionID: "s-self-loop"},
		Turn:    TurnState{RunID: "run-self-loop"},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	snap, _ := store.LoadLatest(context.Background(), "run-self-loop")
	if snap == nil || snap.Status != NodeStatusComplete {
		t.Fatalf("expected complete, got %#v", snap)
	}
	if snap.TurnState.OutputDraft != "completed_at_3" {
		t.Fatalf("expected completed_at_3, got %q", snap.TurnState.OutputDraft)
	}
	if len(snap.TurnState.Ephemerals) != 3 {
		t.Fatalf("expected 3 ephemerals, got %d", len(snap.TurnState.Ephemerals))
	}
}

// ---------------------------------------------------------------------------
// B11: Wait Reason Variants — all three legal wait reasons
// ---------------------------------------------------------------------------

func TestBenchWaitReasonVariants(t *testing.T) {
	validReasons := []struct {
		reason WaitReason
		name   string
	}{
		{WaitReasonApproval, "approval"},
		{WaitReasonBarrier, "barrier"},
		{WaitReasonUserInput, "user_input"},
	}

	for _, tc := range validReasons {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemorySnapshotStore()
			graph := GraphDefinition{
				ID:        "wait-" + tc.name,
				Version:   "v1",
				EntryNode: "waiter",
				Nodes: map[string]Node{
					"waiter": benchNode{
						name: "waiter",
						kind: NodeKindCustom,
						run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
							return NodeResult{
								Status:          NodeStatusWait,
								NeedsCheckpoint: true,
								WaitReason:      tc.reason,
							}, nil
						},
					},
				},
			}

			rt, err := NewRuntime(graph, store)
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			runID := "run-wait-" + tc.name
			if err := rt.Run(context.Background(), RunRequest{
				RunID:   runID,
				Session: &SessionState{SessionID: "s-wait"},
				Turn:    TurnState{RunID: runID},
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			snap, _ := store.LoadLatest(context.Background(), runID)
			if snap == nil || snap.Status != NodeStatusWait {
				t.Fatalf("expected wait snapshot, got %#v", snap)
			}
			if snap.ExecutionState.WaitReason != tc.reason {
				t.Fatalf("expected wait reason %q, got %q", tc.reason, snap.ExecutionState.WaitReason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B12: Structured Output Preservation
// ---------------------------------------------------------------------------

func TestBenchStructuredOutputPreservation(t *testing.T) {
	t.Run("valid_json_draft_creates_structured_output", func(t *testing.T) {
		store := NewInMemorySnapshotStore()
		graph := GraphDefinition{
			ID:        "structured-valid",
			Version:   "v1",
			EntryNode: "node",
			Nodes: map[string]Node{
				"node": benchNode{
					name: "node",
					kind: NodeKindCustom,
					run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
						state.OutputDraft = `{"score":0.95,"verdict":"pass"}`
						return NodeResult{
							Status:           NodeStatusComplete,
							OutputSchemaName: "eval.v1",
						}, nil
					},
				},
			},
		}
		rt, err := NewRuntime(graph, store)
		if err != nil {
			t.Fatalf("NewRuntime: %v", err)
		}
		if err := rt.Run(context.Background(), RunRequest{
			RunID:   "run-structured-valid",
			Session: &SessionState{SessionID: "s-structured"},
			Turn:    TurnState{RunID: "run-structured-valid"},
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		snap, _ := store.LoadLatest(context.Background(), "run-structured-valid")
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		if snap.TurnState.StructuredOutput.SchemaName != "eval.v1" {
			t.Fatalf("expected schema name, got %#v", snap.TurnState.StructuredOutput)
		}
		if !snap.TurnState.StructuredOutput.Valid {
			t.Fatal("expected structured output to be valid")
		}
		if snap.TurnState.StructuredOutput.RawJSON != `{"score":0.95,"verdict":"pass"}` {
			t.Fatalf("unexpected raw JSON: %q", snap.TurnState.StructuredOutput.RawJSON)
		}
	})

	t.Run("non_json_draft_preserves_existing", func(t *testing.T) {
		store := NewInMemorySnapshotStore()
		graph := GraphDefinition{
			ID:        "structured-preserve",
			Version:   "v1",
			EntryNode: "node",
			Nodes: map[string]Node{
				"node": benchNode{
					name: "node",
					kind: NodeKindCustom,
					run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
						state.OutputDraft = "plain text answer"
						state.StructuredOutput = StructuredOutputState{
							SchemaName: "eval.v1",
							RawJSON:    `{"existing":"data"}`,
							Valid:      true,
						}
						return NodeResult{
							Status:           NodeStatusComplete,
							OutputSchemaName: "eval.v1",
						}, nil
					},
				},
			},
		}
		rt, err := NewRuntime(graph, store)
		if err != nil {
			t.Fatalf("NewRuntime: %v", err)
		}
		if err := rt.Run(context.Background(), RunRequest{
			RunID:   "run-structured-preserve",
			Session: &SessionState{SessionID: "s-structured"},
			Turn:    TurnState{RunID: "run-structured-preserve"},
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		snap, _ := store.LoadLatest(context.Background(), "run-structured-preserve")
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		if snap.TurnState.StructuredOutput.RawJSON != `{"existing":"data"}` {
			t.Fatalf("expected existing structured output preserved, got %#v", snap.TurnState.StructuredOutput)
		}
	})
}
