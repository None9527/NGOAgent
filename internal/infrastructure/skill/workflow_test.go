package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

func TestLoadWorkflow(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: test-workflow
description: A test workflow
trigger: /test
steps:
  - id: search
    tool: web_search
    args:
      query: "test query"
    required: true
    retry: 2
  - id: analyze
    mode: llm
    prompt: "Analyze: {{search}}"
    required: true
  - id: cleanup
    mode: command
    command: "echo done"
    required: false
`
	if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	def, err := LoadWorkflow(dir)
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}

	if def.Name != "test-workflow" {
		t.Errorf("name = %q, want test-workflow", def.Name)
	}
	if len(def.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(def.Steps))
	}
	if def.Steps[0].Tool != "web_search" {
		t.Errorf("step[0].tool = %q", def.Steps[0].Tool)
	}
	if !def.Steps[0].Required {
		t.Error("step[0] should be required")
	}
	if def.Steps[0].Retry != 2 {
		t.Errorf("step[0].retry = %d, want 2", def.Steps[0].Retry)
	}
}

func TestHasWorkflow(t *testing.T) {
	dir := t.TempDir()
	if HasWorkflow(dir) {
		t.Error("empty dir should not have workflow")
	}
	os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte("name: x"), 0644)
	if !HasWorkflow(dir) {
		t.Error("dir with workflow.yaml should have workflow")
	}
}

func TestResolveTemplate(t *testing.T) {
	vars := map[string]string{
		"name":  "hello",
		"count": "42",
	}
	tests := []struct {
		in, want string
	}{
		{"no vars", "no vars"},
		{"{{name}} world", "hello world"},
		{"count={{count}}", "count=42"},
		{"{{unknown}} stays", "{{unknown}} stays"},
		{"{{name}} and {{count}}", "hello and 42"},
	}
	for _, tt := range tests {
		got := resolveTemplate(tt.in, vars)
		if got != tt.want {
			t.Errorf("resolveTemplate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWorkflowRunnerSuccess(t *testing.T) {
	toolExec := func(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error) {
		return dtool.ToolResult{Output: "tool-output-" + name}, nil
	}
	llmCall := func(ctx context.Context, sys, user string) (string, error) {
		return "llm-analyzed: " + user[:20], nil
	}

	runner := NewWorkflowRunner(toolExec, llmCall)
	def := &WorkflowDef{
		Name: "test",
		Steps: []WorkflowStep{
			{ID: "s1", Mode: "tool", Tool: "web_search", Args: map[string]any{"q": "test"}, Required: true, SaveAs: "s1"},
			{ID: "s2", Mode: "llm", Prompt: "Analyze {{s1}}", Required: true, SaveAs: "s2"},
		},
	}

	result := runner.Run(context.Background(), def, map[string]string{})
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed. Reason: %s", result.Status, result.Reason)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(result.Steps))
	}
	if !result.Steps[0].Success || !result.Steps[1].Success {
		t.Error("all steps should succeed")
	}
}

func TestWorkflowRunnerRequiredFail(t *testing.T) {
	toolExec := func(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error) {
		return dtool.ToolResult{Output: "Error: not found"}, nil
	}
	llmCall := func(ctx context.Context, sys, user string) (string, error) {
		return "should-not-reach", nil
	}

	runner := NewWorkflowRunner(toolExec, llmCall)
	def := &WorkflowDef{
		Name: "test-fail",
		Steps: []WorkflowStep{
			{ID: "search", Mode: "tool", Tool: "web_search", Args: map[string]any{"q": "x"}, Required: true, SaveAs: "search"},
			{ID: "process", Mode: "llm", Prompt: "do {{search}}", Required: true, SaveAs: "process"},
		},
	}

	result := runner.Run(context.Background(), def, map[string]string{})
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.FailAt != "search" {
		t.Errorf("failAt = %q, want search", result.FailAt)
	}
	// Second step should not have executed
	if len(result.Steps) != 1 {
		t.Errorf("steps executed = %d, want 1 (should abort before step 2)", len(result.Steps))
	}
}

func TestWorkflowRunnerOptionalSkip(t *testing.T) {
	callCount := 0
	toolExec := func(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error) {
		callCount++
		if callCount == 1 {
			return dtool.ToolResult{Output: "Error: optional fail"}, nil
		}
		return dtool.ToolResult{Output: "final-ok"}, nil
	}
	llmCall := func(ctx context.Context, sys, user string) (string, error) {
		return "", nil
	}

	runner := NewWorkflowRunner(toolExec, llmCall)
	def := &WorkflowDef{
		Name: "test-optional",
		Steps: []WorkflowStep{
			{ID: "opt", Mode: "tool", Tool: "t1", Required: false, SaveAs: "opt"},
			{ID: "final", Mode: "tool", Tool: "t2", Required: true, SaveAs: "final"},
		},
	}

	result := runner.Run(context.Background(), def, map[string]string{})
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (optional step should not abort)", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Success {
		t.Error("step[0] should have failed (optional)")
	}
	if !result.Steps[1].Success {
		t.Error("step[1] should have succeeded")
	}
}

func TestWorkflowRunnerRetry(t *testing.T) {
	attempts := 0
	toolExec := func(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error) {
		attempts++
		if attempts < 3 {
			return dtool.ToolResult{}, fmt.Errorf("transient error")
		}
		return dtool.ToolResult{Output: "success on attempt 3"}, nil
	}
	llmCall := func(ctx context.Context, sys, user string) (string, error) {
		return "", nil
	}

	runner := NewWorkflowRunner(toolExec, llmCall)
	def := &WorkflowDef{
		Name: "test-retry",
		Steps: []WorkflowStep{
			{ID: "flaky", Mode: "tool", Tool: "flaky_tool", Retry: 3, Required: true, SaveAs: "flaky"},
		},
	}

	result := runner.Run(context.Background(), def, map[string]string{})
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed after retries", result.Status)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestFormatResult(t *testing.T) {
	wr := WorkflowResult{
		Status: "failed",
		FailAt: "download",
		Reason: "必需步骤 [download] (3/4) 失败: timeout",
		Steps: []StepResult{
			{StepID: "search", Success: true},
			{StepID: "extract", Success: true},
			{StepID: "download", Success: false, Error: "timeout"},
		},
	}
	formatted := wr.FormatResult()
	if !contains(formatted, "❌ Workflow 失败") {
		t.Error("should contain failure marker")
	}
	if !contains(formatted, "download") {
		t.Error("should mention failed step")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSub(s, sub))
}

func containsSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
