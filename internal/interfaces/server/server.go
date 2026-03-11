// Package server provides HTTP/SSE and slash-command routing.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

// Server is the HTTP/SSE server for NGOAgent.
type Server struct {
	loop      *service.AgentLoop
	loopPool  *service.LoopPool
	router    *llm.Router
	cfg       *config.Manager
	sessMgr   *service.SessionManager
	toolAdmin *service.ToolAdmin
	secHook   *security.Hook
	skillMgr  *skill.Manager
	addr      string
	mu        sync.Mutex
}

// NewServer creates an HTTP server.
func NewServer(loop *service.AgentLoop, router *llm.Router, cfg *config.Manager, addr string) *Server {
	return &Server{
		loop:   loop,
		router: router,
		cfg:    cfg,
		addr:   addr,
	}
}

// SetLoopPool sets the per-session loop pool.
func (s *Server) SetLoopPool(pool *service.LoopPool) {
	s.loopPool = pool
}

// SetSkillMgr sets the skill manager for /skill commands.
func (s *Server) SetSkillMgr(mgr *skill.Manager) {
	s.skillMgr = mgr
}

// SetManagers injects optional managers for REST API routes.
func (s *Server) SetManagers(sessMgr *service.SessionManager, toolAdmin *service.ToolAdmin, secHook *security.Hook) {
	s.sessMgr = sessMgr
	s.toolAdmin = toolAdmin
	s.secHook = secHook
}

// Start begins listening for HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Chat endpoint (SSE streaming)
	mux.HandleFunc("/v1/chat", s.handleChat)

	// Approval endpoint (responds to pending approvals)
	mux.HandleFunc("/v1/approve", s.handleApprove)

	// Slash commands
	mux.HandleFunc("/v1/slash/", s.handleSlash)

	// Health check
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"model":  s.router.CurrentModel(),
		})
	})

	// Models list
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"models":  s.router.ListModels(),
			"current": s.router.CurrentModel(),
		})
	})

	// Config endpoint (expose agent limits for verification)
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		c := s.cfg.Get()
		json.NewEncoder(w).Encode(map[string]any{
			"agent": map[string]any{
				"max_steps":     c.Agent.MaxSteps,
				"planning_mode": c.Agent.PlanningMode,
			},
		})
	})

	// REST API routes (session/tools/context/security)
	if s.sessMgr != nil && s.toolAdmin != nil {
		s.RegisterAPIRoutes(mux, s.sessMgr, s.toolAdmin, s.secHook)
	}

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	// Check for slash command
	if strings.HasPrefix(req.Message, "/") {
		result := s.execSlash(req.Message)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": result})
		return
	}

	// SSE streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Resolve loop: per-session if session_id provided, default otherwise
	loop := s.loop
	if req.SessionID != "" && s.loopPool != nil {
		loop = s.loopPool.Get(req.SessionID)
	}

	// Expose session ID to client
	if sid := loop.SessionID(); sid != "" {
		w.Header().Set("X-Session-Id", sid)
	}
	flusher.Flush()
	delta := &service.Delta{
		OnTextFunc: func(text string) {
			data, _ := json.Marshal(map[string]string{"type": "text_delta", "content": text})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnReasoningFunc: func(text string) {
			data, _ := json.Marshal(map[string]string{"type": "thinking", "content": text})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnToolStartFunc: func(name string, args map[string]any) {
			data, _ := json.Marshal(map[string]any{"type": "tool_start", "name": name, "args": args})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnToolResultFunc: func(name, output string, err error) {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			data, _ := json.Marshal(map[string]any{"type": "tool_result", "name": name, "output": output, "error": errStr})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnCompleteFunc: func() {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		},
		OnErrorFunc: func(err error) {
			data, _ := json.Marshal(map[string]string{"type": "error", "message": err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnProgressFunc: func(taskName, status, summary, mode string) {
			data, _ := json.Marshal(map[string]any{
				"type":      "progress",
				"task_name": taskName,
				"status":    status,
				"summary":   summary,
				"mode":      mode,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnApprovalRequestFunc: func(approvalID, toolName string, args map[string]any, reason string) {
			data, _ := json.Marshal(map[string]any{
				"type":        "approval_request",
				"approval_id": approvalID,
				"tool_name":   toolName,
				"args":        args,
				"reason":      reason,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
	}


	loop.SetDelta(delta)
	if err := loop.Run(r.Context(), req.Message); err != nil {
		errMsg := err.Error()
		// Backpressure: return 429 if agent is busy (concurrent run)
		if strings.Contains(errMsg, "agent is busy") {
			w.Header().Set("Retry-After", "5")
			data, _ := json.Marshal(map[string]string{"type": "error", "message": errMsg})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}
		data, _ := json.Marshal(map[string]string{"type": "error", "message": errMsg})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// handleApprove processes approval/denial of pending tool calls.
// POST /v1/approve {"approval_id": "xxx", "approved": true}
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

	if s.secHook == nil {
		http.Error(w, "security hook not configured", http.StatusInternalServerError)
		return
	}

	if err := s.secHook.Resolve(req.ApprovalID, req.Approved); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status":      "resolved",
		"approval_id": req.ApprovalID,
		"approved":    req.Approved,
	})
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
			return "Current model: " + s.router.CurrentModel()
		}
		if err := s.router.SwitchModel(args[0]); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return "Switched to: " + args[0]

	case "/models":
		return strings.Join(s.router.ListModels(), ", ")

	case "/set":
		if len(args) < 2 {
			return "Usage: /set <key> <value>"
		}
		if err := s.cfg.Set(args[0], strings.Join(args[1:], " ")); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("Set %s = %s", args[0], strings.Join(args[1:], " "))

	case "/forge":
		return "Forge mode activated. Use the forge tool to begin."

	case "/plan":
		c := s.cfg.Get()
		newMode := !c.Agent.PlanningMode
		if err := s.cfg.Set("agent.planning_mode", newMode); err != nil {
			return fmt.Sprintf("Failed to toggle planning mode: %v", err)
		}
		if newMode {
			return "Planning mode: ON — 复杂任务将先制定计划再执行"
		}
		return "Planning mode: OFF — 自动判断是否需要计划"

	case "/status":
		c := s.cfg.Get()
		planStr := "off"
		if c.Agent.PlanningMode {
			planStr = "forced"
		}
		return fmt.Sprintf("Model: %s | Security: %s | Planning: %s",
			s.router.CurrentModel(), c.Security.Mode, planStr)

	case "/help":
		return "Commands: /model /models /set /forge /plan /skill /status /clear /compact /help"

	case "/skill":
		if s.skillMgr == nil {
			return "Skill manager not available"
		}
		if len(args) == 0 || args[0] == "list" {
			skills := s.skillMgr.List()
			if len(skills) == 0 {
				return "No skills discovered."
			}
			var lines []string
			for _, sk := range skills {
				lines = append(lines, fmt.Sprintf("  %s [%s] (%s) — %s", sk.Name, sk.ForgeStatus, sk.Type, sk.Description))
			}
			return "Skills:\n" + strings.Join(lines, "\n")
		}
		if args[0] == "info" && len(args) > 1 {
			sk, ok := s.skillMgr.Get(args[1])
			if !ok {
				return "Skill not found: " + args[1]
			}
			return fmt.Sprintf("Name: %s\nType: %s\nStatus: %s\nPath: %s\nDesc: %s", sk.Name, sk.Type, sk.ForgeStatus, sk.Path, sk.Description)
		}
		return "Usage: /skill [list|info <name>]"

	case "/clear":
		s.loop.ClearHistory()
		return "History cleared."

	case "/compact":
		s.loop.Compact()
		return "Context compacted."

	default:
		return "Unknown command: " + cmd
	}
}
