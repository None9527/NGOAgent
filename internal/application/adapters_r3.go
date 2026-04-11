package application

import (
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
)

// mcpDiscoveryAdapter bridges mcp.Manager → service.MCPToolSource
// for use by AggregatedToolDiscovery without import cycles.
type mcpDiscoveryAdapter struct {
	mgr *mcp.Manager
}

func (a *mcpDiscoveryAdapter) ListMCPTools() []service.MCPToolDescriptor {
	if a.mgr == nil {
		return nil
	}
	tools := a.mgr.ListTools()
	out := make([]service.MCPToolDescriptor, len(tools))
	for i, t := range tools {
		out[i] = service.MCPToolDescriptor{
			Name:        t.Name,
			Description: t.Description,
			Server:      t.ServerName,
			InputSchema: t.InputSchema,
		}
	}
	return out
}

// skillDiscoveryAdapter bridges skill.Manager → service.SkillSource
// for use by AggregatedToolDiscovery without import cycles.
type skillDiscoveryAdapter struct {
	mgr *skill.Manager
}

func (a *skillDiscoveryAdapter) ListSkills() []service.SkillDescriptor {
	if a.mgr == nil {
		return nil
	}
	skills := a.mgr.List()
	out := make([]service.SkillDescriptor, len(skills))
	for i, s := range skills {
		out[i] = service.SkillDescriptor{
			Name:        s.Name,
			Description: s.Description,
			Path:        s.Path,
			Enabled:     s.Enabled,
		}
	}
	return out
}

// Compile-time interface checks.
var _ service.MCPToolSource = (*mcpDiscoveryAdapter)(nil)
var _ service.SkillSource = (*skillDiscoveryAdapter)(nil)
