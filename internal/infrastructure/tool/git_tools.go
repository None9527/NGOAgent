package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ═══════════════════════════════════════════
// Git Integration Tools — P1-D #71
// Provides agent-native Git commands: status, diff, log, commit, branch
// ═══════════════════════════════════════════

// GitStatusTool returns the current git repository state (branch, status, recent commits).
type GitStatusTool struct{}

func (t *GitStatusTool) Name() string { return "git_status" }
func (t *GitStatusTool) Description() string {
	return "Get the current git repository status: branch, staged/unstaged changes, and recent commits."
}
func (t *GitStatusTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}
func (t *GitStatusTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	var out strings.Builder

	// Branch name
	if branch, err := runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		out.WriteString("Branch: " + strings.TrimSpace(branch) + "\n\n")
	}

	// Short status
	if status, err := runGit(ctx, "status", "--short"); err == nil {
		if strings.TrimSpace(status) == "" {
			out.WriteString("Status: clean (no changes)\n")
		} else {
			out.WriteString("Changes:\n" + status + "\n")
		}
	}

	// Last 5 commits
	if log, err := runGit(ctx, "log", "--oneline", "-5"); err == nil {
		out.WriteString("\nRecent commits:\n" + log)
	}

	return dtool.ToolResult{Output: out.String()}, nil
}

// GitDiffTool shows the diff of staged or unstaged changes.
type GitDiffTool struct{}

func (t *GitDiffTool) Name() string { return "git_diff" }
func (t *GitDiffTool) Description() string {
	return "Show git diff for staged changes, unstaged changes, or a specific file."
}
func (t *GitDiffTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"staged": map[string]any{"type": "boolean", "description": "Show staged (--cached) diff. Default false (unstaged).", "default": false},
			"path":   map[string]any{"type": "string", "description": "Limit diff to this file path (optional)."},
		},
		"required": []string{},
	}
}
func (t *GitDiffTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	staged, _ := args["staged"].(bool)
	path, _ := args["path"].(string)

	gitArgs := []string{"diff"}
	if staged {
		gitArgs = append(gitArgs, "--cached")
	}
	// Stat summary first to give token-efficient overview
	statArgs := append(append([]string{}, gitArgs...), "--stat")
	if path != "" {
		statArgs = append(statArgs, "--", path)
	}

	var out strings.Builder
	if stat, err := runGit(ctx, statArgs...); err == nil {
		out.WriteString("=== Diff stat ===\n" + stat + "\n")
	}

	// Full diff (limited to 8KB to avoid flooding context)
	fullArgs := append([]string{}, gitArgs...)
	if path != "" {
		fullArgs = append(fullArgs, "--", path)
	}
	if diff, err := runGit(ctx, fullArgs...); err == nil {
		if len(diff) > 8192 {
			diff = diff[:8192] + fmt.Sprintf("\n\n[... diff truncated at 8KB, %d bytes total ...]", len(diff))
		}
		out.WriteString("=== Diff ===\n" + diff)
	}

	if out.Len() == 0 {
		return dtool.ToolResult{Output: "No diff output (nothing changed or git not available)."}, nil
	}
	return dtool.ToolResult{Output: out.String()}, nil
}

// GitLogTool shows the commit history.
type GitLogTool struct{}

func (t *GitLogTool) Name() string { return "git_log" }
func (t *GitLogTool) Description() string {
	return "Show git commit history with author, date, and message."
}
func (t *GitLogTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"n":    map[string]any{"type": "integer", "description": "Number of commits to show. Default 10.", "default": 10},
			"path": map[string]any{"type": "string", "description": "Limit log to commits touching this file (optional)."},
		},
		"required": []string{},
	}
}
func (t *GitLogTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	n := 10
	if nArg, ok := args["n"].(float64); ok && nArg > 0 {
		n = int(nArg)
	}
	if n > 50 {
		n = 50 // cap to prevent flooding
	}
	path, _ := args["path"].(string)

	gitArgs := []string{"log", fmt.Sprintf("-%d", n), "--pretty=format:%h %ad %an: %s", "--date=short"}
	if path != "" {
		gitArgs = append(gitArgs, "--", path)
	}

	out, err := runGit(ctx, gitArgs...)
	if err != nil {
		return dtool.ToolResult{Output: "git log failed: " + err.Error()}, nil
	}
	return dtool.ToolResult{Output: out}, nil
}

// GitCommitTool stages all changes and creates a commit.
type GitCommitTool struct{}

func (t *GitCommitTool) Name() string { return "git_commit" }
func (t *GitCommitTool) Description() string {
	return "Stage all changes (git add -A) and create a commit with the given message."
}
func (t *GitCommitTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "Commit message. Use conventional commits format: 'feat: ...', 'fix: ...', 'refactor: ...'",
				"minLength":   5,
			},
			"add_all": map[string]any{
				"type":        "boolean",
				"description": "Stage all changes before committing (git add -A). Default true.",
				"default":     true,
			},
		},
		"required": []string{"message"},
	}
}
func (t *GitCommitTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	message, _ := args["message"].(string)
	addAll := true
	if v, ok := args["add_all"].(bool); ok {
		addAll = v
	}

	if message == "" {
		return dtool.ToolResult{Output: "Error: 'message' is required"}, nil
	}

	var out strings.Builder

	if addAll {
		if result, err := runGit(ctx, "add", "-A"); err != nil {
			return dtool.ToolResult{Output: "git add failed: " + err.Error() + "\n" + result}, nil
		}
		out.WriteString("✓ Staged all changes\n")
	}

	result, err := runGit(ctx, "commit", "-m", message)
	if err != nil {
		return dtool.ToolResult{Output: "git commit failed: " + err.Error() + "\n" + result}, nil
	}
	out.WriteString("✓ Committed: " + result)

	return dtool.ToolResult{Output: out.String()}, nil
}

// GitBranchTool creates or lists branches.
type GitBranchTool struct{}

func (t *GitBranchTool) Name() string { return "git_branch" }
func (t *GitBranchTool) Description() string {
	return "List all branches, create a new branch, or checkout an existing branch."
}
func (t *GitBranchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "create", "checkout"},
				"description": "Action: 'list' shows all branches, 'create' makes a new branch, 'checkout' switches branches.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Branch name (required for 'create' and 'checkout').",
			},
		},
		"required": []string{"action"},
	}
}
func (t *GitBranchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)

	switch action {
	case "list", "":
		out, err := runGit(ctx, "branch", "-a")
		if err != nil {
			return dtool.ToolResult{Output: "git branch failed: " + err.Error()}, nil
		}
		return dtool.ToolResult{Output: out}, nil

	case "create":
		if name == "" {
			return dtool.ToolResult{Output: "Error: 'name' is required for action=create"}, nil
		}
		out, err := runGit(ctx, "checkout", "-b", name)
		if err != nil {
			return dtool.ToolResult{Output: "git checkout -b failed: " + err.Error() + "\n" + out}, nil
		}
		return dtool.ToolResult{Output: "✓ Created and switched to branch: " + name + "\n" + out}, nil

	case "checkout":
		if name == "" {
			return dtool.ToolResult{Output: "Error: 'name' is required for action=checkout"}, nil
		}
		out, err := runGit(ctx, "checkout", name)
		if err != nil {
			return dtool.ToolResult{Output: "git checkout failed: " + err.Error() + "\n" + out}, nil
		}
		return dtool.ToolResult{Output: "✓ Switched to branch: " + name + "\n" + out}, nil

	default:
		return dtool.ToolResult{Output: "Error: unknown action '" + action + "'. Use 'list', 'create', or 'checkout'."}, nil
	}
}

// ─── helper ──────────────────────────────────────────────────────────────

// runGit executes a git command in the current working directory.
// Returns (stdout+stderr, error).
func runGit(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Detect repo root: walk up from CWD
	cwd, _ := os.Getwd()
	repoRoot := findGitRoot(cwd)

	cmd := exec.CommandContext(ctx, "git", args...)
	if repoRoot != "" {
		cmd.Dir = repoRoot
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// findGitRoot walks up from dir looking for a .git directory.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root, no git repo
		}
		dir = parent
	}
}
