package server

import (
	"encoding/json"
	"net/http"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// registerAPIRoutes registers all REST API routes on the given mux.
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {

	// ─── Session ───

	mux.HandleFunc("/api/v1/session/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Title string `json:"title"` }
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(s.api.NewSession(req.Title))
	})

	mux.HandleFunc("/api/v1/session/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.ListSessions())
	})

	mux.HandleFunc("/api/v1/session/title", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Title == "" {
			http.Error(w, "id and title required", http.StatusBadRequest)
			return
		}
		s.api.SetSessionTitle(req.ID, req.Title)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": req.ID, "title": req.Title})
	})

	mux.HandleFunc("/api/v1/session/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ ID string `json:"id"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.DeleteSession(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	// ─── History ───

	mux.HandleFunc("/api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		msgs, err := s.api.GetHistory(sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"messages": msgs})
	})

	mux.HandleFunc("/api/v1/history/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		s.api.ClearHistory()
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	})

	mux.HandleFunc("/api/v1/history/compact", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		s.api.CompactContext()
		json.NewEncoder(w).Encode(map[string]string{"status": "compacted"})
	})

	// ─── Tools ───

	mux.HandleFunc("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tools": s.api.ListTools()})
	})

	mux.HandleFunc("/api/v1/tools/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.EnableTool(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "enabled", "tool": req.Name})
	})

	mux.HandleFunc("/api/v1/tools/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.DisableTool(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "disabled", "tool": req.Name})
	})

	mux.HandleFunc("/api/v1/skills/list", func(w http.ResponseWriter, r *http.Request) {
		skills, err := s.api.ListSkills()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"skills": skills})
	})

	mux.HandleFunc("/api/v1/skills/read", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		content, err := s.api.ReadSkillContent(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": name, "content": content})
	})

	mux.HandleFunc("/api/v1/skills/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := s.api.RefreshSkills(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "refreshed"})
	})

	mux.HandleFunc("/api/v1/skills/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if err := s.api.DeleteSkill(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "name": req.Name})
	})

	// ─── MCP ───

	mux.HandleFunc("/api/v1/mcp/servers", func(w http.ResponseWriter, r *http.Request) {
		servers, err := s.api.ListMCPServers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"servers": servers})
	})

	mux.HandleFunc("/api/v1/mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		tools, err := s.api.ListMCPTools()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"tools": tools})
	})

	// ─── Config set ───

	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		if err := s.api.SetConfig(req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "key": req.Key, "value": req.Value})
	})

	// ─── Provider management ───

	mux.HandleFunc("/api/v1/config/provider/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req config.ProviderDef
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.api.AddProvider(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "provider": req.Name})
	})

	mux.HandleFunc("/api/v1/config/provider/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.api.RemoveProvider(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "provider": req.Name})
	})

	// ─── MCP management ───

	mux.HandleFunc("/api/v1/config/mcp/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req config.MCPServerDef
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.api.AddMCPServer(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "mcp_server": req.Name})
	})

	mux.HandleFunc("/api/v1/config/mcp/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.api.RemoveMCPServer(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "mcp_server": req.Name})
	})

	// ─── Security ───

	mux.HandleFunc("/api/v1/security", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.GetSecurity())
	})

	// ─── Stats & System ───

	mux.HandleFunc("/api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.GetContextStats())
	})

	mux.HandleFunc("/api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.api.GetSystemInfo())
	})

	// ─── Brain artifacts ───

	mux.HandleFunc("/api/v1/brain/list", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		files, err := s.api.ListBrainArtifacts(sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"artifacts": files})
	})

	mux.HandleFunc("/api/v1/brain/read", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		name := r.URL.Query().Get("name")
		content, err := s.api.ReadBrainArtifact(sid, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": name, "content": content})
	})

	// ─── KI (Knowledge Items) ───

	mux.HandleFunc("/api/v1/ki/list", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.api.ListKI()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	})

	mux.HandleFunc("/api/v1/ki/get", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		item, err := s.api.GetKI(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(item)
	})

	mux.HandleFunc("/api/v1/ki/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ ID string `json:"id"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.DeleteKI(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("/api/v1/ki/artifacts", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		files, err := s.api.ListKIArtifacts(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"artifacts": files})
	})

	mux.HandleFunc("/api/v1/ki/artifact/read", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		name := r.URL.Query().Get("name")
		content, err := s.api.ReadKIArtifact(id, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": name, "content": content})
	})

	// ─── Cron management ───

	mux.HandleFunc("/api/v1/cron/list", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := s.api.ListCronJobs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
	})

	mux.HandleFunc("/api/v1/cron/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Schedule == "" || req.Prompt == "" {
			http.Error(w, "name, schedule, prompt required", http.StatusBadRequest)
			return
		}
		if err := s.api.CreateCronJob(req.Name, req.Schedule, req.Prompt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "created", "name": req.Name})
	})

	mux.HandleFunc("/api/v1/cron/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.DeleteCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("/api/v1/cron/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.EnableCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "enabled", "name": req.Name})
	})

	mux.HandleFunc("/api/v1/cron/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.DisableCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "disabled", "name": req.Name})
	})

	mux.HandleFunc("/api/v1/cron/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.api.RunCronJobNow(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "triggered", "name": req.Name})
	})

	mux.HandleFunc("/api/v1/cron/logs", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		logs, err := s.api.ListCronLogs(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"logs": logs})
	})

	mux.HandleFunc("/api/v1/cron/log/read", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		file := r.URL.Query().Get("file")
		if name == "" || file == "" {
			http.Error(w, "name and file required", http.StatusBadRequest)
			return
		}
		content, err := s.api.ReadCronLog(name, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"file": file, "content": content})
	})
}
