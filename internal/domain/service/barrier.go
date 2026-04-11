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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
)

// SubagentBarrier coordinates parallel subagent completion for a parent loop.
// Thread-safe: all methods are safe for concurrent calls from multiple goroutines.
type SubagentBarrier struct {
	mu              sync.Mutex
	id              string
	pending         int
	finalized       bool // P0-3: prevents double finalize after timeout
	members         map[string]barrierMember
	parentLoop      *AgentLoop
	autoWake        func()                                                                       // called when all subagents complete
	pushProgress    func(runID, taskName, status string, done, total int, errMsg, output string) // WS/SSE push (optional)
	timeout         time.Duration
	timer           *time.Timer
	maxConcurrent   int                                                     // S5: max concurrent subagents (0 = unlimited)
	saveTranscript  func(sessionID, taskName, runID, status, output string) // P2 F1: worker transcript saver (nil = disabled)
	parentSessionID string                                                  // P2 F1: parent session ID for transcript storage
}

type barrierMember struct {
	RunID    string
	TaskName string
	Output   string
	Error    string
	Status   string
	DoneAt   time.Time
}

// NewSubagentBarrier creates a barrier for coordinating N subagent completions.
// autoWake is called (once) when the last subagent completes.
func NewSubagentBarrier(parentLoop *AgentLoop, autoWake func()) *SubagentBarrier {
	return &SubagentBarrier{
		id:            "barrier-" + uuid.New().String()[:8],
		members:       make(map[string]barrierMember),
		parentLoop:    parentLoop,
		autoWake:      autoWake,
		timeout:       5 * time.Minute,
		maxConcurrent: 3, // S5: default limit
	}
}

func NewSubagentBarrierFromState(parentLoop *AgentLoop, autoWake func(), state graphruntime.BarrierState) *SubagentBarrier {
	b := NewSubagentBarrier(parentLoop, autoWake)
	if state.ID != "" {
		b.id = state.ID
	}
	b.finalized = state.Finalized
	b.members = make(map[string]barrierMember, len(state.Members))
	completed := 0
	for _, member := range state.Members {
		b.members[member.RunID] = barrierMember{
			RunID:    member.RunID,
			TaskName: member.TaskName,
			Output:   member.Output,
			Error:    member.Error,
			Status:   member.Status,
			DoneAt:   member.DoneAt,
		}
		if !member.DoneAt.IsZero() {
			completed++
		}
	}
	if state.PendingCount > 0 {
		b.pending = state.PendingCount
	} else if pending := len(state.Members) - completed; pending > 0 {
		b.pending = pending
	}
	return b
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

// SetTimeout overrides the default barrier timeout (5 min).
// Each Add() resets the timer to this duration.
func (b *SubagentBarrier) SetTimeout(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d > 0 {
		b.timeout = d
	}
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
	b.members[runID] = barrierMember{RunID: runID, TaskName: taskName, Status: "running"}
	total := len(b.members)
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
	entry, ok := b.members[runID]
	if !ok {
		entry = barrierMember{RunID: runID, TaskName: runID, Status: "running"}
	}
	if !entry.DoneAt.IsZero() {
		b.mu.Unlock()
		slog.Info(fmt.Sprintf("[barrier] Ignoring duplicate OnComplete for %s", runID))
		return
	}

	entry.Output = result
	if err != nil {
		entry.Error = err.Error()
		entry.Status = "failed"
	} else {
		entry.Error = ""
		entry.Status = "completed"
	}
	entry.DoneAt = time.Now()
	b.members[runID] = entry
	b.pending--

	// P0-3: Guard against negative pending (timeout already finalized)
	if b.finalized {
		b.mu.Unlock()
		slog.Info(fmt.Sprintf("[barrier] Ignoring late OnComplete for %s (already finalized)", runID))
		return
	}

	total := len(b.members)
	done := total - b.pending
	shouldFinalize := b.pending <= 0
	if shouldFinalize {
		b.finalized = true
	}

	slog.Info(fmt.Sprintf("[barrier] Subagent %s completed (%d/%d)", runID, done, total))
	if b.parentLoop != nil {
		b.parentLoop.recordBarrierProgress(runID, b.id, entry.Status)
	}

	// Push per-completion progress event (non-blocking)
	if b.pushProgress != nil {
		name := entry.TaskName
		if name == "" {
			name = runID
		}
		status := "completed"
		errMsg := ""
		if entry.Error != "" {
			status = "failed"
			errMsg = entry.Error
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
		if entry.Error != "" {
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
		if b.parentLoop != nil {
			b.parentLoop.recordBarrierFinalized(b.id, fmt.Sprintf("%d/%d complete", total, total))
		}
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

	slog.Info(fmt.Sprintf("[barrier] Timeout! %d/%d subagents still pending", b.pending, len(b.members)))
	b.pending = 0      // Force finalize
	b.finalized = true // P0-3: Mark as finalized

	// Collect data inside lock, call externals outside
	summary := b.formatResults()
	autoWake := b.autoWake
	b.mu.Unlock()

	// S1: external calls OUTSIDE lock — prevents deadlock
	b.parentLoop.InjectEphemeral(summary)
	b.parentLoop.SignalWake()
	if b.parentLoop != nil {
		b.parentLoop.recordBarrierTimeout(b.id, fmt.Sprintf("timeout with %d members", len(b.members)))
	}
	slog.Info(fmt.Sprintf("[barrier] All %d subagents complete (timeout), waking parent", len(b.members)))

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

	for _, r := range b.members {
		name := r.TaskName
		if name == "" {
			name = r.RunID
		}
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("### ❌ %s (failed)\nError: %s\n", name, r.Error))
		} else if r.Output == "" && r.DoneAt.IsZero() {
			sb.WriteString(fmt.Sprintf("### ⏰ %s (timed out)\nNo result received.\n", name))
		} else {
			// Extract text + tool summary from StructuredResult JSON
			output := extractTextWithTools(r.Output)
			sb.WriteString(fmt.Sprintf("### ✅ %s\n%s\n", name, output))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// extractTextWithTools parses StructuredResult JSON and extracts text + tool summary.
// Falls back to the original string if parsing fails.
func extractTextWithTools(s string) string {
	if len(s) == 0 || s[0] != '{' {
		return s
	}
	type toolEvent struct {
		Name string `json:"name"`
	}
	type structured struct {
		Text       string      `json:"text"`
		ToolEvents []toolEvent `json:"tool_events"`
	}
	var parsed structured
	if err := json.Unmarshal([]byte(s), &parsed); err != nil || parsed.Text == "" {
		return s
	}
	result := parsed.Text
	if len(parsed.ToolEvents) > 0 {
		var names []string
		for _, ev := range parsed.ToolEvents {
			names = append(names, ev.Name)
		}
		result += fmt.Sprintf("\n[Tools used: %s]", strings.Join(names, ", "))
	}
	return result
}

// Pending returns the number of subagents still running.
func (b *SubagentBarrier) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending
}

func (b *SubagentBarrier) Snapshot() graphruntime.BarrierState {
	b.mu.Lock()
	defer b.mu.Unlock()

	members := make([]graphruntime.BarrierMemberState, 0, len(b.members))
	completed := 0
	for _, member := range b.members {
		members = append(members, graphruntime.BarrierMemberState{
			RunID:    member.RunID,
			TaskName: member.TaskName,
			Status:   member.Status,
			Output:   member.Output,
			Error:    member.Error,
			DoneAt:   member.DoneAt,
		})
		if !member.DoneAt.IsZero() {
			completed++
		}
	}
	slices.SortFunc(members, func(a, b graphruntime.BarrierMemberState) int {
		return strings.Compare(a.RunID, b.RunID)
	})
	return graphruntime.BarrierState{
		ID:             b.id,
		TotalCount:     len(b.members),
		PendingCount:   b.pending,
		CompletedCount: completed,
		Finalized:      b.finalized,
		Members:        members,
	}
}
