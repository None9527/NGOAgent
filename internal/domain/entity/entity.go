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
	Type        string   // pipeline / executable / workflow
	Weight      string   // light (→ ScriptTool) / heavy (→ Trigger-Inject + run_command)
	Triggers    []string // Trigger words extracted from description for auto-injection
	Rules       []string // Execution rules auto-injected on trigger match
	Content     string
	Command     string // Quick command extracted from SKILL.md bash block
	Path        string
	Enabled     bool
	EvoStatus   string // draft / evolving / evolved / degraded / re-evolving
	InstalledAt time.Time

	// Skill discovery + execution metadata (from SKILL.md frontmatter)
	WhenToUse string // precise trigger condition for skill listing
	Context   string // "inline" (default) | "fork" (spawn sub-agent)
	Args      string // parameter hint (e.g. "[topic] [style?]")

	// P3 L3: KI categorization
	Category string   // auto-classified: "infra" | "web" | "data" | "ai" | "devops" | "util"
	KIRef    []string // paths to associated KI artifact files
}

// EvoRun records a single evolution execution result.
type EvoRun struct {
	ID            string
	SkillID       string
	Success       bool
	Retries       int
	Strategy      string // param_fix | tool_swap | re_route | iterate | escalate
	FailureReason string
	Timestamp     time.Time
}
