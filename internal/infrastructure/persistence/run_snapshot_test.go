package persistence

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

func TestRunSnapshotStore_SaveLoadDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)

	snap := &graphruntime.RunSnapshot{
		RunID:        "run-1",
		SessionID:    "session-1",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		Cursor: graphruntime.ExecutionCursor{
			GraphID:      "agent_loop",
			GraphVersion: "v1alpha1",
			CurrentNode:  "generate",
			Step:         2,
			RouteKey:     "approved",
		},
		TurnState: graphruntime.TurnState{
			RunID:       "run-1",
			UserMessage: "modify file",
			CurrentPlan: "awaiting approval",
		},
		ExecutionState: graphruntime.ExecutionState{
			Status: graphruntime.NodeStatusWait,
			PendingApproval: &graphruntime.ApprovalState{
				ID:          "approval-1",
				ToolName:    "write_file",
				Reason:      "needs confirmation",
				RequestedAt: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := store.LoadLatest(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded snapshot")
	}
	if loaded.RunID != snap.RunID {
		t.Fatalf("unexpected run id: got %q want %q", loaded.RunID, snap.RunID)
	}
	if loaded.Cursor.CurrentNode != "generate" {
		t.Fatalf("unexpected current node: %q", loaded.Cursor.CurrentNode)
	}
	if loaded.ExecutionState.PendingApproval == nil {
		t.Fatal("expected pending approval")
	}
	if loaded.ExecutionState.PendingApproval.ToolName != "write_file" {
		t.Fatalf("unexpected approval tool: %q", loaded.ExecutionState.PendingApproval.ToolName)
	}

	snap.Status = graphruntime.NodeStatusComplete
	snap.Cursor.CurrentNode = "done"
	snap.ExecutionState.PendingApproval = nil
	snap.UpdatedAt = now.Add(time.Minute)
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save update error: %v", err)
	}

	loaded, err = store.LoadLatest(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("LoadLatest after update error: %v", err)
	}
	if loaded.Status != graphruntime.NodeStatusComplete {
		t.Fatalf("unexpected status after update: %s", loaded.Status)
	}
	if loaded.Cursor.CurrentNode != "done" {
		t.Fatalf("unexpected node after update: %q", loaded.Cursor.CurrentNode)
	}
	if loaded.ExecutionState.PendingApproval != nil {
		t.Fatal("expected pending approval to be cleared")
	}

	if err := store.Delete(context.Background(), "run-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	loaded, err = store.LoadLatest(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("LoadLatest after delete error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestRunSnapshotStore_LoadLatestBySession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-by-session.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)

	first := &graphruntime.RunSnapshot{
		RunID:        "run-a",
		SessionID:    "session-a",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		UpdatedAt:    now,
	}
	second := &graphruntime.RunSnapshot{
		RunID:        "run-b",
		SessionID:    "session-a",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		UpdatedAt:    now.Add(time.Minute),
	}

	if err := store.Save(context.Background(), first); err != nil {
		t.Fatalf("Save first error: %v", err)
	}
	if err := store.Save(context.Background(), second); err != nil {
		t.Fatalf("Save second error: %v", err)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("LoadLatestBySession error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected latest snapshot by session")
	}
	if loaded.RunID != "run-b" {
		t.Fatalf("unexpected latest run id: %q", loaded.RunID)
	}
}
