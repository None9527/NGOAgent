package knowledge

import (
	"fmt"
	"log"
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
// Called on startup.
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
			log.Printf("[ki-index] failed to embed %s: %v", item.ID, err)
			continue
		}
		newCount++
	}

	if newCount > 0 {
		log.Printf("[ki-index] indexed %d new KIs (total: %d)", newCount, r.index.Size())
		if err := r.index.Save(); err != nil {
			log.Printf("[ki-index] save failed: %v", err)
		}
	}
	return nil
}
