package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// mockDeltaSink captures events for assertions.
type mockDeltaSink struct {
	errors    []error
	texts     []string
	approvals []ApprovalSnapshot
	reviews   []struct {
		message string
		paths   []string
	}
	autoWake  int
	completed int
}

func (m *mockDeltaSink) OnText(text string)                                                { m.texts = append(m.texts, text) }
func (m *mockDeltaSink) OnReasoning(text string)                                           {}
func (m *mockDeltaSink) OnToolStart(callID string, name string, args map[string]any)       {}
func (m *mockDeltaSink) OnToolResult(callID string, name string, output string, err error) {}
func (m *mockDeltaSink) OnProgress(taskName, status, summary, mode string)                 {}
func (m *mockDeltaSink) OnPlanReview(message string, paths []string) {
	m.reviews = append(m.reviews, struct {
		message string
		paths   []string
	}{message: message, paths: append([]string(nil), paths...)})
}
func (m *mockDeltaSink) OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string) {
	m.approvals = append(m.approvals, ApprovalSnapshot{
		ID:       approvalID,
		ToolName: toolName,
		Args:     cloneMap(args),
		Reason:   reason,
	})
}
func (m *mockDeltaSink) OnTitleUpdate(sessionID, title string) {}
func (m *mockDeltaSink) OnAutoWakeStart()                      { m.autoWake++ }
func (m *mockDeltaSink) OnComplete()                           { m.completed++ }
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

	result, _ := loop.handleGenerateError(context.Background(), rs, errors.New("network fail"))

	if result.ObservedState != StateError.String() {
		t.Errorf("expected observed state %q, got %q", StateError.String(), result.ObservedState)
	}
	if result.Status != graphruntime.NodeStatusFatal {
		t.Errorf("expected fatal status, got %#v", result)
	}
	if len(delta.errors) != 1 {
		t.Errorf("expected 1 error emitted, got %d", len(delta.errors))
	}
}

func TestHandleGenerateError_Fatal(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{}

	err := &llm.LLMError{Level: llm.ErrorFatal, Message: "model not found"}
	result, _ := loop.handleGenerateError(context.Background(), rs, err)

	if result.ObservedState != StateFatal.String() {
		t.Errorf("expected observed state %q, got %q", StateFatal.String(), result.ObservedState)
	}
	if result.Status != graphruntime.NodeStatusFatal {
		t.Errorf("expected fatal status, got %#v", result)
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
	rs := &runState{}

	// Fast context to avoid long sleep
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := &llm.LLMError{Level: llm.ErrorTransient, Message: "502 Gateway"}
	result, errReturn := loop.handleGenerateError(ctx, rs, err)

	if rs.retryCount() != 1 {
		t.Errorf("expected retries to increment to 1, got %d", rs.retryCount())
	}
	if result.ObservedState != StateError.String() {
		t.Errorf("expected observed state %q, got %q", StateError.String(), result.ObservedState)
	}
	if errReturn != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded error, got %v", errReturn)
	}
	if result.Status != graphruntime.NodeStatusFatal {
		t.Errorf("expected fatal status on context cancellation, got %#v", result)
	}
	if len(delta.texts) != 1 {
		t.Errorf("expected text notification for retry warning")
	}
}

func TestHandleGenerateError_TransientFailover(t *testing.T) {
	loop, delta := setupTestLoop()
	rs := &runState{}
	rs.setRetryCount(5) // Exceeds default maxR for transient
	rs.setLastProvider("default_prov")

	err := &llm.LLMError{Level: llm.ErrorTransient, Message: "502 Gateway"}
	result, _ := loop.handleGenerateError(context.Background(), rs, err)

	if got := rs.excludedProviderList(); len(got) != 1 || got[0] != "default_prov" {
		t.Errorf("expected default_prov to be excluded for failover, got %v", got)
	}
	if rs.retryCount() != 0 {
		t.Errorf("expected retries to reset on failover, got %d", rs.retryCount())
	}
	if result.ObservedState != StateGenerate.String() {
		t.Errorf("expected observed state %q, got %q", StateGenerate.String(), result.ObservedState)
	}
	if result.RouteKey != graphRouteGenerate {
		t.Errorf("expected generate route, got %#v", result)
	}
	if len(delta.texts) != 0 {
		t.Errorf("failover doesn't immediately send text in this path")
	}
}

func TestHandleGenerateError_ContextOverflow(t *testing.T) {
	loop, _ := setupTestLoop()
	rs := &runState{opts: RunOptions{MaxTokens: 2000}}

	err := &llm.LLMError{Level: llm.ErrorContextOverflow, Message: "context length exceeded"}
	result, _ := loop.handleGenerateError(context.Background(), rs, err)

	if rs.retryCount() != 1 {
		t.Errorf("expected retries=1, got %d", rs.retryCount())
	}
	if rs.maxTokens() != 1000 {
		t.Errorf("expected MaxTokens to halve to 1000, got %d", rs.maxTokens())
	}
	if result.ObservedState != StateCompact.String() {
		t.Errorf("expected observed state %q, got %q", StateCompact.String(), result.ObservedState)
	}
	if result.RouteKey != graphRouteCompact {
		t.Errorf("expected compact route, got %#v", result)
	}
}

func TestHandleToolExec_DeniedToolTransitionsToDoneObservedState(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Delta: &mockDeltaSink{},
		Security: &mockSecurityChecker{
			decision: SecurityDeny,
			reason:   "needs approval",
		},
	})
	loop.history = []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "write_file",
				Arguments: `{"path":"a.go","content":"x"}`,
			},
		}},
	}}

	result, err := loop.handleToolExec(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleToolExec error: %v", err)
	}
	if result.RouteKey != graphRouteDone {
		t.Fatalf("expected done route, got %#v", result)
	}
	if result.ObservedState != StateDone.String() {
		t.Fatalf("expected observed state %q, got %q", StateDone.String(), result.ObservedState)
	}
}

type spawnYieldToolExec struct{}

func (spawnYieldToolExec) Execute(_ context.Context, _ string, _ map[string]any) (dtool.ToolResult, error) {
	return dtool.SpawnYieldResult("[spawned]")
}

func (spawnYieldToolExec) ListDefinitions() []llm.ToolDef { return nil }
func (spawnYieldToolExec) Generation() int64              { return 0 }

func TestHandleToolExec_SpawnYieldTransitionsToSpawn(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Delta:    &mockDeltaSink{},
		ToolExec: spawnYieldToolExec{},
		Security: &mockSecurityChecker{decision: SecurityAllow},
	})
	loop.history = []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "spawn_agent",
				Arguments: `{"task":"research"}`,
			},
		}},
	}}
	loop.SetActiveBarrier(NewSubagentBarrierFromState(loop, nil, graphruntime.BarrierState{
		ID:           "barrier-spawn",
		TotalCount:   1,
		PendingCount: 1,
	}))

	result, err := loop.handleToolExec(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleToolExec error: %v", err)
	}
	if result.RouteKey != graphRouteSpawn {
		t.Fatalf("expected spawn route, got %#v", result)
	}
	if result.ObservedState != "spawn" {
		t.Fatalf("expected observed state spawn, got %q", result.ObservedState)
	}
}

func TestPrepareTurn_ClearsStaleBoundaryState(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.task.Name = "stale task"
	loop.task.Mode = "planning"
	loop.task.PreviousMode = "execution"
	loop.task.Status = "running"
	loop.task.Summary = "stale summary"
	loop.task.StepsSinceUpdate = 7
	loop.task.YieldRequested = true

	loop.prepareTurn("next turn")

	if loop.task.Name != "" {
		t.Fatalf("expected task name cleared, got %q", loop.task.Name)
	}
	if loop.task.Mode != "" {
		t.Fatalf("expected task mode cleared, got %q", loop.task.Mode)
	}
	if loop.task.PreviousMode != "" {
		t.Fatalf("expected previous mode cleared, got %q", loop.task.PreviousMode)
	}
	if loop.task.Status != "" {
		t.Fatalf("expected task status cleared, got %q", loop.task.Status)
	}
	if loop.task.Summary != "" {
		t.Fatalf("expected task summary cleared, got %q", loop.task.Summary)
	}
	if loop.task.StepsSinceUpdate != 0 {
		t.Fatalf("expected steps since update reset, got %d", loop.task.StepsSinceUpdate)
	}
	if loop.task.YieldRequested {
		t.Fatal("expected yield request cleared")
	}
}
