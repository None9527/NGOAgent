// Package application provides the Builder that wires all dependencies.
package application

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/anthropic"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/google"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/openai"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/notify" // P3 M1
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
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
		slog.Info(fmt.Sprintf("Warning: bootstrap: %v", err))
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
	snapshotStore := persistence.NewRunSnapshotStore(db)
	evoStore := persistence.NewEvoStore(db)               // Evo tables: evo_traces, evo_evaluations, evo_repairs
	transcriptStore := persistence.NewTranscriptStore(db) // P2 F1: subagent transcripts

	// SubAgent v2: Agent Definition Registry
	agentRegistry := service.NewAgentRegistry()
	// Load built-in agent definitions from agents/built-in/
	exePath, _ := os.Executable()
	builtInDir := filepath.Join(filepath.Dir(exePath), "agents", "built-in")
	if _, err := os.Stat(builtInDir); os.IsNotExist(err) {
		// Fallback: try relative to CWD (dev mode)
		if cwd, err := os.Getwd(); err == nil {
			builtInDir = filepath.Join(cwd, "agents", "built-in")
		}
	}
	if err := agentRegistry.LoadFromDir(builtInDir, "built-in"); err != nil {
		slog.Info(fmt.Sprintf("Warning: agent registry load: %v", err))
	}

	// ═══════════════════════════════════════════
	// Phase 2: Core Infrastructure
	// ═══════════════════════════════════════════
	var providers []llm.Provider
	for _, pd := range cfg.LLM.Providers {
		provType := pd.Type
		if provType == "" {
			provType = "openai"
		}
		baseURL := pd.BaseURL
		if preset, ok := llm.GetPresetProvider(provType); ok {
			if baseURL == "" {
				baseURL = preset.DefaultBaseURL
			}
		}

		switch provType {
		case "anthropic":
			providers = append(providers, anthropic.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
		case "google":
			providers = append(providers, google.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
		default:
			// Fallback to OpenAI-compatible client natively for (DashScope, Volcengine, Mistral, Ollama, DeepSeek, etc.)
			cli := openai.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models)
			if provType == "dashscope" {
				cli.SetExtraHeaders(map[string]string{"X-DashScope-Session-Cache": "enable"}) // implicitly enable cache for qwen
			}
			providers = append(providers, cli)
		}
	}
	router := llm.NewRouter(providers)
	// Apply configured default model (without this, router uses first provider's first model)
	if cfg.Agent.DefaultModel != "" {
		if err := router.SetDefault(cfg.Agent.DefaultModel); err != nil {
			slog.Info(fmt.Sprintf("Warning: default_model %q not found in providers, using fallback", cfg.Agent.DefaultModel))
		}
	}

	// P3 J3: Initialize global telemetry collector
	llm.GlobalTelemetry = llm.NewTelemetryCollector()

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

	// P3 K1: Wire AI Safety Classifier strategy (pattern | llm | hybrid)
	if mode := cfg.Security.ClassifierMode; mode == "llm" || mode == "hybrid" {
		// Use first available LLM provider for classifier (prefer small/fast models)
		clsModel := cfg.Security.ClassifierModel
		if clsModel == "" {
			clsModel = cfg.Agent.DefaultModel // fallback to default model
		}
		if len(providers) > 0 {
			clsProvider := providers[0] // first provider
			cls := security.NewClassifier(mode, secHook, clsProvider, clsModel)
			secHook.SetClassifier(cls)
			slog.Info(fmt.Sprintf("[security] classifier strategy: %s (model: %s)", mode, clsModel))
		}
	}

	// ═══════════════════════════════════════════
	// Phase 3: Storage Layer
	// ═══════════════════════════════════════════
	// Default session ID: fixed identifier for the CLI/fallback loop.
	// Web sessions use LoopPool with per-conversation UUIDs from the DB.
	// Previously used generateSessionID() (random UUID), which created
	// "ghost sessions" — unregistered in conversations table.
	sessionID := "__default__"
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
			slog.Info(fmt.Sprintf("Warning: vector index load: %v", err))
		}
		kiRetriever = knowledge.NewRetriever(kiStore, embedder, vecIndex)
		if err := kiRetriever.BuildIndex(); err != nil {
			slog.Info(fmt.Sprintf("Warning: KI index build: %v", err))
		}
		slog.Info(fmt.Sprintf("[ki] Embedding pipeline active: provider=%s model=%s dims=%d",
			cfg.Embedding.Provider, cfg.Embedding.Model, dims))
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
		slog.Info(fmt.Sprintf("[memory] Vector memory active: dir=%s halfLife=%d maxFrag=%d",
			memDir, memCfg.HalfLifeDays, memCfg.MaxFragments))
	}

	// Diary store — daily markdown entries under memory/diary/
	diaryDir := filepath.Join(config.HomeDir(), "memory", "diary")
	diaryStore := memory.NewDiaryStore(diaryDir)
	slog.Info(fmt.Sprintf("[diary] Diary store active: dir=%s", diaryDir))

	workDir := workspaceDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	wsStore := workspace.NewStore(workDir)

	// FileHistory: snapshot-based file edit rollback
	fileHistory := workspace.NewFileHistory(workDir, sessionID)
	slog.Info(fmt.Sprintf("[file-history] initialized: dir=%s session=%s", fileHistory.BaseDir(), sessionID))

	skillsDir := config.ResolvePath(cfg.Storage.SkillsDir)
	skillMgr := skill.NewManager(skillsDir)
	skillMgr.RegisterBundled() // P1 #49: load built-in recovery skills

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
	// All web tools route through the agent-search pipeline (unified endpoint)
	agentSearchURLB := cfg.Search.Endpoint
	registry.Register(tool.NewWebSearchTool(agentSearchURLB))
	registry.Register(tool.NewWebFetchTool(agentSearchURLB))
	registry.Register(tool.NewDeepResearchTool(agentSearchURLB))
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
	skillTool := tool.NewSkillTool(skillMgr) // Lazy: SpawnFunc set after loop creation
	registry.Register(skillTool)
	registry.Register(tool.NewEvoTool("/tmp/ngoagent-evo", sbMgr))
	brainArtifactTool := tool.NewBrainArtifactTool(nil) // Lazy: Brain set per-session
	registry.Register(brainArtifactTool)
	registry.Register(tool.NewUndoEditTool(fileHistory))
	// P1-D #71: Git integration tools
	registry.Register(&tool.GitStatusTool{})
	registry.Register(&tool.GitDiffTool{})
	registry.Register(&tool.GitLogTool{})
	registry.Register(&tool.GitCommitTool{})
	registry.Register(&tool.GitBranchTool{})
	// Multimodal: view_media tool for native VLM perception
	viewMediaAddr := fmt.Sprintf("http://localhost:%d", cfg.Server.HTTPPort)
	if cfg.Server.HTTPPort == 0 {
		viewMediaAddr = "http://localhost:19996"
	}
	registry.Register(tool.NewViewMediaTool(viewMediaAddr))
	// P3 M2 (#45): 6 new tools — expands tool matrix to CC parity
	registry.Register(&tool.TreeTool{})
	registry.Register(&tool.FindFilesTool{})
	registry.Register(&tool.CountLinesTool{})
	registry.Register(&tool.DiffFilesTool{})
	registry.Register(tool.NewHTTPFetchTool())
	registry.Register(&tool.ClipboardTool{})
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
		// Phase 3: Real-time memory sink — save after every run, not just compaction
		hookChain.Add(service.NewMemoryPostRunHook(memStore, dedupChecker))
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
			slog.Info(fmt.Sprintf("[evo] Evaluator active: model=%s threshold=%.1f maxRetries=%d",
				evalModel, cfg.Evo.ScoreThreshold, cfg.Evo.MaxRetries))
		} else {
			slog.Info(fmt.Sprintf("[evo] Warning: eval model %q not found, evo disabled: %v", evalModel, err))
		}
	}

	baseDeps := service.Deps{
		Config:          cfg,
		LLMRouter:       router,
		PromptEngine:    promptEngine,
		ToolExec:        registry,
		Security:        &securityAdapter{hook: secHook},
		Delta:           &service.Delta{}, // Overridden per-channel
		Brain:           brainStore,
		KIStore:         kiStore,
		KIRetriever:     kiRetriever,
		Workspace:       wsStore,
		SkillMgr:        skillMgr,
		HistoryStore:    &historyAdapter{store: historyStore},
		FileHistory:     fileHistory,
		Hooks:           hookChain,
		MemoryStore:     memStore,
		SnapshotStore:   snapshotStore,
		EvoEvaluator:    evoEvaluator,
		EvoRepairRouter: evoRepairRouter,
		EvoStore:        evoStore,
		WebhookHook:     buildWebhookHook(cfg, sessionID),
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

	spawnTool.SetSpawnFunc(func(ctx context.Context, task, taskName, agentType string) (string, error) {
		// Resolve agent definition from registry
		agentDef, defErr := agentRegistry.Resolve(agentType)
		if defErr != nil {
			return "", fmt.Errorf("agent type %q not found: %w", agentType, defErr)
		}
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

		// SubAgent v2 P2: Enrich task with L1/L2/L3 parent context
		if parentLoop != nil {
			parentHistory := parentLoop.History()
			task = service.BuildSubagentContext(task, parentHistory, agentDef)
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
					slog.Info(fmt.Sprintf("[barrier] Auto-waking parent loop for session %s", capturedSID))
					var wakeLoop *service.AgentLoop
					if loopPool != nil {
						wakeLoop = loopPool.GetIfExists(capturedSID)
					}
					if wakeLoop == nil {
						wakeLoop = capturedLoop
					}
					if err := wakeLoop.Run(context.Background(), ""); err != nil {
						slog.Info(fmt.Sprintf("[barrier] Auto-wake failed for session %s: %v (pendingWake likely handled it)", capturedSID, err))
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
				if capturedLoop != nil {
					capturedLoop.ClearActiveBarrier()
				}
				barrierMu.Lock()
				delete(barriers, capturedSID)
				barrierMu.Unlock()
			})
			if capturedLoop != nil {
				capturedLoop.SetActiveBarrier(b)
			}
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
			// SubAgent v2: Wire per-definition timeout
			if agentDef != nil && agentDef.MaxTimeout > 0 {
				b.SetTimeout(agentDef.MaxTimeout)
			}
			// P2 F1: Wire transcript persistence via TranscriptStore.SaveSimple
			if transcriptStore != nil {
				ts := transcriptStore // capture for closure
				b.SetTranscriptSaver(runtimeSID, func(sid, name, rid, status, output string) {
					if err := ts.SaveSimple(sid, name, rid, status, output); err != nil {
						slog.Info(fmt.Sprintf("[barrier] Transcript save failed for %s: %v", rid, err))
					}
				})
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
		run := factory.Create(runtimeSID, ch, agentDef)
		run.Loop.InjectEphemeral(prompttext.EphSubAgentContext)

		// SubAgent v2 P2: Model routing — agentDef.Model overrides parent model
		if agentDef != nil && agentDef.Model != "" {
			run.Loop.SetModel(agentDef.Model)
		}

		// P1-B #54: Inject coordinator mode into the PARENT loop.
		// This activates orchestrator-mode rules: synthesize research before delegating,
		// write self-contained specs, never lazy-delegate. Mirrors CC's Coordinator Prompt.
		if parentLoop != nil {
			parentLoop.InjectEphemeral(prompttext.EphCoordinatorMode)
		}

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
	// Wire same SpawnFunc to skillTool for fork-mode skill execution
	skillTool.SetSpawnFunc(spawnTool.GetSpawnFunc())

	// Sprint 1-1 / 2-2 / 3-1: Wire runtime context for conditional tool descriptions
	hasGit := false
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			hasGit = true
		}
	}
	spawnTool.ToolCtx = prompttext.ToolContext{
		HasGit:     hasGit,
		HasSandbox: sbMgr != nil,
		SkillCount: len(skillMgr.List()),
		HasBrain:   brainDir != "",
	}
	if brainDir != "" {
		spawnTool.ScratchDir = filepath.Join(brainDir, sessionID, "scratchpad")
	}
	// SubAgent v2: populate agent_type enum from registry
	spawnTool.SetAgentTypes(agentRegistry.TypeNames())

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
		slog.Info(fmt.Sprint("[hot-reload] LLM config changed, rebuilding providers"))
		var newProviders []llm.Provider
		for _, pd := range new.LLM.Providers {
			provType := pd.Type
			if provType == "" {
				provType = "openai"
			}
			baseURL := pd.BaseURL
			if preset, ok := llm.GetPresetProvider(provType); ok {
				if baseURL == "" {
					baseURL = preset.DefaultBaseURL
				}
			}

			switch provType {
			case "anthropic":
				newProviders = append(newProviders, anthropic.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
			case "google":
				newProviders = append(newProviders, google.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
			default:
				cli := openai.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models)
				if provType == "dashscope" {
					cli.SetExtraHeaders(map[string]string{"X-DashScope-Session-Cache": "enable"})
				}
				newProviders = append(newProviders, cli)
			}
		}
		router.Reload(newProviders)
	})

	cfgMgr.Subscribe("security", func(old, new *config.Config) {
		slog.Info(fmt.Sprint("[hot-reload] Security config changed"))
		secHook.ReloadChain(&new.Security)
	})

	cfgMgr.Subscribe("mcp", func(old, new *config.Config) {
		slog.Info(fmt.Sprint("[hot-reload] MCP config changed, reloading servers"))
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
		slog.Info(fmt.Sprintf("[hot-reload] Agent config changed: planning=%v max_steps=%d max_subagents=%d",
			new.Agent.PlanningMode, new.Agent.MaxSteps, new.Agent.MaxSubagents))
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
		slog.Info(fmt.Sprintf("Warning: config watch: %v", err))
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
		slog.Info(fmt.Sprintf("Warning: cron manager init: %v", err))
	} else {
		if cfg.Cron.Enabled {
			if err := cronMgr.Start(); err != nil {
				slog.Info(fmt.Sprintf("Warning: cron start: %v", err))
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
	// - executable/hybrid: register as ScriptTool (LLM calls directly via tool name)
	// - workflow/guide: agent-discoverable (invoked via skill(name="X"))
	for _, sk := range skillMgr.AutoPromote() {
		if sk.Type == "executable" || sk.Type == "hybrid" {
			registry.Register(tool.NewScriptTool(sk))
			slog.Info(fmt.Sprintf("[skill] Registered ScriptTool: %s", sk.Name))
		} else {
			slog.Info(fmt.Sprintf("[skill] Registered standard skill: %s", sk.Name))
		}
	}
	slog.Info(fmt.Sprintf("[skill] %d skills discovered (pre-read, invoked via SkillTool)", len(skillMgr.List())))

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

	// P2 H2: Wire persistent token usage store
	tokenUsageStore := persistence.NewTokenUsageStore(db)
	agentAPI.SetTokenUsageStore(tokenUsageStore)

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
	slog.Info(fmt.Sprint("[app] Shutdown complete"))
}

func generateSessionID() string {
	return uuid.New().String()
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
