package application

import (
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

// ApplicationDeps captures the full construction dependency set for
// ApplicationServices. It is the primary bundle used to assemble the provider,
// including optional infrastructure wiring.
type ApplicationDeps struct {
	Loop       *service.AgentLoop
	LoopPool   *service.LoopPool
	ChatEngine *service.ChatEngine
	SessionMgr *service.SessionManager
	ModelMgr   *service.ModelManager
	ToolAdmin  *service.ToolAdmin
	SecHook    *security.Hook
	SkillMgr   *skill.Manager
	CronMgr    *cron.Manager
	MCPMgr     *mcp.Manager
	Config     *config.Manager
	Router     *llm.Router
	HistQuery  HistoryQuerier
	BrainDir   string
	KIStore    *knowledge.Store
	SandboxMgr *sandbox.Manager
	Wiring     ServiceWiring
}

// ServiceWiring captures optional infrastructure dependencies that should be
// bound at application-service construction time rather than via follow-up setters.
type ServiceWiring struct {
	TokenUsageStore *persistence.TokenUsageStore
	RuntimeStore    *persistence.RunSnapshotStore
}
