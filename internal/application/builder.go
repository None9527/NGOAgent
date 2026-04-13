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
	cfg := foundation.cfg

	// ═══════════════════════════════════════════
	// Phase 2: Core Infrastructure
	// ═══════════════════════════════════════════
	core := assembleCoreInfrastructure(cfg)

	// ═══════════════════════════════════════════
	// Phase 3: Storage Layer
	// ═══════════════════════════════════════════
	storage := assembleStorage(cfg, core.workspaceDir)

	// ═══════════════════════════════════════════
	// Phase 4: Tool Registration
	// ═══════════════════════════════════════════
	tools := assembleBuiltinTools(
		cfg,
		core.workspaceDir,
		storage.brainDir,
		storage.fileHistory,
		core.sandboxMgr,
		storage.kiStore,
		storage.kiRetriever,
		storage.memStore,
		storage.diaryStore,
		storage.skillMgr,
	)
	// manage_cron tool is registered after CronManager creation (Phase 7)

	// ═══════════════════════════════════════════
	// Phase 5: Engine + Facades (unified via LoopFactory)
	// ═══════════════════════════════════════════
	engine := assembleEngine(engineAssemblyInput{
		cfg:             cfg,
		sessionID:       storage.sessionID,
		brainDir:        storage.brainDir,
		router:          core.router,
		promptEngine:    core.promptEngine,
		registry:        tools.registry,
		secHook:         core.securityHook,
		sbMgr:           core.sandboxMgr,
		brainStore:      storage.brainStore,
		kiStore:         storage.kiStore,
		kiRetriever:     storage.kiRetriever,
		wsStore:         storage.workspaceStore,
		skillMgr:        storage.skillMgr,
		historyStore:    foundation.historyStore,
		fileHistory:     storage.fileHistory,
		memStore:        storage.memStore,
		diaryStore:      storage.diaryStore,
		snapshotStore:   foundation.snapshotStore,
		evoStore:        foundation.evoStore,
		transcriptStore: foundation.transcriptStore,
		repo:            foundation.repo,
		agentRegistry:   foundation.agentRegistry,
		spawnTool:       tools.spawn,
		skillTool:       tools.skill,
	})

	// ═══════════════════════════════════════════
	// Phase 6: Hot-Reload Subscriptions
	// ═══════════════════════════════════════════
	registerHotReloadSubscriptions(foundation.cfgMgr, core.router, core.securityHook, storage.mcpMgr, tools.registry, engine.loop, func() *service.LoopPool {
		return engine.loopPool
	})

	// NOTE: Approval flow uses PendingApproval registry (RequestApproval → Resolve via POST /v1/approve).
	// Legacy ApprovalFunc is no longer needed. Pending approvals block on a channel
	// until resolved by an external client (forge script, CLI, web UI).

	// Start config file watching
	if err := foundation.cfgMgr.StartWatching(); err != nil {
		slog.Info(fmt.Sprintf("Warning: config watch: %v", err))
	}

	// ═══════════════════════════════════════════
	// Phase 7: Background Services
	// ═══════════════════════════════════════════
	stopCh := make(chan struct{})

	cronMgr := assembleCronRuntime(cfg, engine.baseDeps, tools.registry)

	startRuntimeCapabilities(cfg, stopCh, tools.registry, storage.mcpMgr, storage.skillMgr)

	// ═══════════════════════════════════════════
	// Phase 7.5: R3 Orchestration Wiring
	// ═══════════════════════════════════════════
	orchestration := assembleOrchestration(cfg, tools, storage.mcpMgr, storage.skillMgr)

	// ═══════════════════════════════════════════
	// Phase 8: Unified API + Server
	// ═══════════════════════════════════════════

	transports := assembleTransports(transportAssemblyInput{
		foundation:    foundation,
		core:          core,
		storage:       storage,
		engine:        engine,
		orchestration: orchestration,
		cronMgr:       cronMgr,
	})

	return assembleApp(appAssemblyInput{
		foundation:    foundation,
		core:          core,
		storage:       storage,
		tools:         tools,
		engine:        engine,
		orchestration: orchestration,
		transports:    transports,
		cronMgr:       cronMgr,
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
