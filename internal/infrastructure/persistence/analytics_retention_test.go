package persistence

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAnalyticsRetentionCleanup(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "analytics-retention.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	evoStore := NewEvoStore(db)
	transcriptStore := NewTranscriptStore(db)
	tokenStore := NewTokenUsageStore(db)

	old := time.Now().AddDate(0, 0, -40)
	if err := db.Create(&EvoTrace{SessionID: "old", CreatedAt: old}).Error; err != nil {
		t.Fatalf("seed old evo trace: %v", err)
	}
	if err := db.Create(&WorkerTranscript{SessionID: "old", RunID: "run-old", CreatedAt: old}).Error; err != nil {
		t.Fatalf("seed old transcript: %v", err)
	}
	if err := db.Create(&SessionTokenUsage{SessionID: "old", UpdatedAt: old, CreatedAt: old}).Error; err != nil {
		t.Fatalf("seed old token usage: %v", err)
	}

	if err := evoStore.CleanOld(30); err != nil {
		t.Fatalf("EvoStore.CleanOld: %v", err)
	}
	if err := transcriptStore.CleanOld(30); err != nil {
		t.Fatalf("TranscriptStore.CleanOld: %v", err)
	}
	if err := tokenStore.CleanOld(30); err != nil {
		t.Fatalf("TokenUsageStore.CleanOld: %v", err)
	}

	var count int64
	for _, model := range []any{&EvoTrace{}, &WorkerTranscript{}, &SessionTokenUsage{}} {
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatalf("count %T: %v", model, err)
		}
		if count != 0 {
			t.Fatalf("expected %T to be cleaned, count=%d", model, count)
		}
	}
}
