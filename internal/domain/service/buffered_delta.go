package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ════════════════════════════════════════════
// BufferedDelta — event buffer for SSE reconnect
// ════════════════════════════════════════════

// BufferedEvent is a serialized delta event that can be replayed.
type BufferedEvent struct {
	SeqID   int             `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// SSEWriter is the callback that sends raw SSE data to the HTTP client.
// Returns false if the write failed (client disconnected).
type SSEWriter func(payload []byte) bool

// BufferedDelta buffers delta events and supports live-attach/detach for SSE reconnect.
// When a live SSE writer is attached, events are forwarded in real-time.
// When detached (client disconnected), events accumulate in an internal buffer.
type BufferedDelta struct {
	mu       sync.Mutex
	events   []BufferedEvent
	seqID    int
	writer   SSEWriter     // nil = detached (buffering mode)
	done     bool          // true after OnComplete/OnError
	expireAt time.Time     // auto-cleanup deadline after done
	doneCh   chan struct{} // closed when done=true, for select-based waiters

	// Text throttling: merge high-frequency token deltas into 50ms batches
	throttleMu    sync.Mutex
	throttleBuf   strings.Builder
	throttleTimer *time.Timer
}

// NewBufferedDelta creates a buffer, optionally with an initial live writer.
func NewBufferedDelta(writer SSEWriter) *BufferedDelta {
	return &BufferedDelta{
		writer: writer,
		events: make([]BufferedEvent, 0, 128),
		doneCh: make(chan struct{}),
	}
}

// emit records and optionally forwards an event.
func (bd *BufferedDelta) emit(eventType string, data any) {
	payload, _ := json.Marshal(data)

	bd.mu.Lock()
	defer bd.mu.Unlock()

	bd.seqID++
	ev := BufferedEvent{
		SeqID:   bd.seqID,
		Type:    eventType,
		Payload: payload,
	}
	bd.events = append(bd.events, ev)

	// Cap buffer at 5000 events to prevent memory leak on very long runs
	if len(bd.events) > 5000 {
		bd.events = bd.events[len(bd.events)-5000:]
	}

	if bd.writer != nil {
		if !bd.writer(payload) {
			// Write failed → client disconnected, switch to buffer mode
			slog.Info(fmt.Sprintf("[delta] SSE write failed, detaching (seq=%d)", bd.seqID))
			bd.writer = nil
		}
	}
}

// Attach connects a live SSE writer and replays buffered events since lastSeqID.
// Returns the number of events replayed.
func (bd *BufferedDelta) Attach(writer SSEWriter, lastSeqID int) int {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	replayed := 0
	for _, ev := range bd.events {
		if ev.SeqID <= lastSeqID {
			continue
		}
		if !writer(ev.Payload) {
			return replayed // Client died during replay
		}
		replayed++
	}

	bd.writer = writer
	return replayed
}

// Detach disconnects the current SSE writer, switching to buffer-only mode.
func (bd *BufferedDelta) Detach() {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.writer = nil
}

// MarkDone marks this run as complete and sets an expiry for cleanup.
func (bd *BufferedDelta) MarkDone() {
	bd.mu.Lock()
	alreadyDone := bd.done
	bd.done = true
	bd.expireAt = time.Now().Add(30 * time.Minute)
	bd.mu.Unlock()
	// Close doneCh exactly once to unblock WaitDone() waiters
	if !alreadyDone {
		close(bd.doneCh)
	}
}

// ResetDone re-opens the run for a new round (evo repair, auto-wake continuation).
// Creates a new doneCh so SSE/WS handlers can re-attach and block on it.
func (bd *BufferedDelta) ResetDone() {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	if bd.done {
		bd.done = false
		bd.doneCh = make(chan struct{})
		bd.expireAt = time.Time{}
	}
}

// Done returns a channel that is closed when MarkDone is called.
// Allows callers to select-wait for run completion.
func (bd *BufferedDelta) Done() <-chan struct{} {
	return bd.doneCh
}

// IsDone returns whether the run has completed.
func (bd *BufferedDelta) IsDone() bool {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return bd.done
}

// IsExpired returns true if the buffer has expired and should be cleaned up.
func (bd *BufferedDelta) IsExpired() bool {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return bd.done && !bd.expireAt.IsZero() && time.Now().After(bd.expireAt)
}

// LastSeqID returns the latest sequence ID.
func (bd *BufferedDelta) LastSeqID() int {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return bd.seqID
}

// EmitDirect pushes a custom event into the SSE stream.
// Used by external components (e.g., SubagentBarrier) to send progress events
// to the parent session without going through the DeltaSink interface.
func (bd *BufferedDelta) EmitDirect(eventType string, data any) {
	bd.emit(eventType, data)
}

// MakeDelta returns a *Delta that routes all callbacks through this BufferedDelta.
// The server creates this once and binds it to the AgentLoop.
// flushThrottledText emits any buffered text delta as a single merged event.
// Called by the throttle timer and on complete/error to ensure no data is lost.
func (bd *BufferedDelta) flushThrottledText() {
	bd.throttleMu.Lock()
	if bd.throttleBuf.Len() == 0 {
		bd.throttleMu.Unlock()
		return
	}
	text := bd.throttleBuf.String()
	bd.throttleBuf.Reset()
	if bd.throttleTimer != nil {
		bd.throttleTimer.Stop()
		bd.throttleTimer = nil
	}
	bd.throttleMu.Unlock()

	bd.emit("text_delta", map[string]string{"type": "text_delta", "content": text})
}

// MakeDelta returns a *Delta that routes all callbacks through this BufferedDelta.
// The server creates this once and binds it to the AgentLoop.
// Text deltas are throttled at 50ms intervals to reduce SSE event frequency.
func (bd *BufferedDelta) MakeDelta() *Delta {
	return &Delta{
		OnTextFunc: func(text string) {
			bd.throttleMu.Lock()
			bd.throttleBuf.WriteString(text)
			if bd.throttleTimer == nil {
				bd.throttleTimer = time.AfterFunc(50*time.Millisecond, func() {
					bd.flushThrottledText()
				})
			}
			bd.throttleMu.Unlock()
		},
		OnReasoningFunc: func(text string) {
			bd.emit("thinking", map[string]string{"type": "thinking", "content": text})
		},
		OnToolStartFunc: func(callID, name string, args map[string]any) {
			bd.flushThrottledText() // flush text before tool event
			bd.emit("tool_start", map[string]any{"type": "tool_start", "call_id": callID, "name": name, "args": args})
		},
		OnToolResultFunc: func(callID, name, output string, err error) {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			bd.emit("tool_result", map[string]any{"type": "tool_result", "call_id": callID, "name": name, "output": output, "error": errStr})
		},
		OnProgressFunc: func(taskName, status, summary, mode string) {
			bd.emit("progress", map[string]any{"type": "progress", "task_name": taskName, "status": status, "summary": summary, "mode": mode})
		},
		OnPlanReviewFunc: func(message string, paths []string) {
			bd.emit("plan_review", map[string]any{"type": "plan_review", "message": message, "paths": paths})
		},
		OnApprovalRequestFunc: func(approvalID, toolName string, args map[string]any, reason string) {
			bd.emit("approval_request", map[string]any{"type": "approval_request", "approval_id": approvalID, "tool_name": toolName, "args": args, "reason": reason})
		},
		OnTitleUpdateFunc: func(sessionID, title string) {
			bd.emit("title_updated", map[string]string{"type": "title_updated", "session_id": sessionID, "title": title})
		},
		OnAutoWakeStartFunc: func() {
			bd.ResetDone() // Re-open stream: SSE/WS handlers will re-attach
			bd.flushThrottledText()
			bd.emit("auto_wake_start", map[string]string{"type": "auto_wake_start"})
		},
		OnCompleteFunc: func() {
			bd.flushThrottledText() // flush remaining text before done signal
			bd.emit("step_done", map[string]string{"type": "step_done"})
			bd.MarkDone()
		},
		OnErrorFunc: func(err error) {
			bd.flushThrottledText() // flush remaining text before error
			bd.emit("error", map[string]string{"type": "error", "message": err.Error()})
			bd.MarkDone()
		},
		EmitFunc: func(event DeltaEvent) {
			bd.flushThrottledText()
			switch event.Type {
			case DeltaEvoEval:
				bd.emit("evo_eval", map[string]any{"type": "evo_eval", "text": event.Text})
			case DeltaEvoRepair:
				bd.emit("evo_repair", map[string]any{"type": "evo_repair", "text": event.Text})
			default:
				bd.emit("generic_event", map[string]any{"type": "generic_event", "delta_type": event.Type, "text": event.Text})
			}
		},
	}
}

// ════════════════════════════════════════════
// RunTracker — tracks active agent runs for reconnect
// ════════════════════════════════════════════

// TrackedRun holds state for a single active agent run.
type TrackedRun struct {
	SessionID string
	Buffer    *BufferedDelta
	StartedAt time.Time
}

// RunTracker manages active runs indexed by session ID.
type RunTracker struct {
	mu   sync.Mutex
	runs map[string]*TrackedRun
}

// NewRunTracker creates a tracker and starts a background cleanup goroutine.
func NewRunTracker() *RunTracker {
	rt := &RunTracker{
		runs: make(map[string]*TrackedRun),
	}
	go rt.cleanupLoop()
	return rt
}

// Register adds a new tracked run. Replaces any existing run for the same session.
func (rt *RunTracker) Register(sessionID string, buf *BufferedDelta) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.runs[sessionID] = &TrackedRun{
		SessionID: sessionID,
		Buffer:    buf,
		StartedAt: time.Now(),
	}
}

// Get returns the tracked run for a session, if any.
func (rt *RunTracker) Get(sessionID string) (*TrackedRun, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	r, ok := rt.runs[sessionID]
	return r, ok
}

// Remove deletes a tracked run.
func (rt *RunTracker) Remove(sessionID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.runs, sessionID)
}

// cleanupLoop periodically removes expired runs.
func (rt *RunTracker) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rt.mu.Lock()
		for sid, run := range rt.runs {
			if run.Buffer.IsExpired() {
				slog.Info(fmt.Sprintf("[loop-pool] Cleaning up expired run for session %s", sid))
				delete(rt.runs, sid)
			}
		}
		rt.mu.Unlock()
	}
}
