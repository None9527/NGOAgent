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
	// Create cancellable context — Stop() calls runCancel() to kill running sandbox processes.
	runCtx, runCancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.runCancel = runCancel
	a.mu.Unlock()
	defer func() {
		runCancel()
		a.mu.Lock()
		a.runCancel = nil
		a.mu.Unlock()
	}()

	// Guarantee history is persisted on ALL exit paths (API error, Stop, ctx cancel).
	// persistHistory is idempotent — the extra call from StateDone is harmless.
	defer a.persistHistory()

	a.mu.Lock()
	// Recreate stopCh if it was closed by a previous Stop()
	select {
	case <-a.stopCh:
		a.stopCh = make(chan struct{})
	default:
	}
	a.history = append(a.history, a.buildUserMessage(userMessage))
	a.state = StatePrepare
	a.mu.Unlock()

	// Persist the user message immediately so that UI refreshes during a long LLM generation
	// will still see the user's prompt in the session history.
	a.persistHistory()

	a.guard.ResetTurn()

	opts := a.options
	steps := 0
	retries := 0

	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-a.stopCh:
			return fmt.Errorf("agent stopped")
		default:
		}

		switch a.CurrentState() {
		case StatePrepare:
			a.doPrepare(runCtx)
			a.transition(StateGenerate)

		case StateGenerate:
			resp, err := a.doGenerate(runCtx, opts)
			if err != nil {
				a.transition(StateError)
				if llmErr, ok := err.(*llm.LLMError); ok {
					switch llmErr.Level {
					case llm.ErrorTransient, llm.ErrorOverload:
						base, maxR := llm.BackoffConfig(llmErr.Level)
						if retries < maxR {
							retries++
							backoff := llm.BackoffWithJitter(base, retries-1)
							log.Printf("[retry] %s attempt %d/%d, backoff %v: %s",
								llmErr.Level, retries, maxR, backoff, llmErr.Code)
							time.Sleep(backoff)
							a.transition(StateGenerate)
							continue
						}
					case llm.ErrorContextOverflow:
						if retries < 1 {
							retries++
							log.Printf("[retry] context overflow → compacting then retry")
							a.transition(StateCompact)
							continue
						}
					case llm.ErrorBilling:
						log.Printf("[error] billing/quota exhausted: %s", llmErr.Message)
						a.transition(StateFatal)
						a.deps.Delta.OnError(err)
						return err
					case llm.ErrorFatal:
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
				continue
			case "warn":
				a.InjectEphemeral(verdict.Message)
			}

			if len(resp.ToolCalls) == 0 {
				a.transition(StateDone)
				continue
			}
			a.transition(StateToolExec)

		case StateToolExec:
			a.mu.Lock()
			lastMsg := a.history[len(a.history)-1]
			a.mu.Unlock()

			for i, tc := range lastMsg.ToolCalls {
				result, err := a.doToolExec(runCtx, tc)

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

					a.deps.Delta.OnText("\n" + result + "\n")
					a.transition(StateDone)
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
				continue
			}

			a.transition(StateGuardCheck)

		case StateGuardCheck:
			steps++

			// maxSteps enforcement is handled by BehaviorGuard.Check() — no duplication here

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
			// Snapshot file edit history for this message turn
			if a.deps.FileHistory != nil && a.deps.FileHistory.HasPendingEdits() {
				msgID := fmt.Sprintf("%s_step%d", a.SessionID(), steps)
				a.deps.FileHistory.Snapshot(msgID)
			}
			a.state = StateIdle
			a.persistHistory()
			// OnComplete FIRST: release frontend (step_done event) immediately.
			a.deps.Delta.OnComplete()
			// Hooks run async: must NOT block runInner return (which releases run lock).
			go a.fireHooks(runCtx, steps)

			// ── Pending Wake tail-check (subagent orchestration) ──
			// If subagent results arrived during this run (barrier called SignalWake),
			// auto-continue within the same lock to process the injected ephemerals.
			// This eliminates the "agent is busy" race between ChatStream and auto-wake.
			if a.pendingWake.CompareAndSwap(true, false) {
				log.Printf("[loop] pendingWake detected, auto-continuing for subagent results")
				steps = 0
				retries = 0
				// Signal frontend: auto-wake phase starting
				if a.deps.Delta != nil {
					a.deps.Delta.OnAutoWakeStart()
				}
				a.mu.Lock()
				a.history = append(a.history, a.buildUserMessage(""))
				a.mu.Unlock()
				a.transition(StatePrepare) // Legal: Done→Prepare
				continue // re-enter the for loop — processes ephemerals in next generate
			}
			return nil

		default:
			return fmt.Errorf("unexpected state: %s", a.CurrentState())
		}
	}
loopEnd:
	a.persistHistory()
	a.deps.Delta.OnComplete()
	go a.fireHooks(runCtx, steps)
	return nil
}

func (a *AgentLoop) transition(to State) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !CanTransition(a.state, to) {
		log.Printf("[WARN] invalid state transition: %s → %s", a.state, to)
	}
	a.state = to
}

// doPrepare and shouldInjectPlanning are in prepare.go

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
	var systemPrompt string
	if a.options.Mode == "subagent" {
		systemPrompt, _ = a.deps.PromptEngine.AssembleSubagent(promptDeps)
	} else {
		systemPrompt, _ = a.deps.PromptEngine.Assemble(promptDeps)
	}

	// Build messages
	messages := make([]llm.Message, 0, len(a.history)+1)
	messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})

	a.mu.Lock()
	messages = append(messages, a.history...)
	// Drain ephemerals
	ephemerals := a.ephemerals
	a.ephemerals = nil
	// Drain pending media
	mediaItems := a.pendingMedia
	a.pendingMedia = nil
	a.mu.Unlock()

	// ═══ Multimodal: inject pending media as ContentParts ═══
	// Media loaded via view_media tool becomes visible to the VLM in the next call.
	if len(mediaItems) > 0 {
		var parts []llm.ContentPart
		var pathList []string
		for _, item := range mediaItems {
			switch item["type"] {
			case "image_url":
				parts = append(parts, llm.ContentPart{
					Type:     "image_url",
					ImageURL: &llm.ImageURL{URL: item["url"]},
				})
			case "video":
				parts = append(parts, llm.ContentPart{
					Type:  "video",
					Video: item["url"],
				})
			case "input_audio":
				parts = append(parts, llm.ContentPart{
					Type: "input_audio",
					InputAudio: &llm.InputAudio{
						Data:   item["data"],
						Format: item["format"],
					},
				})
			}
			if p := item["path"]; p != "" {
				pathList = append(pathList, p)
			}
		}
		if len(parts) > 0 {
			// Prepend a text part identifying the media
			textPart := llm.ContentPart{
				Type: "text",
				Text: fmt.Sprintf("[Media loaded: %s] Describe what you see/hear.", strings.Join(pathList, ", ")),
			}
			parts = append([]llm.ContentPart{textPart}, parts...)
			messages = append(messages, llm.Message{
				Role:         "user",
				Content:      fmt.Sprintf("[Media: %s]", strings.Join(pathList, ", ")),
				ContentParts: parts,
			})
		}
	}

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

	// Resolve per-model parameters (model_config > agent global > fallback)
	mp := a.deps.Config.ResolveModelParams(model)

	req := &llm.Request{
		Model:       model,
		Messages:    messages,
		Tools:       a.deps.ToolExec.ListDefinitions(),
		Temperature: mp.Temperature,
		TopP:        mp.TopP,
		MaxTokens:   mp.MaxOutputTokens,
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
			a.deps.Delta.OnText(chunk.Text) // Stream in real-time
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

	// Flush buffered text to SSE client.
	// Always flush regardless of StopReason to ensure SSE output matches
	// what gets persisted to DB (resp.Content is stored unconditionally).
	if resp != nil {
		log.Printf("[doGenerate] StopReason=%q textLen=%d toolCalls=%d", resp.StopReason, textBuf.Len(), len(resp.ToolCalls))
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

	// Knowledge — inject KI index (title + summary + artifact paths).
	// Agent uses read_file to access full content when needed.
	if a.deps.KIStore != nil {
		deps.ConvSummary = a.deps.KIStore.GenerateKIIndex()
	}

	// Semantic Memory — retrieve relevant conversation fragments from vector memory.
	if a.deps.MemoryStore != nil {
		var query string
		for i := len(a.history) - 1; i >= 0; i-- {
			if a.history[i].Role == "user" {
				query = a.history[i].Content
				break
			}
		}
		if query != "" {
			deps.MemoryContent = a.deps.MemoryStore.FormatForPrompt(query, 5, 2000)
		}
	}

	// Skills — summaries for prompt injection
	if a.deps.SkillMgr != nil {
		skills := a.deps.SkillMgr.List()
		for _, s := range skills {
			deps.SkillInfos = append(deps.SkillInfos, prompt.SkillInfo{
				Name:        s.Name,
				Description: s.Description,
				Type:        s.Type,
				Weight:      s.Weight,
				Command:     s.Command,
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
	b.WriteString("# Environment\n")
	b.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("- Time: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	b.WriteString(fmt.Sprintf("- Model: %s\n", model))

	// Agent home: ~/.ngoagent/ (skills, brain, knowledge, config)
	homeDir := config.HomeDir()
	b.WriteString(fmt.Sprintf("- Agent Home: %s\n", homeDir))

	// Workspace: configured project working directory
	if a.deps.Config != nil && a.deps.Config.Agent.Workspace != "" {
		ws := a.deps.Config.Agent.Workspace
		if strings.HasPrefix(ws, "~") {
			if h, err := os.UserHomeDir(); err == nil {
				ws = h + ws[1:]
			}
		}
		b.WriteString(fmt.Sprintf("- Workspace: %s\n", ws))
	} else {
		cwd, _ := os.Getwd()
		b.WriteString(fmt.Sprintf("- Workspace: %s\n", cwd))
	}

	return b.String()
}

// doToolExec executes a single tool call with security check.
func (a *AgentLoop) doToolExec(ctx context.Context, tc llm.ToolCall) (string, error) {
	// Track how many tool calls since last task_boundary update
	a.mu.Lock()
	a.stepsSinceUpdate++
	a.currentStep++
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
		a.deps.Delta.OnToolStart(tc.ID, tc.Function.Name+" [待审批: "+reason+"]", args)
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

	// Hook: BeforeTool (Modifying — can alter args or skip)
	if a.deps.Hooks != nil {
		var skip bool
		args, skip = a.deps.Hooks.FireBeforeTool(ctx, tc.Function.Name, args)
		if skip {
			return fmt.Sprintf("Tool '%s' skipped by hook", tc.Function.Name), nil
		}
	}

	a.deps.Delta.OnToolStart(tc.ID, tc.Function.Name, args)
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

	a.deps.Delta.OnToolResult(tc.ID, tc.Function.Name, output, err)
	a.deps.Security.AfterToolCall(ctx, tc.Function.Name, output, err)

	// Hook: AfterTool (Void — logging, audit, stats)
	if a.deps.Hooks != nil {
		a.deps.Hooks.FireAfterTool(ctx, tc.Function.Name, output, err)
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
					a.planModified = true
					a.mu.Unlock()
				}
				// Record artifact last step for staleness tracking
				artifactName := planArgs.Type + ".md"
				if planArgs.Type == "" {
					artifactName = "plan.md"
				}
				a.mu.Lock()
				a.artifactLastStep[artifactName] = a.currentStep
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
// Uses unicode-aware counting: CJK characters ≈ 1.5 tokens each, ASCII ≈ 0.25.
func (a *AgentLoop) estimateTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Baseline: system prompt is assembled separately in doGenerate (~3000 tokens)
	total := 3000
	for _, msg := range a.history {
		total += estimateStringTokens(msg.Content)
		total += estimateStringTokens(msg.Reasoning)
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name)/4 + len(tc.Function.Arguments)/4
		}
	}
	return total
}

// estimateStringTokens counts tokens with CJK awareness.
func estimateStringTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	var tokens float64
	for _, r := range s {
		if r >= 0x2E80 { // CJK Radicals Supplement and beyond
			tokens += 1.5
		} else {
			tokens += 0.25
		}
	}
	return int(tokens)
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

	// Hook: BeforeCompact — save to vector memory before content is lost
	if a.deps.Hooks != nil {
		a.deps.Hooks.FireBeforeCompact(context.Background(), middle)
	}

	// Build summarization request
	var content strings.Builder
	for _, msg := range middle {
		if msg.Content != "" {
			content.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
	}

	// Four-dimensional checkpoint (mirrors Anti's CortexStepCheckpoint)
	summaryMessages := []llm.Message{
		{Role: "system", Content: `You are a conversation summarizer. Extract a summary across four dimensions from the conversation below:

## user_intent
The user's core goal and current progress status.

## session_summary
What operations were performed in this session and their outcomes.

## code_changes
Which files were modified, what specifically changed (function names + key change points).

## learned_facts
Important architectural information, constraints, or decisions that need to be remembered.

CRITICAL: If the conversation contains content inside <preference_knowledge> or <semantic_knowledge> tags, it MUST be preserved in full in learned_facts — no omission or abbreviation allowed.

2–3 sentences per dimension, 500 words total maximum.`},
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

	// Hook: AfterCompact — notify of new compacted state
	if a.deps.Hooks != nil {
		go a.deps.Hooks.FireAfterCompact(context.Background(), compacted)
	}
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
			Reasoning:  m.Reasoning,
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
			Reasoning:  m.Reasoning,
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
		Delta:        a.deps.Delta,
	})
}

// Ensure ctxutil is used
var _ = ctxutil.SessionIDFromContext

// buildUserMessage and multimodal logic are in multimodal.go
