package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// FindFilesTool searches for files by name pattern, size, or modification time.
// P3 M2 (#45): Expands tool matrix to CC parity.
type FindFilesTool struct{}

func (t *FindFilesTool) Name() string { return "find_files" }
func (t *FindFilesTool) Description() string {
	return `Search for files by name, size, or modification time. More flexible than glob for complex searches.`
}

func (t *FindFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":            map[string]any{"type": "string", "description": "Search root directory"},
			"pattern":         map[string]any{"type": "string", "description": "Glob name pattern (e.g. '*.go')"},
			"min_size":        map[string]any{"type": "integer", "description": "Minimum file size in bytes"},
			"max_size":        map[string]any{"type": "integer", "description": "Maximum file size in bytes"},
			"modified_after":  map[string]any{"type": "string", "description": "Only files modified after (RFC3339 or relative like '7d', '24h')"},
			"modified_before": map[string]any{"type": "string", "description": "Only files modified before"},
			"max_results":     map[string]any{"type": "integer", "description": "Max results (default: 100)"},
			"exclude_dirs":    map[string]any{"type": "string", "description": "Comma-separated dir names to exclude"},
		},
		"required": []string{"path"},
	}
}

func (t *FindFilesTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	root, _ := args["path"].(string)
	if root == "" {
		return dtool.ToolResult{Output: "Error: 'path' is required"}, nil
	}
	pattern, _ := args["pattern"].(string)

	var minSize, maxSize int64
	if v, ok := args["min_size"].(float64); ok {
		minSize = int64(v)
	}
	if v, ok := args["max_size"].(float64); ok {
		maxSize = int64(v)
	}

	maxResults := 100
	if v, ok := args["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}

	// Build excluded dirs set
	excludeDirs := map[string]bool{".git": true, "node_modules": true}
	if v, ok := args["exclude_dirs"].(string); ok && v != "" {
		for _, d := range strings.Split(v, ",") {
			excludeDirs[strings.TrimSpace(d)] = true
		}
	}

	// Parse time filters
	var afterTime, beforeTime time.Time
	if v, ok := args["modified_after"].(string); ok && v != "" {
		afterTime = parseRelativeTime(v)
	}
	if v, ok := args["modified_before"].(string); ok && v != "" {
		beforeTime = parseRelativeTime(v)
	}

	var results []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if excludeDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		// Pattern filter
		if pattern != "" {
			matched, _ := filepath.Match(pattern, d.Name())
			if !matched {
				// Also try matching on full path segments
				return nil
			}
		}

		// Size filter
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		if minSize > 0 && size < minSize {
			return nil
		}
		if maxSize > 0 && size > maxSize {
			return nil
		}

		// Time filter
		mod := info.ModTime()
		if !afterTime.IsZero() && mod.Before(afterTime) {
			return nil
		}
		if !beforeTime.IsZero() && mod.After(beforeTime) {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		results = append(results, fmt.Sprintf("%s\t%d bytes\t%s", rel, size, mod.Format("2006-01-02 15:04")))
		return nil
	})
	if err != nil && !strings.Contains(err.Error(), "SkipAll") {
		return dtool.ToolResult{Output: fmt.Sprintf("Error walking directory: %v", err)}, nil
	}

	if len(results) == 0 {
		return dtool.ToolResult{Output: "No files found matching the given criteria."}, nil
	}

	sort.Strings(results)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d file(s) in %s:\n\n", len(results), root))
	for _, r := range results {
		sb.WriteString(r + "\n")
	}
	if len(results) >= maxResults {
		sb.WriteString(fmt.Sprintf("\n(truncated at %d results — use max_results to increase)", maxResults))
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

// parseRelativeTime parses a time string: RFC3339 or relative ("7d", "24h", "30m").
func parseRelativeTime(s string) time.Time {
	// Try relative duration first
	if strings.HasSuffix(s, "d") {
		if days, err := parseInt(s[:len(s)-1]); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		}
	}
	if strings.HasSuffix(s, "h") {
		if hours, err := parseInt(s[:len(s)-1]); err == nil {
			return time.Now().Add(-time.Duration(hours) * time.Hour)
		}
	}
	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try date only
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
