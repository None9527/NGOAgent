package application

import "time"

func newApplicationKernel(deps ApplicationDeps) *ApplicationKernel {
	return &ApplicationKernel{
		loop:            deps.Loop,
		loopPool:        deps.LoopPool,
		chatEngine:      deps.ChatEngine,
		sessMgr:         deps.SessionMgr,
		modelMgr:        deps.ModelMgr,
		toolAdmin:       deps.ToolAdmin,
		secHook:         deps.SecHook,
		skillMgr:        deps.SkillMgr,
		cronMgr:         deps.CronMgr,
		mcpMgr:          deps.MCPMgr,
		cfg:             deps.Config,
		router:          deps.Router,
		histQuery:       deps.HistQuery,
		brainDir:        deps.BrainDir,
		kiStore:         deps.KIStore,
		sandboxMgr:      deps.SandboxMgr,
		tokenUsageStore: deps.Wiring.TokenUsageStore,
		runtimeStore:    deps.Wiring.RuntimeStore,
		startedAt:       time.Now(),
	}
}

func buildApplicationServices(kernel *ApplicationKernel) *ApplicationServices {
	facades := newApplicationFacades(kernel)

	return &ApplicationServices{
		chatService: &ChatService{commands: facades.chatCommands, runtimeCommands: facades.runtimeCommands},
		runtime:     &RuntimeService{commands: facades.runtimeCommands, queries: facades.runtimeQueries},
		session:     &SessionService{commands: facades.sessionCommands, queries: facades.sessionQueries},
		admin:       &AdminService{commands: facades.adminCommands, queries: facades.adminQueries},
		cost:        &CostService{kernel: kernel},
	}
}
