package tool

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// GrepSearchTool searches file contents using ripgrep.
type GrepSearchTool struct{}

func (t *GrepSearchTool) Name() string        { return "grep_search" }
func (t *GrepSearchTool) Description() string { return prompttext.ToolGrepSearch }

func (t *GrepSearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":       map[string]any{"type": "string", "description": "Search term or regex pattern"},
			"path":        map[string]any{"type": "string", "description": "Directory or file to search (default: cwd)"},
			"is_regex":    map[string]any{"type": "boolean", "description": "Treat query as regex (default: false)"},
			"includes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to filter files (e.g., *.go)"},
			"max_results": map[string]any{"type": "integer", "description": "Max results (default: 50)"},
		},
		"required": []string{"query"},
	}
}

func (t *GrepSearchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	query, _ := args["query"].(string)
	path, _ := args["path"].(string)
	isRegex, _ := args["is_regex"].(bool)
	maxResults := 50

	if v, ok := args["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}
	if path == "" {
		path = "."
	}

	rgArgs := []string{"--json", "-n", "--max-count", fmt.Sprintf("%d", maxResults)}
	if !isRegex {
		rgArgs = append(rgArgs, "--fixed-strings")
	}

	// File type filters
	if includes, ok := args["includes"].([]any); ok {
		for _, inc := range includes {
			if s, ok := inc.(string); ok {
				rgArgs = append(rgArgs, "--glob", s)
			}
		}
	}

	rgArgs = append(rgArgs, query, path)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "rg", rgArgs...)
	output, err := cmd.CombinedOutput()

	if execCtx.Err() == context.DeadlineExceeded {
		return dtool.ToolResult{Output: "Search timed out after 30s"}, nil
	}

	// rg exits 1 on no match, 2+ on error
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return dtool.ToolResult{Output: "No matches found for: " + query}, nil
			}
		}
		// Fallback to grep if rg not available
		return t.fallbackGrep(ctx, query, path, isRegex, maxResults)
	}

	result := string(output)
	if len(result) > 50*1024 {
		result = result[:50*1024] + "\n... (output truncated)"
	}
	return dtool.ToolResult{Output: result}, nil
}

func (t *GrepSearchTool) fallbackGrep(ctx context.Context, query, path string, isRegex bool, maxResults int) (dtool.ToolResult, error) {
	grepArgs := []string{"-rnI", "--max-count", fmt.Sprintf("%d", maxResults)}
	if !isRegex {
		grepArgs = append(grepArgs, "-F")
	}
	grepArgs = append(grepArgs, query, path)

	cmd := exec.CommandContext(ctx, "grep", grepArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return dtool.ToolResult{Output: "No matches found for: " + query}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Search error: %v", err)}, nil
	}
	return dtool.ToolResult{Output: strings.TrimSpace(string(output))}, nil
}
