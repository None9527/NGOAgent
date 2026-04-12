package application

import (
	"fmt"

	"github.com/ngoclaw/ngoagent/internal/domain/a2a"
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
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
	"gorm.io/gorm"
)

type transportAssemblyInput struct {
	cfg           *config.Config
	cfgMgr        *config.Manager
	db            *gorm.DB
	snapshotStore *persistence.RunSnapshotStore
	historyStore  *persistence.HistoryStore
	loop          *service.AgentLoop
	loopPool      *service.LoopPool
	chatEngine    *service.ChatEngine
	sessionMgr    *service.SessionManager
	modelMgr      *service.ModelManager
	toolAdmin     *service.ToolAdmin
	secHook       *security.Hook
	skillMgr      *skill.Manager
	cronMgr       *cron.Manager
	mcpMgr        *mcp.Manager
	discovery     *service.AggregatedToolDiscovery
	router        *llm.Router
	brainDir      string
	kiStore       *knowledge.Store
	sbMgr         *sandbox.Manager
	a2aHandler    *a2a.Handler
	httpAddr      string
}

type transportAssembly struct {
	services   *ApplicationServices
	httpServer *server.Server
	grpcServer *grpcserver.Server
}

func assembleTransports(in transportAssemblyInput) transportAssembly {
	appServices := NewApplicationServices(ApplicationDeps{
		Loop:       in.loop,
		LoopPool:   in.loopPool,
		ChatEngine: in.chatEngine,
		SessionMgr: in.sessionMgr,
		ModelMgr:   in.modelMgr,
		ToolAdmin:  in.toolAdmin,
		SecHook:    in.secHook,
		SkillMgr:   in.skillMgr,
		CronMgr:    in.cronMgr,
		MCPMgr:     in.mcpMgr,
		Discovery:  in.discovery,
		Config:     in.cfgMgr,
		Router:     in.router,
		HistQuery:  &historyAdapter{store: in.historyStore},
		BrainDir:   in.brainDir,
		KIStore:    in.kiStore,
		SandboxMgr: in.sbMgr,
		Wiring: ServiceWiring{
			TokenUsageStore: persistence.NewTokenUsageStore(in.db),
			RuntimeStore:    in.snapshotStore,
		},
	})

	httpCaps := appServices.HTTPTransport()
	httpCaps.A2A = in.a2aHandler
	httpServer := server.NewServer(httpCaps, in.httpAddr, in.cfg.Server.AuthToken)

	grpcAddr := ":19998"
	if in.cfg.Server.GRPCPort != 0 {
		grpcAddr = fmt.Sprintf(":%d", in.cfg.Server.GRPCPort)
	}
	grpcServer := grpcserver.NewServer(appServices.GRPCTransport(), grpcAddr)

	return transportAssembly{
		services:   appServices,
		httpServer: httpServer,
		grpcServer: grpcServer,
	}
}
