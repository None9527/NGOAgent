package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

// Handler provides HTTP handlers for the A2A protocol.
type Handler struct {
	mu       sync.RWMutex
	card     AgentCard
	tasks    map[string]*TaskRecord
	triggers graphruntime.TriggerRegistry
}

// NewHandler creates an A2A handler with the given agent card and trigger registry.
func NewHandler(card AgentCard, triggers graphruntime.TriggerRegistry) *Handler {
	return &Handler{
		card:     card,
		tasks:    make(map[string]*TaskRecord),
		triggers: triggers,
	}
}

// HandleAgentCard returns the agent's discovery card.
func (h *Handler) HandleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.card)
}

// HandleTaskSubmit creates a new task and dispatches it via the trigger registry.
func (h *Handler) HandleTaskSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	record := &TaskRecord{
		ID:        req.ID,
		SessionID: req.SessionID,
		SkillID:   req.SkillID,
		PushURL:   req.PushURL,
		Status:    TaskStatusPending,
		History:   []MessagePart{req.Message},
		Metadata:  req.Metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	h.mu.Lock()
	h.tasks[req.ID] = record
	h.mu.Unlock()

	// Dispatch via trigger registry.
	if h.triggers != nil {
		event := graphruntime.TriggerEvent{
			Kind:      graphruntime.TriggerA2A,
			Source:    "a2a",
			SessionID: req.SessionID,
			Trigger:   req.SkillID,
			Payload: map[string]any{
				"task_id": req.ID,
				"message": req.Message.Content,
				"mode":    req.Mode,
			},
			At: time.Now().UTC(),
		}
		if err := h.triggers.Dispatch(r.Context(), event); err != nil {
			slog.Warn("a2a trigger dispatch error", slog.String("task_id", req.ID), slog.String("error", err.Error()))
		}
	}

	resp := TaskResponse{
		ID:        record.ID,
		Status:    record.Status,
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// HandleTaskStatus returns the current status of a task.
func (h *Handler) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		// Try path suffix.
		taskID = r.PathValue("id")
	}
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "task_id is required")
		return
	}

	h.mu.RLock()
	record, ok := h.tasks[taskID]
	h.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	resp := TaskResponse{
		ID:        record.ID,
		Status:    record.Status,
		History:   record.History,
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleTaskStream opens an SSE stream for real-time task updates.
func (h *Handler) HandleTaskStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "task_id is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastStatus := TaskStatus("")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.RLock()
			record, exists := h.tasks[taskID]
			h.mu.RUnlock()
			if !exists {
				writeSSE(w, flusher, TaskStreamEvent{
					Type:      StreamEventError,
					Error:     &TaskError{Code: "not_found", Message: "task not found"},
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
				return
			}

			if record.Status != lastStatus {
				lastStatus = record.Status
				evt := TaskStreamEvent{
					Type:      StreamEventStatus,
					Status:    record.Status,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if len(record.History) > 0 {
					last := record.History[len(record.History)-1]
					if last.Role == "agent" {
						evt.Type = StreamEventOutput
						evt.Output = &last
					}
				}
				writeSSE(w, flusher, evt)

				if IsTerminal(record.Status) {
					writeSSE(w, flusher, TaskStreamEvent{
						Type:      StreamEventDone,
						Status:    record.Status,
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					})
					return
				}
			}
		}
	}
}

// HandleTaskHistory returns the conversation history for a task.
func (h *Handler) HandleTaskHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "task_id is required")
		return
	}

	h.mu.RLock()
	record, ok := h.tasks[taskID]
	h.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}

	resp := TaskHistoryResponse{
		TaskID:   record.ID,
		Messages: record.History,
		Total:    len(record.History),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleTaskCancel cancels a running task.
func (h *Handler) HandleTaskCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TaskCancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	h.mu.Lock()
	record, ok := h.tasks[req.TaskID]
	if !ok {
		h.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err := Transition(record, TaskStatusCancelled); err != nil {
		h.mu.Unlock()
		writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		return
	}
	record.UpdatedAt = time.Now().UTC()
	h.mu.Unlock()

	resp := TaskCancelResponse{
		TaskID: record.ID,
		Status: record.Status,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleTaskList returns a list of task summaries.
func (h *Handler) HandleTaskList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.mu.RLock()
	tasks := make([]TaskSummary, 0, len(h.tasks))
	for _, record := range h.tasks {
		tasks = append(tasks, TaskSummary{
			ID:        record.ID,
			SessionID: record.SessionID,
			SkillID:   record.SkillID,
			Status:    record.Status,
			CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	h.mu.RUnlock()

	resp := TaskListResponse{
		Tasks: tasks,
		Total: len(tasks),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateTaskStatus updates the status of a task and sends a push notification if configured.
func (h *Handler) UpdateTaskStatus(ctx context.Context, taskID string, status TaskStatus, output *MessagePart) error {
	h.mu.Lock()
	record, ok := h.tasks[taskID]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("task not found: %s", taskID)
	}
	if err := Transition(record, status); err != nil {
		h.mu.Unlock()
		return err
	}
	record.UpdatedAt = time.Now().UTC()
	if output != nil {
		record.History = append(record.History, *output)
	}
	pushURL := record.PushURL
	h.mu.Unlock()

	// Send push notification if configured.
	if pushURL != "" {
		go h.sendPushNotification(ctx, pushURL, PushNotification{
			TaskID:    taskID,
			Type:      pushTypeForStatus(status),
			Status:    status,
			Output:    output,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return nil
}

func (h *Handler) sendPushNotification(_ context.Context, url string, notification PushNotification) {
	body, err := json.Marshal(notification)
	if err != nil {
		slog.Warn("a2a push marshal error", slog.String("error", err.Error()))
		return
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Warn("a2a push notification error",
			slog.String("task_id", notification.TaskID),
			slog.String("url", url),
			slog.String("error", err.Error()),
		)
		return
	}
	resp.Body.Close()
}

func pushTypeForStatus(status TaskStatus) string {
	switch status {
	case TaskStatusCompleted:
		return PushTypeCompleted
	case TaskStatusFailed:
		return PushTypeFailed
	default:
		return PushTypeStatusChange
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(TaskResponse{
		Error: &TaskError{Code: code, Message: message},
	})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event TaskStreamEvent) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
