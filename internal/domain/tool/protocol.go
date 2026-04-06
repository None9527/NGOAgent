// Package tool defines the domain-level tool interface and protocol types.
//
// This is the centralized protocol layer for agent↔tool communication.
// All signal types, terminal step configuration, and dispatch logic live here,
// defines terminal step types for the agent protocol.
package tool

import (
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
)

// ─── Signal Enum ─────────────────────────────────────────────

// Signal classifies the side-effect a tool result carries.
type Signal int

const (
	SignalNone        Signal = 0 // No special side-effect
	SignalProgress    Signal = 1 // State update (task_boundary)
	SignalYield       Signal = 2 // Yield control to user (notify_user)
	SignalSkillLoaded Signal = 3 // SKILL.md was read by agent (L2 trigger)
	SignalMediaLoaded Signal = 4 // Media content loaded for VLM injection
	SignalSpawnYield  Signal = 5 // Spawn-agent: force yield to wait for auto-wake
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
	OnPlanReview(message string, paths []string)
}

// BoundaryState holds task boundary tracking fields.
// Shared between LoopState (protocol dispatch) and TaskTracker (agent loop).
// By passing a pointer, we eliminate the copy overhead in protoState/syncLoopState.
type BoundaryState struct {
	PreviousMode     string
	BoundaryTaskName string
	BoundaryMode     string
	BoundaryStatus   string
	BoundarySummary  string
	StepsSinceUpdate int
	YieldRequested   bool
}

// LoopState is a mutable bag of state the dispatcher writes back.
// The agent loop provides a concrete implementation.
type LoopState struct {
	AutoApprove       bool                // agentic: skip human approval for tools
	SelfReview        bool                // agentic: self-review plans, don't yield to user
	PendingEphemerals []string            // Ephemeral messages to inject for next LLM turn
	PendingMedia      []map[string]string // Multimodal: media items to inject {"type", "url"/"data", "path", "format"}
	Boundary          *BoundaryState      // Shared pointer — eliminates protoState/syncLoopState copy
	ForceNextTool     string              // Force next LLM call to use this tool (via tool_choice)
	SkillLoaded       string              // L2: skill name just loaded via SKILL.md read
	SkillPath         string              // L2: skill directory path
	ActiveSkills      map[string]string   // Compression protection: skill name → content
}

// ─── Signal Handlers ─────────────────────────────────────────

// SignalHandler processes a specific signal type.
type SignalHandler func(result ToolResult, sink DeltaSink, state *LoopState)

// handlers maps each signal to its dispatch logic.
// Add new signal behavior here — one place, one registration.
var handlers = map[Signal]SignalHandler{
	SignalProgress:    handleProgress,
	SignalYield:       handleYield,
	SignalSkillLoaded: handleSkillLoaded,
	SignalMediaLoaded: handleMediaLoaded,
	SignalSpawnYield:  handleSpawnYield,
}

func handleProgress(result ToolResult, sink DeltaSink, state *LoopState) {
	taskName, _ := result.Payload["task_name"].(string)
	status, _ := result.Payload["status"].(string)
	summary, _ := result.Payload["summary"].(string)
	mode, _ := result.Payload["mode"].(string)

	sink.OnProgress(taskName, status, summary, mode)

	state.Boundary.PreviousMode = state.Boundary.BoundaryMode
	state.Boundary.BoundaryTaskName = taskName
	state.Boundary.BoundaryMode = mode
	state.Boundary.BoundaryStatus = status
	state.Boundary.BoundarySummary = summary
	state.Boundary.StepsSinceUpdate = 0

	// Force next tool: deterministic plan→notify_user enforcement
	if force, ok := result.Payload["force_next_tool"].(string); ok && force != "" {
		state.ForceNextTool = force
	}
}

func handleYield(result ToolResult, sink DeltaSink, state *LoopState) {
	msg, _ := result.Payload["message"].(string)
	paths := extractStringSlice(result.Payload["paths_to_review"])

	// SelfReview mode: agent self-reviews plan, don't stop loop or show banner
	if state.SelfReview {
		state.PendingEphemerals = append(state.PendingEphemerals, prompttext.EphAgenticSelfReview)
		// YieldRequested stays false → loop continues
		return
	}

	// Auto / Plan: normal yield → banner for user approval
	if len(paths) > 0 {
		sink.OnPlanReview(msg, paths)
	} else if msg != "" {
		sink.OnText(msg)
	}
	state.Boundary.YieldRequested = true
}

// extractStringSlice safely converts an any value to []string.
func extractStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		var out []string
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func handleSkillLoaded(result ToolResult, _ DeltaSink, state *LoopState) {
	state.SkillLoaded, _ = result.Payload["skill_name"].(string)
	state.SkillPath, _ = result.Payload["skill_path"].(string)
	// Compression protection: register active skill content
	if content, ok := result.Payload["skill_content"].(string); ok && state.SkillLoaded != "" {
		if state.ActiveSkills == nil {
			state.ActiveSkills = make(map[string]string)
		}
		state.ActiveSkills[state.SkillLoaded] = content
	}
}

func handleMediaLoaded(result ToolResult, _ DeltaSink, state *LoopState) {
	media, ok := result.Payload["media"].([]map[string]string)
	if !ok {
		return
	}
	state.PendingMedia = append(state.PendingMedia, media...)
}

// ─── Dispatcher ──────────────────────────────────────────────

// Dispatch processes the signal in a ToolResult.
// Call from the agent loop after every tool execution — one line replaces
// the scattered switch/case that was previously in run.go.
func Dispatch(result ToolResult, sink DeltaSink, state *LoopState) {
	if h, ok := handlers[result.Signal]; ok {
		h(result, sink, state)
	} else if result.Signal != SignalNone {
		slog.Info(fmt.Sprintf("[protocol] unhandled signal: %d", result.Signal))
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

func SkillLoadedResult(output, skillName, skillPath, skillContent string) (ToolResult, error) {
	return ToolResult{
		Output: output,
		Signal: SignalSkillLoaded,
		Payload: map[string]any{
			"skill_name":    skillName,
			"skill_path":    skillPath,
			"skill_content": skillContent,
		},
	}, nil
}

func MediaLoadedResult(output string, media []map[string]string) (ToolResult, error) {
	return ToolResult{
		Output:  output,
		Signal:  SignalMediaLoaded,
		Payload: map[string]any{"media": media},
	}, nil
}

func SpawnYieldResult(output string) (ToolResult, error) {
	return ToolResult{
		Output: output,
		Signal: SignalSpawnYield,
	}, nil
}

func handleSpawnYield(_ ToolResult, _ DeltaSink, state *LoopState) {
	state.Boundary.YieldRequested = true
}
