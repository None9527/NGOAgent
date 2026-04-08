package service

import (
	"sync"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// ═══════════════════════════════════════════
// Config zero-value resolution tests
// ═══════════════════════════════════════════

func TestConfigResolveModelParams_Defaults(t *testing.T) {
	cfg := &config.Config{}
	mp := cfg.ResolveModelParams("unknown-model")

	if mp.Temperature != 0.7 {
		t.Fatalf("expected temp=0.7, got %f", mp.Temperature)
	}
	if mp.TopP != 0.9 {
		t.Fatalf("expected top_p=0.9, got %f", mp.TopP)
	}
	if mp.MaxOutputTokens != 8192 {
		t.Fatalf("expected max_output_tokens=8192, got %d", mp.MaxOutputTokens)
	}
	if mp.ContextWindow != 32768 {
		t.Fatalf("expected context_window=32768, got %d", mp.ContextWindow)
	}
	if mp.CompactRatio != 0.7 {
		t.Fatalf("expected compact_ratio=0.7, got %f", mp.CompactRatio)
	}
	t.Log("✅ Config: default values applied for unknown model")
}

func TestConfigResolveModelParams_PointerOverride(t *testing.T) {
	// P3-1: *float64 must distinguish 0.0 from unset
	temp := 0.0 // Explicit zero temperature (deterministic)
	topP := 0.5
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Providers: []config.ProviderDef{{
				Name:   "test",
				Models: []string{"test-model"},
				ModelConfig: map[string]config.ModelOverride{
					"test-model": {
						Temperature: &temp,
						TopP:        &topP,
					},
				},
			}},
		},
	}

	mp := cfg.ResolveModelParams("test-model")
	if mp.Temperature != 0.0 {
		t.Fatalf("expected temp=0.0 (explicit), got %f", mp.Temperature)
	}
	if mp.TopP != 0.5 {
		t.Fatalf("expected top_p=0.5, got %f", mp.TopP)
	}
	t.Log("✅ Config: *float64 override distinguishes 0.0 from unset")
}

func TestConfigResolveModelParams_NilPointerKeepsDefault(t *testing.T) {
	// nil pointer (unset) should keep the agent-level default
	cfg := &config.Config{
		Agent: config.AgentConfig{Temperature: 0.3, TopP: 0.8},
		LLM: config.LLMConfig{
			Providers: []config.ProviderDef{{
				Name:   "test",
				Models: []string{"test-model"},
				ModelConfig: map[string]config.ModelOverride{
					"test-model": {
						// Temperature and TopP are nil — should NOT override
						ContextWindow: 128000,
					},
				},
			}},
		},
	}

	mp := cfg.ResolveModelParams("test-model")
	if mp.Temperature != 0.3 {
		t.Fatalf("expected temp=0.3 (agent default), got %f", mp.Temperature)
	}
	if mp.TopP != 0.8 {
		t.Fatalf("expected top_p=0.8 (agent default), got %f", mp.TopP)
	}
	if mp.ContextWindow != 128000 {
		t.Fatalf("expected context=128000 (override), got %d", mp.ContextWindow)
	}
	t.Log("✅ Config: nil pointer preserves agent-level defaults")
}

// ═══════════════════════════════════════════
// ToolMeta AccessLevel tests
// ═══════════════════════════════════════════

func TestToolMetaAccessLevels(t *testing.T) {
	readOnlyTools := []string{"read_file", "glob", "grep_search", "command_status", "git_status", "tree"}
	for _, name := range readOnlyTools {
		meta := dtool.DefaultMeta(name)
		if meta.Access != dtool.AccessReadOnly {
			t.Fatalf("%s: expected AccessReadOnly, got %d", name, meta.Access)
		}
	}

	writeTools := []string{"write_file", "edit_file", "task_plan"}
	for _, name := range writeTools {
		meta := dtool.DefaultMeta(name)
		if meta.Access != dtool.AccessWrite {
			t.Fatalf("%s: expected AccessWrite, got %d", name, meta.Access)
		}
	}

	destructiveTools := []string{"run_command"}
	for _, name := range destructiveTools {
		meta := dtool.DefaultMeta(name)
		if meta.Access != dtool.AccessDestructive {
			t.Fatalf("%s: expected AccessDestructive, got %d", name, meta.Access)
		}
	}

	// Unknown tools should get conservative defaults
	meta := dtool.DefaultMeta("unknown_tool_xyz")
	if meta.Access != dtool.AccessWrite {
		t.Fatalf("unknown tool: expected AccessWrite (conservative), got %d", meta.Access)
	}

	t.Log("✅ ToolMeta: AccessLevel classification correct for all tiers")
}

// ═══════════════════════════════════════════
// Barrier concurrent finalization tests
// ═══════════════════════════════════════════

func TestBarrierConcurrentFinalization(t *testing.T) {
	// Verify: multiple goroutines calling OnComplete simultaneously
	// should result in exactly 1 wakeup callback
	wakeCh := make(chan struct{}, 5) // buffered to catch duplicates

	wakeup := func() {
		wakeCh <- struct{}{}
	}

	loop := &AgentLoop{} // Minimal loop for InjectEphemeral
	b := NewSubagentBarrier(loop, wakeup)
	b.SetMaxConcurrent(10)

	// Add 3 subagents
	for i := 0; i < 3; i++ {
		_ = b.Add(string(rune('a'+i)), "task")
	}

	// Simulate 3 completions arriving simultaneously
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			b.OnComplete(string(rune('a'+id)), "result", nil)
		}(i)
	}
	wg.Wait()

	// Wait for the async wakeup goroutine
	<-wakeCh

	// Check that no duplicate wakeups fire
	select {
	case <-wakeCh:
		t.Fatal("received duplicate wakeup — finalized flag not working")
	default:
		// Good: only 1 wakeup
	}

	t.Log("✅ Barrier: concurrent finalization handled correctly")
}

func TestBarrierSnapshotReflectsMembers(t *testing.T) {
	loop := &AgentLoop{}
	b := NewSubagentBarrier(loop, nil)
	if err := b.Add("run-a", "task a"); err != nil {
		t.Fatalf("Add run-a error: %v", err)
	}
	if err := b.Add("run-b", "task b"); err != nil {
		t.Fatalf("Add run-b error: %v", err)
	}
	b.OnComplete("run-a", "done", nil)

	snap := b.Snapshot()
	if snap.TotalCount != 2 {
		t.Fatalf("unexpected total count: %d", snap.TotalCount)
	}
	if snap.PendingCount != 1 {
		t.Fatalf("unexpected pending count: %d", snap.PendingCount)
	}
	if snap.CompletedCount != 1 {
		t.Fatalf("unexpected completed count: %d", snap.CompletedCount)
	}
	if snap.Finalized {
		t.Fatal("barrier should not be finalized yet")
	}
	if len(snap.Members) != 2 {
		t.Fatalf("unexpected members: %#v", snap.Members)
	}
	if snap.Members[0].RunID == "" && snap.Members[1].RunID == "" {
		t.Fatalf("expected member run ids in snapshot: %#v", snap.Members)
	}
}

func TestNewSubagentBarrierFromState_RestoresMembers(t *testing.T) {
	loop := &AgentLoop{}
	state := graphruntime.BarrierState{
		ID:             "barrier-restore",
		TotalCount:     2,
		PendingCount:   1,
		CompletedCount: 1,
		Members: []graphruntime.BarrierMemberState{
			{
				RunID:    "run-a",
				TaskName: "task a",
				Status:   "completed",
				Output:   "done",
			},
			{
				RunID:    "run-b",
				TaskName: "task b",
				Status:   "failed",
				Error:    "boom",
			},
		},
	}

	restored := NewSubagentBarrierFromState(loop, nil, state)
	snap := restored.Snapshot()

	if snap.ID != state.ID {
		t.Fatalf("unexpected barrier id: %s", snap.ID)
	}
	if snap.PendingCount != 1 {
		t.Fatalf("unexpected pending count: %d", snap.PendingCount)
	}
	if len(snap.Members) != 2 {
		t.Fatalf("unexpected members after restore: %#v", snap.Members)
	}
	if snap.Members[1].Error != "boom" {
		t.Fatalf("expected restored error, got %#v", snap.Members[1])
	}
}

// ═══════════════════════════════════════════
// Guard thread safety (P0-1 verification)
// ═══════════════════════════════════════════

func TestGuardConcurrentAccess(t *testing.T) {
	g := NewBehaviorGuard(&config.AgentConfig{MaxSteps: 100})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(step int) {
			defer wg.Done()
			g.Check("response text", step, step)
			g.PreToolCheck("read_file")
			g.PostToolRecord("read_file")
		}(i)
	}
	wg.Wait()

	// If we get here without panic/race, the mutex is working
	t.Log("✅ Guard: concurrent access safe (no data race)")
}
