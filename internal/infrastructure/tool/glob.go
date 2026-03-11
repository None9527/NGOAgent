package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// GlobTool finds files matching a glob pattern recursively.
type GlobTool struct{}

func (t *GlobTool) Name() string        { return "glob" }
func (t *GlobTool) Description() string { return prompttext.ToolGlob }

func (t *GlobTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": "Glob pattern (e.g., **/*.go)"},
			"path":        map[string]any{"type": "string", "description": "Base directory path (default: cwd)"},
			"max_results": map[string]any{"type": "integer", "description": "Max results (default: 100)"},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	pattern, _ := args["pattern"].(string)
	basePath, _ := args["path"].(string)
	maxResults := 100

	if v, ok := args["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}

	if basePath == "" {
		basePath = "."
	}

	var matches []string
	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if len(matches) >= maxResults {
			return filepath.SkipAll
		}

		// Match against filename
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error walking directory: %v", err)}, nil
	}

	if len(matches) == 0 {
		return dtool.ToolResult{Output: "No files found matching pattern: " + pattern}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d files:\n", len(matches)))
	for _, m := range matches {
		b.WriteString(m + "\n")
	}
	if len(matches) >= maxResults {
		b.WriteString(fmt.Sprintf("\n... (capped at %d results)\n", maxResults))
	}
	return dtool.ToolResult{Output: b.String()}, nil
}
