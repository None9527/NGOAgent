package tool

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// sanitizeSegment converts a name to a valid identifier segment (alphanum + underscore).
var reSanitize = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitize(s string) string {
	return reSanitize.ReplaceAllString(strings.ToLower(s), "_")
}

// mcpToolName returns the canonical prefixed tool name: mcp__<server>__<tool>.
// This matches the Claude Code MCP naming convention (MCP_NO_PREFIX disabled case).
func mcpToolName(serverName, toolName string) string {
	return fmt.Sprintf("mcp__%s__%s", sanitize(serverName), sanitize(toolName))
}

// MCPToolAdapter wraps an MCP-discovered tool as a native agent tool.
type MCPToolAdapter struct {
	server  string        // canonical server name
	tool    mcp.MCPTool   // original MCP tool definition
	manager *mcp.Manager
}

// NewMCPToolAdapter wraps an MCP tool for the agent tool registry.
func NewMCPToolAdapter(tool mcp.MCPTool, manager *mcp.Manager) *MCPToolAdapter {
	return &MCPToolAdapter{
		server:  tool.ServerName,
		tool:    tool,
		manager: manager,
	}
}

// Name returns the prefixed tool name exposed to the LLM: mcp__<server>__<tool>.
func (a *MCPToolAdapter) Name() string {
	return mcpToolName(a.server, a.tool.Name)
}

// Description returns the MCP tool description.
func (a *MCPToolAdapter) Description() string {
	d := a.tool.Description
	if d == "" {
		d = fmt.Sprintf("MCP tool %q from server %q", a.tool.Name, a.server)
	}
	return d
}

// Schema returns the input JSON schema from the MCP server.
func (a *MCPToolAdapter) Schema() map[string]any {
	if a.tool.InputSchema != nil {
		return a.tool.InputSchema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// IsReadOnly reports whether the tool is annotated as read-only.
func (a *MCPToolAdapter) IsReadOnly() bool {
	if a.tool.Annotations != nil {
		return a.tool.Annotations.ReadOnlyHint
	}
	return false
}

// IsDestructive reports whether the tool is annotated as destructive.
func (a *MCPToolAdapter) IsDestructive() bool {
	if a.tool.Annotations != nil {
		return a.tool.Annotations.DestructiveHint
	}
	return false
}

// Execute forwards the call to the MCP server.
// The toolName passed in is the prefixed name (mcp__server__tool); we resolve
// the original MCP tool name from our stored definition.
func (a *MCPToolAdapter) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	return a.manager.CallTool(ctx, a.tool.Name, args)
}

// ─── Registry helpers ──────────────────────────────────────────────────────

// RegisterMCPTools discovers tools from the MCP Manager and registers them.
// Collision-resistant: skips tools whose prefixed names clash with existing tools.
func RegisterMCPTools(registry *Registry, manager *mcp.Manager) {
	tools := manager.ListTools()
	existing := map[string]bool{}
	for _, def := range registry.ListDefinitions() {
		existing[def.Function.Name] = true
	}

	registered := 0
	for _, t := range tools {
		prefixed := mcpToolName(t.ServerName, t.Name)
		if existing[prefixed] {
			continue
		}
		registry.Register(NewMCPToolAdapter(t, manager))
		existing[prefixed] = true
		registered++
	}
	if registered > 0 {
		// Log summary, not per-tool (avoids log spam for large MCP servers)
		_ = registered
	}
}

// MCPDefs converts MCP tools to LLM tool definitions (for prompt/tool injection).
func MCPDefs(manager *mcp.Manager) []llm.ToolDef {
	tools := manager.ListTools()
	defs := make([]llm.ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        mcpToolName(t.ServerName, t.Name),
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return defs
}
