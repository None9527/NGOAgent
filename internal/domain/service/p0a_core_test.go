package service

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

func TestTokenTrackerCostTracking(t *testing.T) {
	tt := &TokenTracker{}

	policy := llm.ModelPolicy{
		PriceInput1K:  0.003,
		PriceOutput1K: 0.015,
	}

	// Record usage for model A
	tt.RecordAPIUsageWithCost(llm.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	}, "gpt-4", policy)

	// Record again for model A
	tt.RecordAPIUsageWithCost(llm.Usage{
		PromptTokens:     2000,
		CompletionTokens: 1000,
		TotalTokens:      3000,
	}, "gpt-4", policy)

	// Record for model B
	policyB := llm.ModelPolicy{
		PriceInput1K:  0.001,
		PriceOutput1K: 0.002,
	}
	tt.RecordAPIUsageWithCost(llm.Usage{
		PromptTokens:     5000,
		CompletionTokens: 2000,
		TotalTokens:      7000,
	}, "qwen-max", policyB)

	stats := tt.Stats()

	// Verify per-model tracking
	if len(stats.ByModel) != 2 {
		t.Fatalf("expected 2 models, got %d", len(stats.ByModel))
	}

	gpt4 := stats.ByModel["gpt-4"]
	if gpt4.Calls != 2 {
		t.Errorf("gpt-4 calls: expected 2, got %d", gpt4.Calls)
	}
	if gpt4.PromptTokens != 3000 {
		t.Errorf("gpt-4 prompt tokens: expected 3000, got %d", gpt4.PromptTokens)
	}
	if gpt4.CompletionTokens != 1500 {
		t.Errorf("gpt-4 completion tokens: expected 1500, got %d", gpt4.CompletionTokens)
	}

	// Cost: (3000/1000)*0.003 + (1500/1000)*0.015 = 0.009 + 0.0225 = 0.0315
	expectedGPT4Cost := 3.0*0.003 + 1.5*0.015
	if abs(gpt4.CostUSD-expectedGPT4Cost) > 0.0001 {
		t.Errorf("gpt-4 cost: expected %.4f, got %.4f", expectedGPT4Cost, gpt4.CostUSD)
	}

	qwen := stats.ByModel["qwen-max"]
	if qwen.Calls != 1 {
		t.Errorf("qwen-max calls: expected 1, got %d", qwen.Calls)
	}

	// Total
	if stats.TotalCalls != 3 {
		t.Errorf("total calls: expected 3, got %d", stats.TotalCalls)
	}
	if stats.TotalPromptTokens != 8000 {
		t.Errorf("total prompt: expected 8000, got %d", stats.TotalPromptTokens)
	}
	if stats.TotalCostUSD <= 0 {
		t.Errorf("total cost should be positive, got %.4f", stats.TotalCostUSD)
	}

	// Reset should preserve byModel
	tt.Reset()
	statsAfter := tt.Stats()
	if statsAfter.TotalCalls != 3 {
		t.Errorf("after reset, total calls should be preserved: expected 3, got %d", statsAfter.TotalCalls)
	}
}

func TestToolResultBudget(t *testing.T) {
	tests := []struct {
		tool     string
		expected int
	}{
		{"web_fetch", 100 * 1024},
		{"read_file", 80 * 1024},
		{"run_command", 60 * 1024},
		{"save_memory", 10 * 1024},
		{"unknown_tool", 50 * 1024}, // default
	}
	for _, tc := range tests {
		got := toolResultBudget(tc.tool)
		if got != tc.expected {
			t.Errorf("toolResultBudget(%q): expected %d, got %d", tc.tool, tc.expected, got)
		}
	}
}

func TestErrorLevelUserMessage(t *testing.T) {
	levels := []llm.ErrorLevel{
		llm.ErrorTransient,
		llm.ErrorOverload,
		llm.ErrorContextOverflow,
		llm.ErrorRecoverable,
		llm.ErrorBilling,
		llm.ErrorFatal,
	}
	for _, l := range levels {
		msg := l.UserMessage()
		if msg == "" {
			t.Errorf("ErrorLevel(%d).UserMessage() returned empty string", l)
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
