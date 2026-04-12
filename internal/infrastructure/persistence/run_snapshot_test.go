package persistence

import (
	"context"
	"encoding/json"
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
			Task: graphruntime.TaskState{
				Summary: "awaiting approval",
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status: graphruntime.NodeStatusWait,
			PendingApproval: &graphruntime.ApprovalState{
				ID:          "approval-1",
				ToolName:    "write_file",
				Args:        map[string]any{"path": "/tmp/x.go"},
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
	if got := loaded.ExecutionState.PendingApproval.Args["path"]; got != "/tmp/x.go" {
		t.Fatalf("unexpected approval args: %#v", loaded.ExecutionState.PendingApproval.Args)
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
	if loaded.RunID != "run-a" {
		t.Fatalf("expected pending wait snapshot to win session lookup, got %q", loaded.RunID)
	}
}

func TestRunSnapshotStore_LoadsFromRuntimeTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-read.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-runtime",
		SessionID:    "session-runtime",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		Cursor: graphruntime.ExecutionCursor{
			GraphID:      "agent_loop",
			GraphVersion: "v1alpha1",
			CurrentNode:  "guard",
			Step:         3,
			RouteKey:     "needs_input",
		},
		TurnState: graphruntime.TurnState{
			RunID:       "run-runtime",
			UserMessage: "resume me",
		},
		ExecutionState: graphruntime.ExecutionState{
			StartedAt:  now.Add(-time.Minute),
			UpdatedAt:  now,
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-runtime",
				ToolName: "write_file",
				Args:     map[string]any{"path": "runtime.go"},
				Reason:   "needs confirmation",
			},
		},
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := store.LoadLatest(context.Background(), snap.RunID)
	if err != nil {
		t.Fatalf("LoadLatest runtime read error: %v", err)
	}
	if loaded == nil || loaded.RunID != snap.RunID {
		t.Fatalf("expected runtime-backed snapshot, got %#v", loaded)
	}
	if loaded.ExecutionState.PendingApproval == nil || loaded.ExecutionState.PendingApproval.ID != "approval-runtime" {
		t.Fatalf("expected approval restored from runtime tables, got %#v", loaded.ExecutionState.PendingApproval)
	}

	loadedBySession, err := store.LoadLatestBySession(context.Background(), snap.SessionID)
	if err != nil {
		t.Fatalf("LoadLatestBySession runtime read error: %v", err)
	}
	if loadedBySession == nil || loadedBySession.RunID != snap.RunID {
		t.Fatalf("expected runtime-backed session snapshot, got %#v", loadedBySession)
	}
}

func TestRunSnapshotStore_ResolvesWaitRowAfterCompletion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-waits.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-wait-row",
		SessionID:    "session-wait-row",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save waiting snapshot error: %v", err)
	}

	var wait RunWaitRecord
	if err := db.First(&wait, "run_id = ?", snap.RunID).Error; err != nil {
		t.Fatalf("load wait row: %v", err)
	}
	if wait.Status != "pending" || wait.WaitType != string(graphruntime.WaitReasonBarrier) {
		t.Fatalf("unexpected wait row after pending save: %#v", wait)
	}

	snap.Status = graphruntime.NodeStatusComplete
	snap.ExecutionState.Status = graphruntime.NodeStatusComplete
	snap.ExecutionState.WaitReason = graphruntime.WaitReasonNone
	snap.UpdatedAt = now.Add(time.Minute)
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save completion snapshot error: %v", err)
	}

	if err := db.First(&wait, "run_id = ?", snap.RunID).Error; err != nil {
		t.Fatalf("reload wait row: %v", err)
	}
	if wait.Status != "resolved" || wait.ResolvedAt == nil {
		t.Fatalf("expected resolved wait row, got %#v", wait)
	}
}

func TestRunSnapshotStore_LoadLatestBySession_PrefersPendingWaitOverNewerCompletedRun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-prefer-wait.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-pending",
		SessionID:    "session-priority",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-priority",
				ToolName: "write_file",
				Args:     map[string]any{"path": "pending.go"},
				Reason:   "needs confirmation",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("Save waiting snapshot error: %v", err)
	}

	completed := &graphruntime.RunSnapshot{
		RunID:        "run-complete",
		SessionID:    "session-priority",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		ExecutionState: graphruntime.ExecutionState{
			Status: graphruntime.NodeStatusComplete,
		},
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), completed); err != nil {
		t.Fatalf("Save completed snapshot error: %v", err)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), "session-priority")
	if err != nil {
		t.Fatalf("LoadLatestBySession error: %v", err)
	}
	if loaded == nil || loaded.RunID != "run-pending" {
		t.Fatalf("expected pending run to win reconnect lookup, got %#v", loaded)
	}
	if loaded.ExecutionState.PendingApproval == nil || loaded.ExecutionState.PendingApproval.ID != "approval-priority" {
		t.Fatalf("expected pending approval snapshot, got %#v", loaded.ExecutionState.PendingApproval)
	}
}

func TestRunSnapshotStore_LoadLatest_ReturnsNilForUnknownRun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-missing.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	loaded, err := store.LoadLatest(context.Background(), "missing-run")
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil for missing run, got %#v", loaded)
	}
}

func TestRunSnapshotStore_PersistsOrchestrationTopologyAndEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-orchestration.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-parent",
		SessionID:    "session-parent",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-parent",
			Orchestration: graphruntime.OrchestrationState{
				ParentRunID: "root-run",
				ChildRunIDs: []string{"run-child"},
				Handoffs: []graphruntime.HandoffState{{
					TargetRunID: "run-child",
					Kind:        "subagent_task",
					PayloadJSON: `{"task_name":"research"}`,
				}},
				Events: []graphruntime.OrchestrationEventState{{
					Type:      "child.spawned",
					RunID:     "run-child",
					SourceRun: "run-parent",
					At:        now,
					Summary:   "research",
				}},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	var run AgentRunRecord
	if err := db.First(&run, "id = ?", snap.RunID).Error; err != nil {
		t.Fatalf("load agent run: %v", err)
	}
	if run.ParentRunID == nil || *run.ParentRunID != "root-run" {
		t.Fatalf("expected persisted parent run id, got %#v", run.ParentRunID)
	}

	var events []RunEventRecord
	if err := db.Where("run_id = ?", snap.RunID).Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatalf("load runtime events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "child.spawned" {
		t.Fatalf("expected persisted runtime events, got %#v", events)
	}

	sessionRuns, err := store.ListBySession(context.Background(), snap.SessionID)
	if err != nil {
		t.Fatalf("ListBySession error: %v", err)
	}
	if len(sessionRuns) != 1 || sessionRuns[0].TurnState.Orchestration.ChildRunIDs[0] != "run-child" {
		t.Fatalf("expected runtime session listing with child topology, got %#v", sessionRuns)
	}

	childRuns, err := store.ListByParentRun(context.Background(), "root-run")
	if err != nil {
		t.Fatalf("ListByParentRun error: %v", err)
	}
	if len(childRuns) != 1 || childRuns[0].RunID != snap.RunID {
		t.Fatalf("expected parent run query to include run, got %#v", childRuns)
	}
}

func TestRunSnapshotStore_RuntimeEventsAreAppendOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-events-append.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-events",
		SessionID:    "session-events",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-events",
			Orchestration: graphruntime.OrchestrationState{
				Events: []graphruntime.OrchestrationEventState{{
					Type:      "child.spawned",
					RunID:     "child-1",
					SourceRun: "run-events",
					At:        now,
					Summary:   "first",
				}},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("first Save error: %v", err)
	}

	snap.TurnState.Orchestration.Events = []graphruntime.OrchestrationEventState{
		{
			Type:      "child.rewritten",
			RunID:     "child-1",
			SourceRun: "run-events",
			At:        now.Add(time.Minute),
			Summary:   "mutated",
		},
		{
			Type:      "child.completed",
			RunID:     "child-1",
			SourceRun: "run-events",
			At:        now.Add(2 * time.Minute),
			Summary:   "second",
		},
	}
	snap.UpdatedAt = now.Add(2 * time.Minute)
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("second Save error: %v", err)
	}

	var events []RunEventRecord
	if err := db.Where("run_id = ?", snap.RunID).Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatalf("load runtime events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 runtime events, got %#v", events)
	}
	if events[0].EventType != "child.spawned" {
		t.Fatalf("expected first event to remain unchanged, got %#v", events[0])
	}
	if events[1].EventType != "child.completed" {
		t.Fatalf("expected appended event to be preserved, got %#v", events[1])
	}
}

func TestRunSnapshotStore_RoundTripsStructuredOrchestrationEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshots-runtime-events-structured.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	store := NewRunSnapshotStore(db)
	now := time.Now().UTC().Round(time.Second)
	snap := &graphruntime.RunSnapshot{
		RunID:        "run-events-structured",
		SessionID:    "session-events-structured",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-events-structured",
			Orchestration: graphruntime.OrchestrationState{
				Ingress: graphruntime.IngressState{
					Kind:         "decision",
					Source:       "decision_apply",
					Trigger:      "plan_review",
					RunID:        "run-events-structured",
					DecisionKind: "plan_review",
					Decision:     "approved",
				},
				Events: []graphruntime.OrchestrationEventState{
					{
						Type:         "trigger.received",
						Kind:         "decision",
						Source:       "decision_apply",
						Trigger:      "plan_review",
						RunID:        "run-events-structured",
						DecisionKind: "plan_review",
						Decision:     "approved",
						At:           now,
						Summary:      "decision:plan_review",
						PayloadJSON:  `{"decision":"approved"}`,
					},
					{
						Type:        "barrier.timeout",
						Kind:        "barrier",
						Source:      "barrier",
						Trigger:     "timeout",
						RunID:       "run-events-structured",
						BarrierID:   "barrier-structured",
						At:          now.Add(time.Minute),
						Summary:     "timeout",
						PayloadJSON: `{"members":2}`,
					},
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
		},
		CreatedAt: now,
		UpdatedAt: now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := store.LoadLatest(context.Background(), snap.RunID)
	if err != nil {
		t.Fatalf("LoadLatest error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded snapshot")
	}
	if loaded.TurnState.Orchestration.Ingress.DecisionKind != "plan_review" || loaded.TurnState.Orchestration.Ingress.Decision != "approved" {
		t.Fatalf("expected ingress decision fields to round-trip, got %#v", loaded.TurnState.Orchestration.Ingress)
	}
	if len(loaded.TurnState.Orchestration.Events) != 2 {
		t.Fatalf("expected 2 structured events, got %#v", loaded.TurnState.Orchestration.Events)
	}
	if loaded.TurnState.Orchestration.Events[0].DecisionKind != "plan_review" || loaded.TurnState.Orchestration.Events[0].PayloadJSON != `{"decision":"approved"}` {
		t.Fatalf("expected trigger event decision fields to round-trip, got %#v", loaded.TurnState.Orchestration.Events[0])
	}
	if loaded.TurnState.Orchestration.Events[1].BarrierID != "barrier-structured" || loaded.TurnState.Orchestration.Events[1].Trigger != "timeout" {
		t.Fatalf("expected barrier event fields to round-trip, got %#v", loaded.TurnState.Orchestration.Events[1])
	}

	sessionRuns, err := store.ListBySession(context.Background(), snap.SessionID)
	if err != nil {
		t.Fatalf("ListBySession error: %v", err)
	}
	if len(sessionRuns) != 1 || len(sessionRuns[0].TurnState.Orchestration.Events) != 2 {
		t.Fatalf("expected session listing to retain structured events, got %#v", sessionRuns)
	}

	var records []RunEventRecord
	if err := db.Where("run_id = ?", snap.RunID).Order("seq ASC").Find(&records).Error; err != nil {
		t.Fatalf("load runtime event records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 runtime event records, got %#v", records)
	}
	var persisted graphruntime.OrchestrationEventState
	if err := json.Unmarshal([]byte(records[1].PayloadJSON), &persisted); err != nil {
		t.Fatalf("unmarshal persisted runtime event payload: %v", err)
	}
	if persisted.BarrierID != "barrier-structured" || persisted.PayloadJSON != `{"members":2}` {
		t.Fatalf("expected persisted event payload to keep structured fields, got %#v", persisted)
	}
}
