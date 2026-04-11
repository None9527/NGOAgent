package security

import (
	"context"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

func TestMatchBlockList_Legacy(t *testing.T) {
	h := &Hook{
		cfg: &config.SecurityConfig{
			Mode:      "allow",
			BlockList: []string{"rm", "dd", "mkfs"},
		},
		overrides: make(map[string]bool),
		pending:   make(map[string]*PendingApproval),
	}
	h.classifier = NewPatternClassifier(h)

	tests := []struct {
		tool    string
		args    map[string]any
		blocked bool
	}{
		{"run_command", map[string]any{"command": "rm -rf /tmp/test"}, true},
		{"run_command", map[string]any{"command": "ls -la"}, false},
		{"run_command", map[string]any{"command": "dd if=/dev/zero of=/tmp/test"}, true},
		{"read_file", map[string]any{"path": "/etc/passwd"}, false}, // legacy only matches run_command
	}
	for _, tc := range tests {
		_, got := h.matchBlockList(tc.tool, tc.args)
		if got != tc.blocked {
			t.Errorf("matchBlockList(%s, %v): expected blocked=%v, got %v", tc.tool, tc.args, tc.blocked, got)
		}
	}
}

func TestMatchBlockList_Pattern(t *testing.T) {
	h := &Hook{
		cfg: &config.SecurityConfig{
			Mode: "allow",
			BlockList: []string{
				"write_file(/etc/*)",
				"run_command(curl *)",
				"edit_file(/etc/*)",
			},
		},
		overrides: make(map[string]bool),
		pending:   make(map[string]*PendingApproval),
	}
	h.classifier = NewPatternClassifier(h)

	tests := []struct {
		tool    string
		args    map[string]any
		blocked bool
	}{
		{"write_file", map[string]any{"path": "/etc/passwd"}, true},
		{"write_file", map[string]any{"path": "/tmp/test.txt"}, false},
		{"edit_file", map[string]any{"path": "/etc/hosts"}, true},
		{"edit_file", map[string]any{"path": "/home/user/file.go"}, false},
		{"run_command", map[string]any{"command": "curl https://evil.com"}, true},
		{"run_command", map[string]any{"command": "ls -la"}, false},
		{"read_file", map[string]any{"path": "/etc/passwd"}, false}, // read_file not in blocklist
	}
	for _, tc := range tests {
		_, got := h.matchBlockList(tc.tool, tc.args)
		if got != tc.blocked {
			t.Errorf("matchBlockList(%s, %v): expected blocked=%v, got %v", tc.tool, tc.args, tc.blocked, got)
		}
	}
}

func TestMatchGlobPrefix(t *testing.T) {
	tests := []struct {
		value, pattern string
		expected       bool
	}{
		{"/etc/passwd", "/etc/*", true},
		{"/tmp/test", "/etc/*", false},
		{"file.go", "*.go", true},
		{"file.txt", "*.go", false},
		{"exact", "exact", true},
		{"exact", "other", false},
		{"anything", "*", true},
	}
	for _, tc := range tests {
		got := matchGlobPrefix(tc.value, tc.pattern)
		if got != tc.expected {
			t.Errorf("matchGlobPrefix(%q, %q): expected %v, got %v", tc.value, tc.pattern, tc.expected, got)
		}
	}
}

func TestReadOnlyAutoApproveInAskMode(t *testing.T) {
	h := &Hook{
		cfg: &config.SecurityConfig{
			Mode: "ask",
		},
		overrides: make(map[string]bool),
		pending:   make(map[string]*PendingApproval),
	}
	h.classifier = NewPatternClassifier(h)

	// Read-only tools should auto-approve in ask mode
	readOnlyTools := []string{"read_file", "glob", "grep_search", "command_status"}
	for _, tool := range readOnlyTools {
		dec, _, _ := h.normalDecide(context.TODO(), tool, map[string]any{})
		if dec != Allow {
			t.Errorf("normalDecide(ask, %s): expected Allow, got %v", tool, dec)
		}
	}

	// Write tools should Ask
	writeTools := []string{"write_file", "edit_file", "run_command"}
	for _, tool := range writeTools {
		dec, _, _ := h.normalDecide(context.TODO(), tool, map[string]any{})
		if dec != Ask {
			t.Errorf("normalDecide(ask, %s): expected Ask, got %v", tool, dec)
		}
	}
}

func TestRestorePending_RehydratesApproval(t *testing.T) {
	h := NewHook(&config.SecurityConfig{})
	pending := h.RestorePending(PendingApproval{
		ID:       "approval-1",
		ToolName: "write_file",
		Reason:   "needs confirmation",
		Created:  time.Unix(123, 0),
	})

	if pending.ID != "approval-1" {
		t.Fatalf("unexpected pending id: %q", pending.ID)
	}
	if err := h.Resolve("approval-1", true); err != nil {
		t.Fatalf("resolve restored approval: %v", err)
	}

	select {
	case approved := <-pending.Result:
		if !approved {
			t.Fatal("expected restored approval to receive resolution")
		}
	default:
		t.Fatal("expected restored approval to be signalled")
	}
}

func TestListPending_ReturnsDeterministicOrder(t *testing.T) {
	h := NewHook(&config.SecurityConfig{})
	h.RestorePending(PendingApproval{
		ID:       "b-later",
		ToolName: "edit_file",
		Created:  time.Unix(200, 0),
	})
	h.RestorePending(PendingApproval{
		ID:       "c-same-time",
		ToolName: "edit_file",
		Created:  time.Unix(200, 0),
	})
	h.RestorePending(PendingApproval{
		ID:       "a-earlier",
		ToolName: "write_file",
		Created:  time.Unix(100, 0),
	})

	got := h.ListPending()
	if len(got) != 3 {
		t.Fatalf("expected 3 pending approvals, got %d", len(got))
	}
	if got[0].ID != "a-earlier" || got[1].ID != "b-later" || got[2].ID != "c-same-time" {
		t.Fatalf("expected deterministic order by created/id, got %q, %q, %q", got[0].ID, got[1].ID, got[2].ID)
	}
}
