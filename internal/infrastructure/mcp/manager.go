// Package mcp provides MCP (Model Context Protocol) server lifecycle management
// with JSON-RPC 2.0 over stdio communication.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPTool represents a tool discovered from an MCP server.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	ServerName  string         // Which MCP server owns this tool
}

// Server represents a running MCP server subprocess.
type Server struct {
	Name    string
	Command string
	Args    []string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	tools   []MCPTool
	nextID  int
	mu      sync.Mutex
	running bool
}

// Manager manages MCP server lifecycle and tool discovery.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*Server
}

// NewManager creates an MCP manager.
func NewManager() *Manager {
	return &Manager{
		servers: make(map[string]*Server),
	}
}

// Start launches an MCP server subprocess and discovers its tools.
func (m *Manager) Start(ctx context.Context, name, command string, args []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.servers[name]; ok {
		return fmt.Errorf("server %s already running", name)
	}

	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server %s: %w", name, err)
	}

	server := &Server{
		Name:    name,
		Command: command,
		Args:    args,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdoutPipe),
		running: true,
	}

	m.servers[name] = server

	// Monitor exit in background
	go func() {
		cmd.Wait()
		server.mu.Lock()
		server.running = false
		server.mu.Unlock()
		log.Printf("[mcp] Server %s exited", name)
	}()

	// Discover tools via initialize + tools/list
	if err := m.initializeServer(server); err != nil {
		log.Printf("[mcp] Warning: initialize %s failed: %v", name, err)
	}

	log.Printf("[mcp] Server %s started with %d tools", name, len(server.tools))
	return nil
}

// Stop shuts down an MCP server.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("server %s not found", name)
	}

	server.stdin.Close()
	if server.cmd.Process != nil {
		server.cmd.Process.Kill()
	}
	delete(m.servers, name)
	return nil
}

// StartAll launches all configured MCP servers.
func (m *Manager) StartAll(ctx context.Context, configs []ServerConfig) {
	for _, cfg := range configs {
		if err := m.Start(ctx, cfg.Name, cfg.Command, cfg.Args); err != nil {
			log.Printf("[mcp] Failed to start %s: %v", cfg.Name, err)
		}
	}
}

// ServerConfig configures an MCP server to launch.
type ServerConfig struct {
	Name    string
	Command string
	Args    []string
}

// Reload stops all servers and starts with new config.
func (m *Manager) Reload(ctx context.Context, configs []ServerConfig) {
	// Stop all
	m.mu.RLock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Stop(name)
	}

	// Start all
	m.StartAll(ctx, configs)
}

// ListTools returns all tools from all MCP servers.
func (m *Manager) ListTools() []MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []MCPTool
	for _, srv := range m.servers {
		srv.mu.Lock()
		tools = append(tools, srv.tools...)
		srv.mu.Unlock()
	}
	return tools
}

// ListServers returns names and running status of all servers.
func (m *Manager) ListServers() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]bool)
	for name, srv := range m.servers {
		srv.mu.Lock()
		result[name] = srv.running
		srv.mu.Unlock()
	}
	return result
}

// CallTool forwards a tool call to the appropriate MCP server.
func (m *Manager) CallTool(ctx context.Context, toolName string, args map[string]any) (dtool.ToolResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, srv := range m.servers {
		srv.mu.Lock()
		for _, t := range srv.tools {
			if t.Name == toolName {
				srv.mu.Unlock()
				return m.callToolOnServer(srv, toolName, args)
			}
		}
		srv.mu.Unlock()
	}
	return dtool.ToolResult{}, fmt.Errorf("tool %s not found on any MCP server", toolName)
}

// --- JSON-RPC communication ---

func (m *Manager) initializeServer(srv *Server) error {
	// Send initialize
	_, err := m.sendRPC(srv, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "ngoagent",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}

	// Send initialized notification
	m.sendNotification(srv, "notifications/initialized", nil)

	// Discover tools
	result, err := m.sendRPC(srv, "tools/list", nil)
	if err != nil {
		return err
	}

	// Parse tool list
	var toolList struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolList); err != nil {
		return fmt.Errorf("parse tools/list: %w", err)
	}

	srv.mu.Lock()
	for i := range toolList.Tools {
		toolList.Tools[i].ServerName = srv.Name
	}
	srv.tools = toolList.Tools
	srv.mu.Unlock()

	return nil
}

func (m *Manager) callToolOnServer(srv *Server, toolName string, args map[string]any) (dtool.ToolResult, error) {
	result, err := m.sendRPC(srv, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return dtool.ToolResult{}, err
	}

	// Parse content array
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return dtool.ToolResult{Output: string(result)}, nil // Return raw if unparseable
	}

	var text string
	for _, c := range response.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return dtool.ToolResult{Output: text}, nil
}

func (m *Manager) sendRPC(srv *Server, method string, params any) (json.RawMessage, error) {
	srv.mu.Lock()
	srv.nextID++
	id := srv.nextID
	srv.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	srv.mu.Lock()
	_, err = srv.stdin.Write(data)
	srv.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write to MCP server: %w", err)
	}

	// Read response (with 10s timeout)
	respCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := srv.stdout.ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}
		respCh <- line
	}()

	select {
	case line := <-respCh:
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case err := <-errCh:
		return nil, fmt.Errorf("read from MCP server: %w", err)
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("MCP server %s timeout", srv.Name)
	}
}

func (m *Manager) sendNotification(srv *Server, method string, params any) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	srv.mu.Lock()
	srv.stdin.Write(data)
	srv.mu.Unlock()
}
