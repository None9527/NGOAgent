// Package application provides the unified AgentAPI facade.
// All protocol adapters (HTTP, gRPC, CLI) call this layer.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

// Version is set at build time via -ldflags.
var Version = "0.5.0"

// HistoryQuerier loads conversation history from persistence.
type HistoryQuerier interface {
	LoadAll(sessionID string) ([]service.HistoryExport, error)
}

// ═══════════════════════════════════════════
// AgentAPI — unified facade
// ═══════════════════════════════════════════

// AgentAPI is the protocol-agnostic API layer.
// All HTTP/gRPC/CLI adapters call these methods.
type AgentAPI struct {
	loop            *service.AgentLoop
	loopPool        *service.LoopPool
	chatEngine      *service.ChatEngine
	sessMgr         *service.SessionManager
	modelMgr        *service.ModelManager
	toolAdmin       *service.ToolAdmin
	secHook         *security.Hook
	skillMgr        *skill.Manager
	cronMgr         *cron.Manager
	mcpMgr          *mcp.Manager
	cfg             *config.Manager
	router          *llm.Router
	histQuery       HistoryQuerier
	brainDir        string // base brain directory for session-scoped artifact access
	kiStore         *knowledge.Store
	sandboxMgr      *sandbox.Manager // for process cleanup on stop
	startedAt       time.Time
	tokenUsageStore *persistence.TokenUsageStore // P2 H2: session token usage persistence (nil = disabled)
}

// NewAgentAPI creates a unified API facade.
func NewAgentAPI(
	loop *service.AgentLoop,
	loopPool *service.LoopPool,
	chatEngine *service.ChatEngine,
	sessMgr *service.SessionManager,
	modelMgr *service.ModelManager,
	toolAdmin *service.ToolAdmin,
	secHook *security.Hook,
	skillMgr *skill.Manager,
	cronMgr *cron.Manager,
	mcpMgr *mcp.Manager,
	cfg *config.Manager,
	router *llm.Router,
	histQuery HistoryQuerier,
	brainDir string,
	kiStore *knowledge.Store,
	sbMgr *sandbox.Manager,
) *AgentAPI {
	return &AgentAPI{
		loop:       loop,
		loopPool:   loopPool,
		chatEngine: chatEngine,
		sessMgr:    sessMgr,
		modelMgr:   modelMgr,
		toolAdmin:  toolAdmin,
		secHook:    secHook,
		skillMgr:   skillMgr,
		cronMgr:    cronMgr,
		mcpMgr:     mcpMgr,
		cfg:        cfg,
		router:     router,
		histQuery:  histQuery,
		brainDir:   brainDir,
		kiStore:    kiStore,
		sandboxMgr: sbMgr,
		startedAt:  time.Now(),
	}
}

// ErrBusy is returned when the agent loop is already running.
var ErrBusy = agenterr.ErrBusy

// ─── Chat ───

// ChatStream runs the agent loop with a user message, streaming events
// through the provided delta sink. This is the unified entry point for
// all transport layers (HTTP/SSE, gRPC, etc.).
//
// Kernel operations encapsulated:
//   - Loop resolution (per-session via LoopPool, or default loop)
//   - Concurrency guard (TryAcquire / ReleaseAcquire)
//   - Delta sink binding
//   - Agent loop execution
func (a *AgentAPI) ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error {
	// D1 fix: Synchronize Active session pointer to prevent ghost sessions on reconnect.
	// Any chat interaction proves the frontend is using this session.
	if sessionID != "" {
		a.sessMgr.Activate(sessionID)
	}

	// Resolve loop: per-session if LoopPool available
	loop := a.loop
	if sessionID != "" && a.loopPool != nil {
		loop = a.loopPool.Get(sessionID)
	}

	// Set per-request plan mode ("auto" | "plan" | "agentic")
	if mode != "" {
		loop.SetPlanMode(mode)
	}

	// Execution resume: reconnects should continue the pending graph run instead of
	// starting an empty user turn.
	if message == "" {
		runID, ok, err := loop.PendingRunID(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no pending execution for session %s", sessionID)
		}
		slog.Info(fmt.Sprintf("[session] Resuming pending run %s for session %s", runID, sessionID))
	}

	// Session resume: load persisted history if loop's memory is empty
	if message != "" && sessionID != "" && a.histQuery != nil && len(loop.GetHistory()) == 0 {
		exports, err := a.histQuery.LoadAll(sessionID)
		if err == nil && len(exports) > 0 {
			msgs := service.RestoreHistory(exports)
			loop.SetHistory(msgs)
			// Auto-compact on resume: prevent full-history overload
			loop.CompactIfNeeded()
			slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (stream)", len(msgs), sessionID))
		}
	}

	// Concurrency guard
	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	// Bind protocol-specific event sink
	loop.SetDelta(delta)

	// Inject session ID into context so SpawnFunc can retrieve runtime session ID
	ctx = ctxutil.WithSessionID(ctx, sessionID)

	// Execute agent loop — title distillation is handled by TitleDistillHook post-run
	err := loop.RunWithoutAcquire(ctx, message)

	// P2 H2: Auto-persist token usage after each run
	if sessionID != "" && a.tokenUsageStore != nil {
		go func() {
			if saveErr := a.SaveSessionCost(sessionID); saveErr != nil {
				slog.Info(fmt.Sprintf("[token] Auto-save cost failed for session %s: %v", sessionID, saveErr))
			}
		}()
	}

	return err
}

// SessionID is DEPRECATED — callers should use the frontend-provided session_id directly.
// The previous implementation fell back to the default loop's startup UUID when
// LoopPool had no entry for a new session, creating "ghost sessions" whose history
// was invisible to the frontend. Retained only for compile-time interface compliance.
func (a *AgentAPI) SessionID(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	// Defensive: return empty to surface bugs early rather than silently
	// routing to the default loop's random UUID.
	return ""
}

// StopRun signals the correct agent loop to stop.
// Uses sessionID to find the pool loop that is actually running.
// Uses GetIfExists to avoid creating a ghost loop for evicted sessions.
func (a *AgentAPI) StopRun(sessionID string) {
	if sessionID != "" && a.loopPool != nil {
		if loop := a.loopPool.GetIfExists(sessionID); loop != nil {
			loop.Stop()
			return
		}
	}
	a.loop.Stop()
}

// RetryRun strips the last assistant turn from the agent loop and returns
// the last user message text. The frontend re-sends it via normal ChatStream.
func (a *AgentAPI) RetryRun(_ context.Context, sessionID string) (string, error) {
	// Use GetIfExists to avoid creating ghost loops for evicted sessions
	loop := a.loop
	if sessionID != "" && a.loopPool != nil {
		if existing := a.loopPool.GetIfExists(sessionID); existing != nil {
			loop = existing
		}
	}
	// Session resume: load persisted history if loop's memory is empty
	if sessionID != "" && a.histQuery != nil && len(loop.GetHistory()) == 0 {
		exports, err := a.histQuery.LoadAll(sessionID)
		if err == nil && len(exports) > 0 {
			msgs := service.RestoreHistory(exports)
			loop.SetHistory(msgs)
			slog.Info(fmt.Sprintf("[retry] Resumed %d messages for session %s", len(msgs), sessionID))
		}
	}
	return loop.StripLastTurn()
}

// Approve resolves a pending tool approval.
func (a *AgentAPI) Approve(approvalID string, approved bool) error {
	if a.secHook == nil {
		return fmt.Errorf("security hook not configured")
	}
	return a.secHook.Resolve(approvalID, approved)
}

// ─── Session ───

// NewSession creates a new conversation session (persisted in DB immediately).
func (a *AgentAPI) NewSession(title string) apitype.SessionResponse {
	// Create in DB first — this ensures the session is durable and visible in ListSessions
	dbID, err := a.sessMgr.CreatePersisted("web", title)
	if err != nil {
		// Fallback to in-memory if DB unavailable
		slog.Info(fmt.Sprintf("[NewSession] DB create failed, falling back to memory: %v", err))
		sess := a.sessMgr.New(title)
		return apitype.SessionResponse{SessionID: sess.ID, Title: sess.Title}
	}
	return apitype.SessionResponse{SessionID: dbID, Title: title}
}

// ListSessions returns all sessions ordered by recency, with titles from DB.
func (a *AgentAPI) ListSessions() apitype.SessionListResponse {
	// Read from DB first — this has LLM-distilled titles
	dbSessions, err := a.sessMgr.ListFromRepo(200, 0)
	if err == nil && len(dbSessions) > 0 {
		// Build DB set for dedup
		inDB := make(map[string]bool, len(dbSessions))
		infos := make([]apitype.SessionInfo, 0, len(dbSessions))
		for _, s := range dbSessions {
			inDB[s.ID] = true
			title := s.Title
			if mem, ok := a.sessMgr.Get(s.ID); ok && mem.Title != "" {
				title = mem.Title
			}
			infos = append(infos, apitype.SessionInfo{ID: s.ID, Title: title, Channel: s.Channel, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt})
		}
		return apitype.SessionListResponse{Sessions: infos, Active: a.sessMgr.Active()}
	}
	// Fallback to in-memory only
	sessions := a.sessMgr.List()
	infos := make([]apitype.SessionInfo, len(sessions))
	for i, s := range sessions {
		infos[i] = apitype.SessionInfo{ID: s.ID, Title: s.Title}
	}
	return apitype.SessionListResponse{Sessions: infos, Active: a.sessMgr.Active()}
}

// DeleteSession removes a session and all its history via the kernel.
func (a *AgentAPI) DeleteSession(id string) error {
	return a.chatEngine.DeleteSession(id)
}

// SetSessionTitle sets a display title for a session.
func (a *AgentAPI) SetSessionTitle(id, title string) {
	a.sessMgr.SetTitle(id, title)
}

// ─── History ───

// GetHistory returns conversation history for a session.
func (a *AgentAPI) GetHistory(sessionID string) ([]apitype.HistoryMessage, error) {
	if a.histQuery == nil {
		return nil, fmt.Errorf("history store not configured")
	}

	// D1 fix: Loading history means the frontend is viewing this session.
	// Sync Active pointer so ListSessions returns the correct active ID.
	if sessionID != "" {
		a.sessMgr.Activate(sessionID)
	}

	// First Principles: The Active AgentLoop memory is the absolute Ground Truth.
	// If the loop is actively running (or recently used and in memory), it has
	// messages that are not yet persisted to the database.
	if sessionID != "" && a.loopPool != nil {
		if loop := a.loopPool.GetIfExists(sessionID); loop != nil {
			msgs := loop.GetHistory()
			if len(msgs) > 0 {
				return a.convertLLMToHistory(msgs), nil
			}
		}
	}

	// Fallback to database
	exports, err := a.histQuery.LoadAll(sessionID)
	if err != nil {
		return nil, err
	}
	return a.convertExportsToHistory(exports), nil
}

// convertLLMToHistory converts the agent's internal memory format to the API format.
func (a *AgentAPI) convertLLMToHistory(msgs []llm.Message) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				nameMap[tc.ID] = tc.Function.Name
				argsMap[tc.ID] = string(tc.Function.Arguments)
			}
		}
	}

	apiMsgs := make([]apitype.HistoryMessage, len(msgs))
	for i, m := range msgs {
		hm := apitype.HistoryMessage{
			Role:      m.Role,
			Content:   m.Content,
			Reasoning: m.Reasoning,
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			hm.ToolName = nameMap[m.ToolCallID]
			hm.ToolArgs = argsMap[m.ToolCallID]
		}
		apiMsgs[i] = hm
	}
	return apiMsgs
}

// convertExportsToHistory converts DB format to API format.
func (a *AgentAPI) convertExportsToHistory(exports []service.HistoryExport) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, e := range exports {
		if e.Role == "assistant" && e.ToolCalls != "" {
			var calls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if json.Unmarshal([]byte(e.ToolCalls), &calls) == nil {
				for _, c := range calls {
					if c.ID != "" {
						if c.Function.Name != "" {
							nameMap[c.ID] = c.Function.Name
						}
						if c.Function.Arguments != "" {
							argsMap[c.ID] = c.Function.Arguments
						}
					}
				}
			}
		}
	}

	msgs := make([]apitype.HistoryMessage, len(exports))
	for i, e := range exports {
		m := apitype.HistoryMessage{
			Role:      e.Role,
			Content:   e.Content,
			Reasoning: e.Reasoning,
		}
		if e.Role == "tool" && e.ToolCallID != "" {
			m.ToolName = nameMap[e.ToolCallID]
			m.ToolArgs = argsMap[e.ToolCallID]
		}
		msgs[i] = m
	}
	return msgs
}

// ClearHistory resets the conversation history for the active session.
func (a *AgentAPI) ClearHistory() {
	sid := a.sessMgr.Active()
	if sid != "" && a.loopPool != nil {
		a.loopPool.Get(sid).ClearHistory()
		return
	}
	a.loop.ClearHistory()
}

// CompactContext triggers context compaction for the active session.
func (a *AgentAPI) CompactContext() {
	sid := a.sessMgr.Active()
	if sid != "" && a.loopPool != nil {
		a.loopPool.Get(sid).Compact()
		return
	}
	a.loop.Compact()
}

// ─── Model ───

// ListModels returns available models.
func (a *AgentAPI) ListModels() apitype.ModelListResponse {
	return apitype.ModelListResponse{
		Models:  a.router.ListModels(),
		Current: a.router.CurrentModel(),
	}
}

// SwitchModel changes the active model.
func (a *AgentAPI) SwitchModel(name string) error {
	return a.router.SwitchModel(name)
}

// CurrentModel returns the current model name.
func (a *AgentAPI) CurrentModel() string {
	return a.router.CurrentModel()
}

// ─── Config ───

// GetConfig returns the full sanitized configuration.
func (a *AgentAPI) GetConfig() map[string]any {
	return a.cfg.Get().Sanitized()
}

// SetConfig sets a configuration value by dot-key.
func (a *AgentAPI) SetConfig(key string, value any) error {
	return a.cfg.Set(key, value)
}

// AddProvider adds an LLM provider.
func (a *AgentAPI) AddProvider(p config.ProviderDef) error {
	return a.cfg.AddProvider(p)
}

// RemoveProvider removes an LLM provider.
func (a *AgentAPI) RemoveProvider(name string) error {
	return a.cfg.RemoveProvider(name)
}

// AddMCPServer adds an MCP server.
func (a *AgentAPI) AddMCPServer(s config.MCPServerDef) error {
	return a.cfg.AddMCPServer(s)
}

// RemoveMCPServer removes an MCP server.
func (a *AgentAPI) RemoveMCPServer(name string) error {
	return a.cfg.RemoveMCPServer(name)
}

// ─── Tools & Skills ───

// ListTools returns all registered tools.
func (a *AgentAPI) ListTools() []apitype.ToolInfoResponse {
	tools := a.toolAdmin.List()
	result := make([]apitype.ToolInfoResponse, len(tools))
	for i, t := range tools {
		result[i] = apitype.ToolInfoResponse{Name: t.Name, Enabled: t.Enabled}
	}
	return result
}

// EnableTool enables a tool by name.
func (a *AgentAPI) EnableTool(name string) error {
	return a.toolAdmin.Enable(name)
}

// DisableTool disables a tool by name.
func (a *AgentAPI) DisableTool(name string) error {
	return a.toolAdmin.Disable(name)
}

// ─── Status & Info ───

// Health returns system health info.
func (a *AgentAPI) Health() apitype.HealthResponse {
	return apitype.HealthResponse{
		Status:  "ok",
		Version: Version,
		Model:   a.router.CurrentModel(),
		Tools:   len(a.toolAdmin.List()),
	}
}

// GetSecurity returns security policy and recent audit log.
func (a *AgentAPI) GetSecurity() apitype.SecurityResponse {
	c := a.cfg.Get()
	resp := apitype.SecurityResponse{
		Mode:         c.Security.Mode,
		BlockList:    c.Security.BlockList,
		SafeCommands: c.Security.SafeCommands,
	}
	if a.secHook != nil {
		entries := a.secHook.GetAuditLog(50)
		resp.AuditEntries = make([]apitype.AuditEntry, len(entries))
		for i, e := range entries {
			dec := "allow"
			switch e.Decision {
			case security.Deny:
				dec = "deny"
			case security.Ask:
				dec = "ask"
			}
			resp.AuditEntries[i] = apitype.AuditEntry{
				Time:     e.Timestamp.Format("15:04:05"),
				Tool:     e.ToolName,
				Decision: dec,
				Reason:   e.Reason,
			}
		}
	}
	return resp
}

// GetContextStats returns context usage stats for the active session.
func (a *AgentAPI) GetContextStats() apitype.ContextStats {
	var history []llm.Message
	var tokenStats service.TokenStats = service.TokenStats{}
	var cacheStats llm.CacheStats
	sid := a.sessMgr.Active()
	if sid != "" && a.loopPool != nil {
		loop := a.loopPool.Get(sid)
		history = loop.GetHistory()
		tokenStats = loop.GetTokenStats()
		cacheStats = loop.GetCacheStats()
	} else {
		history = a.loop.GetHistory()
		tokenStats = a.loop.GetTokenStats()
		cacheStats = a.loop.GetCacheStats()
	}
	tokenEst := 0
	for _, m := range history {
		tokenEst += len(m.Content) / 4
	}

	// Build per-model breakdown for JSON
	byModel := make(map[string]any)
	for model, mu := range tokenStats.ByModel {
		byModel[model] = mu
	}

	return apitype.ContextStats{
		Model:         a.router.CurrentModel(),
		HistoryCount:  len(history),
		TokenEstimate: tokenEst,
		TotalCostUSD:  tokenStats.TotalCostUSD,
		TotalCalls:    tokenStats.TotalCalls,
		ByModel:       byModel,
		CacheHitRate:  cacheStats.HitRate,
		CacheBreaks:   cacheStats.CacheBreaks,
	}
}

// GetSystemInfo returns runtime system information.
func (a *AgentAPI) GetSystemInfo() apitype.SystemInfoResponse {
	return apitype.SystemInfoResponse{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		UptimeMs:  time.Since(a.startedAt).Milliseconds(),
		Models:    len(a.router.ListModels()),
		Tools:     len(a.toolAdmin.List()),
		Skills:    len(a.skillMgr.List()),
	}
}

// CronStatus returns cron job summary.
func (a *AgentAPI) CronStatus() map[string]any {
	if a.cronMgr == nil {
		return map[string]any{"enabled": false, "jobs": 0}
	}
	jobs, _ := a.cronMgr.List()
	active := 0
	for _, j := range jobs {
		if j.Enabled {
			active++
		}
	}
	return map[string]any{
		"enabled": true,
		"total":   len(jobs),
		"active":  active,
	}
}

// ListCronJobs returns all cron jobs.
func (a *AgentAPI) ListCronJobs() (any, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.List()
}

// CreateCronJob creates a new cron job.
func (a *AgentAPI) CreateCronJob(name, schedule, prompt string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Create(name, schedule, prompt)
}

// DeleteCronJob removes a cron job.
func (a *AgentAPI) DeleteCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Delete(name)
}

// EnableCronJob activates a job.
func (a *AgentAPI) EnableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Enable(name)
}

// DisableCronJob deactivates a job.
func (a *AgentAPI) DisableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Disable(name)
}

// RunCronJobNow triggers a job immediately.
func (a *AgentAPI) RunCronJobNow(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.RunNow(name)
}

// ListCronLogs returns log entries for a specific cron job.
func (a *AgentAPI) ListCronLogs(jobName string) (any, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.ListLogs(jobName)
}

// ReadCronLog reads a specific cron job log file.
func (a *AgentAPI) ReadCronLog(jobName, logFile string) (string, error) {
	if a.cronMgr == nil {
		return "", fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.ReadLog(jobName, logFile)
}

// ═══════════════════════════════════════════
// Skills
// ═══════════════════════════════════════════

// ListSkills returns all discovered skills.
func (a *AgentAPI) ListSkills() (any, error) {
	if a.skillMgr == nil {
		return nil, fmt.Errorf("skill manager not configured")
	}
	skills := a.skillMgr.List()
	type skillInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Path        string `json:"path"`
		Type        string `json:"type"`
		Enabled     bool   `json:"enabled"`
		EvoStatus   string `json:"forge_status"`
	}
	var result []skillInfo
	for _, s := range skills {
		result = append(result, skillInfo{
			Name:        s.Name,
			Description: s.Description,
			Path:        s.Path,
			Type:        s.Type,
			Enabled:     s.Enabled,
			EvoStatus:   s.EvoStatus,
		})
	}
	return result, nil
}

// ReadSkillContent reads SKILL.md content for a named skill.
func (a *AgentAPI) ReadSkillContent(name string) (string, error) {
	if a.skillMgr == nil {
		return "", fmt.Errorf("skill manager not configured")
	}
	s, ok := a.skillMgr.Get(name)
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	return s.Content, nil
}

// RefreshSkills re-discovers all skills from disk.
func (a *AgentAPI) RefreshSkills() error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	a.skillMgr.Discover()
	return nil
}

// DeleteSkill removes a skill by name.
func (a *AgentAPI) DeleteSkill(name string) error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	return a.skillMgr.Delete(name)
}

// ═══════════════════════════════════════════
// MCP Servers
// ═══════════════════════════════════════════

// ListMCPServers returns all MCP server names and their running status.
func (a *AgentAPI) ListMCPServers() (any, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	servers := a.mcpMgr.ListServers()
	type srvInfo struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
	}
	var result []srvInfo
	for name, running := range servers {
		result = append(result, srvInfo{Name: name, Running: running})
	}
	return result, nil
}

// ListMCPTools returns all tools exposed by MCP servers.
func (a *AgentAPI) ListMCPTools() (any, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	tools := a.mcpMgr.ListTools()
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Server      string `json:"server"`
	}
	var result []toolInfo
	for _, t := range tools {
		result = append(result, toolInfo{
			Name:        t.Name,
			Description: t.Description,
			Server:      t.ServerName,
		})
	}
	return result, nil
}

// ═══════════════════════════════════════════
// Brain artifacts
// ═══════════════════════════════════════════

// ListBrainArtifacts returns all artifacts for a given session.
func (a *AgentAPI) ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error) {
	if sessionID == "" || a.brainDir == "" {
		return nil, fmt.Errorf("session_id required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	files, err := store.List()
	if err != nil {
		return nil, err
	}
	var result []apitype.BrainArtifactInfo
	for _, f := range files {
		// Filter out metadata/resolved files for clean display
		if strings.HasSuffix(f, ".metadata.json") || strings.Contains(f, ".resolved") {
			continue
		}
		info, err := os.Stat(filepath.Join(a.brainDir, sessionID, f))
		if err != nil {
			continue
		}
		result = append(result, apitype.BrainArtifactInfo{
			Name:    f,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return result, nil
}

// ReadBrainArtifact reads a single brain artifact by name.
func (a *AgentAPI) ReadBrainArtifact(sessionID, name string) (string, error) {
	if sessionID == "" || name == "" || a.brainDir == "" {
		return "", fmt.Errorf("session_id and name required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	return store.Read(name)
}

// ═══════════════════════════════════════════
// KI (Knowledge Items) management
// ═══════════════════════════════════════════

// KIInfo is a summary of a Knowledge Item for API responses.
type KIInfo struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
	Sources   []string `json:"sources"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func kiToInfo(item *knowledge.Item) KIInfo {
	return KIInfo{
		ID:        item.ID,
		Title:     item.Title,
		Summary:   item.Summary,
		Tags:      item.Tags,
		Sources:   item.Sources,
		CreatedAt: item.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

// ListKI returns all Knowledge Items.
func (a *AgentAPI) ListKI() (any, error) {
	if a.kiStore == nil {
		return nil, fmt.Errorf("KI store not configured")
	}
	items, err := a.kiStore.List()
	if err != nil {
		return nil, err
	}
	result := make([]KIInfo, len(items))
	for i, item := range items {
		result[i] = kiToInfo(item)
	}
	return result, nil
}

// GetKI returns a single Knowledge Item with full content.
func (a *AgentAPI) GetKI(id string) (any, error) {
	if a.kiStore == nil {
		return nil, fmt.Errorf("KI store not configured")
	}
	return a.kiStore.Get(id)
}

// DeleteKI removes a Knowledge Item directory.
func (a *AgentAPI) DeleteKI(id string) error {
	if a.kiStore == nil || id == "" {
		return fmt.Errorf("KI store not configured or id empty")
	}
	dir := filepath.Join(a.kiStore.BaseDir(), id)
	return os.RemoveAll(dir)
}

// ListKIArtifacts lists artifact files in a KI.
func (a *AgentAPI) ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error) {
	if a.kiStore == nil || id == "" {
		return nil, fmt.Errorf("id required")
	}
	artDir := filepath.Join(a.kiStore.BaseDir(), id, "artifacts")
	var result []apitype.BrainArtifactInfo
	filepath.Walk(artDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(artDir, path)
		result = append(result, apitype.BrainArtifactInfo{
			Name:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
		return nil
	})
	return result, nil
}

// ReadKIArtifact reads a single artifact file from a KI.
func (a *AgentAPI) ReadKIArtifact(id, name string) (string, error) {
	if a.kiStore == nil || id == "" || name == "" {
		return "", fmt.Errorf("id and name required")
	}
	data, err := os.ReadFile(filepath.Join(a.kiStore.BaseDir(), id, "artifacts", name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ═══════════════════════════════════════════
// P2 H2: Token usage persistence
// ═══════════════════════════════════════════

// SetTokenUsageStore injects the token usage persistence store.
func (a *AgentAPI) SetTokenUsageStore(store *persistence.TokenUsageStore) {
	a.tokenUsageStore = store
}

// SaveSessionCost persists the current session's token usage to the database.
func (a *AgentAPI) SaveSessionCost(sessionID string) error {
	if a.tokenUsageStore == nil {
		return fmt.Errorf("token usage store not configured")
	}
	stats := a.GetContextStats()
	var promptTok, completeTok int
	for _, usage := range stats.ByModel {
		if u, ok := usage.(map[string]any); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				promptTok += int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				completeTok += int(ct)
			}
		}
	}
	return a.tokenUsageStore.SaveSessionUsage(
		sessionID, stats.Model,
		promptTok, completeTok,
		stats.TotalCalls, stats.TotalCostUSD,
		stats.ByModel,
	)
}

// GetSessionCost retrieves stored token usage for a given session.
func (a *AgentAPI) GetSessionCost(sessionID string) (map[string]any, error) {
	if a.tokenUsageStore == nil {
		return nil, fmt.Errorf("token usage store not configured")
	}
	usage, err := a.tokenUsageStore.GetSessionUsage(sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id":   usage.SessionID,
		"model":        usage.Model,
		"prompt_tok":   usage.TotalPromptTok,
		"complete_tok": usage.TotalCompleteTok,
		"total_calls":  usage.TotalCalls,
		"total_cost":   usage.TotalCostUSD,
		"updated_at":   usage.UpdatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}
