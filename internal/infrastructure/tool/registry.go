// Package tool provides the tool interface, registry, and built-in tool implementations.
package tool

import (
	"context"
	"path/filepath"
	"sync"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// Tool is the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any // JSON Schema for parameters
	Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error)
}

// ToolInfo contains metadata about a registered tool.
type ToolInfo struct {
	Name        string
	Description string
	Enabled     bool
}

// Registry manages tool registration, lookup, enable/disable, and path resolution.
type Registry struct {
	mu           sync.RWMutex
	tools        map[string]Tool
	disabled     map[string]bool
	workspaceDir string // Default workspace for resolving relative paths
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		disabled: make(map[string]bool),
	}
}

// SetWorkspaceDir sets the default workspace directory for path resolution.
// Relative paths in tool args will be resolved against this directory.
func (r *Registry) SetWorkspaceDir(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceDir = dir
}

// ResolvePath resolves a path: absolute paths pass through, relative paths
// are joined with the configured workspace directory.
func (r *Registry) ResolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	r.mu.RLock()
	ws := r.workspaceDir
	r.mu.RUnlock()
	if ws == "" {
		return path // No workspace configured, return as-is
	}
	return filepath.Join(ws, path)
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Execute runs a tool by name with the given arguments.
// Before execution, resolves relative file paths against the workspace directory.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	disabled := r.disabled[name]
	r.mu.RUnlock()

	if !ok {
		return dtool.ToolResult{}, &ToolError{Code: "not_found", Message: "tool not found: " + name}
	}
	if disabled {
		return dtool.ToolResult{}, &ToolError{Code: "disabled", Message: "tool is disabled: " + name}
	}

	// ── Path Resolution: resolve relative paths against workspace ──
	r.resolveToolPaths(name, args)

	return t.Execute(ctx, args)
}

// resolveToolPaths resolves relative paths in tool args based on tool type.
// File tools: resolves "path" arg. Shell tools: sets default "cwd".
func (r *Registry) resolveToolPaths(name string, args map[string]any) {
	r.mu.RLock()
	ws := r.workspaceDir
	r.mu.RUnlock()
	if ws == "" {
		return
	}

	switch name {
	case "read_file", "write_file", "edit_file", "glob", "grep_search", "undo_edit":
		// Resolve the "path" argument
		if p, ok := args["path"].(string); ok && p != "" && !filepath.IsAbs(p) {
			args["path"] = filepath.Join(ws, p)
		}
		// grep_search also has "directory" field
		if d, ok := args["directory"].(string); ok && d != "" && !filepath.IsAbs(d) {
			args["directory"] = filepath.Join(ws, d)
		}
	case "run_command":
		// Set default cwd to workspace if not specified
		if cwd, ok := args["cwd"].(string); !ok || cwd == "" {
			args["cwd"] = ws
		} else if !filepath.IsAbs(cwd) {
			args["cwd"] = filepath.Join(ws, cwd)
		}
	}
}

// ListDefinitions returns LLM-compatible tool definitions for all enabled tools.
func (r *Registry) ListDefinitions() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		if r.disabled[t.Name()] {
			continue // Skip disabled tools
		}
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return defs
}

// List returns metadata about all registered tools.
func (r *Registry) List() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]ToolInfo, 0, len(r.tools))
	for _, t := range r.tools {
		infos = append(infos, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Enabled:     !r.disabled[t.Name()],
		})
	}
	return infos
}

// Enable re-enables a disabled tool.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return &ToolError{Code: "not_found", Message: "tool not found: " + name}
	}
	delete(r.disabled, name)
	return nil
}

// Disable prevents a tool from being executed or listed to the LLM.
func (r *Registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return &ToolError{Code: "not_found", Message: "tool not found: " + name}
	}
	r.disabled[name] = true
	return nil
}

// IsEnabled checks whether a tool is enabled.
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.disabled[name]
}

// ToolError is a structured tool execution error.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string { return e.Message }
