package application

import (
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

// NewLegacyAPI creates the compatibility facade from the explicit
// ApplicationDeps bundle. New code should prefer ApplicationServices and its
// capability services directly.
func NewLegacyAPI(deps ApplicationDeps) LegacyAPI {
	return NewApplicationServices(deps).LegacyAPI()
}

// NewAgentAPI creates the legacy compatibility facade.
// Deprecated: prefer NewLegacyAPI(ApplicationDeps{...}) for compatibility paths,
// or NewApplicationServices(...) for new construction.
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
