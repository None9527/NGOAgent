package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

// ErrApprovalDenied signals that a tool call was denied by the security hook.
// When this error is returned, the loop must stop immediately.
var ErrApprovalDenied = errors.New("approval denied by security policy")

// Run executes the ReAct loop for one user turn.
func (a *AgentLoop) Run(ctx context.Context, userMessage string) error {
	// Backpressure: prevent concurrent runs on same loop (Anti's BUSY state)
	if !a.runMu.TryLock() {
		return fmt.Errorf("agent is busy: another run is in progress")
	}
	defer a.runMu.Unlock()
	return a.runInner(ctx, userMessage)
}

// TryAcquire attempts to acquire the run lock without blocking.
// Returns true if acquired (caller MUST call ReleaseAcquire when done).
func (a *AgentLoop) TryAcquire() bool {
	return a.runMu.TryLock()
}

// ReleaseAcquire releases the run lock acquired by TryAcquire.
func (a *AgentLoop) ReleaseAcquire() {
	a.runMu.Unlock()
}

// RunWithoutAcquire runs the loop assuming the caller already holds the run lock.
func (a *AgentLoop) RunWithoutAcquire(ctx context.Context, userMessage string) error {
	return a.runInner(ctx, userMessage)
}

func (a *AgentLoop) runInner(ctx context.Context, userMessage string) error {

	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: "user", Content: userMessage})
	a.state = StatePrepare
	a.mu.Unlock()
	a.guard.ResetTurn()

	opts := a.options
	steps := 0
	retries := 0
	maxRetries := 2

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.stopCh:
			return fmt.Errorf("agent stopped")
		default:
		}

		switch a.CurrentState() {
		case StatePrepare:
			a.doPrepare(ctx)
			a.transition(StateGenerate)

		case StateGenerate:
			resp, err := a.doGenerate(ctx, opts)
			if err != nil {
				a.transition(StateError)
				if llmErr, ok := err.(*llm.LLMError); ok {
					if llmErr.Level == llm.ErrorTransient && retries < maxRetries {
						retries++
						backoff := time.Duration(1<<retries) * time.Second // 2s, 4s
						log.Printf("[retry] attempt %d/%d, backoff %v: %s", retries, maxRetries, backoff, llmErr.Code)
						time.Sleep(backoff)
						a.transition(StateGenerate)
						continue
					}
					if llmErr.Level == llm.ErrorFatal {
						a.transition(StateFatal)
						a.deps.Delta.OnError(err)
						return err
					}
				}
				a.deps.Delta.OnError(err)
				return err
			}
			retries = 0

			a.AppendMessage(llm.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
				Reasoning: resp.Reasoning,
			})

			// BehaviorGuard check
			verdict := a.guard.Check(resp.Content, len(resp.ToolCalls), steps)
			switch verdict.Action {
			case "terminate":
				a.transition(StateDone)
				a.deps.Delta.OnText("\n\n[" + verdict.Message + "]")
				a.deps.Delta.OnComplete()
				continue
			case "warn":
				a.InjectEphemeral(verdict.Message)
			}

			if len(resp.ToolCalls) == 0 {
				a.transition(StateDone)
				a.deps.Delta.OnComplete()
				continue
			}
			a.transition(StateToolExec)

		case StateToolExec:
			a.mu.Lock()
			lastMsg := a.history[len(a.history)-1]
			a.mu.Unlock()

			for i, tc := range lastMsg.ToolCalls {
				result, err := a.doToolExec(ctx, tc)

				// Denial sentinel: stop loop immediately
				if errors.Is(err, ErrApprovalDenied) {
					a.AppendMessage(llm.Message{
						Role:       "tool",
						Content:    result,
						ToolCallID: tc.ID,
					})
					
					// Fill remaining unfinished tool calls to satisfy strict API schema rules
					for j := i + 1; j < len(lastMsg.ToolCalls); j++ {
						a.AppendMessage(llm.Message{
							Role:       "tool",
							Content:    "Cancelled due to previous tool denial.",
							ToolCallID: lastMsg.ToolCalls[j].ID,
						})
					}

					a.deps.Delta.OnText("\n⛔ " + result + "\n")
					a.transition(StateDone)
					a.deps.Delta.OnComplete()
					goto loopEnd
				}

				if err != nil {
					a.AppendMessage(llm.Message{
						Role:       "tool",
						Content:    fmt.Sprintf("Error: %v", err),
						ToolCallID: tc.ID,
					})
					continue
				}
				a.AppendMessage(llm.Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
				})
			}

			// Check if any tool returned a terminal signal (declarative config).
			// Mirrors Anti's TERMINAL_STEP_TYPE mechanism.
			a.mu.Lock()
			shouldStop := a.yieldRequested
			a.yieldRequested = false
			a.mu.Unlock()
			if shouldStop {
				a.transition(StateDone)
				a.deps.Delta.OnComplete()
				continue
			}

			a.transition(StateGuardCheck)

		case StateGuardCheck:
			steps++

			// Hard max_steps enforcement (safety net above BehaviorGuard)
			maxSteps := a.deps.Config.Agent.MaxSteps
			if maxSteps > 0 && steps >= maxSteps {
				a.transition(StateDone)
				a.deps.Delta.OnText("\n\n[Max steps reached: safety limit]")
				a.deps.Delta.OnComplete()
				continue
			}

			tokenEstimate := a.estimateTokens()
			policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
			usage := float64(tokenEstimate) / float64(policy.ContextWindow)

			// Three-level context defense
			if usage > 0.95 {
				// Level 3: Force truncate to last 8 messages
				a.forceTruncate(8)
				a.InjectEphemeral(prompttext.EphCompactionNotice)
				a.transition(StateGenerate)
			} else if usage > 0.70 {
				// Level 1-2: Compact
				a.transition(StateCompact)
			} else {
				a.transition(StateGenerate)
			}

		case StateCompact:
			a.doCompact()
			a.InjectEphemeral(prompttext.EphCompactionNotice)
			a.persistFullHistory() // full replace after restructuring
			a.transition(StateGenerate)

		case StateDone:
			a.state = StateIdle
			a.persistHistory()
			a.fireHooks(ctx, steps)
			return nil

		default:
			return fmt.Errorf("unexpected state: %s", a.CurrentState())
		}
	}
loopEnd:
	a.persistHistory()
	a.fireHooks(ctx, steps)
	return nil
}

func (a *AgentLoop) transition(to State) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = to
}

// doPrepare detects ephemeral injection needs.
// Implements Anti's 3-layer ephemeral injection system.
func (a *AgentLoop) doPrepare(ctx context.Context) {
	a.mu.Lock()
	lastMsg := ""
	if len(a.history) > 0 {
		lastMsg = a.history[len(a.history)-1].Content
	}
	boundaryName := a.boundaryTaskName
	boundaryMode := a.boundaryMode
	boundaryStatus := a.boundaryStatus
	boundarySummary := a.boundarySummary
	planMod := a.planModified
	a.mu.Unlock()

	isPlanning := a.shouldInjectPlanning(lastMsg)

	// Sync planning state to Guard for step-level enforcement
	planExists := false
	if a.deps.Brain != nil {
		if _, err := a.deps.Brain.Read("plan.md"); err == nil {
			planExists = true
		}
	}
	a.guard.SetPlanningState(isPlanning, planExists)

	// === Layer 1: Planning mode base template ===
	if isPlanning {
		a.InjectEphemeral(prompttext.EphPlanningMode)
	}

	a.mu.Lock()
	steps := a.stepsSinceUpdate
	a.mu.Unlock()

	// === Layer 2: Active task boundary reminder (frequency gated: every 3 steps) ===
	if boundaryName != "" {
		if steps == 0 || steps%3 == 0 {
			msg := prompttext.Render(prompttext.EphActiveTaskReminder, map[string]any{
				"TaskName": boundaryName,
				"Status":   boundaryStatus,
				"Summary":  boundarySummary,
				"Mode":     boundaryMode,
			})
			a.InjectEphemeral(msg)
		}
	}

	// === Layer 2b: Boundary frequency nudge (Anti's num_steps pattern) ===
	// When too many tools called without task_boundary, inject a stronger reminder.
	// This is a soft nudge, NOT a forced interruption — LLM can finish current work first.
	if ssb := a.guard.StepsSinceBoundary(); ssb >= 10 {
		a.InjectEphemeral(fmt.Sprintf(
			"<ephemeral_message>You have made %d tool calls without updating task progress. "+
				"Call task_boundary to report your current status when you reach a natural pause point.</ephemeral_message>", ssb))
	}

	// === Layer 3a: Artifact reminder (frequency gated: every 5 steps) ===
	if a.deps.Brain != nil && (steps == 0 || steps%5 == 0) {
		if files, err := a.deps.Brain.List(); err == nil {
			var mdFiles []string
			for _, f := range files {
				if strings.HasSuffix(f, ".md") && !strings.Contains(f, ".resolved") && !strings.Contains(f, ".metadata") {
					mdFiles = append(mdFiles, f)
				}
			}
			if len(mdFiles) > 0 {
				a.InjectEphemeral(prompttext.Render(
					prompttext.EphArtifactReminder,
					map[string]any{"ArtifactList": strings.Join(mdFiles, "\n")},
				))
			}
		}
	}

	// === Layer 3b: Planning mode + no plan.md → force reminder ===
	if isPlanning && a.deps.Brain != nil {
		if _, err := a.deps.Brain.Read("plan.md"); err != nil {
			a.InjectEphemeral(prompttext.EphPlanningNoPlanReminder)
		}
	}

	// === Layer 3c: Plan modified but not reviewed by user ===
	if planMod && boundaryMode == "planning" {
		a.InjectEphemeral(prompttext.EphPlanModifiedReminder)
	}

	// === Layer 3d: Mode transitions (entering/exiting planning) ===
	a.mu.Lock()
	prevMode := a.previousMode
	a.mu.Unlock()
	if boundaryMode != "" && prevMode != "" && boundaryMode != prevMode {
		if boundaryMode == "planning" {
			a.InjectEphemeral(prompttext.EphEnteringPlanningMode)
		} else if prevMode == "planning" {
			a.InjectEphemeral(prompttext.EphExitingPlanningMode)
		}
	}

	// Context usage warning
	tokenEst := a.estimateTokens()
	policy := llm.GetPolicy(a.deps.LLMRouter.CurrentModel())
	pct := int(float64(tokenEst) / float64(policy.ContextWindow) * 100)
	if pct > 60 {
		msg := prompttext.Render(prompttext.EphContextStatus, map[string]any{
			"Percent": pct,
			"Used":    tokenEst,
			"Total":   policy.ContextWindow,
		})
		a.InjectEphemeral(msg)
	}

	// Heartbeat mode
	if ctxutil.ModeFromContext(ctx) == "heartbeat" {
		tasks := ""
		if a.deps.Workspace != nil {
			globalHbPath := ""
			if a.deps.ConfigMgr != nil {
				globalHbPath = config.ResolvePath("heartbeat.md")
			}
			tasks = a.deps.Workspace.ReadHeartbeat(globalHbPath)
		}
		if tasks != "" {
			msg := prompttext.Render(prompttext.EphHeartbeatContext, map[string]any{"Tasks": tasks})
			a.InjectEphemeral(msg)
		}
	}
}

// shouldInjectPlanning checks if planning mode should be triggered.
func (a *AgentLoop) shouldInjectPlanning(userMessage string) bool {
	// Agent self-declared planning mode via task_boundary — strongest signal
	a.mu.Lock()
	mode := a.boundaryMode
	a.mu.Unlock()
	if mode == "planning" {
		return true
	}
	if strings.Contains(userMessage, "/plan") {
		return true
	}
	if a.deps.Config.Agent.PlanningMode {
		return true
	}
	// Heuristic: message > 200 chars likely complex
	if len(userMessage) > 200 {
		words := strings.Fields(userMessage)
		if len(words) > 30 {
			return true
		}
	}
	return false
}

// doGenerate calls the LLM with the fully assembled system prompt.
func (a *AgentLoop) doGenerate(ctx context.Context, opts RunOptions) (*llm.Response, error) {
	model := opts.Model
	if model == "" {
		model = a.deps.LLMRouter.CurrentModel()
	}

	provider, err := a.deps.LLMRouter.Resolve(model)
	if err != nil {
		return nil, err
	}

	// ═══ Assemble system prompt with ALL data sources ═══
	promptDeps := a.buildPromptDeps(ctx, model, opts)
	systemPrompt, _ := a.deps.PromptEngine.Assemble(promptDeps)

	// Build messages
	messages := make([]llm.Message, 0, len(a.history)+1)
	messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})

	a.mu.Lock()
	messages = append(messages, a.history...)
	// Drain ephemerals
	ephemerals := a.ephemerals
	a.ephemerals = nil
	a.mu.Unlock()

	// Inject ephemerals as a system-level hint (not user, to avoid consecutive user merging)
	if len(ephemerals) > 0 {
		var ephContent strings.Builder
		for _, eph := range ephemerals {
			ephContent.WriteString("<EPHEMERAL_MESSAGE>\n")
			ephContent.WriteString(eph)
			ephContent.WriteString("\n</EPHEMERAL_MESSAGE>\n\n")
		}
		messages = append(messages, llm.Message{Role: "system", Content: ephContent.String()})
	}

	// Sanitize: enforce turn ordering first, THEN fix orphan tool_calls/results
	// (ordering can merge messages and create new orphans, so sanitize must come last)
	messages = enforceTurnOrdering(messages)
	messages = sanitizeMessages(messages)

	// Check if Guard wants to force a specific tool (Anti's force_tool_name mechanism)
	forceTool := a.guard.ConsumeForceToolName()

	req := &llm.Request{
		Model:       model,
		Messages:    messages,
		Tools:       a.deps.ToolExec.ListDefinitions(),
		Temperature: DefaultTemperature,
		TopP:        DefaultTopP,
		MaxTokens:   DefaultMaxTokens,
		Stream:      true,
		ToolChoice:  forceTool,
	}

	ch := make(chan llm.StreamChunk, 32)
	var resp *llm.Response
	var genErr error

	// Deadline for LLM streaming: prevents infinite hang if SSE stream never ends.
	// HTTP client timeout (5min) only covers initial response, not streaming.
	genCtx, genCancel := context.WithTimeout(ctx, 8*time.Minute)
	defer genCancel()

	go func() {
		resp, genErr = provider.GenerateStream(genCtx, req, ch)
	}()

	// Buffer text chunks: only flush to client based on StopReason.
	// DashScope/OpenAI API: content is null when tool_calls are present,
	// but we buffer defensively for any provider that mixes them.
	var textBuf strings.Builder

	for chunk := range ch {
		switch chunk.Type {
		case llm.ChunkText:
			textBuf.WriteString(chunk.Text)
		case llm.ChunkReasoning:
			a.deps.Delta.OnReasoning(chunk.Text)
		case llm.ChunkToolCall:
			// Tool call chunks are consumed by StreamAdapter to build resp.ToolCalls.
			// UI notification (OnToolStart) is deferred to doToolExec to avoid duplicates.
		case llm.ChunkError:
			if chunk.Error != nil {
				return nil, chunk.Error
			}
		}
	}

	// Flush buffered text only when StopReason indicates final answer.
	// "stop" / "" = pure text response → flush
	// "tool_calls"  = tool invocation  → suppress (intermediate reasoning)
	if resp != nil {
		log.Printf("[doGenerate] StopReason=%q textLen=%d toolCalls=%d", resp.StopReason, textBuf.Len(), len(resp.ToolCalls))
	}
	if textBuf.Len() > 0 && resp != nil && resp.StopReason != "tool_calls" {
		a.deps.Delta.OnText(textBuf.String())
	}

	if genErr != nil {
		return nil, genErr
	}
	return resp, nil
}

// buildPromptDeps populates ALL 11 fields from injected stores.
func (a *AgentLoop) buildPromptDeps(ctx context.Context, model string, opts RunOptions) prompt.Deps {
	deps := prompt.Deps{
		Mode:        opts.Mode,
		ToolDescs:   a.buildToolDescs(),
		TokenBudget: llm.GetPolicy(model).ContextWindow,
		Runtime:     a.buildRuntimeInfo(model),
	}

	// UserRules — from config discovery or workspace
	if a.deps.Workspace != nil {
		deps.ProjectContext = a.deps.Workspace.ReadContext()
	}

	// UserRules — global (~/.ngoagent/user_rules.md)
	if a.deps.PromptEngine != nil {
		rules, _ := a.deps.PromptEngine.DiscoverUserRules(
			ctxutil.WorkspaceDirFromContext(ctx),
		)
		deps.UserRules = rules
	}

	// Knowledge — KI summaries
	if a.deps.KIStore != nil {
		deps.ConvSummary = a.deps.KIStore.GenerateSummaries()
	}

	// Skills — summaries for prompt injection
	if a.deps.SkillMgr != nil {
		skills := a.deps.SkillMgr.List()
		for _, s := range skills {
			deps.SkillInfos = append(deps.SkillInfos, prompt.SkillInfo{
				Name:        s.Name,
				Description: s.Description,
				Type:        s.Type,
				Path:        s.Path,
			})
		}
	}

	// Focus — Brain plan/task
	if a.deps.Brain != nil {
		if plan, err := a.deps.Brain.Read("plan.md"); err == nil && plan != "" {
			deps.FocusFile = plan
		} else if task, err := a.deps.Brain.Read("task.md"); err == nil && task != "" {
			deps.FocusFile = task
		}
	}

	return deps
}

// buildRuntimeInfo generates runtime context (OS, time, model, workspace).
func (a *AgentLoop) buildRuntimeInfo(model string) string {
	var b strings.Builder
	cwd, _ := os.Getwd()
	b.WriteString("# Environment\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Time: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	b.WriteString(fmt.Sprintf("- Model: %s\n", model))
	b.WriteString(fmt.Sprintf("- Workspace: %s\n", cwd))
	return b.String()
}

// doToolExec executes a single tool call with security check.
func (a *AgentLoop) doToolExec(ctx context.Context, tc llm.ToolCall) (string, error) {
	// Track how many tool calls since last task_boundary update
	a.mu.Lock()
	a.stepsSinceUpdate++
	a.mu.Unlock()

	// Step-level guard: pre-check (planning behavior enforcement)
	if v := a.guard.PreToolCheck(tc.Function.Name); v != nil && v.Action == "warn" {
		a.InjectEphemeral(v.Message)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse tool args: %w", err)
	}

	// Security check (4-level decision chain)
	decision, reason := a.deps.Security.BeforeToolCall(ctx, tc.Function.Name, args)
	switch decision {
	case security.Deny:
		notice := prompttext.Render(prompttext.EphSecurityNotice, map[string]any{
			"ToolName": tc.Function.Name,
			"Reason":   reason,
		})
		a.InjectEphemeral(notice)
		// Return sentinel: loop will detect ErrApprovalDenied and stop
		return fmt.Sprintf("Tool '%s' DENIED by security: %s. Loop stopped.", tc.Function.Name, reason), ErrApprovalDenied
	case security.Ask:
		// 1. Register pending approval
		pending := a.deps.Security.RequestApproval(tc.Function.Name, args, reason)
		// 2. Send SSE events so client can respond via POST /v1/approve
		a.deps.Delta.OnToolStart(tc.Function.Name+" [待审批: "+reason+"]", args)
		a.deps.Delta.OnApprovalRequest(pending.ID, tc.Function.Name, args, reason)
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
			// Denied by client or timeout → sentinel stops loop
			return fmt.Sprintf("Tool '%s' DENIED by approval (id=%s): %s. Loop stopped.", tc.Function.Name, pending.ID, reason), ErrApprovalDenied
		}
	}

	a.deps.Delta.OnToolStart(tc.Function.Name, args)
	// Inject fully-configured brain store into tool context (single key, carries sessionID + workspaceDir)
	toolCtx := ctx
	if a.deps.Brain != nil {
		toolCtx = brain.ContextWithBrainStore(ctx, a.deps.Brain)
	}
	result, err := a.safeToolExec(toolCtx, tc.Function.Name, args)

	// Truncate large outputs
	output := result.Output
	if len(output) > 50*1024 {
		output = output[:25*1024] + "\n... (output truncated) ...\n" + output[len(output)-25*1024:]
	}

	a.deps.Delta.OnToolResult(tc.Function.Name, output, err)
	a.deps.Security.AfterToolCall(ctx, tc.Function.Name, output, err)

	// --- Protocol Dispatch (centralized in protocol.go) ---
	ps := a.protoState()
	dtool.Dispatch(result, a.deps.Delta, ps)
	a.syncLoopState(ps)

	// Step-level guard: post-record
	a.guard.PostToolRecord(tc.Function.Name)

	// Track plan.md modifications for EphPlanModifiedReminder
	if tc.Function.Name == "task_plan" {
		var planArgs struct {
			Action string `json:"action"`
			Type   string `json:"type"`
		}
		if json.Unmarshal([]byte(tc.Function.Arguments), &planArgs) == nil {
			if (planArgs.Action == "create" || planArgs.Action == "update") &&
				(planArgs.Type == "plan" || planArgs.Type == "") {
				a.mu.Lock()
				a.planModified = true
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

// estimateTokens returns a rough token count of the current history.
func (a *AgentLoop) estimateTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Baseline: system prompt is assembled separately in doGenerate (~3000 tokens)
	total := 3000
	for _, msg := range a.history {
		total += len(msg.Content) / 4
		total += len(msg.Reasoning) / 4
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 4
			total += len(tc.Function.Arguments) / 4
		}
	}
	return total
}

// doCompact performs LLM-based history compaction.
// Uses turn-boundary-aware slicing to preserve tool_call/tool message pairs.
func (a *AgentLoop) doCompact() {
	a.mu.Lock()
	if len(a.history) <= 6 {
		a.mu.Unlock()
		return
	}

	// Find safe cut point: walk backward from end to find 2 complete user turns.
	// A "turn" starts at a user message and extends through assistant+tool messages.
	safeCut := 1 // default: keep everything except history[0]
	userCount := 0
	for i := len(a.history) - 1; i > 0; i-- {
		if a.history[i].Role == "user" {
			userCount++
			if userCount >= 2 {
				safeCut = i
				break
			}
		}
	}
	// If we couldn't find 2 user turns, keep last half
	if userCount < 2 {
		safeCut = len(a.history) / 2
	}

	// Extract middle section to summarize (skip first msg + keep tail)
	middle := a.history[1:safeCut]
	tail := make([]llm.Message, len(a.history)-safeCut)
	copy(tail, a.history[safeCut:])
	firstMsg := a.history[0] // Preserve regardless of role
	a.mu.Unlock()

	// Build summarization request
	var content strings.Builder
	for _, msg := range middle {
		if msg.Content != "" {
			content.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
	}

	// Four-dimensional checkpoint (mirrors Anti's CortexStepCheckpoint)
	summaryMessages := []llm.Message{
		{Role: "system", Content: `你是对话摘要器。从以下对话中提取四个维度的摘要：

## user_intent
用户的核心目标和当前进展状态。

## session_summary
本次会话执行了哪些操作，结果如何。

## code_changes
修改了哪些文件，具体改了什么（函数名+改动要点）。

## learned_facts
发现的重要架构信息、约束条件、或需要记住的决策。

每个维度 2-3 句话，总计不超过 500 字。`},
		{Role: "user", Content: content.String()},
	}

	model := a.deps.LLMRouter.CurrentModel()
	provider, _ := a.deps.LLMRouter.Resolve(model)

	// Bug #7 fix: defer cancel at function level, not inside if-block
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary := ""
	if provider != nil {
		req := &llm.Request{
			Model:       model,
			Messages:    summaryMessages,
			Temperature: 0.3,
			MaxTokens:   1024,
			Stream:      false,
		}

		ch := make(chan llm.StreamChunk, 32)
		resp, err := provider.GenerateStream(ctx, req, ch)
		// Drain channel
		for range ch {
		}
		if err == nil && resp != nil && resp.Content != "" {
			summary = resp.Content
		}
	}

	// Fallback: simple truncation if LLM fails
	if summary == "" {
		for _, msg := range middle {
			if msg.Role == "assistant" && msg.Content != "" {
				summary += msg.Content[:min(200, len(msg.Content))] + "... "
			}
		}
	}

	// Rebuild history: first message (preserved) + summary + safe tail (complete turns)
	a.mu.Lock()
	defer a.mu.Unlock()

	compacted := []llm.Message{firstMsg}
	if summary != "" {
		// BUG-19: if firstMsg is already a summary, replace it instead of nesting
		if strings.HasPrefix(firstMsg.Content, "[对话摘要]") {
			compacted = []llm.Message{{
				Role:    "assistant",
				Content: "[对话摘要] " + summary,
			}}
		} else {
			compacted = append(compacted, llm.Message{
				Role:    "assistant",
				Content: "[对话摘要] " + summary,
			})
		}
	}
	compacted = append(compacted, tail...)
	a.history = compacted
}

// forceTruncate keeps only system + last N messages.
func (a *AgentLoop) forceTruncate(keep int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.history) <= keep+1 {
		return
	}
	
	// Preserve first user message at index 0, append the last `keep` items
	truncated := []llm.Message{a.history[0]}
	truncated = append(truncated, a.history[len(a.history)-keep:]...)
	a.history = truncated
	a.persistedCount = 0 // history restructured, next persist must be full replace
}

// persistHistory saves NEW messages incrementally (append-only).
// Only messages added since the last persist (or session load) are written.
// This prevents destructive overwrites of existing DB history.
func (a *AgentLoop) persistHistory() {
	if a.deps.HistoryStore == nil {
		return
	}
	sid := a.SessionID()
	if sid == "" {
		return
	}
	a.mu.Lock()
	baseline := a.persistedCount
	if baseline >= len(a.history) {
		a.mu.Unlock()
		return // nothing new
	}
	newMsgs := a.history[baseline:]
	exports := make([]HistoryExport, len(newMsgs))
	for i, m := range newMsgs {
		tc, _ := json.Marshal(m.ToolCalls)
		exports[i] = HistoryExport{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  string(tc),
			ToolCallID: m.ToolCallID,
		}
	}
	a.persistedCount = len(a.history)
	a.mu.Unlock()
	if err := a.deps.HistoryStore.AppendAll(sid, exports); err != nil {
		log.Printf("[history] incremental persist failed: %v", err)
	}
}

// persistFullHistory does a destructive full replace of the DB history.
// Called ONLY after doCompact/forceTruncate which intentionally restructure the history.
func (a *AgentLoop) persistFullHistory() {
	if a.deps.HistoryStore == nil {
		return
	}
	sid := a.SessionID()
	if sid == "" {
		return
	}
	a.mu.Lock()
	exports := make([]HistoryExport, len(a.history))
	for i, m := range a.history {
		tc, _ := json.Marshal(m.ToolCalls)
		exports[i] = HistoryExport{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  string(tc),
			ToolCallID: m.ToolCallID,
		}
	}
	a.persistedCount = len(a.history)
	a.mu.Unlock()
	if err := a.deps.HistoryStore.SaveAll(sid, exports); err != nil {
		log.Printf("[history] full persist failed: %v", err)
	}
}

// fireHooks invokes all PostRunHooks asynchronously.
func (a *AgentLoop) fireHooks(ctx context.Context, steps int) {
	if a.deps.Hooks == nil {
		return
	}
	a.mu.Lock()
	finalContent := ""
	userMsg := ""
	for _, m := range a.history {
		if m.Role == "user" && m.Content != "" {
			userMsg = m.Content
			break
		}
	}
	if len(a.history) > 0 {
		finalContent = a.history[len(a.history)-1].Content
	}
	// Snapshot history for async hooks (KI distillation)
	historySnapshot := make([]llm.Message, len(a.history))
	copy(historySnapshot, a.history)
	a.mu.Unlock()
	a.deps.Hooks.OnRunComplete(ctx, RunInfo{
		SessionID:    a.SessionID(),
		UserMessage:  userMsg,
		Steps:        steps,
		Mode:         a.options.Mode,
		FinalContent: finalContent,
		History:      historySnapshot,
	})
}

// Ensure ctxutil is used
var _ = ctxutil.SessionIDFromContext

