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
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

type orchestrationAssembly struct {
	eventBus   *service.EventBus
	discovery  *service.AggregatedToolDiscovery
	a2aHandler *a2a.Handler
	addr       string
}

func assembleOrchestration(
	cfg *config.Config,
	registry *tool.Registry,
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
		&toolRegistryAdapter{reg: registry},
		&mcpDiscoveryAdapter{mgr: mcpMgr},
		&skillDiscoveryAdapter{mgr: skillMgr},
	)
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
