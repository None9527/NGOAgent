// Package workspace provides project-level knowledge and state storage.
// file_history.go implements file edit history with snapshot-based rollback capability.
// FileHistory mechanism: backup files before edit,
// snapshot state per message, rewind to any snapshot point.
package workspace

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxSnapshots = 50

// BackupEntry records a single file backup.
type BackupEntry struct {
	BackupPath string    // Full path to backup file on disk (empty if file was new)
	Version    int       // Incremental version number
	BackupTime time.Time // When the backup was created
	IsNew      bool      // True if file did not exist before this edit
}

// Snapshot captures the state of all tracked files at a specific message point.
type Snapshot struct {
	MessageID string                  // Associated message identifier
	Backups   map[string]BackupEntry  // normalizedPath → backup entry
	Timestamp time.Time
}

// FileHistory manages file edit history with backup and rollback support.
type FileHistory struct {
	mu           sync.Mutex
	baseDir      string            // .ngoagent/workspace/.file-history/<sessionID>/
	sessionID    string
	trackedFiles map[string]bool   // Set of tracked file paths
	snapshots    []Snapshot
	pendingEdits map[string]BackupEntry // Edits since last snapshot
}

// NewFileHistory creates a FileHistory for the given workspace and session.
func NewFileHistory(workDir, sessionID string) *FileHistory {
	baseDir := filepath.Join(workDir, ".ngoagent", "workspace", ".file-history", sessionID)
	os.MkdirAll(baseDir, 0755)
	return &FileHistory{
		baseDir:      baseDir,
		sessionID:    sessionID,
		trackedFiles: make(map[string]bool),
		pendingEdits: make(map[string]BackupEntry),
	}
}

// TrackEdit backs up a file before it is modified.
// Should be called BEFORE the actual file write in edit_file/write_file tools.
func (fh *FileHistory) TrackEdit(filePath string) error {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	normalized := filepath.Clean(filePath)

	// Already backed up in this pending batch — skip
	if _, exists := fh.pendingEdits[normalized]; exists {
		return nil
	}

	fh.trackedFiles[normalized] = true

	info, statErr := os.Stat(normalized)
	if statErr != nil && os.IsNotExist(statErr) {
		// File doesn't exist yet — mark as new (rewind will delete it)
		fh.pendingEdits[normalized] = BackupEntry{
			BackupPath: "",
			Version:    fh.nextVersion(normalized),
			BackupTime: time.Now(),
			IsNew:      true,
		}
		log.Printf("[file-history] tracked new file: %s", normalized)
		return nil
	} else if statErr != nil {
		return fmt.Errorf("file-history: stat %s: %w", normalized, statErr)
	}

	// Read and backup existing file
	data, err := os.ReadFile(normalized)
	if err != nil {
		return fmt.Errorf("file-history: read %s: %w", normalized, err)
	}

	version := fh.nextVersion(normalized)
	backupName := fh.backupFileName(normalized, version)
	backupPath := filepath.Join(fh.baseDir, backupName)

	if err := os.WriteFile(backupPath, data, info.Mode()); err != nil {
		return fmt.Errorf("file-history: backup write %s: %w", backupPath, err)
	}

	fh.pendingEdits[normalized] = BackupEntry{
		BackupPath: backupPath,
		Version:    version,
		BackupTime: time.Now(),
		IsNew:      false,
	}
	log.Printf("[file-history] backed up %s → %s (v%d, %d bytes)", normalized, backupName, version, len(data))
	return nil
}

// Snapshot creates a snapshot for the given message ID, capturing all pending edits.
// Should be called after all tool calls for a message are complete.
func (fh *FileHistory) Snapshot(messageID string) error {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	if len(fh.pendingEdits) == 0 {
		return nil // No edits to snapshot
	}

	// Build snapshot from pending edits
	backups := make(map[string]BackupEntry, len(fh.pendingEdits))
	for path, entry := range fh.pendingEdits {
		backups[path] = entry
	}

	snapshot := Snapshot{
		MessageID: messageID,
		Backups:   backups,
		Timestamp: time.Now(),
	}

	fh.snapshots = append(fh.snapshots, snapshot)

	// Enforce max snapshots
	if len(fh.snapshots) > maxSnapshots {
		fh.snapshots = fh.snapshots[len(fh.snapshots)-maxSnapshots:]
	}

	// Clear pending edits
	fh.pendingEdits = make(map[string]BackupEntry)

	log.Printf("[file-history] snapshot %s: %d files", messageID, len(backups))
	return nil
}

// Rewind restores all tracked files to the state recorded in the specified snapshot.
// Returns the list of files that were restored.
func (fh *FileHistory) Rewind(messageID string) ([]string, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	// Find target snapshot
	targetIdx := -1
	for i := len(fh.snapshots) - 1; i >= 0; i-- {
		if fh.snapshots[i].MessageID == messageID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return nil, fmt.Errorf("file-history: snapshot %s not found", messageID)
	}

	// Collect all files modified AFTER the target snapshot
	var restored []string
	for i := targetIdx; i < len(fh.snapshots); i++ {
		for path, entry := range fh.snapshots[i].Backups {
			if entry.IsNew {
				// File was created by agent — delete it
				if err := os.Remove(path); err == nil {
					restored = append(restored, path)
					log.Printf("[file-history] rewind: deleted %s (was new)", path)
				}
			} else if entry.BackupPath != "" {
				// Restore from backup
				data, err := os.ReadFile(entry.BackupPath)
				if err != nil {
					log.Printf("[file-history] rewind: failed to read backup %s: %v", entry.BackupPath, err)
					continue
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					log.Printf("[file-history] rewind: failed to restore %s: %v", path, err)
					continue
				}
				restored = append(restored, path)
				log.Printf("[file-history] rewind: restored %s from v%d", path, entry.Version)
			}
		}
	}

	// Trim snapshots after the target (they're now invalid)
	fh.snapshots = fh.snapshots[:targetIdx]
	fh.pendingEdits = make(map[string]BackupEntry)

	log.Printf("[file-history] rewind to %s complete: %d files restored", messageID, len(restored))
	return restored, nil
}

// ListSnapshots returns a summary of all available snapshots.
func (fh *FileHistory) ListSnapshots() []SnapshotInfo {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	infos := make([]SnapshotInfo, len(fh.snapshots))
	for i, s := range fh.snapshots {
		files := make([]string, 0, len(s.Backups))
		for path := range s.Backups {
			files = append(files, path)
		}
		infos[i] = SnapshotInfo{
			MessageID: s.MessageID,
			Timestamp: s.Timestamp,
			Files:     files,
		}
	}
	return infos
}

// SnapshotInfo is a read-only summary of a snapshot.
type SnapshotInfo struct {
	MessageID string
	Timestamp time.Time
	Files     []string
}

// HasPendingEdits returns true if there are tracked edits not yet snapshotted.
func (fh *FileHistory) HasPendingEdits() bool {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return len(fh.pendingEdits) > 0
}

// --- internal helpers ---

// nextVersion returns the next version number for a file path.
func (fh *FileHistory) nextVersion(path string) int {
	maxV := 0
	// Check pending edits
	if entry, ok := fh.pendingEdits[path]; ok && entry.Version > maxV {
		maxV = entry.Version
	}
	// Check all snapshots
	for _, s := range fh.snapshots {
		if entry, ok := s.Backups[path]; ok && entry.Version > maxV {
			maxV = entry.Version
		}
	}
	return maxV + 1
}

// backupFileName generates a deterministic backup filename: <hash_prefix>@v<N>
func (fh *FileHistory) backupFileName(path string, version int) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x@v%d", h[:8], version)
}

// CleanupBackups removes all backup files for this session (call on session end if desired).
func (fh *FileHistory) CleanupBackups() error {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	entries, err := os.ReadDir(fh.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(fh.baseDir, e.Name()))
	}
	return os.Remove(fh.baseDir)
}

// BaseDir returns the backup storage directory.
func (fh *FileHistory) BaseDir() string {
	return fh.baseDir
}
