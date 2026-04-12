// Package mcp provides a full MCP (Model Context Protocol) client implementation.
// Protocol: JSON-RPC 2.0 over stdio with JSON Lines framing (MCP 2024-11-05 spec).
// Features: concurrent-safe routing, tool/resource/prompt discovery, change notifications,
// auto-reconnect, and full content-type support (text, image, resource).
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ─── JSON-RPC types ──────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"` // nil = notification
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP domain types ────────────────────────────────────────────────────────

// ToolAnnotations mirrors MCP spec annotations on tool definitions.
type ToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint,omitempty"`
	DestructiveHint bool `json:"destructiveHint,omitempty"`
	OpenWorldHint   bool `json:"openWorldHint,omitempty"`
	IdempotentHint  bool `json:"idempotentHint,omitempty"`
}

// MCPTool represents a tool discovered from an MCP server.
type MCPTool struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema map[string]any   `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
	ServerName  string           // populated at discovery time
}

// MCPContent is a single item in an MCP tools/call response.
type MCPContent struct {
	Type     string               `json:"type"`               // "text" | "image" | "resource"
	Text     string               `json:"text,omitempty"`     // type=text
	Data     string               `json:"data,omitempty"`     // type=image (base64)
	MimeType string               `json:"mimeType,omitempty"` // type=image
	Resource *MCPEmbeddedResource `json:"resource,omitempty"` // type=resource
}

// MCPEmbeddedResource is inline resource content.
type MCPEmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // base64
}

// MCPResource represents a resource listed from an MCP server.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	ServerName  string // populated at discovery time
}

// MCPPrompt represents a prompt template from an MCP server.
type MCPPrompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
	ServerName  string
}

// PromptArgument is a prompt parameter definition.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ServerConfig configures an MCP server to launch.
type ServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string // Working directory for the server process (empty = inherit)
}

// ─── Server (per-process state) ───────────────────────────────────────────────

// pendingCall holds an in-flight RPC request.
type pendingCall struct {
	ch chan rpcResponse
}

// Server represents a single running MCP server subprocess.
type Server struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string // Working directory for the server process

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	nextID    int
	running   bool
	tools     []MCPTool
	resources []MCPResource
	prompts   []MCPPrompt

	pendingMu sync.Mutex
	pending   map[int]*pendingCall

	// triggerRefresh allows notifications to signal the Manager.
	onToolsChanged func()
}

// ─── Manager ─────────────────────────────────────────────────────────────────

// Manager manages the lifecycle of one or more MCP server subprocesses.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*Server

	// globalID for RPC IDs that span all servers (atomic)
	idGen atomic.Int64
}

// NewManager creates an MCP manager.
func NewManager() *Manager {
	return &Manager{servers: make(map[string]*Server)}
}

// ─── Public API ────────────────────────────────────────────────────────────

// Start launches a named MCP server and discovers its capabilities.
func (m *Manager) Start(ctx context.Context, name, command string, args []string, env map[string]string) error {
	return m.StartWithCwd(ctx, name, command, args, env, "")
}

// StartWithCwd launches a named MCP server with a specific working directory.
func (m *Manager) StartWithCwd(ctx context.Context, name, command string, args []string, env map[string]string, cwd string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.servers[name]; ok {
		return fmt.Errorf("MCP server %q already running", name)
	}

	srv := &Server{
		Name:    name,
		Command: command,
		Args:    args,
		Env:     env,
		Cwd:     cwd,
		pending: make(map[int]*pendingCall),
	}
	srv.onToolsChanged = func() { m.refreshServer(srv) }

	if err := m.spawnServer(ctx, srv); err != nil {
		return err
	}

	m.servers[name] = srv
	return nil
}

// Stop shuts down a named MCP server.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	srv, ok := m.servers[name]
	if ok {
		delete(m.servers, name)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}
	m.killServer(srv)
	return nil
}

// StartAll launches all configured servers (best-effort, logs failures).
func (m *Manager) StartAll(ctx context.Context, configs []ServerConfig) {
	for _, cfg := range configs {
		if err := m.StartWithCwd(ctx, cfg.Name, cfg.Command, cfg.Args, cfg.Env, cfg.Cwd); err != nil {
			slog.Info(fmt.Sprintf("[mcp] Failed to start %q: %v", cfg.Name, err))
		}
	}
}

// Reload stops all servers and starts fresh with the new config.
func (m *Manager) Reload(ctx context.Context, configs []ServerConfig) {
	m.mu.RLock()
	names := make([]string, 0, len(m.servers))
	for n := range m.servers {
		names = append(names, n)
	}
	m.mu.RUnlock()

	for _, n := range names {
		m.Stop(n) //nolint:errcheck
	}
	m.StartAll(ctx, configs)
}

// ListTools returns all tools across all servers.
func (m *Manager) ListTools() []MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []MCPTool
	for _, srv := range m.servers {
		srv.mu.Lock()
		out = append(out, srv.tools...)
		srv.mu.Unlock()
	}
	return out
}

// ListResources returns all resources across all servers.
func (m *Manager) ListResources() []MCPResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []MCPResource
	for _, srv := range m.servers {
		srv.mu.Lock()
		out = append(out, srv.resources...)
		srv.mu.Unlock()
	}
	return out
}

// ListPrompts returns all prompt templates across all servers.
func (m *Manager) ListPrompts() []MCPPrompt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []MCPPrompt
	for _, srv := range m.servers {
		srv.mu.Lock()
		out = append(out, srv.prompts...)
		srv.mu.Unlock()
	}
	return out
}

// ListServers returns a map of serverName → running status.
func (m *Manager) ListServers() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]bool, len(m.servers))
	for name, srv := range m.servers {
		srv.mu.Lock()
		out[name] = srv.running
		srv.mu.Unlock()
	}
	return out
}

// CallTool routes a tool call to the correct server and returns the ToolResult.
func (m *Manager) CallTool(ctx context.Context, toolName string, args map[string]any) (dtool.ToolResult, error) {
	m.mu.RLock()
	var target *Server
	var mcpName string // actual MCP tool name (without prefix)
	for _, srv := range m.servers {
		srv.mu.Lock()
		for _, t := range srv.tools {
			if t.Name == toolName {
				target = srv
				mcpName = t.Name // MCPTool.Name is the original name
				break
			}
		}
		srv.mu.Unlock()
		if target != nil {
			break
		}
	}
	m.mu.RUnlock()

	if target == nil {
		return dtool.ToolResult{}, fmt.Errorf("MCP tool %q not found", toolName)
	}
	return callTool(ctx, target, mcpName, args)
}

// ReadResource fetches the content of a named resource from its server.
func (m *Manager) ReadResource(ctx context.Context, serverName, uri string) (string, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("MCP server %q not found", serverName)
	}

	result, err := sendRPC(ctx, srv, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", err
	}

	var resp struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var sb strings.Builder
	for _, c := range resp.Contents {
		if c.Text != "" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

// GetPrompt retrieves a rendered prompt from its server.
func (m *Manager) GetPrompt(ctx context.Context, serverName, promptName string, args map[string]string) (string, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("MCP server %q not found", serverName)
	}

	result, err := sendRPC(ctx, srv, "prompts/get", map[string]any{
		"name":      promptName,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}

	var resp struct {
		Messages []struct {
			Role    string     `json:"role"`
			Content MCPContent `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var sb strings.Builder
	for _, msg := range resp.Messages {
		if msg.Content.Text != "" {
			sb.WriteString(msg.Content.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// ─── Internal: spawn, kill, reconnect ────────────────────────────────────────

func (m *Manager) spawnServer(ctx context.Context, srv *Server) error {
	cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)

	// Set working directory if configured
	if srv.Cwd != "" {
		cmd.Dir = srv.Cwd
	}

	// Inherit parent environment + server-specific overrides
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe for %q: %w", srv.Name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe for %q: %w", srv.Name, err)
	}
	cmd.Stderr = os.Stderr // surface server errors to host logs

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server %q: %w", srv.Name, err)
	}

	srv.mu.Lock()
	srv.cmd = cmd
	srv.stdin = stdin
	srv.stdout = bufio.NewReader(stdoutPipe)
	srv.running = true
	srv.pending = make(map[int]*pendingCall)
	srv.mu.Unlock()

	// Start background reader goroutine
	go m.readLoop(srv)

	// Perform MCP handshake + capability discovery
	if err := m.initialize(srv); err != nil {
		slog.Info(fmt.Sprintf("[mcp] Server %q initialize failed: %v", srv.Name, err))
		// Non-fatal — server might still be usable
	}

	slog.Info(fmt.Sprintf("[mcp] Server %q started: %d tools, %d resources, %d prompts",
		srv.Name, len(srv.tools), len(srv.resources), len(srv.prompts)))
	return nil
}

func (m *Manager) killServer(srv *Server) {
	srv.mu.Lock()
	srv.running = false
	if srv.stdin != nil {
		srv.stdin.Close()
	}
	if srv.cmd != nil && srv.cmd.Process != nil {
		srv.cmd.Process.Kill()
	}
	// Drain all pending calls with an error
	srv.pendingMu.Lock()
	for id, call := range srv.pending {
		call.ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: "server stopped"}}
		delete(srv.pending, id)
	}
	srv.pendingMu.Unlock()
	srv.mu.Unlock()
}

// refreshServer re-discovers tools/resources/prompts after a notification.
func (m *Manager) refreshServer(srv *Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools, err := discoverTools(ctx, srv)
	if err == nil {
		srv.mu.Lock()
		srv.tools = tools
		srv.mu.Unlock()
		slog.Info(fmt.Sprintf("[mcp] Server %q tools refreshed: %d tools", srv.Name, len(tools)))
	}
}

// ─── Internal: JSON Lines I/O (MCP 2024-11-05 spec) ──────────────────────────

// writeRPC marshals a JSON-RPC message as a single JSON line.
func writeRPC(srv *Server, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	// JSON Lines: one complete JSON object per line
	if _, err := srv.stdin.Write(data); err != nil {
		return err
	}
	_, err = io.WriteString(srv.stdin, "\n")
	return err
}

// readFrame reads one JSON Lines message (one line = one JSON object).
func readFrame(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		// Trim whitespace and skip empty lines
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		return line, nil
	}
}

// ─── Internal: reader goroutine ──────────────────────────────────────────────

// readLoop reads messages from a server in a dedicated goroutine.
// It routes responses to pending calls and dispatches notifications.
func (m *Manager) readLoop(srv *Server) {
	for {
		srv.mu.Lock()
		r := srv.stdout
		running := srv.running
		srv.mu.Unlock()

		if !running || r == nil {
			return
		}

		data, err := readFrame(r)
		if err != nil {
			srv.mu.Lock()
			srv.running = false
			srv.mu.Unlock()
			slog.Info(fmt.Sprintf("[mcp] Server %q reader error: %v", srv.Name, err))
			// Drain pending
			srv.pendingMu.Lock()
			for id, call := range srv.pending {
				call.ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: err.Error()}}
				delete(srv.pending, id)
			}
			srv.pendingMu.Unlock()
			return
		}

		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			slog.Info(fmt.Sprintf("[mcp] Server %q bad message: %v", srv.Name, err))
			continue
		}

		if resp.ID != nil {
			// Response to a pending RPC call
			srv.pendingMu.Lock()
			call, ok := srv.pending[*resp.ID]
			if ok {
				delete(srv.pending, *resp.ID)
			}
			srv.pendingMu.Unlock()
			if ok {
				call.ch <- resp
			}
		} else {
			// Notification (no ID)
			var notif struct {
				Method string `json:"method"`
			}
			json.Unmarshal(data, &notif) //nolint:errcheck
			m.handleNotification(srv, notif.Method)
		}
	}
}

// handleNotification dispatches MCP server-sent notifications.
func (m *Manager) handleNotification(srv *Server, method string) {
	switch method {
	case "notifications/tools/list_changed":
		slog.Info(fmt.Sprintf("[mcp] Server %q: tools list changed, refreshing...", srv.Name))
		go srv.onToolsChanged()
	case "notifications/resources/list_changed":
		slog.Info(fmt.Sprintf("[mcp] Server %q: resources list changed", srv.Name))
		// Future: refresh resources
	case "notifications/prompts/list_changed":
		slog.Info(fmt.Sprintf("[mcp] Server %q: prompts list changed", srv.Name))
		// Future: refresh prompts
	default:
		slog.Info(fmt.Sprintf("[mcp] Server %q: unhandled notification %q", srv.Name, method))
	}
}

// ─── Internal: RPC call abstraction ─────────────────────────────────────────

// sendRPC sends a JSON-RPC request and waits for the response.
func sendRPC(ctx context.Context, srv *Server, method string, params any) (json.RawMessage, error) {
	srv.mu.Lock()
	srv.nextID++
	id := srv.nextID
	srv.mu.Unlock()

	ch := make(chan rpcResponse, 1)
	srv.pendingMu.Lock()
	srv.pending[id] = &pendingCall{ch: ch}
	srv.pendingMu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if err := writeRPC(srv, req); err != nil {
		srv.pendingMu.Lock()
		delete(srv.pending, id)
		srv.pendingMu.Unlock()
		return nil, fmt.Errorf("write RPC to %q: %w", srv.Name, err)
	}

	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		srv.pendingMu.Lock()
		delete(srv.pending, id)
		srv.pendingMu.Unlock()
		return nil, fmt.Errorf("RPC timeout for method %q on server %q", method, srv.Name)
	case <-ctx.Done():
		srv.pendingMu.Lock()
		delete(srv.pending, id)
		srv.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// sendNotification sends a JSON-RPC notification (no response expected).
func sendNotification(srv *Server, method string, params any) {
	req := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	if err := writeRPC(srv, req); err != nil {
		slog.Info(fmt.Sprintf("[mcp] Failed to send notification %q to %q: %v", method, srv.Name, err))
	}
}

// ─── Internal: initialization + discovery ────────────────────────────────────

func (m *Manager) initialize(srv *Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. initialize
	_, err := sendRPC(ctx, srv, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"roots":    map[string]any{"listChanged": true},
			"sampling": map[string]any{},
		},
		"clientInfo": map[string]any{
			"name":    "ngoagent",
			"version": "0.5.0",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// 2. notifications/initialized (fire-and-forget)
	sendNotification(srv, "notifications/initialized", nil)

	// 3. Discover capabilities in parallel
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		tools, err := discoverTools(ctx, srv)
		if err != nil {
			slog.Info(fmt.Sprintf("[mcp] %q tools/list error: %v", srv.Name, err))
			return
		}
		srv.mu.Lock()
		srv.tools = tools
		srv.mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		resources, err := discoverResources(ctx, srv)
		if err != nil {
			slog.Info(fmt.Sprintf("[mcp] %q resources/list error (server may not support): %v", srv.Name, err))
			return
		}
		srv.mu.Lock()
		srv.resources = resources
		srv.mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		prompts, err := discoverPrompts(ctx, srv)
		if err != nil {
			slog.Info(fmt.Sprintf("[mcp] %q prompts/list error (server may not support): %v", srv.Name, err))
			return
		}
		srv.mu.Lock()
		srv.prompts = prompts
		srv.mu.Unlock()
	}()

	wg.Wait()
	return nil
}

func discoverTools(ctx context.Context, srv *Server) ([]MCPTool, error) {
	result, err := sendRPC(ctx, srv, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	for i := range resp.Tools {
		resp.Tools[i].ServerName = srv.Name
	}
	return resp.Tools, nil
}

func discoverResources(ctx context.Context, srv *Server) ([]MCPResource, error) {
	result, err := sendRPC(ctx, srv, "resources/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Resources []MCPResource `json:"resources"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse resources/list: %w", err)
	}
	for i := range resp.Resources {
		resp.Resources[i].ServerName = srv.Name
	}
	return resp.Resources, nil
}

func discoverPrompts(ctx context.Context, srv *Server) ([]MCPPrompt, error) {
	result, err := sendRPC(ctx, srv, "prompts/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Prompts []MCPPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse prompts/list: %w", err)
	}
	for i := range resp.Prompts {
		resp.Prompts[i].ServerName = srv.Name
	}
	return resp.Prompts, nil
}

// ─── Internal: tool execution ────────────────────────────────────────────────

func callTool(ctx context.Context, srv *Server, toolName string, args map[string]any) (dtool.ToolResult, error) {
	result, err := sendRPC(ctx, srv, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return dtool.ToolResult{}, err
	}

	var resp struct {
		Content []MCPContent `json:"content"`
		IsError bool         `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		// Return raw JSON as string fallback
		return dtool.ToolResult{Output: string(result)}, nil
	}

	if resp.IsError {
		// MCP tools can signal errors via isError flag
		var msgs []string
		for _, c := range resp.Content {
			if c.Text != "" {
				msgs = append(msgs, c.Text)
			}
		}
		errText := strings.Join(msgs, "\n")
		return dtool.ToolResult{Output: errText}, fmt.Errorf("MCP tool error: %s", errText)
	}

	// Collect all content into a unified output string
	var sb strings.Builder
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		case "image":
			// Surface image as a reference marker; agent can decide what to do
			sb.WriteString(fmt.Sprintf("[image: data:%s;base64,%s...]", c.MimeType, truncate(c.Data, 40)))
		case "resource":
			if c.Resource != nil {
				if c.Resource.Text != "" {
					sb.WriteString(c.Resource.Text)
				} else {
					sb.WriteString(fmt.Sprintf("[resource: %s]", c.Resource.URI))
				}
			}
		}
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
