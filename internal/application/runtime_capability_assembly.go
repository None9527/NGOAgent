package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

type capabilitySource interface {
	Start(context.Context, *tool.Registry)
}

func startRuntimeCapabilities(
	cfg *config.Config,
	stopCh <-chan struct{},
	registry *tool.Registry,
	mcpMgr *mcp.Manager,
	skillMgr *skill.Manager,
) {
	sources := []capabilitySource{
		skillWatcherSource{mgr: skillMgr, stopCh: stopCh},
		mcpCapabilitySource{mgr: mcpMgr, configs: loadInlineMCPConfigs(cfg)},
		skillPromotionSource{mgr: skillMgr},
	}
	for _, source := range sources {
		source.Start(context.Background(), registry)
	}
}

func loadInlineMCPConfigs(cfg *config.Config) []mcp.ServerConfig {
	var inlineMCP []mcp.ServerConfig
	for _, s := range cfg.MCP.Servers {
		inlineMCP = append(inlineMCP, mcp.ServerConfig{
			Name:    s.Name,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
		})
	}
	return mcp.LoadMCPConfigs(config.HomeDir(), inlineMCP)
}

type mcpCapabilitySource struct {
	mgr     *mcp.Manager
	configs []mcp.ServerConfig
}

func (s mcpCapabilitySource) Start(ctx context.Context, registry *tool.Registry) {
	if len(s.configs) == 0 {
		return
	}
	s.mgr.StartAll(ctx, s.configs)
	tool.RegisterMCPTools(registry, s.mgr)
}

type skillWatcherSource struct {
	mgr    *skill.Manager
	stopCh <-chan struct{}
}

func (s skillWatcherSource) Start(context.Context, *tool.Registry) {
	s.mgr.StartWatcher(s.stopCh)
}

type skillPromotionSource struct {
	mgr *skill.Manager
}

func (s skillPromotionSource) Start(_ context.Context, registry *tool.Registry) {
	for _, sk := range s.mgr.AutoPromote() {
		if sk.Type == "executable" || sk.Type == "hybrid" {
			registry.Register(tool.NewScriptTool(sk))
			slog.Info(fmt.Sprintf("[skill] Registered ScriptTool: %s", sk.Name))
		} else {
			slog.Info(fmt.Sprintf("[skill] Registered standard skill: %s", sk.Name))
		}
	}
	slog.Info(fmt.Sprintf("[skill] %d skills discovered (pre-read, invoked via SkillTool)", len(s.mgr.List())))
}
