// Package entity defines the core domain entities.
package entity

import "time"

// Conversation represents a chat session.
type Conversation struct {
	ID        string
	Channel   string
	Title     string
	Status    string // active / archived
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message represents a single message in a conversation.
type Message struct {
	ID             string
	ConversationID string
	Role           string // system / user / assistant / tool
	Content        string
	ToolCallID     string
	CreatedAt      time.Time
}

// Skill represents an installed skill with metadata.
type Skill struct {
	ID          string
	Name        string
	Description string
	Type        string   // executable / workflow
	Weight      string   // light (→ ScriptTool) / heavy (→ Trigger-Inject + run_command)
	Triggers    []string // Trigger words extracted from description for auto-injection
	Content     string
	Command     string // Quick command extracted from SKILL.md bash block
	Path        string
	Enabled     bool
	ForgeStatus string // draft / forging / forged / degraded / reforging
	InstalledAt time.Time
}

// ForgeRun records a single forge execution result.
type ForgeRun struct {
	ID            string
	SkillID       string
	Success       bool
	Retries       int
	FailureReason string
	DepsAdded     []string
	Timestamp     time.Time
}
