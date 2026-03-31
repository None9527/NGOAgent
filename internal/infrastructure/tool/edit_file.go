package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// EditFileTool performs string replacement editing on files.
type EditFileTool struct {
	WorkDir     string                  // If set, enforces path within workspace (E5)
	FileHistory *workspace.FileHistory  // If set, backs up files before edit
}

func (t *EditFileTool) Name() string        { return "edit_file" }
func (t *EditFileTool) Description() string { return prompttext.ToolEditFile }

func (t *EditFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Absolute file path"},
			"old_string":  map[string]any{"type": "string", "description": "Exact string to find and replace"},
			"new_string":  map[string]any{"type": "string", "description": "Replacement string"},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default: false)"},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

// package-level FileState for tracking read/write status
var globalFileState = NewFileState()

func (t *EditFileTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)
	fuzzyUsed := false

	if path == "" {
		return dtool.ToolResult{Output: "Error: 'path' is required"}, nil
	}
	path = filepath.Clean(path)

	// Error code 1: No change needed
	if oldStr == newStr {
		return dtool.ToolResult{Output: "Error [code 1]: old_string and new_string are identical. No changes made."}, nil
	}

	// Error code 2: Empty old_string for existing non-empty file
	if oldStr == "" {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return dtool.ToolResult{Output: "Error [code 2]: old_string is empty but file exists and is not empty. Use write_file to overwrite entirely, or provide old_string to edit."}, nil
		}
	}

	// Error code 5: Path outside workspace
	if t.WorkDir != "" && !strings.HasPrefix(path, t.WorkDir) {
		if !strings.HasPrefix(path, "/tmp") {
			return dtool.ToolResult{Output: fmt.Sprintf("Error [code 5]: path %s is outside the workspace %s", path, t.WorkDir)}, nil
		}
	}

	// Error code 6: File not read before edit
	if oldStr != "" {
		if !globalFileState.WasRead(path) {
			return dtool.ToolResult{Output: "Error [code 6]: you must read the file before editing it — use read_file first to view the current content."}, nil
		}
	}

	// Error code 7: File changed since last read
	if oldStr != "" {
		checkData, _ := os.ReadFile(path)
		if globalFileState.HasChanged(path, checkData) {
			return dtool.ToolResult{Output: "Error [code 7]: file has been modified since you last read it — use read_file again to get the latest content."}, nil
		}
	}

	// Create new file if old_string is empty
	if oldStr == "" {
		if _, err := os.Stat(path); err == nil {
			data, _ := os.ReadFile(path)
			if strings.TrimSpace(string(data)) != "" {
				// Error code 3: File exists and is not empty
				return dtool.ToolResult{Output: "Error [code 3]: file already exists and is not empty. Cannot create."}, nil
			}
		}
		// Create new file
		dir := filepath.Dir(path)
		os.MkdirAll(dir, 0755)
		// FileHistory: track before creation (records as "new file")
		if t.FileHistory != nil {
			t.FileHistory.TrackEdit(path)
		}
		if err := os.WriteFile(path, []byte(newStr), 0644); err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
		}
		globalFileState.MarkModified(path, []byte(newStr))
		return dtool.ToolResult{Output: fmt.Sprintf("Created new file: %s", path)}, nil
	}

	// Read existing file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Error code 4: File does not exist
			return dtool.ToolResult{Output: fmt.Sprintf("Error [code 4]: file does not exist: %s", path)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading file: %v", err)}, nil
	}

	content := string(data)

	// Normalize line endings for matching
	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	normalizedOld := strings.ReplaceAll(oldStr, "\r\n", "\n")

	// Count occurrences
	count := strings.Count(normalizedContent, normalizedOld)

	// LAZY_COMMENT detection (pre-match): check BEFORE fuzzy cascade
	// so placeholder patterns are caught regardless of match method.
	// Only trigger when new_string is suspiciously shorter (< 75% of old_string length).
	if len(newStr)*4 < len(oldStr)*3 {
		lazyPatterns := []string{
			"// ... rest", "// ...remaining", "/* ... */",
			"// existing code", "// ... 其余代码", "// ... same as before",
			"// ... unchanged", "// ... keep existing",
			"// ... rest of", "// rest of the code", "// ...rest",
			"// todo:", "// fixme:", "// placeholder",
			"// ... other", "// ... 其他", "// ... 省略",
			"// ... 以下省略", "// 其余代码不变",
		}
		lowerNew := strings.ToLower(newStr)
		for _, pat := range lazyPatterns {
			if strings.Contains(lowerNew, pat) {
				return dtool.ToolResult{Output: fmt.Sprintf(
					"Error [code 10]: new_string contains lazy placeholder '%s'. "+
						"You must provide the COMPLETE replacement content, not placeholders.", pat)}, nil
			}
		}
	}

	if count == 0 {
		// Cascade fuzzy matching: L1 unicode → L2 line-trim → L3 block-anchor
		if fuzzyMatch := cascadeFuzzyMatch(normalizedContent, normalizedOld); fuzzyMatch != "" {
			// Use the matched slice for replacement instead of the original oldStr
			normalizedOld = fuzzyMatch
			count = 1
			// Track fuzzy match for output notice
			fuzzyUsed = true
		} else {
			// Error code 8: String not found — include best candidate feedback
			errMsg := fmt.Sprintf("Error [code 8]: old_string not found in file.\nString: %s", oldStr)
			if hint := findSimilarLines(normalizedContent, normalizedOld, 0.6); hint != "" {
				errMsg += fmt.Sprintf("\n\nDid you mean to match this section?\n```\n%s\n```\nPlease re-read the file and try again with the correct old_string.", hint)
			}
			return dtool.ToolResult{Output: errMsg}, nil
		}
	}

	if count > 1 && !replaceAll {
		// Error code 9: Multiple matches
		return dtool.ToolResult{Output: fmt.Sprintf("Error [code 9]: found %d occurrences, but replace_all is false. Set replace_all=true or provide more context to uniquely identify the instance.", count)}, nil
	}

	// Perform replacement

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(normalizedContent, normalizedOld, newStr)
	} else {
		newContent = strings.Replace(normalizedContent, normalizedOld, newStr, 1)
	}

	// FileHistory: backup before overwriting
	if t.FileHistory != nil {
		t.FileHistory.TrackEdit(path)
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error writing file: %v", err)}, nil
	}
	globalFileState.MarkModified(path, []byte(newContent))

	if replaceAll {
		msg := fmt.Sprintf("Successfully replaced %d occurrences in %s", count, path)
		if fuzzyUsed {
			msg += " (fuzzy matched — actual content differed in whitespace/unicode from old_string)"
		}
		return dtool.ToolResult{Output: msg}, nil
	}
	msg := fmt.Sprintf("Successfully edited %s", path)
	if fuzzyUsed {
		msg += " (fuzzy matched — actual content differed in whitespace/unicode from old_string)"
	}
	return dtool.ToolResult{Output: msg}, nil
}
