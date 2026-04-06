// Package service — SecurityMiddleware extracts the security decision chain
// from doToolExec into a reusable middleware layer.
// Sprint 1-4: Centralizes Allow/Ask/Deny logic with ToolMeta.AccessLevel awareness.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// SecurityGate holds the result of a security check before tool execution.
type SecurityGate struct {
	Allowed bool   // true = proceed with execution
	Output  string // non-empty if denied (returned as tool output)
	Err     error  // ErrApprovalDenied if loop should stop
}

// checkSecurity performs the full security decision chain for a tool call.
// This is the single chokepoint for all security decisions — no tool bypasses this.
//
// Decision flow:
//  1. ToolMeta.Access == ReadOnly → skip check (safe by definition)
//  2. Hook.BeforeToolCall → Allow/Deny/Ask
//  3. Ask → block on approval channel with 5min timeout
//
// Returns SecurityGate with the decision.
func (a *AgentLoop) checkSecurity(ctx context.Context, toolName string, args map[string]any) SecurityGate {
	// Fast path: read-only tools never need security checks
	meta := dtool.DefaultMeta(toolName)
	if meta.Access == dtool.AccessReadOnly {
		return SecurityGate{Allowed: true}
	}

	// Standard security decision chain
	decision, reason := a.deps.Security.BeforeToolCall(ctx, toolName, args)
	switch decision {
	case SecurityAllow:
		return SecurityGate{Allowed: true}

	case SecurityDeny:
		notice := prompttext.Render(prompttext.EphSecurityNotice, map[string]any{
			"ToolName": toolName,
			"Reason":   reason,
		})
		a.InjectEphemeral(notice)
		return SecurityGate{
			Allowed: false,
			Output:  fmt.Sprintf("Tool '%s' DENIED by security: %s. Loop stopped.", toolName, reason),
			Err:     ErrApprovalDenied,
		}

	case SecurityAsk:
		// AutoApprove modes (agentic/agentic+evo): skip human approval gate
		if a.Mode().AutoApprove {
			return SecurityGate{Allowed: true}
		}
		return a.handleApprovalFlow(ctx, toolName, args, reason)

	default:
		return SecurityGate{Allowed: true}
	}
}

// handleApprovalFlow manages the interactive approval process for a tool call.
// Sends SSE events, blocks until client responds, and returns the result.
func (a *AgentLoop) handleApprovalFlow(ctx context.Context, toolName string, args map[string]any, reason string) SecurityGate {
	// 1. Register pending approval
	pending := a.deps.Security.RequestApproval(toolName, args, reason)

	// 2. Send SSE events so client can respond via POST /v1/approve
	a.deps.Delta.OnApprovalRequest(pending.ID, toolName, args, reason)

	// 3. Block until client responds, context cancelled, or TIMEOUT
	var approved bool
	select {
	case approved = <-pending.Result:
		// Client responded
	case <-time.After(5 * time.Minute):
		// Approval timeout → deny to prevent permanent hang
		approved = false
	case <-ctx.Done():
		approved = false
	}
	a.deps.Security.CleanupPending(pending.ID)

	if !approved {
		return SecurityGate{
			Allowed: false,
			Output:  fmt.Sprintf("Tool '%s' DENIED by approval (id=%s): %s. Loop stopped.", toolName, pending.ID, reason),
			Err:     ErrApprovalDenied,
		}
	}

	return SecurityGate{Allowed: true}
}
