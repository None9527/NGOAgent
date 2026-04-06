// Package service — Runtime helper functions for AgentLoop.
// Extracted from run.go (Sprint 1-3): buildRuntimeInfo, git snapshot,
// token estimation, tool tiering, and utility functions.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// buildRuntimeInfo creates the environment context block injected into the system prompt.
func (a *AgentLoop) buildRuntimeInfo(model string) string {
	var b strings.Builder
	b.WriteString("# Environment\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Time: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	b.WriteString(fmt.Sprintf("- Model: %s\n", model))
	b.WriteString(fmt.Sprintf("- Prompt: %s\n", prompttext.PromptVersion))

	// Agent home: ~/.ngoagent/ (skills, brain, knowledge, config)
	homeDir := config.HomeDir()
	b.WriteString(fmt.Sprintf("- Agent Home: %s\n", homeDir))

	// Workspace: configured project working directory
	wsDir := ""
	if a.deps.Config != nil && a.deps.Config.Agent.Workspace != "" {
		ws := a.deps.Config.Agent.Workspace
		if strings.HasPrefix(ws, "~") {
			if h, err := os.UserHomeDir(); err == nil {
				ws = h + ws[1:]
			}
		}
		wsDir = ws
		b.WriteString(fmt.Sprintf("- Workspace: %s\n", ws))
	} else {
		cwd, _ := os.Getwd()
		wsDir = cwd
		b.WriteString(fmt.Sprintf("- Workspace: %s\n", cwd))
	}

	// P0-B #9: Git status snapshot — branch, modified files, recent commits
	if gitInfo := buildGitSnapshot(wsDir); gitInfo != "" {
		b.WriteString("\n# Git Status\n")
		b.WriteString(gitInfo)
	}

	return b.String()
}

// buildGitSnapshot collects git branch, short status, and recent commits.
// Returns empty string if not a git repo or git unavailable.
// Uses short timeouts (500ms) to avoid blocking.
func buildGitSnapshot(dir string) string {
	if dir == "" {
		return ""
	}

	runGit := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	// Check if this is a git repo
	branch := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("- Branch: %s\n", branch))

	// Short status: modified/added/deleted files
	status := runGit("status", "--short", "--branch")
	if status != "" {
		lines := strings.Split(status, "\n")
		// First line is branch info (already shown), skip it
		changes := []string{}
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if line != "" {
				changes = append(changes, line)
			}
		}
		if len(changes) > 0 {
			b.WriteString(fmt.Sprintf("- Modified files: %d\n", len(changes)))
			// Show at most 8 changed files
			limit := len(changes)
			if limit > 8 {
				limit = 8
			}
			for _, c := range changes[:limit] {
				b.WriteString(fmt.Sprintf("  %s\n", c))
			}
			if len(changes) > 8 {
				b.WriteString(fmt.Sprintf("  ... and %d more\n", len(changes)-8))
			}
		} else {
			b.WriteString("- Working tree: clean\n")
		}
	}

	// Recent commits (last 3)
	logOutput := runGit("log", "--oneline", "-3", "--no-decorate")
	if logOutput != "" {
		b.WriteString("- Recent commits:\n")
		for _, line := range strings.Split(logOutput, "\n") {
			if line != "" {
				b.WriteString(fmt.Sprintf("  %s\n", line))
			}
		}
	}

	return b.String()
}

// estimateTokens returns the best estimate of current prompt token usage.
// Prefers hybrid tracker (API precise + delta estimate, ±5% error) when available.
// Falls back to character-based heuristic (±30% error) on first call.
func (a *AgentLoop) estimateTokens() int {
	// Try hybrid tracker first
	if est, ok := a.tokenTracker.CurrentEstimate(); ok {
		return est
	}

	// Fallback: full character-based estimation
	a.mu.Lock()
	defer a.mu.Unlock()

	// Baseline: use tracked system prompt size (precise) instead of hardcoded guess
	total := a.tokenTracker.SystemPromptTokens()
	for _, msg := range a.history {
		total += estimateStringTokens(msg.Content)
		total += estimateStringTokens(msg.Reasoning)
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name)/4 + len(tc.Function.Arguments)/4
		}
	}
	return total
}

// estimateStringTokens counts tokens with CJK awareness.
func estimateStringTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	var tokens float64
	for _, r := range s {
		if r >= 0x2E80 { // CJK Radicals Supplement and beyond
			tokens += 1.5
		} else {
			tokens += 0.25
		}
	}
	return int(tokens)
}

// countEffectiveSteps counts non-meta tool calls in a trace JSON string.
// Meta tools (task_boundary, notify_user, etc.) don't represent substantive work.
func countEffectiveSteps(traceJSON string, metaTools map[string]bool) int {
	var steps []struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal([]byte(traceJSON), &steps); err != nil {
		return 0
	}
	count := 0
	for _, s := range steps {
		if !metaTools[s.Tool] {
			count++
		}
	}
	return count
}

// toolResultBudget returns the max output size (bytes) for a given tool.
// P0-C: Uses unified ToolMeta from domain layer instead of hardcoded map.

// ═══════════════════════════════════════════
// P3 J1: ToolSearch Lazy Loading
// ═══════════════════════════════════════════

// toolTier0 contains core tools always sent (Tier0 = always present, ~12 tools).
var toolTier0 = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"run_command": true, "command_status": true, "glob": true,
	"grep_search": true, "task_boundary": true, "notify_user": true,
	"task_plan": true, "spawn_agent": true, "brain_artifact": true,
	// P3 M2: file utility tools — always include (lightweight, frequently useful)
	"tree": true, "find_files": true, "count_lines": true, "diff_files": true,
}

// toolTierSearchKeywords triggers Tier1 (search) tools when present in user message.
var toolTierSearchKeywords = []string{
	"search", "find", "look", "browse", "web", "fetch", "url", "http", "curl", "api", "request",
}

// toolTierHeavyKeywords triggers Tier2 (heavy) tools when present in user message.
var toolTierHeavyKeywords = []string{
	"image", "resize", "media", "video", "cron", "schedule", "browser",
	"git", "commit", "diff", "branch", "memory", "recall", "knowledge",
	"clipboard", "paste", "copy",
}

// activeToolDefs returns the tool definitions to send to the LLM for this step.
// When total tool count > 30, applies tier-based lazy loading to reduce token usage.
// NOTE: mu must be held by caller (called inside a.mu.Lock block in doGenerate).
func (a *AgentLoop) activeToolDefs(all []llm.ToolDef) []llm.ToolDef {
	// Below threshold: always send all tools
	if len(all) <= 30 {
		return all
	}

	// After first 2 steps: send all tools (agent has established context)
	if a.task.CurrentStep >= 2 {
		return all
	}

	// Gather last user message for keyword detection
	lastMsg := ""
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "user" {
			lastMsg = strings.ToLower(a.history[i].Content)
			break
		}
	}

	// Check for search/heavy keywords → include those tiers
	wantSearch := false
	for _, kw := range toolTierSearchKeywords {
		if strings.Contains(lastMsg, kw) {
			wantSearch = true
			break
		}
	}
	wantHeavy := false
	for _, kw := range toolTierHeavyKeywords {
		if strings.Contains(lastMsg, kw) {
			wantHeavy = true
			break
		}
	}

	// If both extra tiers needed, just return all
	if wantSearch && wantHeavy {
		return all
	}

	// Filter to active tiers
	var active []llm.ToolDef
	for _, t := range all {
		if toolTier0[t.Function.Name] {
			active = append(active, t)
			continue
		}
		if wantSearch {
			switch t.Function.Name {
			case "web_search", "web_fetch", "deep_research", "http_fetch":
				active = append(active, t)
			}
		}
		if wantHeavy {
			switch t.Function.Name {
			case "git_status", "git_diff", "git_log", "git_commit", "git_branch",
				"save_memory", "search_memory", "recall",
				"save_knowledge", "resize_image", "view_media", "manage_cron",
				"clipboard":
				active = append(active, t)
			}
		}
	}
	return active
}
