package tool

import (
	"context"
	"encoding/json"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
)

// TaskBoundaryTool indicates task state transitions and reports structured progress.
// Mirrors Antigravity's task_boundary: sets Mode, TaskName, TaskStatus, TaskSummary.
// The infrastructure tracks the latest state and re-injects it via ephemeral reminders.
type TaskBoundaryTool struct{}

func NewTaskBoundaryTool() *TaskBoundaryTool {
	return &TaskBoundaryTool{}
}

func (t *TaskBoundaryTool) Name() string        { return "task_boundary" }
func (t *TaskBoundaryTool) Description() string { return prompttext.ToolTaskBoundary }

func (t *TaskBoundaryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_name": map[string]any{"type": "string", "description": "Name of the current task. Use the same name to update an existing task, or a new name to start a new task."},
			"status":    map[string]any{"type": "string", "description": "What you are GOING TO DO NEXT (not what you already did)"},
			"summary":   map[string]any{"type": "string", "description": "Cumulative summary of what has been accomplished so far"},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"planning", "execution", "verification"},
				"description": "Current work mode",
			},
			"predicted_task_size": map[string]any{
				"type":        "integer",
				"description": "Estimated number of tool calls needed to complete this task",
			},
		},
		"required": []string{"task_name", "status"},
	}
}

// BoundaryUpdate is the structured progress event sent to the frontend.
type BoundaryUpdate struct {
	TaskName          string `json:"task_name"`
	Status            string `json:"status"`
	Summary           string `json:"summary,omitempty"`
	Mode              string `json:"mode,omitempty"`
	PredictedTaskSize int    `json:"predicted_task_size,omitempty"`
}

func (t *TaskBoundaryTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	taskName, _ := args["task_name"].(string)
	status, _ := args["status"].(string)
	summary, _ := args["summary"].(string)
	mode, _ := args["mode"].(string)
	predictedSize, _ := args["predicted_task_size"].(float64) // JSON numbers are float64

	if taskName == "" {
		return dtool.ToolResult{Output: "Error: task_name is required"}, nil
	}

	update := BoundaryUpdate{
		TaskName:          taskName,
		Status:            status,
		Summary:           summary,
		Mode:              mode,
		PredictedTaskSize: int(predictedSize),
	}

	data, _ := json.Marshal(update)
	return dtool.ToolResult{
		Output:  "Task boundary updated.",
		Signal:  dtool.SignalProgress,
		Payload: map[string]any{
			"task_name": taskName,
			"status":    status,
			"summary":   summary,
			"mode":      mode,
			"raw_json":  string(data),
		},
	}, nil
}
