package service

import "context"

// ToolCapability describes a single tool's capability for discovery and advertisement.
type ToolCapability struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`      // "builtin", "mcp", "skill"
	Source      string   `json:"source"`         // stable provider/server/skill identity
	SourceKind  string   `json:"source_kind,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	InputSchema any      `json:"input_schema,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Version     string   `json:"version,omitempty"`
}

// ToolDiscovery provides dynamic tool discovery and capability advertisement.
type ToolDiscovery interface {
	// ListCapabilities returns all available tool capabilities from all sources.
	ListCapabilities(ctx context.Context) []ToolCapability

	// FindByCategory filters capabilities by category.
	FindByCategory(ctx context.Context, category string) []ToolCapability

	// FindByName returns a specific capability by name, or nil if not found.
	FindByName(ctx context.Context, name string) *ToolCapability

	// FindByTag returns capabilities matching any of the given tags.
	FindByTag(ctx context.Context, tags ...string) []ToolCapability

	// Advertise returns a compact description suitable for A2A AgentCard filling.
	Advertise(ctx context.Context) []string

	// Refresh forces a re-scan of all capability sources.
	Refresh(ctx context.Context)
}

// MCPToolSource provides MCP tool listing for discovery aggregation.
type MCPToolSource interface {
	ListMCPTools() []MCPToolDescriptor
}

// MCPToolDescriptor describes a single tool from an MCP server.
type MCPToolDescriptor struct {
	Name        string
	Description string
	Server      string
	InputSchema any
}

// SkillSource provides skill listing for discovery aggregation.
type SkillSource interface {
	ListSkills() []SkillDescriptor
}

// SkillDescriptor describes a single skill for discovery.
type SkillDescriptor struct {
	Name        string
	Description string
	Path        string
	Enabled     bool
}
