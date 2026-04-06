package persistence

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// SessionTokenUsage stores cumulative token usage for a completed session.
type SessionTokenUsage struct {
	ID               uint   `gorm:"primarykey"`
	SessionID        string `gorm:"uniqueIndex"` // one record per session
	Model            string // primary model used
	TotalPromptTok   int
	TotalCompleteTok int
	TotalCalls       int
	TotalCostUSD     float64
	ByModelJSON      string `gorm:"type:text"` // JSON: map[model]ModelUsage
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TokenUsageStore provides persistence for session-level token usage.
type TokenUsageStore struct {
	db *gorm.DB
}

// NewTokenUsageStore creates and migrates the token usage table.
func NewTokenUsageStore(db *gorm.DB) *TokenUsageStore {
	db.AutoMigrate(&SessionTokenUsage{})
	return &TokenUsageStore{db: db}
}

// SaveSessionUsage persists or updates token usage for a session.
func (ts *TokenUsageStore) SaveSessionUsage(sessionID, model string, promptTok, completeTok, calls int, costUSD float64, byModel map[string]any) error {
	byModelData, _ := json.Marshal(byModel)

	usage := SessionTokenUsage{
		SessionID:        sessionID,
		Model:            model,
		TotalPromptTok:   promptTok,
		TotalCompleteTok: completeTok,
		TotalCalls:       calls,
		TotalCostUSD:     costUSD,
		ByModelJSON:      string(byModelData),
	}

	// Upsert: update if exists, create if not
	var existing SessionTokenUsage
	if err := ts.db.Where("session_id = ?", sessionID).First(&existing).Error; err != nil {
		// Not found — create
		return ts.db.Create(&usage).Error
	}
	// Update existing
	return ts.db.Model(&existing).Updates(map[string]any{
		"model":              model,
		"total_prompt_tok":   promptTok,
		"total_complete_tok": completeTok,
		"total_calls":        calls,
		"total_cost_usd":     costUSD,
		"by_model_json":      string(byModelData),
		"updated_at":         time.Now(),
	}).Error
}

// GetSessionUsage retrieves token usage for a specific session.
func (ts *TokenUsageStore) GetSessionUsage(sessionID string) (*SessionTokenUsage, error) {
	var usage SessionTokenUsage
	if err := ts.db.Where("session_id = ?", sessionID).First(&usage).Error; err != nil {
		return nil, err
	}
	return &usage, nil
}

// ListRecentUsage returns the most recent N session usage records.
func (ts *TokenUsageStore) ListRecentUsage(limit int) ([]SessionTokenUsage, error) {
	var usages []SessionTokenUsage
	err := ts.db.Order("updated_at DESC").Limit(limit).Find(&usages).Error
	return usages, err
}
