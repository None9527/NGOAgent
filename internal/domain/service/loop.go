// Package engine implements the core agent loop (ReAct pattern),
// state machine, Delta streaming protocol, and context management.
package service

import (
	"context"
	"log"
	"path/filepath"
	"sync"

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
	Hooks        *PostRunHookChain
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
	options        RunOptions
	guard          *BehaviorGuard
	ephemerals     []string // Pending ephemeral messages for next LLM call

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

	// Artifact staleness tracking (Anti-style: steps since last interaction)
	artifactLastStep map[string]int // artifact name → last step that touched it
	currentStep      int            // global step counter across tool calls
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
	return &AgentLoop{
		deps:            deps,
		state:           StateIdle,
		stopCh:          make(chan struct{}),
		guard:           NewBehaviorGuard(agentCfg),
		artifactLastStep: make(map[string]int),
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

	// Model context budget: default 128K, use 70%
	budget := 128000 * 70 / 100 // ~89K tokens
	if tokenEst > budget {
		log.Printf("[compact] Auto-compacting on resume: %d tokens > %d budget (%d messages)",
			tokenEst, budget, msgCount)
		a.doCompact()
	}
}

// Stop signals the loop to terminate the current run.
// The stopCh is recreated automatically when the next Run() begins.
func (a *AgentLoop) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.stopCh:
		// Already closed
	default:
		close(a.stopCh)
	}
}

// protoState snapshots the loop's boundary fields into a dtool.LoopState
// for the centralized protocol dispatcher.
func (a *AgentLoop) protoState() *dtool.LoopState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &dtool.LoopState{
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
	// L2 Progressive Disclosure: capture skill loaded signal
	if ps.SkillLoaded != "" {
		a.skillLoaded = ps.SkillLoaded
		a.skillPath = ps.SkillPath
	}
	// Deterministic force: plan.md → must call notify_user next
	if ps.ForceNextTool != "" {
		a.guard.SetForceToolName(ps.ForceNextTool)
	}
}
