package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

type stubTriggerRegistry struct {
	dispatched atomic.Int32
}

func (s *stubTriggerRegistry) Register(kind graphruntime.TriggerKind, handler graphruntime.TriggerHandler) string {
	return "stub-id"
}
func (s *stubTriggerRegistry) Subscribe(sub graphruntime.TriggerSubscription) string {
	return sub.ID
}
func (s *stubTriggerRegistry) Unsubscribe(id string) {}
func (s *stubTriggerRegistry) Dispatch(_ context.Context, event graphruntime.TriggerEvent) error {
	s.dispatched.Add(1)
	return nil
}
func (s *stubTriggerRegistry) ListRegistered() []graphruntime.TriggerKind { return nil }
func (s *stubTriggerRegistry) Close()                                      {}

func TestHandleAgentCard(t *testing.T) {
	card := AgentCard{Name: "test-agent", Version: "1.0", URL: "http://localhost"}
	h := NewHandler(card, nil)

	req := httptest.NewRequest(http.MethodGet, WellKnownPath, nil)
	w := httptest.NewRecorder()
	h.HandleAgentCard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got AgentCard
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "test-agent" || got.Version != "1.0" {
		t.Errorf("unexpected card: %+v", got)
	}
}

func TestHandleTaskSubmit(t *testing.T) {
	triggers := &stubTriggerRegistry{}
	h := NewHandler(AgentCard{Name: "x"}, triggers)

	body := `{"message":{"role":"user","content":"hello"}}`
	req := httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleTaskSubmit(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected non-empty task ID")
	}
	if resp.Status != TaskStatusPending {
		t.Errorf("expected pending, got %s", resp.Status)
	}

	time.Sleep(20 * time.Millisecond)
	if got := triggers.dispatched.Load(); got != 1 {
		t.Errorf("expected 1 dispatch, got %d", got)
	}
}

func TestHandleTaskStatus(t *testing.T) {
	h := NewHandler(AgentCard{Name: "x"}, nil)

	// Submit first.
	body := `{"id":"task-1","message":{"role":"user","content":"hi"}}`
	submitReq := httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body))
	submitW := httptest.NewRecorder()
	h.HandleTaskSubmit(submitW, submitReq)

	// Query status.
	statusReq := httptest.NewRequest(http.MethodGet, PathTaskByID+"?task_id=task-1", nil)
	statusW := httptest.NewRecorder()
	h.HandleTaskStatus(statusW, statusReq)

	if statusW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", statusW.Code)
	}

	var resp TaskResponse
	json.NewDecoder(statusW.Body).Decode(&resp)
	if resp.ID != "task-1" || resp.Status != TaskStatusPending {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestHandleTaskCancel(t *testing.T) {
	h := NewHandler(AgentCard{Name: "x"}, nil)

	// Submit a task.
	body := `{"id":"task-cancel","message":{"role":"user","content":"x"}}`
	h.HandleTaskSubmit(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body)))

	// Cancel it.
	cancelBody := `{"task_id":"task-cancel","reason":"test"}`
	cancelReq := httptest.NewRequest(http.MethodPost, PathTaskCancel, strings.NewReader(cancelBody))
	cancelW := httptest.NewRecorder()
	h.HandleTaskCancel(cancelW, cancelReq)

	if cancelW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", cancelW.Code, cancelW.Body.String())
	}

	var resp TaskCancelResponse
	json.NewDecoder(cancelW.Body).Decode(&resp)
	if resp.Status != TaskStatusCancelled {
		t.Errorf("expected cancelled, got %s", resp.Status)
	}
}

func TestHandleTaskHistory(t *testing.T) {
	h := NewHandler(AgentCard{Name: "x"}, nil)

	body := `{"id":"task-hist","message":{"role":"user","content":"hello"}}`
	h.HandleTaskSubmit(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body)))

	histReq := httptest.NewRequest(http.MethodGet, PathTaskHistory+"?task_id=task-hist", nil)
	histW := httptest.NewRecorder()
	h.HandleTaskHistory(histW, histReq)

	if histW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", histW.Code)
	}

	var resp TaskHistoryResponse
	json.NewDecoder(histW.Body).Decode(&resp)
	if len(resp.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(resp.Messages))
	}
}

func TestHandleTaskList(t *testing.T) {
	h := NewHandler(AgentCard{Name: "x"}, nil)

	for i := 0; i < 3; i++ {
		body := `{"message":{"role":"user","content":"msg"}}`
		h.HandleTaskSubmit(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body)))
	}

	listReq := httptest.NewRequest(http.MethodGet, PathTaskList, nil)
	listW := httptest.NewRecorder()
	h.HandleTaskList(listW, listReq)

	var resp TaskListResponse
	json.NewDecoder(listW.Body).Decode(&resp)
	if resp.Total != 3 {
		t.Errorf("expected 3 tasks, got %d", resp.Total)
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	h := NewHandler(AgentCard{Name: "x"}, nil)

	body := `{"id":"task-update","message":{"role":"user","content":"x"}}`
	h.HandleTaskSubmit(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, PathTasks, strings.NewReader(body)))

	output := &MessagePart{Role: "agent", Content: "done"}
	if err := h.UpdateTaskStatus(context.Background(), "task-update", TaskStatusRunning, nil); err != nil {
		t.Fatalf("transition to running: %v", err)
	}
	if err := h.UpdateTaskStatus(context.Background(), "task-update", TaskStatusCompleted, output); err != nil {
		t.Fatalf("transition to completed: %v", err)
	}

	h.mu.RLock()
	record := h.tasks["task-update"]
	h.mu.RUnlock()
	if record.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", record.Status)
	}
	if len(record.History) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(record.History))
	}
}

func TestProtocol_CanTransition(t *testing.T) {
	tests := []struct {
		from, to TaskStatus
		valid    bool
	}{
		{TaskStatusPending, TaskStatusRunning, true},
		{TaskStatusRunning, TaskStatusCompleted, true},
		{TaskStatusRunning, TaskStatusFailed, true},
		{TaskStatusRunning, TaskStatusInputNeeded, true},
		{TaskStatusCompleted, TaskStatusRunning, false},
		{TaskStatusFailed, TaskStatusRunning, false},
		{TaskStatusCancelled, TaskStatusRunning, false},
	}
	for _, tt := range tests {
		if got := CanTransition(tt.from, tt.to); got != tt.valid {
			t.Errorf("CanTransition(%s→%s) = %v, want %v", tt.from, tt.to, got, tt.valid)
		}
	}
}

func TestProtocol_IsTerminal(t *testing.T) {
	if !IsTerminal(TaskStatusCompleted) {
		t.Error("completed should be terminal")
	}
	if !IsTerminal(TaskStatusFailed) {
		t.Error("failed should be terminal")
	}
	if IsTerminal(TaskStatusRunning) {
		t.Error("running should not be terminal")
	}
}
