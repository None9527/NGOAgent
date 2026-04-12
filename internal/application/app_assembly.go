package application

import "github.com/ngoclaw/ngoagent/internal/infrastructure/cron"

type appAssemblyInput struct {
	foundation    foundationAssembly
	core          coreInfrastructureAssembly
	storage       storageAssembly
	tools         assembledTools
	engine        engineAssembly
	orchestration orchestrationAssembly
	transports    transportAssembly
	cronMgr       *cron.Manager
	stopCh        chan struct{}
}

func assembleApp(in appAssemblyInput) *App {
	return &App{
		Config:       in.foundation.cfgMgr,
		DB:           in.foundation.db,
		Repo:         in.foundation.repo,
		Services:     in.transports.services,
		Router:       in.core.router,
		Loop:         in.engine.loop,
		Factory:      in.engine.factory,
		Server:       in.transports.httpServer,
		GRPCServer:   in.transports.grpcServer,
		ChatEngine:   in.engine.chatEngine,
		SessionMgr:   in.engine.sessionMgr,
		ModelMgr:     in.engine.modelMgr,
		ToolAdmin:    in.engine.toolAdmin,
		CronMgr:      in.cronMgr,
		MCPMgr:       in.storage.mcpMgr,
		SkillMgr:     in.storage.skillMgr,
		SecurityHook: in.core.securityHook,
		SpawnTool:    in.tools.spawn,
		EventBus:     in.orchestration.eventBus,
		A2AHandler:   in.orchestration.a2aHandler,
		Discovery:    in.orchestration.discovery,
		StopCh:       in.stopCh,
	}
}
