package service

import (
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// BehaviorGuard enforces safety rules on agent responses and tool calls.
// Operates at two levels:
//   - Turn level: Check() — runs after each LLM response (4 original rules)
//   - Step level: PreToolCheck()/PostToolRecord() — runs per tool call (3 planning rules)
type BehaviorGuard struct {
	cfg           *config.AgentConfig
	lastResponses []string // Track last N responses for repetition detection
	toolCallCount int
	stepCount     int
	highCount     int // Consecutive high-severity violations

	// Step-level tracking (new — mirrors Anti's pre/post_tool hooks)
	turnToolNames    []string // All tool names called this turn
	hasBoundary      bool     // This turn has called task_boundary
	hasNotify        bool     // This turn has called notify_user
	codeModInPlan    int      // write/edit calls while isPlanning=true
	isPlanning       bool     // Sync'd from doPrepare each turn
	planExists       bool     // Sync'd from doPrepare each turn
	taskMdExists     bool     // Sync'd from doPrepare each turn
	currentMode      string   // Sync'd from doPrepare (planning/execution/verification)
	forceToolName    string   // Non-empty → force this tool on next LLM call
	stepsSinceBoundary int    // Tool calls since last task_boundary (for ephemeral gating)
}

// GuardVerdict is the result of a guard check.
type GuardVerdict struct {
	Action  string // "pass" / "warn" / "terminate"
	Rule    string // Which rule triggered
	Message string // Ephemeral message to inject
}

// NewBehaviorGuard creates a guard with config-driven limits.
func NewBehaviorGuard(cfg *config.AgentConfig) *BehaviorGuard {
	return &BehaviorGuard{
		cfg:           cfg,
		lastResponses: make([]string, 0, 5),
	}
}

// ═══════════════════════════════════════════
// Turn-level: Check (runs after each LLM response)
// ═══════════════════════════════════════════

// Check evaluates 4 guard rules against the latest response.
func (g *BehaviorGuard) Check(response string, toolCalls int, step int) GuardVerdict {
	g.stepCount = step
	g.toolCallCount += toolCalls

	maxSteps := 200
	if g.cfg != nil && g.cfg.MaxSteps > 0 {
		maxSteps = g.cfg.MaxSteps
	}

	// Rule 1: empty_response (High)
	if strings.TrimSpace(response) == "" && toolCalls == 0 {
		g.highCount++
		if g.highCount >= 3 {
			return GuardVerdict{Action: "terminate", Rule: "empty_response_escalated",
				Message: "Agent produced 3 consecutive empty responses. Terminating."}
		}
		return GuardVerdict{Action: "warn", Rule: "empty_response",
			Message: "Your last response was empty. Please provide a meaningful response or use a tool."}
	}

	// Rule 2: repetition_loop — 3 consecutive identical responses (Critical)
	if response != "" {
		g.lastResponses = append(g.lastResponses, response)
		if len(g.lastResponses) > 5 {
			g.lastResponses = g.lastResponses[1:]
		}
		if len(g.lastResponses) >= 3 {
			last3 := g.lastResponses[len(g.lastResponses)-3:]
			if last3[0] == last3[1] && last3[1] == last3[2] {
				return GuardVerdict{Action: "terminate", Rule: "repetition_loop",
					Message: "Agent is in a repetition loop (3 identical responses). Terminating."}
			}
		}
	}

	// Rule 3: step_limit — MAX_INVOCATIONS equivalent (Critical, only safety valve)
	if g.stepCount > maxSteps {
		return GuardVerdict{Action: "terminate", Rule: "step_limit",
			Message: fmt.Sprintf("Agent exceeded %d steps. Terminating to prevent runaway.", maxSteps)}
	}

	// Reset high count on clean pass
	if response != "" {
		g.highCount = 0
	}

	return GuardVerdict{Action: "pass"}
}

// ═══════════════════════════════════════════
// Step-level: Pre/Post tool hooks (new)
// ═══════════════════════════════════════════

// SetModeState is called by doPrepare to sync planning/mode context.
func (g *BehaviorGuard) SetModeState(isPlanning, planExists, taskMdExists bool, mode string) {
	g.isPlanning = isPlanning
	g.planExists = planExists
	g.taskMdExists = taskMdExists
	g.currentMode = mode
}

// PreToolCheck runs before each tool execution (step-level guard).
// Returns nil if no issue, or a verdict with an ephemeral to inject.
func (g *BehaviorGuard) PreToolCheck(toolName string) *GuardVerdict {
	g.turnToolNames = append(g.turnToolNames, toolName)

	// Rule 6: Planning + no plan.md + code modification → warn
	if g.isPlanning && !g.planExists &&
		(toolName == "write_file" || toolName == "edit_file") {
		g.codeModInPlan++
		if g.codeModInPlan >= 2 {
			return &GuardVerdict{Action: "warn", Rule: "planning_code_modify",
				Message: "你在 planning 模式下直接修改了代码。请先创建 plan.md 并调用 notify_user 等待审批。"}
		}
	}

	// Rule 8: Execution + no task.md + code modification → warn
	if g.currentMode == "execution" && !g.taskMdExists &&
		(toolName == "write_file" || toolName == "edit_file") {
		return &GuardVerdict{Action: "warn", Rule: "execution_no_task_md",
			Message: "You are modifying code in execution mode but task.md doesn't exist. Create it first via task_plan(action=create, type=task)."}
	}

	// Rule 7: notify_user(blocked=true) was called but agent continues calling tools
	if g.hasNotify && toolName != "task_boundary" {
		return &GuardVerdict{Action: "warn", Rule: "post_notify_continue",
			Message: "你已调用 notify_user(blocked_on_user=true)，不应继续执行工具。请停止。"}
	}

	return nil
}

// PostToolRecord runs after each tool execution to track protocol compliance.
func (g *BehaviorGuard) PostToolRecord(toolName string) {
	g.stepsSinceBoundary++
	switch toolName {
	case "task_boundary":
		g.hasBoundary = true
		g.forceToolName = "" // reset force once boundary is called
		g.stepsSinceBoundary = 0
	case "notify_user":
		g.hasNotify = true
	}
}

// StepsSinceBoundary returns steps since last task_boundary call.
// Used by doPrepare for ephemeral injection gating (Anti's num_steps pattern).
func (g *BehaviorGuard) StepsSinceBoundary() int {
	return g.stepsSinceBoundary
}

// ConsumeForceToolName returns and clears any pending force_tool_name.
// Called by doGenerate to pass tool_choice to the LLM API.
func (g *BehaviorGuard) ConsumeForceToolName() string {
	name := g.forceToolName
	g.forceToolName = ""
	return name
}

// SetForceToolName sets the force tool for the next LLM call.
// Used by the plan→notify_user deterministic enforcement chain.
func (g *BehaviorGuard) SetForceToolName(name string) {
	g.forceToolName = name
}

// ResetTurn resets per-turn counters (call at start of each Run).
func (g *BehaviorGuard) ResetTurn() {
	g.toolCallCount = 0
	g.turnToolNames = g.turnToolNames[:0]
	g.hasBoundary = false
	g.hasNotify = false
	g.codeModInPlan = 0
	g.lastResponses = g.lastResponses[:0]
	g.highCount = 0
}
