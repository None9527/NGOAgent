package tool

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// GrepSearchTool searches file contents using ripgrep.
// Output format mirrors CC/Anti: "File: path\nL{n}: content\n---"
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
			"max_results": map[string]any{"type": "integer", "description": "Max matching lines to return (default: 50)"},
		},
		"required": []string{"query"},
	}
}

// grepMatch holds a single match result.
type grepMatch struct {
	File    string
	Line    int
	Content string
}

const maxOutputChars = 50 * 1024 // 50KB char limit (safety net)

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

	// Build rg args: plain text output with line numbers and file paths
	rgArgs := []string{"-n", "-H", "--no-heading", "--color", "never", "--smart-case"}
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

	// Parse rg output (format: "file:line:content")
	matches := parseGrepOutput(string(output))

	if len(matches) == 0 {
		return dtool.ToolResult{Output: "No matches found for: " + query}, nil
	}

	// Format: group by file → "File: path\nL{n}: content\n---"
	return t.formatMatches(matches, query, path, maxResults), nil
}

// parseGrepOutput parses "file:line:content" lines from rg/grep output.
func parseGrepOutput(raw string) []grepMatch {
	var matches []grepMatch
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse "file:line:content" — find first two colons
		first := strings.Index(line, ":")
		if first < 0 {
			continue
		}
		rest := line[first+1:]
		second := strings.Index(rest, ":")
		if second < 0 {
			continue
		}
		filePath := line[:first]
		lineNum, err := strconv.Atoi(rest[:second])
		if err != nil {
			continue
		}
		content := rest[second+1:]
		matches = append(matches, grepMatch{File: filePath, Line: lineNum, Content: content})
	}
	return matches
}

// formatMatches groups matches by file and formats CC/Anti-style output.
func (t *GrepSearchTool) formatMatches(matches []grepMatch, query, searchPath string, maxResults int) dtool.ToolResult {
	totalMatches := len(matches)

	// Apply line limit
	truncated := false
	if totalMatches > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}

	// Group by file, preserving first-seen order
	type fileGroup struct {
		path    string
		matches []grepMatch
	}
	seen := map[string]int{}
	var groups []fileGroup
	for _, m := range matches {
		if idx, ok := seen[m.File]; ok {
			groups[idx].matches = append(groups[idx].matches, m)
		} else {
			seen[m.File] = len(groups)
			groups = append(groups, fileGroup{path: m.File, matches: []grepMatch{m}})
		}
	}

	// Sort matches within each file by line number
	for i := range groups {
		sort.Slice(groups[i].matches, func(a, b int) bool {
			return groups[i].matches[a].Line < groups[i].matches[b].Line
		})
	}

	// Build output
	matchTerm := "matches"
	if totalMatches == 1 {
		matchTerm = "match"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d %s for pattern %q in %q:\n---\n", totalMatches, matchTerm, query, searchPath))

	for _, g := range groups {
		b.WriteString(fmt.Sprintf("File: %s\n", g.path))
		for _, m := range g.matches {
			content := strings.TrimSpace(m.Content)
			// Truncate very long lines
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			b.WriteString(fmt.Sprintf("L%d: %s\n", m.Line, content))
		}
		b.WriteString("---\n")
	}

	if truncated {
		omitted := totalMatches - maxResults
		b.WriteString(fmt.Sprintf("[%d lines truncated]\n", omitted))
	}

	// Safety: char limit
	result := b.String()
	charTruncated := false
	if len(result) > maxOutputChars {
		result = result[:maxOutputChars] + "\n... (output truncated)"
		charTruncated = true
	}

	display := fmt.Sprintf("Found %d %s", totalMatches, matchTerm)
	if truncated || charTruncated {
		display += " (truncated)"
	}

	return dtool.ToolResult{Output: result}
}

func (t *GrepSearchTool) fallbackGrep(ctx context.Context, query, path string, isRegex bool, maxResults int) (dtool.ToolResult, error) {
	grepArgs := []string{"-rnIH", "--color=never"}
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

	// Parse and format fallback output (same file:line:content format)
	matches := parseGrepOutput(string(output))
	if len(matches) == 0 {
		return dtool.ToolResult{Output: "No matches found for: " + query}, nil
	}
	return t.formatMatches(matches, query, path, maxResults), nil
}
