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

// capabilitySourceDescriptor is the minimum identity contract shared by all
// runtime capability sources, including future plugin-backed sources.
type capabilitySourceDescriptor struct {
	Name            string
	Kind            string
	DiscoverySource string
}

type capabilitySourceStartStatus string

const (
	capabilitySourceStarted capabilitySourceStartStatus = "started"
	capabilitySourceSkipped capabilitySourceStartStatus = "skipped"
	capabilitySourceFailed  capabilitySourceStartStatus = "failed"
)

type capabilitySourceStartResult struct {
	Source  capabilitySourceDescriptor
	Status  capabilitySourceStartStatus
	Details map[string]any
}

type capabilitySource interface {
	Descriptor() capabilitySourceDescriptor
	Start(context.Context, *tool.Registry) capabilitySourceStartResult
}

func startRuntimeCapabilities(
	cfg *config.Config,
	stopCh <-chan struct{},
	registry *tool.Registry,
	mcpMgr *mcp.Manager,
	skillMgr *skill.Manager,
) []capabilitySourceStartResult {
	sources := []capabilitySource{
		skillWatcherSource{mgr: skillMgr, stopCh: stopCh},
		mcpCapabilitySource{mgr: mcpMgr, configs: loadInlineMCPConfigs(cfg)},
		skillPromotionSource{mgr: skillMgr},
	}
	results := make([]capabilitySourceStartResult, 0, len(sources))
	names := make([]string, 0, len(sources))
	started := 0
	skipped := 0
	failed := 0
	for _, source := range sources {
		descriptor := source.Descriptor()
		names = append(names, descriptor.Name)
		result := source.Start(context.Background(), registry)
		if result.Source.Name == "" {
			result.Source = descriptor
		}
		switch result.Status {
		case capabilitySourceStarted:
			started++
		case capabilitySourceFailed:
			failed++
		default:
			skipped++
		}
		results = append(results, result)
	}
	slog.Info("[runtime] capability sources started",
		"sources", names,
		"started", started,
		"skipped", skipped,
		"failed", failed,
	)
	return results
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

func (mcpCapabilitySource) Descriptor() capabilitySourceDescriptor {
	return capabilitySourceDescriptor{
		Name:            "mcp",
		Kind:            "mcp_server",
		DiscoverySource: "mcp",
	}
}

func (s mcpCapabilitySource) Start(ctx context.Context, registry *tool.Registry) capabilitySourceStartResult {
	descriptor := s.Descriptor()
	if len(s.configs) == 0 {
		return capabilitySourceStartResult{
			Source: descriptor,
			Status: capabilitySourceSkipped,
			Details: map[string]any{
				"configured_servers": 0,
			},
		}
	}
	s.mgr.StartAll(ctx, s.configs)
	tool.RegisterMCPTools(registry, s.mgr)
	return capabilitySourceStartResult{
		Source: descriptor,
		Status: capabilitySourceStarted,
		Details: map[string]any{
			"configured_servers": len(s.configs),
			"registered_tools":   len(s.mgr.ListTools()),
		},
	}
}

type skillWatcherSource struct {
	mgr    *skill.Manager
	stopCh <-chan struct{}
}

func (skillWatcherSource) Descriptor() capabilitySourceDescriptor {
	return capabilitySourceDescriptor{
		Name:            "skill_watcher",
		Kind:            "skill_runtime",
		DiscoverySource: "skill",
	}
}

func (s skillWatcherSource) Start(context.Context, *tool.Registry) capabilitySourceStartResult {
	s.mgr.StartWatcher(s.stopCh)
	return capabilitySourceStartResult{
		Source: s.Descriptor(),
		Status: capabilitySourceStarted,
		Details: map[string]any{
			"discovered_skills": len(s.mgr.List()),
		},
	}
}

type skillPromotionSource struct {
	mgr *skill.Manager
}

func (skillPromotionSource) Descriptor() capabilitySourceDescriptor {
	return capabilitySourceDescriptor{
		Name:            "skill_promotion",
		Kind:            "skill_runtime",
		DiscoverySource: "skill",
	}
}

func (s skillPromotionSource) Start(_ context.Context, registry *tool.Registry) capabilitySourceStartResult {
	promoted := 0
	for _, sk := range s.mgr.AutoPromote() {
		if sk.Type == "executable" || sk.Type == "hybrid" {
			registry.Register(tool.NewScriptTool(sk))
			slog.Info(fmt.Sprintf("[skill] Registered ScriptTool: %s", sk.Name))
			promoted++
		} else {
			slog.Info(fmt.Sprintf("[skill] Registered standard skill: %s", sk.Name))
		}
	}
	slog.Info(fmt.Sprintf("[skill] %d skills discovered (pre-read, invoked via SkillTool)", len(s.mgr.List())))
	status := capabilitySourceStarted
	if promoted == 0 {
		status = capabilitySourceSkipped
	}
	return capabilitySourceStartResult{
		Source: s.Descriptor(),
		Status: status,
		Details: map[string]any{
			"discovered_skills": len(s.mgr.List()),
			"promoted_tools":    promoted,
		},
	}
}
