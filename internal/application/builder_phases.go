package application

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/openai"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	"gorm.io/gorm"
)

// foundation holds Phase 1-3 outputs: config, DB, all stores, core infra.
type foundation struct {
	cfg          *config.Config
	cfgMgr       *config.Manager
	db           *gorm.DB
	repo         *persistence.Repository
	historyStore *persistence.HistoryStore
	router       *llm.Router
	promptEngine *prompt.Engine
	secHook      *security.Hook
	sbMgr        *sandbox.Manager
	workspaceDir string
	sessionID    string
	brainDir     string
	brainStore   *brain.ArtifactStore
	kiStore      *knowledge.Store
	kiRetriever  *knowledge.Retriever
	memStore     *memory.Store
	diaryStore   *memory.DiaryStore
	wsStore      *workspace.Store
	fileHistory  *workspace.FileHistory
	skillMgr     *skill.Manager
	mcpMgr       *mcp.Manager
}

// buildFoundation executes Phases 1-3: config, DB, LLM, storage, workspace.
func buildFoundation() (*foundation, error) {
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

	// Phase 2: Core Infrastructure
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
	if cfg.Agent.DefaultModel != "" {
		if err := router.SetDefault(cfg.Agent.DefaultModel); err != nil {
			log.Printf("Warning: default_model %q not found in providers, using fallback", cfg.Agent.DefaultModel)
		}
	}

	homeDir := config.HomeDir()
	promptEngine := prompt.NewEngineWithHome(homeDir)
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
	cfg.Security.Workspace = workspaceDir
	secHook := security.NewHook(&cfg.Security)

	// Phase 3: Storage Layer
	sessionID := generateSessionID()
	brainDir := config.ResolvePath(cfg.Storage.BrainDir)
	brainStore := brain.NewArtifactStore(brainDir, sessionID)

	kiDir := config.ResolvePath(cfg.Storage.KnowledgeDir)
	kiStore := knowledge.NewStore(kiDir)

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

	diaryDir := filepath.Join(config.HomeDir(), "memory", "diary")
	diaryStore := memory.NewDiaryStore(diaryDir)
	log.Printf("[diary] Diary store active: dir=%s", diaryDir)

	workDir := workspaceDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	wsStore := workspace.NewStore(workDir)
	fileHistory := workspace.NewFileHistory(workDir, sessionID)
	log.Printf("[file-history] initialized: dir=%s session=%s", fileHistory.BaseDir(), sessionID)

	skillsDir := config.ResolvePath(cfg.Storage.SkillsDir)
	skillMgr := skill.NewManager(skillsDir)
	mcpMgr := mcp.NewManager()

	return &foundation{
		cfg: cfg, cfgMgr: cfgMgr,
		db: db, repo: repo, historyStore: historyStore,
		router: router, promptEngine: promptEngine,
		secHook: secHook, sbMgr: sbMgr,
		workspaceDir: workspaceDir, sessionID: sessionID,
		brainDir: brainDir, brainStore: brainStore,
		kiStore: kiStore, kiRetriever: kiRetriever,
		memStore: memStore, diaryStore: diaryStore,
		wsStore: wsStore, fileHistory: fileHistory,
		skillMgr: skillMgr, mcpMgr: mcpMgr,
	}, nil
}

// buildTools registers all tools into a registry (Phase 4).
func buildTools(f *foundation) (*tool.Registry, *tool.SpawnAgentTool, *tool.BrainArtifactTool) {
	cfg := f.cfg
	registry := tool.NewRegistry()
	registry.SetWorkspaceDir(f.workspaceDir)
	registry.Register(&tool.ReadFileTool{})
	registry.Register(&tool.WriteFileTool{FileHistory: f.fileHistory})
	registry.Register(&tool.EditFileTool{FileHistory: f.fileHistory})
	registry.Register(&tool.GlobTool{})
	registry.Register(&tool.GrepSearchTool{})
	registry.Register(tool.NewRunCommandTool(f.sbMgr))
	registry.Register(tool.NewCommandStatusTool(f.sbMgr))
	searxngURL := cfg.Search.SearXNGURL
	if searxngURL == "" {
		// Backward compat: if searxng_url not set, fall back to endpoint
		searxngURL = cfg.Search.Endpoint
	}
	registry.Register(tool.NewWebSearchTool(searxngURL))
	registry.Register(tool.NewWebFetchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewDeepResearchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewTaskPlanTool(f.brainDir))
	registry.Register(tool.NewTaskBoundaryTool())
	registry.Register(tool.NewNotifyUserTool())
	registry.Register(&tool.UpdateProjectContextTool{})
	registry.Register(tool.NewSaveKnowledgeTool(f.kiStore, f.kiRetriever, cfg.Embedding.SimilarityThreshold))
	registry.Register(tool.NewRecallTool(f.kiRetriever, f.memStore, f.diaryStore))
	registry.Register(tool.NewSendMessageTool(f.brainDir))
	registry.Register(tool.NewTaskListTool(f.brainDir))
	spawnTool := tool.NewSpawnAgentTool(nil)
	registry.Register(spawnTool)
	registry.Register(tool.NewEvoTool("/tmp/ngoagent-evo"))
	brainArtifactTool := tool.NewBrainArtifactTool(nil)
	registry.Register(brainArtifactTool)
	registry.Register(tool.NewUndoEditTool(f.fileHistory))
	viewMediaAddr := fmt.Sprintf("http://localhost:%d", cfg.Server.HTTPPort)
	if cfg.Server.HTTPPort == 0 {
		viewMediaAddr = "http://localhost:19996"
	}
	registry.Register(tool.NewViewMediaTool(viewMediaAddr))

	return registry, spawnTool, brainArtifactTool
}

// buildBaseDeps assembles the service.Deps struct from foundation and tools.
func buildBaseDeps(f *foundation, registry *tool.Registry) service.Deps {
	return service.Deps{
		Config:       f.cfg,
		ConfigMgr:    f.cfgMgr,
		LLMRouter:    f.router,
		PromptEngine: f.promptEngine,
		ToolExec:     registry,
		Security:     f.secHook,
		Delta:        &service.Delta{},
		Brain:        f.brainStore,
		KIStore:      f.kiStore,
		KIRetriever:  f.kiRetriever,
		Workspace:    f.wsStore,
		SkillMgr:     f.skillMgr,
		HistoryStore: &historyAdapter{store: f.historyStore},
		FileHistory:  f.fileHistory,
		MemoryStore:  f.memStore,
	}
}
