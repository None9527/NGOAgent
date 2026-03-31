package service

import (
	"sync"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// ═══════════════════════════════════════════
// TokenTracker — hybrid precise+estimated token tracking
//   with per-model usage accumulation and USD cost
// ═══════════════════════════════════════════

// ModelUsage tracks cumulative token usage for a single model.
type ModelUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Calls            int     `json:"calls"`
	CostUSD          float64 `json:"cost_usd"`
}

// TokenStats is the structured output for API exposure.
type TokenStats struct {
	TotalPromptTokens     int                   `json:"total_prompt_tokens"`
	TotalCompletionTokens int                   `json:"total_completion_tokens"`
	TotalCostUSD          float64               `json:"total_cost_usd"`
	TotalCalls            int                   `json:"total_calls"`
	ByModel               map[string]ModelUsage `json:"by_model"`
}

// TokenTracker combines API-reported precise token counts with
// character-based estimates for content added since the last API call.
// Also tracks cumulative per-model usage and USD cost.
type TokenTracker struct {
	mu                  sync.Mutex
	lastAPIPromptTokens int // Last precise prompt_tokens from API response
	deltaEstimate       int // Estimated tokens added since last API call
	hasAPIData          bool
	systemPromptTokens  int // Actual system prompt token count (set by doGenerate)

	// Per-model cumulative tracking (P0-A #1 + #3)
	byModel map[string]*ModelUsage
}

// RecordAPIUsage records the precise token usage returned by the LLM API.
// Called after each doGenerate completes successfully.
func (t *TokenTracker) RecordAPIUsage(usage llm.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if usage.PromptTokens > 0 {
		t.lastAPIPromptTokens = usage.PromptTokens
		t.deltaEstimate = 0 // Reset delta since we have a fresh baseline
		t.hasAPIData = true
	}
}

// RecordAPIUsageWithCost records usage and accumulates per-model cost.
// model is the model name used for this call.
// policy provides pricing info (PriceInput1K, PriceOutput1K).
func (t *TokenTracker) RecordAPIUsageWithCost(usage llm.Usage, model string, policy llm.ModelPolicy) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Baseline tracking
	if usage.PromptTokens > 0 {
		t.lastAPIPromptTokens = usage.PromptTokens
		t.deltaEstimate = 0
		t.hasAPIData = true
	}

	// Per-model accumulation
	if t.byModel == nil {
		t.byModel = make(map[string]*ModelUsage)
	}
	mu, ok := t.byModel[model]
	if !ok {
		mu = &ModelUsage{}
		t.byModel[model] = mu
	}
	mu.PromptTokens += usage.PromptTokens
	mu.CompletionTokens += usage.CompletionTokens
	mu.TotalTokens += usage.TotalTokens
	mu.Calls++

	// USD cost calculation
	inputCost := float64(usage.PromptTokens) / 1000.0 * policy.PriceInput1K
	outputCost := float64(usage.CompletionTokens) / 1000.0 * policy.PriceOutput1K
	mu.CostUSD += inputCost + outputCost
}

// AddEstimate adds an estimated token count for new content (e.g. tool result).
// Uses the same heuristic as ephemeral estimation.
func (t *TokenTracker) AddEstimate(text string) {
	tokens := estimateStringTokensHybrid(text)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deltaEstimate += tokens
}

// CurrentEstimate returns the best current estimate of total prompt tokens.
// If API data is available, returns API baseline + delta (±5% error).
// Otherwise, falls through to 0 (caller should use legacy estimateTokens).
func (t *TokenTracker) CurrentEstimate() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasAPIData {
		return 0, false
	}
	return t.lastAPIPromptTokens + t.deltaEstimate, true
}

// Stats returns a structured summary of all accumulated usage and cost.
func (t *TokenTracker) Stats() TokenStats {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats := TokenStats{
		ByModel: make(map[string]ModelUsage),
	}
	for model, mu := range t.byModel {
		stats.ByModel[model] = *mu
		stats.TotalPromptTokens += mu.PromptTokens
		stats.TotalCompletionTokens += mu.CompletionTokens
		stats.TotalCostUSD += mu.CostUSD
		stats.TotalCalls += mu.Calls
	}
	return stats
}

// Reset clears prompt estimation state. Called after compaction restructures history.
// NOTE: cumulative usage (byModel) is intentionally NOT reset — it tracks session lifetime.
func (t *TokenTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastAPIPromptTokens = 0
	t.deltaEstimate = 0
	t.hasAPIData = false
	// systemPromptTokens intentionally preserved — prompt structure doesn't change on compact
	// byModel intentionally preserved — cumulative cost tracking
}

// SetSystemPromptSize records the actual system prompt token count.
// Called by doGenerate after assembling the prompt.
func (t *TokenTracker) SetSystemPromptSize(tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.systemPromptTokens = tokens
}

// SystemPromptTokens returns the recorded system prompt size, or 3000 as fallback.
func (t *TokenTracker) SystemPromptTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.systemPromptTokens > 0 {
		return t.systemPromptTokens
	}
	return 3000 // fallback for first call before doGenerate runs
}

// estimateStringTokensHybrid estimates tokens for a string.
// CJK characters ≈ 1.5 tokens, ASCII ≈ 0.25 tokens per char.
func estimateStringTokensHybrid(s string) int {
	tokens := 0.0
	for _, r := range s {
		if r >= 0x2E80 {
			tokens += 1.5
		} else {
			tokens += 0.25
		}
	}
	if tokens < 1 {
		return 1
	}
	return int(tokens)
}
