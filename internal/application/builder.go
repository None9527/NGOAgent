// Package application provides the Builder that wires all dependencies.
package application

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/openai"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
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
	SpawnTool    *tool.SpawnAgentTool // exposed for server-side EventPusher wiring
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
	evoStore := persistence.NewEvoStore(db) // Evo tables: evo_traces, evo_evaluations, evo_repairs

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
	// Apply configured default model (without this, router uses first provider's first model)
	if cfg.Agent.DefaultModel != "" {
		if err := router.SetDefault(cfg.Agent.DefaultModel); err != nil {
			log.Printf("Warning: default_model %q not found in providers, using fallback", cfg.Agent.DefaultModel)
		}
	}

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
	cfg.Security.Workspace = workspaceDir // Inject resolved workspace path for safe-zone checks
	secHook := security.NewHook(&cfg.Security)

	// ═══════════════════════════════════════════
	// Phase 3: Storage Layer
	// ═══════════════════════════════════════════
	sessionID := generateSessionID()
	brainDir := config.ResolvePath(cfg.Storage.BrainDir)
	brainStore := brain.NewArtifactStore(brainDir, sessionID)

	kiDir := config.ResolvePath(cfg.Storage.KnowledgeDir)
	kiStore := knowledge.NewStore(kiDir)

	// KI Embedding pipeline — only if provider is configured
	var kiRetriever *knowledge.Retriever
	if cfg.Embedding.Provider != "" && cfg.Embedding.BaseURL != "" {
		dims := cfg.Embedding.Dimensions
		if dims == 0 {
			dims = 1024
		}
		embedder := knowledge.NewDashScopeEmbedder(
			cfg.Embedding.BaseURL, cfg.Embedding.APIKey,
			cfg.Embedding.Model, dims,
		)
		vecIndex := knowledge.NewVectorIndex(dims, filepath.Join(kiDir, "index"))
		if err := vecIndex.Load(); err != nil {
			log.Printf("Warning: vector index load: %v", err)
		}
		kiRetriever = knowledge.NewRetriever(kiStore, embedder, vecIndex)
		if err := kiRetriever.BuildIndex(); err != nil {
			log.Printf("Warning: KI index build: %v", err)
		}
		log.Printf("[ki] Embedding pipeline active: provider=%s model=%s dims=%d",
			cfg.Embedding.Provider, cfg.Embedding.Model, dims)
	}

	// Memory store — reuses same embedder as KI for vector conversation memory
	var memStore *memory.Store
	if cfg.Embedding.Provider != "" && cfg.Embedding.BaseURL != "" {
		dims := cfg.Embedding.Dimensions
		if dims == 0 {
			dims = 1024
		}
		memEmbedder := knowledge.NewDashScopeEmbedder(
			cfg.Embedding.BaseURL, cfg.Embedding.APIKey,
			cfg.Embedding.Model, dims,
		)
		memDir := filepath.Join(brainDir, "memory_vec")
		memCfg := memory.StoreConfig{
			HalfLifeDays: cfg.Memory.HalfLifeDays,
			MaxFragments: cfg.Memory.MaxFragments,
		}
		memStore = memory.NewStore(memEmbedder, memDir, memCfg)
		log.Printf("[memory] Vector memory active: dir=%s halfLife=%d maxFrag=%d",
			memDir, memCfg.HalfLifeDays, memCfg.MaxFragments)
	}

	// Diary store — daily markdown entries under memory/diary/
	diaryDir := filepath.Join(config.HomeDir(), "memory", "diary")
	diaryStore := memory.NewDiaryStore(diaryDir)
	log.Printf("[diary] Diary store active: dir=%s", diaryDir)

	workDir := workspaceDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	wsStore := workspace.NewStore(workDir)

	// FileHistory: snapshot-based file edit rollback
	fileHistory := workspace.NewFileHistory(workDir, sessionID)
	log.Printf("[file-history] initialized: dir=%s session=%s", fileHistory.BaseDir(), sessionID)

	skillsDir := config.ResolvePath(cfg.Storage.SkillsDir)
	skillMgr := skill.NewManager(skillsDir)

	mcpMgr := mcp.NewManager()

	// ═══════════════════════════════════════════
	// Phase 4: Tool Registration
	// ═══════════════════════════════════════════
	registry := tool.NewRegistry()
	registry.SetWorkspaceDir(workspaceDir) // Resolve relative paths against workspace
	registry.Register(&tool.ReadFileTool{})
	registry.Register(&tool.WriteFileTool{FileHistory: fileHistory})
	registry.Register(&tool.EditFileTool{FileHistory: fileHistory})
	registry.Register(&tool.GlobTool{})
	registry.Register(&tool.GrepSearchTool{})
	registry.Register(tool.NewRunCommandTool(sbMgr))
	registry.Register(tool.NewCommandStatusTool(sbMgr))
	searxngURLB := cfg.Search.SearXNGURL
	if searxngURLB == "" {
		searxngURLB = cfg.Search.Endpoint
	}
	registry.Register(tool.NewWebSearchTool(searxngURLB))
	registry.Register(tool.NewWebFetchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewDeepResearchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewTaskPlanTool(brainDir))
	registry.Register(tool.NewTaskBoundaryTool())
	registry.Register(tool.NewNotifyUserTool())
	registry.Register(&tool.UpdateProjectContextTool{})
	registry.Register(tool.NewSaveKnowledgeTool(kiStore, kiRetriever, cfg.Embedding.SimilarityThreshold))
	registry.Register(tool.NewRecallTool(kiRetriever, memStore, diaryStore))
	registry.Register(tool.NewSendMessageTool(brainDir))
	registry.Register(tool.NewTaskListTool(brainDir))
	spawnTool := tool.NewSpawnAgentTool(nil) // Lazy: SpawnFunc set after loop creation
	registry.Register(spawnTool)
	registry.Register(tool.NewEvoTool("/tmp/ngoagent-evo"))
	brainArtifactTool := tool.NewBrainArtifactTool(nil) // Lazy: Brain set per-session
	registry.Register(brainArtifactTool)
	registry.Register(tool.NewUndoEditTool(fileHistory))
	// Multimodal: view_media tool for native VLM perception
	viewMediaAddr := fmt.Sprintf("http://localhost:%d", cfg.Server.HTTPPort)
	if cfg.Server.HTTPPort == 0 {
		viewMediaAddr = "http://localhost:19996"
	}
	registry.Register(tool.NewViewMediaTool(viewMediaAddr))
	// manage_cron tool is registered after CronManager creation (Phase 7)

	// ═══════════════════════════════════════════
	// Phase 5: Engine + Facades (unified via LoopFactory)
	// ═══════════════════════════════════════════
	// PostRun hooks: KI distillation after meaningful sessions
	kiDistiller := llm.NewKnowledgeDistiller(router)
	var dedupChecker service.KIDuplicateChecker
	if kiRetriever != nil {
		dedupChecker = kiRetriever
	}
	hookChain := service.NewHookManager()
	hookChain.Add(service.NewKIDistillHook(func() service.KIStore { return kiStore }, kiDistiller, cfg.Embedding.SimilarityThreshold, dedupChecker))
	hookChain.AddToolHook(service.NewAuditHook())
	hookChain.AddCompactHook(service.NewCompactAuditHook())
	if memStore != nil {
		hookChain.AddCompactHook(service.NewMemoryCompactHook(memStore, sessionID, dedupChecker))
	}
	// Diary hook: record run summary to daily diary after each session
	hookChain.Add(service.NewDiaryHook(&diaryAdapter{store: diaryStore}))
	// NOTE: TraceCollectorHook is per-loop (created in NewAgentLoop), NOT global.

	// Evo evaluator + repair router (nil-safe: if EvoConfig disabled, these are inert)
	var evoEvaluator *service.EvoEvaluator
	var evoRepairRouter *service.RepairRouter
	if cfg.Evo.AutoEval {
		evalModel := cfg.Evo.EvalModel
		if evalModel == "" {
			evalModel = cfg.Agent.DefaultModel
		}
		if evalProvider, err := router.Resolve(evalModel); err == nil {
			evoEvaluator = service.NewEvoEvaluator(evalProvider, cfg.Evo, evoStore)
			evoRepairRouter = service.NewRepairRouter(cfg.Evo, evoStore)
			log.Printf("[evo] Evaluator active: model=%s threshold=%.1f maxRetries=%d",
				evalModel, cfg.Evo.ScoreThreshold, cfg.Evo.MaxRetries)
		} else {
			log.Printf("[evo] Warning: eval model %q not found, evo disabled: %v", evalModel, err)
		}
	}

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
		KIRetriever:  kiRetriever,
		Workspace:    wsStore,
		SkillMgr:     skillMgr,
		HistoryStore: &historyAdapter{store: historyStore},
		FileHistory:     fileHistory,
		Hooks:           hookChain,
		MemoryStore:     memStore,
		EvoEvaluator:    evoEvaluator,
		EvoRepairRouter: evoRepairRouter,
		EvoStore:        evoStore,
	}
	factory := service.NewLoopFactory(baseDeps, 8) // max 8 concurrent runs

	// Main chat loop (backward-compat: server still uses loop directly)
	delta := &service.Delta{}
	loop := service.NewAgentLoop(baseDeps)
	loop.SetDelta(delta)

	// Wire SpawnFunc via factory — creates async subagent loops with barrier coordination
	// IMPORTANT: loopPool is declared here as a pointer so the SpawnFunc closure can capture it
	// by reference. It must be assigned BEFORE any SpawnFunc call (which only happens at runtime).
	var loopPool *service.LoopPool

	var barrierMu sync.Mutex
	barriers := make(map[string]*service.SubagentBarrier) // keyed by sessionID for correctness

	spawnTool.SetSpawnFunc(func(ctx context.Context, task, taskName string) (string, error) {
		// Get the RUNTIME session ID from context (injected by ChatStream)
		// instead of the fixed builder-time sessionID variable.
		runtimeSID := ctxutil.SessionIDFromContext(ctx)
		if runtimeSID == "" {
			runtimeSID = sessionID // fallback for backward compat (CLI mode)
		}

		// Get the correct parent loop: the session's loopPool loop, not the single main loop.
		var parentLoop *service.AgentLoop
		if loopPool != nil {
			parentLoop = loopPool.GetIfExists(runtimeSID)
		}
		if parentLoop == nil {
			parentLoop = loop // fallback for backward compat (CLI mode)
		}

		// Get or create barrier for this parent session's turn (keyed by runtimeSID)
		barrierMu.Lock()
		b, exists := barriers[runtimeSID]
		if !exists || b.Pending() == 0 {
			// Create new barrier — captures parentLoop and runtimeSID at closure time
			capturedLoop := parentLoop
			capturedSID := runtimeSID
			// Capture EventPusher for auto-wake done notification
			var wakeEventPusher func(string, string, any)
			if spawnTool.EventPusher != nil {
				wakeEventPusher = spawnTool.EventPusher
			}
			b = service.NewSubagentBarrier(capturedLoop, func() {
				// Auto-wake: re-run the parent loop to process subagent results.
				// The loop's Delta is still the one set by the original ChatStream.
				// For WS: wsWriter is still valid (no MarkDone), events flow through.
				go func() {
					log.Printf("[barrier] Auto-waking parent loop for session %s", capturedSID)
					var wakeLoop *service.AgentLoop
					if loopPool != nil {
						wakeLoop = loopPool.GetIfExists(capturedSID)
					}
					if wakeLoop == nil {
						wakeLoop = capturedLoop
					}
					if err := wakeLoop.Run(context.Background(), ""); err != nil {
						log.Printf("[barrier] Auto-wake failed for session %s: %v (pendingWake likely handled it)", capturedSID, err)
					} else {
						// Only signal frontend if fallback Run actually succeeded.
						// If Run failed ("agent is busy"), pendingWake already handled it
						// and ChatStream's done event will clean up the frontend state.
						if wakeEventPusher != nil {
							wakeEventPusher(capturedSID, "auto_wake_done", map[string]string{"type": "auto_wake_done"})
						}
					}
				}()
				// Clean up barrier reference
				barrierMu.Lock()
				delete(barriers, capturedSID)
				barrierMu.Unlock()
			})
			// Wire SSE/WS progress push if server has configured it
			if spawnTool.EventPusher != nil {
				pusher := spawnTool.EventPusher // capture
				capturedSID2 := runtimeSID
				b.SetProgressPush(func(runID, taskName, status string, done, total int, errMsg, output string) {
					pusher(capturedSID2, "subagent_progress", map[string]any{
						"type":      "subagent_progress",
						"run_id":    runID,
						"task_name": taskName,
						"status":    status,
						"done":      done,
						"total":     total,
						"error":     errMsg,
						"output":    output,
					})
				})
			}
			barriers[runtimeSID] = b
			// Wire config-driven concurrency limit
			if cfg.Agent.MaxSubagents > 0 {
				b.SetMaxConcurrent(cfg.Agent.MaxSubagents)
			}
		}
		barrierMu.Unlock()

		// S7: taskName comes directly from spawn_agent tool — no need to re-extract
		if taskName == "" {
			taskName = "sub-agent"
		}

		ch := service.NewSubagentChannel(func(runID, result string, err error) {
			// Route completion through barrier instead of direct ephemeral
			b.OnComplete(runID, result, err)
		})
		run := factory.Create(runtimeSID, ch)
		run.Loop.InjectEphemeral(prompttext.EphSubAgentContext)

		// S2: Register in barrier BEFORE RunAsync to prevent race
		// (fast-completing subagent could call OnComplete before Add)
		if err := b.Add(run.ID, taskName); err != nil {
			// S5: concurrency limit reached
			return "", fmt.Errorf("cannot spawn sub-agent: %v", err)
		}

		// Use Background context — subagent must survive parent loop completion.
		// Parent context cancellation should NOT kill running subagents.
		runID := factory.RunAsync(context.Background(), run, task)

		// Wire per-tool step push so SubagentDock shows current activity
		if spawnTool.EventPusher != nil {
			capturedPusher := spawnTool.EventPusher
			capturedSID3 := runtimeSID
			capturedRunID := runID
			capturedName := taskName
			ch.Collector().StepPush = func(toolName string) {
				capturedPusher(capturedSID3, "subagent_progress", map[string]any{
					"type":         "subagent_progress",
					"run_id":       capturedRunID,
					"task_name":    capturedName,
					"status":       "running",
					"done":         0,
					"total":        0,
					"current_step": toolName,
				})
			}
		}
		return runID, nil
	})

	// Per-session loop pool (uses factory baseDeps)
	// NOTE: must be assigned AFTER spawnTool.SetSpawnFunc so the closure captures the pointer correctly.
	loopPool = service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(baseDeps)
	}, brainDir)

	sessMgr := service.NewSessionManager(&sessionRepoAdapter{repo: repo, loc: cfg.LoadLocation()})
	chatEngine := service.NewChatEngine(loopPool, sessMgr, &historyAdapter{store: historyStore})
	modelMgr := service.NewModelManager(router)
	toolAdmin := service.NewToolAdmin(&toolRegistryAdapter{reg: registry})

	// Register TitleDistillHook after sessMgr is available
	hookChain.Add(service.NewTitleDistillHook(
		llm.NewTitleDistiller(router),
		sessMgr,
	))

	// MarkReady: transition .state.json from "new" → "ready" after first conversation
	if !config.IsBootstrapped() {
		hookChain.Add(&bootstrapReadyHook{})
	}

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
		var inline []mcp.ServerConfig
		for _, s := range new.MCP.Servers {
			inline = append(inline, mcp.ServerConfig{
				Name:    s.Name,
				Command: s.Command,
				Args:    s.Args,
				Env:     s.Env,
			})
		}
		configs := mcp.LoadMCPConfigs(config.HomeDir(), inline)
		mcpMgr.Reload(context.Background(), configs)
	})

	cfgMgr.Subscribe("agent", func(old, new *config.Config) {
		log.Printf("[hot-reload] Agent config changed: planning=%v max_steps=%d max_subagents=%d",
			new.Agent.PlanningMode, new.Agent.MaxSteps, new.Agent.MaxSubagents)
		// Push to main loop
		loop.ReloadConfig(new)
		// Push to all active session loops
		if loopPool != nil {
			loopPool.ForEach(func(l *service.AgentLoop) {
				l.ReloadConfig(new)
			})
		}
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

	// Start cron manager — file-based, each job = directory + job.json + logs/
	// Jobs are created/managed by the agent via manage_cron tool.
	cronDir := filepath.Join(config.HomeDir(), "cron")
	cronMgr, err := cron.NewManager(cronDir, func(jobName string) cron.Runner {
		// Cron loops use isolated deps: no KI hooks, no KI distillation.
		// Cron output stays in session history + log files only.
		cronDeps := baseDeps
		cronDeps.Hooks = nil       // Disable KIDistillHook
		cronDeps.KIStore = nil     // Prevent save_memory from writing to KI
		cronDeps.KIRetriever = nil // No KI retrieval injection
		cronDeps.Delta = &service.LogSink{Prefix: "[cron]"}
		cronDeps.Brain = brain.NewArtifactStoreFromDir(filepath.Join(cronDir, jobName, "artifacts"))
		cronLoop := service.NewAgentLoop(cronDeps)
		return cronLoop
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

	// Load MCP servers: merge mcp.json files with inline config.yaml entries
	var inlineMCP []mcp.ServerConfig
	for _, s := range cfg.MCP.Servers {
		inlineMCP = append(inlineMCP, mcp.ServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
		})
	}
	mcpConfigs := mcp.LoadMCPConfigs(config.HomeDir(), inlineMCP)
	if len(mcpConfigs) > 0 {
		mcpMgr.StartAll(context.Background(), mcpConfigs)
		// Auto-register MCP-discovered tools into agent registry
		tool.RegisterMCPTools(registry, mcpMgr)
	}
	// Skill registration:
	// - light + executable: register as ScriptTool (LLM calls directly)
	// - heavy + executable: trigger-inject + run_command (LLM uses run_command after ephemeral hint)
	// - workflow: agent uses read_file on SKILL.md when needed (no dedicated tool)
	for _, sk := range skillMgr.AutoPromote() {
		if (sk.Type == "executable" || sk.Type == "hybrid") && sk.Weight == "light" {
			registry.Register(tool.NewScriptTool(sk))
			log.Printf("[skill] Registered light ScriptTool: %s", sk.Name)
		} else if sk.Weight == "heavy" {
			log.Printf("[skill] Heavy skill (trigger-inject): %s [%d triggers]", sk.Name, len(sk.Triggers))
		}
	}
	log.Printf("[skill] %d skills discovered (YAML pre-read, no use_skill tool)", len(skillMgr.List()))

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
		secHook, skillMgr, cronMgr, mcpMgr,
		cfgMgr, router,
		&historyAdapter{store: historyStore},
		brainDir,
		kiStore,
		sbMgr,
	)
	srv := server.NewServer(agentAPI, addr, cfg.Server.AuthToken)

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
		SpawnTool:    spawnTool,
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

// Adapter types are in adapters.go
