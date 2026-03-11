package service

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// ═══════════════════════════════════════════
// Item 1: Config-driven max_steps (only safety valve)
// ═══════════════════════════════════════════

func TestGuardConfigDrivenLimits(t *testing.T) {
	cfg := &config.AgentConfig{MaxSteps: 50}
	g := NewBehaviorGuard(cfg)

	// Should not trigger at step 49
	v := g.Check("response", 5, 49)
	if v.Action != "pass" {
		t.Fatalf("step 49: expected pass, got %s (%s)", v.Action, v.Rule)
	}

	// Should trigger at step 51 (maxSteps=50)
	v = g.Check("response", 0, 51)
	if v.Action != "terminate" || v.Rule != "step_limit" {
		t.Fatalf("step 51: expected terminate/step_limit, got %s/%s", v.Action, v.Rule)
	}

	t.Log("✅ Guard: config-driven MaxSteps=50 enforced")
}

func TestGuardNilConfig(t *testing.T) {
	// nil config should use defaults (200 max_steps)
	g := NewBehaviorGuard(nil)
	v := g.Check("ok", 100, 100)
	if v.Action != "pass" {
		t.Fatalf("nil config, 100 tools, step 100: expected pass, got %s (%s)", v.Action, v.Rule)
	}

	t.Log("✅ Guard: nil config falls back to hardcoded defaults")
}

// ═══════════════════════════════════════════
// Step-level planning rules
// ═══════════════════════════════════════════

func TestGuardPreToolCheck_PlanningCodeModify(t *testing.T) {
	g := NewBehaviorGuard(&config.AgentConfig{})
	g.SetPlanningState(true, false) // no plan.md

	// Mark boundary so tracking works
	g.PostToolRecord("task_boundary")

	// First code mod: should pass (threshold is 2)
	v := g.PreToolCheck("write_file")
	if v != nil {
		t.Fatal("first write_file: should pass")
	}

	// Second code mod: should warn
	v = g.PreToolCheck("edit_file")
	if v == nil || v.Rule != "planning_code_modify" {
		t.Fatal("second edit: expected planning_code_modify warning")
	}

	// With planExists=true, no warning
	g2 := NewBehaviorGuard(&config.AgentConfig{})
	g2.SetPlanningState(true, true) // plan.md exists
	g2.PostToolRecord("task_boundary")
	v = g2.PreToolCheck("write_file")
	if v != nil {
		t.Fatal("with plan.md: write should pass")
	}
	v = g2.PreToolCheck("edit_file")
	if v != nil {
		t.Fatal("with plan.md: edit should pass")
	}

	t.Log("✅ Guard: planning_code_modify rule")
}

func TestGuardPreToolCheck_PostNotifyContinue(t *testing.T) {
	g := NewBehaviorGuard(&config.AgentConfig{})
	g.PostToolRecord("task_boundary")
	g.PostToolRecord("notify_user")

	// After notify_user, any non-task_boundary tool should warn
	v := g.PreToolCheck("read_file")
	if v == nil || v.Rule != "post_notify_continue" {
		t.Fatal("post-notify read_file: expected post_notify_continue warning")
	}

	// task_boundary itself should pass
	v = g.PreToolCheck("task_boundary")
	if v != nil {
		t.Fatal("post-notify task_boundary: should pass")
	}

	t.Log("✅ Guard: post_notify_continue rule")
}

func TestGuardPostToolRecord(t *testing.T) {
	g := NewBehaviorGuard(nil)

	// Initially no boundary
	if g.hasBoundary || g.hasNotify {
		t.Fatal("initial state should be false")
	}

	g.PostToolRecord("task_boundary")
	if !g.hasBoundary {
		t.Fatal("hasBoundary should be true after task_boundary")
	}

	g.PostToolRecord("notify_user")
	if !g.hasNotify {
		t.Fatal("hasNotify should be true after notify_user")
	}

	t.Log("✅ Guard: PostToolRecord tracking")
}

func TestGuardResetTurn(t *testing.T) {
	g := NewBehaviorGuard(nil)
	g.SetPlanningState(true, false)
	g.PostToolRecord("task_boundary")
	g.PostToolRecord("notify_user")
	g.PreToolCheck("write_file")
	g.PreToolCheck("edit_file")

	g.ResetTurn()

	if g.hasBoundary || g.hasNotify || g.codeModInPlan != 0 || len(g.turnToolNames) != 0 {
		t.Fatal("ResetTurn should clear all per-turn state")
	}

	t.Log("✅ Guard: ResetTurn clears all counters")
}

// ═══════════════════════════════════════════
// Original turn-level rules
// ═══════════════════════════════════════════

func TestGuardEmptyResponse(t *testing.T) {
	g := NewBehaviorGuard(nil)
	v := g.Check("", 0, 1)
	if v.Action != "warn" || v.Rule != "empty_response" {
		t.Fatalf("empty response: expected warn, got %s/%s", v.Action, v.Rule)
	}

	// 3 consecutive → terminate
	g.Check("", 0, 2)
	v = g.Check("", 0, 3)
	if v.Action != "terminate" {
		t.Fatal("3x empty: expected terminate")
	}

	t.Log("✅ Guard: Rule 1 empty_response + escalation")
}

func TestGuardRepetitionLoop(t *testing.T) {
	g := NewBehaviorGuard(nil)
	g.Check("same text", 1, 1)
	g.Check("same text", 1, 2)
	v := g.Check("same text", 1, 3)
	if v.Action != "terminate" || v.Rule != "repetition_loop" {
		t.Fatalf("repetition: expected terminate, got %s/%s", v.Action, v.Rule)
	}

	t.Log("✅ Guard: Rule 2 repetition_loop")
}
