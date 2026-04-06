package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ─── P1 #36: Path validation with symlink resolution ────────────────────

// ValidatePath resolves symlinks and checks path is within allowed boundaries.
// Returns the resolved real path and an error if validation fails.
// This prevents symlink-based workspace escapes (e.g., symlink pointing outside workspace).
func ValidatePath(rawPath, workspace string) (string, error) {
	// Clean the path first
	cleaned := filepath.Clean(rawPath)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be absolute: %s", rawPath)
	}

	// Check path traversal patterns before resolving
	if strings.Contains(rawPath, "/../") || strings.HasSuffix(rawPath, "/..") {
		return "", fmt.Errorf("path traversal detected: %s", rawPath)
	}

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// File might not exist yet (write_file creates new files)
		// In that case, check the parent directory
		parentDir := filepath.Dir(cleaned)
		realParent, parentErr := filepath.EvalSymlinks(parentDir)
		if parentErr != nil {
			// Parent doesn't exist either — will be created, so just use cleaned path
			return cleaned, nil
		}
		realPath = filepath.Join(realParent, filepath.Base(cleaned))
	}

	// If workspace is set, verify the resolved path is within it
	if workspace != "" {
		wsReal, wsErr := filepath.EvalSymlinks(workspace)
		if wsErr == nil {
			workspace = wsReal
		}
		if !strings.HasPrefix(realPath, workspace+"/") && realPath != workspace {
			// Allow /tmp/ as a safe zone even outside workspace
			if !strings.HasPrefix(realPath, "/tmp/") {
				return "", fmt.Errorf("path %s resolves to %s which is outside workspace %s", rawPath, realPath, workspace)
			}
		}
	}

	// Block sensitive system paths regardless of workspace
	sensitive := []string{"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/root/.ssh/"}
	for _, s := range sensitive {
		if strings.HasPrefix(realPath, s) {
			return "", fmt.Errorf("access to sensitive system path blocked: %s", realPath)
		}
	}

	return realPath, nil
}
