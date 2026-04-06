// Package notify provides outbound webhook notifications for agent lifecycle events.
// P3 M1: WebhookNotifier wraps a DeltaSink and fans out selected events to
// configured HTTP endpoints asynchronously (fire-and-forget with retry).
//
// Config (config.yaml):
//
//	notifications:
//	  webhooks:
//	    - url: https://hooks.example.com/notify
//	      events: [complete, error, tool_result]  # optional filter
//	      secret: "hmac-secret"                   # optional; adds X-Hub-Signature-256
//	      retry: 2                                # optional; default 1
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// WebhookEvent is the payload sent to webhook endpoints.
type WebhookEvent struct {
	Type      string         `json:"type"` // complete | error | tool_result | progress
	Timestamp time.Time      `json:"ts"`
	SessionID string         `json:"session_id,omitempty"`
	Message   string         `json:"message,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Output    string         `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// WebhookTarget is one configured destination.
type WebhookTarget struct {
	URL    string   // required
	Events []string // nil/empty = all events; otherwise filter list
	Secret string   // optional HMAC-SHA256 signing key
	Retry  int      // number of extra retries (0 = try once)
}

// WebhookNotifier sends agent events to one or more HTTP endpoints.
// It is safe for concurrent use; deliveries are fire-and-forget.
type WebhookNotifier struct {
	targets   []*WebhookTarget
	sessionID string
	client    *http.Client
	queue     chan WebhookEvent
	stop      chan struct{}
}

// NewWebhookNotifier creates a notifier and starts its delivery goroutine.
// Call Close() when the session ends to flush the queue.
func NewWebhookNotifier(targets []*WebhookTarget, sessionID string) *WebhookNotifier {
	n := &WebhookNotifier{
		targets:   targets,
		sessionID: sessionID,
		client:    &http.Client{Timeout: 8 * time.Second},
		queue:     make(chan WebhookEvent, 128),
		stop:      make(chan struct{}),
	}
	go n.deliver()
	return n
}

// Emit queues an event for delivery. Non-blocking.
func (n *WebhookNotifier) Emit(evt WebhookEvent) {
	evt.SessionID = n.sessionID
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	select {
	case n.queue <- evt:
	default:
		slog.Info(fmt.Sprintf("[webhook] queue full — dropping event %s", evt.Type))
	}
}

// Close signals the delivery goroutine to flush and stop.
func (n *WebhookNotifier) Close() {
	close(n.stop)
}

// deliver runs the async delivery loop.
func (n *WebhookNotifier) deliver() {
	for {
		select {
		case evt := <-n.queue:
			n.fan(evt)
		case <-n.stop:
			// Drain remaining events with a 3s deadline
			deadline := time.After(3 * time.Second)
		drain:
			for {
				select {
				case evt := <-n.queue:
					n.fan(evt)
				case <-deadline:
					break drain
				}
			}
			return
		}
	}
}

// fan delivers an event to all matching targets concurrently.
func (n *WebhookNotifier) fan(evt WebhookEvent) {
	for _, t := range n.targets {
		if !n.wantEvent(t, evt.Type) {
			continue
		}
		go n.post(t, evt) // async per-target
	}
}

// wantEvent returns true if the target's event filter includes evtType.
func (n *WebhookNotifier) wantEvent(t *WebhookTarget, evtType string) bool {
	if len(t.Events) == 0 {
		return true
	}
	for _, e := range t.Events {
		if e == evtType || e == "*" {
			return true
		}
	}
	return false
}

// post sends a single event to one target with optional retry.
func (n *WebhookNotifier) post(t *WebhookTarget, evt WebhookEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		slog.Info(fmt.Sprintf("[webhook] marshal error: %v", err))
		return
	}

	maxAttempts := 1 + t.Retry
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second) // backoff: 2s, 4s
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, t.URL, bytes.NewReader(data))
		if err != nil {
			slog.Info(fmt.Sprintf("[webhook] build request error: %v", err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "NGOAgent-Webhook/1.0")
		req.Header.Set("X-Agent-Event", evt.Type)

		// Optional HMAC signature
		if t.Secret != "" {
			mac := hmac.New(sha256.New, []byte(t.Secret))
			mac.Write(data)
			sig := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Hub-Signature-256", "sha256="+sig)
		}

		resp, err := n.client.Do(req)
		if err != nil {
			slog.Info(fmt.Sprintf("[webhook] POST %s attempt %d error: %v", t.URL, attempt+1, err))
			continue
		}
		resp.Body.Close()

		if resp.StatusCode < 300 {
			return // success
		}
		slog.Info(fmt.Sprintf("[webhook] POST %s attempt %d → HTTP %d", t.URL, attempt+1, resp.StatusCode))
	}
}

// ═══════════════════════════════════════════
// NotifyAdapter: wires WebhookNotifier into
// agent events via simple fn hooks (no DeltaSink
// interface required — avoids import cycle).
// ═══════════════════════════════════════════

// Hook is a set of agent event callbacks the WebhookNotifier attaches to.
// Caller registers these in tool_exec.go / run.go hooks.
type Hook struct {
	notifier *WebhookNotifier
}

// NewHook creates a hook adapter wrapping a WebhookNotifier.
func NewHook(n *WebhookNotifier) *Hook { return &Hook{notifier: n} }

// OnComplete fires on agent turn completion.
func (h *Hook) OnComplete(sessionID string) {
	h.notifier.Emit(WebhookEvent{
		Type:      "complete",
		SessionID: sessionID,
		Message:   "Agent turn completed",
	})
}

// OnError fires on agent turn error.
func (h *Hook) OnError(sessionID string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	h.notifier.Emit(WebhookEvent{
		Type:      "error",
		SessionID: sessionID,
		Error:     errStr,
	})
}

// OnToolResult fires after tool execution.
func (h *Hook) OnToolResult(sessionID, toolName, output string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	// Only emit for expensive/side-effectful tools to avoid spam
	sideEffectTools := map[string]bool{
		"write_file": true, "edit_file": true, "run_command": true,
		"git_commit": true, "spawn_agent": true, "manage_cron": true,
	}
	if !sideEffectTools[toolName] {
		return
	}
	h.notifier.Emit(WebhookEvent{
		Type:      "tool_result",
		SessionID: sessionID,
		ToolName:  toolName,
		Output:    truncate(output, 512),
		Error:     errStr,
	})
}

// OnProgress fires on task progress updates.
func (h *Hook) OnProgress(sessionID, taskName, status, summary string) {
	h.notifier.Emit(WebhookEvent{
		Type:      "progress",
		SessionID: sessionID,
		ToolName:  taskName, // reuse field for task name
		Message:   fmt.Sprintf("[%s] %s — %s", status, taskName, summary),
	})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
