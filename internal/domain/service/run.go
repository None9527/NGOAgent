package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

// ErrApprovalDenied signals that a tool call was denied by the security hook.
// When this error is returned, the loop must stop immediately.
var ErrApprovalDenied = agenterr.ErrDenied

// Run executes the ReAct loop for one user turn.
func (a *AgentLoop) Run(ctx context.Context, userMessage string) error {
	// Backpressure: prevent concurrent runs on same loop (Anti's BUSY state)
	if !a.runMu.TryLock() {
		return agenterr.NewBusy("another run is in progress")
	}
	defer a.runMu.Unlock()
	return a.runEntry(ctx, userMessage)
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
	return a.runEntry(ctx, userMessage)
}

func (a *AgentLoop) runEntry(ctx context.Context, userMessage string) error {
	if userMessage == "" {
		if handled, err := a.HandleReconnect(ctx); err != nil {
			return err
		} else if handled {
			return nil
		}
		return fmt.Errorf("no pending execution for session %s", a.SessionID())
	}
	return a.runGraph(ctx, userMessage)
}

func (a *AgentLoop) prepareTurn(userMessage string) {
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		// Recreate stopCh if it was closed by a previous Stop()
		select {
		case <-a.stopCh:
			a.stopCh = make(chan struct{})
		default:
		}
		a.history = append(a.history, a.buildUserMessage(userMessage))
	}()

	// Persist the user message immediately so that UI refreshes during a long LLM generation
	// will still see the user's prompt in the session history.
	a.persistHistory()

	// Detect which behavior overlays should be active based on user message + workspace.
	if a.deps.PromptEngine != nil {
		var wsFiles []string
		if a.deps.Workspace != nil {
			wsFiles = a.deps.Workspace.RootFiles()
		}
		a.deps.PromptEngine.ActivateOverlays(userMessage, wsFiles)
		slog.Info(fmt.Sprintf("[overlay] Active: %s", a.deps.PromptEngine.ActiveProfile()))
	}

	a.dream.OnWake()
	a.phaseDetector.Reset()

	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.task.Name = ""
		a.task.Mode = ""
		a.task.PreviousMode = ""
		a.task.Status = ""
		a.task.Summary = ""
		a.task.StepsSinceUpdate = 0
		a.task.YieldRequested = false
	}()

	a.guard.ResetTurn()
}

func (a *AgentLoop) runGraph(ctx context.Context, userMessage string) (runErr error) {
	runCtx, runCancel := context.WithCancel(ctx)
	func() { a.mu.Lock(); defer a.mu.Unlock(); a.runCancel = runCancel }()
	defer func() {
		runCancel()
		func() { a.mu.Lock(); defer a.mu.Unlock(); a.runCancel = nil }()
	}()

	defer a.persistHistory()
	defer func() {
		if a.deps.WebhookHook != nil {
			if runErr != nil {
				a.deps.WebhookHook.OnError(a.SessionID(), runErr)
			} else {
				a.deps.WebhookHook.OnComplete(a.SessionID())
			}
		}
	}()

	a.prepareTurn(userMessage)

	rt, err := NewAgentLoopRuntime(a, a.deps.SnapshotStore)
	if err != nil {
		return err
	}

	turnState := graphruntime.TurnState{
		RunID:       MakeRunID(a.SessionID(), "chat"),
		UserMessage: userMessage,
		Mode:        a.options.Mode,
	}
	runCtx = ctxutil.WithRunID(runCtx, turnState.RunID)
	req := graphruntime.RunRequest{
		RunID:   turnState.RunID,
		Session: &graphruntime.SessionState{SessionID: a.SessionID()},
		Turn:    turnState,
	}
	return rt.Run(runCtx, req)
}

func (a *AgentLoop) resumeGraph(ctx context.Context, runID string) (runErr error) {
	runCtx, runCancel := context.WithCancel(ctx)
	func() { a.mu.Lock(); defer a.mu.Unlock(); a.runCancel = runCancel }()
	defer func() {
		runCancel()
		func() { a.mu.Lock(); defer a.mu.Unlock(); a.runCancel = nil }()
	}()

	defer a.persistHistory()
	defer func() {
		if a.deps.WebhookHook != nil {
			if runErr != nil {
				a.deps.WebhookHook.OnError(a.SessionID(), runErr)
			} else {
				a.deps.WebhookHook.OnComplete(a.SessionID())
			}
		}
	}()

	rt, err := NewAgentLoopRuntime(a, a.deps.SnapshotStore)
	if err != nil {
		return err
	}
	runCtx = ctxutil.WithRunID(runCtx, runID)
	if err := a.hydratePendingBarrier(runCtx, runID); err != nil {
		return err
	}
	return rt.Resume(runCtx, runID)
}

func (a *AgentLoop) hydratePendingBarrier(ctx context.Context, runID string) error {
	if a.deps.SnapshotStore == nil {
		return nil
	}
	snap, err := a.deps.SnapshotStore.LoadLatest(ctx, runID)
	if err != nil {
		return err
	}
	wait := newWaitSnapshotView(snap)
	target := wait.hydratableBarrier()
	if target == nil {
		a.ClearActiveBarrier()
		return nil
	}

	if existing := a.activeBarrierSnapshot(); existing != nil && existing.ID == target.ID {
		return nil
	}

	a.SetActiveBarrier(NewSubagentBarrierFromState(a, func() {
		a.ClearActiveBarrier()
		a.SignalWake()
	}, *target))
	return nil
}

func (a *AgentLoop) RestorePendingApproval(ctx context.Context, approvalID string) (bool, error) {
	if approvalID == "" || a.deps.SnapshotStore == nil || a.deps.Security == nil {
		return false, nil
	}

	wait, err := a.latestWaitSnapshotView(ctx)
	if err != nil {
		return false, err
	}
	snap := wait.approvalSnapshot(approvalID)
	if snap == nil {
		return false, nil
	}

	a.deps.Security.RestorePendingApproval(ApprovalSnapshot{
		ID:        snap.ExecutionState.PendingApproval.ID,
		ToolName:  snap.ExecutionState.PendingApproval.ToolName,
		Args:      cloneMap(snap.ExecutionState.PendingApproval.Args),
		Reason:    snap.ExecutionState.PendingApproval.Reason,
		Requested: snap.ExecutionState.PendingApproval.RequestedAt,
	})
	return true, nil
}

func (a *AgentLoop) ApprovePending(ctx context.Context, approvalID string, approved bool) (bool, error) {
	if approvalID == "" || a.deps.Security == nil {
		return false, nil
	}

	if err := a.deps.Security.ResolvePendingApproval(approvalID, approved); err == nil {
		return true, a.finalizePendingApproval(ctx, approvalID)
	} else if restored, restoreErr := a.RestorePendingApproval(ctx, approvalID); restoreErr != nil {
		return false, restoreErr
	} else if !restored {
		return false, nil
	}

	if err := a.deps.Security.ResolvePendingApproval(approvalID, approved); err != nil {
		return false, err
	}
	return true, a.finalizePendingApproval(ctx, approvalID)
}

func (a *AgentLoop) finalizePendingApproval(ctx context.Context, approvalID string) error {
	a.deps.Security.CleanupPending(approvalID)
	_, err := a.ClearPendingApprovalSnapshot(ctx, approvalID)
	return err
}

func (a *AgentLoop) HandleReconnect(ctx context.Context) (bool, error) {
	wait, err := a.latestWaitSnapshotView(ctx)
	if err != nil {
		return false, err
	}

	switch wait.reconnectAction() {
	case reconnectActionResumeBarrier:
		runID, _ := wait.autoResumeRunID()
		slog.Info(fmt.Sprintf("[session] Resuming pending run %s for session %s", runID, a.SessionID()))
		return true, a.resumeGraph(ctx, runID)
	case reconnectActionReplayApproval:
		return wait.emitApprovalRequest(a.deps.Delta), nil
	case reconnectActionReplayPlan:
		return wait.emitPlanReview(a.deps.Delta), nil
	}

	return false, nil
}

func (a *AgentLoop) ClearPendingApprovalSnapshot(ctx context.Context, approvalID string) (bool, error) {
	if approvalID == "" || a.deps.SnapshotStore == nil {
		return false, nil
	}

	wait, err := a.latestWaitSnapshotView(ctx)
	if err != nil {
		return false, err
	}
	snap := wait.approvalSnapshot(approvalID)
	if snap == nil {
		return false, nil
	}

	snap.ExecutionState.PendingApproval = nil
	snap.ExecutionState.WaitReason = graphruntime.WaitReasonNone
	snap.UpdatedAt = time.Now()
	return true, a.deps.SnapshotStore.Save(ctx, snap)
}

func (a *AgentLoop) ReviewPendingPlan(ctx context.Context, approved bool, feedback string) (bool, error) {
	decision := "revise"
	if approved {
		decision = "approved"
	}
	return a.ApplyPendingDecision(ctx, string(graphruntime.DecisionKindPlanReview), decision, feedback)
}

func (a *AgentLoop) ResumeRun(ctx context.Context, runID string) (bool, error) {
	if a.deps.SnapshotStore == nil {
		return false, nil
	}

	runID = strings.TrimSpace(runID)
	if runID != "" {
		snap, err := a.deps.SnapshotStore.LoadLatest(ctx, runID)
		if err != nil {
			return false, err
		}
		if snap == nil || snap.SessionID != a.SessionID() {
			return false, nil
		}
		return true, a.resumeGraph(ctx, runID)
	}

	snap, err := a.latestWaitSnapshot(ctx)
	if err != nil {
		return false, err
	}
	if snap == nil {
		return false, nil
	}
	return true, a.resumeGraph(ctx, snap.RunID)
}

func (a *AgentLoop) latestWaitSnapshot(ctx context.Context) (*graphruntime.RunSnapshot, error) {
	if a.deps.SnapshotStore == nil {
		return nil, nil
	}
	snap, err := a.deps.SnapshotStore.LoadLatestBySession(ctx, a.SessionID())
	if err != nil {
		return nil, err
	}
	if snap == nil || snap.Status != graphruntime.NodeStatusWait {
		return nil, nil
	}
	return snap, nil
}

func (a *AgentLoop) latestWaitSnapshotView(ctx context.Context) (waitSnapshotView, error) {
	snap, err := a.latestWaitSnapshot(ctx)
	if err != nil {
		return waitSnapshotView{}, err
	}
	return newWaitSnapshotView(snap), nil
}

type waitSnapshotKind string
type reconnectAction string

const (
	waitSnapshotNone     waitSnapshotKind = ""
	waitSnapshotApproval waitSnapshotKind = "approval"
	waitSnapshotBarrier  waitSnapshotKind = "barrier"
	waitSnapshotPlan     waitSnapshotKind = "plan_review"
	waitSnapshotOther    waitSnapshotKind = "other"

	reconnectActionNone           reconnectAction = ""
	reconnectActionReplayApproval reconnectAction = "replay_approval"
	reconnectActionReplayPlan     reconnectAction = "replay_plan_review"
	reconnectActionResumeBarrier  reconnectAction = "resume_barrier"
)

type waitSnapshotView struct {
	snapshot *graphruntime.RunSnapshot
}

func newWaitSnapshotView(snap *graphruntime.RunSnapshot) waitSnapshotView {
	return waitSnapshotView{snapshot: snap}
}

func (w waitSnapshotView) kind() waitSnapshotKind {
	if w.snapshot == nil || w.snapshot.Status != graphruntime.NodeStatusWait {
		return waitSnapshotNone
	}
	switch w.snapshot.ExecutionState.WaitReason {
	case graphruntime.WaitReasonApproval:
		return waitSnapshotApproval
	case graphruntime.WaitReasonBarrier:
		return waitSnapshotBarrier
	case graphruntime.WaitReasonUserInput:
		if w.snapshot.TurnState.Intelligence.Planning.Required && w.snapshot.TurnState.Intelligence.Planning.ReviewRequired {
			return waitSnapshotPlan
		}
		return waitSnapshotOther
	case graphruntime.WaitReasonNone:
		return waitSnapshotNone
	default:
		return waitSnapshotOther
	}
}

func (w waitSnapshotView) approvalSnapshot(approvalID string) *graphruntime.RunSnapshot {
	if w.kind() != waitSnapshotApproval || w.snapshot.ExecutionState.PendingApproval == nil {
		return nil
	}
	if approvalID != "" && w.snapshot.ExecutionState.PendingApproval.ID != approvalID {
		return nil
	}
	return w.snapshot
}

func (w waitSnapshotView) planSnapshot() *graphruntime.RunSnapshot {
	if w.kind() != waitSnapshotPlan {
		return nil
	}
	return w.snapshot
}

func (w waitSnapshotView) hydratableBarrier() *graphruntime.BarrierState {
	if w.kind() != waitSnapshotBarrier || w.snapshot.ExecutionState.PendingWake {
		return nil
	}
	return w.snapshot.ExecutionState.PendingBarrier
}

func (w waitSnapshotView) autoResumeRunID() (string, bool) {
	if w.kind() != waitSnapshotBarrier || !w.snapshot.ExecutionState.PendingWake {
		return "", false
	}
	return w.snapshot.RunID, true
}

func (w waitSnapshotView) replaysApprovalOnReconnect() bool {
	return w.kind() == waitSnapshotApproval && w.snapshot.ExecutionState.PendingApproval != nil
}

func (w waitSnapshotView) reconnectAction() reconnectAction {
	if _, ok := w.autoResumeRunID(); ok {
		return reconnectActionResumeBarrier
	}
	if w.replaysApprovalOnReconnect() {
		return reconnectActionReplayApproval
	}
	if w.replaysPlanReviewOnReconnect() {
		return reconnectActionReplayPlan
	}
	return reconnectActionNone
}

func (w waitSnapshotView) emitApprovalRequest(delta DeltaSink) bool {
	if !w.replaysApprovalOnReconnect() || delta == nil {
		return false
	}
	pending := w.snapshot.ExecutionState.PendingApproval
	delta.OnApprovalRequest(
		pending.ID,
		pending.ToolName,
		cloneMap(pending.Args),
		pending.Reason,
	)
	return true
}

func (w waitSnapshotView) replaysPlanReviewOnReconnect() bool {
	return w.kind() == waitSnapshotPlan && strings.TrimSpace(w.snapshot.TurnState.Intelligence.Planning.ReviewDecision) == ""
}

func (w waitSnapshotView) emitPlanReview(delta DeltaSink) bool {
	if !w.replaysPlanReviewOnReconnect() || delta == nil {
		return false
	}
	planning := w.snapshot.TurnState.Intelligence.Planning
	message := "Planning review required before execution"
	if planning.Trigger != "" {
		message = "Planning trigger: " + planning.Trigger
	}
	delta.OnPlanReview(message, append([]string(nil), planning.MissingArtifacts...))
	return true
}

func (a *AgentLoop) activeBarrierSnapshot() *graphruntime.BarrierState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.barrier == nil {
		return nil
	}
	barrier := a.barrier.Snapshot()
	return &barrier
}

func (a *AgentLoop) transitionTo(next State, route string) graphruntime.NodeResult {
	return graphruntime.NodeResult{RouteKey: route, ObservedState: next.String()}
}

func (a *AgentLoop) finishWith(observed State, status graphruntime.NodeStatus) graphruntime.NodeResult {
	return graphruntime.NodeResult{Status: status, ObservedState: observed.String()}
}

// doPrepare and shouldInjectPlanning are in prepare.go

// doGenerate calls the LLM with the fully assembled system prompt.
func (a *AgentLoop) doGenerate(ctx context.Context, opts RunOptions, excluded []string) (*llm.Response, string, error) {
	model := opts.Model
	if model == "" {
		model = a.deps.LLMRouter.CurrentModel()
	}

	// P1 #27: Resolve with fallback and explicit exclusions (Phase 4 intelligent failover)
	provider, model, err := a.deps.LLMRouter.ResolveWithExclusions(model, excluded)
	if err != nil {
		return nil, "", err
	}
	provName := provider.Name()

	// ═══ Assemble system prompt with ALL data sources ═══
	promptDeps := a.buildPromptDeps(ctx, model, opts)
	var systemPrompt string
	var useCache bool
	var splitResult prompt.AssembleResult
	if a.options.Mode == "subagent" {
		systemPrompt, _ = a.deps.PromptEngine.AssembleSubagent(promptDeps)
	} else {
		// Use AssembleSplit: static (cacheable) + dynamic (per-request)
		splitResult = a.deps.PromptEngine.AssembleSplit(promptDeps)
		systemPrompt = splitResult.Static + "\n\n" + splitResult.Dynamic
		// Gate on provider capability + DashScope minimum 1024 token threshold
		policy := llm.GetPolicy(model)
		useCache = policy.SupportsCache && splitResult.TokenStatic >= 1024
	}

	// Track actual system prompt size for precise token estimation
	a.tokenTracker.SetSystemPromptSize(estimateStringTokens(systemPrompt))

	// P2 E2: Record prompt hash for cache-break tracking
	a.cacheTracker.RecordCall(llm.HashString(systemPrompt))

	// Build messages — use ContentParts with cache_control when provider supports it
	messages := make([]llm.Message, 0, len(a.history)+1)
	if useCache {
		// Multi-breakpoint cache: each segment gets its own cache_control marker
		// DashScope supports up to 4 markers; we use 2 (core + session)
		var parts []llm.ContentPart
		for _, seg := range splitResult.Segments {
			part := llm.ContentPart{Type: "text", Text: seg.Content}
			if seg.Cacheable {
				part.CacheControl = &llm.CacheControl{Type: "ephemeral"}
			}
			parts = append(parts, part)
		}
		messages = append(messages, llm.Message{
			Role:         "system",
			Content:      systemPrompt, // Fallback for providers that ignore ContentParts
			ContentParts: parts,
		})
	} else {
		messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})
	}

	var ephemerals []string
	var mediaItems []map[string]string
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		messages = append(messages, a.history...)
		// Drain ephemerals
		ephemerals = a.ephemerals
		a.ephemerals = nil
		// Drain pending media
		mediaItems = a.pendingMedia
		a.pendingMedia = nil
	}()

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

	// Sanitize only when history was restructured (compact/truncate).
	// Normal flow produces well-ordered messages, so this is unnecessary overhead.
	var dirty bool
	func() { a.mu.Lock(); defer a.mu.Unlock(); dirty = a.historyDirty; a.historyDirty = false }()
	if dirty {
		messages = enforceTurnOrdering(messages)
		messages = sanitizeMessages(messages)
	}

	// Check if Guard wants to force a specific tool (Anti's force_tool_name mechanism)
	forceTool := a.guard.ConsumeForceToolName()

	// Resolve per-model parameters (model_config > agent global > fallback)
	mp := a.deps.Config.ResolveModelParams(model)

	// Cache tool definitions — tools don't change during a session
	var toolDefs []llm.ToolDef
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.cachedToolDefs == nil {
			a.cachedToolDefs = a.deps.ToolExec.ListDefinitions()
		}
		toolDefs = a.activeToolDefs(a.cachedToolDefs)
	}()

	// P0-A #5: MaxTokens override for context overflow recovery
	maxTokens := mp.MaxOutputTokens
	if opts.MaxTokens > 0 {
		maxTokens = opts.MaxTokens
	}

	req := &llm.Request{
		Model:       model,
		Messages:    messages,
		Tools:       toolDefs,
		Temperature: mp.Temperature,
		TopP:        mp.TopP,
		MaxTokens:   maxTokens,
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
				// Must drain remaining chunks to prevent goroutine leak
				go func() {
					for range ch {
					}
				}()
				return nil, provName, chunk.Error
			}
		}
	}

	// Flush buffered text to SSE client.
	// Always flush regardless of StopReason to ensure SSE output matches
	// what gets persisted to DB (resp.Content is stored unconditionally).
	if resp != nil {
		slog.Info(fmt.Sprintf("[loop] StopReason=%q textLen=%d toolCalls=%d", resp.StopReason, textBuf.Len(), len(resp.ToolCalls)))
	}

	if genErr != nil {
		return nil, provName, genErr
	}

	// Record precise token usage with per-model cost tracking (P0-A #1+#3)
	if resp != nil {
		model := a.deps.LLMRouter.CurrentModel()
		policy := llm.GetPolicy(model)
		a.tokenTracker.RecordAPIUsageWithCost(resp.Usage, model, policy)

		// Evo: feed token counts and model to trace collector for RL training data
		if a.traceCollector != nil {
			a.traceCollector.RecordTokens(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
			a.traceCollector.SetModel(model)
			// Capture LLM reasoning to attach to the next tool call
			if resp.Reasoning != "" {
				a.traceCollector.RecordReasoning(resp.Reasoning)
			}
		}

		// P3 J3: Structured API telemetry
		if llm.GlobalTelemetry != nil {
			providerName := ""
			if provider != nil {
				providerName = provider.Name()
			}
			errStr := ""
			if genErr != nil {
				errStr = genErr.Error()
			}
			llm.GlobalTelemetry.Record(llm.TelemetryEvent{
				Timestamp:   time.Now(),
				SessionID:   a.SessionID(),
				Model:       model,
				Provider:    providerName,
				PromptTok:   resp.Usage.PromptTokens,
				CompleteTok: resp.Usage.CompletionTokens,
				CachedTok:   resp.Usage.CachedTokens,
				Error:       errStr,
			})
		}
	}

	return resp, provName, nil
}

// buildPromptDeps populates ALL 11 fields from injected stores.
func (a *AgentLoop) buildPromptDeps(ctx context.Context, model string, opts RunOptions) prompt.Deps {
	deps := prompt.Deps{
		Mode:        opts.Mode,
		ToolDescs:   a.buildToolDescs(),
		TokenBudget: llm.GetPolicy(model).ContextWindow,
		Runtime:     a.buildRuntimeInfo(model),
		CurrentStep: a.task.CurrentStep,
	}

	// UserRules — from config discovery or workspace
	if a.deps.Workspace != nil {
		deps.ProjectContext = a.deps.Workspace.ReadContextWithIncludes() // P3 L1: @include recursion
	}

	// UserRules — global (~/.ngoagent/user_rules.md)
	if a.deps.PromptEngine != nil {
		rules, _ := a.deps.PromptEngine.DiscoverUserRules(
			ctxutil.WorkspaceDirFromContext(ctx),
		)
		// P3 L4: compress customInstructions to reduce system prompt bloat
		deps.UserRules = workspace.CompressCustomInstructions(rules)
		slog.Info(fmt.Sprintf("[prompt] UserRules loaded: %d bytes (profile: %s)", len(deps.UserRules), a.deps.PromptEngine.ActiveProfile()))
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
				Content:     s.Content,
				WhenToUse:   s.WhenToUse,
				Context:     s.Context,
				Args:        s.Args,
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

	// SubAgent v2: Thread agent definition into prompt deps
	if opts.AgentDef != nil {
		deps.AgentType = opts.AgentDef.AgentType
		deps.AgentSystemPrompt = opts.AgentDef.SystemPrompt
	}

	return deps
}

// buildRuntimeInfo, buildGitSnapshot, estimateTokens, estimateStringTokens,
// countEffectiveSteps, activeToolDefs → moved to run_helpers.go
//
// persistHistory, persistFullHistory, persistTranscript → moved to persistence_ops.go
//
// fireHooks, runEvoEval, pushEvo → moved to evo_controller.go
