package service

import (
	"context"
	"testing"
)

// ──────────────────────────────────────────────
// Mock SecurityChecker for testing
// ──────────────────────────────────────────────

type mockSecurityChecker struct {
	decision SecurityDecision
	reason   string
	ticket   *ApprovalTicket
}

func (m *mockSecurityChecker) BeforeToolCall(_ context.Context, _ string, _ map[string]any) (SecurityDecision, string) {
	return m.decision, m.reason
}
func (m *mockSecurityChecker) AfterToolCall(_ context.Context, _ string, _ string, _ error) {}
func (m *mockSecurityChecker) RequestApproval(_ string, _ map[string]any, _ string) *ApprovalTicket {
	return m.ticket
}
func (m *mockSecurityChecker) RestorePendingApproval(snapshot ApprovalSnapshot) *ApprovalTicket {
	return &ApprovalTicket{ID: snapshot.ID, Result: make(chan bool, 1)}
}
func (m *mockSecurityChecker) ResolvePendingApproval(_ string, _ bool) error { return nil }
func (m *mockSecurityChecker) ListPendingApprovals() []ApprovalSnapshot      { return nil }
func (m *mockSecurityChecker) CleanupPending(_ string)                       {}

// ──────────────────────────────────────────────
// SecurityGate unit tests
// ──────────────────────────────────────────────

func newTestLoop(sec SecurityChecker) *AgentLoop {
	a := &AgentLoop{}
	a.deps.Security = sec
	return a
}

func TestCheckSecurity_ReadOnlyToolSkipsCheck(t *testing.T) {
	// read_file is AccessReadOnly by default convention
	sec := &mockSecurityChecker{decision: SecurityDeny, reason: "should never reach"}
	loop := newTestLoop(sec)

	gate := loop.checkSecurity(context.Background(), "read_file", nil)
	if !gate.Allowed {
		t.Error("read-only tool should always be allowed, even if hook would deny")
	}
}

func TestCheckSecurity_AllowDecision(t *testing.T) {
	sec := &mockSecurityChecker{decision: SecurityAllow}
	loop := newTestLoop(sec)

	gate := loop.checkSecurity(context.Background(), "write_file", nil)
	if !gate.Allowed {
		t.Error("SecurityAllow should allow execution")
	}
	if gate.Err != nil {
		t.Errorf("expected nil error, got %v", gate.Err)
	}
}

func TestCheckSecurity_DenyDecision(t *testing.T) {
	sec := &mockSecurityChecker{decision: SecurityDeny, reason: "dangerous operation"}
	loop := newTestLoop(sec)

	gate := loop.checkSecurity(context.Background(), "run_command", map[string]any{"cmd": "rm -rf /"})
	if gate.Allowed {
		t.Error("SecurityDeny should block execution")
	}
	if gate.Err == nil {
		t.Error("SecurityDeny should return ErrApprovalDenied")
	}
	if gate.Output == "" {
		t.Error("SecurityDeny should provide output message")
	}
}

func TestCheckSecurity_AskAutoApprove(t *testing.T) {
	sec := &mockSecurityChecker{decision: SecurityAsk, reason: "needs confirmation"}
	loop := newTestLoop(sec)
	loop.SetMode("agentic") // Has AutoApprove = true

	gate := loop.checkSecurity(context.Background(), "write_file", nil)
	if !gate.Allowed {
		t.Error("SecurityAsk with AutoApprove should auto-allow")
	}
}
