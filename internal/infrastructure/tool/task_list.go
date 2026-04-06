package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// TaskListTool manages a persistent task list stored in brain/tasks.json.
type TaskListTool struct {
	filePath string
}

// NewTaskListTool creates a task list tool with brain directory.
func NewTaskListTool(brainDir string) *TaskListTool {
	return &TaskListTool{
		filePath: filepath.Join(brainDir, "tasks.json"),
	}
}

func (t *TaskListTool) Name() string { return "task_list" }
func (t *TaskListTool) Description() string {
	return `Manage a persistent task list. Tasks are stored in the brain and persist across sessions.
Use this to track subtasks, todos, and project items.`
}

func (t *TaskListTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: 'list', 'add', 'done', 'remove'",
				"enum":        []string{"list", "add", "done", "remove"},
			},
			"task":     map[string]any{"type": "string", "description": "Task description (for 'add')"},
			"task_id":  map[string]any{"type": "integer", "description": "Task ID (for 'done'/'remove')"},
			"priority": map[string]any{"type": "string", "description": "Priority: 'low', 'normal', 'high'"},
		},
		"required": []string{"action"},
	}
}

type taskItem struct {
	ID        int    `json:"id"`
	Task      string `json:"task"`
	Priority  string `json:"priority"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"created_at"`
	DoneAt    string `json:"done_at,omitempty"`
}

func (t *TaskListTool) Execute(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)

	tasks := t.load()

	switch action {
	case "list":
		if len(tasks) == 0 {
			return dtool.ToolResult{Output: "No tasks."}, nil
		}
		var result string
		for _, task := range tasks {
			status := "[ ]"
			if task.Done {
				status = "[x]"
			}
			result += fmt.Sprintf("%d. %s %s [%s]\n", task.ID, status, task.Task, task.Priority)
		}
		return dtool.ToolResult{Output: result}, nil

	case "add":
		taskDesc, _ := args["task"].(string)
		if taskDesc == "" {
			return dtool.ToolResult{Output: "Error: 'task' is required for 'add'"}, nil
		}
		priority := "normal"
		if p, ok := args["priority"].(string); ok && p != "" {
			priority = p
		}
		maxID := 0
		for _, task := range tasks {
			if task.ID > maxID {
				maxID = task.ID
			}
		}
		tasks = append(tasks, taskItem{
			ID:        maxID + 1,
			Task:      taskDesc,
			Priority:  priority,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		t.save(tasks)
		return dtool.ToolResult{Output: fmt.Sprintf("Added task #%d: %s", maxID+1, taskDesc)}, nil

	case "done":
		taskID := int(0)
		if id, ok := args["task_id"].(float64); ok {
			taskID = int(id)
		}
		for i, task := range tasks {
			if task.ID == taskID {
				tasks[i].Done = true
				tasks[i].DoneAt = time.Now().Format(time.RFC3339)
				t.save(tasks)
				return dtool.ToolResult{Output: fmt.Sprintf("Marked task #%d as done: %s", taskID, task.Task)}, nil
			}
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Task #%d not found", taskID)}, nil

	case "remove":
		taskID := int(0)
		if id, ok := args["task_id"].(float64); ok {
			taskID = int(id)
		}
		for i, task := range tasks {
			if task.ID == taskID {
				tasks = append(tasks[:i], tasks[i+1:]...)
				t.save(tasks)
				return dtool.ToolResult{Output: fmt.Sprintf("Removed task #%d: %s", taskID, task.Task)}, nil
			}
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Task #%d not found", taskID)}, nil

	default:
		return dtool.ToolResult{Output: fmt.Sprintf("Unknown action: %s (use list/add/done/remove)", action)}, nil
	}
}

func (t *TaskListTool) load() []taskItem {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return nil
	}
	var tasks []taskItem
	if err := json.Unmarshal(data, &tasks); err != nil {
		slog.Info(fmt.Sprintf("[task_list] WARN: corrupt task data: %v", err))
	}
	return tasks
}

func (t *TaskListTool) save(tasks []taskItem) {
	data, _ := json.MarshalIndent(tasks, "", "  ")
	os.MkdirAll(filepath.Dir(t.filePath), 0755)
	os.WriteFile(t.filePath, data, 0644)
}
