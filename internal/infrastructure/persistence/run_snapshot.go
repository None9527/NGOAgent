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

// RunSnapshotStore persists graph runtime checkpoints in the normalized runtime tables.
type RunSnapshotStore struct {
	db *gorm.DB
}

func NewRunSnapshotStore(db *gorm.DB) *RunSnapshotStore {
	return &RunSnapshotStore{db: db}
}

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

	return s.db.Transaction(func(tx *gorm.DB) error {
		return saveRuntimeSnapshot(tx, snap, string(cursorJSON), string(turnStateJSON), string(execStateJSON))
	})
}

func (s *RunSnapshotStore) LoadLatest(_ context.Context, runID string) (*graphruntime.RunSnapshot, error) {
	return s.loadLatestRuntime(runID)
}

func (s *RunSnapshotStore) LoadLatestBySession(_ context.Context, sessionID string) (*graphruntime.RunSnapshot, error) {
	if snap, err := s.loadLatestWaitingRuntimeBySession(sessionID); err != nil {
		return nil, err
	} else if snap != nil {
		return snap, nil
	}
	return s.loadLatestRuntimeBySession(sessionID)
}

func (s *RunSnapshotStore) ListBySession(_ context.Context, sessionID string) ([]*graphruntime.RunSnapshot, error) {
	var runs []AgentRunRecord
	if err := s.db.Where("conversation_id = ?", sessionID).Order("updated_at DESC").Find(&runs).Error; err != nil {
		return nil, err
	}
	return s.loadSnapshotsForRuns(runs)
}

func (s *RunSnapshotStore) ListByParentRun(_ context.Context, parentRunID string) ([]*graphruntime.RunSnapshot, error) {
	var runs []AgentRunRecord
	if err := s.db.Where("parent_run_id = ?", parentRunID).Order("updated_at DESC").Find(&runs).Error; err != nil {
		return nil, err
	}
	return s.loadSnapshotsForRuns(runs)
}

func (s *RunSnapshotStore) Delete(_ context.Context, runID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&RunWaitRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id = ?", runID).Delete(&RunEventRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id = ?", runID).Delete(&RunCheckpointRecord{}).Error; err != nil {
			return err
		}
		return tx.Delete(&AgentRunRecord{}, "id = ?", runID).Error
	})
}

func (s *RunSnapshotStore) loadLatestRuntime(runID string) (*graphruntime.RunSnapshot, error) {
	var run AgentRunRecord
	if err := s.db.First(&run, "id = ?", runID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	checkpoint, err := s.latestCheckpoint(runID)
	if err != nil {
		return nil, err
	}
	if checkpoint == nil {
		return nil, nil
	}
	return runtimeRecordsToSnapshot(run, *checkpoint)
}

func (s *RunSnapshotStore) loadLatestRuntimeBySession(sessionID string) (*graphruntime.RunSnapshot, error) {
	var run AgentRunRecord
	if err := s.db.Where("conversation_id = ?", sessionID).Order("updated_at DESC").First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	checkpoint, err := s.latestCheckpoint(run.ID)
	if err != nil {
		return nil, err
	}
	if checkpoint == nil {
		return nil, nil
	}
	return runtimeRecordsToSnapshot(run, *checkpoint)
}

func (s *RunSnapshotStore) loadLatestWaitingRuntimeBySession(sessionID string) (*graphruntime.RunSnapshot, error) {
	var run AgentRunRecord
	err := s.db.Table("agent_runs").
		Joins("JOIN run_waits ON run_waits.run_id = agent_runs.id").
		Where("agent_runs.conversation_id = ? AND run_waits.status = ?", sessionID, "pending").
		Order("run_waits.created_at DESC").
		Select("agent_runs.*").
		First(&run).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	checkpoint, err := s.latestCheckpoint(run.ID)
	if err != nil {
		return nil, err
	}
	if checkpoint == nil {
		return nil, nil
	}
	return runtimeRecordsToSnapshot(run, *checkpoint)
}

func (s *RunSnapshotStore) latestCheckpoint(runID string) (*RunCheckpointRecord, error) {
	var checkpoint RunCheckpointRecord
	if err := s.db.Where("run_id = ?", runID).Order("checkpoint_no DESC").First(&checkpoint).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &checkpoint, nil
}

func (s *RunSnapshotStore) loadSnapshotsForRuns(runs []AgentRunRecord) ([]*graphruntime.RunSnapshot, error) {
	snaps := make([]*graphruntime.RunSnapshot, 0, len(runs))
	for _, run := range runs {
		checkpoint, err := s.latestCheckpoint(run.ID)
		if err != nil {
			return nil, err
		}
		if checkpoint == nil {
			continue
		}
		snap, err := runtimeRecordsToSnapshot(run, *checkpoint)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

func saveRuntimeSnapshot(tx *gorm.DB, snap *graphruntime.RunSnapshot, cursorJSON, turnStateJSON, execStateJSON string) error {
	updatedAt := snap.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	createdAt := snap.CreatedAt
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	startedAt := snap.ExecutionState.StartedAt
	if startedAt.IsZero() {
		startedAt = createdAt
	}

	runRecord := AgentRunRecord{
		ID:                   snap.RunID,
		ConversationID:       snap.SessionID,
		EntryType:            "graph_runtime",
		Status:               string(snap.Status),
		CurrentNode:          snap.Cursor.CurrentNode,
		CurrentRoute:         snap.Cursor.RouteKey,
		WaitReason:           string(snap.ExecutionState.WaitReason),
		GraphID:              snap.GraphID,
		GraphVersion:         snap.GraphVersion,
		RuntimeSchemaVersion: 1,
		StartedAt:            startedAt,
		UpdatedAt:            updatedAt,
	}
	if parentRunID := snap.TurnState.Orchestration.ParentRunID; parentRunID != "" {
		runRecord.ParentRunID = &parentRunID
	}
	if snap.Status == graphruntime.NodeStatusComplete || snap.Status == graphruntime.NodeStatusFatal {
		finishedAt := updatedAt
		runRecord.FinishedAt = &finishedAt
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"conversation_id",
			"parent_run_id",
			"entry_type",
			"status",
			"current_node",
			"current_route",
			"wait_reason",
			"graph_id",
			"graph_version",
			"runtime_schema_version",
			"started_at",
			"updated_at",
			"finished_at",
		}),
	}).Create(&runRecord).Error; err != nil {
		return err
	}

	var lastCheckpoint RunCheckpointRecord
	checkpointNo := int64(1)
	err := tx.Where("run_id = ?", snap.RunID).Order("checkpoint_no DESC").First(&lastCheckpoint).Error
	if err == nil {
		checkpointNo = lastCheckpoint.CheckpointNo + 1
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	checkpoint := RunCheckpointRecord{
		ID:                 fmt.Sprintf("%s:%d", snap.RunID, checkpointNo),
		RunID:              snap.RunID,
		CheckpointNo:       checkpointNo,
		Status:             string(snap.Status),
		SchemaVersion:      1,
		CursorJSON:         cursorJSON,
		TurnStateJSON:      turnStateJSON,
		ExecutionStateJSON: execStateJSON,
		CreatedAt:          updatedAt,
	}
	if err := tx.Create(&checkpoint).Error; err != nil {
		return err
	}

	if snap.Status == graphruntime.NodeStatusWait {
		wait := RunWaitRecord{
			ID:          snap.RunID,
			RunID:       snap.RunID,
			WaitType:    string(snap.ExecutionState.WaitReason),
			Status:      "pending",
			PayloadJSON: execStateJSON,
			CreatedAt:   updatedAt,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"wait_type",
				"status",
				"payload_json",
				"created_at",
				"resolved_at",
			}),
		}).Create(&wait).Error; err != nil {
			return err
		}
	} else {
		resolvedAt := updatedAt
		if err := tx.Model(&RunWaitRecord{}).
			Where("run_id = ?", snap.RunID).
			Updates(map[string]any{"status": "resolved", "resolved_at": resolvedAt}).Error; err != nil {
			return err
		}
	}

	if err := saveRuntimeEvents(tx, snap.RunID, snap.TurnState.Orchestration.Events, updatedAt); err != nil {
		return err
	}

	return nil
}

func saveRuntimeEvents(tx *gorm.DB, runID string, events []graphruntime.OrchestrationEventState, defaultAt time.Time) error {
	var lastSeq int64
	if err := tx.Model(&RunEventRecord{}).
		Where("run_id = ?", runID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&lastSeq).Error; err != nil {
		return err
	}

	for idx, event := range events {
		if int64(idx+1) <= lastSeq {
			continue
		}
		createdAt := event.At
		if createdAt.IsZero() {
			createdAt = defaultAt
		}
		payloadJSON, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal runtime event: %w", err)
		}
		record := RunEventRecord{
			ID:          fmt.Sprintf("%s:%d", runID, idx+1),
			RunID:       runID,
			Seq:         int64(idx + 1),
			EventType:   event.Type,
			PayloadJSON: string(payloadJSON),
			CreatedAt:   createdAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

func runtimeRecordsToSnapshot(run AgentRunRecord, checkpoint RunCheckpointRecord) (*graphruntime.RunSnapshot, error) {
	var cursor graphruntime.ExecutionCursor
	if err := json.Unmarshal([]byte(checkpoint.CursorJSON), &cursor); err != nil {
		return nil, fmt.Errorf("unmarshal cursor: %w", err)
	}
	var turnState graphruntime.TurnState
	if err := json.Unmarshal([]byte(checkpoint.TurnStateJSON), &turnState); err != nil {
		return nil, fmt.Errorf("unmarshal turn state: %w", err)
	}
	var execState graphruntime.ExecutionState
	if err := json.Unmarshal([]byte(checkpoint.ExecutionStateJSON), &execState); err != nil {
		return nil, fmt.Errorf("unmarshal execution state: %w", err)
	}

	return &graphruntime.RunSnapshot{
		RunID:          run.ID,
		SessionID:      run.ConversationID,
		GraphID:        run.GraphID,
		GraphVersion:   run.GraphVersion,
		Status:         graphruntime.NodeStatus(run.Status),
		Cursor:         cursor,
		TurnState:      turnState,
		ExecutionState: execState,
		CreatedAt:      run.StartedAt,
		UpdatedAt:      run.UpdatedAt,
	}, nil
}
