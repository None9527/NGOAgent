// Package port defines the domain boundary interfaces.
// These interfaces decouple the domain layer (service/) from infrastructure
// implementations, enabling testability and clean architecture.
package port

import "context"

// LLMProvider generates responses from language models.
type LLMProvider interface {
	// Generate sends a request and returns the response content.
	Generate(ctx context.Context, model string, messages []Message, opts GenerateOpts) (string, error)
}

// Message is a domain-level message (role + content).
type Message struct {
	Role    string
	Content string
}

// GenerateOpts holds LLM generation parameters.
type GenerateOpts struct {
	Temperature float64
	MaxTokens   int
	Stream      bool
}

// HistoryStore persists and retrieves conversation history.
type HistoryStore interface {
	// SaveAll replaces the entire session history (used after compaction).
	SaveAll(sessionID string, msgs []HistoryEntry) error
	// AppendBatch inserts new messages incrementally.
	AppendBatch(sessionID string, msgs []HistoryEntry) error
	// LoadAll retrieves all messages for a session.
	LoadAll(sessionID string) ([]HistoryEntry, error)
	// DeleteSession removes all messages for a session.
	DeleteSession(sessionID string) error
}

// HistoryEntry is a persistable message.
type HistoryEntry struct {
	Role       string
	Content    string
	ToolCalls  string // JSON-encoded
	ToolCallID string
	Reasoning  string
}

// BrainStore manages conversation artifacts (plan.md, task.md, etc.).
type BrainStore interface {
	// Read returns the content of a named artifact.
	Read(name string) (string, error)
	// Write persists an artifact.
	Write(name string, content string) error
	// BaseDir returns the brain directory path.
	BaseDir() string
}

// KnowledgeStore provides access to the knowledge item index.
type KnowledgeStore interface {
	// GenerateKIIndex returns a formatted index of all knowledge items.
	GenerateKIIndex() string
}

// MemoryStore provides semantic vector memory for conversation recall.
type MemoryStore interface {
	// FormatForPrompt returns relevant memory fragments for prompt injection.
	FormatForPrompt(query string, topK int, maxChars int) string
}

// DeltaSink receives streaming output events from the agent loop.
type DeltaSink interface {
	OnText(text string)
	OnReasoning(text string)
	OnToolStart(name string, args string)
	OnToolResult(name string, result string)
	OnComplete()
	OnAutoWakeStart()
}

// SecurityChecker validates tool calls against safety policies.
type SecurityChecker interface {
	// Check returns an approval decision for a tool call.
	Check(toolName string, args map[string]any) (approved bool, reason string)
}
