package tool

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestForgeE2E(t *testing.T) {
	sandboxRoot := "/tmp/forge_e2e_test"
	os.MkdirAll(sandboxRoot, 0755)
	defer os.RemoveAll(sandboxRoot)

	forge := NewForgeTool(sandboxRoot)
	ctx := context.Background()

	// Step 1: Setup
	result, err := forge.Execute(ctx, map[string]any{
		"action": "setup",
		"files": map[string]any{
			"hello.sh": "#!/bin/bash\necho hello world",
			"data.txt": "test content 123",
		},
		"commands": []any{"chmod +x hello.sh"},
	})
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}
	t.Log("Setup:", result.Output)

	var setup struct {
		ForgeID     string `json:"forge_id"`
		SandboxPath string `json:"sandbox_path"`
	}
	if err := json.Unmarshal([]byte(result.Output), &setup); err != nil {
		t.Fatalf("parse setup: %v", err)
	}
	if setup.ForgeID == "" {
		t.Fatal("empty forge_id")
	}

	// Step 2: Assert (with expected pass and fail)
	result, err = forge.Execute(ctx, map[string]any{
		"action":      "assert",
		"forge_id":    setup.ForgeID,
		"file_exists": []any{"hello.sh", "data.txt", "missing.txt"},
		"file_contains": map[string]any{
			"data.txt": "test content",
		},
		"shell_check": []any{"./hello.sh"},
	})
	if err != nil {
		t.Fatalf("assert error: %v", err)
	}
	t.Log("Assert:", result.Output)

	// Verify assert reports: 4 passed, 1 failed
	var assertResult struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	}
	if err := json.Unmarshal([]byte(result.Output), &assertResult); err != nil {
		t.Fatalf("parse assert: %v", err)
	}
	if assertResult.Total != 5 {
		t.Errorf("expected 5 total, got %d", assertResult.Total)
	}
	if assertResult.Passed != 4 {
		t.Errorf("expected 4 passed, got %d", assertResult.Passed)
	}
	if assertResult.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", assertResult.Failed)
	}

	// Step 3: Cleanup
	result, err = forge.Execute(ctx, map[string]any{
		"action":   "cleanup",
		"forge_id": setup.ForgeID,
	})
	if err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if result.Output != "OK" {
		t.Errorf("expected OK, got: %s", result.Output)
	}

	// Verify sandbox removed
	if _, err := os.Stat(setup.SandboxPath); !os.IsNotExist(err) {
		t.Error("sandbox not cleaned up")
	}
}

