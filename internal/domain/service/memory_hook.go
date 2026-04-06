package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
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
// M1: Noise filter applied — tool-output-heavy and trivially-short messages are skipped.
// If a KI already covers the content (cosine similarity > threshold), the fragment is skipped.
func (h *MemoryCompactHook) BeforeCompact(ctx context.Context, history []model.Message) {
	if h.store == nil || len(history) == 0 {
		return
	}

	var kept, skipped, noisy int
	var content strings.Builder
	for _, msg := range history {
		if msg.Content == "" {
			continue
		}

		// Bug1: Skip compact summary messages — prevent self-reinforcing loop.
		// When BeforeCompact runs, history may include the previous compact's
		// LLM-generated summary (role=assistant). Storing it again creates:
		// compact→summary→memory→next_compact→same_summary→memory (infinite accumulation).
		if msg.Role == "assistant" && isCompactSummary(msg.Content) {
			noisy++
			continue
		}

		// M1: Noise gate — ported from CC's WHAT_NOT_TO_SAVE philosophy.
		// Skip messages that are too short or dominated by tool output noise.
		if isNoisyMemoryContent(msg.Content) {
			noisy++
			continue
		}

		// KI dedup: skip fragments already covered by distilled knowledge
		if h.kiDedup != nil {
			dupID, score := h.kiDedup.FindDuplicate(msg.Content, h.threshold)
			if dupID != "" {
				skipped++
				slog.Info(fmt.Sprintf("[memory-hook] skip (KI %q covers it, score=%.2f)", dupID, score))
				continue
			}
		}
		kept++
		content.WriteString("[" + msg.Role + "]: " + msg.Content + "\n\n")
	}

	if content.Len() == 0 {
		slog.Info(fmt.Sprintf("[memory-hook] nothing to save: noisy=%d ki_dup=%d", noisy, skipped))
		return
	}

	if err := h.store.Save(h.sessionID, content.String()); err != nil {
		slog.Info(fmt.Sprintf("[memory-hook] save failed: %v", err))
	} else {
		slog.Info(fmt.Sprintf("[memory-hook] saved=%d skipped_ki=%d skipped_noise=%d", kept, skipped, noisy))
	}
}

// AfterCompact is a no-op for memory hook.
func (h *MemoryCompactHook) AfterCompact(ctx context.Context, compacted []model.Message) {}

// ─── M1: Noise filter ────────────────────────────────────────────────────────

// isNoisyMemoryContent returns true if content is too low-quality to store in memory.
// Inspired by CC's WHAT_NOT_TO_SAVE section: avoid tool failures, trivially short messages,
// user impatience markers, and content that is >2/3 unstructured tool output.
func isNoisyMemoryContent(content string) bool {
	trimmed := strings.TrimSpace(content)

	// Too short to carry semantic value
	if utf8.RuneCountInString(trimmed) < 30 {
		return true
	}

	// User impatience / placeholder messages
	lower := strings.ToLower(trimmed)
	for _, pat := range noisyPatterns {
		if lower == pat {
			return true
		}
	}

	// Dominated by tool output (>60% of lines are [tool]: prefix)
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return false
	}
	toolLines := 0
	for _, l := range lines {
		stripped := strings.TrimSpace(l)
		if strings.HasPrefix(stripped, "[tool]:") || strings.HasPrefix(stripped, "[tool_result]") {
			toolLines++
		}
	}
	return toolLines*10 > len(lines)*6 // >60% tool lines
}

// noisyPatterns are exact-match low-value user messages (trimmed, lowercased).
var noisyPatterns = []string{
	"?", "??", "???",
	"继续", "continue", "ok", "好", "好的",
	"jixu", "继续啊", ".", "..",
}

// isCompactSummary detects assistant messages that are LLM-generated compact summaries.
// These markers are injected by the compact pipeline and should not be re-stored in memory.
func isCompactSummary(content string) bool {
	return strings.Contains(content, "[COMPACT_SUMMARY]") ||
		strings.Contains(content, "## user_intent") ||
		strings.Contains(content, "## session_summary") ||
		strings.Contains(content, "## code_changes") ||
		strings.Contains(content, "## learned_facts")
}
