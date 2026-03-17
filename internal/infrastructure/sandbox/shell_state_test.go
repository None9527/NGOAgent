package sandbox

import (
	"testing"
)

func TestEnvDiffCapture(t *testing.T) {
	s := NewShellState("/tmp")

	// Simulate command output with cwd + env markers
	output := `hello world
__NGOAGENT_CWD_MARKER__
/home/user/project
__NGOAGENT_ENV_MARKER__
HOME=/home/user
PATH=/usr/bin:/usr/local/bin
MY_NEW_VAR=my_value
ANOTHER_VAR=another_value
__NGOAGENT_ENV_END__`

	clean, cwd := s.ExtractStateFromOutput(output)

	if clean != "hello world" {
		t.Errorf("clean output = %q, want %q", clean, "hello world")
	}
	if cwd != "/home/user/project" {
		t.Errorf("cwd = %q, want %q", cwd, "/home/user/project")
	}

	// Check that new vars were captured
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.userEnv["MY_NEW_VAR"]; !ok || val != "my_value" {
		t.Errorf("MY_NEW_VAR = %q, want %q", val, "my_value")
	}
	if val, ok := s.userEnv["ANOTHER_VAR"]; !ok || val != "another_value" {
		t.Errorf("ANOTHER_VAR = %q, want %q", val, "another_value")
	}
}

func TestEnvBlacklist(t *testing.T) {
	s := NewShellState("/tmp")

	output := `ok
__NGOAGENT_CWD_MARKER__
/tmp
__NGOAGENT_ENV_MARKER__
_=/usr/bin/env
SHLVL=2
BASH_FUNC_nvm%%=() { echo hi; }
REAL_VAR=should_capture
__NGOAGENT_ENV_END__`

	s.ExtractStateFromOutput(output)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.userEnv["_"]; ok {
		t.Error("_ should be blacklisted")
	}
	if _, ok := s.userEnv["SHLVL"]; ok {
		t.Error("SHLVL should be blacklisted")
	}
	if val, ok := s.userEnv["REAL_VAR"]; !ok || val != "should_capture" {
		t.Errorf("REAL_VAR = %q, want %q", val, "should_capture")
	}
}

func TestParseEnvOutput(t *testing.T) {
	input := `FOO=bar
MULTI_LINE=line1
line2
line3
SINGLE=value`

	result := parseEnvOutput(input)

	if result["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", result["FOO"], "bar")
	}
	if result["MULTI_LINE"] != "line1\nline2\nline3" {
		t.Errorf("MULTI_LINE = %q, want %q", result["MULTI_LINE"], "line1\nline2\nline3")
	}
	if result["SINGLE"] != "value" {
		t.Errorf("SINGLE = %q, want %q", result["SINGLE"], "value")
	}
}

func TestBuildEnvIncludesUserVars(t *testing.T) {
	s := NewShellState("/tmp")
	s.SetEnv("MY_API_KEY", "secret123")

	env := s.BuildEnv()
	found := false
	for _, e := range env {
		if e == "MY_API_KEY=secret123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("BuildEnv should include MY_API_KEY=secret123")
	}
}

func TestWrapCommandFormat(t *testing.T) {
	s := NewShellState("/tmp")
	wrapped := s.WrapCommand("echo hello")

	if !containsAll(wrapped, "echo hello", cwdMarker, envMarker, envEnd, "pwd", "env") {
		t.Errorf("wrapped command missing expected markers: %s", wrapped)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
