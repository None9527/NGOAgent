package application

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
)

type storageAssembly struct {
	sessionID      string
	brainDir       string
	brainStore     *brain.ArtifactStore
	kiStore        *knowledge.Store
	kiRetriever    *knowledge.Retriever
	memStore       *memory.Store
	diaryStore     *memory.DiaryStore
	workspaceStore *workspace.Store
	fileHistory    *workspace.FileHistory
	skillMgr       *skill.Manager
	mcpMgr         *mcp.Manager
}

func assembleStorage(cfg *config.Config, workspaceDir string) storageAssembly {
	sessionID := "__default__"
	brainDir := config.ResolvePath(cfg.Storage.BrainDir)
	brainStore := brain.NewArtifactStore(brainDir, sessionID)

	kiDir := config.ResolvePath(cfg.Storage.KnowledgeDir)
	kiStore := knowledge.NewStore(kiDir)
	kiRetriever := assembleKIRetriever(cfg, kiDir, kiStore)
	memStore := assembleMemoryStore(cfg, brainDir)

	diaryDir := filepath.Join(config.HomeDir(), "memory", "diary")
	diaryStore := memory.NewDiaryStore(diaryDir)
	slog.Info(fmt.Sprintf("[diary] Diary store active: dir=%s", diaryDir))

	workDir := workspaceDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	wsStore := workspace.NewStore(workDir)
	fileHistory := workspace.NewFileHistory(workDir, sessionID)
	slog.Info(fmt.Sprintf("[file-history] initialized: dir=%s session=%s", fileHistory.BaseDir(), sessionID))

	skillMgr := skill.NewManager(config.ResolvePath(cfg.Storage.SkillsDir))
	skillMgr.RegisterBundled()

	return storageAssembly{
		sessionID:      sessionID,
		brainDir:       brainDir,
		brainStore:     brainStore,
		kiStore:        kiStore,
		kiRetriever:    kiRetriever,
		memStore:       memStore,
		diaryStore:     diaryStore,
		workspaceStore: wsStore,
		fileHistory:    fileHistory,
		skillMgr:       skillMgr,
		mcpMgr:         mcp.NewManager(),
	}
}

func assembleKIRetriever(cfg *config.Config, kiDir string, kiStore *knowledge.Store) *knowledge.Retriever {
	if cfg.Embedding.Provider == "" || cfg.Embedding.BaseURL == "" {
		return nil
	}
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
	kiRetriever := knowledge.NewRetriever(kiStore, embedder, vecIndex)
	if err := kiRetriever.BuildIndex(); err != nil {
		slog.Info(fmt.Sprintf("Warning: KI index build: %v", err))
	}
	slog.Info(fmt.Sprintf("[ki] Embedding pipeline active: provider=%s model=%s dims=%d",
		cfg.Embedding.Provider, cfg.Embedding.Model, dims))
	return kiRetriever
}

func assembleMemoryStore(cfg *config.Config, brainDir string) *memory.Store {
	if cfg.Embedding.Provider == "" || cfg.Embedding.BaseURL == "" {
		return nil
	}
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
	memStore := memory.NewStore(memEmbedder, memDir, memCfg)
	slog.Info(fmt.Sprintf("[memory] Vector memory active: dir=%s halfLife=%d maxFrag=%d",
		memDir, memCfg.HalfLifeDays, memCfg.MaxFragments))
	return memStore
}
