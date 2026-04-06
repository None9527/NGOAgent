package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// TreeTool generates a directory tree, similar to the Unix `tree` command.
// P3 M2 (#45): Expands tool matrix to CC parity.
type TreeTool struct{}

func (t *TreeTool) Name() string { return "tree" }
func (t *TreeTool) Description() string {
	return `Display directory structure as a tree. Respects .gitignore. Default depth 3.`
}

func (t *TreeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":          map[string]any{"type": "string", "description": "Directory to display (absolute path or relative to workspace)"},
			"max_depth":     map[string]any{"type": "integer", "description": "Max recursion depth (default: 3, max: 8)"},
			"show_hidden":   map[string]any{"type": "boolean", "description": "Show hidden files/dirs (default: false)"},
			"include_files": map[string]any{"type": "boolean", "description": "Include files in tree (default: true)"},
		},
		"required": []string{"path"},
	}
}

func (t *TreeTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	dir, _ := args["path"].(string)
	if dir == "" {
		cwd, _ := os.Getwd()
		dir = cwd
	}

	maxDepth := 3
	if v, ok := args["max_depth"].(float64); ok && v > 0 {
		maxDepth = int(v)
		if maxDepth > 8 {
			maxDepth = 8
		}
	}

	showHidden := false
	if v, ok := args["show_hidden"].(bool); ok {
		showHidden = v
	}

	includeFiles := true
	if v, ok := args["include_files"].(bool); ok {
		includeFiles = v
	}

	// Load .gitignore patterns
	ignorer := loadGitignore(dir)

	info, err := os.Stat(dir)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}
	if !info.IsDir() {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: '%s' is not a directory", dir)}, nil
	}

	var sb strings.Builder
	sb.WriteString(dir + "\n")

	var walk func(path, prefix string, depth int)
	walk = func(path, prefix string, depth int) {
		if depth > maxDepth {
			return
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}

		// Filter and sort entries
		var filtered []os.DirEntry
		for _, e := range entries {
			name := e.Name()
			if !showHidden && strings.HasPrefix(name, ".") {
				continue
			}
			relPath, _ := filepath.Rel(dir, filepath.Join(path, name))
			if ignorer != nil && ignorer(relPath, e.IsDir()) {
				continue
			}
			if !includeFiles && !e.IsDir() {
				continue
			}
			filtered = append(filtered, e)
		}
		sort.Slice(filtered, func(i, j int) bool {
			// Directories first, then alphabetical
			iDir := filtered[i].IsDir()
			jDir := filtered[j].IsDir()
			if iDir != jDir {
				return iDir
			}
			return filtered[i].Name() < filtered[j].Name()
		})

		for i, e := range filtered {
			isLast := i == len(filtered)-1
			connector := "├── "
			childPrefix := prefix + "│   "
			if isLast {
				connector = "└── "
				childPrefix = prefix + "    "
			}

			extra := ""
			if e.IsDir() {
				extra = "/"
			}
			sb.WriteString(prefix + connector + e.Name() + extra + "\n")

			if e.IsDir() {
				walk(filepath.Join(path, e.Name()), childPrefix, depth+1)
			}
		}
	}

	walk(dir, "", 1)

	// Count summary
	var totalDirs, totalFiles int
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		if d.IsDir() {
			totalDirs++
		} else {
			totalFiles++
		}
		return nil
	})
	sb.WriteString(fmt.Sprintf("\n%d directories, %d files", totalDirs, totalFiles))

	return dtool.ToolResult{Output: sb.String()}, nil
}

// gitignoreFunc returns true if the given relative path should be ignored.
type gitignoreFunc func(relPath string, isDir bool) bool

// loadGitignore reads .gitignore from the given directory and returns a simple pattern matcher.
// Returns nil if no .gitignore found or parsing fails.
func loadGitignore(dir string) gitignoreFunc {
	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if len(patterns) == 0 {
		return nil
	}
	return func(relPath string, isDir bool) bool {
		for _, pat := range patterns {
			// Normalize: strip trailing slash from pattern (dir-only markers)
			dirOnly := strings.HasSuffix(pat, "/")
			p := strings.TrimRight(pat, "/")
			if dirOnly && !isDir {
				continue
			}
			// Simple glob: match against the base name or the full relative path
			base := filepath.Base(relPath)
			if ok, _ := filepath.Match(p, base); ok {
				return true
			}
			if ok, _ := filepath.Match(p, relPath); ok {
				return true
			}
		}
		return false
	}
}
