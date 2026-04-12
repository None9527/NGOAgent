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

func startRuntimeCapabilities(
	cfg *config.Config,
	stopCh <-chan struct{},
	registry *tool.Registry,
	mcpMgr *mcp.Manager,
	skillMgr *skill.Manager,
) {
	skillMgr.StartWatcher(stopCh)

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
		tool.RegisterMCPTools(registry, mcpMgr)
	}

	for _, sk := range skillMgr.AutoPromote() {
		if sk.Type == "executable" || sk.Type == "hybrid" {
			registry.Register(tool.NewScriptTool(sk))
			slog.Info(fmt.Sprintf("[skill] Registered ScriptTool: %s", sk.Name))
		} else {
			slog.Info(fmt.Sprintf("[skill] Registered standard skill: %s", sk.Name))
		}
	}
	slog.Info(fmt.Sprintf("[skill] %d skills discovered (pre-read, invoked via SkillTool)", len(skillMgr.List())))
}
