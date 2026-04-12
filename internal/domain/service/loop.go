// Package service implements the graph-backed agent loop,
// Delta streaming protocol, and context management.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error)
	ListDefinitions() []llm.ToolDef
	Generation() int64 // Monotonic counter: changes when tools are added/removed
}

// DeltaSink receives streaming events from the engine.
type DeltaSink interface {
	OnText(text string)
	OnReasoning(text string)
	OnToolStart(callID string, name string, args map[string]any)
	OnToolResult(callID string, name string, output string, err error)
	OnProgress(taskName, status, summary, mode string)
	OnPlanReview(message string, paths []string)
	OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string)
	OnTitleUpdate(sessionID, title string)
	OnAutoWakeStart() // Subagent results arrived, parent auto-continuing
	OnComplete()
	OnError(err error)
	Emit(event DeltaEvent) // Generic event emitter for extensibility (evo, etc.)
}

// HistoryPersister saves and loads conversation history to/from durable storage.
type HistoryPersister interface {
	SaveAll(sessionID string, msgs []HistoryExport) error
	AppendAll(sessionID string, msgs []HistoryExport) error
	LoadAll(sessionID string) ([]HistoryExport, error)
	DeleteSession(sessionID string) error
}

// HistoryExport is a serializable snapshot of a single message for persistence.
type HistoryExport struct {
	Role        string
	Content     string
	ToolCalls   string // JSON-encoded
	ToolCallID  string
	Reasoning   string // Thinking/reasoning content (assistant messages only)
	Attachments string // B2: JSON-encoded [{type,path,mime_type,name}] multimodal references
}

// RestoreHistory converts persisted HistoryExport records back to llm.Message slice.
// Centralizes ToolCalls/Attachments JSON deserialization and multimodal ContentParts rebuild.
// All callers (ChatStream, RetryRun, ChatEngine.Chat) MUST use this instead of inline conversion.
func RestoreHistory(exports []HistoryExport) []llm.Message {
	msgs := make([]llm.Message, len(exports))
	for i, e := range exports {
		msgs[i] = llm.Message{
			Role:       e.Role,
			Content:    e.Content,
			ToolCallID: e.ToolCallID,
			Reasoning:  e.Reasoning,
		}
		if e.ToolCalls != "" {
			if err := json.Unmarshal([]byte(e.ToolCalls), &msgs[i].ToolCalls); err != nil {
				slog.Info(fmt.Sprintf("[history] WARN: unmarshal ToolCalls msg %d: %v", i, err))
			}
		}
		if e.Attachments != "" {
			if err := json.Unmarshal([]byte(e.Attachments), &msgs[i].Attachments); err != nil {
				slog.Info(fmt.Sprintf("[history] WARN: unmarshal Attachments msg %d: %v", i, err))
			}
		}
		RebuildContentParts(&msgs[i])
	}
	return msgs
}

// Deps groups all dependencies injected into the AgentLoop.
type Deps struct {
	// Core (still concrete — too entangled for clean interface extraction)
	Config       *config.Config
	LLMRouter    ModelRouter // was: *llm.Router
	PromptEngine *prompt.Engine
	ToolExec     ToolExecutor
	Security     SecurityChecker // was: *security.Hook
	Delta        DeltaSink

	// Storage (data sources for prompt assembly) — abstracted via ports.go interfaces
	Brain       ArtifactReader      // was: *brain.ArtifactStore
	KIStore     KIIndexer           // was: *knowledge.Store
	KIRetriever KISemanticRetriever // Embedding-based KI search (nil = disabled)
	Workspace   WorkspaceReader     // was: *workspace.Store
	SkillMgr    SkillLister         // was: *skill.Manager

	// Persistence + Hooks
	HistoryStore  HistoryPersister
	FileHistory   FileEditTracker // was: *workspace.FileHistory
	Hooks         *HookManager
	MemoryStore   MemoryStorer // Vector memory for semantic recall (nil = disabled)
	SnapshotStore graphruntime.SnapshotStore

	// Evo Mode
	EvoEvaluator    *EvoEvaluator                               // Quality evaluation engine (nil = disabled)
	EvoRepairRouter *RepairRouter                               // Repair strategy router (nil = disabled)
	EvoStore        *persistence.EvoStore                       // Evo persistence (nil = disabled)
	EventPusher     func(sessionID, eventType string, data any) // WS push for async events (evo, subagent)

	// P3 M1: Outbound webhook notifications (nil = disabled)
	WebhookHook WebhookNotifyHook
}

// WebhookNotifyHook is implemented by notify.Hook to receive agent lifecycle events.
// Using an interface here avoids an import cycle between domain/service and infrastructure/notify.
type WebhookNotifyHook interface {
	OnComplete(sessionID string)
	OnError(sessionID string, err error)
	OnToolResult(sessionID, toolName, output string, err error)
	OnProgress(sessionID, taskName, status, summary string)
}

// KISemanticRetriever abstracts semantic search over KIs (avoids import cycle with knowledge.Retriever).
type KISemanticRetriever interface {
	RetrieveForPrompt(query string, topK int, budgetChars int) string
}

// SetEventPusher injects the WS push function for async events (evo, etc.).
// Called from main.go after Server is created to break the build-time dependency cycle.
func (a *AgentLoop) SetEventPusher(fn func(string, string, any)) {
	a.deps.EventPusher = fn
}

// AgentLoop manages a single agent conversation loop.
type AgentLoop struct {
	deps Deps

	// ── Lifecycle & Synchronization ──
	mu        sync.Mutex // Protects: history, ephemerals, task, evo state, historyDirty
	runMu     sync.Mutex // Backpressure: prevents concurrent Run()
	stopCh    chan struct{}
	runCancel context.CancelFunc // Cancels the running context on Stop()
	options   RunOptions
	guard     *BehaviorGuard

	// ── History & Persistence ──
	history        []llm.Message
	persistedCount int         // Messages already persisted to DB (incremental write baseline)
	ephemerals     []string    // Pending ephemeral messages for next LLM call
	pendingWake    atomic.Bool // Set by barrier when subagents complete

	// ── Context Tracking (per-run, protected by mu) ──
	task                *TaskTracker        // Task state (step count, artifacts, plan status)
	pendingMedia        []map[string]string // Multimodal media items pending injection
	historyDirty        bool                // True after compact/truncate → triggers sanitize
	cachedToolDefs      []llm.ToolDef       // Tool definitions cache (invalidated by generation counter)
	cachedToolDefsGen   int64               // Registry generation when cache was built
	tokenTracker        TokenTracker        // Hybrid API+estimate token tracking
	compactCount        int                 // Consecutive compaction count (guards summary loss)
	outputContinuations int                 // Auto-continue count when LLM output is truncated
	cacheTracker        llm.CacheTracker    // System prompt hash stability tracking

	// ── Phase Detection & Dream ──
	phaseDetector *PhaseDetector
	dream         *DreamTask
	barrier       *SubagentBarrier

	// ── Evo State (per-run, reset on new user message) ──
	intelligence   graphruntime.IntelligenceState
	orchestration  graphruntime.OrchestrationState
	traceCollector *TraceCollectorHook

	// ── Runtime Config ──
	mode ModePermissions // Execution mode permissions — set via API, NOT persisted

	// ── Compression Protection ──
	activeSkills map[string]string // skill name → content (re-injected after compact)
}

// RunOptions configure a single run.
type RunOptions struct {
	Mode      string // chat / evo / subagent
	Model     string
	MaxTokens int                    // P0-A #5: overridable max_tokens for context overflow recovery (0 = use model default)
	AgentType string                 // SubAgent v2: agent type identifier (e.g. "researcher", "code-reviewer")
	AgentDef  *model.AgentDefinition // SubAgent v2: full agent definition (nil for parent/chat loops)
}

// NewAgentLoop creates an agent loop with injected dependencies.
func NewAgentLoop(deps Deps) *AgentLoop {
	var agentCfg *config.AgentConfig
	if deps.Config != nil {
		agentCfg = &deps.Config.Agent
	}
	// Wire Resolution Pipeline: connect workspace root to brain store
	if deps.Brain != nil && deps.Workspace != nil {
		deps.Brain.SetWorkspaceDir(deps.Workspace.WorkDir())
	}
	// Derive initial mode from config
	initMode := ModeFromString("auto", false)
	if deps.Config != nil && deps.Config.Agent.PlanningMode {
		initMode = ModeFromString("plan", false)
	}
	a := &AgentLoop{
		deps:          deps,
		stopCh:        make(chan struct{}),
		guard:         NewBehaviorGuard(agentCfg),
		task:          NewTaskTracker(),
		mode:          initMode,
		phaseDetector: &PhaseDetector{},
		dream:         NewDreamTask(30 * time.Second),
		options: RunOptions{
			Mode: "chat",
		},
	}
	// Per-loop trace collector: isolates evo trace data per session
	if deps.EvoStore != nil {
		a.traceCollector = NewTraceCollectorHook(deps.EvoStore)
	}
	return a
}

// ReloadConfig hot-swaps the loop's config references (called by agent subscriber).
func (a *AgentLoop) ReloadConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	agentCfg := cfg.Agent
	a.guard.UpdateConfig(&agentCfg)
	a.deps.Config = cfg
	// Refresh execution mode if toggled via config (preserve evo layer)
	evoWas := a.mode.EvoEnabled
	if cfg.Agent.PlanningMode {
		a.mode = ModeFromString("plan", evoWas)
	} else {
		a.mode = ModeFromString("auto", evoWas)
	}
}

// SessionID returns the session UUID extracted from the brain directory path.
func (a *AgentLoop) SessionID() string {
	if a.deps.Brain != nil {
		return filepath.Base(a.deps.Brain.BaseDir())
	}
	return ""
}

// SetHistory replaces the conversation history (for session restore).
// Sets persistedCount to len(msgs) since these are all from DB.
func (a *AgentLoop) SetHistory(msgs []llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = msgs
	a.persistedCount = len(msgs)
	a.historyDirty = true // loaded history may need sanitization
}

// AppendMessage adds a message to the history.
func (a *AgentLoop) AppendMessage(msg llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = append(a.history, msg)
}

// GetHistory returns a copy of the current history.
func (a *AgentLoop) GetHistory() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := make([]llm.Message, len(a.history))
	copy(h, a.history)
	return h
}

// History is an alias for GetHistory used by the context builder.
func (a *AgentLoop) History() []llm.Message {
	return a.GetHistory()
}

// SetModel overrides the default model for the next LLM call.
// Used by SubAgent v2 model routing (agentDef.Model override).
func (a *AgentLoop) SetModel(model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.options.Model = model
}

// StripLastTurn removes the last assistant turn (tool + assistant messages)
// and extracts the last user message content. Used for retry: the frontend
// re-sends the returned text through the normal ChatStream flow.
func (a *AgentLoop) StripLastTurn() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Strip trailing assistant/tool messages
	for len(a.history) > 0 {
		last := a.history[len(a.history)-1]
		if last.Role == "user" {
			break
		}
		a.history = a.history[:len(a.history)-1]
	}
	// Extract last user message
	if len(a.history) == 0 {
		return "", agenterr.NewValidation("retry", "no previous user message to retry")
	}
	lastUser := a.history[len(a.history)-1].Content
	a.history = a.history[:len(a.history)-1]
	// Update persisted count to avoid re-persisting stripped messages
	if a.persistedCount > len(a.history) {
		a.persistedCount = len(a.history)
	}
	return lastUser, nil
}

// SetDelta replaces the DeltaSink (used by server to inject per-request SSE sink).
func (a *AgentLoop) SetDelta(d DeltaSink) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deps.Delta = d
}

func (a *AgentLoop) SetActiveBarrier(b *SubagentBarrier) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.barrier = b
}

func (a *AgentLoop) ClearActiveBarrier() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.barrier = nil
}

// InjectEphemeral adds an ephemeral message to the next LLM call.
func (a *AgentLoop) InjectEphemeral(msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ephemerals = append(a.ephemerals, msg)
}

// SignalWake sets the pendingWake flag. The currently-running runInner will
// check this flag at its tail and auto-continue to process injected ephemerals.
// If no run is active, the caller should also try Run() as a fallback.
func (a *AgentLoop) SignalWake() {
	a.pendingWake.Store(true)
}

// ClearHistory resets the conversation history.
func (a *AgentLoop) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = nil
	a.persistedCount = 0
}

// GetTokenStats returns cumulative per-model token usage and USD cost (P0-A #1).
func (a *AgentLoop) GetTokenStats() TokenStats {
	return a.tokenTracker.Stats()
}

// GetCacheStats returns system prompt cache-break tracking metrics (P2 E2).
func (a *AgentLoop) GetCacheStats() llm.CacheStats {
	return a.cacheTracker.Stats()
}

// Compact triggers context compaction on the current history.
func (a *AgentLoop) Compact() {
	a.doCompact(context.Background())
}

// CompactIfNeeded estimates history token count and auto-compacts if over budget.
// Called on session resume to prevent full-history overload.
// Budget = 70% of model context window (reserves 30% for system prompt + response).
func (a *AgentLoop) CompactIfNeeded() {
	a.mu.Lock()
	tokenEst := 0
	for _, m := range a.history {
		tokenEst += len(m.Content) / 4
		tokenEst += len(m.Reasoning) / 4
	}
	msgCount := len(a.history)
	a.mu.Unlock()

	// Skip if history is short (< 20 messages or < 4K tokens)
	if msgCount < 20 || tokenEst < 4000 {
		return
	}

	// Model context budget: resolved per-model (model_config > agent > fallback)
	mp := a.deps.Config.ResolveModelParams(a.deps.LLMRouter.CurrentModel())
	budget := int(float64(mp.ContextWindow) * mp.CompactRatio)
	if tokenEst > budget {
		slog.Info(fmt.Sprintf("[compact] Auto-compacting on resume: %d tokens > %d budget (%d messages)",
			tokenEst, budget, msgCount))
		a.doCompact(context.Background()) // resume compaction uses background context
	}
}

// Stop signals the loop to terminate the current run.
// Cancels the running context (kills sandbox processes) and closes stopCh.
// The stopCh is recreated automatically when the next Run() begins.
func (a *AgentLoop) Stop() {
	a.mu.Lock()
	// Cancel running context first — propagates to sandbox.Run/RunBackground
	if a.runCancel != nil {
		a.runCancel()
	}
	select {
	case <-a.stopCh:
		// Already closed
	default:
		close(a.stopCh)
	}
	a.mu.Unlock()

	// Wait for the agent loop to completely exit by acquiring and releasing the run lock.
	// This prevents a race condition where the frontend immediately sends a new request
	// while the stopping loop is still doing teardown (e.g., persisting history).
	func() {
		a.runMu.Lock()
		defer a.runMu.Unlock()
		// intentional fence: wait for run loop to fully exit before returning
	}()
}

// SetMode sets the execution mode from an API string.
// Supports: "auto", "plan", "agentic", "evo" (backward compat → auto+evo).
// This is an in-memory toggle; it does NOT write to config.yaml.
func (a *AgentLoop) SetMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = ModeFromString(mode, false)
}

// SetPlanMode is a backward-compatible alias for SetMode.
func (a *AgentLoop) SetPlanMode(mode string) { a.SetMode(mode) }

// Mode returns the current execution mode permissions.
func (a *AgentLoop) Mode() ModePermissions {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

// PlanMode returns the mode name string (backward compat).
func (a *AgentLoop) PlanMode() string {
	return a.Mode().String()
}

// protoState creates a LoopState with a shared BoundaryState pointer.
// No copy needed — protocol handlers write directly to a.task.BoundaryState.
func (a *AgentLoop) protoState() *dtool.LoopState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &dtool.LoopState{
		AutoApprove: a.mode.AutoApprove,
		SelfReview:  a.mode.SelfReview,
		Boundary:    &a.task.BoundaryState,
	}
}

// syncLoopState writes back the protocol dispatcher's non-boundary mutations.
// Boundary fields are already shared by pointer — no copy needed.
func (a *AgentLoop) syncLoopState(ps *dtool.LoopState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Agentic self-review: inject pending ephemerals from protocol dispatcher
	for _, eph := range ps.PendingEphemerals {
		a.ephemerals = append(a.ephemerals, eph)
	}
	ps.PendingEphemerals = nil
	// L2 Progressive Disclosure: capture skill loaded signal
	if ps.SkillLoaded != "" {
		a.task.SkillLoaded = ps.SkillLoaded
		a.task.SkillPath = ps.SkillPath
	}
	// Compression protection: copy active skills from protocol state
	for name, content := range ps.ActiveSkills {
		if a.activeSkills == nil {
			a.activeSkills = make(map[string]string)
		}
		a.activeSkills[name] = content
	}
	// Deterministic force: plan.md → must call notify_user next
	// Skip in agentic mode — agent self-reviews, no forced yield
	if ps.ForceNextTool != "" && !ps.SelfReview {
		a.guard.SetForceToolName(ps.ForceNextTool)
	}
	// Multimodal: transfer pending media from protocol dispatcher
	if len(ps.PendingMedia) > 0 {
		a.pendingMedia = append(a.pendingMedia, ps.PendingMedia...)
		ps.PendingMedia = nil
	}
}
