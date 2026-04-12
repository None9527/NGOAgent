package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
)

func TestHandleReconnect_WithPersistenceStore_ReplaysApproval(t *testing.T) {
	db, err := persistence.Open(filepath.Join(t.TempDir(), "runtime-reconnect.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-persist-approval",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-persist-approval"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-persist",
				ToolName: "write_file",
				Args:     map[string]any{"path": "persist.go"},
				Reason:   "needs confirmation",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("Save: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect: %v", err)
	}
	if !handled {
		t.Fatal("expected reconnect to be handled")
	}
	if len(delta.approvals) != 1 || delta.approvals[0].ID != "approval-persist" {
		t.Fatalf("expected replayed approval, got %#v", delta.approvals)
	}
}

func TestHandleReconnect_WithPersistenceStore_PrefersPendingWaitOverCompletedRun(t *testing.T) {
	db, err := persistence.Open(filepath.Join(t.TempDir(), "runtime-reconnect-priority.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	now := time.Now().UTC().Round(time.Second)
	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-persist-wait",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-persist-wait"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-priority",
				ToolName: "write_file",
				Args:     map[string]any{"path": "priority.go"},
				Reason:   "still waiting",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("Save waiting: %v", err)
	}

	completed := &graphruntime.RunSnapshot{
		RunID:        "run-persist-complete",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		TurnState:    graphruntime.TurnState{RunID: "run-persist-complete"},
		ExecutionState: graphruntime.ExecutionState{
			Status: graphruntime.NodeStatusComplete,
		},
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), completed); err != nil {
		t.Fatalf("Save completed: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect: %v", err)
	}
	if !handled {
		t.Fatal("expected reconnect to be handled")
	}
	if len(delta.approvals) != 1 || delta.approvals[0].ID != "approval-priority" {
		t.Fatalf("expected pending wait to win reconnect lookup, got %#v", delta.approvals)
	}
}

func TestHandleReconnect_WithPersistenceStore_ReplaysPlanReviewAndPreservesStructuredEvents(t *testing.T) {
	db, err := persistence.Open(filepath.Join(t.TempDir(), "runtime-reconnect-plan-review.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	now := time.Now().UTC().Round(time.Second)
	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-persist-plan-review",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-persist-plan-review",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:         true,
					ReviewRequired:   true,
					Trigger:          "resume_run",
					MissingArtifacts: []string{"plan.md"},
				},
			},
			Orchestration: graphruntime.OrchestrationState{
				Ingress: graphruntime.IngressState{
					Kind:    "message",
					Source:  "chat_stream",
					Trigger: "user_message",
					RunID:   "run-persist-plan-review",
				},
				Events: []graphruntime.OrchestrationEventState{
					{
						Type:        "trigger.received",
						Kind:        "message",
						Source:      "chat_stream",
						Trigger:     "user_message",
						RunID:       "run-persist-plan-review",
						At:          now,
						Summary:     "message:user_message",
						PayloadJSON: `{"message":"review plan"}`,
					},
					{
						Type:        "barrier.timeout",
						Kind:        "barrier",
						Source:      "barrier",
						Trigger:     "timeout",
						RunID:       "run-persist-plan-review",
						BarrierID:   "barrier-plan",
						At:          now.Add(time.Minute),
						Summary:     "timeout",
						PayloadJSON: `{"members":1}`,
					},
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
		},
		CreatedAt: now,
		UpdatedAt: now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("Save: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect: %v", err)
	}
	if !handled {
		t.Fatal("expected reconnect to replay plan review")
	}
	if len(delta.reviews) != 1 {
		t.Fatalf("expected one replayed plan review, got %#v", delta.reviews)
	}
	if delta.reviews[0].message != "Planning trigger: resume_run" {
		t.Fatalf("unexpected plan review message: %#v", delta.reviews[0])
	}
	if len(delta.reviews[0].paths) != 1 || delta.reviews[0].paths[0] != "plan.md" {
		t.Fatalf("unexpected plan review paths: %#v", delta.reviews[0].paths)
	}

	reloaded, err := store.LoadLatest(context.Background(), waiting.RunID)
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	if reloaded == nil || len(reloaded.TurnState.Orchestration.Events) != 2 {
		t.Fatalf("expected structured events to survive reconnect replay, got %#v", reloaded)
	}
	if reloaded.TurnState.Orchestration.Events[0].Type != "trigger.received" || reloaded.TurnState.Orchestration.Events[0].PayloadJSON != `{"message":"review plan"}` {
		t.Fatalf("expected trigger event to remain intact, got %#v", reloaded.TurnState.Orchestration.Events[0])
	}
	if reloaded.TurnState.Orchestration.Events[1].BarrierID != "barrier-plan" || reloaded.TurnState.Orchestration.Events[1].Trigger != "timeout" {
		t.Fatalf("expected barrier event to remain intact, got %#v", reloaded.TurnState.Orchestration.Events[1])
	}
}
