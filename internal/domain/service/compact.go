package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// ═══════════════════════════════════════════
// Context Compaction — extracted from run.go for maintainability
// ═══════════════════════════════════════════

// doCompact performs LLM-based history compaction.
// Uses turn-boundary-aware slicing to preserve tool_call/tool message pairs.
func (a *AgentLoop) doCompact(runCtx context.Context) {
	a.mu.Lock()
	if len(a.history) <= 6 {
		a.mu.Unlock()
		return
	}

	// Density-aware cut: score each user turn by information density,
	// then keep the highest-density recent turns in the tail.
	type turnInfo struct {
		start   int
		density int // len(content) + toolCalls*200
	}
	var turns []turnInfo
	for i := 1; i < len(a.history); i++ {
		if a.history[i].Role == "user" {
			turns = append(turns, turnInfo{start: i})
		}
	}
	// Calculate density for each turn
	for idx := range turns {
		end := len(a.history)
		if idx+1 < len(turns) {
			end = turns[idx+1].start
		}
		for j := turns[idx].start; j < end; j++ {
			turns[idx].density += len(a.history[j].Content)
			turns[idx].density += len(a.history[j].ToolCalls) * 200
		}
	}

	// Keep last 2 turns (at minimum), but prefer high-density ones
	safeCut := 1
	if len(turns) >= 2 {
		safeCut = turns[len(turns)-2].start
	} else if len(turns) >= 1 {
		safeCut = turns[len(turns)-1].start
	} else {
		safeCut = len(a.history) / 2
	}

	// Extract middle section to summarize (skip first msg + keep tail)
	middle := a.history[1:safeCut]
	tail := make([]llm.Message, len(a.history)-safeCut)
	copy(tail, a.history[safeCut:])
	firstMsg := a.history[0] // Preserve regardless of role
	a.mu.Unlock()

	// Hook: BeforeCompact — save to vector memory before content is lost
	if a.deps.Hooks != nil {
		a.deps.Hooks.FireBeforeCompact(context.Background(), middle)
	}

	// Build summarization request
	var content strings.Builder
	for _, msg := range middle {
		if msg.Content != "" {
			content.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
	}

	// P0-D #5: Seven-dimensional checkpoint (enhanced from 4D)
	// P1 #37: Analysis draft zone — chain-of-thought improves summary quality
	summaryMessages := []llm.Message{
		{Role: "system", Content: `You are a conversation summarizer. Follow this two-step process:

STEP 1: Analysis (inside <analysis> tags)
First, analyze the conversation to identify key themes, decisions, and state changes. This draft is for your internal reasoning only.

<analysis>
- What was the user trying to accomplish?
- What were the key decision points?
- What state changes occurred (files, tools, errors)?
- What knowledge should be preserved?
</analysis>

STEP 2: Structured Summary
Then produce the final summary across seven dimensions:

## user_intent
The user's core goal and current progress status.

## session_summary
What operations were performed in this session and their outcomes.

## code_changes
Which files were modified, what specifically changed (function names + key change points).

## learned_facts
Important architectural information, constraints, or decisions that need to be remembered.

## all_user_messages
Preserve ALL user messages verbatim — these are the primary source of intent and must not be lost.

## current_work
What is currently in-progress or was the last active task. Include any pending tool calls or unfinished operations.

## errors_and_fixes
Any errors encountered during execution and how they were resolved. Include error messages and fix approaches.

CRITICAL: If the conversation contains content inside <preference_knowledge> or <semantic_knowledge> tags, it MUST be preserved in full in learned_facts — no omission or abbreviation allowed.

Output the <analysis> block first, then the seven sections. 2–3 sentences per dimension, 700 words total maximum (excluding analysis).`},
		{Role: "user", Content: content.String()},
	}

	model := a.deps.LLMRouter.CurrentModel()
	provider, _ := a.deps.LLMRouter.Resolve(model)

	// Use runCtx-derived timeout so compaction respects user Stop
	ctx, cancel := context.WithTimeout(runCtx, 30*time.Second)
	defer cancel()

	// Compact depth guard: prevent recursive summary loss (>3 consecutive compacts)
	summary := ""
	a.compactCount++
	if a.compactCount > 3 {
		// Skip LLM summary — just truncate raw to prevent information loss cascading
		for _, msg := range middle {
			if msg.Role == "assistant" && msg.Content != "" {
				summary += msg.Content[:min(300, len(msg.Content))] + "... "
			}
		}
		if summary != "" {
			summary = "[Compact limit reached — raw extraction] " + summary
		}
	} else {
		// Normal LLM-based summarization
		if provider != nil {
			req := &llm.Request{
				Model:       model,
				Messages:    summaryMessages,
				Temperature: 0.3,
				MaxTokens:   1024,
				Stream:      false,
			}

			ch := make(chan llm.StreamChunk, 32)
			resp, err := provider.GenerateStream(ctx, req, ch)
			// Drain channel
			for range ch {
			}
			if err == nil && resp != nil && resp.Content != "" {
				summary = resp.Content
			}
		}

		// Fallback: simple truncation if LLM fails
		if summary == "" {
			for _, msg := range middle {
				if msg.Role == "assistant" && msg.Content != "" {
					summary += msg.Content[:min(200, len(msg.Content))] + "... "
				}
			}
		}
	}

	// Rebuild history: first message (preserved) + summary + safe tail (complete turns)
	a.mu.Lock()
	defer a.mu.Unlock()

	compacted := []llm.Message{firstMsg}
	if summary != "" {
		// BUG-19: if firstMsg is already a summary, replace it instead of nesting
		if strings.HasPrefix(firstMsg.Content, "[COMPACT_SUMMARY]") {
			compacted = []llm.Message{{
				Role:    "assistant",
				Content: "[COMPACT_SUMMARY] " + summary,
			}}
		} else {
			compacted = append(compacted, llm.Message{
				Role:    "assistant",
				Content: "[COMPACT_SUMMARY] " + summary,
			})
		}
	}
	compacted = append(compacted, tail...)
	a.history = compacted
	a.historyDirty = true  // triggers sanitize in next doGenerate
	a.tokenTracker.Reset() // hybrid tracker baseline invalidated by restructure

	// P0-D #4: Post-compact file recovery — re-inject recently accessed file paths
	recentFiles := extractRecentFiles(middle)
	if len(recentFiles) > 0 {
		var fileBuf strings.Builder
		fileBuf.WriteString("Files recently accessed before context compaction (may need re-reading):\n")
		for _, f := range recentFiles {
			fileBuf.WriteString("- " + f + "\n")
		}
		a.history = append(a.history, llm.Message{
			Role:    "user",
			Content: fileBuf.String(),
		})
	}

	// Compression protection: re-inject active skill content after compaction.
	// Without this, skill instructions are lost when the context is summarized,
	// causing the agent to "forget" what skill it was executing.
	if len(a.activeSkills) > 0 {
		var skillBuf strings.Builder
		skillBuf.WriteString("[SKILL_RECOVERY] The following skills were active before context compaction. Resume execution:\n\n")
		for name, content := range a.activeSkills {
			skillBuf.WriteString(fmt.Sprintf("<skill_execution name=\"%s\">\n", name))
			skillBuf.WriteString(content)
			skillBuf.WriteString("\n</skill_execution>\n\n")
		}
		a.history = append(a.history, llm.Message{
			Role:    "user",
			Content: skillBuf.String(),
		})
		slog.Info(fmt.Sprintf("[compact] re-injected %d active skill(s) after compaction", len(a.activeSkills)))
	}

	// Hook: AfterCompact — notify of new compacted state
	if a.deps.Hooks != nil {
		go a.deps.Hooks.FireAfterCompact(context.Background(), compacted)
	}
}

// forceTruncate keeps only system + last N messages (turn-boundary-aware).
// Fires BeforeCompact hook on discarded messages so vector memory can preserve them.

func (a *AgentLoop) forceTruncate(keep int) {
	a.mu.Lock()
	if len(a.history) <= keep+1 {
		a.mu.Unlock()
		return
	}

	// Find safe cut point: walk backward to ensure we don't start on an orphaned tool result
	safeCut := len(a.history) - keep
	if safeCut < 1 {
		safeCut = 1
	}
	for safeCut > 1 && a.history[safeCut].Role == "tool" {
		safeCut-- // Never start on a tool result (orphaned without its tool_call)
	}

	// Fire BeforeCompact hook for discarded content (vector memory persistence)
	discarded := make([]llm.Message, len(a.history[1:safeCut]))
	copy(discarded, a.history[1:safeCut])
	a.mu.Unlock()

	if a.deps.Hooks != nil && len(discarded) > 0 {
		a.deps.Hooks.FireBeforeCompact(context.Background(), discarded)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// Rebuild: first message (preserved) + safe tail
	truncated := []llm.Message{a.history[0]}
	truncated = append(truncated, a.history[safeCut:]...)
	a.history = truncated
	a.persistedCount = 0  // history restructured, next persist must be full replace
	a.historyDirty = true // triggers sanitize in next doGenerate
}

// ─── P1-A #24: Tool-heavy compression ────────────────────────────────────

// toolHeavyCompact detects and compresses tool-heavy conversations.
// If tool outputs account for > 60% of estimated tokens, aggressively truncate
// large tool results to head+tail excerpts. This prevents the "tool output bloat"
// problem identified in CC's tool-heavy compact strategy.
//
// Called in StateGuardCheck before triggering full LLM-based doCompact.
func (a *AgentLoop) toolHeavyCompact() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.history) < 8 {
		return false
	}

	// Measure: what fraction of tokens are in tool outputs?
	totalChars := 0
	toolChars := 0
	for _, msg := range a.history {
		totalChars += len(msg.Content)
		if msg.Role == "tool" {
			toolChars += len(msg.Content)
		}
	}

	if totalChars == 0 {
		return false
	}

	toolRatio := float64(toolChars) / float64(totalChars)
	if toolRatio < 0.60 {
		return false // not tool-heavy, use standard compact
	}

	// Tool-heavy: compress all tool outputs > 10KB to head+tail excerpts
	const threshold = 10 * 1024 // 10KB
	compressed := 0
	savedChars := 0

	for i := range a.history {
		if a.history[i].Role != "tool" || len(a.history[i].Content) <= threshold {
			continue
		}

		content := a.history[i].Content
		originalLen := len(content)

		// Keep 500 chars head + 1500 chars tail (key info usually at start/end)
		headSize := 500
		tailSize := 1500
		if originalLen < headSize+tailSize+100 {
			continue // too small margin to bother
		}

		a.history[i].Content = content[:headSize] +
			fmt.Sprintf("\n\n[... tool output compressed: %d → %d bytes, %.0f%% reduction ...]\n\n",
				originalLen, headSize+tailSize, float64(originalLen-headSize-tailSize)/float64(originalLen)*100) +
			content[originalLen-tailSize:]

		compressed++
		savedChars += originalLen - headSize - tailSize
	}

	if compressed > 0 {
		slog.Info(fmt.Sprintf("[tool-heavy-compact] compressed %d outputs, saved ~%dKB (tool ratio: %.0f%%)",
			compressed, savedChars/1024, toolRatio*100))
	}
	return compressed > 0
}

// persistHistory saves NEW messages incrementally (append-only).
// Only messages added since the last persist (or session load) are written.
// This prevents destructive overwrites of existing DB history.

// This dramatically reduces context bloat in long conversations.
func (a *AgentLoop) microCompact() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.history) < 10 {
		return // too short to bother
	}

	// Count assistant messages from tail backward to find the digest boundary
	assistantCount := 0
	digestBoundary := len(a.history) // everything before this index is candidate for clearing
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "assistant" {
			assistantCount++
			if assistantCount >= 2 {
				digestBoundary = i
				break
			}
		}
	}

	// Clear tool results before the digest boundary
	cleared := 0
	for i := 0; i < digestBoundary; i++ {
		if a.history[i].Role == "tool" && len(a.history[i].Content) > 200 {
			// Preserve first line as context hint
			firstLine := a.history[i].Content
			if idx := strings.IndexByte(firstLine, '\n'); idx > 0 {
				firstLine = firstLine[:idx]
			}
			if len(firstLine) > 100 {
				firstLine = firstLine[:100]
			}
			a.history[i].Content = fmt.Sprintf("[cleared: %s]", firstLine)
			cleared++
		}
	}

	if cleared > 0 {
		slog.Info(fmt.Sprintf("[microcompact] cleared %d old tool results (boundary: msg %d/%d)", cleared, digestBoundary, len(a.history)))
	}
}

// extractRecentFiles extracts unique file paths from tool calls in the given messages.
// Returns at most 5 most recent paths for re-injection after compaction.
func extractRecentFiles(messages []llm.Message) []string {
	seen := make(map[string]bool)
	var files []string

	// Walk backward to get most recent first
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			switch tc.Function.Name {
			case "read_file", "edit_file", "write_file", "undo_edit":
				var args struct {
					Path string `json:"path"`
				}
				if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && args.Path != "" {
					if !seen[args.Path] {
						seen[args.Path] = true
						files = append(files, args.Path)
					}
				}
			}
		}
		if len(files) >= 5 {
			break
		}
	}
	return files
}

// ═══════════════════════════════════════════
// P3 I3: Git Diff Smart Compression
// ═══════════════════════════════════════════

// compressDiffOutput compresses git diff output by stripping context lines
// while preserving structural markers (file names, hunk headers, change lines).
// maxLines=0 means no line limit.
func compressDiffOutput(raw string, maxLines int) string {
	lines := strings.Split(raw, "\n")
	var out []string
	inDiff := false
	fileCount := 0
	skipped := 0

	for _, line := range lines {
		// Structural markers — always keep
		if strings.HasPrefix(line, "diff --git") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "index ") {
			inDiff = true
			fileCount++
			out = append(out, line)
			continue
		}

		// Hunk headers — always keep
		if strings.HasPrefix(line, "@@") {
			out = append(out, line)
			continue
		}

		if inDiff {
			// Change lines (+/-) — keep
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
				out = append(out, line)
				continue
			}
			// Pure context lines (space prefix or empty) — skip
			skipped++
			continue
		}

		// Outside diff block — keep everything
		out = append(out, line)
	}

	// Apply line limit
	if maxLines > 0 && len(out) > maxLines {
		remaining := len(lines) - maxLines
		out = out[:maxLines]
		out = append(out, fmt.Sprintf("... (%d more context lines across %d files)", remaining, fileCount))
	} else if skipped > 0 {
		out = append(out, fmt.Sprintf("... (%d context lines omitted)", skipped))
	}

	return strings.Join(out, "\n")
}

// ═══════════════════════════════════════════
// P3 I4: Tool Result Injection Optimization
// ═══════════════════════════════════════════

const (
	toolResultInlineMax  = 2048      // < 2K: inline directly
	toolResultSummaryMax = 32 * 1024 // 2K-32K: inline with header
	// > 32K: spill to /tmp + path reference
)

// processToolResult applies tiered size-based injection strategy to tool results.
// Prevents large outputs from bloating conversation history and consuming context budget.
func (a *AgentLoop) processToolResult(toolName, result string) string {
	n := len(result)

	switch {
	case n <= toolResultInlineMax:
		// Small: inject directly, no changes
		return result

	case n <= toolResultSummaryMax:
		// Medium: inject with size header for LLM awareness
		lineCount := strings.Count(result, "\n") + 1
		header := fmt.Sprintf("[Tool output: %d lines, %d bytes]\n", lineCount, n)

		// Git diff tools: apply structural compression
		if isDiffTool(toolName) {
			compressed := compressDiffOutput(result, 200)
			return header + compressed
		}

		// Grep: limit to first 30 matches
		if toolName == "grep_search" {
			return header + limitLines(result, 30)
		}

		return header + result

	default:
		// Large (>32K): spill to temp file, return path reference + preview
		// Exception: read_file results are always inlined — spilling prevents
		// the agent from seeing full file content (e.g. SKILL.md).
		if toolName == "read_file" {
			header := fmt.Sprintf("[Tool output: %d lines, %d bytes]\n", strings.Count(result, "\n")+1, n)
			return header + result
		}
		lineCount := strings.Count(result, "\n") + 1
		preview := limitLines(result, 30)

		// Git diff: compress before spilling
		if isDiffTool(toolName) {
			compressed := compressDiffOutput(result, 150)
			if len(compressed) <= toolResultSummaryMax {
				// Compression brought it under threshold — inline
				header := fmt.Sprintf("[Tool output: %d lines compressed from %d]\n", strings.Count(compressed, "\n")+1, lineCount)
				return header + compressed
			}
			result = compressed // spill compressed version
		}

		tmpPath := spillToTemp(toolName, result)
		return fmt.Sprintf(
			"[Tool output too large: %d lines, %d bytes. Full result saved to: %s]\n"+
				"Preview (first 30 lines):\n%s\n"+
				"Use read_file('%s') to view the complete output.",
			lineCount, n, tmpPath, preview, tmpPath,
		)
	}
}

// isDiffTool returns true for tools that produce git diff-format output.
func isDiffTool(toolName string) bool {
	switch toolName {
	case "git_diff", "diff_files", "git_log":
		return true
	}
	return false
}

// limitLines returns the first n lines of s.
func limitLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, fmt.Sprintf("... (%d more lines)", strings.Count(s, "\n")+1-n))
	}
	return strings.Join(lines, "\n")
}

// spillToTemp writes large tool output to a temp file and returns the path.
func spillToTemp(toolName, content string) string {
	tmpFile, err := os.CreateTemp("/tmp", fmt.Sprintf("ngo_%s_*.txt", toolName))
	if err != nil {
		return "/tmp/ngo_result_unknown.txt"
	}
	defer tmpFile.Close()
	tmpFile.WriteString(content)
	return tmpFile.Name()
}
