// legacy_compat.go — AgentAPI compatibility facade.
//
// AgentAPI is a thin 1:1 delegation shell over the explicit capability services.
// It is retained only for compatibility callers and tests that prove that
// compatibility contract. New code must use ApplicationServices directly.
package application

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

// ═══════════════════════════════════════════
// AgentAPI struct + factory
// ═══════════════════════════════════════════

// AgentAPI is a compatibility facade over the explicit application services.
// It is not part of the R4 primary application contract.
type AgentAPI struct {
	chat    *ChatService
	runtime *RuntimeService
	session *SessionService
	admin   *AdminService
	cost    *CostService
}

// legacyFacade is the internal bridge from ApplicationServices to the concrete
// compatibility shell retained for legacy callers.
func (s *ApplicationServices) legacyFacade() *AgentAPI {
	return &AgentAPI{
		chat:    s.chatService,
		runtime: s.runtime,
		session: s.session,
		admin:   s.admin,
		cost:    s.cost,
	}
}

// NewLegacyAPI creates the compatibility facade from the explicit
// ApplicationDeps bundle. New code should prefer ApplicationServices and its
// capability services directly.
func NewLegacyAPI(deps ApplicationDeps) LegacyAPI {
	return NewApplicationServices(deps).LegacyAPI()
}

// NewAgentAPI creates the legacy compatibility facade.
// Deprecated: prefer NewLegacyAPI(ApplicationDeps{...}) only for compatibility
// paths, or NewApplicationServices(...) for all new construction.
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
	return NewApplicationServices(ApplicationDeps{
		Loop:       loop,
		LoopPool:   loopPool,
		ChatEngine: chatEngine,
		SessionMgr: sessMgr,
		ModelMgr:   modelMgr,
		ToolAdmin:  toolAdmin,
		SecHook:    secHook,
		SkillMgr:   skillMgr,
		CronMgr:    cronMgr,
		MCPMgr:     mcpMgr,
		Config:     cfg,
		Router:     router,
		HistQuery:  histQuery,
		BrainDir:   brainDir,
		KIStore:    kiStore,
		SandboxMgr: sbMgr,
	}).legacyFacade()
}

// ═══════════════════════════════════════════
// Legacy capability interfaces
// ═══════════════════════════════════════════

// LegacyChatAPI is the chat-oriented subset of the legacy facade.
type LegacyChatAPI interface {
	ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error
	SessionID(sessionID string) string
	StopRun(sessionID string)
	RetryRun(ctx context.Context, sessionID string) (string, error)
	Approve(approvalID string, approved bool) error
	ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error
	ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error
	ResumeRun(ctx context.Context, sessionID, runID string) error
	ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error)
}

// LegacyRuntimeAPI is the orchestration/runtime subset of the legacy facade.
type LegacyRuntimeAPI interface {
	ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error
	ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error
	ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error
	ResumeRun(ctx context.Context, sessionID, runID string) error
	ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error)
	ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	ListRuntimeRunsByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) ([]apitype.RuntimeRunInfo, error)
	ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	PendingDecision(ctx context.Context, sessionID, runID string) (*apitype.RuntimeRunInfo, error)
	ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error)
	ListRuntimeGraphByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) (apitype.OrchestrationGraphInfo, error)
	ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error)
}

// LegacySessionAPI is the session/history subset of the legacy facade.
type LegacySessionAPI interface {
	NewSession(title string) apitype.SessionResponse
	ListSessions() apitype.SessionListResponse
	SetSessionTitle(id, title string)
	DeleteSession(id string) error
	GetHistory(sessionID string) ([]apitype.HistoryMessage, error)
	ClearHistory()
	CompactContext()
}

// LegacyAdminAPI is the admin/config/tools subset of the legacy facade.
type LegacyAdminAPI interface {
	ListModels() apitype.ModelListResponse
	SwitchModel(name string) error
	CurrentModel() string
	GetConfig() map[string]any
	SetConfig(key string, value any) error
	AddProvider(p config.ProviderDef) error
	RemoveProvider(name string) error
	AddMCPServer(s config.MCPServerDef) error
	RemoveMCPServer(name string) error
	ListTools() []apitype.ToolInfoResponse
	EnableTool(name string) error
	DisableTool(name string) error
	Health() apitype.HealthResponse
	GetSecurity() apitype.SecurityResponse
	GetContextStats() apitype.ContextStats
	GetSystemInfo() apitype.SystemInfoResponse
	CronStatus() map[string]any
	ListCronJobs() ([]apitype.CronJobInfo, error)
	CreateCronJob(name, schedule, prompt string) error
	DeleteCronJob(name string) error
	EnableCronJob(name string) error
	DisableCronJob(name string) error
	RunCronJobNow(name string) error
	ListCronLogs(jobName string) ([]apitype.CronLogInfo, error)
	ReadCronLog(jobName, logFile string) (string, error)
	ListSkills() ([]apitype.SkillInfoResponse, error)
	ReadSkillContent(name string) (string, error)
	RefreshSkills() error
	DeleteSkill(name string) error
	ListMCPServers() ([]apitype.MCPServerInfo, error)
	ListMCPTools() ([]apitype.MCPToolInfo, error)
	ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error)
	ReadBrainArtifact(sessionID, name string) (string, error)
	ListKI() ([]apitype.KIInfo, error)
	GetKI(id string) (apitype.KIDetailResponse, error)
	DeleteKI(id string) error
	ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error)
	ReadKIArtifact(id, name string) (string, error)
	ListCapabilities(ctx context.Context) []apitype.CapabilityInfo
	RefreshCapabilities(ctx context.Context) error
}

// LegacyCostAPI is the token usage/cost subset of the legacy facade.
type LegacyCostAPI interface {
	SaveSessionCost(sessionID string) error
	GetSessionCost(sessionID string) (map[string]any, error)
}

// LegacyAPI is the full compatibility contract preserved by AgentAPI.
// It exists for backward compatibility only.
type LegacyAPI interface {
	LegacyChatAPI
	LegacyRuntimeAPI
	LegacySessionAPI
	LegacyAdminAPI
	LegacyCostAPI
}

// ═══════════════════════════════════════════
// AgentAPI — 1:1 delegation methods
// ═══════════════════════════════════════════

// --- Chat ---

func (a *AgentAPI) ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error {
	return a.chat.ChatStream(ctx, sessionID, message, mode, delta)
}

func (a *AgentAPI) SessionID(sessionID string) string { return a.chat.SessionID(sessionID) }
func (a *AgentAPI) StopRun(sessionID string)          { a.chat.StopRun(sessionID) }
func (a *AgentAPI) RetryRun(ctx context.Context, sessionID string) (string, error) {
	return a.chat.RetryRun(ctx, sessionID)
}
func (a *AgentAPI) Approve(approvalID string, approved bool) error {
	return a.chat.Approve(approvalID, approved)
}

// --- Runtime ---

func (a *AgentAPI) ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error {
	return a.runtime.ReviewPlan(ctx, sessionID, approved, feedback)
}
func (a *AgentAPI) ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error {
	return a.runtime.ApplyDecision(ctx, sessionID, kind, decision, feedback)
}
func (a *AgentAPI) ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error {
	return a.runtime.ApplyDecisionToRun(ctx, sessionID, runID, kind, decision, feedback)
}
func (a *AgentAPI) ResumeRun(ctx context.Context, sessionID, runID string) error {
	return a.runtime.ResumeRun(ctx, sessionID, runID)
}
func (a *AgentAPI) ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error) {
	return a.runtime.ApplyRuntimeIngress(ctx, req)
}
func (a *AgentAPI) ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListRuntimeRuns(ctx, sessionID)
}
func (a *AgentAPI) ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListPendingRuns(ctx, sessionID)
}
func (a *AgentAPI) ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListPendingDecisions(ctx, sessionID)
}
func (a *AgentAPI) PendingDecision(ctx context.Context, sessionID, runID string) (*apitype.RuntimeRunInfo, error) {
	return a.runtime.PendingDecision(ctx, sessionID, runID)
}
func (a *AgentAPI) ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error) {
	return a.runtime.ListRuntimeGraph(ctx, sessionID)
}
func (a *AgentAPI) ListRuntimeRunsByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListRuntimeRunsByEvent(ctx, sessionID, eventType, trigger, barrierID)
}
func (a *AgentAPI) ListRuntimeGraphByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) (apitype.OrchestrationGraphInfo, error) {
	return a.runtime.ListRuntimeGraphByEvent(ctx, sessionID, eventType, trigger, barrierID)
}
func (a *AgentAPI) ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListChildRuns(ctx, parentRunID)
}

// --- Session ---

func (a *AgentAPI) NewSession(title string) apitype.SessionResponse {
	return a.session.NewSession(title)
}
func (a *AgentAPI) ListSessions() apitype.SessionListResponse { return a.session.ListSessions() }
func (a *AgentAPI) DeleteSession(id string) error             { return a.session.DeleteSession(id) }
func (a *AgentAPI) SetSessionTitle(id, title string)          { a.session.SetSessionTitle(id, title) }
func (a *AgentAPI) GetHistory(sessionID string) ([]apitype.HistoryMessage, error) {
	return a.session.GetHistory(sessionID)
}
func (a *AgentAPI) ClearHistory()   { a.session.ClearHistory() }
func (a *AgentAPI) CompactContext() { a.session.CompactContext() }

// --- Admin ---

func (a *AgentAPI) ListModels() apitype.ModelListResponse        { return a.admin.ListModels() }
func (a *AgentAPI) SwitchModel(name string) error                { return a.admin.SwitchModel(name) }
func (a *AgentAPI) CurrentModel() string                         { return a.admin.CurrentModel() }
func (a *AgentAPI) GetConfig() map[string]any                    { return a.admin.GetConfig() }
func (a *AgentAPI) SetConfig(key string, value any) error        { return a.admin.SetConfig(key, value) }
func (a *AgentAPI) AddProvider(p config.ProviderDef) error       { return a.admin.AddProvider(p) }
func (a *AgentAPI) RemoveProvider(name string) error             { return a.admin.RemoveProvider(name) }
func (a *AgentAPI) AddMCPServer(s config.MCPServerDef) error     { return a.admin.AddMCPServer(s) }
func (a *AgentAPI) RemoveMCPServer(name string) error            { return a.admin.RemoveMCPServer(name) }
func (a *AgentAPI) ListTools() []apitype.ToolInfoResponse        { return a.admin.ListTools() }
func (a *AgentAPI) EnableTool(name string) error                 { return a.admin.EnableTool(name) }
func (a *AgentAPI) DisableTool(name string) error                { return a.admin.DisableTool(name) }
func (a *AgentAPI) Health() apitype.HealthResponse               { return a.admin.Health() }
func (a *AgentAPI) GetSecurity() apitype.SecurityResponse        { return a.admin.GetSecurity() }
func (a *AgentAPI) GetContextStats() apitype.ContextStats        { return a.admin.GetContextStats() }
func (a *AgentAPI) GetSystemInfo() apitype.SystemInfoResponse    { return a.admin.GetSystemInfo() }
func (a *AgentAPI) CronStatus() map[string]any                   { return a.admin.CronStatus() }
func (a *AgentAPI) ListCronJobs() ([]apitype.CronJobInfo, error) { return a.admin.ListCronJobs() }
func (a *AgentAPI) CreateCronJob(name, schedule, prompt string) error {
	return a.admin.CreateCronJob(name, schedule, prompt)
}
func (a *AgentAPI) DeleteCronJob(name string) error  { return a.admin.DeleteCronJob(name) }
func (a *AgentAPI) EnableCronJob(name string) error  { return a.admin.EnableCronJob(name) }
func (a *AgentAPI) DisableCronJob(name string) error { return a.admin.DisableCronJob(name) }
func (a *AgentAPI) RunCronJobNow(name string) error  { return a.admin.RunCronJobNow(name) }
func (a *AgentAPI) ListCronLogs(jobName string) ([]apitype.CronLogInfo, error) {
	return a.admin.ListCronLogs(jobName)
}
func (a *AgentAPI) ReadCronLog(jobName, logFile string) (string, error) {
	return a.admin.ReadCronLog(jobName, logFile)
}
func (a *AgentAPI) ListSkills() ([]apitype.SkillInfoResponse, error) { return a.admin.ListSkills() }
func (a *AgentAPI) ReadSkillContent(name string) (string, error) {
	return a.admin.ReadSkillContent(name)
}
func (a *AgentAPI) RefreshSkills() error                             { return a.admin.RefreshSkills() }
func (a *AgentAPI) DeleteSkill(name string) error                    { return a.admin.DeleteSkill(name) }
func (a *AgentAPI) ListMCPServers() ([]apitype.MCPServerInfo, error) { return a.admin.ListMCPServers() }
func (a *AgentAPI) ListMCPTools() ([]apitype.MCPToolInfo, error)     { return a.admin.ListMCPTools() }
func (a *AgentAPI) ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error) {
	return a.admin.ListBrainArtifacts(sessionID)
}
func (a *AgentAPI) ReadBrainArtifact(sessionID, name string) (string, error) {
	return a.admin.ReadBrainArtifact(sessionID, name)
}
func (a *AgentAPI) ListKI() ([]apitype.KIInfo, error) { return a.admin.ListKI() }
func (a *AgentAPI) GetKI(id string) (apitype.KIDetailResponse, error) {
	return a.admin.GetKI(id)
}
func (a *AgentAPI) DeleteKI(id string) error { return a.admin.DeleteKI(id) }
func (a *AgentAPI) ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error) {
	return a.admin.ListKIArtifacts(id)
}
func (a *AgentAPI) ReadKIArtifact(id, name string) (string, error) {
	return a.admin.ReadKIArtifact(id, name)
}
func (a *AgentAPI) ListCapabilities(ctx context.Context) []apitype.CapabilityInfo {
	return a.admin.ListCapabilities(ctx)
}
func (a *AgentAPI) RefreshCapabilities(ctx context.Context) error {
	return a.admin.RefreshCapabilities(ctx)
}

// --- Cost ---

func (a *AgentAPI) SaveSessionCost(sessionID string) error { return a.cost.SaveSessionCost(sessionID) }
func (a *AgentAPI) GetSessionCost(sessionID string) (map[string]any, error) {
	return a.cost.GetSessionCost(sessionID)
}

// Compile-time legacy interface checks.
var _ LegacyAPI = (*AgentAPI)(nil)
var _ LegacyChatAPI = (*ChatService)(nil)
var _ LegacyRuntimeAPI = (*RuntimeService)(nil)
var _ LegacySessionAPI = (*SessionService)(nil)
var _ LegacyAdminAPI = (*AdminService)(nil)
var _ LegacyCostAPI = (*CostService)(nil)
