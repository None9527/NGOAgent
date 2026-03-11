package tool

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// MCPToolAdapter wraps an MCP-discovered tool as a native agent tool.
type MCPToolAdapter struct {
	tool    mcp.MCPTool
	manager *mcp.Manager
}

// NewMCPToolAdapter wraps an MCP tool for the agent registry.
func NewMCPToolAdapter(tool mcp.MCPTool, manager *mcp.Manager) *MCPToolAdapter {
	return &MCPToolAdapter{tool: tool, manager: manager}
}

func (a *MCPToolAdapter) Name() string        { return a.tool.Name }
func (a *MCPToolAdapter) Description() string { return a.tool.Description }

func (a *MCPToolAdapter) Schema() map[string]any {
	if a.tool.InputSchema != nil {
		return a.tool.InputSchema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (a *MCPToolAdapter) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	return a.manager.CallTool(ctx, a.tool.Name, args)
}

// RegisterMCPTools discovers tools from MCP manager and registers them.
func RegisterMCPTools(registry *Registry, manager *mcp.Manager) {
	tools := manager.ListTools()
	for _, t := range tools {
		// Check for name collision
		exists := false
		for _, existing := range registry.ListDefinitions() {
			if existing.Function.Name == t.Name {
				exists = true
				break
			}
		}
		if !exists {
			registry.Register(NewMCPToolAdapter(t, manager))
		}
	}
}

// MCPDefs converts MCP tools to LLM tool definitions (for prompt assembly).
func MCPDefs(manager *mcp.Manager) []llm.ToolDef {
	tools := manager.ListTools()
	defs := make([]llm.ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return defs
}
