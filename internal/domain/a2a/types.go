// Package a2a defines the Agent-to-Agent protocol types and state machine.
// These types form the contract for cross-agent communication, supporting
// synchronous request/response, SSE streaming, push notification callbacks,
// and task history queries.
package a2a

import "time"

// AgentCard describes an agent's identity and capabilities for discovery.
type AgentCard struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Version       string            `json:"version"`
	URL           string            `json:"url"`
	Provider      string            `json:"provider,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	Skills        []SkillDescriptor `json:"skills,omitempty"`
	InputModes    []string          `json:"input_modes,omitempty"`
	OutputModes   []string          `json:"output_modes,omitempty"`
	PushEndpoint  string            `json:"push_endpoint,omitempty"`
	Authentication *AuthInfo        `json:"authentication,omitempty"`
}

// SkillDescriptor describes a single skill an agent can perform.
type SkillDescriptor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	InputModes  []string `json:"input_modes,omitempty"`
	OutputModes []string `json:"output_modes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// AuthInfo describes the authentication requirements for an agent.
type AuthInfo struct {
	Schemes []string `json:"schemes,omitempty"` // "bearer", "api_key", etc.
}

// TaskRequest is the inbound request to create or continue a task.
type TaskRequest struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id,omitempty"`
	SkillID   string         `json:"skill_id,omitempty"`
	Message   MessagePart    `json:"message"`
	Mode      string         `json:"mode,omitempty"`
	PushURL   string         `json:"push_url,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// MessagePart represents a single message in the A2A exchange.
type MessagePart struct {
	Role    string `json:"role"` // "user", "agent"
	Content string `json:"content"`
}

// TaskResponse is the outbound response for a task.
type TaskResponse struct {
	ID        string         `json:"id"`
	Status    TaskStatus     `json:"status"`
	Output    *MessagePart   `json:"output,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []MessagePart  `json:"history,omitempty"`
	Error     *TaskError     `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

// TaskError describes an error in task execution.
type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Artifact represents a file or data artifact produced by a task.
type Artifact struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type,omitempty"`
	Content  string `json:"content,omitempty"`
	URL      string `json:"url,omitempty"`
}

// TaskStreamEvent is a Server-Sent Event payload for streaming task updates.
type TaskStreamEvent struct {
	Type      string       `json:"type"` // "status", "output", "artifact", "error", "done"
	Status    TaskStatus   `json:"status,omitempty"`
	Output    *MessagePart `json:"output,omitempty"`
	Artifact  *Artifact    `json:"artifact,omitempty"`
	Error     *TaskError   `json:"error,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"`
}

// PushNotification is the payload sent to a push callback URL.
type PushNotification struct {
	TaskID    string       `json:"task_id"`
	Type      string       `json:"type"` // "status_change", "output", "completed", "failed"
	Status    TaskStatus   `json:"status"`
	Output    *MessagePart `json:"output,omitempty"`
	Error     *TaskError   `json:"error,omitempty"`
	Timestamp string       `json:"timestamp"`
}

// TaskHistoryRequest queries the history of a task.
type TaskHistoryRequest struct {
	TaskID string `json:"task_id"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// TaskHistoryResponse returns the conversation history for a task.
type TaskHistoryResponse struct {
	TaskID   string        `json:"task_id"`
	Messages []MessagePart `json:"messages"`
	Total    int           `json:"total"`
}

// TaskCancelRequest requests cancellation of a running task.
type TaskCancelRequest struct {
	TaskID  string `json:"task_id"`
	Reason  string `json:"reason,omitempty"`
}

// TaskCancelResponse acknowledges a cancellation request.
type TaskCancelResponse struct {
	TaskID string     `json:"task_id"`
	Status TaskStatus `json:"status"`
}

// TaskListRequest queries tasks, optionally filtered by session.
type TaskListRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status,omitempty"` // filter by status
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// TaskListResponse returns a list of task summaries.
type TaskListResponse struct {
	Tasks []TaskSummary `json:"tasks"`
	Total int           `json:"total"`
}

// TaskSummary is a compact representation of a task for listing.
type TaskSummary struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id,omitempty"`
	SkillID   string     `json:"skill_id,omitempty"`
	Status    TaskStatus `json:"status"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

// TaskRecord is the internal storage representation bridging to RunSnapshot.
type TaskRecord struct {
	ID        string
	SessionID string
	SkillID   string
	RunID     string
	PushURL   string
	Status    TaskStatus
	History   []MessagePart
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}
