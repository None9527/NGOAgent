// Package security provides the security decision chain, audit logging,
// multi-channel approval, and specialized strategies for heartbeat/forge modes.
package security

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

// Decision is the result of a security check.
type Decision int

const (
	Allow Decision = iota
	Deny
	Ask // Require user confirmation
)

// DecisionLevel classifies the source of a decision.
type DecisionLevel string

const (
	LevelBlock  DecisionLevel = "block"  // Hard blocklist match
	LevelUser   DecisionLevel = "user"   // User override/approval
	LevelSystem DecisionLevel = "system" // System policy
	LevelPolicy DecisionLevel = "policy" // Mode-based policy
)

// AuditEntry records a security decision for auditing.
type AuditEntry struct {
	Timestamp time.Time
	ToolName  string
	Args      map[string]any
	Decision  Decision
	Level     DecisionLevel
	Reason    string
	Mode      string // chat / heartbeat / forge
}

// ApprovalFunc blocks until user approves/denies. Returns true=approved.
type ApprovalFunc func(ctx context.Context, toolName string, args map[string]any) bool

// PendingApproval represents a tool call awaiting user approval.
type PendingApproval struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	Reason   string         `json:"reason"`
	Result   chan bool       `json:"-"` // Receives approval decision
	Created  time.Time      `json:"created"`
}

// Hook implements the full AgentHook security interface (6 methods).
type Hook struct {
	cfg          *config.SecurityConfig
	heartbeatCfg *config.HeartbeatSecCfg
	audit        []AuditEntry
	overrides    map[string]bool            // Session-scoped user overrides
	approvalFns  map[string]ApprovalFunc    // channel → approval func (legacy)
	pending      map[string]*PendingApproval // approval_id → pending
	mu           sync.RWMutex
}

// NewHook creates a security hook.
func NewHook(cfg *config.SecurityConfig, heartbeatCfg *config.HeartbeatSecCfg) *Hook {
	return &Hook{
		cfg:          cfg,
		heartbeatCfg: heartbeatCfg,
		overrides:    make(map[string]bool),
		approvalFns:  make(map[string]ApprovalFunc),
		pending:      make(map[string]*PendingApproval),
	}
}

// ═══════════════════════════════════════════
// AgentHook Interface — 6 Methods
// ═══════════════════════════════════════════

// BeforeToolCall is called before every tool execution.
func (h *Hook) BeforeToolCall(ctx context.Context, toolName string, args map[string]any) (Decision, string) {
	mode := ctxutil.ModeFromContext(ctx)
	forgeID := ctxutil.ActiveForgeIDFromContext(ctx)

	var decision Decision
	var reason string
	var level DecisionLevel

	switch {
	case mode == "heartbeat":
		decision, reason = h.heartbeatDecide(toolName, args)
		level = LevelPolicy
	case forgeID != "":
		decision, reason = h.forgeDecide(toolName, args, forgeID)
		level = LevelPolicy
	default:
		decision, reason, level = h.normalDecide(ctx, toolName, args)
	}

	h.recordAudit(toolName, args, decision, level, reason, mode)
	return decision, reason
}

// AfterToolCall is called after tool execution for post-hoc auditing.
func (h *Hook) AfterToolCall(ctx context.Context, toolName string, result string, err error) {
	// Track file modifications for FileState
	if err == nil && (toolName == "write_file" || toolName == "edit_file") {
		// Future: update FileState tracker here
	}
}

// BeforeLLMCall is called before each LLM request for rate limiting / logging.
func (h *Hook) BeforeLLMCall(ctx context.Context, req *llm.Request, step int) {
	// Log step count for audit
	if step > 100 {
		// High step count warning — logged but not blocked (BehaviorGuard handles this)
	}
}

// AfterLLMCall is called after LLM response for content validation.
func (h *Hook) AfterLLMCall(ctx context.Context, resp *llm.Response, step int) {
	// Future: content policy checks (e.g., detect harmful output)
}

// BeforeRespond is the BehaviorGuard entry point.
// Can modify the response before it reaches the user.
func (h *Hook) BeforeRespond(ctx context.Context, resp *llm.Response) *llm.Response {
	// Strip any accidentally leaked internal state / system prompt fragments
	if resp != nil && strings.Contains(resp.Content, "<system>") {
		resp.Content = strings.ReplaceAll(resp.Content, "<system>", "[REDACTED]")
	}
	return resp
}

// RequestApproval creates a pending approval entry and returns it.
// The caller should send an SSE event and then block on pending.Result.
func (h *Hook) RequestApproval(toolName string, args map[string]any, reason string) *PendingApproval {
	id := uuid.New().String()[:8]
	pending := &PendingApproval{
		ID:       id,
		ToolName: toolName,
		Args:     args,
		Reason:   reason,
		Result:   make(chan bool, 1),
		Created:  time.Now(),
	}

	h.mu.Lock()
	h.pending[id] = pending
	h.mu.Unlock()

	return pending
}

// WaitForApproval blocks until the user approves or denies a tool call.
// It creates a pending approval, sends an event via the DeltaSink, and waits.
func (h *Hook) WaitForApproval(ctx context.Context, toolName string, args map[string]any, reason string) (string, bool) {
	pending := h.RequestApproval(toolName, args, reason)

	// Wait for external resolution (HTTP POST /v1/approve) or context cancellation
	select {
	case approved := <-pending.Result:
		h.mu.Lock()
		delete(h.pending, pending.ID)
		h.mu.Unlock()
		return pending.ID, approved
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, pending.ID)
		h.mu.Unlock()
		return pending.ID, false
	}
}

// Resolve resolves a pending approval by its ID.
func (h *Hook) Resolve(approvalID string, approved bool) error {
	h.mu.RLock()
	pending, ok := h.pending[approvalID]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("approval %s not found or already resolved", approvalID)
	}

	// Non-blocking send; the channel is buffered(1)
	select {
	case pending.Result <- approved:
	default:
	}
	return nil
}

// ListPending returns all pending approval requests.
func (h *Hook) ListPending() []*PendingApproval {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]*PendingApproval, 0, len(h.pending))
	for _, p := range h.pending {
		result = append(result, p)
	}
	return result
}

// RegisterApprovalFunc registers a channel-specific approval function (legacy).
func (h *Hook) RegisterApprovalFunc(channel string, fn ApprovalFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approvalFns[channel] = fn
}

// CleanupPending removes a pending approval entry.
func (h *Hook) CleanupPending(approvalID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pending, approvalID)
}

// CleanupExpired removes pending approvals older than maxAge.
// Call this when a session ends or periodically to prevent leaks.
func (h *Hook) CleanupExpired(maxAge time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, p := range h.pending {
		if p.Created.Before(cutoff) {
			delete(h.pending, id)
			removed++
		}
	}
	return removed
}

// ═══════════════════════════════════════════
// Admin Methods
// ═══════════════════════════════════════════

// AddOverride allows a user to override a denied tool for the current session.
func (h *Hook) AddOverride(toolName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overrides[toolName] = true
}

// GetAuditLog returns recent audit entries.
func (h *Hook) GetAuditLog(limit int) []AuditEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if limit <= 0 || limit > len(h.audit) {
		return h.audit
	}
	return h.audit[len(h.audit)-limit:]
}

// ReloadChain reloads the security config (for hot-reload callback).
func (h *Hook) ReloadChain(cfg *config.SecurityConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
}

// ═══════════════════════════════════════════
// Private decision logic
// ═══════════════════════════════════════════

var shellInjectionRe = regexp.MustCompile(`[;&|` + "`" + `$()]`)

// normalDecide handles chat mode security.
func (h *Hook) normalDecide(_ context.Context, toolName string, args map[string]any) (Decision, string, DecisionLevel) {
	// Step 0: Blocklist hard deny
	if toolName == "run_command" {
		cmd, _ := args["command"].(string)
		// Blocklist check
		for _, blocked := range h.cfg.BlockList {
			if strings.Contains(cmd, blocked) {
				return Deny, fmt.Sprintf("command contains blocked word: %s", blocked), LevelBlock
			}
		}
		// Shell injection detection
		if hasShellInjection(cmd) {
			return Ask, "command contains shell metacharacters", LevelSystem
		}
	}

	// Step 1: User overrides
	h.mu.RLock()
	override := h.overrides[toolName]
	h.mu.RUnlock()
	if override {
		return Allow, "user override", LevelUser
	}

	// Step 2: Mode-based
	switch h.cfg.Mode {
	case "allow":
		return Allow, "mode=allow", LevelPolicy
	case "ask":
		return Ask, "mode=ask", LevelPolicy
	}

	// Step 3: Auto mode — check if safe
	if toolName == "run_command" {
		cmd, _ := args["command"].(string)
		for _, safe := range h.cfg.SafeCommands {
			if strings.HasPrefix(strings.TrimSpace(cmd), safe) {
				return Allow, fmt.Sprintf("safe command: %s", safe), LevelSystem
			}
		}
		return Ask, "unrecognized command in auto mode", LevelPolicy
	}

	// Safe tools: always allowed (no destructive side-effects or pure state-tracking)
	safeTool := map[string]bool{
		// Read-only
		"read_file": true, "glob": true, "grep_search": true,
		"web_search": true, "web_fetch": true, "command_status": true,
		// State-tracking (no side effects outside agent)
		"task_boundary": true, "task_plan": true, "notify_user": true,
		"save_memory": true, "update_project_context": true,
		// Code generation (core agent capability)
		"write_file": true, "edit_file": true,
	}
	if safeTool[toolName] {
		return Allow, "safe tool", LevelSystem
	}

	// Step 4: Default
	return Ask, "requires user approval", LevelPolicy
}

// heartbeatDecide restricts tools to the heartbeat allowlist.
func (h *Hook) heartbeatDecide(toolName string, _ map[string]any) (Decision, string) {
	if h.heartbeatCfg == nil {
		return Deny, "heartbeat security not configured"
	}

	for _, blocked := range h.heartbeatCfg.BlockedTools {
		if toolName == blocked {
			return Deny, fmt.Sprintf("tool %s blocked in heartbeat mode", toolName)
		}
	}

	for _, allowed := range h.heartbeatCfg.AllowedTools {
		if toolName == allowed {
			return Allow, "heartbeat allowed"
		}
	}

	return Deny, fmt.Sprintf("tool %s not in heartbeat allowlist", toolName)
}

// forgeDecide allows operations within the forge sandbox.
func (h *Hook) forgeDecide(toolName string, args map[string]any, _ string) (Decision, string) {
	if toolName == "forge" {
		return Allow, "forge self-access"
	}

	if toolName == "write_file" || toolName == "edit_file" {
		path, _ := args["path"].(string)
		if path == "" {
			path, _ = args["file_path"].(string)
		}
		if !strings.HasPrefix(path, "/tmp/ngoagent-forge/") {
			return Deny, "forge: file operation outside sandbox"
		}
		return Allow, "forge: inside sandbox"
	}

	if toolName == "read_file" || toolName == "glob" || toolName == "grep_search" {
		return Allow, "forge: read-only"
	}

	// run_command: check if it's a dependency install → allow with audit
	if toolName == "run_command" {
		cmd, _ := args["command"].(string)
		if isDependencyInstall(cmd) {
			return Allow, "forge: dependency install (audited)"
		}
		return Allow, "forge: command execution (audited)"
	}

	return Ask, "forge: unexpected tool"
}

// ═══════════════════════════════════════════
// Detection helpers
// ═══════════════════════════════════════════

// hasShellInjection detects dangerous shell metacharacters in commands.
func hasShellInjection(cmd string) bool {
	// Skip simple safe patterns first
	parts := strings.Fields(cmd)
	if len(parts) <= 1 {
		return false
	}
	// Check for shell metacharacters that could indicate injection
	return shellInjectionRe.MatchString(cmd)
}

// isDependencyInstall checks if a command is a package manager install.
func isDependencyInstall(cmd string) bool {
	prefixes := []string{
		"pip install", "pip3 install",
		"npm install", "npm i ",
		"yarn add", "pnpm add",
		"go get", "go install",
		"apt install", "apt-get install",
		"brew install",
	}
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func (h *Hook) recordAudit(toolName string, args map[string]any, decision Decision, level DecisionLevel, reason, mode string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.audit = append(h.audit, AuditEntry{
		Timestamp: time.Now(),
		ToolName:  toolName,
		Args:      args,
		Decision:  decision,
		Level:     level,
		Reason:    reason,
		Mode:      mode,
	})
	// Cap audit log at 1000 entries
	if len(h.audit) > 1000 {
		h.audit = h.audit[len(h.audit)-500:]
	}
}
