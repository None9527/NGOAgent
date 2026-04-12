package application

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"gorm.io/gorm"
)

type foundationAssembly struct {
	cfgMgr          *config.Manager
	cfg             *config.Config
	db              *gorm.DB
	repo            *persistence.Repository
	historyStore    *persistence.HistoryStore
	snapshotStore   *persistence.RunSnapshotStore
	evoStore        *persistence.EvoStore
	transcriptStore *persistence.TranscriptStore
	agentRegistry   *service.AgentRegistry
}

func assembleFoundation() (foundationAssembly, error) {
	if err := config.Bootstrap(); err != nil {
		slog.Info(fmt.Sprintf("Warning: bootstrap: %v", err))
	}
	cfgMgr := config.NewManager(config.ConfigPath())
	cfg := cfgMgr.Get()

	dbPath := config.ResolvePath(cfg.Storage.DBPath)
	db, err := persistence.Open(dbPath)
	if err != nil {
		return foundationAssembly{}, err
	}

	agentRegistry := service.NewAgentRegistry()
	exePath, _ := os.Executable()
	builtInDir := filepath.Join(filepath.Dir(exePath), "agents", "built-in")
	if _, err := os.Stat(builtInDir); os.IsNotExist(err) {
		if cwd, err := os.Getwd(); err == nil {
			builtInDir = filepath.Join(cwd, "agents", "built-in")
		}
	}
	if err := agentRegistry.LoadFromDir(builtInDir, "built-in"); err != nil {
		slog.Info(fmt.Sprintf("Warning: agent registry load: %v", err))
	}

	return foundationAssembly{
		cfgMgr:          cfgMgr,
		cfg:             cfg,
		db:              db,
		repo:            persistence.NewRepository(db),
		historyStore:    persistence.NewHistoryStore(db),
		snapshotStore:   persistence.NewRunSnapshotStore(db),
		evoStore:        persistence.NewEvoStore(db),
		transcriptStore: persistence.NewTranscriptStore(db),
		agentRegistry:   agentRegistry,
	}, nil
}
