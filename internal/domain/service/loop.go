// Package engine implements the core agent loop (ReAct pattern),
// state machine, Delta streaming protocol, and context management.
package service

import (
	"context"
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
	OnToolStart(name string, args map[string]any)
	OnToolResult(name string, output string, err error)
	OnProgress(taskName, status, summary, mode string)
	OnApprovalRequest(approvalID, toolName string, args map[string]any, reason string)
	OnComplete()
	OnError(err error)
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
	Brain     *brain.ArtifactStore
	KIStore   *knowledge.Store
	Workspace *workspace.Store
	SkillMgr  *skill.Manager
}

// AgentLoop manages a single agent conversation loop.
type AgentLoop struct {
	deps       Deps
	history    []llm.Message
	state      State
	mu         sync.Mutex
	runMu      sync.Mutex // Backpressure: prevents concurrent Run() (Anti's BUSY state)
	stopCh     chan struct{}
	options    RunOptions
	guard      *BehaviorGuard
	ephemerals []string // Pending ephemeral messages for next LLM call

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
}

// RunOptions configure a single run.
type RunOptions struct {
	Mode  string // chat / heartbeat / forge
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
		deps:   deps,
		state:  StateIdle,
		stopCh: make(chan struct{}),
		guard:  NewBehaviorGuard(agentCfg),
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
func (a *AgentLoop) SetHistory(msgs []llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = msgs
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
}

// Compact triggers context compaction on the current history.
func (a *AgentLoop) Compact() {
	a.doCompact()
}

// Stop signals the loop to terminate.
func (a *AgentLoop) Stop() {
	close(a.stopCh)
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
	// Deterministic force: plan.md → must call notify_user next
	if ps.ForceNextTool != "" {
		a.guard.SetForceToolName(ps.ForceNextTool)
	}
}
