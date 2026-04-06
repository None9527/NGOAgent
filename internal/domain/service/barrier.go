// Package service — barrier.go implements the SubagentBarrier for
// coordinating multiple parallel subagent completions.
//
// When a parent agent spawns N subagents, the barrier tracks them and
// triggers a parent-loop auto-wake once ALL have completed.
//
// Flow:
//
//	parent spawns A, B, C → barrier.pending = 3
//	C completes → pending = 2, progress pushed
//	A completes → pending = 1, progress pushed
//	B completes → pending = 0 → InjectEphemeral(allResults) → autoWake()
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
)

// SubagentBarrier coordinates parallel subagent completion for a parent loop.
// Thread-safe: all methods are safe for concurrent calls from multiple goroutines.
type SubagentBarrier struct {
	mu              sync.Mutex
	pending         int
	finalized       bool // P0-3: prevents double finalize after timeout
	results         map[string]subagentResult
	parentLoop      *AgentLoop
	autoWake        func()                                                                       // called when all subagents complete
	pushProgress    func(runID, taskName, status string, done, total int, errMsg, output string) // WS/SSE push (optional)
	timeout         time.Duration
	timer           *time.Timer
	maxConcurrent   int                                                     // S5: max concurrent subagents (0 = unlimited)
	saveTranscript  func(sessionID, taskName, runID, status, output string) // P2 F1: worker transcript saver (nil = disabled)
	parentSessionID string                                                  // P2 F1: parent session ID for transcript storage
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
		results:       make(map[string]subagentResult),
		parentLoop:    parentLoop,
		autoWake:      autoWake,
		timeout:       5 * time.Minute,
		maxConcurrent: 3, // S5: default limit
	}
}

// SetProgressPush registers a function for pushing per-subagent SSE progress events.
// The push func is called each time a subagent completes (before finalize).
func (b *SubagentBarrier) SetProgressPush(fn func(runID, taskName, status string, done, total int, errMsg, output string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pushProgress = fn
}

// SetMaxConcurrent overrides the default concurrency limit (0 = unlimited).
func (b *SubagentBarrier) SetMaxConcurrent(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maxConcurrent = n
}

// SetTranscriptSaver configures a function to persist worker transcripts on completion.
// The saver receives (parentSessionID, taskName, runID, status, output).
func (b *SubagentBarrier) SetTranscriptSaver(sessionID string, saver func(string, string, string, string, string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.parentSessionID = sessionID
	b.saveTranscript = saver
}

// Add registers a new subagent to track. Call BEFORE RunAsync to prevent race.
// Returns error if max concurrent limit reached (S5).
func (b *SubagentBarrier) Add(runID, taskName string) error {
	b.mu.Lock()

	// S5: enforce concurrency limit
	if b.maxConcurrent > 0 && b.pending >= b.maxConcurrent {
		b.mu.Unlock()
		return agenterr.NewBusy(fmt.Sprintf("max concurrent subagents (%d) reached", b.maxConcurrent))
	}

	b.pending++
	b.results[runID] = subagentResult{RunID: runID, TaskName: taskName}
	total := len(b.results)
	// S8: compute done inside lock
	done := total - b.pending
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
		name := taskName
		if name == "" {
			name = runID
		}
		go push(runID, name, "running", done, total, "", "")
	}
	return nil
}

// OnComplete is called when a subagent finishes. Thread-safe.
// Releases lock before calling external methods to prevent deadlock.
func (b *SubagentBarrier) OnComplete(runID, result string, err error) {
	b.mu.Lock()

	// S4: dedup — skip if already completed (prevents double finalize)
	entry := b.results[runID]
	if !entry.DoneAt.IsZero() {
		b.mu.Unlock()
		slog.Info(fmt.Sprintf("[barrier] Ignoring duplicate OnComplete for %s", runID))
		return
	}

	entry.Output = result
	entry.Error = err
	entry.DoneAt = time.Now()
	b.results[runID] = entry
	b.pending--

	// P0-3: Guard against negative pending (timeout already finalized)
	if b.finalized {
		b.mu.Unlock()
		slog.Info(fmt.Sprintf("[barrier] Ignoring late OnComplete for %s (already finalized)", runID))
		return
	}

	total := len(b.results)
	done := total - b.pending
	shouldFinalize := b.pending <= 0
	if shouldFinalize {
		b.finalized = true
	}

	slog.Info(fmt.Sprintf("[barrier] Subagent %s completed (%d/%d)", runID, done, total))

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
		push := b.pushProgress
		go push(runID, name, status, done, total, errMsg, outputPreview)
	}

	// P2 F1: Persist worker transcript to DB (non-blocking)
	if b.saveTranscript != nil {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		saver := b.saveTranscript
		sid := b.parentSessionID
		name := entry.TaskName
		output := entry.Output
		go saver(sid, name, runID, status, output)
	}

	// Prepare finalize data while still holding the lock
	var summary string
	var autoWake func()
	if shouldFinalize {
		if b.timer != nil {
			b.timer.Stop()
		}
		summary = b.formatResults()
		autoWake = b.autoWake
	}

	b.mu.Unlock() // Release lock BEFORE external calls to prevent deadlock

	if shouldFinalize {
		b.parentLoop.InjectEphemeral(summary)
		b.parentLoop.SignalWake()
		slog.Info(fmt.Sprintf("[barrier] All %d subagents complete, waking parent", total))

		if autoWake != nil {
			go autoWake()
		}
	}
}

// onTimeout fires when subagents take too long. Collects partial results.
// S1 fix: release lock BEFORE calling external methods to prevent deadlock.
func (b *SubagentBarrier) onTimeout() {
	b.mu.Lock()

	if b.pending <= 0 {
		b.mu.Unlock()
		return // Already finalized
	}

	slog.Info(fmt.Sprintf("[barrier] Timeout! %d/%d subagents still pending", b.pending, len(b.results)))
	b.pending = 0      // Force finalize
	b.finalized = true // P0-3: Mark as finalized

	// Collect data inside lock, call externals outside
	summary := b.formatResults()
	autoWake := b.autoWake
	b.mu.Unlock()

	// S1: external calls OUTSIDE lock — prevents deadlock
	b.parentLoop.InjectEphemeral(summary)
	b.parentLoop.SignalWake()
	slog.Info(fmt.Sprintf("[barrier] All %d subagents complete (timeout), waking parent", len(b.results)))

	if autoWake != nil {
		go autoWake()
	}
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
