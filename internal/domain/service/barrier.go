// Package service — barrier.go implements the SubagentBarrier for
// coordinating multiple parallel subagent completions.
//
// When a parent agent spawns N subagents, the barrier tracks them and
// triggers a parent-loop auto-wake once ALL have completed.
//
// Flow:
//   parent spawns A, B, C → barrier.pending = 3
//   C completes → pending = 2, progress pushed
//   A completes → pending = 1, progress pushed
//   B completes → pending = 0 → InjectEphemeral(allResults) → autoWake()
package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
)

// SubagentBarrier coordinates parallel subagent completion for a parent loop.
// Thread-safe: all methods are safe for concurrent calls from multiple goroutines.
type SubagentBarrier struct {
	mu           sync.Mutex
	pending      int
	results      map[string]subagentResult
	parentLoop   *AgentLoop
	autoWake     func()                          // called when all subagents complete
	pushProgress func(runID, taskName, status string, done, total int, errMsg, output string) // WS/SSE push (optional)
	timeout      time.Duration
	timer        *time.Timer
}

type subagentResult struct {
	RunID    string
	TaskName string
	Output   string
	Error    error
	DoneAt   time.Time
}

// NewSubagentBarrier creates a barrier for coordinating N subagent completions.
// autoWake is called (once) when the last subagent completes.
func NewSubagentBarrier(parentLoop *AgentLoop, autoWake func()) *SubagentBarrier {
	return &SubagentBarrier{
		results:  make(map[string]subagentResult),
		parentLoop: parentLoop,
		autoWake:   autoWake,
		timeout:    5 * time.Minute,
	}
}

// SetProgressPush registers a function for pushing per-subagent SSE progress events.
// The push func is called each time a subagent completes (before finalize).
func (b *SubagentBarrier) SetProgressPush(fn func(runID, taskName, status string, done, total int, errMsg, output string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pushProgress = fn
}


// Add registers a new subagent to track. Call before RunAsync.
func (b *SubagentBarrier) Add(runID, taskName string) {
	b.mu.Lock()
	b.pending++
	b.results[runID] = subagentResult{RunID: runID, TaskName: taskName}
	total := len(b.results)
	push := b.pushProgress

	// Start/reset timeout timer
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.timeout, func() {
		b.onTimeout()
	})
	b.mu.Unlock()

	// Push "running" progress event so the frontend knows about this subagent immediately
	if push != nil {
		done := total - b.pending
		name := taskName
		if name == "" {
			name = runID
		}
		go push(runID, name, "running", done, total, "", "")
	}
}

// OnComplete is called when a subagent finishes. Thread-safe.
func (b *SubagentBarrier) OnComplete(runID, result string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.results[runID]
	entry.Output = result
	entry.Error = err
	entry.DoneAt = time.Now()
	b.results[runID] = entry
	b.pending--

	total := len(b.results)
	done := total - b.pending
	log.Printf("[barrier] Subagent %s completed (%d/%d)", runID, done, total)

	// Push per-completion progress event (non-blocking)
	if b.pushProgress != nil {
		name := entry.TaskName
		if name == "" {
			name = runID
		}
		status := "completed"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		// Truncate output for preview (max 500 chars)
		outputPreview := entry.Output
		if len(outputPreview) > 500 {
			outputPreview = outputPreview[:500] + "..."
		}
		// Call outside lock to avoid deadlock (capture values)
		push := b.pushProgress
		go push(runID, name, status, done, total, errMsg, outputPreview)
	}

	if b.pending <= 0 {
		if b.timer != nil {
			b.timer.Stop()
		}
		b.finalize()
	}
}

// finalize injects all results into parent loop and triggers wake.
// Uses SignalWake for orchestration — if parent is running, the tail-check
// picks it up within the same lock. If idle, autoWake fallback calls Run().
// Caller must hold b.mu.
func (b *SubagentBarrier) finalize() {
	summary := b.formatResults()
	b.parentLoop.InjectEphemeral(summary)
	b.parentLoop.SignalWake()
	log.Printf("[barrier] All %d subagents complete, waking parent", len(b.results))

	if b.autoWake != nil {
		go b.autoWake()
	}
}

// onTimeout fires when subagents take too long. Collects partial results.
func (b *SubagentBarrier) onTimeout() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.pending <= 0 {
		return // Already finalized
	}

	log.Printf("[barrier] Timeout! %d/%d subagents still pending", b.pending, len(b.results))
	b.pending = 0 // Force finalize
	b.finalize()
}

// formatResults builds a structured summary of all subagent results.
// Uses EphSubAgentResults as authoritative header so parent LLM trusts the results.
// Caller must hold b.mu.
func (b *SubagentBarrier) formatResults() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n", prompttext.EphSubAgentResults))

	for _, r := range b.results {
		name := r.TaskName
		if name == "" {
			name = r.RunID
		}
		if r.Error != nil {
			sb.WriteString(fmt.Sprintf("### ❌ %s (failed)\nError: %v\n", name, r.Error))
		} else if r.Output == "" && r.DoneAt.IsZero() {
			sb.WriteString(fmt.Sprintf("### ⏰ %s (timed out)\nNo result received.\n", name))
		} else {
			// Extract plain text from StructuredResult JSON if present
			output := extractText(r.Output)
			sb.WriteString(fmt.Sprintf("### ✅ %s\n%s\n", name, output))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// extractText tries to parse StructuredResult JSON and extract the .text field.
// Falls back to the original string if parsing fails.
func extractText(s string) string {
	if len(s) == 0 || s[0] != '{' {
		return s
	}
	type structured struct {
		Text string `json:"text"`
	}
	var parsed structured
	if err := json.Unmarshal([]byte(s), &parsed); err == nil && parsed.Text != "" {
		return parsed.Text
	}
	return s
}

// Pending returns the number of subagents still running.
func (b *SubagentBarrier) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending
}
