package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

func registerHotReloadSubscriptions(
	cfgMgr *config.Manager,
	router *llm.Router,
	secHook *security.Hook,
	mcpMgr *mcp.Manager,
	registry *tool.Registry,
	loop *service.AgentLoop,
	loopPool func() *service.LoopPool,
) {
	cfgMgr.Subscribe("llm", func(old, new *config.Config) {
		slog.Info(fmt.Sprint("[hot-reload] LLM config changed, rebuilding providers"))
		router.Reload(buildLLMProviders(new))
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
		tool.SyncMCPTools(registry, mcpMgr)
	})

	cfgMgr.Subscribe("agent", func(old, new *config.Config) {
		slog.Info(fmt.Sprintf("[hot-reload] Agent config changed: planning=%v max_steps=%d max_subagents=%d",
			new.Agent.PlanningMode, new.Agent.MaxSteps, new.Agent.MaxSubagents))
		loop.ReloadConfig(new)
		if pool := loopPool(); pool != nil {
			pool.ForEach(func(l *service.AgentLoop) {
				l.ReloadConfig(new)
			})
		}
	})
}
