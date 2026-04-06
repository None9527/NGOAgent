// Package tool — typed argument structs for all tools.
// Provides ParseArgs[T] generic parser and typed arg structs for all 27+ tools.
// Eliminates map[string]any type assertions and centralizes validation.
package tool

import (
	"encoding/json"
	"fmt"
)

// ═══════════════════════════════════════════
// ParseArgs — Generic JSON-round-trip parser
// ═══════════════════════════════════════════

// ParseArgs unmarshals a map[string]any into a typed struct via JSON round-trip.
// JSON numbers arrive from the LLM as float64 — struct fields should use int or float64
// with json tags. Required fields with zero values should be validated post-parse.
func ParseArgs[T any](args map[string]any) (T, error) {
	var result T
	data, err := json.Marshal(args)
	if err != nil {
		return result, fmt.Errorf("marshal args: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("unmarshal args into %T: %w", result, err)
	}
	return result, nil
}

// MustString safely extracts a string from map[string]any with empty fallback.
// Use for hot-paths that still use map access for backward compat.
func MustString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// MustInt safely extracts an int from map[string]any (handles float64 from JSON).
func MustInt(args map[string]any, key string) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return 0
}

// MustBool safely extracts a bool from map[string]any.
func MustBool(args map[string]any, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ═══════════════════════════════════════════
// Core File Tool Args
// ═══════════════════════════════════════════

// ReadFileArgs are the typed arguments for the read_file tool.
type ReadFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// WriteFileArgs are the typed arguments for the write_file tool.
type WriteFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditFileArgs are the typed arguments for the edit_file tool.
type EditFileArgs struct {
	Path          string `json:"path"`
	OldContent    string `json:"old_content"`
	NewContent    string `json:"new_content"`
	AllowMultiple bool   `json:"allow_multiple,omitempty"`
}

// ═══════════════════════════════════════════
// Execution Tool Args
// ═══════════════════════════════════════════

// RunCommandArgs are the typed arguments for the run_command tool.
type RunCommandArgs struct {
	Command     string `json:"command"`
	Cwd         string `json:"cwd,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
	Background  bool   `json:"background,omitempty"`
	Description string `json:"description,omitempty"`
}

// CommandStatusArgs are the typed arguments for the command_status tool.
type CommandStatusArgs struct {
	CommandID   string `json:"command_id"`
	WaitSeconds int    `json:"wait_seconds,omitempty"`
	OutputChars int    `json:"output_chars,omitempty"`
}

// ═══════════════════════════════════════════
// Search Tool Args
// ═══════════════════════════════════════════

// GlobArgs are the typed arguments for the glob tool.
type GlobArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// GrepSearchArgs are the typed arguments for the grep_search tool.
type GrepSearchArgs struct {
	Query           string   `json:"query"`
	SearchPath      string   `json:"search_path"`
	Includes        []string `json:"includes,omitempty"`
	IsRegex         bool     `json:"is_regex,omitempty"`
	CaseInsensitive bool     `json:"case_insensitive,omitempty"`
	MatchPerLine    bool     `json:"match_per_line,omitempty"`
}

// ═══════════════════════════════════════════
// Network Tool Args
// ═══════════════════════════════════════════

// WebSearchArgs are the typed arguments for the web_search tool.
type WebSearchArgs struct {
	Query      string `json:"query"`
	Domain     string `json:"domain,omitempty"`
	Categories string `json:"categories,omitempty"`
}

// WebFetchArgs are the typed arguments for the web_fetch tool.
type WebFetchArgs struct {
	URL          string `json:"url"`
	ForceStealth bool   `json:"force_stealth,omitempty"`
}

// ═══════════════════════════════════════════
// Knowledge Tool Args
// ═══════════════════════════════════════════

// SaveMemoryArgs are the typed arguments for the save_memory tool.
type SaveMemoryArgs struct {
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Importance float64  `json:"importance,omitempty"`
}

// SearchMemoryArgs are the typed arguments for the search_memory tool.
type SearchMemoryArgs struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// SaveKnowledgeArgs are the typed arguments for the save_knowledge tool.
type SaveKnowledgeArgs struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

// TaskPlanArgs are the typed arguments for the task_plan tool.
type TaskPlanArgs struct {
	Action       string `json:"action"` // create | update | read
	Type         string `json:"type"`   // plan | task
	Content      string `json:"content,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
}

// BrainArtifactArgs are the typed arguments for the brain_artifact tool.
type BrainArtifactArgs struct {
	Action  string `json:"action"` // read | write | list | delete
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

// UpdateProjectContextArgs are the typed arguments for the update_project_context tool.
type UpdateProjectContextArgs struct {
	Content string `json:"content"`
}

// ═══════════════════════════════════════════
// Agent Tool Args
// ═══════════════════════════════════════════

// SpawnAgentArgs are the typed arguments for the spawn_agent tool.
type SpawnAgentArgs struct {
	Task       string `json:"task"`
	WorkDir    string `json:"work_dir,omitempty"`
	Model      string `json:"model,omitempty"`
	MaxSteps   int    `json:"max_steps,omitempty"`
	Background bool   `json:"background,omitempty"`
}

// SkillArgs are the typed arguments for the skill tool.
type SkillArgs struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// EvoArgs are the typed arguments for the evo tool.
type EvoArgs struct {
	Action    string `json:"action"` // start | evaluate | repair
	Target    string `json:"target,omitempty"`
	MaxCycles int    `json:"max_cycles,omitempty"`
}

// ═══════════════════════════════════════════
// Meta/Protocol Tool Args
// ═══════════════════════════════════════════

// TaskBoundaryArgs are the typed arguments for the task_boundary tool.
type TaskBoundaryArgs struct {
	TaskName string `json:"task_name"`
	Mode     string `json:"mode,omitempty"`   // planning | execution | verification | complete
	Status   string `json:"status,omitempty"` // in_progress | blocked | complete
	Summary  string `json:"summary,omitempty"`
}

// NotifyUserArgs are the typed arguments for the notify_user tool.
type NotifyUserArgs struct {
	Message string `json:"message"`
	Level   string `json:"level,omitempty"` // info | warning | error
}

// SendMessageArgs are the typed arguments for the send_message tool.
type SendMessageArgs struct {
	ChatID  string `json:"chat_id"`
	Message string `json:"message"`
}

// ═══════════════════════════════════════════
// Git Tool Args
// ═══════════════════════════════════════════

// GitStatusArgs are the typed arguments for the git_status tool.
type GitStatusArgs struct {
	WorkDir string `json:"work_dir,omitempty"`
}

// GitDiffArgs are the typed arguments for the git_diff tool.
type GitDiffArgs struct {
	WorkDir  string `json:"work_dir,omitempty"`
	Staged   bool   `json:"staged,omitempty"`
	CommitA  string `json:"commit_a,omitempty"`
	CommitB  string `json:"commit_b,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

// GitLogArgs are the typed arguments for the git_log tool.
type GitLogArgs struct {
	WorkDir string `json:"work_dir,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Author  string `json:"author,omitempty"`
}

// GitCommitArgs are the typed arguments for the git_commit tool.
type GitCommitArgs struct {
	WorkDir string `json:"work_dir,omitempty"`
	Message string `json:"message"`
	AddAll  bool   `json:"add_all,omitempty"`
}

// GitBranchArgs are the typed arguments for the git_branch tool.
type GitBranchArgs struct {
	WorkDir string `json:"work_dir,omitempty"`
	Action  string `json:"action,omitempty"` // list | create | switch | delete
	Name    string `json:"name,omitempty"`
}

// ═══════════════════════════════════════════
// Other Tool Args
// ═══════════════════════════════════════════

// ManageCronArgs are the typed arguments for the manage_cron tool.
type ManageCronArgs struct {
	Action   string `json:"action"` // list | add | delete | enable | disable
	JobID    string `json:"job_id,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Command  string `json:"command,omitempty"`
}

// RecallArgs are the typed arguments for the recall tool.
type RecallArgs struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// ViewMediaArgs are the typed arguments for the view_media tool.
type ViewMediaArgs struct {
	Path string `json:"path"`
}

// ResizeImageArgs are the typed arguments for the resize_image tool.
type ResizeImageArgs struct {
	Path   string `json:"path"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Output string `json:"output,omitempty"`
}

// UndoEditArgs are the typed arguments for the undo_edit tool.
type UndoEditArgs struct {
	Path string `json:"path"`
}

// ═══════════════════════════════════════════
// P3 M2 New Tool Args (Batch M)
// ═══════════════════════════════════════════

// TreeArgs are the typed arguments for the tree tool.
type TreeArgs struct {
	Path       string `json:"path"`
	MaxDepth   int    `json:"max_depth,omitempty"`
	ShowHidden bool   `json:"show_hidden,omitempty"`
}

// FindFilesArgs are the typed arguments for the find_files tool.
type FindFilesArgs struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Extension string `json:"extension,omitempty"`
	MinSizeKB int    `json:"min_size_kb,omitempty"`
	MaxSizeKB int    `json:"max_size_kb,omitempty"`
	ModDays   int    `json:"mod_days,omitempty"` // modified within last N days
}

// CountLinesArgs are the typed arguments for the count_lines tool.
type CountLinesArgs struct {
	Path     string   `json:"path"`
	Includes []string `json:"includes,omitempty"`
}

// DiffFilesArgs are the typed arguments for the diff_files tool.
type DiffFilesArgs struct {
	PathA   string `json:"path_a"`
	PathB   string `json:"path_b"`
	Context int    `json:"context,omitempty"` // context lines around changes
}

// HTTPFetchArgs are the typed arguments for the http_fetch tool.
type HTTPFetchArgs struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// ClipboardArgs are the typed arguments for the clipboard tool.
type ClipboardArgs struct {
	Action  string `json:"action"`            // read | write
	Content string `json:"content,omitempty"` // write only
}
