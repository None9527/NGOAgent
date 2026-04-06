package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// mockDeltaSink captures events for assertions.
type mockDeltaSink struct {
	errors []error
	texts  []string
}

func (m *mockDeltaSink) OnText(text string)                                                { m.texts = append(m.texts, text) }
func (m *mockDeltaSink) OnReasoning(text string)                                           {}
func (m *mockDeltaSink) OnToolStart(callID string, name string, args map[string]any)       {}
func (m *mockDeltaSink) OnToolResult(callID string, name string, output string, err error) {}
func (m *mockDeltaSink) OnProgress(taskName, status, summary, mode string)                 {}
func (m *mockDeltaSink) OnPlanReview(message string, paths []string)                       {}
func (m *mockDeltaSink) OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string) {
}
func (m *mockDeltaSink) OnTitleUpdate(sessionID, title string) {}
func (m *mockDeltaSink) OnAutoWakeStart()                      {}
func (m *mockDeltaSink) OnComplete()                           {}
func (m *mockDeltaSink) OnError(err error)                     { m.errors = append(m.errors, err) }
func (m *mockDeltaSink) Emit(event DeltaEvent)                 {}

func setupTestLoop() (*AgentLoop, *mockDeltaSink) {
	delta := &mockDeltaSink{}
	loop := &AgentLoop{}
	loop.deps.Delta = delta
	return loop, delta
}

func TestHandleGenerateError_NonLLMError(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{}

	action, _ := loop.handleGenerateError(context.Background(), rs, errors.New("network fail"))

	if loop.state != StateError {
		t.Errorf("expected StateError, got %v", loop.state)
	}
	if action != actionContinue {
		t.Errorf("expected actionContinue, got %v", action)
	}
	if len(delta.errors) != 1 {
		t.Errorf("expected 1 error emitted, got %d", len(delta.errors))
	}
}

func TestHandleGenerateError_Fatal(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{}

	err := &llm.LLMError{Level: llm.ErrorFatal, Message: "model not found"}
	action, _ := loop.handleGenerateError(context.Background(), rs, err)

	if loop.state != StateFatal {
		t.Errorf("expected StateFatal, got %v", loop.state)
	}
	if action != actionContinue {
		t.Errorf("expected actionContinue, got %v", action)
	}
	if len(delta.errors) != 1 {
		t.Errorf("expected 1 error emitted, got %d", len(delta.errors))
	}
	if len(delta.texts) != 1 {
		t.Errorf("expected text notification for fatal error")
	}
}

func TestHandleGenerateError_TransientRetry(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{retries: 0}

	// Fast context to avoid long sleep
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := &llm.LLMError{Level: llm.ErrorTransient, Message: "502 Gateway"}
	action, errReturn := loop.handleGenerateError(ctx, rs, err)

	if rs.retries != 1 {
		t.Errorf("expected retries to increment to 1, got %d", rs.retries)
	}
	if loop.state != StateError {
		t.Errorf("expected state to remain StateError due to context cancellation, got %v", loop.state)
	}
	if errReturn != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded error, got %v", errReturn)
	}
	if action != actionContinue {
		t.Errorf("expected actionContinue, got %v", action)
	}
	if len(delta.texts) != 1 {
		t.Errorf("expected text notification for retry warning")
	}
}

func TestHandleGenerateError_TransientFailover(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{retries: 5, lastProvName: "default_prov"} // Exceeds default maxR for transient

	err := &llm.LLMError{Level: llm.ErrorTransient, Message: "502 Gateway"}
	action, _ := loop.handleGenerateError(context.Background(), rs, err)

	if len(rs.excludedProviders) != 1 || rs.excludedProviders[0] != "default_prov" {
		t.Errorf("expected default_prov to be excluded for failover, got %v", rs.excludedProviders)
	}
	if rs.retries != 0 {
		t.Errorf("expected retries to reset on failover, got %d", rs.retries)
	}
	if loop.state != StateGenerate {
		t.Errorf("expected transition to StateGenerate for failover, got %v", loop.state)
	}
	if action != actionContinue {
		t.Errorf("expected actionContinue, got %v", action)
	}
	if len(delta.texts) != 0 {
		t.Errorf("failover doesn't immediately send text in this path")
	}
}

func TestHandleGenerateError_ContextOverflow(t *testing.T) {
	loop, _ := setupTestLoop()
	rs := &runState{retries: 0, opts: RunOptions{MaxTokens: 2000}}

	err := &llm.LLMError{Level: llm.ErrorContextOverflow, Message: "context length exceeded"}
	action, _ := loop.handleGenerateError(context.Background(), rs, err)

	if rs.retries != 1 {
		t.Errorf("expected retries=1, got %d", rs.retries)
	}
	if rs.opts.MaxTokens != 1000 {
		t.Errorf("expected MaxTokens to halve to 1000, got %d", rs.opts.MaxTokens)
	}
	if loop.state != StateCompact {
		t.Errorf("first overflow should transition to StateCompact, got %v", loop.state)
	}
	if action != actionContinue {
		t.Errorf("expected actionContinue, got %v", action)
	}
}
