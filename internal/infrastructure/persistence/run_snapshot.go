package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RunSnapshotRecord stores the latest persisted execution snapshot for a run.
// It is intentionally separated from HistoryMessage so execution resume does not
// depend on chat transcript storage semantics.
type RunSnapshotRecord struct {
	RunID              string `gorm:"primaryKey"`
	SessionID          string `gorm:"index"`
	GraphID            string `gorm:"index"`
	GraphVersion       string
	Status             string `gorm:"index"`
	CursorJSON         string `gorm:"type:text"`
	TurnStateJSON      string `gorm:"type:text"`
	ExecutionStateJSON string `gorm:"type:text"`
	CreatedAt          time.Time
	UpdatedAt          time.Time `gorm:"index"`
}

// RunSnapshotStore persists graph runtime checkpoints.
type RunSnapshotStore struct {
	db *gorm.DB
}

// NewRunSnapshotStore creates a snapshot store and migrates the table.
func NewRunSnapshotStore(db *gorm.DB) *RunSnapshotStore {
	db.AutoMigrate(&RunSnapshotRecord{})
	return &RunSnapshotStore{db: db}
}

// Save upserts the latest snapshot for a run.
func (s *RunSnapshotStore) Save(_ context.Context, snap *graphruntime.RunSnapshot) error {
	if snap == nil {
		return fmt.Errorf("run snapshot is nil")
	}

	cursorJSON, err := json.Marshal(snap.Cursor)
	if err != nil {
		return fmt.Errorf("marshal cursor: %w", err)
	}
	turnStateJSON, err := json.Marshal(snap.TurnState)
	if err != nil {
		return fmt.Errorf("marshal turn state: %w", err)
	}
	execStateJSON, err := json.Marshal(snap.ExecutionState)
	if err != nil {
		return fmt.Errorf("marshal execution state: %w", err)
	}

	record := RunSnapshotRecord{
		RunID:              snap.RunID,
		SessionID:          snap.SessionID,
		GraphID:            snap.GraphID,
		GraphVersion:       snap.GraphVersion,
		Status:             string(snap.Status),
		CursorJSON:         string(cursorJSON),
		TurnStateJSON:      string(turnStateJSON),
		ExecutionStateJSON: string(execStateJSON),
		CreatedAt:          snap.CreatedAt,
		UpdatedAt:          snap.UpdatedAt,
	}

	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "run_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"session_id",
			"graph_id",
			"graph_version",
			"status",
			"cursor_json",
			"turn_state_json",
			"execution_state_json",
			"updated_at",
		}),
	}).Create(&record).Error
}

// LoadLatest loads the latest snapshot for a run.
func (s *RunSnapshotStore) LoadLatest(_ context.Context, runID string) (*graphruntime.RunSnapshot, error) {
	var record RunSnapshotRecord
	if err := s.db.First(&record, "run_id = ?", runID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return s.recordToSnapshot(record)
}

// LoadLatestBySession loads the most recently updated snapshot for a session.
func (s *RunSnapshotStore) LoadLatestBySession(_ context.Context, sessionID string) (*graphruntime.RunSnapshot, error) {
	var record RunSnapshotRecord
	if err := s.db.Where("session_id = ?", sessionID).Order("updated_at DESC").First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return s.recordToSnapshot(record)
}

// Delete removes the latest snapshot for a run.
func (s *RunSnapshotStore) Delete(_ context.Context, runID string) error {
	return s.db.Delete(&RunSnapshotRecord{}, "run_id = ?", runID).Error
}

func (s *RunSnapshotStore) recordToSnapshot(record RunSnapshotRecord) (*graphruntime.RunSnapshot, error) {
	var cursor graphruntime.ExecutionCursor
	if err := json.Unmarshal([]byte(record.CursorJSON), &cursor); err != nil {
		return nil, fmt.Errorf("unmarshal cursor: %w", err)
	}

	var turnState graphruntime.TurnState
	if err := json.Unmarshal([]byte(record.TurnStateJSON), &turnState); err != nil {
		return nil, fmt.Errorf("unmarshal turn state: %w", err)
	}

	var execState graphruntime.ExecutionState
	if err := json.Unmarshal([]byte(record.ExecutionStateJSON), &execState); err != nil {
		return nil, fmt.Errorf("unmarshal execution state: %w", err)
	}

	return &graphruntime.RunSnapshot{
		RunID:          record.RunID,
		SessionID:      record.SessionID,
		GraphID:        record.GraphID,
		GraphVersion:   record.GraphVersion,
		Status:         graphruntime.NodeStatus(record.Status),
		Cursor:         cursor,
		TurnState:      turnState,
		ExecutionState: execState,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}
