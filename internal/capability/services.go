package capability

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

// Chat is the chat-oriented application capability contract.
type Chat interface {
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

// ChatControl is the narrower chat capability used by transports that do not
// expose the full runtime/orchestration control surface.
type ChatControl interface {
	ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error
	SessionID(sessionID string) string
	StopRun(sessionID string)
	Approve(approvalID string, approved bool) error
}

// Runtime is the orchestration/runtime application capability contract.
type Runtime interface {
	ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error
	ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error
	ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error
	ResumeRun(ctx context.Context, sessionID, runID string) error
	ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error)
	ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error)
	PendingDecision(ctx context.Context, sessionID, runID string) (*apitype.RuntimeRunInfo, error)
	ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error)
	ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error)
}

// Session is the session/history application capability contract.
type Session interface {
	NewSession(title string) apitype.SessionResponse
	ListSessions() apitype.SessionListResponse
	SetSessionTitle(id, title string)
	DeleteSession(id string) error
	GetHistory(sessionID string) ([]apitype.HistoryMessage, error)
	ClearHistory()
	CompactContext()
}

// Admin is the admin/config/tools application capability contract.
type Admin interface {
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
}

// Cost is the token usage/cost application capability contract.
type Cost interface {
	SaveSessionCost(sessionID string) error
	GetSessionCost(sessionID string) (map[string]any, error)
}

// HTTP is the full HTTP transport capability contract.
type HTTP interface {
	Chat
	Session
	Admin
	Runtime
	Cost
}

// GRPC is the narrower gRPC transport capability contract.
type GRPC interface {
	ChatControl
	Session
	Admin
}
