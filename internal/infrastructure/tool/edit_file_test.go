package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ═══════════════════════════════════════════
// Item 9: LAZY_COMMENT detection
// ═══════════════════════════════════════════

func TestEditFileLazyCommentDetection(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.go")
	// Realistic multi-line function — lazy edits try to replace this with much shorter stubs
	longContent := "func main() {\n\tfmt.Println(\"hello\")\n\tfmt.Println(\"world\")\n\tif err := doSomething(); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
	os.WriteFile(filePath, []byte(longContent), 0644)

	tool := &EditFileTool{}
	// Mark file as read
	globalFileState.MarkRead(filePath, []byte(longContent))

	oldStr := "func main() {\n\tfmt.Println(\"hello\")\n\tfmt.Println(\"world\")\n\tif err := doSomething(); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}"

	patterns := []struct {
		name   string
		newStr string
	}{
		{"rest", "func main() {\n// ... rest\n}"},
		{"remaining", "func main() {\n// ...remaining code\n}"},
		{"existing", "func main() {\n// existing code\n}"},
		{"unchanged", "func main() {\n// ... unchanged\n}"},
		{"keep", "func main() {\n// ... keep existing\n}"},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), map[string]any{
				"path":       filePath,
				"old_string": oldStr,
				"new_string": p.newStr,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.Output, "Error [code 10]") {
				t.Fatalf("expected code 10 error for pattern %q, got: %s", p.name, result.Output)
			}
		})
	}

	t.Log("✅ EditFile: LAZY_COMMENT detection blocks 5 placeholder patterns")
}

func TestEditFileValidEditStillWorks(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.go")
	content := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	os.WriteFile(filePath, []byte(content), 0644)

	tool := &EditFileTool{}
	globalFileState.MarkRead(filePath, []byte(content))

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":       filePath,
		"old_string": "fmt.Println(\"hello\")",
		"new_string": "fmt.Println(\"world\")",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "Successfully edited") {
		t.Fatalf("expected success, got: %s", result.Output)
	}

	// Verify content
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "\"world\"") {
		t.Fatalf("file not updated: %s", string(data))
	}

	t.Log("✅ EditFile: valid edits (no placeholder) still work correctly")
}

// ═══════════════════════════════════════════
// Item 4: Security Ask → WaitForApproval
// Tested via security_hook_test.go
// ═══════════════════════════════════════════

// ═══════════════════════════════════════════
// Item 8: Prompt CRITICAL enforcement
// ═══════════════════════════════════════════

func TestPromptCriticalExists(t *testing.T) {
	// We verify the Guidelines constant contains the CRITICAL section
	// Import the prompttext package doesn't compile into tool package,
	// so this is tested in the evolution_test.go integration test.
	t.Log("✅ Prompt CRITICAL: tested in evolution_test.go")
}
