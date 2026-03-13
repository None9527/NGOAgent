// Package application provides the Builder that wires all dependencies.
package application

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
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
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
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
	GRPCServer   *grpcserver.Server
	ChatEngine   *service.ChatEngine
	SessionMgr   *service.SessionManager
	ModelMgr     *service.ModelManager
	ToolAdmin    *service.ToolAdmin
	CronMgr      *cron.Manager
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

	// ═══════════════════════════════════════════
	// Phase 2: Core Infrastructure
	// ═══════════════════════════════════════════
	var providers []llm.Provider
	for _, pd := range cfg.LLM.Providers {
		if pd.Type == "" || pd.Type == "openai" {
			providers = append(providers, openai.NewClient(pd.Name, pd.BaseURL, pd.APIKey, pd.Models))
		} else {
			p := llm.BuildProviderFromConfig(pd.Type, pd.Name, pd.BaseURL, pd.APIKey, pd.Models)
			if p != nil {
				providers = append(providers, p)
			}
		}
	}
	router := llm.NewRouter(providers)

	homeDir := config.HomeDir()
	promptEngine := prompt.NewEngineWithHome(homeDir)
	// Resolve workspace path (~ → home dir) and ensure it exists
	workspaceDir := cfg.Agent.Workspace
	if strings.HasPrefix(workspaceDir, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			workspaceDir = h + workspaceDir[1:]
		}
	}
	if workspaceDir != "" {
		os.MkdirAll(workspaceDir, 0755)
	}
	sbMgr := sandbox.NewManager(workspaceDir)
	secHook := security.NewHook(&cfg.Security)

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
	registry.Register(tool.NewSaveMemoryTool(kiStore))
	registry.Register(tool.NewSendMessageTool(brainDir))
	registry.Register(tool.NewTaskListTool(brainDir))
	spawnTool := tool.NewSpawnAgentTool(nil) // Lazy: SpawnFunc set after loop creation
	registry.Register(spawnTool)
	registry.Register(tool.NewForgeTool(cfg.Forge.SandboxDir))
	brainArtifactTool := tool.NewBrainArtifactTool(nil) // Lazy: Brain set per-session
	registry.Register(brainArtifactTool)
	// manage_cron tool is registered after CronManager creation (Phase 7)

	// ═══════════════════════════════════════════
	// Phase 5: Engine + Facades (unified via LoopFactory)
	// ═══════════════════════════════════════════
	// PostRun hooks: KI distillation after meaningful sessions
	kiDistiller := llm.NewKnowledgeDistiller(router)
	hookChain := service.NewPostRunHookChain(
		service.NewKIDistillHook(func() service.KIStore { return kiStore }, kiDistiller),
	)

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
		HistoryStore: &historyAdapter{store: historyStore},
		Hooks:        hookChain,
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

	sessMgr := service.NewSessionManager(&sessionRepoAdapter{repo: repo, loc: cfg.LoadLocation()})
	chatEngine := service.NewChatEngine(loop, sessMgr, &historyAdapter{store: historyStore})
	modelMgr := service.NewModelManager(router)
	toolAdmin := service.NewToolAdmin(&toolRegistryAdapter{reg: registry})
	_ = chatEngine // Used by server routes

	// Register TitleDistillHook after sessMgr is available
	hookChain.Add(service.NewTitleDistillHook(
		llm.NewTitleDistiller(router),
		sessMgr,
	))

	// ═══════════════════════════════════════════
	// Phase 6: Hot-Reload Subscriptions
	// ═══════════════════════════════════════════
	cfgMgr.Subscribe("llm", func(old, new *config.Config) {
		log.Println("[hot-reload] LLM config changed, rebuilding providers")
		var newProviders []llm.Provider
		for _, pd := range new.LLM.Providers {
			if pd.Type == "" || pd.Type == "openai" {
				newProviders = append(newProviders, openai.NewClient(pd.Name, pd.BaseURL, pd.APIKey, pd.Models))
			} else {
				p := llm.BuildProviderFromConfig(pd.Type, pd.Name, pd.BaseURL, pd.APIKey, pd.Models)
				if p != nil {
					newProviders = append(newProviders, p)
				}
			}
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

	// Start cron manager — purely SQLite-driven, no default jobs.
	// Jobs are created/managed by the agent via manage_cron tool.
	cronMgr, err := cron.NewManager(db, func() cron.Runner {
		// Create a new per-run session tagged as "cron" channel
		conv, convErr := repo.CreateConversation("cron", "")
		cronSID := sessionID // fallback to global session
		if convErr == nil {
			cronSID = conv.ID
		}
		run := factory.Create(cronSID, service.NewHeartbeatChannel(nil))
		return run.Loop
	})
	if err != nil {
		log.Printf("Warning: cron manager init: %v", err)
	} else {
		if cfg.Cron.Enabled {
			if err := cronMgr.Start(); err != nil {
				log.Printf("Warning: cron start: %v", err)
			}
		}
		// Register manage_cron tool now that manager is ready
		registry.Register(tool.NewManageCronTool(cronMgr))
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
	// Phase 8: Unified API + Server
	// ═══════════════════════════════════════════
	addr := ":19996"
	if cfg.Server.HTTPPort != 0 {
		addr = fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	}

	agentAPI := NewAgentAPI(
		loop, loopPool, chatEngine,
		sessMgr, modelMgr, toolAdmin,
		secHook, skillMgr, cronMgr,
		cfgMgr, router,
		&historyAdapter{store: historyStore},
		brainDir,
		kiStore,
	)
	srv := server.NewServer(agentAPI, addr)

	// gRPC server — defaults to :19998
	grpcAddr := ":19998"
	if cfg.Server.GRPCPort != 0 {
		grpcAddr = fmt.Sprintf(":%d", cfg.Server.GRPCPort)
	}
	grpcSrv := grpcserver.NewServer(agentAPI, grpcAddr)

	return &App{
		Config:       cfgMgr,
		DB:           db,
		Repo:         repo,
		Router:       router,
		Loop:         loop,
		Factory:      factory,
		Server:       srv,
		GRPCServer:   grpcSrv,
		ChatEngine:   chatEngine,
		SessionMgr:   sessMgr,
		ModelMgr:     modelMgr,
		ToolAdmin:    toolAdmin,
		CronMgr:      cronMgr,
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
	if app.CronMgr != nil {
		app.CronMgr.Stop()
	}
	log.Println("[app] Shutdown complete")
}

func generateSessionID() string {
	return uuid.New().String()
}

// historyAdapter bridges domain.HistoryPersister → infrastructure.HistoryStore
// to avoid import cycle (domain cannot import persistence).
type historyAdapter struct {
	store *persistence.HistoryStore
}

func (a *historyAdapter) SaveAll(sessionID string, msgs []service.HistoryExport) error {
	rows := make([]persistence.HistoryMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = persistence.HistoryMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return a.store.SaveAll(sessionID, rows)
}

func (a *historyAdapter) LoadAll(sessionID string) ([]service.HistoryExport, error) {
	rows, err := a.store.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}
	exports := make([]service.HistoryExport, len(rows))
	for i, r := range rows {
		exports[i] = service.HistoryExport{
			Role:       r.Role,
			Content:    r.Content,
			ToolCalls:  r.ToolCalls,
			ToolCallID: r.ToolCallID,
		}
	}
	return exports, nil
}

func (a *historyAdapter) DeleteSession(sessionID string) error {
	return a.store.DeleteSession(sessionID)
}

func (a *historyAdapter) AppendAll(sessionID string, msgs []service.HistoryExport) error {
	rows := make([]persistence.HistoryMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = persistence.HistoryMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}
	return a.store.AppendBatch(sessionID, rows)
}

// sessionRepoAdapter bridges domain.SessionRepo → *persistence.Repository.
type sessionRepoAdapter struct {
	repo *persistence.Repository
	loc  *time.Location
}

func (a *sessionRepoAdapter) CreateConversation(channel, title string) (string, error) {
	conv, err := a.repo.CreateConversation(channel, title)
	if err != nil {
		return "", err
	}
	return conv.ID, nil
}

func (a *sessionRepoAdapter) ListConversations(limit, offset int) ([]service.ConversationInfo, error) {
	convs, err := a.repo.ListConversations(limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]service.ConversationInfo, len(convs))
	for i, c := range convs {
		result[i] = service.ConversationInfo{
			ID:        c.ID,
			Title:     c.Title,
			Channel:   c.Channel,
			CreatedAt: c.CreatedAt.In(a.loc).Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: c.UpdatedAt.In(a.loc).Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return result, nil
}

func (a *sessionRepoAdapter) UpdateTitle(id, title string) error {
	return a.repo.UpdateConversationTitle(id, title)
}

func (a *sessionRepoAdapter) Touch(id string) error {
	return a.repo.TouchConversation(id)
}

func (a *sessionRepoAdapter) DeleteConversation(id string) error {
	return a.repo.DeleteConversation(id)
}

// toolRegistryAdapter bridges domain.ToolRegistry → *tool.Registry.
type toolRegistryAdapter struct {
	reg *tool.Registry
}

func (a *toolRegistryAdapter) List() []service.ToolInfo {
	infos := a.reg.List()
	result := make([]service.ToolInfo, len(infos))
	for i, ti := range infos {
		result[i] = service.ToolInfo{Name: ti.Name, Enabled: ti.Enabled}
	}
	return result
}

func (a *toolRegistryAdapter) Enable(name string) error  { return a.reg.Enable(name) }
func (a *toolRegistryAdapter) Disable(name string) error { return a.reg.Disable(name) }
