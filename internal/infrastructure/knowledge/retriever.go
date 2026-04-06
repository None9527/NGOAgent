package knowledge

import (
	"fmt"
	"log/slog"
	"strings"
)

// Retriever provides semantic search over Knowledge Items using embeddings.
type Retriever struct {
	store    *Store
	embedder Embedder
	index    *VectorIndex
}

// NewRetriever creates a retriever. Call BuildIndex() to populate vectors for existing KIs.
func NewRetriever(store *Store, embedder Embedder, index *VectorIndex) *Retriever {
	return &Retriever{store: store, embedder: embedder, index: index}
}

// Retrieve returns the top-K most semantically relevant KIs for a query.
// Uses GetWithContent to ensure full overview.md content is available.
func (r *Retriever) Retrieve(query string, topK int) ([]*Item, error) {
	queryVec, err := r.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	results := r.index.Search(queryVec, topK)

	var items []*Item
	for _, res := range results {
		item, err := r.store.GetWithContent(res.ID)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// RetrieveForPrompt returns full KI content for relevant items, within a character budget.
// L1: Each matched KI gets title + summary + full content (capped per-item to prevent
// one huge KI from consuming the entire budget).
func (r *Retriever) RetrieveForPrompt(query string, topK int, budgetChars int) string {
	items, err := r.Retrieve(query, topK)
	if err != nil || len(items) == 0 {
		return ""
	}

	var b strings.Builder
	totalChars := 0
	for _, item := range items {
		var entry strings.Builder
		entry.WriteString(fmt.Sprintf("## %s\n", item.Title))
		entry.WriteString(fmt.Sprintf("摘要: %s\n\n", item.Summary))
		if item.Content != "" {
			content := item.Content
			// Per-item cap: prevent one huge KI from eating the entire budget
			maxPerItem := budgetChars / 2
			if len(content) > maxPerItem {
				content = content[:maxPerItem] + "\n... (truncated, full content: " +
					strings.Join(r.store.ListArtifacts(item.ID), ", ") + ")"
			}
			entry.WriteString(content)
			entry.WriteString("\n")
		}
		entry.WriteString("\n")

		entryStr := entry.String()
		if totalChars+len(entryStr) > budgetChars {
			break
		}
		b.WriteString(entryStr)
		totalChars += len(entryStr)
	}
	return b.String()
}

// EmbedAndIndex generates an embedding for a KI and adds it to the index.
// Called after saving a new KI.
func (r *Retriever) EmbedAndIndex(item *Item) error {
	text := item.Title + "\n" + item.Summary
	if item.Content != "" {
		// Use first 500 chars of content for richer embedding
		content := item.Content
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500])
		}
		text += "\n" + content
	}

	vec, err := r.embedder.Embed(text)
	if err != nil {
		return fmt.Errorf("embed KI %s: %w", item.ID, err)
	}

	r.index.Add(item.ID, vec)
	return nil
}

// FindDuplicate checks if a similar KI already exists (cosine > threshold).
// Returns the ID and score of the best match, or ("", 0) if none found.
func (r *Retriever) FindDuplicate(text string, threshold float64) (string, float64) {
	vec, err := r.embedder.Embed(text)
	if err != nil {
		return "", 0
	}

	results := r.index.Search(vec, 1)
	if len(results) == 0 || results[0].Score < threshold {
		return "", 0
	}
	return results[0].ID, results[0].Score
}

// EmbedAndIndexByID re-indexes a KI by ID (used after content update).
func (r *Retriever) EmbedAndIndexByID(id string) error {
	item, err := r.store.GetWithContent(id)
	if err != nil {
		return fmt.Errorf("get KI %s: %w", id, err)
	}
	return r.EmbedAndIndex(item)
}

// BuildIndex generates embeddings for all existing KIs that aren't already indexed.
// Called on startup. After indexing, runs ConsolidateDuplicates to merge similar KIs.
func (r *Retriever) BuildIndex() error {
	items, err := r.store.List()
	if err != nil {
		return fmt.Errorf("list KIs: %w", err)
	}

	newCount := 0
	for _, item := range items {
		if r.index.Has(item.ID) {
			continue
		}
		if err := r.EmbedAndIndex(item); err != nil {
			slog.Info(fmt.Sprintf("[ki-index] failed to embed %s: %v", item.ID, err))
			continue
		}
		newCount++
	}

	if newCount > 0 {
		slog.Info(fmt.Sprintf("[ki-index] indexed %d new KIs (total: %d)", newCount, r.index.Size()))
		if err := r.index.Save(); err != nil {
			slog.Info(fmt.Sprintf("[ki-index] save failed: %v", err))
		}
	}

	return nil
}

// ConsolidateDuplicates scans all indexed KIs for duplicate pairs (cosine > threshold)
// and merges the shorter into the longer, deleting the redundant one.
// This is the system-level mechanism that prevents KI sprawl — runs at every startup.
func (r *Retriever) ConsolidateDuplicates(threshold float64) {
	items, err := r.store.List()
	if err != nil {
		return
	}

	// Build a set of all KI IDs for quick lookup
	type kiInfo struct {
		id    string
		title string
		len   int // content length, as a proxy for "completeness"
	}

	var kis []kiInfo
	for _, item := range items {
		full, err := r.store.GetWithContent(item.ID)
		if err != nil {
			continue
		}
		kis = append(kis, kiInfo{id: item.ID, title: item.Title, len: len(full.Content)})
	}

	deleted := make(map[string]bool)
	merged := 0

	for i := 0; i < len(kis); i++ {
		if deleted[kis[i].id] {
			continue
		}

		// Search for the best match of this KI in the index
		vec := r.index.GetVec(kis[i].id)
		if vec == nil {
			continue
		}

		results := r.index.Search(vec, 5) // top 5 to find all near-duplicates
		for _, res := range results {
			if res.ID == kis[i].id || deleted[res.ID] || res.Score < threshold {
				continue
			}

			// Found a duplicate pair: merge shorter into longer
			var keeper, victim kiInfo
			// Find the victim info
			var victimInfo kiInfo
			for _, k := range kis {
				if k.id == res.ID {
					victimInfo = k
					break
				}
			}

			if kis[i].len >= victimInfo.len {
				keeper, victim = kis[i], victimInfo
			} else {
				keeper, victim = victimInfo, kis[i]
			}

			// Merge: append victim's unique content into keeper
			victimFull, err := r.store.GetWithContent(victim.id)
			if err != nil {
				continue
			}

			appendContent := fmt.Sprintf("\n\n---\n\n## Consolidated from: %s\n\n%s", victim.title, victimFull.Content)
			if err := r.store.UpdateMerge(keeper.id, appendContent, keeper.title); err != nil {
				slog.Info(fmt.Sprintf("[ki-consolidate] merge failed %s←%s: %v", keeper.id, victim.id, err))
				continue
			}

			// Delete victim and remove from index
			if err := r.store.Delete(victim.id); err != nil {
				slog.Info(fmt.Sprintf("[ki-consolidate] delete failed %s: %v", victim.id, err))
				continue
			}
			r.index.Remove(victim.id)
			deleted[victim.id] = true
			merged++

			slog.Info(fmt.Sprintf("[ki-consolidate] merged %q into %q (score=%.2f)", victim.title, keeper.title, res.Score))
		}
	}

	if merged > 0 {
		slog.Info(fmt.Sprintf("[ki-consolidate] consolidated %d duplicate KIs", merged))
		if err := r.index.Save(); err != nil {
			slog.Info(fmt.Sprintf("[ki-consolidate] index save failed: %v", err))
		}
	}
}
