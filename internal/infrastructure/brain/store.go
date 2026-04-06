// Package brain provides session-scoped artifact storage.
// Brain stores working documents (task plans, walkthroughs, checkpoints)
// within conversation-specific directories.
package brain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ArtifactStore manages brain artifacts for a conversation.
type ArtifactStore struct {
	baseDir      string // ~/.ngoagent/brain/<session_id>/
	workspaceDir string // workspace root for file resolution
}

// BaseDir returns the session-scoped brain directory path.
func (s *ArtifactStore) BaseDir() string { return s.baseDir }

// SetWorkspaceDir sets the workspace root for the Resolution Pipeline.
func (s *ArtifactStore) SetWorkspaceDir(dir string) { s.workspaceDir = dir }

type contextKey string

const brainDirKey contextKey = "brain_dir"
const brainStoreKey contextKey = "brain_store"

// ContextWithBrainDir injects the brain session directory into context (legacy).
func ContextWithBrainDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, brainDirKey, dir)
}

// BrainDirFromContext extracts the brain session directory from context.
func BrainDirFromContext(ctx context.Context) string {
	// Prefer store-based lookup
	if store := BrainStoreFromContext(ctx); store != nil {
		return store.BaseDir()
	}
	if v, ok := ctx.Value(brainDirKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithBrainStore injects the brain store into context.
// Accepts any to allow domain-side interface injection; BrainStoreFromContext performs type assertion.
func ContextWithBrainStore(ctx context.Context, store any) context.Context {
	return context.WithValue(ctx, brainStoreKey, store)
}

// BrainStoreFromContext extracts the ArtifactStore from context.
func BrainStoreFromContext(ctx context.Context) *ArtifactStore {
	if v, ok := ctx.Value(brainStoreKey).(*ArtifactStore); ok {
		return v
	}
	return nil
}

// NewArtifactStore creates an artifact store for a conversation.
func NewArtifactStore(brainDir, sessionID string) *ArtifactStore {
	dir := filepath.Join(brainDir, sessionID)
	os.MkdirAll(dir, 0755)
	return &ArtifactStore{baseDir: dir}
}

// NewArtifactStoreFromDir creates an artifact store from an existing session directory.
func NewArtifactStoreFromDir(dir string) *ArtifactStore {
	os.MkdirAll(dir, 0755)
	return &ArtifactStore{baseDir: dir}
}

// ArtifactMetadata stores metadata alongside each artifact file.
type ArtifactMetadata struct {
	Summary      string    `json:"summary"`
	ArtifactType string    `json:"artifact_type"` // task, plan, walkthrough, other
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Version      int       `json:"version"`
}

// Write saves an artifact file with automatic version rotation.
// Only .md files get version rotation (.resolved.N); JSON files are overwritten in-place.
func (s *ArtifactStore) Write(name, content string) error {
	path := filepath.Join(s.baseDir, name)
	os.MkdirAll(filepath.Dir(path), 0755)

	// Rotate: only .md files get version chain
	if strings.HasSuffix(name, ".md") {
		if _, err := os.Stat(path); err == nil {
			s.rotateVersion(path)
		}
	}

	// Write source file
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	// Resolution Pipeline: generate .resolved version with deep links
	if strings.HasSuffix(name, ".md") && s.workspaceDir != "" {
		resolved := s.resolveLinks(content)
		os.WriteFile(path+".resolved", []byte(resolved), 0644)
	}

	// Update metadata (create if new, update if existing)
	s.touchMetadata(path, "")
	return nil
}

// WriteArtifact saves an artifact with explicit metadata (Summary, Type).
// Only .md files get version rotation; JSON/other files are overwritten in-place.
func (s *ArtifactStore) WriteArtifact(name, content, summary, artifactType string) error {
	path := filepath.Join(s.baseDir, name)
	os.MkdirAll(filepath.Dir(path), 0755)

	// Only .md files get version chain
	if strings.HasSuffix(name, ".md") {
		if _, err := os.Stat(path); err == nil {
			s.rotateVersion(path)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	// Resolution Pipeline: generate .resolved version with deep links
	if strings.HasSuffix(name, ".md") && s.workspaceDir != "" {
		resolved := s.resolveLinks(content)
		os.WriteFile(path+".resolved", []byte(resolved), 0644)
	}

	// Write metadata JSON
	meta := s.loadMetadata(path)
	meta.Summary = summary
	meta.ArtifactType = artifactType
	meta.UpdatedAt = time.Now()
	meta.Version++
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = meta.UpdatedAt
	}
	return s.saveMetadata(path, meta)
}

// rotateVersion archives the current file content as .resolved.N (version history only).
// NOTE: Does NOT write .resolved — that's managed by the Resolution Pipeline.
func (s *ArtifactStore) rotateVersion(path string) {
	// Find next version number
	ver := 0
	for {
		resolved := fmt.Sprintf("%s.resolved.%d", path, ver)
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			break
		}
		ver++
	}
	// Copy current source to .resolved.N
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.WriteFile(fmt.Sprintf("%s.resolved.%d", path, ver), data, 0644)
}

// touchMetadata creates or updates the metadata timestamp.
func (s *ArtifactStore) touchMetadata(path, summary string) {
	meta := s.loadMetadata(path)
	meta.UpdatedAt = time.Now()
	meta.Version++
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = meta.UpdatedAt
	}
	if summary != "" {
		meta.Summary = summary
	}
	s.saveMetadata(path, meta)
}

// loadMetadata reads the .metadata.json for a file.
func (s *ArtifactStore) loadMetadata(path string) ArtifactMetadata {
	metaPath := path + ".metadata.json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ArtifactMetadata{}
	}
	var meta ArtifactMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		fmt.Printf("[brain] WARN: corrupt metadata %s: %v\n", metaPath, err)
	}
	return meta
}

// saveMetadata writes the .metadata.json for a file.
func (s *ArtifactStore) saveMetadata(path string, meta ArtifactMetadata) error {
	metaPath := path + ".metadata.json"
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}

// ReadMetadata reads the metadata for an artifact.
func (s *ArtifactStore) ReadMetadata(name string) (*ArtifactMetadata, error) {
	path := filepath.Join(s.baseDir, name)
	meta := s.loadMetadata(path)
	if meta.CreatedAt.IsZero() {
		return nil, fmt.Errorf("no metadata for %s", name)
	}
	return &meta, nil
}

// ListVersions returns available version numbers for an artifact.
func (s *ArtifactStore) ListVersions(name string) []int {
	path := filepath.Join(s.baseDir, name)
	var versions []int
	for ver := 0; ; ver++ {
		resolved := fmt.Sprintf("%s.resolved.%d", path, ver)
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			break
		}
		versions = append(versions, ver)
	}
	return versions
}

// ReadVersion reads a specific version of an artifact.
func (s *ArtifactStore) ReadVersion(name string, ver int) (string, error) {
	path := fmt.Sprintf("%s.resolved.%d", filepath.Join(s.baseDir, name), ver)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Read loads an artifact file.
func (s *ArtifactStore) Read(name string) (string, error) {
	path := filepath.Join(s.baseDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// List returns all artifact files in this conversation's brain.
func (s *ArtifactStore) List() ([]string, error) {
	var files []string
	err := filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(s.baseDir, path)
		files = append(files, rel)
		return nil
	})
	return files, err
}

// Dir returns the base directory.
func (s *ArtifactStore) Dir() string { return s.baseDir }

// ArtifactMeta holds metadata about an artifact file.
type ArtifactMeta struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetMeta returns metadata for an artifact.
func (s *ArtifactStore) GetMeta(name string) (*ArtifactMeta, error) {
	path := filepath.Join(s.baseDir, name)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &ArtifactMeta{
		Name:      name,
		Size:      info.Size(),
		Hash:      hashContent(string(data)),
		CreatedAt: info.ModTime(),
		UpdatedAt: info.ModTime(),
	}, nil
}

// Snapshot creates a versioned copy of the current brain state.
func (s *ArtifactStore) Snapshot(label string) error {
	snapshotDir := filepath.Join(s.baseDir, ".snapshots", label)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	// Copy all artifact files to snapshot
	return filepath.Walk(s.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(s.baseDir, path)
		// Skip .snapshots directory itself
		if filepath.HasPrefix(rel, ".snapshots") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		dest := filepath.Join(snapshotDir, rel)
		os.MkdirAll(filepath.Dir(dest), 0755)
		return os.WriteFile(dest, data, 0644)
	})
}

// ListSnapshots returns available snapshot labels.
func (s *ArtifactStore) ListSnapshots() ([]string, error) {
	snapshotDir := filepath.Join(s.baseDir, ".snapshots")
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return nil, nil // No snapshots yet
	}
	var labels []string
	for _, e := range entries {
		if e.IsDir() {
			labels = append(labels, e.Name())
		}
	}
	return labels, nil
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// ═══════════════════════════════════════════
// Resolution Pipeline (Anti §三)
// ═══════════════════════════════════════════

var backtickFileRe = regexp.MustCompile("`([a-zA-Z0-9_.-]+\\.[a-zA-Z]{1,4})`")
var absPathRe = regexp.MustCompile(`(/[a-zA-Z0-9_./+-]+\.[a-zA-Z]{1,4})`)
var fileTagRe = regexp.MustCompile(`\[(MODIFY|NEW|DELETE)\]\s+(\S+\.[a-zA-Z]{1,4})`)

// resolveLinks converts filenames and paths to file:// URIs.
// Handles: backtick-quoted filenames, absolute paths, and [MODIFY]/[NEW]/[DELETE] tags.
func (s *ArtifactStore) resolveLinks(content string) string {
	if s.workspaceDir == "" {
		return content
	}

	// Phase 1: Convert backtick-quoted filenames
	result := backtickFileRe.ReplaceAllStringFunc(content, func(match string) string {
		name := strings.Trim(match, "`")
		if absPath := s.findFile(name); absPath != "" {
			return fmt.Sprintf("[%s](file://%s)", name, absPath)
		}
		return match
	})

	// Phase 2: Convert [MODIFY]/[NEW]/[DELETE] tagged file references
	result = fileTagRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := fileTagRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		tag, name := parts[1], parts[2]
		// Skip if already a link
		if strings.Contains(match, "file://") {
			return match
		}
		if absPath := s.findFile(filepath.Base(name)); absPath != "" {
			return fmt.Sprintf("[%s] [%s](file://%s)", tag, name, absPath)
		}
		return match
	})

	// Phase 3: Convert bare absolute paths that exist on disk
	result = absPathRe.ReplaceAllStringFunc(result, func(match string) string {
		// Skip if already inside a markdown link
		if strings.Contains(match, "file://") {
			return match
		}
		if _, err := os.Stat(match); err == nil {
			return fmt.Sprintf("[%s](file://%s)", filepath.Base(match), match)
		}
		return match
	})

	return result
}

// findFile searches the workspace for a file by basename.
func (s *ArtifactStore) findFile(name string) string {
	var found string
	filepath.Walk(s.workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Skip hidden dirs and common noise
		rel, _ := filepath.Rel(s.workspaceDir, path)
		if strings.Contains(rel, "/.git/") || strings.Contains(rel, "/vendor/") || strings.Contains(rel, "/node_modules/") {
			return nil
		}
		if filepath.Base(path) == name {
			found = path
			return filepath.SkipAll // stop at first match
		}
		return nil
	})
	return found
}
