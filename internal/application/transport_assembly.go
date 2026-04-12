package application

import (
	"fmt"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
)

type transportAssemblyInput struct {
	foundation    foundationAssembly
	core          coreInfrastructureAssembly
	storage       storageAssembly
	engine        engineAssembly
	orchestration orchestrationAssembly
	cronMgr       *cron.Manager
}

type transportAssembly struct {
	services   *ApplicationServices
	httpServer *server.Server
	grpcServer *grpcserver.Server
}

func assembleTransports(in transportAssemblyInput) transportAssembly {
	appServices := NewApplicationServices(ApplicationDeps{
		Loop:       in.engine.loop,
		LoopPool:   in.engine.loopPool,
		ChatEngine: in.engine.chatEngine,
		SessionMgr: in.engine.sessionMgr,
		ModelMgr:   in.engine.modelMgr,
		ToolAdmin:  in.engine.toolAdmin,
		SecHook:    in.core.securityHook,
		SkillMgr:   in.storage.skillMgr,
		CronMgr:    in.cronMgr,
		MCPMgr:     in.storage.mcpMgr,
		Discovery:  in.orchestration.discovery,
		Config:     in.foundation.cfgMgr,
		Router:     in.core.router,
		HistQuery:  &historyAdapter{store: in.foundation.historyStore},
		BrainDir:   in.storage.brainDir,
		KIStore:    in.storage.kiStore,
		SandboxMgr: in.core.sandboxMgr,
		Wiring: ServiceWiring{
			TokenUsageStore: persistence.NewTokenUsageStore(in.foundation.db),
			RuntimeStore:    in.foundation.snapshotStore,
		},
	})

	httpCaps := appServices.HTTPTransport()
	httpCaps.A2A = in.orchestration.a2aHandler
	httpServer := server.NewServer(httpCaps, in.orchestration.addr, in.foundation.cfg.Server.AuthToken)

	grpcAddr := ":19998"
	if in.foundation.cfg.Server.GRPCPort != 0 {
		grpcAddr = fmt.Sprintf(":%d", in.foundation.cfg.Server.GRPCPort)
	}
	grpcServer := grpcserver.NewServer(appServices.GRPCTransport(), grpcAddr)

	return transportAssembly{
		services:   appServices,
		httpServer: httpServer,
		grpcServer: grpcServer,
	}
}
