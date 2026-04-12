package graphruntime

import (
	"context"
	"fmt"
	"testing"

	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
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
						state.Task.Summary = "awaiting approval"
						return NodeResult{
							Status:          NodeStatusWait,
							RouteKey:        "approved",
							NeedsCheckpoint: true,
							WaitReason:      WaitReasonApproval,
						}, nil
					}
					rt.Execution.PendingApproval = nil
					state.Task.Summary = "approved"
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
	if snap.ExecutionState.WaitReason != WaitReasonApproval {
		t.Fatalf("expected wait reason approval in snapshot, got %q", snap.ExecutionState.WaitReason)
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
	if snap.ExecutionState.WaitReason != WaitReasonNone {
		t.Fatalf("expected wait reason to clear after resume completion, got %q", snap.ExecutionState.WaitReason)
	}
	if snap.TurnState.OutputDraft != "resumed" {
		t.Fatalf("unexpected output after resume: %q", snap.TurnState.OutputDraft)
	}
}

func TestRuntimePersistsIngressAcrossRunAndResume(t *testing.T) {
	store := NewInMemorySnapshotStore()
	resumed := false
	graph := GraphDefinition{
		ID:        "ingress-flow",
		Version:   "v1",
		EntryNode: "generate",
		Nodes: map[string]Node{
			"generate": testNode{
				name: "generate",
				kind: NodeKindGenerate,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if !resumed {
						if state.Orchestration.Ingress.Kind != "message" {
							t.Fatalf("expected initial ingress on run, got %#v", state.Orchestration.Ingress)
						}
						return NodeResult{
							Status:          NodeStatusWait,
							RouteKey:        "approved",
							NeedsCheckpoint: true,
							WaitReason:      WaitReasonApproval,
						}, nil
					}
					if state.Orchestration.Ingress.Kind != "resume" || state.Orchestration.Ingress.RunID != "run-ingress" {
						t.Fatalf("expected resume ingress on resume, got %#v", state.Orchestration.Ingress)
					}
					return NodeResult{RouteKey: "approved"}, nil
				},
			},
			"done": testNode{
				name: "done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if state.Orchestration.Ingress.Kind != "resume" {
						t.Fatalf("expected resume ingress to persist to completion, got %#v", state.Orchestration.Ingress)
					}
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{{From: "generate", To: "done", Condition: "approved", Priority: 1}},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	runCtx := ctxutil.WithRuntimeIngress(context.Background(), ctxutil.RuntimeIngressMetadata{
		Kind:    "message",
		Source:  "chat_stream",
		Trigger: "user_message",
	})
	if err := rt.Run(runCtx, RunRequest{
		RunID:   "run-ingress",
		Session: &SessionState{SessionID: "session-ingress"},
		Turn:    TurnState{RunID: "run-ingress", UserMessage: "hello"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	waiting, err := store.LoadLatest(context.Background(), "run-ingress")
	if err != nil {
		t.Fatalf("LoadLatest waiting: %v", err)
	}
	if waiting == nil || waiting.TurnState.Orchestration.Ingress.Kind != "message" {
		t.Fatalf("expected waiting snapshot to persist message ingress, got %#v", waiting)
	}
	if len(waiting.TurnState.Orchestration.Events) != 1 || waiting.TurnState.Orchestration.Events[0].Type != "trigger.received" || waiting.TurnState.Orchestration.Events[0].Summary != "message:user_message" {
		t.Fatalf("expected trigger event on waiting snapshot, got %#v", waiting.TurnState.Orchestration.Events)
	}
	if waiting.TurnState.Orchestration.Events[0].Kind != "message" || waiting.TurnState.Orchestration.Events[0].Source != "chat_stream" || waiting.TurnState.Orchestration.Events[0].Trigger != "user_message" {
		t.Fatalf("expected structured trigger fields on waiting snapshot, got %#v", waiting.TurnState.Orchestration.Events[0])
	}

	resumed = true
	resumeCtx := ctxutil.WithRuntimeIngress(context.Background(), ctxutil.RuntimeIngressMetadata{
		Kind:   "resume",
		Source: "resume_run",
		RunID:  "run-ingress",
	})
	if err := rt.Resume(resumeCtx, "run-ingress"); err != nil {
		t.Fatalf("Resume error: %v", err)
	}

	final, err := store.LoadLatest(context.Background(), "run-ingress")
	if err != nil {
		t.Fatalf("LoadLatest final: %v", err)
	}
	if final == nil || final.TurnState.Orchestration.Ingress.Kind != "resume" || final.TurnState.Orchestration.Ingress.RunID != "run-ingress" {
		t.Fatalf("expected final snapshot to persist resume ingress, got %#v", final)
	}
	if len(final.TurnState.Orchestration.Events) != 2 {
		t.Fatalf("expected trigger history to persist across resume, got %#v", final.TurnState.Orchestration.Events)
	}
	if final.TurnState.Orchestration.Events[1].Type != "trigger.received" || final.TurnState.Orchestration.Events[1].Summary != "resume" {
		t.Fatalf("expected resume trigger event, got %#v", final.TurnState.Orchestration.Events[1])
	}
	if final.TurnState.Orchestration.Events[1].Kind != "resume" || final.TurnState.Orchestration.Events[1].Source != "resume_run" {
		t.Fatalf("expected structured resume trigger event, got %#v", final.TurnState.Orchestration.Events[1])
	}
}

func TestRuntimeWaitAndResume_PreservesRichTurnState(t *testing.T) {
	store := NewInMemorySnapshotStore()
	resumed := false

	graph := GraphDefinition{
		ID:        "rich-state",
		Version:   "v1",
		EntryNode: "generate",
		Nodes: map[string]Node{
			"generate": testNode{
				name: "generate",
				kind: NodeKindGenerate,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if !resumed {
						state.PendingMedia = []map[string]string{{"type": "image_url", "path": "/tmp/diagram.png"}}
						state.Task = TaskState{
							Name:             "plan",
							Mode:             "planning",
							Status:           "running",
							Summary:          "waiting",
							PlanModified:     true,
							CurrentStep:      5,
							ArtifactLastStep: map[string]int{"plan.md": 3},
							SkillLoaded:      "git",
							SkillPath:        "/skills/git",
						}
						state.ForceNextTool = "notify_user"
						state.ActiveSkills = map[string]string{"git": "skill content"}
						return NodeResult{
							Status:          NodeStatusWait,
							RouteKey:        "done",
							NeedsCheckpoint: true,
							WaitReason:      WaitReasonApproval,
						}, nil
					}
					if got := state.PendingMedia[0]["path"]; got != "/tmp/diagram.png" {
						t.Fatalf("pending media drifted across resume: %#v", state.PendingMedia)
					}
					if !state.Task.PlanModified || state.Task.CurrentStep != 5 {
						t.Fatalf("task state drifted across resume: %#v", state.Task)
					}
					if got := state.Task.ArtifactLastStep["plan.md"]; got != 3 {
						t.Fatalf("artifact state drifted across resume: %#v", state.Task.ArtifactLastStep)
					}
					if state.ForceNextTool != "notify_user" {
						t.Fatalf("force tool drifted across resume: %q", state.ForceNextTool)
					}
					if got := state.ActiveSkills["git"]; got != "skill content" {
						t.Fatalf("active skills drifted across resume: %#v", state.ActiveSkills)
					}
					state.OutputDraft = "resumed"
					return NodeResult{RouteKey: "done"}, nil
				},
			},
			"done": testNode{
				name: "done",
				kind: NodeKindDone,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if state.OutputDraft == "" {
						state.OutputDraft = "completed"
					}
					return NodeResult{Status: NodeStatusComplete}, nil
				},
			},
		},
		Edges: []Edge{
			{From: "generate", To: "done", Condition: "done", Priority: 1},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-rich",
		Session: &SessionState{SessionID: "session-rich"},
		Turn:    TurnState{RunID: "run-rich"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-rich")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap == nil || snap.Status != NodeStatusWait {
		t.Fatalf("expected wait snapshot, got %#v", snap)
	}
	if got := snap.TurnState.PendingMedia[0]["path"]; got != "/tmp/diagram.png" {
		t.Fatalf("pending media missing from snapshot: %#v", snap.TurnState.PendingMedia)
	}
	if !snap.TurnState.Task.PlanModified || snap.TurnState.Task.CurrentStep != 5 {
		t.Fatalf("task state missing from snapshot: %#v", snap.TurnState.Task)
	}
	if snap.TurnState.ForceNextTool != "notify_user" {
		t.Fatalf("force tool missing from snapshot: %q", snap.TurnState.ForceNextTool)
	}
	if got := snap.TurnState.ActiveSkills["git"]; got != "skill content" {
		t.Fatalf("active skills missing from snapshot: %#v", snap.TurnState.ActiveSkills)
	}

	resumed = true
	if err := rt.Resume(context.Background(), "run-rich"); err != nil {
		t.Fatalf("Resume error: %v", err)
	}

	snap, err = store.LoadLatest(context.Background(), "run-rich")
	if err != nil {
		t.Fatalf("LoadLatest after resume error: %v", err)
	}
	if snap.Status != NodeStatusComplete {
		t.Fatalf("expected complete snapshot after resume, got %#v", snap.Status)
	}
	if snap.TurnState.OutputDraft != "resumed" {
		t.Fatalf("unexpected final output draft: %q", snap.TurnState.OutputDraft)
	}
}

func TestRuntimeStructuredOutputContractPersistsAcrossCheckpointAndResume(t *testing.T) {
	store := NewInMemorySnapshotStore()
	resumed := false

	graph := GraphDefinition{
		ID:        "structured-output",
		Version:   "v1",
		EntryNode: "reflect",
		Nodes: map[string]Node{
			"reflect": testNode{
				name: "reflect",
				kind: NodeKindCustom,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					if !resumed {
						state.OutputDraft = `{"decision":"revise","reason":"missing test coverage"}`
						return NodeResult{
							Status:           NodeStatusWait,
							RouteKey:         "done",
							NeedsCheckpoint:  true,
							WaitReason:       WaitReasonApproval,
							OutputSchemaName: "reflection.review.v1",
						}, nil
					}
					if state.StructuredOutput.SchemaName != "reflection.review.v1" {
						t.Fatalf("structured output schema drifted across resume: %#v", state.StructuredOutput)
					}
					if !state.StructuredOutput.Valid || state.StructuredOutput.RawJSON == "" {
						t.Fatalf("structured output payload missing across resume: %#v", state.StructuredOutput)
					}
					state.OutputDraft = `{"decision":"accept","reason":"review complete"}`
					return NodeResult{
						Status:           NodeStatusComplete,
						OutputSchemaName: "reflection.review.v1",
					}, nil
				},
			},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-structured",
		Session: &SessionState{SessionID: "session-structured"},
		Turn:    TurnState{RunID: "run-structured"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-structured")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap.ExecutionState.OutputSchemaName != "reflection.review.v1" {
		t.Fatalf("expected execution schema name persisted, got %#v", snap.ExecutionState)
	}
	if !snap.TurnState.StructuredOutput.Valid || snap.TurnState.StructuredOutput.RawJSON == "" {
		t.Fatalf("expected structured output persisted in turn state, got %#v", snap.TurnState.StructuredOutput)
	}

	resumed = true
	if err := rt.Resume(context.Background(), "run-structured"); err != nil {
		t.Fatalf("Resume error: %v", err)
	}

	snap, err = store.LoadLatest(context.Background(), "run-structured")
	if err != nil {
		t.Fatalf("LoadLatest after resume error: %v", err)
	}
	if snap.ExecutionState.OutputSchemaName != "reflection.review.v1" {
		t.Fatalf("expected execution schema name after resume, got %#v", snap.ExecutionState)
	}
	if snap.TurnState.StructuredOutput.RawJSON != `{"decision":"accept","reason":"review complete"}` {
		t.Fatalf("unexpected structured output after resume: %#v", snap.TurnState.StructuredOutput)
	}
}

func TestRuntimePreservesExplicitStructuredOutputWhenDraftIsNotJSON(t *testing.T) {
	store := NewInMemorySnapshotStore()
	graph := GraphDefinition{
		ID:        "structured-preserve",
		Version:   "v1",
		EntryNode: "reflect",
		Nodes: map[string]Node{
			"reflect": testNode{
				name: "reflect",
				kind: NodeKindReflect,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.OutputDraft = "human readable answer"
					state.StructuredOutput = StructuredOutputState{
						SchemaName: "reflection.review.v1",
						RawJSON:    `{"decision":"accept","reason":"ok"}`,
						Valid:      true,
					}
					return NodeResult{
						Status:           NodeStatusComplete,
						OutputSchemaName: "reflection.review.v1",
					}, nil
				},
			},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}
	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-structured-preserve",
		Session: &SessionState{SessionID: "session-structured-preserve"},
		Turn:    TurnState{RunID: "run-structured-preserve"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-structured-preserve")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap.TurnState.StructuredOutput.RawJSON != `{"decision":"accept","reason":"ok"}` {
		t.Fatalf("expected explicit structured output to survive, got %#v", snap.TurnState.StructuredOutput)
	}
}

func TestRuntimeRejectsWaitWithoutSupportedReason(t *testing.T) {
	tests := []struct {
		name       string
		waitReason WaitReason
		wantErr    string
	}{
		{name: "missing", waitReason: WaitReasonNone, wantErr: "wait status requires wait reason"},
		{name: "external", waitReason: WaitReasonExternal, wantErr: `unsupported wait reason "external"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemorySnapshotStore()
			graph := GraphDefinition{
				ID:        "invalid-wait",
				Version:   "v1",
				EntryNode: "waiter",
				Nodes: map[string]Node{
					"waiter": testNode{
						name: "waiter",
						kind: NodeKindCustom,
						run: func(_ context.Context, _ *RuntimeContext, _ *TurnState) (NodeResult, error) {
							return NodeResult{
								Status:          NodeStatusWait,
								NeedsCheckpoint: true,
								WaitReason:      tc.waitReason,
							}, nil
						},
					},
				},
			}

			rt, err := NewRuntime(graph, store)
			if err != nil {
				t.Fatalf("NewRuntime error: %v", err)
			}

			err = rt.Run(context.Background(), RunRequest{
				RunID:   "run-invalid-" + tc.name,
				Session: &SessionState{SessionID: "session-invalid"},
				Turn:    TurnState{RunID: "run-invalid-" + tc.name},
			})
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}

			snap, err := store.LoadLatest(context.Background(), "run-invalid-"+tc.name)
			if err != nil {
				t.Fatalf("LoadLatest error: %v", err)
			}
			if snap == nil || snap.Status != NodeStatusFatal {
				t.Fatalf("expected fatal snapshot, got %#v", snap)
			}
			if snap.ExecutionState.LastError != tc.wantErr {
				t.Fatalf("expected fatal last error %q, got %q", tc.wantErr, snap.ExecutionState.LastError)
			}
		})
	}
}

func TestRuntimeAcceptsUserInputWaitReason(t *testing.T) {
	store := NewInMemorySnapshotStore()
	graph := GraphDefinition{
		ID:        "user-input-wait",
		Version:   "v1",
		EntryNode: "plan",
		Nodes: map[string]Node{
			"plan": testNode{
				name: "plan",
				kind: NodeKindPlan,
				run: func(_ context.Context, _ *RuntimeContext, state *TurnState) (NodeResult, error) {
					state.Intelligence.Planning = PlanningState{
						Required:         true,
						ReviewRequired:   true,
						Trigger:          "mode_force_plan",
						MissingArtifacts: []string{"plan.md"},
					}
					return NodeResult{
						Status:          NodeStatusWait,
						NeedsCheckpoint: true,
						WaitReason:      WaitReasonUserInput,
						RouteKey:        "plan",
						ObservedState:   "plan",
					}, nil
				},
			},
		},
	}

	rt, err := NewRuntime(graph, store)
	if err != nil {
		t.Fatalf("NewRuntime error: %v", err)
	}

	if err := rt.Run(context.Background(), RunRequest{
		RunID:   "run-user-input",
		Session: &SessionState{SessionID: "session-user-input"},
		Turn:    TurnState{RunID: "run-user-input"},
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	snap, err := store.LoadLatest(context.Background(), "run-user-input")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if snap == nil || snap.Status != NodeStatusWait {
		t.Fatalf("expected wait snapshot, got %#v", snap)
	}
	if snap.ExecutionState.WaitReason != WaitReasonUserInput {
		t.Fatalf("expected user input wait reason, got %#v", snap.ExecutionState)
	}
	if !snap.TurnState.Intelligence.Planning.Required || !snap.TurnState.Intelligence.Planning.ReviewRequired {
		t.Fatalf("expected planning state persisted, got %#v", snap.TurnState.Intelligence.Planning)
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
