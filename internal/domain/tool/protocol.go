// Package tool defines the domain-level tool interface and protocol types.
//
// This is the centralized protocol layer for agent↔tool communication.
// All signal types, terminal step configuration, and dispatch logic live here,
// mirroring Antigravity's CascadeExecutorConfig.terminal_step_types pattern.
package tool

// ─── Signal Enum ─────────────────────────────────────────────

// Signal classifies the side-effect a tool result carries.
type Signal int

const (
	SignalNone     Signal = iota // No special side-effect
	SignalProgress               // State update (task_boundary)
	SignalYield                  // Yield control to user (notify_user)
)

// ─── Terminal Step Configuration (declarative, like Anti) ─────

// TerminalSignals declares which signals terminate the agent loop.
// Mirrors Anti's CascadeExecutorConfig.terminal_step_types.
// To add a new terminal signal, just add it here — no switch/case edits needed.
var TerminalSignals = map[Signal]bool{
	SignalYield: true,
}

// IsTerminal returns true if this signal should stop the agent loop.
func (s Signal) IsTerminal() bool {
	return TerminalSignals[s]
}

// ─── ToolResult ──────────────────────────────────────────────

// ToolResult is the structured return type from tool execution.
type ToolResult struct {
	Output  string         // Text returned to the LLM
	Signal  Signal         // Protocol signal for the agent loop
	Payload map[string]any // Signal-specific data
}

// ─── DeltaSink / LoopState (interfaces for dispatch) ──────────

// DeltaSink is the subset of delta methods the dispatcher needs.
type DeltaSink interface {
	OnProgress(taskName, status, summary, mode string)
	OnText(text string)
}

// LoopState is a mutable bag of state the dispatcher writes back.
// The agent loop provides a concrete implementation.
type LoopState struct {
	PreviousMode     string
	BoundaryTaskName string
	BoundaryMode     string
	BoundaryStatus   string
	BoundarySummary  string
	StepsSinceUpdate int
	YieldRequested   bool
	ForceNextTool    string // Force next LLM call to use this tool (via tool_choice)
}

// ─── Signal Handlers ─────────────────────────────────────────

// SignalHandler processes a specific signal type.
type SignalHandler func(result ToolResult, sink DeltaSink, state *LoopState)

// handlers maps each signal to its dispatch logic.
// Add new signal behavior here — one place, one registration.
var handlers = map[Signal]SignalHandler{
	SignalProgress: handleProgress,
	SignalYield:    handleYield,
}

func handleProgress(result ToolResult, sink DeltaSink, state *LoopState) {
	taskName, _ := result.Payload["task_name"].(string)
	status, _ := result.Payload["status"].(string)
	summary, _ := result.Payload["summary"].(string)
	mode, _ := result.Payload["mode"].(string)

	sink.OnProgress(taskName, status, summary, mode)

	state.PreviousMode = state.BoundaryMode
	state.BoundaryTaskName = taskName
	state.BoundaryMode = mode
	state.BoundaryStatus = status
	state.BoundarySummary = summary
	state.StepsSinceUpdate = 0

	// Force next tool: deterministic plan→notify_user enforcement
	if force, ok := result.Payload["force_next_tool"].(string); ok && force != "" {
		state.ForceNextTool = force
	}
}

func handleYield(result ToolResult, sink DeltaSink, state *LoopState) {
	if msg, ok := result.Payload["message"].(string); ok {
		sink.OnText(msg)
	}
	state.YieldRequested = true
}

// ─── Dispatcher ──────────────────────────────────────────────

// Dispatch processes the signal in a ToolResult.
// Call from the agent loop after every tool execution — one line replaces
// the scattered switch/case that was previously in run.go.
func Dispatch(result ToolResult, sink DeltaSink, state *LoopState) {
	if h, ok := handlers[result.Signal]; ok {
		h(result, sink, state)
	}
}

// ─── Result Helpers ──────────────────────────────────────────

func TextResult(output string) (ToolResult, error) {
	return ToolResult{Output: output}, nil
}

func ErrorResult(msg string) (ToolResult, error) {
	return ToolResult{Output: msg}, nil
}

func ProgressResult(output string, payload map[string]any) (ToolResult, error) {
	return ToolResult{Output: output, Signal: SignalProgress, Payload: payload}, nil
}

func YieldResult(output string, payload map[string]any) (ToolResult, error) {
	return ToolResult{Output: output, Signal: SignalYield, Payload: payload}, nil
}

