// Package tool defines the domain-level tool interface.
// Implementations live in infrastructure/tool.
package tool

import "context"

// Kind classifies tool categories.
type Kind int

const (
	KindFile    Kind = iota // File operations (read, write, edit)
	KindSearch              // Search (glob, grep)
	KindExec                // Execution (run_command, command_status)
	KindNetwork             // Network (web_search, web_fetch)
	KindKnow                // Knowledge (save_memory, update_project_context, task_plan)
	KindAgent               // Agent (spawn_agent, forge)
)

// Tool is the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (ToolResult, error)
}

// Registry provides tool lookup and execution.
type Registry interface {
	Register(t Tool)
	Get(name string) (Tool, bool)
	Execute(ctx context.Context, name string, args map[string]any) (ToolResult, error)
}
