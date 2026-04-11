package application

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

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
func (a *AgentAPI) ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error) {
	return a.runtime.ListChildRuns(ctx, parentRunID)
}

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

func (a *AgentAPI) SaveSessionCost(sessionID string) error {
	return a.cost.SaveSessionCost(sessionID)
}
func (a *AgentAPI) GetSessionCost(sessionID string) (map[string]any, error) {
	return a.cost.GetSessionCost(sessionID)
}
