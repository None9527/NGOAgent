// Package knowledge provides cross-session persistent knowledge management.
// Knowledge Items (KIs) persist beyond conversation scope for long-term learning.
package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// Scope namespace — enables multi-project/domain memory isolation.
	// "global" or "" means accessible from any scope.
	Scope string `json:"scope,omitempty"`

	// Temporal Knowledge Graph — knowledge has a lifecycle.
	ValidFrom    *time.Time `json:"valid_from,omitempty"`    // When this knowledge became valid (nil = CreatedAt)
	ValidUntil   *time.Time `json:"valid_until,omitempty"`   // When this knowledge expires (nil = never)
	Deprecated   bool       `json:"deprecated,omitempty"`    // Explicitly superseded by newer knowledge
	SupersededBy string     `json:"superseded_by,omitempty"` // ID of the KI that replaced this one

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsActive returns true if this KI is neither deprecated nor expired.
func (item *Item) IsActive() bool {
	if item.Deprecated {
		return false
	}
	if item.ValidUntil != nil && time.Now().After(*item.ValidUntil) {
		return false
	}
	return true
}

// MatchesScope returns true if this KI is accessible from the given scope.
// Global KIs (empty scope) are always accessible.
func (item *Item) MatchesScope(scope string) bool {
	if item.Scope == "" || item.Scope == "global" {
		return true
	}
	if scope == "" || scope == "global" {
		return true
	}
	return item.Scope == scope
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

// Delete removes a KI directory entirely.
func (s *Store) Delete(id string) error {
	path := filepath.Join(s.baseDir, id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("KI %q not found", id)
	}
	return os.RemoveAll(path)
}

// Get retrieves a KI by ID (metadata only, Content from metadata.json snapshot).
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

// GetWithContent retrieves a KI with fresh content from overview.md.
// After UpdateMerge, overview.md may have more content than metadata.json.content.
// This method reads the authoritative overview.md and fills item.Content.
func (s *Store) GetWithContent(id string) (*Item, error) {
	item, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	artPath := filepath.Join(s.baseDir, id, "artifacts", "overview.md")
	if data, err := os.ReadFile(artPath); err == nil && len(data) > 0 {
		item.Content = string(data)
	}
	return item, nil
}

// List returns summaries of all KIs (metadata only).
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

// ═══════════════════════════════════════════
// Three-Tier KI Injection
// ═══════════════════════════════════════════

// GeneratePreferenceKI returns full content for preference-tagged KIs.
// L0: These are enforceable rules — always injected with complete overview.md content.
func (s *Store) GeneratePreferenceKI() string {
	items, err := s.List()
	if err != nil || len(items) == 0 {
		return ""
	}

	var b strings.Builder
	first := true
	for _, item := range items {
		if !hasTag(item.Tags, "preference") {
			continue
		}
		if !first {
			b.WriteString("\n---\n\n")
		}
		first = false
		// Read authoritative content from overview.md
		full, err := s.GetWithContent(item.ID)
		if err != nil {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", full.Title))
		if full.Content != "" {
			b.WriteString(full.Content)
			b.WriteString("\n")
		} else {
			b.WriteString(full.Summary + "\n")
		}
	}
	return b.String()
}

// GenerateKIIndex returns a discovery index of all KIs.
// Each entry has title + summary, then artifact path on a separate indented line.
// Agent uses read_file on artifact paths to get full content.
func (s *Store) GenerateKIIndex() string {
	items, err := s.List()
	if err != nil || len(items) == 0 {
		return ""
	}

	var b strings.Builder
	for _, item := range items {
		prefix := ""
		if hasTag(item.Tags, "preference") {
			prefix = " [PREFERENCE]"
		}
		b.WriteString(fmt.Sprintf("- **%s**%s: %s\n", item.Title, prefix, item.Summary))
		artifacts := s.ListArtifacts(item.ID)
		for _, art := range artifacts {
			b.WriteString(fmt.Sprintf("  📄 %s\n", art))
		}
	}
	return b.String()
}

// GeneratePreferenceSummaries is kept as an alias for backward compatibility.
// Deprecated: Use GeneratePreferenceKI instead.
func (s *Store) GeneratePreferenceSummaries() string {
	return s.GeneratePreferenceKI()
}

// GenerateSummaries is kept as an alias for backward compatibility.
// Deprecated: Use GenerateKIIndex instead.
func (s *Store) GenerateSummaries() string {
	return s.GenerateKIIndex()
}

// ListArtifacts returns absolute paths of artifact files for a KI.
func (s *Store) ListArtifacts(id string) []string {
	artDir := filepath.Join(s.baseDir, id, "artifacts")
	entries, err := os.ReadDir(artDir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(s.baseDir, id, "artifacts", e.Name()))
	}
	return paths
}

// BaseDir returns the knowledge store root path.
func (s *Store) BaseDir() string { return s.baseDir }

// SaveDistilled implements the domain KIStore interface for auto-distillation hooks.
func (s *Store) SaveDistilled(title, summary, content string, tags, sources []string) error {
	return s.Save(&Item{
		Title:   title,
		Summary: summary,
		Content: content,
		Tags:    tags,
		Sources: sources,
	})
}

// MarkDeprecated marks a KI as deprecated and optionally links to its replacement.
// Implements the KIStore interface for Phase 2 governance.
func (s *Store) MarkDeprecated(id, supersededBy string) error {
	item, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("get KI for deprecation %s: %w", id, err)
	}
	item.Deprecated = true
	if supersededBy != "" {
		item.SupersededBy = supersededBy
	}
	item.UpdatedAt = time.Now()
	meta, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal KI %s: %w", id, err)
	}
	return os.WriteFile(filepath.Join(s.baseDir, id, "metadata.json"), meta, 0644)
}

// UpdateMerge appends content to an existing KI AND refreshes its metadata (summary, updated_at).
// This ensures dedup-merged KIs stay discoverable with current summaries.
func (s *Store) UpdateMerge(id, appendContent, newSummary string) error {
	// 1. Append to overview.md
	artPath := filepath.Join(s.baseDir, id, "artifacts", "overview.md")
	existing, err := os.ReadFile(artPath)
	if err != nil {
		return fmt.Errorf("read existing KI %s: %w", id, err)
	}
	if err := os.WriteFile(artPath, []byte(string(existing)+appendContent), 0644); err != nil {
		return fmt.Errorf("write KI %s: %w", id, err)
	}

	// 2. Refresh metadata.json
	item, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("get KI metadata %s: %w", id, err)
	}
	if newSummary != "" {
		item.Summary = newSummary
	}
	item.UpdatedAt = time.Now()
	meta, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal KI %s: %w", id, err)
	}
	return os.WriteFile(filepath.Join(s.baseDir, id, "metadata.json"), meta, 0644)
}

// ReplaceMerge replaces the entire content of a KI (LLM-consolidated merge).
// Unlike UpdateMerge which appends, this does a full replacement to keep KIs concise.
func (s *Store) ReplaceMerge(id, newContent, newSummary string) error {
	// 1. Write new content to overview.md
	artPath := filepath.Join(s.baseDir, id, "artifacts", "overview.md")
	if err := os.WriteFile(artPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write KI %s: %w", id, err)
	}

	// 2. Refresh metadata.json
	item, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("get KI metadata %s: %w", id, err)
	}
	if newSummary != "" {
		item.Summary = newSummary
	}
	item.Content = newContent
	item.UpdatedAt = time.Now()
	meta, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal KI %s: %w", id, err)
	}
	return os.WriteFile(filepath.Join(s.baseDir, id, "metadata.json"), meta, 0644)
}

// GetContent reads the authoritative content of a KI from overview.md.
func (s *Store) GetContent(id string) (string, error) {
	artPath := filepath.Join(s.baseDir, id, "artifacts", "overview.md")
	data, err := os.ReadFile(artPath)
	if err != nil {
		return "", fmt.Errorf("read KI %s content: %w", id, err)
	}
	return string(data), nil
}

// UpdateContent appends new content to an existing KI's overview.md artifact.
func (s *Store) UpdateContent(id, newContent string) error {
	artPath := filepath.Join(s.baseDir, id, "artifacts", "overview.md")
	existing, err := os.ReadFile(artPath)
	if err != nil {
		return fmt.Errorf("read existing KI %s: %w", id, err)
	}
	updated := string(existing) + newContent
	return os.WriteFile(artPath, []byte(updated), 0644)
}

// idUnsafe matches any character not suitable for directory names.
var idUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

func sanitizeID(title string) string {
	id := strings.ToLower(title)
	id = idUnsafe.ReplaceAllString(id, "_")
	id = strings.Trim(id, "_")
	if len(id) > 50 {
		id = id[:50]
	}
	// Append timestamp to prevent collision from similar titles
	id = fmt.Sprintf("%s_%d", id, time.Now().UnixMilli())
	return id
}

// hasTag checks if a tag exists in the tag list (case-insensitive).
func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, target) {
			return true
		}
	}
	return false
}
