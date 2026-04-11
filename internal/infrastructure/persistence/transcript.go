package persistence

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// WorkerTranscript stores a subagent's tool trace for a parent session.
type WorkerTranscript struct {
	ID        uint   `gorm:"primarykey"`
	SessionID string `gorm:"index"` // parent session that spawned this worker
	TraceID   uint   `gorm:"index"` // FK → EvoTrace (links worker to parent run)
	TaskName  string // human-readable task label
	RunID     string `gorm:"index"`     // worker's unique run ID
	TraceJSON string `gorm:"type:text"` // full tool trace as JSON
	Status    string // completed / failed / timeout
	CreatedAt time.Time
}

// TranscriptStore provides persistence for worker (subagent) transcripts.
type TranscriptStore struct {
	db *gorm.DB
}

// NewTranscriptStore creates a transcript store and migrates the table.
func NewTranscriptStore(db *gorm.DB) *TranscriptStore {
	return &TranscriptStore{db: db}
}

// TraceEntry is a single tool invocation record.
type TraceEntry struct {
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration int64          `json:"duration_ms"`
}

// SaveTranscript persists a worker's tool trace.
func (ts *TranscriptStore) SaveTranscript(sessionID, taskName, runID, status string, trace []TraceEntry) error {
	traceData, err := json.Marshal(trace)
	if err != nil {
		traceData = []byte("[]")
	}
	return ts.db.Create(&WorkerTranscript{
		SessionID: sessionID,
		TaskName:  taskName,
		RunID:     runID,
		TraceJSON: string(traceData),
		Status:    status,
	}).Error
}

// SaveSimple persists a worker transcript with raw output string (no structured trace).
// Used by the barrier callback which only receives the final output, not per-step trace entries.
func (ts *TranscriptStore) SaveSimple(sessionID, taskName, runID, status, output string) error {
	// Wrap the output into a single-entry trace for schema consistency
	singleTrace, _ := json.Marshal([]TraceEntry{{
		Tool:   "subagent_result",
		Output: output,
	}})
	return ts.db.Create(&WorkerTranscript{
		SessionID: sessionID,
		TaskName:  taskName,
		RunID:     runID,
		TraceJSON: string(singleTrace),
		Status:    status,
	}).Error
}

// LoadTranscripts retrieves all worker transcripts for a session.
func (ts *TranscriptStore) LoadTranscripts(sessionID string) ([]WorkerTranscript, error) {
	var transcripts []WorkerTranscript
	err := ts.db.Where("session_id = ?", sessionID).
		Order("created_at ASC").Find(&transcripts).Error
	return transcripts, err
}

// LoadTranscript retrieves a single transcript by runID.
func (ts *TranscriptStore) LoadTranscript(runID string) (*WorkerTranscript, error) {
	var t WorkerTranscript
	err := ts.db.Where("run_id = ?", runID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteSessionTranscripts removes all transcripts for a session.
func (ts *TranscriptStore) DeleteSessionTranscripts(sessionID string) error {
	return ts.db.Where("session_id = ?", sessionID).Delete(&WorkerTranscript{}).Error
}

// CleanOld removes transcript rows older than the provided retention window.
func (ts *TranscriptStore) CleanOld(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return ts.db.Where("created_at < ?", cutoff).Delete(&WorkerTranscript{}).Error
}
