package persistence

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository provides CRUD operations for conversations, messages, and tasks.
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

func (r *Repository) DeleteConversation(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ?", id).Delete(&Message{}).Error; err != nil {
			return err
		}
		if err := tx.Where("conversation_id = ?", id).Delete(&Task{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Conversation{}).Error
	})
}

// --- Messages ---

func (r *Repository) AppendMessage(conversationID, role, content string) (*Message, error) {
	msg := &Message{
		ID:             uuid.New().String(),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
	}
	if err := r.db.Create(msg).Error; err != nil {
		return nil, fmt.Errorf("append message: %w", err)
	}
	// Touch conversation
	r.db.Model(&Conversation{}).Where("id = ?", conversationID).
		Update("updated_at", time.Now())
	return msg, nil
}

func (r *Repository) GetMessages(conversationID string, limit int) ([]Message, error) {
	var msgs []Message
	q := r.db.Where("conversation_id = ?", conversationID).Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return msgs, nil
}

// --- Tasks ---

func (r *Repository) CreateTask(conversationID, title string, sortOrder int) (*Task, error) {
	task := &Task{
		ID:             uuid.New().String(),
		ConversationID: conversationID,
		Title:          title,
		Status:         "pending",
		SortOrder:      sortOrder,
	}
	if err := r.db.Create(task).Error; err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (r *Repository) UpdateTaskStatus(id, status string) error {
	return r.db.Model(&Task{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": time.Now()}).Error
}

func (r *Repository) GetTasks(conversationID string) ([]Task, error) {
	var tasks []Task
	if err := r.db.Where("conversation_id = ?", conversationID).
		Order("sort_order ASC").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("get tasks: %w", err)
	}
	return tasks, nil
}
