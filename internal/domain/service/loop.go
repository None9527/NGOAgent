// Package engine implements the core agent loop (ReAct pattern),
// state machine, Delta streaming protocol, and context management.
package service

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
)

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (dtool.ToolResult, error)
	ListDefinitions() []llm.ToolDef
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
	Role       string
	Content    string
	ToolCalls  string // JSON-encoded
	ToolCallID string
	Reasoning  string // Thinking/reasoning content (assistant messages only)
}

// Deps groups all dependencies injected into the AgentLoop.
type Deps struct {
	// Core
	Config       *config.Config
	ConfigMgr    *config.Manager
	LLMRouter    *llm.Router
	PromptEngine *prompt.Engine
	ToolExec     ToolExecutor
	Security     *security.Hook
	Delta        DeltaSink

	// Storage (data sources for prompt assembly)
	Brain       *brain.ArtifactStore
	KIStore     *knowledge.Store
	KIRetriever KISemanticRetriever // Embedding-based KI search (nil = disabled)
	Workspace   *workspace.Store
	SkillMgr    *skill.Manager

	// Persistence + Hooks
	HistoryStore HistoryPersister
	FileHistory  *workspace.FileHistory // File edit history with snapshot rollback
	Hooks        *HookManager
	MemoryStore  MemoryStorer // Vector memory for semantic recall (nil = disabled)
}

// KISemanticRetriever abstracts semantic search over KIs (avoids import cycle with knowledge.Retriever).
type KISemanticRetriever interface {
	RetrieveForPrompt(query string, topK int, budgetChars int) string
}

// AgentLoop manages a single agent conversation loop.
type AgentLoop struct {
	deps           Deps
	history        []llm.Message
	persistedCount int // number of messages already persisted to DB (incremental write baseline)
	state          State
	mu             sync.Mutex
	runMu          sync.Mutex // Backpressure: prevents concurrent Run() (Anti's BUSY state)
	stopCh         chan struct{}
	runCancel      context.CancelFunc // Cancels the running context on Stop()
	options        RunOptions
	guard          *BehaviorGuard
	ephemerals     []string // Pending ephemeral messages for next LLM call
	pendingWake    atomic.Bool // Set by barrier when subagents complete; checked by runInner tail

	// Task boundary state (written by task_boundary tool intercept in doToolExec).
	// Mirrors Anti's latest_task_boundary_step tracking.
	boundaryTaskName string
	boundaryMode     string // "planning" / "execution" / "verification"
	boundaryStatus   string
	boundarySummary  string
	previousMode     string // previous mode, for detecting transitions
	stepsSinceUpdate int    // tool calls since last task_boundary call
	planModified     bool   // true if plan.md was written/updated this session
	yieldRequested   bool   // set true by notify_user(blocked_on_user=true)
	skillLoaded      string // L2: skill name loaded via SKILL.md read (one-shot)
	skillPath        string // L2: skill directory path
	pendingMedia     []map[string]string // Multimodal: media items pending injection into next LLM call

	// Artifact staleness tracking (Anti-style: steps since last interaction)
	artifactLastStep map[string]int // artifact name → last step that touched it
	currentStep      int            // global step counter across tool calls

	// Runtime plan mode: "plan" | "auto" — set via API, NOT persisted to config.yaml.
	// Default from config.Agent.PlanningMode on startup.
	runtimePlanMode string
}

// RunOptions configure a single run.
type RunOptions struct {
	Mode  string // chat / forge
	Model string
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
	// Derive initial plan mode from config (bool → string)
	initPlanMode := "auto"
	if deps.Config != nil && deps.Config.Agent.PlanningMode {
		initPlanMode = "plan"
	}
	return &AgentLoop{
		deps:            deps,
		state:           StateIdle,
		stopCh:          make(chan struct{}),
		guard:           NewBehaviorGuard(agentCfg),
		artifactLastStep: make(map[string]int),
		runtimePlanMode: initPlanMode,
		options: RunOptions{
			Mode: "chat",
		},
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
		return "", fmt.Errorf("no previous user message to retry")
	}
	lastUser := a.history[len(a.history)-1].Content
	a.history = a.history[:len(a.history)-1]
	// Update persisted count to avoid re-persisting stripped messages
	if a.persistedCount > len(a.history) {
		a.persistedCount = len(a.history)
	}
	return lastUser, nil
}

// CurrentState returns the current state machine state.
func (a *AgentLoop) CurrentState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// SetDelta replaces the DeltaSink (used by server to inject per-request SSE sink).
func (a *AgentLoop) SetDelta(d DeltaSink) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deps.Delta = d
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

// Compact triggers context compaction on the current history.
func (a *AgentLoop) Compact() {
	a.doCompact()
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
		log.Printf("[compact] Auto-compacting on resume: %d tokens > %d budget (%d messages)",
			tokenEst, budget, msgCount)
		a.doCompact()
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

// SetPlanMode sets the runtime planning mode ("plan" or "auto").
// This is an in-memory toggle; it does NOT write to config.yaml.
func (a *AgentLoop) SetPlanMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if mode == "plan" || mode == "auto" {
		a.runtimePlanMode = mode
	}
}

// PlanMode returns the current runtime planning mode.
func (a *AgentLoop) PlanMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtimePlanMode == "" {
		return "auto"
	}
	return a.runtimePlanMode
}

// protoState snapshots the loop's boundary fields into a dtool.LoopState
// for the centralized protocol dispatcher.
func (a *AgentLoop) protoState() *dtool.LoopState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &dtool.LoopState{
		PlanMode:         a.runtimePlanMode,
		PreviousMode:     a.previousMode,
		BoundaryTaskName: a.boundaryTaskName,
		BoundaryMode:     a.boundaryMode,
		BoundaryStatus:   a.boundaryStatus,
		BoundarySummary:  a.boundarySummary,
		StepsSinceUpdate: a.stepsSinceUpdate,
		YieldRequested:   a.yieldRequested,
	}
}

// syncLoopState writes back the protocol dispatcher's mutations to private fields.
func (a *AgentLoop) syncLoopState(ps *dtool.LoopState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.previousMode = ps.PreviousMode
	a.boundaryTaskName = ps.BoundaryTaskName
	a.boundaryMode = ps.BoundaryMode
	a.boundaryStatus = ps.BoundaryStatus
	a.boundarySummary = ps.BoundarySummary
	a.stepsSinceUpdate = ps.StepsSinceUpdate
	a.yieldRequested = ps.YieldRequested
	// Agentic self-review: inject pending ephemerals from protocol dispatcher
	for _, eph := range ps.PendingEphemerals {
		a.ephemerals = append(a.ephemerals, eph)
	}
	ps.PendingEphemerals = nil
	// L2 Progressive Disclosure: capture skill loaded signal
	if ps.SkillLoaded != "" {
		a.skillLoaded = ps.SkillLoaded
		a.skillPath = ps.SkillPath
	}
	// Deterministic force: plan.md → must call notify_user next
	// Skip in agentic mode — agent self-reviews, no forced yield
	if ps.ForceNextTool != "" && ps.PlanMode != "agentic" {
		a.guard.SetForceToolName(ps.ForceNextTool)
	}
	// Multimodal: transfer pending media from protocol dispatcher
	if len(ps.PendingMedia) > 0 {
		a.pendingMedia = append(a.pendingMedia, ps.PendingMedia...)
		ps.PendingMedia = nil
	}
}
