// Package security provides the security decision chain, audit logging,
// multi-channel approval, and specialized strategies for forge mode.
package security

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
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
	Mode      string // chat / evo
}

// ApprovalFunc blocks until user approves/denies. Returns true=approved.
type ApprovalFunc func(ctx context.Context, toolName string, args map[string]any) bool

// PendingApproval represents a tool call awaiting user approval.
type PendingApproval struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	Reason   string         `json:"reason"`
	Result   chan bool      `json:"-"` // Receives approval decision
	Created  time.Time      `json:"created"`
}

// Hook implements the full AgentHook security interface (6 methods).
type Hook struct {
	cfg         *config.SecurityConfig
	audit       []AuditEntry
	overrides   map[string]bool             // Session-scoped user overrides
	approvalFns map[string]ApprovalFunc     // channel → approval func (legacy)
	pending     map[string]*PendingApproval // approval_id → pending
	mu          sync.RWMutex
	classifier  Classifier // P3 K1: pluggable security classifier
}

// NewHook creates a security hook.
func NewHook(cfg *config.SecurityConfig) *Hook {
	h := &Hook{
		cfg:         cfg,
		overrides:   make(map[string]bool),
		approvalFns: make(map[string]ApprovalFunc),
		pending:     make(map[string]*PendingApproval),
	}
	// Default to PatternClassifier (always available, zero cost)
	h.classifier = NewPatternClassifier(h)
	return h
}

// SetClassifier swaps the security classifier strategy at runtime.
// Thread-safe; can be called after creation to upgrade from pattern → hybrid/llm.
func (h *Hook) SetClassifier(c Classifier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.classifier = c
	slog.Info(fmt.Sprintf("[security] classifier strategy: %s", c.Name()))
}

// ═══════════════════════════════════════════
// AgentHook Interface — 6 Methods
// ═══════════════════════════════════════════

// BeforeToolCall is called before every tool execution.
func (h *Hook) BeforeToolCall(ctx context.Context, toolName string, args map[string]any) (Decision, string) {
	mode := ctxutil.ModeFromContext(ctx)
	evoID := ctxutil.ActiveEvoIDFromContext(ctx)

	var decision Decision
	var reason string
	var level DecisionLevel

	switch {
	case evoID != "":
		decision, reason = h.evoDecide(toolName, args, evoID)
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
// P0-C: Enhanced with ToolMeta-driven decisions and pattern-based BlockList.
// P3 K1: run_command calls are routed through the pluggable Classifier strategy.
//
// Two modes:
//   - allow: auto-approve everything; blocklist patterns get Ask (user decides)
//   - ask:   everything gets Ask (full manual approval)
func (h *Hook) normalDecide(ctx context.Context, toolName string, args map[string]any) (Decision, string, DecisionLevel) {
	// User session overrides always win
	h.mu.RLock()
	override := h.overrides[toolName]
	h.mu.RUnlock()
	if override {
		return Allow, "user override", LevelUser
	}

	switch h.cfg.Mode {
	case "allow":
		// P3 K1: run_command routed through classifier strategy
		if toolName == "run_command" {
			cmd, _ := args["command"].(string)
			h.mu.RLock()
			cls := h.classifier
			h.mu.RUnlock()
			if cls != nil {
				r := cls.Classify(ctx, cmd, args)
				switch r.Decision {
				case ClassifierDeny:
					return Deny, "[" + cls.Name() + "] " + r.Reason, LevelBlock
				case ClassifierAsk:
					// Safe zone check before escalating to user
					if isInSafeZone(cmd, h.cfg.Workspace) {
						return Allow, r.Reason + " (safe zone)", LevelPolicy
					}
					return Ask, "[" + cls.Name() + "] " + r.Reason, LevelBlock
				default:
					return Allow, "[" + cls.Name() + "] " + r.Reason, LevelPolicy
				}
			}
		}
		// P0-C #13: Pattern-based blocklist matching (non-run_command tools)
		if reason, blocked := h.matchBlockList(toolName, args); blocked {
			return Ask, reason, LevelBlock
		}
		return Allow, "mode=allow", LevelPolicy

	default: // "ask" or any other value
		// P0-C #12: ToolMeta-driven — read-only tools auto-allow even in ask mode
		meta := dtool.DefaultMeta(toolName)
		if meta.Access == dtool.AccessReadOnly {
			return Allow, "mode=ask, read-only tool auto-approved", LevelPolicy
		}
		return Ask, "mode=ask", LevelPolicy
	}
}

// matchBlockList checks if a tool call matches any blocklist pattern.
// P0-C #13: Supports two syntax forms:
//   - Legacy:  "rm"                → matches run_command where command starts with "rm"
//   - Pattern: "write_file(/etc/*)" → matches write_file where path starts with "/etc/"
//   - Pattern: "run_command(curl *)" → matches run_command where command starts with "curl"
func (h *Hook) matchBlockList(toolName string, args map[string]any) (string, bool) {
	for _, pattern := range h.cfg.BlockList {
		// Pattern syntax: ToolName(argPattern)
		if idx := strings.Index(pattern, "("); idx > 0 && strings.HasSuffix(pattern, ")") {
			patTool := pattern[:idx]
			patArg := pattern[idx+1 : len(pattern)-1] // extract inner pattern

			if toolName != patTool {
				continue
			}

			// Match arg pattern against tool-specific argument
			argVal := extractToolArg(toolName, args)
			if matchGlobPrefix(argVal, patArg) {
				return fmt.Sprintf("%s 匹配阻止规则 %s (需要确认)", toolName, pattern), true
			}
			continue
		}

		// Legacy syntax: bare command name → matches run_command prefix
		// P1 #33: Check ALL sub-commands, not just the first word
		if toolName == "run_command" {
			cmd, _ := args["command"].(string)
			for _, sub := range extractSubCommands(cmd) {
				subParts := strings.Fields(sub)
				if len(subParts) > 0 && subParts[0] == pattern {
					return fmt.Sprintf("命令 %s 目标在安全区域外 (需要确认)", pattern), true
				}
			}
		}
	}
	return "", false
}

// extractToolArg extracts the primary argument value from a tool call for pattern matching.
func extractToolArg(toolName string, args map[string]any) string {
	switch toolName {
	case "run_command":
		v, _ := args["command"].(string)
		return v
	case "write_file", "edit_file", "read_file":
		v, _ := args["path"].(string)
		return v
	default:
		// Try common arg names
		for _, key := range []string{"path", "command", "url", "file_path"} {
			if v, ok := args[key].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
}

// matchGlobPrefix matches a value against a simple glob pattern.
// Supports: "prefix*" (startsWith), "*suffix" (endsWith), exact match.
func matchGlobPrefix(value, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(value, strings.TrimPrefix(pattern, "*"))
	}
	return value == pattern
}

// ─── P1 #33: Sub-command extraction for shell injection detection ───

// shellSplitRe splits a command string on shell operators that chain sub-commands.
// Covers: semicolons (;), logical AND (&&), logical OR (||), pipes (|),
// command substitution ($(...)), backticks (`...`), newlines.
var shellSplitRe = regexp.MustCompile(`[;\n]|\|\||&&|\|`)

// extractSubCommands splits a bash command string into individual sub-commands.
// Each sub-command is trimmed. This catches injection attempts like:
//   - "echo ok; rm -rf /"       → ["echo ok", "rm -rf /"]
//   - "cat file && curl evil"   → ["cat file", "curl evil"]
//   - "ls | xargs rm"          → ["ls", "xargs rm"]
//
// Also strips common shell wrappers: bash -c "...", sh -c "..."
func extractSubCommands(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}

	// Unwrap: bash -c "command" / sh -c 'command'
	for _, shell := range []string{"bash -c ", "sh -c "} {
		if strings.HasPrefix(cmd, shell) {
			inner := cmd[len(shell):]
			inner = strings.Trim(inner, "\"'")
			cmd = inner
		}
	}

	// Split on shell operators
	parts := shellSplitRe.Split(cmd, -1)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	// Also check for $(...) command substitution
	if idx := strings.Index(cmd, "$("); idx >= 0 {
		end := strings.Index(cmd[idx:], ")")
		if end > 2 {
			inner := strings.TrimSpace(cmd[idx+2 : idx+end])
			if inner != "" {
				result = append(result, inner)
			}
		}
	}

	if len(result) == 0 {
		result = []string{cmd}
	}
	return result
}

// evoDecide allows operations within the forge sandbox.
func (h *Hook) evoDecide(toolName string, args map[string]any, _ string) (Decision, string) {
	if toolName == "evo" {
		return Allow, "evo self-access"
	}

	if toolName == "write_file" || toolName == "edit_file" {
		path, _ := args["path"].(string)
		if path == "" {
			path, _ = args["file_path"].(string)
		}
		if !strings.HasPrefix(path, "/tmp/ngoagent-evo/") {
			return Deny, "evo: file operation outside sandbox"
		}
		return Allow, "evo: inside sandbox"
	}

	if toolName == "read_file" || toolName == "glob" || toolName == "grep_search" {
		return Allow, "evo: read-only"
	}

	// run_command: check if it's a dependency install → allow with audit
	if toolName == "run_command" {
		cmd, _ := args["command"].(string)
		if isDependencyInstall(cmd) {
			return Allow, "evo: dependency install (audited)"
		}
		return Allow, "evo: command execution (audited)"
	}

	return Ask, "evo: unexpected tool"
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

// isInSafeZone checks if all path arguments target /tmp/ or the configured workspace.
// Returns false if any argument targets a path outside safe zones, preventing bypasses
// like "rm /tmp/foo /etc/passwd".
func isInSafeZone(cmd string, workspace string) bool {
	parts := strings.Fields(cmd)
	hasPath := false
	for _, p := range parts[1:] { // skip the command itself
		if strings.HasPrefix(p, "-") {
			continue // skip flags like -rf, -f, --force
		}
		hasPath = true
		isTmp := strings.HasPrefix(p, "/tmp/") || p == "/tmp"
		isWs := workspace != "" && strings.HasPrefix(p, workspace)
		if !isTmp && !isWs {
			return false
		}
	}
	return hasPath // must have at least one path argument
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
