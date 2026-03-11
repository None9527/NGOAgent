package server

import (
	"encoding/json"
	"net/http"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
)

// RegisterAPIRoutes registers the REST API routes on the given mux.
// These complement the core /v1/chat and /v1/slash routes.
func (s *Server) RegisterAPIRoutes(mux *http.ServeMux, sessMgr *service.SessionManager, toolAdmin *service.ToolAdmin, secHook *security.Hook) {
	// Session management
	mux.HandleFunc("/api/v1/session/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Title string `json:"title"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		sess := sessMgr.New(req.Title)
		json.NewEncoder(w).Encode(map[string]string{"session_id": sess.ID, "title": sess.Title})
	})

	mux.HandleFunc("/api/v1/session/list", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessMgr.List()
		type sessionInfo struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		var list []sessionInfo
		for _, sess := range sessions {
			list = append(list, sessionInfo{ID: sess.ID, Title: sess.Title})
		}
		json.NewEncoder(w).Encode(map[string]any{"sessions": list, "active": sessMgr.Active()})
	})

	mux.HandleFunc("/api/v1/session/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := sessMgr.Delete(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	// Tools management
	mux.HandleFunc("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		tools := toolAdmin.List()
		json.NewEncoder(w).Encode(map[string]any{"tools": tools})
	})

	mux.HandleFunc("/api/v1/tools/enable", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := toolAdmin.Enable(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "enabled", "tool": req.Name})
	})

	mux.HandleFunc("/api/v1/tools/disable", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := toolAdmin.Disable(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "disabled", "tool": req.Name})
	})

	// Context stats
	mux.HandleFunc("/api/v1/context/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"model":         s.router.CurrentModel(),
			"history_count": 0, // Future: wire to loop.HistoryLen()
		})
	})

	// Security audit log
	mux.HandleFunc("/api/v1/security/audit", func(w http.ResponseWriter, r *http.Request) {
		if secHook == nil {
			json.NewEncoder(w).Encode(map[string]any{"entries": []any{}})
			return
		}
		entries := secHook.GetAuditLog(50)
		type auditEntry struct {
			Time     string `json:"time"`
			Tool     string `json:"tool"`
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
			Mode     string `json:"mode"`
		}
		var result []auditEntry
		for _, e := range entries {
			dec := "allow"
			switch e.Decision {
			case security.Deny:
				dec = "deny"
			case security.Ask:
				dec = "ask"
			}
			result = append(result, auditEntry{
				Time:     e.Timestamp.Format("15:04:05"),
				Tool:     e.ToolName,
				Decision: dec,
				Reason:   e.Reason,
				Mode:     e.Mode,
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"entries": result})
	})
}

// RegisterExtraSlashCommands adds slash commands not in the original server.
func (s *Server) RegisterExtraSlashCommands() {
	// These are handled within execSlash
}
