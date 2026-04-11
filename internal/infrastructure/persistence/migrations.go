package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"gorm.io/gorm"
)

// SchemaMigration records applied schema migrations.
type SchemaMigration struct {
	Version   int `gorm:"primaryKey;autoIncrement:false"`
	Name      string
	AppliedAt time.Time `gorm:"index"`
}

// MessageRecord is the future canonical message table for core conversation data.
type MessageRecord struct {
	ID              string  `gorm:"primaryKey"`
	ConversationID  string  `gorm:"index;not null"`
	Seq             int64   `gorm:"not null"`
	Role            string  `gorm:"index;not null"`
	MessageType     string  `gorm:"index;not null"`
	ContentText     string  `gorm:"type:text"`
	ReasoningText   string  `gorm:"type:text"`
	ToolCallID      string  `gorm:"index"`
	ParentMessageID *string `gorm:"index"`
	TokenCount      int
	CreatedAt       time.Time    `gorm:"index"`
	UpdatedAt       time.Time    `gorm:"index"`
	Conversation    Conversation `gorm:"foreignKey:ConversationID;references:ID;constraint:OnDelete:CASCADE"`
}

func (MessageRecord) TableName() string { return "messages" }

// MessageToolCallRecord stores normalized tool calls attached to a message.
type MessageToolCallRecord struct {
	ID         string        `gorm:"primaryKey"`
	MessageID  string        `gorm:"index;not null"`
	ToolName   string        `gorm:"index;not null"`
	ToolCallID string        `gorm:"index;not null"`
	Position   int           `gorm:"not null"`
	ArgsJSON   string        `gorm:"type:text"`
	ResultJSON string        `gorm:"type:text"`
	Status     string        `gorm:"index"`
	CreatedAt  time.Time     `gorm:"index"`
	Message    MessageRecord `gorm:"foreignKey:MessageID;references:ID;constraint:OnDelete:CASCADE"`
}

func (MessageToolCallRecord) TableName() string { return "message_tool_calls" }

// MessageAttachmentRecord stores normalized multimodal message attachments.
type MessageAttachmentRecord struct {
	ID           string `gorm:"primaryKey"`
	MessageID    string `gorm:"index;not null"`
	Position     int    `gorm:"not null"`
	Kind         string `gorm:"index;not null"`
	Path         string
	MIMEType     string `gorm:"column:mime_type"`
	Name         string
	MetadataJSON string        `gorm:"type:text"`
	CreatedAt    time.Time     `gorm:"index"`
	Message      MessageRecord `gorm:"foreignKey:MessageID;references:ID;constraint:OnDelete:CASCADE"`
}

func (MessageAttachmentRecord) TableName() string { return "message_attachments" }

// ArtifactRecord indexes durable conversation artifacts.
type ArtifactRecord struct {
	ID             string       `gorm:"primaryKey"`
	ConversationID string       `gorm:"index;not null"`
	Kind           string       `gorm:"index;not null"`
	Path           string       `gorm:"index;not null"`
	Version        int64        `gorm:"not null;default:1"`
	ContentHash    string       `gorm:"index"`
	MetadataJSON   string       `gorm:"type:text"`
	CreatedAt      time.Time    `gorm:"index"`
	UpdatedAt      time.Time    `gorm:"index"`
	Conversation   Conversation `gorm:"foreignKey:ConversationID;references:ID;constraint:OnDelete:CASCADE"`
}

func (ArtifactRecord) TableName() string { return "artifacts" }

// AgentRunRecord is the runtime index table for resumable executions.
type AgentRunRecord struct {
	ID                   string  `gorm:"primaryKey"`
	ConversationID       string  `gorm:"index"`
	ParentRunID          *string `gorm:"index"`
	EntryType            string  `gorm:"index;not null"`
	Status               string  `gorm:"index;not null"`
	CurrentNode          string  `gorm:"index"`
	CurrentRoute         string
	WaitReason           string `gorm:"index"`
	GraphID              string `gorm:"index;not null"`
	GraphVersion         string
	RuntimeSchemaVersion int        `gorm:"not null"`
	StartedAt            time.Time  `gorm:"index"`
	UpdatedAt            time.Time  `gorm:"index"`
	FinishedAt           *time.Time `gorm:"index"`
}

func (AgentRunRecord) TableName() string { return "agent_runs" }

// RunCheckpointRecord stores versioned execution checkpoints.
type RunCheckpointRecord struct {
	ID                 string         `gorm:"primaryKey"`
	RunID              string         `gorm:"index;not null"`
	CheckpointNo       int64          `gorm:"not null"`
	Status             string         `gorm:"index;not null"`
	SchemaVersion      int            `gorm:"not null"`
	CursorJSON         string         `gorm:"type:text"`
	TurnStateJSON      string         `gorm:"type:text"`
	ExecutionStateJSON string         `gorm:"type:text"`
	CreatedAt          time.Time      `gorm:"index"`
	Run                AgentRunRecord `gorm:"foreignKey:RunID;references:ID;constraint:OnDelete:CASCADE"`
}

func (RunCheckpointRecord) TableName() string { return "run_checkpoints" }

// RunWaitRecord tracks active or historical runtime waits.
type RunWaitRecord struct {
	ID          string         `gorm:"primaryKey"`
	RunID       string         `gorm:"index;not null"`
	WaitType    string         `gorm:"index;not null"`
	Status      string         `gorm:"index;not null"`
	PayloadJSON string         `gorm:"type:text"`
	CreatedAt   time.Time      `gorm:"index"`
	ResolvedAt  *time.Time     `gorm:"index"`
	Run         AgentRunRecord `gorm:"foreignKey:RunID;references:ID;constraint:OnDelete:CASCADE"`
}

func (RunWaitRecord) TableName() string { return "run_waits" }

// RunEventRecord stores append-only runtime audit events.
type RunEventRecord struct {
	ID          string `gorm:"primaryKey"`
	RunID       string `gorm:"index;not null"`
	Seq         int64  `gorm:"not null"`
	EventType   string `gorm:"index;not null"`
	Node        string `gorm:"index"`
	Route       string
	PayloadJSON string         `gorm:"type:text"`
	CreatedAt   time.Time      `gorm:"index"`
	Run         AgentRunRecord `gorm:"foreignKey:RunID;references:ID;constraint:OnDelete:CASCADE"`
}

func (RunEventRecord) TableName() string { return "run_events" }

type schemaMigrationStep struct {
	version int
	name    string
	apply   func(tx *gorm.DB) error
}

type legacyRunSnapshotRow struct {
	RunID              string `gorm:"column:run_id"`
	SessionID          string `gorm:"column:session_id"`
	GraphID            string `gorm:"column:graph_id"`
	GraphVersion       string `gorm:"column:graph_version"`
	Status             string `gorm:"column:status"`
	CursorJSON         string `gorm:"column:cursor_json"`
	TurnStateJSON      string `gorm:"column:turn_state_json"`
	ExecutionStateJSON string `gorm:"column:execution_state_json"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (legacyRunSnapshotRow) TableName() string { return "run_snapshot_records" }

type legacyHistoryMessageRow struct {
	ID          uint   `gorm:"column:id"`
	SessionID   string `gorm:"column:session_id"`
	Role        string `gorm:"column:role"`
	Content     string `gorm:"column:content"`
	ToolCalls   string `gorm:"column:tool_calls"`
	ToolCallID  string `gorm:"column:tool_call_id"`
	TokenCount  int    `gorm:"column:token_count"`
	Reasoning   string `gorm:"column:reasoning"`
	Attachments string `gorm:"column:attachments"`
	CreatedAt   time.Time
}

func (legacyHistoryMessageRow) TableName() string { return "history_messages" }

func schemaMigrationSteps() []schemaMigrationStep {
	return []schemaMigrationStep{
		{
			version: 1,
			name:    "baseline_unified_schema",
			apply: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(
					&Conversation{},
					&EvoTrace{},
					&EvoEvaluation{},
					&EvoRepair{},
					&EvoToolUsage{},
					&WorkerTranscript{},
					&SessionTokenUsage{},
					&MessageRecord{},
					&MessageToolCallRecord{},
					&MessageAttachmentRecord{},
					&ArtifactRecord{},
					&AgentRunRecord{},
					&RunCheckpointRecord{},
					&RunWaitRecord{},
					&RunEventRecord{},
				); err != nil {
					return err
				}
				if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_conversation_seq ON messages(conversation_id, seq)").Error; err != nil {
					return err
				}
				if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_run_checkpoints_run_checkpoint_no ON run_checkpoints(run_id, checkpoint_no)").Error; err != nil {
					return err
				}
				return tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_run_events_run_seq ON run_events(run_id, seq)").Error
			},
		},
		{
			version: 2,
			name:    "backfill_runtime_from_legacy_snapshots",
			apply: func(tx *gorm.DB) error {
				if !tx.Migrator().HasTable("run_snapshot_records") {
					return nil
				}
				var records []legacyRunSnapshotRow
				if err := tx.Find(&records).Error; err != nil {
					return err
				}

				for _, record := range records {
					var runCount int64
					if err := tx.Model(&AgentRunRecord{}).Where("id = ?", record.RunID).Count(&runCount).Error; err != nil {
						return err
					}
					if runCount > 0 {
						continue
					}

					snap, err := legacySnapshotRowToSnapshot(record)
					if err != nil {
						return fmt.Errorf("decode legacy snapshot %s: %w", record.RunID, err)
					}
					if err := saveRuntimeSnapshot(tx, snap, record.CursorJSON, record.TurnStateJSON, record.ExecutionStateJSON); err != nil {
						return fmt.Errorf("backfill legacy snapshot %s: %w", record.RunID, err)
					}
				}
				return nil
			},
		},
		{
			version: 3,
			name:    "backfill_core_messages_from_legacy_history",
			apply: func(tx *gorm.DB) error {
				if !tx.Migrator().HasTable("history_messages") || !tx.Migrator().HasTable(&MessageRecord{}) {
					return nil
				}

				var sessions []string
				if err := tx.Model(&legacyHistoryMessageRow{}).Distinct("session_id").Pluck("session_id", &sessions).Error; err != nil {
					return err
				}
				hs := &HistoryStore{db: tx}
				for _, sessionID := range sessions {
					var rows []legacyHistoryMessageRow
					if err := tx.Where("session_id = ?", sessionID).Order("created_at ASC").Find(&rows).Error; err != nil {
						return err
					}
					msgs := make([]HistoryMessage, 0, len(rows))
					for _, row := range rows {
						msgs = append(msgs, HistoryMessage{
							Role:        row.Role,
							Content:     row.Content,
							ToolCalls:   row.ToolCalls,
							ToolCallID:  row.ToolCallID,
							TokenCount:  row.TokenCount,
							Reasoning:   row.Reasoning,
							Attachments: row.Attachments,
							CreatedAt:   row.CreatedAt,
						})
					}
					if len(msgs) == 0 {
						continue
					}
					if err := hs.saveNormalizedMessages(tx, sessionID, msgs, 0); err != nil {
						return fmt.Errorf("backfill history %s: %w", sessionID, err)
					}
				}
				return nil
			},
		},
		{
			version: 4,
			name:    "drop_legacy_history_and_snapshot_tables",
			apply: func(tx *gorm.DB) error {
				if tx.Migrator().HasTable("history_messages") {
					if err := tx.Migrator().DropTable("history_messages"); err != nil {
						return err
					}
				}
				if tx.Migrator().HasTable("run_snapshot_records") {
					if err := tx.Migrator().DropTable("run_snapshot_records"); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}

func legacySnapshotRowToSnapshot(row legacyRunSnapshotRow) (*graphruntime.RunSnapshot, error) {
	var cursor graphruntime.ExecutionCursor
	if err := json.Unmarshal([]byte(row.CursorJSON), &cursor); err != nil {
		return nil, fmt.Errorf("unmarshal cursor: %w", err)
	}

	var turn graphruntime.TurnState
	if err := json.Unmarshal([]byte(row.TurnStateJSON), &turn); err != nil {
		return nil, fmt.Errorf("unmarshal turn state: %w", err)
	}

	var exec graphruntime.ExecutionState
	if err := json.Unmarshal([]byte(row.ExecutionStateJSON), &exec); err != nil {
		return nil, fmt.Errorf("unmarshal execution state: %w", err)
	}

	return &graphruntime.RunSnapshot{
		RunID:          row.RunID,
		SessionID:      row.SessionID,
		GraphID:        row.GraphID,
		GraphVersion:   row.GraphVersion,
		Status:         graphruntime.NodeStatus(row.Status),
		Cursor:         cursor,
		TurnState:      turn,
		ExecutionState: exec,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

// RunMigrations applies all pending persistence schema migrations.
func RunMigrations(db *gorm.DB) error {
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		return fmt.Errorf("migrate schema_migrations: %w", err)
	}

	for _, step := range schemaMigrationSteps() {
		var existing SchemaMigration
		err := db.First(&existing, "version = ?", step.version).Error
		if err == nil {
			continue
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return fmt.Errorf("check migration %d: %w", step.version, err)
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := step.apply(tx); err != nil {
				return err
			}
			return tx.Create(&SchemaMigration{
				Version:   step.version,
				Name:      step.name,
				AppliedAt: time.Now().UTC(),
			}).Error
		}); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", step.version, step.name, err)
		}
	}

	return nil
}
