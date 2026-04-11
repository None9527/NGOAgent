package persistence

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenRunsUnifiedMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrations.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	expectedTables := []string{
		"conversations",
		"evo_traces",
		"worker_transcripts",
		"session_token_usages",
		"schema_migrations",
		"messages",
		"message_tool_calls",
		"message_attachments",
		"artifacts",
		"agent_runs",
		"run_checkpoints",
		"run_waits",
		"run_events",
	}
	for _, table := range expectedTables {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	for _, dropped := range []string{"history_messages", "run_snapshot_records"} {
		if db.Migrator().HasTable(dropped) {
			t.Fatalf("expected legacy table %q to be dropped", dropped)
		}
	}

	var fkEnabled int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&fkEnabled).Error; err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Fatalf("expected foreign_keys pragma enabled, got %d", fkEnabled)
	}

	var count int64
	if err := db.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != int64(len(schemaMigrationSteps())) {
		t.Fatalf("unexpected schema migration count: got %d want %d", count, len(schemaMigrationSteps()))
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrations-idempotent.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations second pass: %v", err)
	}

	var count int64
	if err := db.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != int64(len(schemaMigrationSteps())) {
		t.Fatalf("unexpected schema migration count: got %d want %d", count, len(schemaMigrationSteps()))
	}
}

func TestRunMigrations_BackfillsLegacySnapshotsIntoRuntimeTablesAndDropsLegacyTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrations-backfill.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}

	if err := db.AutoMigrate(
		&SchemaMigration{},
		&Conversation{},
		&AgentRunRecord{},
		&RunCheckpointRecord{},
		&RunWaitRecord{},
		&RunEventRecord{},
		&MessageRecord{},
		&MessageToolCallRecord{},
		&MessageAttachmentRecord{},
	); err != nil {
		t.Fatalf("AutoMigrate baseline: %v", err)
	}
	if err := db.Exec(`CREATE TABLE run_snapshot_records (
		run_id TEXT PRIMARY KEY,
		session_id TEXT,
		graph_id TEXT,
		graph_version TEXT,
		status TEXT,
		cursor_json TEXT,
		turn_state_json TEXT,
		execution_state_json TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create legacy run_snapshot_records: %v", err)
	}
	if err := db.Create(&SchemaMigration{
		Version:   1,
		Name:      "baseline_unified_schema",
		AppliedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed v1 migration: %v", err)
	}

	now := time.Now().UTC().Round(time.Second)
	cursorJSON, _ := json.Marshal(graphruntime.ExecutionCursor{
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		CurrentNode:  "guard",
		Step:         4,
		RouteKey:     "await_approval",
	})
	turnJSON, _ := json.Marshal(graphruntime.TurnState{
		RunID:       "legacy-run",
		UserMessage: "migrate me",
	})
	execJSON, _ := json.Marshal(graphruntime.ExecutionState{
		StartedAt:  now.Add(-time.Minute),
		UpdatedAt:  now,
		Status:     graphruntime.NodeStatusWait,
		WaitReason: graphruntime.WaitReasonApproval,
		PendingApproval: &graphruntime.ApprovalState{
			ID:       "approval-migrate",
			ToolName: "write_file",
			Args:     map[string]any{"path": "migrated.go"},
			Reason:   "legacy wait",
		},
	})
	if err := db.Exec(
		`INSERT INTO run_snapshot_records
		(run_id, session_id, graph_id, graph_version, status, cursor_json, turn_state_json, execution_state_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-run", "legacy-session", "agent_loop", "v1alpha1", string(graphruntime.NodeStatusWait),
		string(cursorJSON), string(turnJSON), string(execJSON), now.Add(-time.Minute), now,
	).Error; err != nil {
		t.Fatalf("seed legacy snapshot: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var run AgentRunRecord
	if err := db.First(&run, "id = ?", "legacy-run").Error; err != nil {
		t.Fatalf("load backfilled run: %v", err)
	}
	if run.ConversationID != "legacy-session" || run.WaitReason != string(graphruntime.WaitReasonApproval) {
		t.Fatalf("unexpected backfilled run: %#v", run)
	}

	var checkpoint RunCheckpointRecord
	if err := db.First(&checkpoint, "run_id = ?", "legacy-run").Error; err != nil {
		t.Fatalf("load backfilled checkpoint: %v", err)
	}
	if checkpoint.Status != string(graphruntime.NodeStatusWait) {
		t.Fatalf("unexpected checkpoint status: %#v", checkpoint)
	}

	var wait RunWaitRecord
	if err := db.First(&wait, "run_id = ?", "legacy-run").Error; err != nil {
		t.Fatalf("load backfilled wait: %v", err)
	}
	if wait.Status != "pending" || wait.WaitType != string(graphruntime.WaitReasonApproval) {
		t.Fatalf("unexpected backfilled wait: %#v", wait)
	}
	if db.Migrator().HasTable("run_snapshot_records") {
		t.Fatal("expected legacy run_snapshot_records table to be dropped")
	}
}

func TestRunMigrations_BackfillsLegacyHistoryIntoMessagesAndDropsLegacyTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrations-history-backfill.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}

	if err := db.AutoMigrate(
		&SchemaMigration{},
		&Conversation{},
		&MessageRecord{},
		&MessageToolCallRecord{},
		&MessageAttachmentRecord{},
	); err != nil {
		t.Fatalf("AutoMigrate baseline: %v", err)
	}
	if err := db.Exec(`CREATE TABLE history_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		role TEXT,
		content TEXT,
		tool_calls TEXT,
		tool_call_id TEXT,
		token_count INTEGER,
		reasoning TEXT,
		attachments TEXT,
		created_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create legacy history_messages: %v", err)
	}
	if err := db.Create(&SchemaMigration{Version: 1, Name: "baseline_unified_schema", AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("seed v1 migration: %v", err)
	}
	if err := db.Create(&SchemaMigration{Version: 2, Name: "backfill_runtime_from_legacy_snapshots", AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("seed v2 migration: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO history_messages
		(session_id, role, content, tool_calls, tool_call_id, token_count, reasoning, attachments, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"history-session",
		"assistant",
		"legacy history",
		`[{"id":"call-9","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"x.go\"}"}}]`,
		"",
		0,
		"",
		`[{"type":"file","path":"/tmp/x.txt","mime_type":"text/plain","name":"x.txt"}]`,
		time.Now().UTC(),
	).Error; err != nil {
		t.Fatalf("seed legacy history: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var msg MessageRecord
	if err := db.First(&msg, "conversation_id = ?", "history-session").Error; err != nil {
		t.Fatalf("load backfilled message: %v", err)
	}
	if msg.ContentText != "legacy history" {
		t.Fatalf("unexpected backfilled message: %#v", msg)
	}

	var tools int64
	if err := db.Model(&MessageToolCallRecord{}).Where("message_id = ?", msg.ID).Count(&tools).Error; err != nil {
		t.Fatalf("count tool calls: %v", err)
	}
	if tools != 1 {
		t.Fatalf("expected backfilled tool call row, got %d", tools)
	}
	if db.Migrator().HasTable("history_messages") {
		t.Fatal("expected legacy history_messages table to be dropped")
	}
}
