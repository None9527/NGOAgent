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
	KindMeta                // Meta/protocol (task_boundary, notify_user)
)

// AccessLevel classifies tool impact for security decisions.
type AccessLevel int

const (
	AccessReadOnly    AccessLevel = iota // No side-effects (read_file, glob, grep)
	AccessWrite                          // Modifiable but reversible (edit_file, write_file)
	AccessDestructive                    // Potentially irreversible (run_command with rm, etc.)
	AccessNetwork                        // External communication (web_fetch, web_search)
)

// ToolMeta provides rich metadata about a tool for security, orchestration, and output control.
// P0-C: Unified metadata layer — replaces hardcoded tool-name checks throughout the system.
type ToolMeta struct {
	Kind            Kind        // Category classification
	Access          AccessLevel // Impact level for security decisions
	ConcurrencySafe bool        // Safe for parallel execution in subagent barrier
	MaxOutputSize   int         // Max output bytes before truncation (0 = use default 50KB)
}

// Tool is the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (ToolResult, error)
}

// MetaProvider is an optional interface tools can implement to provide custom metadata.
// Tools that don't implement this get defaults from DefaultMeta().
type MetaProvider interface {
	Meta() ToolMeta
}

// Registry provides tool lookup and execution.
type Registry interface {
	Register(t Tool)
	Get(name string) (Tool, bool)
	Execute(ctx context.Context, name string, args map[string]any) (ToolResult, error)
}

// DefaultMeta returns the metadata for a tool by name.
// If the tool implements MetaProvider, that takes priority.
// Otherwise, falls back to the built-in defaultMetas table.
func DefaultMeta(name string) ToolMeta {
	if m, ok := defaultMetas[name]; ok {
		return m
	}
	// Unknown tools get conservative defaults
	return ToolMeta{
		Kind:            KindMeta,
		Access:          AccessWrite,
		ConcurrencySafe: false,
		MaxOutputSize:   50 * 1024,
	}
}

// defaultMetas is the built-in metadata registry for all known tools.
// This is the single source of truth for tool classification.
var defaultMetas = map[string]ToolMeta{
	// ─── File (read-only) ───
	"read_file":   {Kind: KindFile, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 80 * 1024},
	"glob":        {Kind: KindSearch, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},
	"grep_search": {Kind: KindSearch, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},

	// ─── File (write) ───
	"write_file": {Kind: KindFile, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"edit_file":  {Kind: KindFile, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 20 * 1024},
	"undo_edit":  {Kind: KindFile, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},

	// ─── Execution (destructive) ───
	"run_command":    {Kind: KindExec, Access: AccessDestructive, ConcurrencySafe: false, MaxOutputSize: 60 * 1024},
	"command_status": {Kind: KindExec, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 60 * 1024},

	// ─── Network ───
	"web_search":    {Kind: KindNetwork, Access: AccessNetwork, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},
	"web_fetch":     {Kind: KindNetwork, Access: AccessNetwork, ConcurrencySafe: true, MaxOutputSize: 100 * 1024},
	"deep_research": {Kind: KindNetwork, Access: AccessNetwork, ConcurrencySafe: true, MaxOutputSize: 80 * 1024},

	// ─── Knowledge ───
	"task_plan":              {Kind: KindKnow, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"update_project_context": {Kind: KindKnow, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"save_knowledge":         {Kind: KindKnow, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"save_memory":            {Kind: KindKnow, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"search_memory":          {Kind: KindKnow, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"brain_artifact":         {Kind: KindKnow, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 20 * 1024},

	// ─── Agent ───
	"spawn_agent": {Kind: KindAgent, Access: AccessWrite, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},
	"evo":         {Kind: KindAgent, Access: AccessDestructive, ConcurrencySafe: false, MaxOutputSize: 20 * 1024},

	// ─── Meta/Protocol ───
	"task_boundary": {Kind: KindMeta, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 2 * 1024},
	"notify_user":   {Kind: KindMeta, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 5 * 1024},
	"send_message":  {Kind: KindMeta, Access: AccessNetwork, ConcurrencySafe: true, MaxOutputSize: 5 * 1024},
	"view_media":    {Kind: KindFile, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"resize_image":  {Kind: KindFile, Access: AccessWrite, ConcurrencySafe: true, MaxOutputSize: 10 * 1024},
	"manage_cron":   {Kind: KindMeta, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"recall":        {Kind: KindKnow, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},

	// ─── Git (P1-D) ───
	"git_status": {Kind: KindExec, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"git_diff":   {Kind: KindExec, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"git_log":    {Kind: KindExec, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"git_commit": {Kind: KindExec, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},
	"git_branch": {Kind: KindExec, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 10 * 1024},

	// ─── P3 M2 (#45): New tools — CC parity ───
	"tree":        {Kind: KindFile, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},
	"find_files":  {Kind: KindSearch, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 30 * 1024},
	"count_lines": {Kind: KindFile, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 20 * 1024},
	"diff_files":  {Kind: KindFile, Access: AccessReadOnly, ConcurrencySafe: true, MaxOutputSize: 50 * 1024},
	"http_fetch":  {Kind: KindNetwork, Access: AccessNetwork, ConcurrencySafe: true, MaxOutputSize: 100 * 1024},
	"clipboard":   {Kind: KindMeta, Access: AccessWrite, ConcurrencySafe: false, MaxOutputSize: 5 * 1024},
}
