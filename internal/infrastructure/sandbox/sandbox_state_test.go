package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════
// ShellState unit tests
// ═══════════════════════════════════════════

func TestNewShellState_DefaultCwd(t *testing.T) {
	expected, _ := os.Getwd()
	state := NewShellState("")
	if state.Cwd() != expected {
		t.Errorf("default cwd: got %q, want %q", state.Cwd(), expected)
	}
}

func TestNewShellState_ExplicitCwd(t *testing.T) {
	state := NewShellState("/tmp")
	if state.Cwd() != "/tmp" {
		t.Errorf("explicit cwd: got %q, want /tmp", state.Cwd())
	}
}

func TestShellState_SetCwd(t *testing.T) {
	state := NewShellState("/usr")
	state.SetCwd("/var")
	if state.Cwd() != "/var" {
		t.Errorf("after SetCwd: got %q, want /var", state.Cwd())
	}
}

func TestShellState_WrapCommand(t *testing.T) {
	state := NewShellState("/tmp")
	wrapped := state.WrapCommand("echo hello")
	if !strings.Contains(wrapped, cwdMarker) {
		t.Error("wrapped command should contain cwd marker")
	}
	if !strings.HasPrefix(wrapped, "echo hello") {
		t.Error("wrapped command should start with original command")
	}
}

func TestShellState_ExtractCwdFromOutput(t *testing.T) {
	state := NewShellState("/usr")

	// Simulate output with marker
	fakeOutput := "some output\nmore output\n\n" + cwdMarker + "\n/tmp/new_dir\n"
	clean, cwd := state.ExtractCwdFromOutput(fakeOutput)

	if cwd != "/tmp/new_dir" {
		t.Errorf("extracted cwd: got %q, want /tmp/new_dir", cwd)
	}
	if strings.Contains(clean, cwdMarker) {
		t.Error("clean output should not contain cwd marker")
	}
	if state.Cwd() != "/tmp/new_dir" {
		t.Errorf("state cwd should be updated: got %q", state.Cwd())
	}
}

func TestShellState_ExtractCwdFromOutput_NoMarker(t *testing.T) {
	state := NewShellState("/usr")
	output := "normal output without marker"
	clean, cwd := state.ExtractCwdFromOutput(output)
	if clean != output {
		t.Error("output without marker should be returned unchanged")
	}
	if cwd != "" {
		t.Error("no marker = no cwd")
	}
	if state.Cwd() != "/usr" {
		t.Error("state cwd should be unchanged")
	}
}

func TestShellState_BuildEnv(t *testing.T) {
	state := NewShellState("/tmp")
	env := state.BuildEnv()
	if len(env) == 0 {
		t.Fatal("env should not be empty (snapshot from os.Environ)")
	}

	// Check that PATH is present
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Error("env should contain PATH")
	}
}

func TestShellState_SetEnv_OverlaysSnapshot(t *testing.T) {
	state := NewShellState("/tmp")
	state.SetEnv("NGOCLAW_TEST_VAR", "hello")
	env := state.BuildEnv()

	found := false
	for _, e := range env {
		if e == "NGOCLAW_TEST_VAR=hello" {
			found = true
			break
		}
	}
	if !found {
		t.Error("user env var should appear in BuildEnv output")
	}
}

// ═══════════════════════════════════════════
// Manager integration tests (cwd persistence)
// ═══════════════════════════════════════════

func TestManager_CwdPersistence(t *testing.T) {
	mgr := NewManager("")
	ctx := context.Background()

	// Run a command that changes directory
	result, err := mgr.Run(ctx, "cd /tmp && echo ok", "", 5*time.Second)
	if err != nil {
		t.Fatalf("Run cd: %v", err)
	}
	if !strings.Contains(result.Stdout, "ok") {
		t.Errorf("stdout should contain 'ok', got: %q", result.Stdout)
	}

	// Check that cwd was updated to /tmp
	if mgr.State.Cwd() != "/tmp" {
		t.Errorf("cwd should be /tmp after cd, got: %q", mgr.State.Cwd())
	}

	// Run another command without explicit cwd — should inherit /tmp
	result2, err := mgr.Run(ctx, "pwd", "", 5*time.Second)
	if err != nil {
		t.Fatalf("Run pwd: %v", err)
	}
	if !strings.Contains(result2.Stdout, "/tmp") {
		t.Errorf("second command should run in /tmp, got: %q", result2.Stdout)
	}
}

func TestManager_CwdExplicitOverride(t *testing.T) {
	mgr := NewManager("/usr")
	ctx := context.Background()

	// Explicit cwd should override persisted state
	result, err := mgr.Run(ctx, "pwd", "/var", 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result.Stdout, "/var") {
		t.Errorf("explicit cwd should be /var, got: %q", result.Stdout)
	}
}

func TestManager_EnvInjection(t *testing.T) {
	mgr := NewManager("")
	ctx := context.Background()

	// Set a custom env var
	mgr.State.SetEnv("NGOCLAW_TEST_INJECTION", "injected_value")

	// Run a command that reads the env var
	result, err := mgr.Run(ctx, "echo $NGOCLAW_TEST_INJECTION", "", 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result.Stdout, "injected_value") {
		t.Errorf("env var should be injected, got: %q", result.Stdout)
	}
}

func TestManager_StdinPipe(t *testing.T) {
	mgr := NewManager("")
	ctx := context.Background()

	// Start a background command that reads from stdin
	err := mgr.RunBackground(ctx, "test-stdin", "cat", "")
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}

	// Send input via stdin pipe
	err = mgr.SendInput("test-stdin", "hello from stdin\n")
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Check output
	result, err := mgr.GetStatus("test-stdin", 0)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello from stdin") {
		t.Errorf("stdin should be echoed back by cat, got: %q", result.Stdout)
	}

	// Cleanup
	mgr.Kill("test-stdin")
}
