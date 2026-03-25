// Package server provides HTTP/SSE and slash-command routing.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

// API is the interface that the server requires from the application layer.
// application.AgentAPI satisfies this interface implicitly.
type API interface {
	// Chat — unified streaming entry point
	ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error
	SessionID(sessionID string) string
	StopRun(sessionID string)
	RetryRun(ctx context.Context, sessionID string) (string, error)
	Approve(approvalID string, approved bool) error

	// Session
	NewSession(title string) apitype.SessionResponse
	ListSessions() apitype.SessionListResponse
	SetSessionTitle(id, title string)
	DeleteSession(id string) error

	// History
	GetHistory(sessionID string) ([]apitype.HistoryMessage, error)
	ClearHistory()
	CompactContext()

	// Model
	ListModels() apitype.ModelListResponse
	SwitchModel(name string) error
	CurrentModel() string

	// Config
	GetConfig() map[string]any
	SetConfig(key string, value any) error
	AddProvider(p config.ProviderDef) error
	RemoveProvider(name string) error
	AddMCPServer(s config.MCPServerDef) error
	RemoveMCPServer(name string) error

	// Tools & Skills
	ListTools() []apitype.ToolInfoResponse
	EnableTool(name string) error
	DisableTool(name string) error
	ListSkills() (any, error)
	ReadSkillContent(name string) (string, error)
	RefreshSkills() error
	DeleteSkill(name string) error

	// MCP
	ListMCPServers() (any, error)
	ListMCPTools() (any, error)

	// Status
	Health() apitype.HealthResponse
	GetSecurity() apitype.SecurityResponse
	GetContextStats() apitype.ContextStats
	GetSystemInfo() apitype.SystemInfoResponse
	CronStatus() map[string]any

	// Cron management
	ListCronJobs() (any, error)
	CreateCronJob(name, schedule, prompt string) error
	DeleteCronJob(name string) error
	EnableCronJob(name string) error
	DisableCronJob(name string) error
	RunCronJobNow(name string) error
	ListCronLogs(jobName string) (any, error)
	ReadCronLog(jobName, logFile string) (string, error)

	// Brain artifacts
	ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error)
	ReadBrainArtifact(sessionID, name string) (string, error)

	// KI management
	ListKI() (any, error)
	GetKI(id string) (interface{}, error)
	DeleteKI(id string) error
	ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error)
	ReadKIArtifact(id, name string) (string, error)
}

// Server is the HTTP/SSE server for NGOAgent.
type Server struct {
	api        API
	addr       string
	authToken  string // Bearer token for API auth (empty = no auth)
	mu         sync.Mutex
	runTracker *service.RunTracker
	wsTracker  sync.Map // sessionID → *wsConn (active WebSocket connections)
}

// NewServer creates an HTTP server with the unified API.
func NewServer(api API, addr string, authToken string) *Server {
	return &Server{
		api:        api,
		addr:       addr,
		authToken:  authToken,
		runTracker: service.NewRunTracker(),
	}
}

// PushEvent sends a custom event to the specified session's stream.
// Checks WebSocket connections first, falls back to SSE RunTracker.
func (s *Server) PushEvent(sessionID, eventType string, data any) {
	// Try WS path first
	if ws, ok := s.wsTracker.Load(sessionID); ok {
		log.Printf("[push] %s → WS for session %s", eventType, sessionID)
		ws.(*wsConn).pushEvent(eventType, data)
		return
	}
	// Fallback to SSE RunTracker
	run, ok := s.runTracker.Get(sessionID)
	if !ok {
		log.Printf("[push] %s → NO target for session %s (no WS, no SSE)", eventType, sessionID)
		return
	}
	run.Buffer.EmitDirect(eventType, data)
}

// authMiddleware validates Bearer token on all requests except exempted paths.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exempt paths: health check (for connectivity detection)
		if r.URL.Path == "/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		// CORS preflight always allowed
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		token := ""
		if auth != "" && strings.HasPrefix(auth, "Bearer ") {
			token = auth[7:]
		}
		// Fallback: allow ?token= query param for /v1/file and /v1/ws
		if token == "" && (r.URL.Path == "/v1/file" || r.URL.Path == "/v1/ws") {
			token = r.URL.Query().Get("token")
		}
		if token != s.authToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid or missing auth token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start begins listening for HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// ─── Core SSE / Chat ───
	mux.HandleFunc("/v1/chat", s.handleChat)
	mux.HandleFunc("/v1/chat/reconnect", s.handleReconnect)
	mux.HandleFunc("/v1/chat/status", s.handleRunStatus)
	mux.HandleFunc("/v1/approve", s.handleApprove)
	mux.HandleFunc("/v1/stop", s.handleStop)
	mux.HandleFunc("/v1/retry", s.handleRetry)

	// ─── WebSocket ───
	mux.HandleFunc("/v1/ws", s.handleWS)

	// ─── Health / Models / Config (read) ───
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.Health())
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.ListModels())
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.GetConfig())
	})

	// ─── Slash commands (backward compat) ───
	mux.HandleFunc("/v1/slash/", s.handleSlash)

	// ─── Model switch ───
	mux.HandleFunc("/v1/model/switch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Model string `json:"model"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.api.SwitchModel(req.Model); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "model": req.Model})
	})

	// ─── Local file proxy (images, media) ───
	mux.HandleFunc("/v1/file", s.handleFile)

	// ─── File upload (user attaches files to chat) ───
	mux.HandleFunc("/v1/upload", s.handleUpload)

	// ─── REST API routes ───
	s.registerAPIRoutes(mux)

	// Auth middleware — always enforced (token auto-generated on first run)
	handler := s.authMiddleware(mux)
	log.Printf("Auth token enforced — all API requests require Bearer token")

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(l net.Listener) context.Context { return ctx },
	}

	log.Printf("Server listening on %s", s.addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	return srv.ListenAndServe()
}

// handleChat processes a chat message with SSE streaming response.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message   string `json:"message"`
		Stream    bool   `json:"stream"`
		SessionID string `json:"session_id"`
		Mode      string `json:"mode"` // "auto" | "plan" | "agentic"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	// Check for slash command — only intercept known commands, not file paths
	if strings.HasPrefix(req.Message, "/") {
		firstWord := strings.Fields(req.Message)[0]
		knownCmds := map[string]bool{
			"/model": true, "/models": true, "/set": true, "/forge": true,
			"/plan": true, "/status": true, "/help": true, "/skill": true,
			"/clear": true, "/compact": true, "/cron": true,
		}
		if knownCmds[firstWord] {
			result := s.execSlash(req.Message)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"result": result})
			return
		}
	}

	// SSE streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Resolve session
	req.SessionID = s.api.SessionID(req.SessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// Create SSE writer with disconnect detection
	var sseMu sync.Mutex
	disconnected := false
	sseWriter := func(payload []byte) bool {
		sseMu.Lock()
		defer sseMu.Unlock()
		if disconnected {
			return false
		}
		_, err := fmt.Fprintf(w, "data: %s\n\n", payload)
		if err != nil {
			disconnected = true
			return false
		}
		// Flush may panic if the underlying connection is already closed
		func() {
			defer func() {
				if r := recover(); r != nil {
					disconnected = true
				}
			}()
			flusher.Flush()
		}()
		if disconnected {
			return false
		}
		return true
	}

	// BufferedDelta: survives SSE disconnection, supports reconnect
	buf := service.NewBufferedDelta(sseWriter)
	delta := buf.MakeDelta()

	// Register in RunTracker so reconnect can find it
	s.runTracker.Register(req.SessionID, buf)

	// Detached context: agent loop MUST survive HTTP disconnect.
	// Use server's base context (from Start()), not r.Context().
	runCtx := context.Background()

	// Run agent loop asynchronously — SSE connection may close before run completes
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.api.ChatStream(runCtx, req.SessionID, req.Message, req.Mode, delta); err != nil {
			if err.Error() == "agent is busy" {
				data, _ := json.Marshal(map[string]string{"type": "error", "message": "agent is busy"})
				buf.MakeDelta().OnError(fmt.Errorf("%s", string(data)))
			} else {
				log.Printf("[handleChat] run error: %v", err)
			}
			buf.MarkDone()
		}
	}()

	// Wait until either the run finishes or the client disconnects
	select {
	case <-done:
		// Run completed while SSE still connected — send [DONE]
		sseMu.Lock()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		sseMu.Unlock()
	case <-r.Context().Done():
		// Client disconnected — BufferedDelta auto-detects write failure
		// and switches to buffer mode. Run continues in background.
		log.Printf("[handleChat] SSE client disconnected, run continues in background (session=%s)", req.SessionID)
		buf.Detach()
	}
}

// handleReconnect resumes an SSE stream for an active run.
// GET /v1/chat/reconnect?session_id=xxx&last_seq=0
func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	run, ok := s.runTracker.Get(sessionID)
	if !ok {
		// No active run — return 404 so frontend knows not to retry
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "no active run for session"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// Parse last sequence ID for dedup
	lastSeq := 0
	if v := r.URL.Query().Get("last_seq"); v != "" {
		fmt.Sscanf(v, "%d", &lastSeq)
	}

	// Create new SSE writer for reconnected client
	var mu sync.Mutex
	disconnected := false
	writer := func(payload []byte) bool {
		mu.Lock()
		defer mu.Unlock()
		if disconnected {
			return false
		}
		_, err := fmt.Fprintf(w, "data: %s\n\n", payload)
		if err != nil {
			disconnected = true
			return false
		}
		flusher.Flush()
		return true
	}

	// Attach — replays buffered events and then streams live
	replayed := run.Buffer.Attach(writer, lastSeq)
	log.Printf("[reconnect] Replayed %d events for session %s (from seq %d)", replayed, sessionID, lastSeq)

	// If run already done (or completed during replay), send [DONE] immediately
	if run.Buffer.IsDone() {
		run.Buffer.Detach()
		mu.Lock()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		mu.Unlock()
		return
	}

	// Block until run completes OR client disconnects
	select {
	case <-run.Buffer.Done():
		// Agent finished — send [DONE] to close SSE stream cleanly
		run.Buffer.Detach()
		mu.Lock()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		mu.Unlock()
		log.Printf("[reconnect] Run completed, sent [DONE] for session %s", sessionID)
	case <-r.Context().Done():
		// Client disconnected before run finished
		run.Buffer.Detach()
		log.Printf("[reconnect] Client disconnected for session %s", sessionID)
	}
}

// handleRunStatus returns whether a session has an active run.
// GET /v1/chat/status?session_id=xxx
func (s *Server) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	run, ok := s.runTracker.Get(sessionID)
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{"active": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"active":   true,
		"done":     run.Buffer.IsDone(),
		"last_seq": run.Buffer.LastSeqID(),
	})
}

// handleApprove processes approval/denial of pending tool calls.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ApprovalID string `json:"approval_id"`
		Approved   bool   `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.ApprovalID == "" {
		http.Error(w, "approval_id required", http.StatusBadRequest)
		return
	}
	if err := s.api.Approve(req.ApprovalID, req.Approved); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status": "resolved", "approval_id": req.ApprovalID, "approved": req.Approved,
	})
}

// handleStop stops the current agent run.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // best-effort parse, empty body is ok
	s.api.StopRun(req.SessionID)
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// handleRetry strips the last assistant turn from history and returns
// the last user message so the frontend can re-send via the normal chat flow.
func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	lastMsg, err := s.api.RetryRun(context.Background(), req.SessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "last_message": lastMsg})
}

// handleSlash processes slash commands via HTTP.
func (s *Server) handleSlash(w http.ResponseWriter, r *http.Request) {
	cmd := strings.TrimPrefix(r.URL.Path, "/v1/slash/")
	result := s.execSlash("/" + cmd)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"result": result})
}

// execSlash routes slash commands.
func (s *Server) execSlash(input string) string {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "unknown command"
	}
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/model":
		if len(args) == 0 {
			return "Current model: " + s.api.CurrentModel()
		}
		if err := s.api.SwitchModel(args[0]); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return "Switched to: " + args[0]

	case "/models":
		return strings.Join(s.api.ListModels().Models, ", ")

	case "/set":
		if len(args) < 2 {
			return "Usage: /set <key> <value>"
		}
		if err := s.api.SetConfig(args[0], strings.Join(args[1:], " ")); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("Set %s = %s", args[0], strings.Join(args[1:], " "))

	case "/forge":
		return "Forge mode activated. Use the forge tool to begin."

	case "/plan":
		return "请使用输入框旁的模式切换 pill（⭐ Auto / 📋 Plan / 🤖 Agentic）"

	case "/status":
		stats := s.api.GetContextStats()
		sec := s.api.GetSecurity()
		return fmt.Sprintf("Model: %s | Security: %s | History: %d msgs",
			stats.Model, sec.Mode, stats.HistoryCount)

	case "/help":
		return "Commands: /model /models /set /forge /plan /skill /status /clear /compact /help"

	case "/skill":
		skillsRaw, err := s.api.ListSkills()
		if err != nil {
			return "Error: " + err.Error()
		}
		data, _ := json.Marshal(skillsRaw)
		var skills []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
			ForgeStatus string `json:"forge_status"`
		}
		json.Unmarshal(data, &skills)
		if len(skills) == 0 {
			return "No skills discovered."
		}
		var lines []string
		for _, sk := range skills {
			lines = append(lines, fmt.Sprintf("  %s [%s] (%s) — %s", sk.Name, sk.ForgeStatus, sk.Type, sk.Description))
		}
		return "Skills:\n" + strings.Join(lines, "\n")

	case "/clear":
		s.api.ClearHistory()
		return "History cleared."

	case "/compact":
		s.api.CompactContext()
		return "Context compacted."

	default:
		return "Unknown command: " + cmd
	}
}

// handleFile serves a local file over HTTP for browser rendering (images, media, etc.).
// Usage: GET /v1/file?path=/absolute/path/to/file.png
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		http.Error(w, "missing 'path' parameter", http.StatusBadRequest)
		return
	}

	// Resolve to absolute and clean to prevent directory traversal
	absPath := filepath.Clean(rawPath)
	if !filepath.IsAbs(absPath) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}

	// Verify file exists and is not a directory
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot serve directories", http.StatusForbidden)
		return
	}

	// Serve the file (Go auto-detects Content-Type from extension)
	http.ServeFile(w, r, absPath)
}

// handleUpload receives multipart file uploads from the web UI.
// Saves files to {workspace}/uploads/ and returns the absolute path.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 50MB max
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "file too large (max 50MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Determine upload directory: workspace/uploads/
	c := s.api.GetConfig()
	agent, _ := c["agent"].(map[string]any)
	workspace, _ := agent["workspace"].(string)
	if workspace == "" {
		workspace = os.TempDir()
	}
	uploadDir := filepath.Join(workspace, "uploads")
	os.MkdirAll(uploadDir, 0755)

	// Timestamp-prefixed filename to avoid conflicts
	safeFilename := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), filepath.Base(header.Filename))
	dstPath := filepath.Join(uploadDir, safeFilename)

	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, fmt.Sprintf("failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"path":     dstPath,
		"filename": header.Filename,
		"size":     fmt.Sprintf("%d", header.Size),
	})
}
