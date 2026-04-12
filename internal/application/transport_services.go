package application

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

type ChatService struct {
	commands        *ChatCommands
	runtimeCommands *RuntimeCommands
}

func (c *ChatService) ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error {
	return c.commands.ChatStream(ctx, sessionID, message, mode, delta)
}

func (c *ChatService) SessionID(sessionID string) string {
	return c.commands.SessionID(sessionID)
}

func (c *ChatService) StopRun(sessionID string) {
	c.commands.StopRun(sessionID)
}

func (c *ChatService) RetryRun(ctx context.Context, sessionID string) (string, error) {
	return c.commands.RetryRun(ctx, sessionID)
}

func (c *ChatService) Approve(approvalID string, approved bool) error {
	return c.commands.Approve(approvalID, approved)
}

func (c *ChatService) ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error {
	return c.ApplyDecisionToRun(ctx, sessionID, "", kind, decision, feedback)
}

func (c *ChatService) ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error {
	return c.runtimeCommands.ApplyDecisionToRun(ctx, sessionID, runID, kind, decision, feedback)
}

func (c *ChatService) ResumeRun(ctx context.Context, sessionID, runID string) error {
	return c.runtimeCommands.ResumeRun(ctx, sessionID, runID)
}

func (c *ChatService) ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error) {
	return c.runtimeCommands.ApplyRuntimeIngress(ctx, req)
}

type RuntimeService struct {
	commands *RuntimeCommands
	queries  *RuntimeQueries
}

func (r *RuntimeService) ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error {
	return r.commands.ReviewPlan(ctx, sessionID, approved, feedback)
}

func (r *RuntimeService) ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error {
	return r.ApplyDecisionToRun(ctx, sessionID, "", kind, decision, feedback)
}

func (r *RuntimeService) ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error {
	return r.commands.ApplyDecisionToRun(ctx, sessionID, runID, kind, decision, feedback)
}

func (r *RuntimeService) ResumeRun(ctx context.Context, sessionID, runID string) error {
	return r.commands.ResumeRun(ctx, sessionID, runID)
}

func (r *RuntimeService) ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error) {
	return r.commands.ApplyRuntimeIngress(ctx, req)
}

func (r *RuntimeService) ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return r.queries.ListRuntimeRuns(ctx, sessionID)
}

func (r *RuntimeService) ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return r.queries.ListPendingRuns(ctx, sessionID)
}

func (r *RuntimeService) ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	return r.queries.ListPendingDecisions(ctx, sessionID)
}

func (r *RuntimeService) PendingDecision(ctx context.Context, sessionID, runID string) (*apitype.RuntimeRunInfo, error) {
	return r.queries.PendingDecision(ctx, sessionID, runID)
}

func (r *RuntimeService) ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error) {
	return r.queries.ListRuntimeGraph(ctx, sessionID)
}

func (r *RuntimeService) ListRuntimeRunsByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) ([]apitype.RuntimeRunInfo, error) {
	return r.queries.ListRuntimeRunsByEvent(ctx, sessionID, eventType, trigger, barrierID)
}

func (r *RuntimeService) ListRuntimeGraphByEvent(ctx context.Context, sessionID, eventType, trigger, barrierID string) (apitype.OrchestrationGraphInfo, error) {
	return r.queries.ListRuntimeGraphByEvent(ctx, sessionID, eventType, trigger, barrierID)
}

func (r *RuntimeService) ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error) {
	return r.queries.ListChildRuns(ctx, parentRunID)
}

type SessionService struct {
	commands *SessionCommands
	queries  *SessionQueries
}

func (s *SessionService) NewSession(title string) apitype.SessionResponse {
	return s.queries.NewSession(title)
}

func (s *SessionService) ListSessions() apitype.SessionListResponse {
	return s.queries.ListSessions()
}

func (s *SessionService) SetSessionTitle(id, title string) {
	s.commands.SetSessionTitle(id, title)
}

func (s *SessionService) DeleteSession(id string) error {
	return s.commands.DeleteSession(id)
}

func (s *SessionService) GetHistory(sessionID string) ([]apitype.HistoryMessage, error) {
	return s.queries.GetHistory(sessionID)
}

func (s *SessionService) ClearHistory() {
	s.commands.ClearHistory()
}

func (s *SessionService) CompactContext() {
	s.commands.CompactContext()
}

type AdminService struct {
	commands *AdminCommands
	queries  *AdminQueries
}

func (a *AdminService) ListModels() apitype.ModelListResponse {
	return a.queries.ListModels()
}

func (a *AdminService) SwitchModel(name string) error {
	return a.commands.SwitchModel(name)
}

func (a *AdminService) CurrentModel() string {
	return a.queries.CurrentModel()
}

func (a *AdminService) GetConfig() map[string]any {
	return a.queries.GetConfig()
}

func (a *AdminService) SetConfig(key string, value any) error {
	return a.commands.SetConfig(key, value)
}

func (a *AdminService) AddProvider(provider config.ProviderDef) error {
	return a.commands.AddProvider(provider)
}

func (a *AdminService) RemoveProvider(name string) error {
	return a.commands.RemoveProvider(name)
}

func (a *AdminService) AddMCPServer(server config.MCPServerDef) error {
	return a.commands.AddMCPServer(server)
}

func (a *AdminService) RemoveMCPServer(name string) error {
	return a.commands.RemoveMCPServer(name)
}

func (a *AdminService) ListTools() []apitype.ToolInfoResponse {
	return a.queries.ListTools()
}

func (a *AdminService) EnableTool(name string) error {
	return a.commands.EnableTool(name)
}

func (a *AdminService) DisableTool(name string) error {
	return a.commands.DisableTool(name)
}

func (a *AdminService) ListSkills() ([]apitype.SkillInfoResponse, error) {
	return a.queries.ListSkills()
}

func (a *AdminService) ReadSkillContent(name string) (string, error) {
	return a.queries.ReadSkillContent(name)
}

func (a *AdminService) RefreshSkills() error {
	return a.commands.RefreshSkills()
}

func (a *AdminService) DeleteSkill(name string) error {
	return a.commands.DeleteSkill(name)
}

func (a *AdminService) ListMCPServers() ([]apitype.MCPServerInfo, error) {
	return a.queries.ListMCPServers()
}

func (a *AdminService) ListMCPTools() ([]apitype.MCPToolInfo, error) {
	return a.queries.ListMCPTools()
}

func (a *AdminService) ListCapabilities(ctx context.Context) []apitype.CapabilityInfo {
	return a.queries.ListCapabilities(ctx)
}

func (a *AdminService) RefreshCapabilities(ctx context.Context) error {
	return a.commands.RefreshCapabilities(ctx)
}

func (a *AdminService) Health() apitype.HealthResponse {
	return a.queries.Health()
}

func (a *AdminService) GetSecurity() apitype.SecurityResponse {
	return a.queries.GetSecurity()
}

func (a *AdminService) GetContextStats() apitype.ContextStats {
	return a.queries.GetContextStats()
}

func (a *AdminService) GetSystemInfo() apitype.SystemInfoResponse {
	return a.queries.GetSystemInfo()
}

func (a *AdminService) CronStatus() map[string]any {
	return a.queries.CronStatus()
}

func (a *AdminService) ListCronJobs() ([]apitype.CronJobInfo, error) {
	return a.queries.ListCronJobs()
}

func (a *AdminService) CreateCronJob(name, schedule, prompt string) error {
	return a.commands.CreateCronJob(name, schedule, prompt)
}

func (a *AdminService) DeleteCronJob(name string) error {
	return a.commands.DeleteCronJob(name)
}

func (a *AdminService) EnableCronJob(name string) error {
	return a.commands.EnableCronJob(name)
}

func (a *AdminService) DisableCronJob(name string) error {
	return a.commands.DisableCronJob(name)
}

func (a *AdminService) RunCronJobNow(name string) error {
	return a.commands.RunCronJobNow(name)
}

func (a *AdminService) ListCronLogs(jobName string) ([]apitype.CronLogInfo, error) {
	return a.queries.ListCronLogs(jobName)
}

func (a *AdminService) ReadCronLog(jobName, logFile string) (string, error) {
	return a.queries.ReadCronLog(jobName, logFile)
}

func (a *AdminService) ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error) {
	return a.queries.ListBrainArtifacts(sessionID)
}

func (a *AdminService) ReadBrainArtifact(sessionID, name string) (string, error) {
	return a.queries.ReadBrainArtifact(sessionID, name)
}

func (a *AdminService) ListKI() ([]apitype.KIInfo, error) {
	return a.queries.ListKI()
}

func (a *AdminService) GetKI(id string) (apitype.KIDetailResponse, error) {
	return a.queries.GetKI(id)
}

func (a *AdminService) DeleteKI(id string) error {
	return a.commands.DeleteKI(id)
}

func (a *AdminService) ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error) {
	return a.queries.ListKIArtifacts(id)
}

func (a *AdminService) ReadKIArtifact(id, name string) (string, error) {
	return a.queries.ReadKIArtifact(id, name)
}

type CostService struct {
	kernel *ApplicationKernel
}

func (c *CostService) SaveSessionCost(sessionID string) error {
	return c.kernel.saveSessionCost(sessionID)
}

func (c *CostService) GetSessionCost(sessionID string) (map[string]any, error) {
	return c.kernel.sessionCost(sessionID)
}
