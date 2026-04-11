// Package persistence provides SQLite-based storage using GORM.
package persistence

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	// Register sqlite-vec extension functions for all future SQLite connections.
	sqlite_vec.Auto()
}

// Conversation represents a chat session (sidebar metadata).
// All other tables reference this via session_id = Conversation.ID.
type Conversation struct {
	ID        string `gorm:"primaryKey"`
	Channel   string `gorm:"index"`
	Title     string
	Status    string `gorm:"index;default:active"` // active / archived
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Open initializes the SQLite database and runs centralized schema migrations.
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
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Info(fmt.Sprintf("[persistence] PRAGMA WAL failed: %v", err))
	}
	if _, err := sqlDB.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		slog.Info(fmt.Sprintf("[persistence] PRAGMA synchronous failed: %v", err))
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		slog.Info(fmt.Sprintf("[persistence] PRAGMA foreign_keys failed: %v", err))
	}

	if err := RunMigrations(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}
