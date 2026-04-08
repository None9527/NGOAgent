// Package model — agent_def.go defines the AgentDefinition type for the
// SubAgent v2 system. Each definition describes a reusable agent template
// with its own tool set, context level, memory scope, and timeout.
//
// Agent definitions are loaded from YAML files at startup and resolved
// by the AgentRegistry when spawn_agent is invoked.
package model

import "time"

// ContextLevel controls how much parent context is passed to a subagent.
type ContextLevel string

const (
	// ContextTaskOnly passes only the task description and scratch directory.
	// Lowest token cost (~200). For independent, self-contained tasks.
	ContextTaskOnly ContextLevel = "task"

	// ContextWithSummary passes the task plus a distilled parent intent summary.
	// Default level (~500 tokens). Balances context and efficiency.
	ContextWithSummary ContextLevel = "summary"

	// ContextWithHistory passes the task plus recent N rounds of parent conversation.
	// Highest token cost (~2000+). For tasks that need full conversational context.
	ContextWithHistory ContextLevel = "history"
)

// MemoryScope controls cross-session memory persistence for an agent type.
type MemoryScope string

const (
	MemoryNone       MemoryScope = "none"       // No memory persistence
	MemorySession    MemoryScope = "session"     // Memory within current session only
	MemoryPersistent MemoryScope = "persistent"  // Cross-session MEMORY.md
)

// AgentDefinition describes a reusable agent template.
// Definitions are loaded from YAML files under agents/built-in/ and .ngo/agents/.
type AgentDefinition struct {
	// AgentType is the unique identifier: "researcher", "code-reviewer", "general", etc.
	AgentType string `yaml:"agent_type" json:"agent_type"`

	// DisplayName is the human-readable name shown in the frontend.
	DisplayName string `yaml:"display_name" json:"display_name"`

	// Description explains the agent's capabilities (injected into system prompt).
	Description string `yaml:"description" json:"description"`

	// Model overrides the parent model for this agent type. Empty = inherit parent.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// SystemPrompt is additional prompt text appended to the base subagent prompt.
	SystemPrompt string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`

	// Tools is the tool whitelist. ["*"] means all tools are available.
	// If non-empty, only these tools are exposed to the agent.
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty"`

	// DisallowedTools is a blacklist applied on top of the whitelist.
	DisallowedTools []string `yaml:"disallowed,omitempty" json:"disallowed,omitempty"`

	// ContextLevel controls how much parent context is passed.
	ContextLevel ContextLevel `yaml:"context_level" json:"context_level"`

	// Memory controls cross-session memory persistence.
	Memory MemoryScope `yaml:"memory" json:"memory"`

	// Background indicates whether this agent runs asynchronously by default.
	Background bool `yaml:"background" json:"background"`

	// Color is the hex color for frontend UI badge display.
	Color string `yaml:"color,omitempty" json:"color,omitempty"`

	// MaxTimeout overrides the default 5-minute barrier timeout.
	MaxTimeout time.Duration `yaml:"max_timeout" json:"max_timeout"`

	// Source indicates where the definition was loaded from.
	// Not persisted: set at load time by the registry.
	Source string `yaml:"-" json:"source"` // built-in | user | project
}

// IsReadOnly returns true if the agent has no write tools in its whitelist.
func (d *AgentDefinition) IsReadOnly() bool {
	writableTools := map[string]bool{
		"write_to_file":            true,
		"replace_file_content":     true,
		"multi_replace_file_content": true,
		"run_command":              true,
		"git_commit":               true,
	}
	// If wildcard, not read-only
	for _, t := range d.Tools {
		if t == "*" {
			return false
		}
	}
	// Check if any tool in whitelist is writable
	for _, t := range d.Tools {
		if writableTools[t] {
			return false
		}
	}
	return true
}

// HasTool checks if a tool name is allowed by this definition's whitelist/blacklist.
func (d *AgentDefinition) HasTool(toolName string) bool {
	// Check blacklist first
	for _, t := range d.DisallowedTools {
		if t == toolName {
			return false
		}
	}
	// Wildcard = allow all (minus blacklist)
	for _, t := range d.Tools {
		if t == "*" {
			return true
		}
	}
	// Explicit whitelist
	for _, t := range d.Tools {
		if t == toolName {
			return true
		}
	}
	// Empty whitelist + not blacklisted = allow (backward compat)
	return len(d.Tools) == 0
}
