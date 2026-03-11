package forge

import (
	"fmt"
	"os"
	"path/filepath"
)

// SandboxManager creates and manages isolated forge sandboxes.
type SandboxManager struct {
	baseDir string // /tmp/ngoagent-forge/
}

// NewSandboxManager creates a sandbox manager.
func NewSandboxManager(baseDir string) *SandboxManager {
	return &SandboxManager{baseDir: baseDir}
}

// Create creates a new sandbox directory, returns its path.
func (sm *SandboxManager) Create(id string) (string, error) {
	path := filepath.Join(sm.baseDir, id)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}
	return path, nil
}

// WriteFiles writes multiple files into the sandbox.
func (sm *SandboxManager) WriteFiles(sandboxPath string, files map[string]string) error {
	for relPath, content := range files {
		absPath := filepath.Join(sandboxPath, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(absPath), err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
	}
	return nil
}

// Cleanup removes a sandbox directory.
func (sm *SandboxManager) Cleanup(id string) error {
	path := filepath.Join(sm.baseDir, id)
	return os.RemoveAll(path)
}

// Path returns the sandbox directory path for a given ID.
func (sm *SandboxManager) Path(id string) string {
	return filepath.Join(sm.baseDir, id)
}

// Exists checks if a sandbox exists.
func (sm *SandboxManager) Exists(id string) bool {
	_, err := os.Stat(filepath.Join(sm.baseDir, id))
	return err == nil
}
