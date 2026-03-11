package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// FileState tracks read/write state for edit_file safety.
// Ensures: files must be read before editing, and detects external modifications.
type FileState struct {
	mu    sync.RWMutex
	files map[string]*fileEntry
}

type fileEntry struct {
	ContentHash string
	LastReadAt  time.Time
	ReadBy      string // Tool that read it (read_file / glob / etc.)
}

// NewFileState creates a file state tracker.
func NewFileState() *FileState {
	return &FileState{
		files: make(map[string]*fileEntry),
	}
}

// MarkRead records that a file was read with the given content hash.
func (fs *FileState) MarkRead(path string, content []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[path] = &fileEntry{
		ContentHash: hashContent256(content),
		LastReadAt:  time.Now(),
		ReadBy:      "read_file",
	}
}

// WasRead checks if a file was previously read (for E6 error code).
func (fs *FileState) WasRead(path string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	_, ok := fs.files[path]
	return ok
}

// HasChanged checks if a file's content has changed since it was last read (for E7 error code).
func (fs *FileState) HasChanged(path string, currentContent []byte) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	entry, ok := fs.files[path]
	if !ok {
		return false // Not tracked
	}
	return entry.ContentHash != hashContent256(currentContent)
}

// MarkModified updates the hash after a successful edit.
func (fs *FileState) MarkModified(path string, newContent []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if entry, ok := fs.files[path]; ok {
		entry.ContentHash = hashContent256(newContent)
	}
}

// Remove clears tracking for a file (e.g., after deletion).
func (fs *FileState) Remove(path string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.files, path)
}

func hashContent256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
