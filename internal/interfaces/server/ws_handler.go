// Package server — ws_handler.go implements WebSocket session-level connections.
//
// Core design: WS writer lifetime = WS connection lifetime.
// Unlike SSE (BufferedDelta per request), WS uses a persistent Delta
// that writes directly through wsWriter. No MarkDone, no RunTracker.
// This allows auto-wake events to flow through the same WS connection
// after the initial chat turn completes.
package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"nhooyr.io/websocket"
)

// wsConn wraps a single WebSocket connection bound to a session.
type wsConn struct {
	conn      *websocket.Conn
	sessionID string // set on first chat message
	mu        sync.Mutex
	closed    bool
	cancel    context.CancelFunc
}

// wsWriter implements the SSEWriter func signature.
// Writes a raw JSON payload as a WebSocket text message.
// Lifetime: same as the WS connection — never nil'd or MarkDone'd.
// P0: recover() protects against panics when subagents concurrently push
// events to a broken connection — prevents process crash.
func (c *wsConn) wsWriter(payload []byte) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Info(fmt.Sprintf("[ws] write panic recovered: %v", r))
			ok = false
		}
	}()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		slog.Info(fmt.Sprintf("[ws] write failed: %v", err))
		c.closed = true // Mark closed to fast-fail subsequent writes
		return false
	}
	return true
}

// writeJSON marshals and sends a JSON message to the client.
func (c *wsConn) writeJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.wsWriter(data)
}

// pushEvent sends a custom event to this WS connection (used by PushEvent).
func (c *wsConn) pushEvent(eventType string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_ = c.wsWriter(payload)
	_ = eventType // included in the data payload already
}

// closeConn terminates the WebSocket connection.
func (c *wsConn) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.conn.Close(websocket.StatusNormalClosure, "bye")
}

// ─── Upstream message types ───

type wsUpstream struct {
	Type         string          `json:"type"`
	Message      string          `json:"message,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	ApprovalID   string          `json:"approval_id,omitempty"`
	Approved     bool            `json:"approved,omitempty"`
	ContentParts []wsContentPart `json:"content_parts,omitempty"` // B2: native multimodal
}

// wsContentPart is a structured attachment in the WS protocol.
type wsContentPart struct {
	Type     string `json:"type"`           // "image" | "audio" | "video" | "file"
	Path     string `json:"path,omitempty"` // server path (from prior upload)
	MimeType string `json:"mime_type,omitempty"`
	Name     string `json:"name,omitempty"`
	Data     string `json:"data,omitempty"` // base64 (inline, for small files)
}

// handleWS upgrades an HTTP request to a WebSocket connection.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != s.authToken {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] accept failed: %v", err))
		return
	}

	c.SetReadLimit(4 * 1024 * 1024) // 4MB

	ctx, cancel := context.WithCancel(r.Context())
	ws := &wsConn{
		conn:   c,
		cancel: cancel,
	}

	// Active heartbeat to prevent zombie connections & proxy timeouts
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := c.Ping(pingCtx)
				pingCancel()
				if err != nil {
					slog.Info(fmt.Sprintf("[ws] ping failed, closing connection: %v", err))
					ws.closeConn()
					cancel()
					return
				}
			}
		}
	}()

	slog.Info(fmt.Sprintf("[ws] New connection from %s", r.RemoteAddr))

	ws.readLoop(ctx, s)

	// Cleanup — D5 fix: close connection first (fast-fail writes), then clean tracker
	ws.closeConn()
	if ws.sessionID != "" {
		s.wsTracker.Delete(ws.sessionID)
		slog.Info(fmt.Sprintf("[ws] Connection closed for session %s", ws.sessionID))
	}
	cancel()
}

// readLoop continuously reads and dispatches upstream messages.
func (ws *wsConn) readLoop(ctx context.Context, s *Server) {
	for {
		_, data, err := ws.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure || ctx.Err() != nil {
				return
			}
			slog.Info(fmt.Sprintf("[ws] read error: %v", err))
			return
		}

		var msg wsUpstream
		if err := json.Unmarshal(data, &msg); err != nil {
			ws.writeJSON(map[string]string{"type": "error", "message": "invalid JSON"})
			continue
		}

		switch msg.Type {
		case "chat":
			ws.onChat(s, msg)
		case "stop":
			sid := msg.SessionID
			if sid == "" {
				sid = ws.sessionID
			}
			if sid != "" {
				s.chat.StopRun(sid)
			}
		case "approve":
			if msg.ApprovalID != "" {
				if err := s.chat.Approve(msg.ApprovalID, msg.Approved); err != nil {
					ws.writeJSON(map[string]string{"type": "error", "message": err.Error()})
				}
			}
		case "ping":
			ws.writeJSON(map[string]string{"type": "pong"})
		default:
			ws.writeJSON(map[string]string{"type": "error", "message": fmt.Sprintf("unknown type: %s", msg.Type)})
		}
	}
}

// onChat handles a chat message from the WebSocket client.
//
// Key design: uses a "thin" BufferedDelta without MarkDone.
// The wsWriter stays valid for the entire WS connection lifetime,
// so auto-wake (barrier callback → loopPool.Run) can reuse the same
// Delta/BufferedDelta → wsWriter path to push events to the frontend.
func (ws *wsConn) onChat(s *Server, msg wsUpstream) {
	// B2: If content_parts are present, process inline attachments
	message := msg.Message
	if len(msg.ContentParts) > 0 {
		message = s.processContentParts(msg)
	}

	if message == "" {
		ws.writeJSON(map[string]string{"type": "error", "message": "empty message"})
		return
	}

	// P0 fix: use frontend session_id directly — do NOT resolve through SessionID()
	// which falls back to the default loop's startup UUID (ghost session bug).
	// ChatStream's loopPool.Get() will auto-create a loop for new sessions.
	sessionID := msg.SessionID
	if sessionID == "" {
		slog.Warn("[ws] chat received without session_id, rejecting")
		ws.writeJSON(map[string]string{"type": "error", "message": "session_id required"})
		return
	}
	ws.sessionID = sessionID

	// Register for async push (subagent_progress, title_updated, etc.)
	s.wsTracker.Store(sessionID, ws)

	// Slash command interception
	if strings.HasPrefix(message, "/") {
		firstWord := strings.Fields(message)[0]
		knownCmds := map[string]bool{
			"/model": true, "/models": true, "/set": true, "/evo": true,
			"/plan": true, "/status": true, "/help": true, "/skill": true,
			"/clear": true, "/compact": true, "/cron": true,
		}
		if knownCmds[firstWord] {
			result := s.execSlash(message)
			ws.writeJSON(map[string]string{"type": "slash_result", "result": result})
			return
		}
	}

	// Create Delta with WS writer.
	// D3 fix: Also register in RunTracker so WS disconnections can be recovered
	// via SSE reconnect (checkActiveRun + handleReconnect path).
	buf := service.NewBufferedDelta(ws.wsWriter)
	delta := buf.MakeDelta()

	// D3 fix: Register in RunTracker — enables SSE reconnect after WS drop
	s.runTracker.Register(sessionID, buf)

	go func() {
		slog.Info(fmt.Sprintf("[ws] ChatStream: session=%s mode=%q msgLen=%d", sessionID, msg.Mode, len(message)))
		err := s.chat.ChatStream(context.Background(), sessionID, message, msg.Mode, delta)
		if err != nil {
			if err.Error() == "agent is busy" {
				ws.writeJSON(map[string]string{"type": "error", "message": "agent is busy"})
			} else {
				slog.Info(fmt.Sprintf("[ws] chat error for session %s: %v", sessionID, err))
				// D2 fix: Forward all errors to frontend — don't silently swallow 503/context overflow
				ws.writeJSON(map[string]string{"type": "error", "message": err.Error()})
			}
		}
		// Signal frontend: this chat turn is done. Input can be re-enabled.
		// NOTE: auto-wake may send more events later through the same wsWriter.
		ws.writeJSON(map[string]string{"type": "done"})
		// D3+D4 fix: Mark the run as complete so RunTracker can clean up after 30min
		buf.MarkDone()
	}()
}

// processContentParts converts native content_parts to the XML format
// that buildUserMessage expects. Inline base64 data is saved to disk first.
func (s *Server) processContentParts(msg wsUpstream) string {
	var fileEntries []string
	for _, p := range msg.ContentParts {
		path := p.Path
		// Inline base64 → save to disk
		if path == "" && p.Data != "" {
			saved, err := s.saveInlineData(p)
			if err != nil {
				slog.Info(fmt.Sprintf("[ws] failed to save inline data: %v", err))
				continue
			}
			path = saved
		}
		if path == "" {
			continue
		}
		role := "reference_file"
		switch p.Type {
		case "image":
			role = "reference_image"
		case "audio":
			role = "reference_audio"
		case "video":
			role = "reference_video"
		}
		name := p.Name
		if name == "" {
			name = filepath.Base(path)
		}
		fileEntries = append(fileEntries, fmt.Sprintf(
			`  <file name="%s" path="%s" type="%s" role="%s" />`,
			name, path, p.MimeType, role,
		))
	}
	text := msg.Message
	if len(fileEntries) > 0 {
		xml := "<user_attachments>\n" + strings.Join(fileEntries, "\n") + "\n</user_attachments>"
		text = xml + "\n\n" + text
	}
	return text
}

// saveInlineData decodes base64 data from a content_part and saves to uploads/.
func (s *Server) saveInlineData(p wsContentPart) (string, error) {
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	c := s.admin.GetConfig()
	agent, _ := c["agent"].(map[string]any)
	workspace, _ := agent["workspace"].(string)
	if workspace == "" {
		workspace = os.TempDir()
	}
	uploadDir := filepath.Join(workspace, "uploads")
	os.MkdirAll(uploadDir, 0755)

	ext := ".bin"
	if p.Name != "" {
		ext = filepath.Ext(p.Name)
	} else if p.MimeType != "" {
		// mime → ext guess
		switch {
		case strings.HasPrefix(p.MimeType, "image/png"):
			ext = ".png"
		case strings.HasPrefix(p.MimeType, "image/jpeg"):
			ext = ".jpg"
		case strings.HasPrefix(p.MimeType, "image/webp"):
			ext = ".webp"
		case strings.HasPrefix(p.MimeType, "audio/"):
			ext = ".mp3"
		}
	}
	filename := fmt.Sprintf("%d_inline%s", time.Now().UnixMilli(), ext)
	dstPath := filepath.Join(uploadDir, filename)
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return "", err
	}
	slog.Info(fmt.Sprintf("[ws] saved inline %s → %s (%d bytes)", p.Type, dstPath, len(data)))
	return dstPath, nil
}
