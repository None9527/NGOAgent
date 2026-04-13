package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/a2a"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

type orchestrationAssembly struct {
	eventBus   *service.EventBus
	discovery  *service.AggregatedToolDiscovery
	a2aHandler *a2a.Handler
	addr       string
}

func assembleOrchestration(
	cfg *config.Config,
	tools assembledTools,
	mcpMgr *mcp.Manager,
	skillMgr *skill.Manager,
) orchestrationAssembly {
	addr := ":19996"
	if cfg.Server.HTTPPort != 0 {
		addr = fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	}

	eventBus := service.NewEventBus(
		service.WithBufferSize(256),
		service.WithWorkerCount(4),
	)
	slog.Info("[r3] EventBus started: buffer=256, workers=4")

	toolDiscovery := service.NewAggregatedToolDiscovery(
		&toolRegistryAdapter{reg: tools.registry},
		&mcpDiscoveryAdapter{mgr: mcpMgr},
		&skillDiscoveryAdapter{mgr: skillMgr},
	)
	toolDiscovery.SetBuiltinSources(builtinToolSources(tools.manifest))
	capabilities := toolDiscovery.Advertise(context.Background())
	slog.Info(fmt.Sprintf("[r3] ToolDiscovery initialized: %d capabilities", len(capabilities)))

	agentCard := a2a.AgentCard{
		Name:         "NGOAgent",
		Description:  "Autonomous coding agent with graph-based execution engine",
		Version:      Version,
		URL:          "http://localhost" + addr,
		Capabilities: capabilities,
		InputModes:   []string{"text"},
		OutputModes:  []string{"text", "sse"},
	}
	a2aHandler := a2a.NewHandler(agentCard, eventBus)
	slog.Info(fmt.Sprintf("[r3] A2A handler ready: %s v%s", agentCard.Name, agentCard.Version))

	return orchestrationAssembly{
		eventBus:   eventBus,
		discovery:  toolDiscovery,
		a2aHandler: a2aHandler,
		addr:       addr,
	}
}

func builtinToolSources(manifest []toolProviderManifest) map[string]string {
	sources := make(map[string]string)
	for _, entry := range manifest {
		for _, name := range entry.Tools {
			sources[name] = entry.Name
		}
	}
	return sources
}
