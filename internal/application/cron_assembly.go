package application

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

func assembleCronRuntime(cfg *config.Config, baseDeps service.Deps, registry *tool.Registry) *cron.Manager {
	cronDir := filepath.Join(config.HomeDir(), "cron")
	cronMgr, err := cron.NewManager(cronDir, func(jobName string) cron.Runner {
		cronDeps := baseDeps
		cronDeps.Hooks = nil
		cronDeps.KIStore = nil
		cronDeps.KIRetriever = nil
		cronDeps.Delta = &service.LogSink{Prefix: "[cron]"}
		cronDeps.Brain = brain.NewArtifactStoreFromDir(filepath.Join(cronDir, jobName, "artifacts"))
		return service.NewAgentLoop(cronDeps)
	})
	if err != nil {
		slog.Info(fmt.Sprintf("Warning: cron manager init: %v", err))
		return nil
	}

	if cfg.Cron.Enabled {
		if err := cronMgr.Start(); err != nil {
			slog.Info(fmt.Sprintf("Warning: cron start: %v", err))
		}
	}
	registry.Register(tool.NewManageCronTool(cronMgr))
	return cronMgr
}
