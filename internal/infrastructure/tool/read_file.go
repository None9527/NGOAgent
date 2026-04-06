package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ReadFileTool reads file contents with optional line range.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return `Read a file (text or image). Absolute path required.
- Default: first 800 lines. Use start_line/end_line for ranges.
- Images (PNG/JPG) are read as multimodal data. Files >10MB rejected.`
}

func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "Absolute file path"},
			"start_line": map[string]any{"type": "integer", "description": "Start line (1-indexed, optional)"},
			"end_line":   map[string]any{"type": "integer", "description": "End line (1-indexed, optional)"},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return dtool.ToolResult{Output: "Error: 'path' is required"}, nil
	}
	path = filepath.Clean(path)

	// P1-D #43: Validate path — resolve symlinks, block sensitive paths (symmetric with write/edit)
	if resolved, err := ValidatePath(path, ""); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: path validation failed: %v", err)}, nil
	} else {
		path = resolved
	}

	// Large file protection: reject files > 10MB to prevent OOM
	info, err := os.Stat(path)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}
	if info.IsDir() {
		return dtool.ToolResult{Output: "Error: path is a directory. Use run_command with ls to list directories."}, nil
	}
	if info.Size() > 10*1024*1024 {
		return dtool.ToolResult{Output: fmt.Sprintf(
			"Error: file is too large (%d MB). Use run_command with head/tail/sed to read specific sections.",
			info.Size()/(1024*1024))}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading file: %v", err)}, nil
	}

	// Binary file detection: check for NUL bytes in first 8KB
	checkLen := len(data)
	if checkLen > 8192 {
		checkLen = 8192
	}
	isBinary := false
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			isBinary = true
			break
		}
	}

	if isBinary {
		info, _ := os.Stat(path)
		size := int64(len(data))
		if info != nil {
			size = info.Size()
		}
		ext := filepath.Ext(path)
		return dtool.ToolResult{Output: fmt.Sprintf("Binary file detected: %s\nSize: %d bytes\nExtension: %s\n\nThis appears to be a binary file. Use appropriate tools for binary content.", path, size, ext)}, nil
	}

	// P2 G1: Check for external modifications before reading
	extModified, extWarning := globalFileWatcher.CheckRead(path)

	// Track read for FileState (edit_file E6 checks this)
	globalFileState.MarkRead(path, data)

	lines := strings.Split(string(data), "\n")
	startLine := 1
	endLine := len(lines)

	if v, ok := args["start_line"].(float64); ok && v > 0 {
		startLine = int(v)
	}
	if v, ok := args["end_line"].(float64); ok && v > 0 {
		endLine = int(v)
	}

	// Default: first 800 lines (design says 800, not 2000)
	if _, hasStart := args["start_line"]; !hasStart {
		if endLine > 800 {
			endLine = 800
		}
	}

	// Clamp
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		startLine = endLine
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("File: %s (%d lines total)\n\n", path, len(lines)))
	for i := startLine - 1; i < endLine; i++ {
		line := lines[i]
		// Truncate long lines
		if len(line) > 2000 {
			line = line[:2000] + "..."
		}
		b.WriteString(fmt.Sprintf("%6d\t%s\n", i+1, line))
	}

	if endLine < len(lines) {
		b.WriteString(fmt.Sprintf(
			"\n[TRUNCATED: showing lines %d-%d of %d total. To read next chunk: read_file(path='%s', start_line=%d)]\n",
			startLine, endLine, len(lines), path, endLine+1,
		))
	}

	output := b.String()

	// P2 G1: Prepend external modification warning if detected
	if extModified {
		output = extWarning + "\n\n" + output
	}
	return dtool.ToolResult{Output: output}, nil
}
