package persistence

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

func TestRepositoryDeleteConversation_RemovesRuntimeData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "repository-delete.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	repo := NewRepository(db)
	store := NewRunSnapshotStore(db)
	conv, err := repo.CreateConversation("cli", "runtime cleanup")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-delete-conv",
		SessionID:    conv.ID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-delete",
				ToolName: "write_file",
				Args:     map[string]any{"path": "delete.go"},
				Reason:   "cleanup",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save snapshot: %v", err)
	}

	if err := repo.DeleteConversation(conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	var count int64
	for _, model := range []any{
		&AgentRunRecord{},
		&RunCheckpointRecord{},
		&RunWaitRecord{},
		&RunEventRecord{},
	} {
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatalf("count %T: %v", model, err)
		}
		if count != 0 {
			t.Fatalf("expected %T rows to be removed, count=%d", model, count)
		}
	}
}
