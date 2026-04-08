package memory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
)

// Fragment is a single piece of recalled memory.
type Fragment struct {
	ID        string
	Content   string
	SessionID string
	Score     float64 // final score (cosine × time-decay)
	CreatedAt time.Time
}

// Store provides vector-based conversation memory with disk persistence.
// It uses the same Embedder and VectorIndex primitives as the KI system
// but stores conversation fragments rather than distilled knowledge.
type Store struct {
	mu           sync.RWMutex
	embedder     knowledge.Embedder
	index        *knowledge.VectorIndex
	contents     map[string]fragmentMeta // id → metadata
	nextID       int
	dir          string // persistence directory (same as VectorIndex indexDir)
	halfLifeDays int    // time-decay half-life in days (default 30)
	maxFragments int    // 0 = unlimited (capacity eviction)
}

type fragmentMeta struct {
	Content   string    `json:"content"`
	SessionID string    `json:"session_id"`
	Scope     string    `json:"scope,omitempty"` // Namespace isolation
	CreatedAt time.Time `json:"created_at"`
}

// StoreConfig holds optional configuration for the memory store.
type StoreConfig struct {
	HalfLifeDays int // time-decay half-life (default 30, 0 = no decay)
	MaxFragments int // capacity limit (0 = unlimited)
}

// NewStore creates a memory store backed by embeddings.
// indexDir is where the vector index and fragments persist (e.g., brain/memory_vec/).
func NewStore(embedder knowledge.Embedder, indexDir string, cfg ...StoreConfig) *Store {
	idx := knowledge.NewVectorIndex(embedder.Dimensions(), indexDir)
	_ = idx.Load()

	halfLife := 30
	maxFrag := 0
	if len(cfg) > 0 {
		if cfg[0].HalfLifeDays > 0 {
			halfLife = cfg[0].HalfLifeDays
		}
		maxFrag = cfg[0].MaxFragments
	}

	s := &Store{
		embedder:     embedder,
		index:        idx,
		contents:     make(map[string]fragmentMeta),
		nextID:       1,
		dir:          indexDir,
		halfLifeDays: halfLife,
		maxFragments: maxFrag,
	}
	s.loadFragments()
	return s
}

// Save splits content into chunks, embeds each, and stores them.
// Called by CompactHook.BeforeCompact to preserve conversation before compression.
// Optional scope parameter enables namespace isolation for multi-project support.
func (s *Store) Save(sessionID, content string, scope ...string) error {
	if content == "" {
		return nil
	}
	chunks := splitChunks(content, 500) // ~500 chars per chunk
	if len(chunks) == 0 {
		return nil
	}

	// Batch embed for efficiency
	vecs, err := s.embedder.EmbedBatch(chunks)
	if err != nil {
		return fmt.Errorf("embed memory chunks: %w", err)
	}

	activeScope := ""
	if len(scope) > 0 {
		activeScope = scope[0]
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, chunk := range chunks {
		if i >= len(vecs) || vecs[i] == nil {
			continue
		}
		id := fmt.Sprintf("mem-%d-%d", time.Now().UnixMilli(), s.nextID)
		s.nextID++
		s.index.Add(id, vecs[i])
		s.contents[id] = fragmentMeta{
			Content:   chunk,
			SessionID: sessionID,
			Scope:     activeScope,
			CreatedAt: time.Now(),
		}
	}

	// Capacity eviction (if enabled)
	s.evictIfNeeded()

	// Persist both index and fragments to disk
	if err := s.index.Save(); err != nil {
		slog.Info(fmt.Sprintf("[memory] index save failed: %v", err))
	}
	if err := s.saveFragments(); err != nil {
		slog.Info(fmt.Sprintf("[memory] fragments save failed: %v", err))
	}

	slog.Info(fmt.Sprintf("[memory] saved %d chunks from session %s scope=%q (total: %d)", len(chunks), sessionID, activeScope, len(s.contents)))
	return nil
}

// Search returns the top-K most relevant memory fragments for a query.
// Results are scored with cosine similarity × time-decay weighting.
// Optional scope parameter filters by namespace.
func (s *Store) Search(query string, topK int, scope ...string) ([]Fragment, error) {
	queryVec, err := s.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	results := s.index.Search(queryVec, topK*2) // Overfetch for re-ranking

	activeScope := ""
	if len(scope) > 0 {
		activeScope = scope[0]
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var fragments []Fragment
	for _, r := range results {
		meta, ok := s.contents[r.ID]
		if !ok {
			continue
		}

		// Scope filter: skip fragments from other scopes
		if activeScope != "" && meta.Scope != "" && meta.Scope != activeScope {
			continue
		}

		// Apply time-decay: finalScore = cosine × 1/(1 + ageDays/halfLife)
		finalScore := r.Score
		if s.halfLifeDays > 0 {
			ageDays := time.Since(meta.CreatedAt).Hours() / 24
			decay := 1.0 / (1.0 + ageDays/float64(s.halfLifeDays))
			finalScore = r.Score * decay
		}

		fragments = append(fragments, Fragment{
			ID:        r.ID,
			Content:   meta.Content,
			SessionID: meta.SessionID,
			Score:     finalScore,
			CreatedAt: meta.CreatedAt,
		})
	}

	// Re-sort by decayed score
	sort.Slice(fragments, func(i, j int) bool {
		return fragments[i].Score > fragments[j].Score
	})

	// Trim to topK
	if topK > 0 && len(fragments) > topK {
		fragments = fragments[:topK]
	}
	return fragments, nil
}

// FormatForPrompt searches and formats results for injection into system prompt.
func (s *Store) FormatForPrompt(query string, topK, budgetChars int) string {
	fragments, err := s.Search(query, topK)
	if err != nil || len(fragments) == 0 {
		return ""
	}

	var b strings.Builder
	totalChars := 0
	for _, f := range fragments {
		age := formatAge(f.CreatedAt)
		entry := fmt.Sprintf("- [%.2f, %s] %s\n", f.Score, age, f.Content)
		if totalChars+len(entry) > budgetChars {
			break
		}
		b.WriteString(entry)
		totalChars += len(entry)
	}
	return b.String()
}

// Size returns the number of stored memory fragments.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.contents)
}

// PruneStale removes fragments older than maxAgeDays.
// Only deletes fragments with low relevance signal (never recalled = still original).
// Returns the number of pruned fragments.
func (s *Store) PruneStale(maxAgeDays int) int {
	if maxAgeDays <= 0 {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	var toRemove []string

	for id, meta := range s.contents {
		if meta.CreatedAt.Before(cutoff) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		delete(s.contents, id)
		s.index.Remove(id)
	}

	if len(toRemove) > 0 {
		if err := s.index.Save(); err != nil {
			slog.Info(fmt.Sprintf("[memory] index save after prune failed: %v", err))
		}
		if err := s.saveFragments(); err != nil {
			slog.Info(fmt.Sprintf("[memory] fragments save after prune failed: %v", err))
		}
		slog.Info(fmt.Sprintf("[memory] pruned %d stale fragments (older than %d days)", len(toRemove), maxAgeDays))
	}

	return len(toRemove)
}

// ═══════════════════════════════════════════
// Persistence: fragments.json
// ═══════════════════════════════════════════

// saveFragments writes fragment metadata to disk as JSON.
func (s *Store) saveFragments() error {
	data, err := json.Marshal(s.contents)
	if err != nil {
		return fmt.Errorf("marshal fragments: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, "fragments.json"), data, 0644)
}

// loadFragments restores fragment metadata from disk.
func (s *Store) loadFragments() {
	path := filepath.Join(s.dir, "fragments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return // No fragments file yet, fresh start
	}

	var loaded map[string]fragmentMeta
	if err := json.Unmarshal(data, &loaded); err != nil {
		slog.Info(fmt.Sprintf("[memory] failed to parse fragments.json: %v", err))
		return
	}

	s.contents = loaded

	// Rebuild nextID from existing IDs
	maxID := 0
	for range s.contents {
		maxID++
	}
	s.nextID = maxID + 1

	slog.Info(fmt.Sprintf("[memory] loaded %d fragments from disk", len(s.contents)))
}

// ═══════════════════════════════════════════
// Capacity Eviction (optional)
// ═══════════════════════════════════════════

// evictIfNeeded removes oldest fragments when capacity is exceeded.
// Must be called with s.mu held.
func (s *Store) evictIfNeeded() {
	if s.maxFragments <= 0 || len(s.contents) <= s.maxFragments {
		return
	}

	// Collect all fragments sorted by creation time (oldest first)
	type entry struct {
		id        string
		createdAt time.Time
	}
	var entries []entry
	for id, meta := range s.contents {
		entries = append(entries, entry{id, meta.CreatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].createdAt.Before(entries[j].createdAt)
	})

	// Remove oldest until within limit
	toRemove := len(s.contents) - s.maxFragments
	for i := 0; i < toRemove && i < len(entries); i++ {
		id := entries[i].id
		delete(s.contents, id)
		s.index.Remove(id)
	}

	slog.Info(fmt.Sprintf("[memory] evicted %d fragments (max=%d)", toRemove, s.maxFragments))
}

// ═══════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════

// formatAge returns a human-readable age string like "2d", "5h", "just now".
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		days := int(math.Round(d.Hours() / 24))
		return fmt.Sprintf("%dd", days)
	}
}

// splitChunks breaks text into paragraph-aware chunks of roughly maxChars.
func splitChunks(text string, maxChars int) []string {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if current.Len()+len(p) > maxChars && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(p)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
