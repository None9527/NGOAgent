package tool

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// GlobTool finds files matching a pattern.
// Uses fd (fast, supports ** globs, respects .gitignore) with fallback to find.
type GlobTool struct{}

func (t *GlobTool) Name() string { return "glob" }
func (t *GlobTool) Description() string {
	return `Find files matching a glob pattern.
- Searches recursively from the given path
- Returns matching file paths`
}

func (t *GlobTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": "Glob pattern (e.g., **/*.go, *.ts)"},
			"path":        map[string]any{"type": "string", "description": "Base directory path (default: cwd)"},
			"type":        map[string]any{"type": "string", "description": "Filter by type: file, directory, any (default: file)"},
			"extensions":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "File extensions to include (without leading dot)"},
			"excludes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to exclude"},
			"max_depth":   map[string]any{"type": "integer", "description": "Max directory depth"},
			"max_results": map[string]any{"type": "integer", "description": "Max results (default: 50)"},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	pattern, _ := args["pattern"].(string)
	basePath, _ := args["path"].(string)
	filterType, _ := args["type"].(string)
	maxResults := 50

	if v, ok := args["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}
	if basePath == "" {
		basePath = "."
	}
	if filterType == "" {
		filterType = "file"
	}

	// Try fd first (fast, supports ** globs, respects .gitignore)
	result, err := t.searchWithFd(ctx, pattern, basePath, filterType, args, maxResults)
	if err == nil {
		return result, nil
	}

	// Fallback to find
	return t.searchWithFind(ctx, pattern, basePath, filterType, maxResults)
}

func (t *GlobTool) searchWithFd(ctx context.Context, pattern, basePath, filterType string, args map[string]any, maxResults int) (dtool.ToolResult, error) {
	fdArgs := []string{"--glob", pattern, "--color", "never"}

	// Type filter
	switch filterType {
	case "file":
		fdArgs = append(fdArgs, "--type", "f")
	case "directory":
		fdArgs = append(fdArgs, "--type", "d")
	}

	// Extensions
	if exts, ok := args["extensions"].([]any); ok {
		for _, e := range exts {
			if s, ok := e.(string); ok {
				fdArgs = append(fdArgs, "--extension", s)
			}
		}
	}

	// Excludes
	if excludes, ok := args["excludes"].([]any); ok {
		for _, e := range excludes {
			if s, ok := e.(string); ok {
				fdArgs = append(fdArgs, "--exclude", s)
			}
		}
	}

	// Max depth
	if v, ok := args["max_depth"].(float64); ok && v > 0 {
		fdArgs = append(fdArgs, "--max-depth", fmt.Sprintf("%d", int(v)))
	}

	// Search directory
	fdArgs = append(fdArgs, basePath)

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "fd", fdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return dtool.ToolResult{}, fmt.Errorf("fd failed: %w", err)
	}

	return t.formatResults(string(output), pattern, basePath, maxResults), nil
}

func (t *GlobTool) searchWithFind(ctx context.Context, pattern, basePath, filterType string, maxResults int) (dtool.ToolResult, error) {
	findArgs := []string{basePath}

	// Type filter
	switch filterType {
	case "file":
		findArgs = append(findArgs, "-type", "f")
	case "directory":
		findArgs = append(findArgs, "-type", "d")
	}

	// Pattern — convert glob to find's -name
	namePattern := filepath.Base(pattern)
	if namePattern != "" && namePattern != "." {
		findArgs = append(findArgs, "-name", namePattern)
	}

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "find", findArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error searching: %v", err)}, nil
	}

	return t.formatResults(string(output), pattern, basePath, maxResults), nil
}

func (t *GlobTool) formatResults(raw, pattern, basePath string, maxResults int) dtool.ToolResult {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var matches []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			matches = append(matches, l)
		}
	}

	if len(matches) == 0 {
		return dtool.ToolResult{Output: "No files found matching pattern: " + pattern}
	}

	total := len(matches)
	truncated := false
	if total > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d file(s) matching %q in %q:\n", total, pattern, basePath))
	for _, m := range matches {
		b.WriteString(m + "\n")
	}
	if truncated {
		b.WriteString(fmt.Sprintf("\n... (%d files truncated, showing first %d)\n", total-maxResults, maxResults))
	}
	return dtool.ToolResult{Output: b.String()}
}
