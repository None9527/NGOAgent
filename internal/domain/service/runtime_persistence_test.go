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
