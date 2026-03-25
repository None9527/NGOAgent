package service

import (
	"context"
	"log"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// MemoryStorer abstracts the vector memory store (avoids import cycle with memory package).
type MemoryStorer interface {
	Save(sessionID, content string) error
	FormatForPrompt(query string, topK, budgetChars int) string
}

// MemoryCompactHook saves conversation content to vector memory before compaction.
// Includes KI dedup: fragments already covered by distilled knowledge are skipped,
// preventing duplicate information across the KI and Memory systems.
type MemoryCompactHook struct {
	store     MemoryStorer
	sessionID string
	kiDedup   KIDuplicateChecker // nil = no dedup
	threshold float64            // cosine similarity threshold for KI overlap
}

// NewMemoryCompactHook creates a hook that persists conversation to vector memory.
// kiDedup is optional — when provided, fragments overlapping with existing KIs are skipped.
func NewMemoryCompactHook(store MemoryStorer, sessionID string, kiDedup KIDuplicateChecker) *MemoryCompactHook {
	return &MemoryCompactHook{
		store:     store,
		sessionID: sessionID,
		kiDedup:   kiDedup,
		threshold: 0.75, // Skip memory if KI covers >75% similar content
	}
}

// BeforeCompact saves the conversation history that is about to be summarized
// and discarded. Each message becomes a searchable memory fragment.
// If a KI already covers the content (cosine similarity > threshold), the fragment is skipped.
func (h *MemoryCompactHook) BeforeCompact(ctx context.Context, history []llm.Message) {
	if h.store == nil || len(history) == 0 {
		return
	}

	var kept, skipped int
	var content strings.Builder
	for _, msg := range history {
		if msg.Content == "" {
			continue
		}
		// KI dedup: skip fragments already covered by distilled knowledge
		if h.kiDedup != nil {
			dupID, score := h.kiDedup.FindDuplicate(msg.Content, h.threshold)
			if dupID != "" {
				skipped++
				log.Printf("[memory-hook] skip (KI %q covers it, score=%.2f)", dupID, score)
				continue
			}
		}
		kept++
		content.WriteString("[" + msg.Role + "]: " + msg.Content + "\n\n")
	}

	if content.Len() == 0 {
		log.Printf("[memory-hook] all %d messages already covered by KI, nothing to save", skipped)
		return
	}

	if err := h.store.Save(h.sessionID, content.String()); err != nil {
		log.Printf("[memory-hook] save failed: %v", err)
	} else {
		log.Printf("[memory-hook] saved %d fragments, skipped %d (KI overlap)", kept, skipped)
	}
}

// AfterCompact is a no-op for memory hook.
func (h *MemoryCompactHook) AfterCompact(ctx context.Context, compacted []llm.Message) {}
