package persistence

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository provides CRUD operations for conversations.
// All session-scoped tables cascade-delete through DeleteConversation.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// --- Conversations ---

func (r *Repository) CreateConversation(channel, title string) (*Conversation, error) {
	conv := &Conversation{
		ID:      uuid.New().String(),
		Channel: channel,
		Title:   title,
		Status:  "active",
	}
	if err := r.db.Create(conv).Error; err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return conv, nil
}

func (r *Repository) GetConversation(id string) (*Conversation, error) {
	var conv Conversation
	if err := r.db.First(&conv, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("get conversation %s: %w", id, err)
	}
	return &conv, nil
}

func (r *Repository) ListConversations(limit, offset int) ([]Conversation, error) {
	var convs []Conversation
	q := r.db.Order("updated_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	if err := q.Find(&convs).Error; err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return convs, nil
}

func (r *Repository) UpdateConversationTitle(id, title string) error {
	return r.db.Model(&Conversation{}).Where("id = ?", id).
		Updates(map[string]any{"title": title, "updated_at": time.Now()}).Error
}

func (r *Repository) TouchConversation(id string) error {
	return r.db.Model(&Conversation{}).Where("id = ?", id).
		Update("updated_at", time.Now()).Error
}

// DeleteConversation removes a conversation and ALL associated data.
// Cascade: history_messages, worker_transcripts, session_token_usages, evo_*.
func (r *Repository) DeleteConversation(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 1. Conversation history
		if err := tx.Where("session_id = ?", id).Delete(&HistoryMessage{}).Error; err != nil {
			return err
		}
		// 2. Subagent transcripts
		if err := tx.Where("session_id = ?", id).Delete(&WorkerTranscript{}).Error; err != nil {
			return err
		}
		// 3. Token usage
		if err := tx.Where("session_id = ?", id).Delete(&SessionTokenUsage{}).Error; err != nil {
			return err
		}
		// 4. Evo chain (order: repairs → evaluations → traces)
		if err := tx.Where("session_id = ?", id).Delete(&EvoRepair{}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", id).Delete(&EvoEvaluation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", id).Delete(&EvoTrace{}).Error; err != nil {
			return err
		}
		// 5. Conversation metadata
		return tx.Where("id = ?", id).Delete(&Conversation{}).Error
	})
}
