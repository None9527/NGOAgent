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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
func (c *wsConn) wsWriter(payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		log.Printf("[ws] write failed: %v", err)
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
	Type       string `json:"type"`
	Message    string `json:"message,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Mode       string `json:"mode,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	Approved   bool   `json:"approved,omitempty"`
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
		log.Printf("[ws] accept failed: %v", err)
		return
	}

	c.SetReadLimit(4 * 1024 * 1024) // 4MB

	ctx, cancel := context.WithCancel(r.Context())
	ws := &wsConn{
		conn:   c,
		cancel: cancel,
	}

	log.Printf("[ws] New connection from %s", r.RemoteAddr)

	ws.readLoop(ctx, s)

	// Cleanup
	if ws.sessionID != "" {
		s.wsTracker.Delete(ws.sessionID)
		log.Printf("[ws] Connection closed for session %s", ws.sessionID)
	}
	ws.closeConn()
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
			log.Printf("[ws] read error: %v", err)
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
				s.api.StopRun(sid)
			}
		case "approve":
			if msg.ApprovalID != "" {
				if err := s.api.Approve(msg.ApprovalID, msg.Approved); err != nil {
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
	if msg.Message == "" {
		ws.writeJSON(map[string]string{"type": "error", "message": "empty message"})
		return
	}

	sessionID := s.api.SessionID(msg.SessionID)
	ws.sessionID = sessionID

	// Register for async push (subagent_progress, title_updated, etc.)
	s.wsTracker.Store(sessionID, ws)

	// Slash command interception
	if strings.HasPrefix(msg.Message, "/") {
		firstWord := strings.Fields(msg.Message)[0]
		knownCmds := map[string]bool{
			"/model": true, "/models": true, "/set": true, "/forge": true,
			"/plan": true, "/status": true, "/help": true, "/skill": true,
			"/clear": true, "/compact": true, "/cron": true,
		}
		if knownCmds[firstWord] {
			result := s.execSlash(msg.Message)
			ws.writeJSON(map[string]string{"type": "slash_result", "result": result})
			return
		}
	}

	// Create Delta with WS writer — NO MarkDone, NO RunTracker.
	// BufferedDelta is reused here only for its emit() serialization,
	// NOT for SSE reconnect buffering. Writer lifetime = WS lifetime.
	buf := service.NewBufferedDelta(ws.wsWriter)
	delta := buf.MakeDelta()

	// DO NOT register in RunTracker — WS doesn't need SSE reconnect.
	// DO NOT call buf.MarkDone() — writer must stay alive for auto-wake.

	go func() {
		err := s.api.ChatStream(context.Background(), sessionID, msg.Message, msg.Mode, delta)
		if err != nil {
			if err.Error() == "agent is busy" {
				ws.writeJSON(map[string]string{"type": "error", "message": "agent is busy"})
			} else {
				log.Printf("[ws] chat error for session %s: %v", sessionID, err)
			}
		}
		// Signal frontend: this chat turn is done. Input can be re-enabled.
		// NOTE: auto-wake may send more events later through the same wsWriter.
		ws.writeJSON(map[string]string{"type": "done"})
	}()
}
