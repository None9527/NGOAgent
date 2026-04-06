package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
)

// ═══════════════════════════════════════════
// Tool Execution — extracted from run.go for maintainability
// ═══════════════════════════════════════════

// doToolExec executes a single tool call with security check.
func (a *AgentLoop) doToolExec(ctx context.Context, tc llm.ToolCall) (string, error) {
	// Track how many tool calls since last task_boundary update
	a.mu.Lock()
	a.task.RecordToolCall()
	a.mu.Unlock()

	// P3 I1: Update execution phase based on tool call pattern (PhaseDetect modes)
	if a.Mode().PhaseDetect {
		a.phaseDetector.RecordTool(tc.Function.Name, tc.Function.Arguments)
	}

	// Step-level guard: pre-check (planning behavior enforcement)
	if v := a.guard.PreToolCheck(tc.Function.Name); v != nil && v.Action == "warn" {
		a.InjectEphemeral(v.Message)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse tool args: %w", err)
	}

	// Security middleware: centralized decision chain (security_middleware.go)
	// Fast path: read-only tools skip security checks entirely.
	gate := a.checkSecurity(ctx, tc.Function.Name, args)
	if !gate.Allowed {
		if gate.Err == nil {
			// Soft deny: send tool start for UI visibility
			a.deps.Delta.OnToolStart(tc.ID, tc.Function.Name+" [blocked]", args)
		}
		return gate.Output, gate.Err
	}

	// Hook: BeforeTool (Modifying — can alter args or skip)
	if a.deps.Hooks != nil {
		var skip bool
		args, skip = a.deps.Hooks.FireBeforeTool(ctx, tc.Function.Name, args)
		if skip {
			return fmt.Sprintf("Tool '%s' skipped by hook", tc.Function.Name), nil
		}
	}
	// Evo trace: record tool invocation (per-loop, session-isolated)
	traceIdx := -1
	if a.traceCollector != nil {
		traceIdx, _, _ = a.traceCollector.BeforeTool(ctx, tc.Function.Name, args)
	}

	a.deps.Delta.OnToolStart(tc.ID, tc.Function.Name, args)
	// Inject fully-configured brain store into tool context (single key, carries sessionID + workspaceDir)
	toolCtx := ctx
	if a.deps.Brain != nil {
		toolCtx = brain.ContextWithBrainStore(ctx, a.deps.Brain)
	}
	result, err := a.safeToolExec(toolCtx, tc.Function.Name, args)

	// P3 I4: Unified tiered tool result injection (replaces P1 ad-hoc truncation)
	// Handles: <2K inline, 2K-8K with header, >8K spill to /tmp + path reference.
	// Diff-tools and grep_search get additional structural compression.
	output := a.processToolResult(tc.Function.Name, result.Output)

	a.deps.Delta.OnToolResult(tc.ID, tc.Function.Name, output, err)
	a.deps.Security.AfterToolCall(ctx, tc.Function.Name, output, err)

	// Hook: AfterTool (Void — logging, audit, stats)
	if a.deps.Hooks != nil {
		a.deps.Hooks.FireAfterTool(ctx, tc.Function.Name, output, err)
	}
	// Evo trace: record tool output with concurrent-safe index matching
	if a.traceCollector != nil {
		a.traceCollector.AfterTool(ctx, traceIdx, tc.Function.Name, output, err)
	}
	// P3 M1: Webhook notification for side-effectful tools
	if a.deps.WebhookHook != nil {
		a.deps.WebhookHook.OnToolResult(a.SessionID(), tc.Function.Name, output, err)
	}

	// --- Protocol Dispatch (centralized in protocol.go) ---
	ps := a.protoState()
	dtool.Dispatch(result, a.deps.Delta, ps)
	a.syncLoopState(ps)

	// Step-level guard: post-record
	a.guard.PostToolRecord(tc.Function.Name)

	// Track plan.md modifications for EphPlanModifiedReminder
	// + Artifact staleness tracking (record last step for each artifact)
	if tc.Function.Name == "task_plan" {
		var planArgs struct {
			Action string `json:"action"`
			Type   string `json:"type"`
		}
		if json.Unmarshal([]byte(tc.Function.Arguments), &planArgs) == nil {
			if planArgs.Action == "create" || planArgs.Action == "update" {
				// Track plan.md modification
				if planArgs.Type == "plan" || planArgs.Type == "" {
					a.mu.Lock()
					a.task.PlanModified = true
					a.mu.Unlock()
				}
				// Record artifact last step for staleness tracking
				artifactName := planArgs.Type + ".md"
				if planArgs.Type == "" {
					artifactName = "plan.md"
				}
				a.mu.Lock()
				a.task.RecordArtifactTouch(artifactName)
				a.mu.Unlock()
			}
		}
	}

	return output, err
}

// safeToolExec wraps tool execution with panic recovery.
// Prevents a single malformed tool call from crashing the entire agent loop.
func (a *AgentLoop) safeToolExec(ctx context.Context, name string, args map[string]any) (result dtool.ToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			err = fmt.Errorf("tool '%s' panicked: %v\n%s", name, r, string(buf[:n]))
			result = dtool.ToolResult{Output: fmt.Sprintf("Internal error: tool panicked: %v", r)}
		}
	}()
	return a.deps.ToolExec.Execute(ctx, name, args)
}

// buildToolDescs converts tool definitions to prompt descriptions.
func (a *AgentLoop) buildToolDescs() []prompt.ToolDesc {
	defs := a.deps.ToolExec.ListDefinitions()
	descs := make([]prompt.ToolDesc, len(defs))
	for i, d := range defs {
		descs[i] = prompt.ToolDesc{
			Name:        d.Function.Name,
			Description: d.Function.Description,
		}
	}
	return descs
}

func toolResultBudget(toolName string) int {
	meta := dtool.DefaultMeta(toolName)
	if meta.MaxOutputSize > 0 {
		return meta.MaxOutputSize
	}
	return 50 * 1024 // fallback default
}

// ─── P0-D #1: Concurrent tool execution helpers ─────────────────────────

// splitToolCalls partitions tool calls into side-effect-free (ReadOnly/Network)
// and write (everything else) groups. Preserves original ordering within each group.
// P2: Enables mixed batch splitting — concurrent reads + serial writes.
func splitToolCalls(calls []llm.ToolCall) (readOnly, write []llm.ToolCall) {
	for _, tc := range calls {
		meta := dtool.DefaultMeta(tc.Function.Name)
		if meta.Access == dtool.AccessReadOnly || meta.Access == dtool.AccessNetwork {
			readOnly = append(readOnly, tc)
		} else {
			write = append(write, tc)
		}
	}
	return
}

// execToolsConcurrent executes all tool calls in parallel using goroutines.
// Only called when allSideEffectFree returns true — no write conflicts possible.
func (a *AgentLoop) execToolsConcurrent(ctx context.Context, calls []llm.ToolCall) {
	type toolOutput struct {
		idx    int
		result string
		err    error
	}

	results := make([]toolOutput, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))

	for i, tc := range calls {
		go func(idx int, tc llm.ToolCall) {
			defer wg.Done()
			result, err := a.doToolExec(ctx, tc)
			results[idx] = toolOutput{idx: idx, result: result, err: err}
		}(i, tc)
	}
	wg.Wait()

	// P0-4: Check for approval denial before appending results.
	// In concurrent mode, if any tool was denied, transition to Done.
	for _, r := range results {
		if r.err != nil && errors.Is(r.err, ErrApprovalDenied) {
			// Append denial message for all calls, then stop
			for j, tc := range calls {
				content := "Cancelled due to tool denial."
				if results[j].result != "" {
					content = results[j].result
				}
				a.AppendMessage(llm.Message{
					Role:       "tool",
					Content:    content,
					ToolCallID: tc.ID,
				})
			}
			return
		}
	}

	// Append results in original order (LLM API requires ordered tool results)
	for i, r := range results {
		content := r.result
		if r.err != nil {
			content = fmt.Sprintf("Error: %v", r.err)
		}
		a.AppendMessage(llm.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: calls[i].ID,
		})
	}
}

// execToolsSerial executes tool calls one by one. Returns true if a denial stopped the loop.
func (a *AgentLoop) execToolsSerial(ctx context.Context, calls []llm.ToolCall) bool {
	for i, tc := range calls {
		result, err := a.doToolExec(ctx, tc)

		if errors.Is(err, ErrApprovalDenied) {
			a.AppendMessage(llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
			for j := i + 1; j < len(calls); j++ {
				a.AppendMessage(llm.Message{
					Role:       "tool",
					Content:    "Cancelled due to previous tool denial.",
					ToolCallID: calls[j].ID,
				})
			}
			a.deps.Delta.OnText("\n" + result + "\n")
			a.transition(StateDone)
			return true
		}

		content := result
		if err != nil {
			content = fmt.Sprintf("Error: %v", err)
		}
		a.AppendMessage(llm.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: tc.ID,
		})
	}
	return false
}
