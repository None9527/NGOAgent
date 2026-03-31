package persistence

import (
	"time"

	"gorm.io/gorm"
)

// HistoryMessage is a persisted conversation message.
type HistoryMessage struct {
	ID         uint   `gorm:"primarykey"`
	SessionID  string `gorm:"index"`
	Role       string // user / assistant / tool / system
	Content    string `gorm:"type:text"`
	ToolCalls  string `gorm:"type:text"` // JSON-encoded tool calls
	ToolCallID string
	Reasoning  string `gorm:"type:text"` // Thinking/reasoning content
	CreatedAt  time.Time
}

// HistoryStore persists and retrieves conversation history.
type HistoryStore struct {
	db *gorm.DB
}

// NewHistoryStore creates a history store.
func NewHistoryStore(db *gorm.DB) *HistoryStore {
	db.AutoMigrate(&HistoryMessage{})
	return &HistoryStore{db: db}
}

// SaveAll replaces the entire session history in a transaction.
// Handles compact/truncation by deleting old rows first.
func (hs *HistoryStore) SaveAll(sessionID string, msgs []HistoryMessage) error {
	return hs.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Delete(&HistoryMessage{}).Error; err != nil {
			return err
		}
		if len(msgs) == 0 {
			return nil
		}
		// Prepare for bulk insert
		for i := range msgs {
			msgs[i].ID = 0
			msgs[i].SessionID = sessionID
		}
		return tx.CreateInBatches(msgs, 100).Error
	})
}

// Save persists a message.
func (hs *HistoryStore) Save(sessionID string, msg *HistoryMessage) error {
	msg.SessionID = sessionID
	return hs.db.Create(msg).Error
}

// LoadSession retrieves all messages for a session in order.
func (hs *HistoryStore) LoadSession(sessionID string) ([]HistoryMessage, error) {
	var msgs []HistoryMessage
	err := hs.db.Where("session_id = ?", sessionID).Order("created_at ASC").Find(&msgs).Error
	return msgs, err
}

// LoadSessionRecent retrieves the last `limit` messages for a session.
// Used by the frontend API to avoid loading full history for display.
func (hs *HistoryStore) LoadSessionRecent(sessionID string, limit int) ([]HistoryMessage, error) {
	var msgs []HistoryMessage
	// Sub-query: get IDs of last N messages, then load them in ASC order
	err := hs.db.Where("session_id = ? AND id IN (SELECT id FROM history_messages WHERE session_id = ? ORDER BY created_at DESC LIMIT ?)",
		sessionID, sessionID, limit).
		Order("created_at ASC").Find(&msgs).Error
	return msgs, err
}

// DeleteSession removes all messages for a session.
func (hs *HistoryStore) DeleteSession(sessionID string) error {
	return hs.db.Where("session_id = ?", sessionID).Delete(&HistoryMessage{}).Error
}

// AppendBatch inserts new messages using batch insert for reduced lock contention.
// Used for incremental persistence (normal turn endings).
func (hs *HistoryStore) AppendBatch(sessionID string, msgs []HistoryMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	return hs.db.Transaction(func(tx *gorm.DB) error {
		for i := range msgs {
			msgs[i].ID = 0
			msgs[i].SessionID = sessionID
		}
		return tx.CreateInBatches(msgs, 100).Error
	})
}

// TruncateSession keeps only the last N messages.
func (hs *HistoryStore) TruncateSession(sessionID string, keep int) error {
	var count int64
	hs.db.Model(&HistoryMessage{}).Where("session_id = ?", sessionID).Count(&count)
	if int(count) <= keep {
		return nil
	}
	// Delete oldest messages
	return hs.db.Exec(
		"DELETE FROM history_messages WHERE session_id = ? AND id NOT IN (SELECT id FROM history_messages WHERE session_id = ? ORDER BY created_at DESC LIMIT ?)",
		sessionID, sessionID, keep,
	).Error
}
