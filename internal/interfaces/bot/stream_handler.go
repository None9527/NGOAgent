package bot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// StreamHandler provides an HTTP-based client for the application transport.
// It handles SSE parsing, throttled text accumulation, and event routing.
// Platform bots consume events via the OnEvent callback.
type StreamHandler struct {
	httpBase string
	token    string
	client   *http.Client
}

// NewStreamHandler creates a StreamHandler targeting the given base URL.
func NewStreamHandler(httpBase, token string) *StreamHandler {
	return &StreamHandler{
		httpBase: strings.TrimRight(httpBase, "/"),
		token:    token,
		client:   &http.Client{Timeout: 0},
	}
}

// SSEEvent represents a parsed SSE event from the agent.
type SSEEvent struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	Message    string `json:"message,omitempty"`
	CallID     string `json:"call_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Name       string `json:"name,omitempty"`
	Args       string `json:"args,omitempty"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	Reason     string `json:"reason,omitempty"`
	TaskName   string `json:"task_name,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Title      string `json:"title,omitempty"`
}

// ChatRequest holds params for a streaming chat call.
type ChatRequest struct {
	SessionID string
	Message   string
}

// ChatResult is the accumulated result of a streaming chat.
type ChatResult struct {
	Text   string     // Final accumulated text
	Events []SSEEvent // All events received
}

// ChatSSE sends a message and returns a channel of SSE events.
// The caller consumes events and handles platform-specific rendering.
func (h *StreamHandler) ChatSSE(ctx context.Context, req ChatRequest) (<-chan SSEEvent, error) {
	body, _ := json.Marshal(map[string]any{
		"message":    req.Message,
		"session_id": req.SessionID,
		"stream":     true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.httpBase+"/v1/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+h.token)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var event SSEEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// ChatSync sends a message and blocks until the response is complete.
// Returns the accumulated text and all events. Use when you don't need
// streaming (e.g., simple bot integrations).
func (h *StreamHandler) ChatSync(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	events, err := h.ChatSSE(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &ChatResult{}
	var textBuf strings.Builder
	for event := range events {
		result.Events = append(result.Events, event)
		if event.Type == "text_delta" {
			textBuf.WriteString(event.Content)
		}
		if event.Type == "error" {
			msg := event.Message
			if msg == "" {
				msg = event.Error
			}
			textBuf.WriteString(fmt.Sprintf("\n\n❌ %s", msg))
		}
	}
	result.Text = textBuf.String()
	return result, nil
}

// StreamToTelegram is a convenience method that handles the full
// Telegram streaming flow: placeholder → progressive edit → final.
func (h *StreamHandler) StreamToTelegram(
	ctx context.Context,
	sessionID, message string,
	sendMsg func(text string) (msgID int, err error),
	editMsg func(msgID int, text string),
) {
	// Send placeholder
	msgID, err := sendMsg("⏳ 思考中...")
	if err != nil {
		return
	}

	events, err := h.ChatSSE(ctx, ChatRequest{SessionID: sessionID, Message: message})
	if err != nil {
		editMsg(msgID, fmt.Sprintf("❌ 连接失败: %v", err))
		return
	}

	var (
		textBuf     strings.Builder
		lastEditLen int
		lastEditAt  time.Time
	)

	flush := func(final bool) {
		current := textBuf.String()
		if !final && len(current)-lastEditLen < 20 &&
			time.Since(lastEditAt) < 300*time.Millisecond {
			return
		}
		lastEditLen = len(current)
		lastEditAt = time.Now()
		display := current
		if !final {
			display += " ▌"
		}
		if len(display) > 4000 {
			display = display[:4000] + "\n…(截断)"
		}
		editMsg(msgID, display)
	}

	for event := range events {
		switch event.Type {
		case "text_delta":
			textBuf.WriteString(event.Content)
			flush(false)
		case "error":
			msg := event.Message
			if msg == "" {
				msg = event.Error
			}
			textBuf.WriteString(fmt.Sprintf("\n\n❌ %s", msg))
		case "tool_start":
			slog.Info(fmt.Sprintf("[bot-stream] tool_start: %s", event.Name))
		case "approval_request":
			slog.Info(fmt.Sprintf("[bot-stream] approval: %s", event.ApprovalID))
		}
	}

	finalText := textBuf.String()
	if finalText == "" {
		finalText = "✅ 完成"
	}
	flush(true)
}

// Stop stops a running agent session.
func (h *StreamHandler) Stop(sessionID string) error {
	body, _ := json.Marshal(map[string]string{"session_id": sessionID})
	return h.post("/v1/stop", body)
}

// Approve resolves a pending tool approval.
func (h *StreamHandler) Approve(approvalID string, approved bool) error {
	body, _ := json.Marshal(map[string]any{
		"approval_id": approvalID,
		"approved":    approved,
	})
	return h.post("/v1/approve", body)
}

// NewSession creates a new conversation session.
func (h *StreamHandler) NewSession(title string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title})
	req, err := http.NewRequest(http.MethodPost, h.httpBase+"/v1/session/new", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.SessionID, nil
}

func (h *StreamHandler) post(path string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, h.httpBase+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
