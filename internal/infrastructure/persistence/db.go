// Package persistence provides SQLite-based storage using GORM.
package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Conversation represents a chat session.
type Conversation struct {
	ID        string `gorm:"primaryKey"`
	Channel   string `gorm:"index"`
	Title     string
	Status    string `gorm:"default:active"` // active / archived
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message represents a single message in a conversation.
type Message struct {
	ID             string `gorm:"primaryKey"`
	ConversationID string `gorm:"index"`
	Role           string // system / user / assistant / tool
	Content        string `gorm:"type:text"`
	ToolCallID     string
	CreatedAt      time.Time
}

// Task represents a structured task item linked to a conversation.
type Task struct {
	ID             string `gorm:"primaryKey"`
	ConversationID string `gorm:"index"`
	Title          string
	Status         string `gorm:"default:pending"` // pending / in_progress / done
	SortOrder      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Open initializes the SQLite database and runs auto-migrations.
func Open(dbPath string) (*gorm.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", dbPath, err)
	}

	// WAL mode for better concurrent read performance
	sqlDB, _ := db.DB()
	sqlDB.Exec("PRAGMA journal_mode=WAL")
	sqlDB.Exec("PRAGMA synchronous=NORMAL")

	// Auto-migrate
	if err := db.AutoMigrate(&Conversation{}, &Message{}, &Task{}, &EvoTrace{}, &EvoEvaluation{}, &EvoRepair{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}
