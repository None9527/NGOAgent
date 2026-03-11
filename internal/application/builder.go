// Package application provides the Builder that wires all dependencies.
package application

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/heartbeat"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/openai"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
	"gorm.io/gorm"
)

// App holds all initialized components.
type App struct {
	Config       *config.Manager
	DB           *gorm.DB
	Repo         *persistence.Repository
	Router       *llm.Router
	Loop         *service.AgentLoop
	Factory      *service.LoopFactory
	Server       *server.Server
	ChatEngine   *service.ChatEngine
	SessionMgr   *service.SessionManager
	ModelMgr     *service.ModelManager
	ToolAdmin    *service.ToolAdmin
	HeartbeatEng *heartbeat.Engine
	MCPMgr       *mcp.Manager
	SkillMgr     *skill.Manager
	SecurityHook *security.Hook
	StopCh       chan struct{}
}

// Build initializes all 7 phases of NGOAgent in dependency order.
func Build() (*App, error) {
	// ═══════════════════════════════════════════
	// Phase 1: Foundation
	// ═══════════════════════════════════════════
	if err := config.Bootstrap(); err != nil {
		log.Printf("Warning: bootstrap: %v", err)
	}
	cfgMgr := config.NewManager(config.ConfigPath())
	cfg := cfgMgr.Get()

	dbPath := config.ResolvePath(cfg.Storage.DBPath)
	db, err := persistence.Open(dbPath)
	if err != nil {
		return nil, err
	}
	repo := persistence.NewRepository(db)
	historyStore := persistence.NewHistoryStore(db)
	_ = historyStore // Future: wire into session state

	// ═══════════════════════════════════════════
	// Phase 2: Core Infrastructure
	// ═══════════════════════════════════════════
	var providers []llm.Provider
	for _, pd := range cfg.LLM.Providers {
		providers = append(providers, openai.NewClient(pd.Name, pd.BaseURL, pd.APIKey, pd.Models))
	}
	router := llm.NewRouter(providers)

	homeDir := config.HomeDir()
	promptEngine := prompt.NewEngineWithHome(homeDir)
	sbMgr := sandbox.NewManager()
	secHook := security.NewHook(&cfg.Security, &cfg.Heartbeat.Security)

	// ═══════════════════════════════════════════
	// Phase 3: Storage Layer
	// ═══════════════════════════════════════════
	sessionID := generateSessionID()
	brainDir := config.ResolvePath(cfg.Storage.BrainDir)
	brainStore := brain.NewArtifactStore(brainDir, sessionID)

	kiDir := config.ResolvePath(cfg.Storage.KnowledgeDir)
	kiStore := knowledge.NewStore(kiDir)

	workDir, _ := os.Getwd()
	wsStore := workspace.NewStore(workDir)

	skillsDir := config.ResolvePath(cfg.Storage.SkillsDir)
	skillMgr := skill.NewManager(skillsDir)

	mcpMgr := mcp.NewManager()

	// ═══════════════════════════════════════════
	// Phase 4: Tool Registration
	// ═══════════════════════════════════════════
	registry := tool.NewRegistry()
	registry.Register(&tool.ReadFileTool{})
	registry.Register(&tool.WriteFileTool{})
	registry.Register(&tool.EditFileTool{})
	registry.Register(&tool.GlobTool{})
	registry.Register(&tool.GrepSearchTool{})
	registry.Register(tool.NewRunCommandTool(sbMgr))
	registry.Register(tool.NewCommandStatusTool(sbMgr))
	registry.Register(tool.NewWebSearchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewWebFetchTool())
	registry.Register(tool.NewTaskPlanTool(brainDir))
	registry.Register(tool.NewTaskBoundaryTool())
	registry.Register(tool.NewNotifyUserTool())
	registry.Register(&tool.UpdateProjectContextTool{})
	registry.Register(tool.NewSaveMemoryTool(homeDir))
	registry.Register(tool.NewSendMessageTool(brainDir))
	registry.Register(tool.NewTaskListTool(brainDir))
	spawnTool := tool.NewSpawnAgentTool(nil) // Lazy: SpawnFunc set after loop creation
	registry.Register(spawnTool)
	registry.Register(tool.NewForgeTool(cfg.Forge.SandboxDir))
	brainArtifactTool := tool.NewBrainArtifactTool(nil) // Lazy: Brain set per-session
	registry.Register(brainArtifactTool)

	// ═══════════════════════════════════════════
	// Phase 5: Engine + Facades (unified via LoopFactory)
	// ═══════════════════════════════════════════
	baseDeps := service.Deps{
		Config:       cfg,
		ConfigMgr:    cfgMgr,
		LLMRouter:    router,
		PromptEngine: promptEngine,
		ToolExec:     registry,
		Security:     secHook,
		Delta:        &service.Delta{}, // Overridden per-channel
		Brain:        brainStore,
		KIStore:      kiStore,
		Workspace:    wsStore,
		SkillMgr:     skillMgr,
	}
	factory := service.NewLoopFactory(baseDeps, 8) // max 8 concurrent runs

	// Main chat loop (backward-compat: server still uses loop directly)
	delta := &service.Delta{}
	loop := service.NewAgentLoop(baseDeps)
	loop.SetDelta(delta)

	// Wire SpawnFunc via factory — creates independent subagent loops
	spawnTool.SetSpawnFunc(func(ctx context.Context, task string) (string, error) {
		ch := service.NewSubagentChannel(nil) // Sync mode: no announce callback
		run := factory.Create(sessionID, ch)
		if err := factory.RunSync(ctx, run, task); err != nil {
			return ch.Result(), err
		}
		return ch.Result(), nil
	})

	// Per-session loop pool (uses factory baseDeps)
	loopPool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(baseDeps)
	}, brainDir)

	sessMgr := service.NewSessionManager(repo)
	chatEngine := service.NewChatEngine(loop, sessMgr)
	modelMgr := service.NewModelManager(router)
	toolAdmin := service.NewToolAdmin(registry)
	_ = chatEngine // Used by server routes

	// ═══════════════════════════════════════════
	// Phase 6: Hot-Reload Subscriptions
	// ═══════════════════════════════════════════
	cfgMgr.Subscribe("llm", func(old, new *config.Config) {
		log.Println("[hot-reload] LLM config changed, rebuilding providers")
		var newProviders []llm.Provider
		for _, pd := range new.LLM.Providers {
			newProviders = append(newProviders, openai.NewClient(pd.Name, pd.BaseURL, pd.APIKey, pd.Models))
		}
		router.Reload(newProviders)
	})

	cfgMgr.Subscribe("security", func(old, new *config.Config) {
		log.Println("[hot-reload] Security config changed")
		secHook.ReloadChain(&new.Security)
	})

	cfgMgr.Subscribe("mcp", func(old, new *config.Config) {
		log.Println("[hot-reload] MCP config changed, reloading servers")
		var configs []mcp.ServerConfig
		for _, s := range new.MCP.Servers {
			configs = append(configs, mcp.ServerConfig{
				Name:    s.Name,
				Command: s.Command,
				Args:    s.Args,
			})
		}
		mcpMgr.Reload(context.Background(), configs)
	})

	cfgMgr.Subscribe("agent", func(old, new *config.Config) {
		log.Printf("[hot-reload] Agent config changed: planning=%v",
			new.Agent.PlanningMode)
	})

	// NOTE: Approval flow uses PendingApproval registry (RequestApproval → Resolve via POST /v1/approve).
	// Legacy ApprovalFunc is no longer needed. Pending approvals block on a channel
	// until resolved by an external client (forge script, CLI, web UI).

	// Start config file watching
	if err := cfgMgr.StartWatching(); err != nil {
		log.Printf("Warning: config watch: %v", err)
	}

	// ═══════════════════════════════════════════
	// Phase 7: Background Services
	// ═══════════════════════════════════════════
	stopCh := make(chan struct{})

	// Start heartbeat engine (if configured)
	// Heartbeat uses its own independent AgentLoop via LoopFactory.
	hbRun := factory.Create(sessionID, service.NewHeartbeatChannel(nil))
	hbEngine := heartbeat.NewEngine(&cfg.Heartbeat, hbRun.Loop, func() string {
		return wsStore.ReadHeartbeat(config.ResolvePath("heartbeat.md"))
	})
	if cfg.Heartbeat.Enabled {
		go hbEngine.Start(context.Background())
		log.Printf("[heartbeat] Started with interval %s (independent loop via factory)", cfg.Heartbeat.Interval)
	}

	// Start skill file watcher
	skillMgr.StartWatcher(stopCh)

	// Start MCP servers
	var mcpConfigs []mcp.ServerConfig
	for _, s := range cfg.MCP.Servers {
		mcpConfigs = append(mcpConfigs, mcp.ServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
		})
	}
	if len(mcpConfigs) > 0 {
		mcpMgr.StartAll(context.Background(), mcpConfigs)
		// Auto-register MCP-discovered tools into agent registry
		tool.RegisterMCPTools(registry, mcpMgr)
	}
	// Auto-promote executable skills into tool registry
	for _, sk := range skillMgr.AutoPromote() {
		registry.Register(tool.NewScriptTool(sk))
		log.Printf("[skill] Auto-promoted: %s", sk.Name)
	}

	// ═══════════════════════════════════════════
	// Phase 8: Server
	// ═══════════════════════════════════════════
	addr := ":8080"
	if cfg.Server.HTTPPort != 0 {
		addr = fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	}
	srv := server.NewServer(loop, router, cfgMgr, addr)
	srv.SetManagers(sessMgr, toolAdmin, secHook)
	srv.SetLoopPool(loopPool)
	srv.SetSkillMgr(skillMgr)

	return &App{
		Config:       cfgMgr,
		DB:           db,
		Repo:         repo,
		Router:       router,
		Loop:         loop,
		Factory:      factory,
		Server:       srv,
		ChatEngine:   chatEngine,
		SessionMgr:   sessMgr,
		ModelMgr:     modelMgr,
		ToolAdmin:    toolAdmin,
		HeartbeatEng: hbEngine,
		MCPMgr:       mcpMgr,
		SkillMgr:     skillMgr,
		SecurityHook: secHook,
		StopCh:       stopCh,
	}, nil
}

// Shutdown gracefully shuts down all components.
func (app *App) Shutdown() {
	close(app.StopCh)
	app.Config.StopWatching()
	if app.Factory != nil {
		app.Factory.StopAll() // Cascade stop all active runs
	}
	if app.HeartbeatEng != nil {
		app.HeartbeatEng.Stop()
	}
	log.Println("[app] Shutdown complete")
}

func generateSessionID() string {
	return uuid.New().String()
}
