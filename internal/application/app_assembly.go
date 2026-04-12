package application

import (
	"github.com/ngoclaw/ngoagent/internal/domain/a2a"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
	"gorm.io/gorm"
)

type appAssemblyInput struct {
	cfgMgr        *config.Manager
	db            *gorm.DB
	repo          *persistence.Repository
	appServices   *ApplicationServices
	router        *llm.Router
	loop          *service.AgentLoop
	factory       *service.LoopFactory
	httpServer    *server.Server
	grpcServer    *grpcserver.Server
	chatEngine    *service.ChatEngine
	sessionMgr    *service.SessionManager
	modelMgr      *service.ModelManager
	toolAdmin     *service.ToolAdmin
	cronMgr       *cron.Manager
	mcpMgr        *mcp.Manager
	skillMgr      *skill.Manager
	secHook       *security.Hook
	spawnTool     *tool.SpawnAgentTool
	eventBus      *service.EventBus
	a2aHandler    *a2a.Handler
	toolDiscovery *service.AggregatedToolDiscovery
	stopCh        chan struct{}
}

func assembleApp(in appAssemblyInput) *App {
	return &App{
		Config:       in.cfgMgr,
		DB:           in.db,
		Repo:         in.repo,
		Services:     in.appServices,
		Router:       in.router,
		Loop:         in.loop,
		Factory:      in.factory,
		Server:       in.httpServer,
		GRPCServer:   in.grpcServer,
		ChatEngine:   in.chatEngine,
		SessionMgr:   in.sessionMgr,
		ModelMgr:     in.modelMgr,
		ToolAdmin:    in.toolAdmin,
		CronMgr:      in.cronMgr,
		MCPMgr:       in.mcpMgr,
		SkillMgr:     in.skillMgr,
		SecurityHook: in.secHook,
		SpawnTool:    in.spawnTool,
		EventBus:     in.eventBus,
		A2AHandler:   in.a2aHandler,
		Discovery:    in.toolDiscovery,
		StopCh:       in.stopCh,
	}
}
