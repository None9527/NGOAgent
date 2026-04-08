package service

import (
	"context"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type fakeSecurityChecker struct {
	pending []ApprovalSnapshot
}

func (f *fakeSecurityChecker) BeforeToolCall(_ context.Context, _ string, _ map[string]any) (SecurityDecision, string) {
	return SecurityAllow, ""
}
func (f *fakeSecurityChecker) AfterToolCall(_ context.Context, _ string, _ string, _ error) {}
func (f *fakeSecurityChecker) RequestApproval(_ string, _ map[string]any, _ string) *ApprovalTicket {
	return nil
}
func (f *fakeSecurityChecker) ListPendingApprovals() []ApprovalSnapshot { return f.pending }
func (f *fakeSecurityChecker) CleanupPending(_ string)                  {}

func TestNewAgentLoopGraph_ValidDefinition(t *testing.T) {
	loop := &AgentLoop{}
	graph := NewAgentLoopGraph(loop)

	if err := graph.Validate(); err != nil {
		t.Fatalf("graph should validate: %v", err)
	}
	if graph.ID != "agent_loop" {
		t.Fatalf("unexpected graph id: %s", graph.ID)
	}
	if graph.EntryNode != "prepare" {
		t.Fatalf("unexpected entry node: %s", graph.EntryNode)
	}
}

func TestGraphAdapterSyncsLoopStateIntoGraphState(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Security: &fakeSecurityChecker{
			pending: []ApprovalSnapshot{{
				ID:        "approval-1",
				ToolName:  "write_file",
				Reason:    "needs confirmation",
				Requested: time.Unix(123, 0),
			}},
		},
	})
	loop.state = StatePrepare
	loop.mode = ModePermissions{Name: "agentic", SelfReview: true}
	loop.outputContinuations = 2
	loop.compactCount = 1
	loop.ephemerals = []string{"remember this"}
	loop.task.RecordBoundary("write feature", "plan", "running", "implement runtime")
	loop.SetActiveBarrier(NewSubagentBarrierFromState(loop, nil, graphruntime.BarrierState{
		ID:           "barrier-1",
		PendingCount: 1,
		Members: []graphruntime.BarrierMemberState{{
			RunID:    "run-a",
			TaskName: "task a",
			Status:   "running",
		}},
	}))
	loop.history = []llm.Message{{
		Role:      "assistant",
		Content:   "draft answer",
		Reasoning: "analysis",
		Attachments: []llm.Attachment{
			{Path: "/tmp/example.txt"},
		},
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "write_file",
				Arguments: `{"path":"a.go"}`,
			},
		}},
	}}

	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{}

	adapter.syncToGraphState(state, exec)

	if state.Mode != "chat" {
		t.Fatalf("expected loop default mode to sync as chat, got %q", state.Mode)
	}

	state.Mode = "agentic"
	adapter.syncFromGraphState(state, exec)
	adapter.syncToGraphState(state, exec)

	if state.Mode != "agentic" {
		t.Fatalf("expected mode sync, got %q", state.Mode)
	}
	if state.CurrentPlan != "implement runtime" {
		t.Fatalf("unexpected current plan: %q", state.CurrentPlan)
	}
	if len(state.Ephemerals) != 1 || state.Ephemerals[0] != "remember this" {
		t.Fatalf("unexpected ephemerals: %#v", state.Ephemerals)
	}
	if len(state.Attachments) != 1 || state.Attachments[0] != "/tmp/example.txt" {
		t.Fatalf("unexpected attachments: %#v", state.Attachments)
	}
	if len(state.ToolCalls) != 1 || state.ToolCalls[0].Name != "write_file" {
		t.Fatalf("unexpected tool calls: %#v", state.ToolCalls)
	}
	if exec.Continuation.Count != 2 {
		t.Fatalf("unexpected continuation count: %d", exec.Continuation.Count)
	}
	if exec.PendingApproval == nil || exec.PendingApproval.ID != "approval-1" {
		t.Fatalf("expected pending approval to sync, got %#v", exec.PendingApproval)
	}
	if exec.PendingBarrier == nil || exec.PendingBarrier.ID != "barrier-1" {
		t.Fatalf("expected pending barrier to sync, got %#v", exec.PendingBarrier)
	}
	if !state.Reflection.Required {
		t.Fatal("expected reflection requirement to sync from mode")
	}
}

func TestHandleDone_CompletesWithoutPendingWake(t *testing.T) {
	loop := NewAgentLoop(Deps{Delta: &mockDeltaSink{}})
	rs := &runState{}

	result, err := loop.handleDone(context.Background(), rs)
	if err != nil {
		t.Fatalf("handleDone error: %v", err)
	}
	if result.Status != graphruntime.NodeStatusComplete {
		t.Fatalf("expected complete status, got %#v", result)
	}
}

func TestHydratePendingBarrier_RestoresFromSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	loop := NewAgentLoop(Deps{SnapshotStore: store})

	snap := &graphruntime.RunSnapshot{
		RunID:        "run-1",
		SessionID:    "",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-1"},
		ExecutionState: graphruntime.ExecutionState{
			Status: graphruntime.NodeStatusWait,
			PendingBarrier: &graphruntime.BarrierState{
				ID:           "barrier-restore",
				PendingCount: 1,
				Members: []graphruntime.BarrierMemberState{{
					RunID:    "sub-1",
					TaskName: "research",
					Status:   "running",
				}},
			},
		},
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	if err := loop.hydratePendingBarrier(context.Background(), "run-1"); err != nil {
		t.Fatalf("hydrate pending barrier: %v", err)
	}

	restored := loop.activeBarrierSnapshot()
	if restored == nil {
		t.Fatal("expected active barrier after hydration")
	}
	if restored.ID != "barrier-restore" {
		t.Fatalf("unexpected barrier id: %s", restored.ID)
	}
	if len(restored.Members) != 1 || restored.Members[0].RunID != "sub-1" {
		t.Fatalf("unexpected restored members: %#v", restored.Members)
	}
}
