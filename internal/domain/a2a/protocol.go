package a2a

import "fmt"

// TaskStatus represents the lifecycle state of an A2A task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
	TaskStatusInputNeeded TaskStatus = "input_needed"
)

// ValidTransitions defines the allowed state transitions for a task.
var ValidTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:     {TaskStatusRunning, TaskStatusCancelled},
	TaskStatusRunning:     {TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, TaskStatusInputNeeded},
	TaskStatusInputNeeded: {TaskStatusRunning, TaskStatusCancelled},
	TaskStatusCompleted:   {},
	TaskStatusFailed:      {},
	TaskStatusCancelled:   {},
}

// CanTransition checks whether a state transition is valid.
func CanTransition(from, to TaskStatus) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// Transition attempts to transition a task record to a new status.
// Returns an error if the transition is not allowed.
func Transition(record *TaskRecord, to TaskStatus) error {
	if record == nil {
		return fmt.Errorf("nil task record")
	}
	if !CanTransition(record.Status, to) {
		return fmt.Errorf("invalid transition from %q to %q", record.Status, to)
	}
	record.Status = to
	return nil
}

// IsTerminal returns true if the status is a final state.
func IsTerminal(status TaskStatus) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// StreamEventType constants for SSE streaming.
const (
	StreamEventStatus   = "status"
	StreamEventOutput   = "output"
	StreamEventArtifact = "artifact"
	StreamEventError    = "error"
	StreamEventDone     = "done"
)

// PushNotificationType constants.
const (
	PushTypeStatusChange = "status_change"
	PushTypeOutput       = "output"
	PushTypeCompleted    = "completed"
	PushTypeFailed       = "failed"
)

// WellKnownPath is the standard discovery endpoint for agent cards.
const WellKnownPath = "/.well-known/agent.json"

// API paths for A2A endpoints.
const (
	PathTasks        = "/a2a/tasks"
	PathTaskByID     = "/a2a/tasks/"      // + {id}
	PathTaskStream   = "/a2a/tasks/stream" // + ?task_id={id}
	PathTaskHistory  = "/a2a/tasks/history" // + ?task_id={id}
	PathTaskCancel   = "/a2a/tasks/cancel"
	PathTaskList     = "/a2a/tasks/list"
)
