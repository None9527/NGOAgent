package service

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// AggregatedToolDiscovery implements ToolDiscovery by aggregating tools from
// builtin ToolRegistry, MCP servers, and skill sources.
type AggregatedToolDiscovery struct {
	mu             sync.RWMutex
	cache          []ToolCapability
	registry       ToolRegistry
	mcp            MCPToolSource
	skills         SkillSource
	builtinSources map[string]string
}

// NewAggregatedToolDiscovery creates a discovery aggregator.
// Any source can be nil and will be skipped during aggregation.
func NewAggregatedToolDiscovery(registry ToolRegistry, mcp MCPToolSource, skills SkillSource) *AggregatedToolDiscovery {
	return &AggregatedToolDiscovery{
		registry: registry,
		mcp:      mcp,
		skills:   skills,
	}
}

// SetBuiltinSources records builtin tool provider names for discovery metadata.
func (d *AggregatedToolDiscovery) SetBuiltinSources(sources map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.builtinSources = make(map[string]string, len(sources))
	for name, source := range sources {
		d.builtinSources[strings.ToLower(name)] = source
	}
	d.cache = nil
}

// ListCapabilities returns all tools from all sources, deduped by name.
func (d *AggregatedToolDiscovery) ListCapabilities(_ context.Context) []ToolCapability {
	d.mu.RLock()
	if d.cache != nil {
		out := append([]ToolCapability(nil), d.cache...)
		d.mu.RUnlock()
		return out
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = d.aggregate()
	return append([]ToolCapability(nil), d.cache...)
}

// FindByCategory filters capabilities by category.
func (d *AggregatedToolDiscovery) FindByCategory(ctx context.Context, category string) []ToolCapability {
	all := d.ListCapabilities(ctx)
	out := make([]ToolCapability, 0, len(all))
	for _, cap := range all {
		if cap.Category == category {
			out = append(out, cap)
		}
	}
	return out
}

// FindByName returns a specific capability by name, or nil if not found.
func (d *AggregatedToolDiscovery) FindByName(ctx context.Context, name string) *ToolCapability {
	for _, cap := range d.ListCapabilities(ctx) {
		if cap.Name == name {
			c := cap
			return &c
		}
	}
	return nil
}

// FindByTag returns capabilities matching any of the given tags.
func (d *AggregatedToolDiscovery) FindByTag(ctx context.Context, tags ...string) []ToolCapability {
	if len(tags) == 0 {
		return nil
	}
	tagSet := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = struct{}{}
	}

	all := d.ListCapabilities(ctx)
	out := make([]ToolCapability, 0, len(all))
	for _, cap := range all {
		for _, t := range cap.Tags {
			if _, ok := tagSet[strings.ToLower(t)]; ok {
				out = append(out, cap)
				break
			}
		}
	}
	return out
}

// Advertise returns a compact name list suitable for AgentCard capabilities.
func (d *AggregatedToolDiscovery) Advertise(ctx context.Context) []string {
	caps := d.ListCapabilities(ctx)
	names := make([]string, 0, len(caps))
	for _, cap := range caps {
		names = append(names, cap.Name)
	}
	sort.Strings(names)
	return names
}

// Refresh forces a re-scan of all capability sources.
func (d *AggregatedToolDiscovery) Refresh(_ context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = d.aggregate()
}

func (d *AggregatedToolDiscovery) aggregate() []ToolCapability {
	seen := make(map[string]struct{})
	var out []ToolCapability

	add := func(cap ToolCapability) {
		key := strings.ToLower(cap.Name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, cap)
	}

	// Builtin tools.
	if d.registry != nil {
		for _, tool := range d.registry.List() {
			if !tool.Enabled {
				continue
			}
			source := d.builtinSources[strings.ToLower(tool.Name)]
			tags := []string{"builtin"}
			if source != "" {
				tags = append(tags, source)
			}
			add(ToolCapability{
				Name:     tool.Name,
				Category: "builtin",
				Source:   source,
				SourceKind: "builtin_provider",
				Tags:     tags,
			})
		}
	}

	// MCP tools.
	if d.mcp != nil {
		for _, tool := range d.mcp.ListMCPTools() {
			add(ToolCapability{
				Name:        tool.Name,
				Description: tool.Description,
				Category:    "mcp",
				Source:      tool.Server,
				SourceKind:  "mcp_server",
				InputSchema: tool.InputSchema,
				Tags:        []string{"mcp", tool.Server},
			})
		}
	}

	// Skills.
	if d.skills != nil {
		for _, skill := range d.skills.ListSkills() {
			if !skill.Enabled {
				continue
			}
			add(ToolCapability{
				Name:        skill.Name,
				Description: skill.Description,
				Category:    "skill",
				Source:      skill.Name,
				SourceKind:  "skill",
				SourcePath:  skill.Path,
				Tags:        []string{"skill", skill.Name},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})

	return out
}

// Compile-time interface check.
var _ ToolDiscovery = (*AggregatedToolDiscovery)(nil)
