package tool

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"
)

// globalFileWatcher is shared across all tools (same pattern as globalFileState).
var globalFileWatcher = NewFileWatcher()

// FileWatcher tracks inode+mtime for files modified through agent tools.
// When read_file is called, it compares the stored stat against the current
// filesystem state. If mismatched, the file was edited externally.
type FileWatcher struct {
	mu      sync.RWMutex
	tracked map[string]fileStat
}

type fileStat struct {
	Inode     uint64
	Mtime     time.Time
	Size      int64
	WrittenAt time.Time // when the agent last wrote this file
}

// NewFileWatcher creates a new FileWatcher.
func NewFileWatcher() *FileWatcher {
	return &FileWatcher{
		tracked: make(map[string]fileStat),
	}
}

// RecordWrite snapshots the inode+mtime of a file after the agent writes it.
// Call after successful write_file or edit_file.
func (fw *FileWatcher) RecordWrite(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	stat := fileStat{
		Mtime:     info.ModTime(),
		Size:      info.Size(),
		WrittenAt: time.Now(),
	}

	// Extract inode on Linux/macOS
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		stat.Inode = sys.Ino
	}

	fw.mu.Lock()
	fw.tracked[path] = stat
	fw.mu.Unlock()
}

// CheckRead compares stored stat against current filesystem state.
// Returns (wasModifiedExternally, warningMessage).
// If the file was never written by the agent, returns (false, "").
func (fw *FileWatcher) CheckRead(path string) (bool, string) {
	fw.mu.RLock()
	stored, ok := fw.tracked[path]
	fw.mu.RUnlock()

	if !ok {
		return false, "" // never written by agent, nothing to compare
	}

	info, err := os.Stat(path)
	if err != nil {
		return false, "" // file gone or inaccessible
	}

	// Compare mtime
	if !info.ModTime().Equal(stored.Mtime) {
		age := time.Since(stored.WrittenAt).Round(time.Second)
		msg := fmt.Sprintf("⚠️ File modified externally since last agent edit (%s ago). "+
			"Size: %d→%d bytes, mtime changed.", age, stored.Size, info.Size())
		slog.Info(fmt.Sprintf("[file-watcher] External modification detected: %s", path))
		return true, msg
	}

	// Compare inode (detects mv+recreate patterns)
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		if stored.Inode != 0 && sys.Ino != stored.Inode {
			msg := "⚠️ File was replaced (different inode) since last agent edit."
			slog.Info(fmt.Sprintf("[file-watcher] Inode change detected: %s (was %d, now %d)",
				path, stored.Inode, sys.Ino))
			return true, msg
		}
	}

	return false, ""
}

// TrackedCount returns the number of files being tracked.
func (fw *FileWatcher) TrackedCount() int {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return len(fw.tracked)
}
