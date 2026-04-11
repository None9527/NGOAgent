package server

import (
	"encoding/json"
	"net/http"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

// registerAPIRoutes registers all REST API routes on the given mux.
func (s *Server) registerAPIRoutes(mux *http.ServeMux) {

	// ─── Session ───

	mux.HandleFunc("/api/v1/session/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Title string `json:"title"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(s.session.NewSession(req.Title))
	})

	mux.HandleFunc("/api/v1/session/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.session.ListSessions())
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
		s.session.SetSessionTitle(req.ID, req.Title)
		json.NewEncoder(w).Encode(apitype.StatusIDResponse{Status: "ok", ID: req.ID})
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
		if err := s.session.DeleteSession(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "deleted"})
	})

	// ─── History ───

	mux.HandleFunc("/api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		msgs, err := s.session.GetHistory(sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.MessageListResponse{Messages: msgs})
	})

	mux.HandleFunc("/api/v1/history/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		s.session.ClearHistory()
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "cleared"})
	})

	mux.HandleFunc("/api/v1/history/compact", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		s.session.CompactContext()
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "compacted"})
	})

	// ─── Runtime / Orchestration ───

	mux.HandleFunc("/api/v1/runtime/runs", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			http.Error(w, "session_id required", http.StatusBadRequest)
			return
		}
		runs, err := s.runtime.ListRuntimeRuns(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeRunListResponse{Runs: runs})
	})

	mux.HandleFunc("/api/v1/runtime/graph", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			http.Error(w, "session_id required", http.StatusBadRequest)
			return
		}
		graph, err := s.runtime.ListRuntimeGraph(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(graph)
	})

	mux.HandleFunc("/api/v1/runtime/runs/pending", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			http.Error(w, "session_id required", http.StatusBadRequest)
			return
		}
		runs, err := s.runtime.ListPendingRuns(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeRunListResponse{Runs: runs})
	})

	mux.HandleFunc("/api/v1/runtime/decisions/pending", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			http.Error(w, "session_id required", http.StatusBadRequest)
			return
		}
		runs, err := s.runtime.ListPendingDecisions(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeRunListResponse{Runs: runs})
	})

	mux.HandleFunc("/api/v1/runtime/runs/children", func(w http.ResponseWriter, r *http.Request) {
		parentRunID := r.URL.Query().Get("parent_run_id")
		if parentRunID == "" {
			http.Error(w, "parent_run_id required", http.StatusBadRequest)
			return
		}
		runs, err := s.runtime.ListChildRuns(r.Context(), parentRunID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeRunListResponse{Runs: runs})
	})

	mux.HandleFunc("/api/v1/runtime/runs/resume", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		req, err := decodeRuntimeResumeRequest(r)
		if err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.SessionID == "" {
			http.Error(w, "session_id required", http.StatusBadRequest)
			return
		}
		ingressResp, err := s.chat.ApplyRuntimeIngress(r.Context(), apitype.NewRuntimeResumeIngressRequest(req))
		if err != nil {
			writeJSONError(w, err)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeResumeResponseFromIngress(ingressResp))
	})

	mux.HandleFunc("/api/v1/runtime/decision/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		req, err := decodeRuntimeDecisionApplyRequest(r)
		if err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.SessionID == "" || req.Decision.Decision == "" {
			http.Error(w, "session_id and decision required", http.StatusBadRequest)
			return
		}
		ingressResp, err := s.chat.ApplyRuntimeIngress(r.Context(), apitype.NewRuntimeDecisionIngressRequest(req))
		if err != nil {
			writeJSONError(w, err)
			return
		}
		json.NewEncoder(w).Encode(apitype.RuntimeDecisionApplyResponseFromIngress(ingressResp))
	})

	mux.HandleFunc("/api/v1/runtime/ingress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		req, err := decodeRuntimeIngressRequest(r)
		if err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		resp, err := s.chat.ApplyRuntimeIngress(r.Context(), req)
		if err != nil {
			writeJSONError(w, err)
			return
		}
		json.NewEncoder(w).Encode(resp)
	})

	// ─── Tools ───

	mux.HandleFunc("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apitype.ToolListResponse{Tools: s.admin.ListTools()})
	})

	mux.HandleFunc("/api/v1/tools/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.EnableTool(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusToolResponse{Status: "enabled", Tool: req.Name})
	})

	mux.HandleFunc("/api/v1/tools/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.DisableTool(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusToolResponse{Status: "disabled", Tool: req.Name})
	})

	mux.HandleFunc("/api/v1/skills/list", func(w http.ResponseWriter, r *http.Request) {
		skills, err := s.admin.ListSkills()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.SkillListResponse{Skills: skills})
	})

	mux.HandleFunc("/api/v1/skills/read", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		content, err := s.admin.ReadSkillContent(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.SkillContentResponse{Name: name, Content: content})
	})

	mux.HandleFunc("/api/v1/skills/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := s.admin.RefreshSkills(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "refreshed"})
	})

	mux.HandleFunc("/api/v1/skills/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if err := s.admin.DeleteSkill(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusNameResponse{Status: "deleted", Name: req.Name})
	})

	// ─── MCP ───

	mux.HandleFunc("/api/v1/mcp/servers", func(w http.ResponseWriter, r *http.Request) {
		servers, err := s.admin.ListMCPServers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.ServerListResponse{Servers: servers})
	})

	mux.HandleFunc("/api/v1/mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		tools, err := s.admin.ListMCPTools()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.MCPToolListResponse{Tools: tools})
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
		if err := s.admin.SetConfig(req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusKeyValueResponse{Status: "ok", Key: req.Key, Value: req.Value})
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
		if err := s.admin.AddProvider(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusProviderResponse{Status: "added", Provider: req.Name})
	})

	mux.HandleFunc("/api/v1/config/provider/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.admin.RemoveProvider(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusProviderResponse{Status: "removed", Provider: req.Name})
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
		if err := s.admin.AddMCPServer(req); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusMCPServerResponse{Status: "added", MCPServer: req.Name})
	})

	mux.HandleFunc("/api/v1/config/mcp/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := s.admin.RemoveMCPServer(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusMCPServerResponse{Status: "removed", MCPServer: req.Name})
	})

	// ─── Security ───

	mux.HandleFunc("/api/v1/security", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.admin.GetSecurity())
	})

	// ─── Stats & System ───

	mux.HandleFunc("/api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.admin.GetContextStats())
	})

	mux.HandleFunc("/api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(s.admin.GetSystemInfo())
	})

	// ─── Brain artifacts ───

	mux.HandleFunc("/api/v1/brain/list", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		files, err := s.admin.ListBrainArtifacts(sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.ArtifactListResponse{Artifacts: files})
	})

	mux.HandleFunc("/api/v1/brain/read", func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session_id")
		name := r.URL.Query().Get("name")
		content, err := s.admin.ReadBrainArtifact(sid, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.SkillContentResponse{Name: name, Content: content})
	})

	// ─── KI (Knowledge Items) ───

	mux.HandleFunc("/api/v1/ki/list", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.admin.ListKI()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.KIItemListResponse{Items: items})
	})

	mux.HandleFunc("/api/v1/ki/get", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		item, err := s.admin.GetKI(id)
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
		var req struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.DeleteKI(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "deleted"})
	})

	mux.HandleFunc("/api/v1/ki/artifacts", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		files, err := s.admin.ListKIArtifacts(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.ArtifactListResponse{Artifacts: files})
	})

	mux.HandleFunc("/api/v1/ki/artifact/read", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		name := r.URL.Query().Get("name")
		content, err := s.admin.ReadKIArtifact(id, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.SkillContentResponse{Name: name, Content: content})
	})

	// ─── Cron management ───

	mux.HandleFunc("/api/v1/cron/list", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := s.admin.ListCronJobs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.CronJobListResponse{Jobs: jobs})
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
		if err := s.admin.CreateCronJob(req.Name, req.Schedule, req.Prompt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusNameResponse{Status: "created", Name: req.Name})
	})

	mux.HandleFunc("/api/v1/cron/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.DeleteCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusResponse{Status: "deleted"})
	})

	mux.HandleFunc("/api/v1/cron/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.EnableCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusNameResponse{Status: "enabled", Name: req.Name})
	})

	mux.HandleFunc("/api/v1/cron/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.DisableCronJob(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusNameResponse{Status: "disabled", Name: req.Name})
	})

	mux.HandleFunc("/api/v1/cron/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if err := s.admin.RunCronJobNow(req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.StatusNameResponse{Status: "triggered", Name: req.Name})
	})

	mux.HandleFunc("/api/v1/cron/logs", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		logs, err := s.admin.ListCronLogs(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(apitype.CronLogListResponse{Logs: logs})
	})

	mux.HandleFunc("/api/v1/cron/log/read", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		file := r.URL.Query().Get("file")
		if name == "" || file == "" {
			http.Error(w, "name and file required", http.StatusBadRequest)
			return
		}
		content, err := s.admin.ReadCronLog(name, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(apitype.FileContentResponse{File: file, Content: content})
	})
}
