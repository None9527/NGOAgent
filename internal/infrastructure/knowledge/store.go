// Package knowledge provides cross-session persistent knowledge management.
// Knowledge Items (KIs) persist beyond conversation scope for long-term learning.
package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item represents a Knowledge Item.
type Item struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Sources   []string  `json:"sources,omitempty"` // Conversation IDs
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store manages the KI Store directory.
type Store struct {
	baseDir string // ~/.ngoagent/knowledge/
}

// NewStore creates a knowledge store.
func NewStore(knowledgeDir string) *Store {
	os.MkdirAll(knowledgeDir, 0755)
	return &Store{baseDir: knowledgeDir}
}

// Save writes or updates a KI.
func (s *Store) Save(item *Item) error {
	if item.ID == "" {
		item.ID = sanitizeID(item.Title)
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	item.UpdatedAt = time.Now()

	itemDir := filepath.Join(s.baseDir, item.ID)
	os.MkdirAll(filepath.Join(itemDir, "artifacts"), 0755)

	// Write metadata
	meta, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal KI: %w", err)
	}
	if err := os.WriteFile(filepath.Join(itemDir, "metadata.json"), meta, 0644); err != nil {
		return fmt.Errorf("write KI metadata: %w", err)
	}

	// Write content
	if item.Content != "" {
		os.WriteFile(filepath.Join(itemDir, "artifacts", "overview.md"), []byte(item.Content), 0644)
	}

	return nil
}

// Get retrieves a KI by ID.
func (s *Store) Get(id string) (*Item, error) {
	path := filepath.Join(s.baseDir, id, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read KI %s: %w", id, err)
	}
	var item Item
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("parse KI %s: %w", id, err)
	}
	return &item, nil
}

// List returns summaries of all KIs.
func (s *Store) List() ([]*Item, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, err
	}

	var items []*Item
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, err := s.Get(entry.Name())
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// Search returns KIs matching a query in title, summary, or tags.
func (s *Store) Search(query string) ([]*Item, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var matches []*Item
	for _, item := range all {
		if strings.Contains(strings.ToLower(item.Title), q) ||
			strings.Contains(strings.ToLower(item.Summary), q) {
			matches = append(matches, item)
			continue
		}
		for _, tag := range item.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				matches = append(matches, item)
				break
			}
		}
	}
	return matches, nil
}

// GenerateSummaries returns a formatted string of all KI summaries for prompt injection.
func (s *Store) GenerateSummaries() string {
	items, err := s.List()
	if err != nil || len(items) == 0 {
		return ""
	}

	var b strings.Builder
	for _, item := range items {
		b.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Title, item.Summary))
	}
	return b.String()
}

func sanitizeID(title string) string {
	id := strings.ToLower(title)
	id = strings.ReplaceAll(id, " ", "_")
	id = strings.ReplaceAll(id, "/", "_")
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}
