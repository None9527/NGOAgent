// Package application provides the Builder that wires all dependencies.
package application

import (
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/a2a"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/notify" // P3 M1
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
	"gorm.io/gorm"
)

// App holds all initialized components.
type App struct {
	Config       *config.Manager
	DB           *gorm.DB
	Repo         *persistence.Repository
	Services     *ApplicationServices
	Router       *llm.Router
	Loop         *service.AgentLoop
	Factory      *service.LoopFactory
	Server       *server.Server
	GRPCServer   *grpcserver.Server
	ChatEngine   *service.ChatEngine
	SessionMgr   *service.SessionManager
	ModelMgr     *service.ModelManager
	ToolAdmin    *service.ToolAdmin
	CronMgr      *cron.Manager
	MCPMgr       *mcp.Manager
	SkillMgr     *skill.Manager
	SecurityHook *security.Hook
	SpawnTool    *tool.SpawnAgentTool             // exposed for server-side EventPusher wiring
	EventBus     *service.EventBus                // R3: event-driven orchestration
	A2AHandler   *a2a.Handler                     // R3: Agent-to-Agent protocol
	Discovery    *service.AggregatedToolDiscovery // R3: dynamic tool capability
	StopCh       chan struct{}
}

// Build initializes all 7 phases of NGOAgent in dependency order.
func Build() (*App, error) {
	// ═══════════════════════════════════════════
	// Phase 1: Foundation
	// ═══════════════════════════════════════════
	foundation, err := assembleFoundation()
	if err != nil {
		return nil, err
	}
	cfgMgr := foundation.cfgMgr
	cfg := foundation.cfg
	db := foundation.db
	repo := foundation.repo
	historyStore := foundation.historyStore
	snapshotStore := foundation.snapshotStore
	evoStore := foundation.evoStore
	transcriptStore := foundation.transcriptStore
	agentRegistry := foundation.agentRegistry

	// ═══════════════════════════════════════════
	// Phase 2: Core Infrastructure
	// ═══════════════════════════════════════════
	core := assembleCoreInfrastructure(cfg)
	router := core.router
	promptEngine := core.promptEngine
	workspaceDir := core.workspaceDir
	sbMgr := core.sandboxMgr
	secHook := core.securityHook

	// ═══════════════════════════════════════════
	// Phase 3: Storage Layer
	// ═══════════════════════════════════════════
	storage := assembleStorage(cfg, workspaceDir)
	sessionID := storage.sessionID
	brainDir := storage.brainDir
	brainStore := storage.brainStore
	kiStore := storage.kiStore
	kiRetriever := storage.kiRetriever
	memStore := storage.memStore
	diaryStore := storage.diaryStore
	wsStore := storage.workspaceStore
	fileHistory := storage.fileHistory
	skillMgr := storage.skillMgr
	mcpMgr := storage.mcpMgr

	// ═══════════════════════════════════════════
	// Phase 4: Tool Registration
	// ═══════════════════════════════════════════
	tools := assembleBuiltinTools(cfg, workspaceDir, brainDir, fileHistory, sbMgr, kiStore, kiRetriever, memStore, diaryStore, skillMgr)
	registry := tools.registry
	spawnTool := tools.spawn
	skillTool := tools.skill
	// manage_cron tool is registered after CronManager creation (Phase 7)

	// ═══════════════════════════════════════════
	// Phase 5: Engine + Facades (unified via LoopFactory)
	// ═══════════════════════════════════════════
	engine := assembleEngine(engineAssemblyInput{
		cfg:             cfg,
		sessionID:       sessionID,
		brainDir:        brainDir,
		router:          router,
		promptEngine:    promptEngine,
		registry:        registry,
		secHook:         secHook,
		sbMgr:           sbMgr,
		brainStore:      brainStore,
		kiStore:         kiStore,
		kiRetriever:     kiRetriever,
		wsStore:         wsStore,
		skillMgr:        skillMgr,
		historyStore:    historyStore,
		fileHistory:     fileHistory,
		memStore:        memStore,
		diaryStore:      diaryStore,
		snapshotStore:   snapshotStore,
		evoStore:        evoStore,
		transcriptStore: transcriptStore,
		repo:            repo,
		agentRegistry:   agentRegistry,
		spawnTool:       spawnTool,
		skillTool:       skillTool,
	})
	baseDeps := engine.baseDeps
	factory := engine.factory
	loop := engine.loop
	loopPool := engine.loopPool
	sessMgr := engine.sessionMgr
	chatEngine := engine.chatEngine
	modelMgr := engine.modelMgr
	toolAdmin := engine.toolAdmin

	// ═══════════════════════════════════════════
	// Phase 6: Hot-Reload Subscriptions
	// ═══════════════════════════════════════════
	registerHotReloadSubscriptions(cfgMgr, router, secHook, mcpMgr, registry, loop, func() *service.LoopPool {
		return loopPool
	})

	// NOTE: Approval flow uses PendingApproval registry (RequestApproval → Resolve via POST /v1/approve).
	// Legacy ApprovalFunc is no longer needed. Pending approvals block on a channel
	// until resolved by an external client (forge script, CLI, web UI).

	// Start config file watching
	if err := cfgMgr.StartWatching(); err != nil {
		slog.Info(fmt.Sprintf("Warning: config watch: %v", err))
	}

	// ═══════════════════════════════════════════
	// Phase 7: Background Services
	// ═══════════════════════════════════════════
	stopCh := make(chan struct{})

	cronMgr := assembleCronRuntime(cfg, baseDeps, registry)

	startRuntimeCapabilities(cfg, stopCh, registry, mcpMgr, skillMgr)

	// ═══════════════════════════════════════════
	// Phase 7.5: R3 Orchestration Wiring
	// ═══════════════════════════════════════════
	orchestration := assembleOrchestration(cfg, registry, mcpMgr, skillMgr)
	eventBus := orchestration.eventBus
	toolDiscovery := orchestration.discovery
	a2aHandler := orchestration.a2aHandler
	addr := orchestration.addr

	// ═══════════════════════════════════════════
	// Phase 8: Unified API + Server
	// ═══════════════════════════════════════════

	transports := assembleTransports(transportAssemblyInput{
		cfg:           cfg,
		cfgMgr:        cfgMgr,
		db:            db,
		snapshotStore: snapshotStore,
		historyStore:  historyStore,
		loop:          loop,
		loopPool:      loopPool,
		chatEngine:    chatEngine,
		sessionMgr:    sessMgr,
		modelMgr:      modelMgr,
		toolAdmin:     toolAdmin,
		secHook:       secHook,
		skillMgr:      skillMgr,
		cronMgr:       cronMgr,
		mcpMgr:        mcpMgr,
		discovery:     toolDiscovery,
		router:        router,
		brainDir:      brainDir,
		kiStore:       kiStore,
		sbMgr:         sbMgr,
		a2aHandler:    a2aHandler,
		httpAddr:      addr,
	})
	appServices := transports.services
	srv := transports.httpServer
	grpcSrv := transports.grpcServer

	return assembleApp(appAssemblyInput{
		cfgMgr:        cfgMgr,
		db:            db,
		repo:          repo,
		appServices:   appServices,
		router:        router,
		loop:          loop,
		factory:       factory,
		httpServer:    srv,
		grpcServer:    grpcSrv,
		chatEngine:    chatEngine,
		sessionMgr:    sessMgr,
		modelMgr:      modelMgr,
		toolAdmin:     toolAdmin,
		cronMgr:       cronMgr,
		mcpMgr:        mcpMgr,
		skillMgr:      skillMgr,
		secHook:       secHook,
		spawnTool:     spawnTool,
		eventBus:      eventBus,
		a2aHandler:    a2aHandler,
		toolDiscovery: toolDiscovery,
		stopCh:        stopCh,
	}), nil
}

// Shutdown gracefully shuts down all components.
func (app *App) Shutdown() {
	close(app.StopCh)
	app.Config.StopWatching()
	if app.EventBus != nil {
		app.EventBus.Close()
	}
	if app.Factory != nil {
		app.Factory.StopAll() // Cascade stop all active runs
	}
	if app.CronMgr != nil {
		app.CronMgr.Stop()
	}
	slog.Info(fmt.Sprint("[app] Shutdown complete"))
}

// Adapter types are in adapters.go

// buildWebhookHook creates a notify.Hook if webhooks are configured; returns nil otherwise.
// P3 M1.
func buildWebhookHook(cfg *config.Config, sessionID string) service.WebhookNotifyHook {
	if len(cfg.Notifications.Webhooks) == 0 {
		return nil
	}
	var targets []*notify.WebhookTarget
	for _, wh := range cfg.Notifications.Webhooks {
		if wh.URL == "" {
			continue
		}
		targets = append(targets, &notify.WebhookTarget{
			URL:    wh.URL,
			Events: wh.Events,
			Secret: wh.Secret,
			Retry:  wh.Retry,
		})
	}
	if len(targets) == 0 {
		return nil
	}
	n := notify.NewWebhookNotifier(targets, sessionID)
	return notify.NewHook(n)
}
