package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// WriteFileTool writes content to a file, creating parent directories as needed.
type WriteFileTool struct {
	FileHistory *workspace.FileHistory // If set, backs up files before write
}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string { return prompttext.ToolWriteFile }

func (t *WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string", "description": "Absolute file path"},
			"content":   map[string]any{"type": "string", "description": "File content to write"},
			"overwrite": map[string]any{"type": "boolean", "description": "Overwrite if exists (default: false)"},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	overwrite, _ := args["overwrite"].(bool)

	if path == "" {
		return dtool.ToolResult{Output: "Error: 'path' is required"}, nil
	}
	path = filepath.Clean(path)

	// Check if file exists
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return dtool.ToolResult{Output: "Error: file already exists. Set overwrite=true to replace."}, nil
		}
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating directory: %v", err)}, nil
	}

	// FileHistory: backup before writing
	if t.FileHistory != nil {
		t.FileHistory.TrackEdit(path)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
	}

	// Sync FileState so subsequent edit_file calls don't get E6/E7 errors
	globalFileState.MarkRead(path, []byte(content))

	return dtool.ToolResult{Output: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)}, nil
}
