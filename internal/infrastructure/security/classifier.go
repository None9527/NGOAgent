package security

// ═══════════════════════════════════════════
// P3 K1: AI Safety Classifier — Strategy Pattern
// ═══════════════════════════════════════════
//
// Three interchangeable classifiers share a single Classifier interface.
// Selection is controlled by SecurityConfig.ClassifierMode:
//
//   - "pattern"  (default) — pure regex/glob matching, zero latency, zero cost
//   - "llm"                — LLM-based contextual analysis, highest accuracy
//   - "hybrid"             — pattern first, LLM escalation on borderline cases
//
// The Classifier only classifies run_command tool calls.
// All other tools use the existing normalDecide/blocklist chain.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// ClassifierDecision is the verdict from a Classifier.
type ClassifierDecision int

const (
	ClassifierAllow     ClassifierDecision = iota // Clearly safe
	ClassifierAsk                                 // Borderline — ask user
	ClassifierDeny                                // Clearly dangerous
	ClassifierUncertain                           // Uncertain — escalate (hybrid only)
)

// ClassifierResult carries the verdict and explanation.
type ClassifierResult struct {
	Decision   ClassifierDecision
	Reason     string
	Confidence float64 // 0.0-1.0; <0.7 triggers hybrid escalation
	ElapsedMs  int64
}

// Classifier is the strategy interface for run_command security classification.
type Classifier interface {
	Classify(ctx context.Context, command string, args map[string]any) ClassifierResult
	Name() string
}

// ═══════════════════════════════════════════
// Strategy 1: PatternClassifier (default)
// ═══════════════════════════════════════════

// PatternClassifier uses the existing regex/glob blocklist matching.
// Zero latency, zero API cost. Handles the vast majority of cases.
type PatternClassifier struct {
	hook *Hook // shared hook for matchBlockList
}

func NewPatternClassifier(hook *Hook) *PatternClassifier {
	return &PatternClassifier{hook: hook}
}

func (c *PatternClassifier) Name() string { return "pattern" }

func (c *PatternClassifier) Classify(_ context.Context, _ string, args map[string]any) ClassifierResult {
	reason, blocked := c.hook.matchBlockList("run_command", args)
	if blocked {
		return ClassifierResult{
			Decision:   ClassifierAsk,
			Reason:     reason,
			Confidence: 0.95,
		}
	}
	return ClassifierResult{
		Decision:   ClassifierAllow,
		Reason:     "pattern: no blocklist match",
		Confidence: 0.90,
	}
}

// ═══════════════════════════════════════════
// Strategy 2: LLMClassifier
// ═══════════════════════════════════════════

// LLMClassifier uses a small, fast LLM (Haiku/Flash) to contextually classify
// run_command safety. Falls back to PatternClassifier on provider error.
type LLMClassifier struct {
	provider llm.Provider
	model    string
	fallback *PatternClassifier
}

func NewLLMClassifier(provider llm.Provider, model string, hook *Hook) *LLMClassifier {
	return &LLMClassifier{
		provider: provider,
		model:    model,
		fallback: NewPatternClassifier(hook),
	}
}

func (c *LLMClassifier) Name() string { return "llm" }

const llmClassifierSystemPrompt = `You are a strict security classifier for shell commands in an AI coding agent.
Classify the following shell command as SAFE, ASK, or DANGEROUS.

Rules:
- SAFE: read-only commands, file edits in /tmp or /home, standard build/test/install tools (go build, npm install, pytest, etc.)
- ASK: any command that modifies system-wide state, network exfiltration, privilege escalation attempts, suspicious patterns
- DANGEROUS: rm -rf /, curl | bash, eval with untrusted input, /etc/passwd writes, SSH key manipulation

Respond with ONLY one line in this exact format:
VERDICT: <SAFE|ASK|DANGEROUS>
CONFIDENCE: <0.0-1.0>
REASON: <one sentence>`

func (c *LLMClassifier) Classify(ctx context.Context, command string, args map[string]any) ClassifierResult {
	if c.provider == nil {
		return c.fallback.Classify(ctx, command, args)
	}

	t0 := time.Now()

	req := &llm.Request{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "system", Content: llmClassifierSystemPrompt},
			{Role: "user", Content: fmt.Sprintf("Classify this shell command:\n```\n%s\n```", command)},
		},
		Temperature: 0.05, // Near-deterministic
		MaxTokens:   80,
	}

	ch := make(chan llm.StreamChunk, 16)
	resp, err := c.provider.GenerateStream(ctx, req, ch)
	// Drain the stream
	for range ch {
	}

	elapsed := time.Since(t0).Milliseconds()

	if err != nil || resp == nil {
		slog.Info(fmt.Sprintf("[classifier/llm] provider error: %v — falling back to pattern", err))
		r := c.fallback.Classify(ctx, command, args)
		r.ElapsedMs = elapsed
		return r
	}

	return parseLLMClassifierResponse(resp.Content, elapsed)
}

// parseLLMClassifierResponse parses the structured LLM response.
func parseLLMClassifierResponse(raw string, elapsedMs int64) ClassifierResult {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	result := ClassifierResult{
		Decision:   ClassifierAsk, // default: cautious
		Confidence: 0.5,
		ElapsedMs:  elapsedMs,
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "VERDICT:") {
			verdict := strings.TrimSpace(strings.TrimPrefix(line, "VERDICT:"))
			switch strings.ToUpper(verdict) {
			case "SAFE":
				result.Decision = ClassifierAllow
			case "DANGEROUS":
				result.Decision = ClassifierDeny
			default:
				result.Decision = ClassifierAsk
			}
		} else if strings.HasPrefix(line, "CONFIDENCE:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "CONFIDENCE:"))
			var conf float64
			fmt.Sscanf(raw, "%f", &conf)
			result.Confidence = conf
		} else if strings.HasPrefix(line, "REASON:") {
			result.Reason = strings.TrimSpace(strings.TrimPrefix(line, "REASON:"))
		}
	}

	return result
}

// ═══════════════════════════════════════════
// Strategy 3: HybridClassifier
// ═══════════════════════════════════════════

// HybridClassifier runs PatternClassifier first. If confidence < 0.75 (borderline),
// escalates to LLMClassifier for contextual analysis.
// Combines speed (pattern) with accuracy (LLM) at minimal cost.
type HybridClassifier struct {
	pattern   *PatternClassifier
	llmCls    *LLMClassifier
	llmThresh float64 // confidence below this triggers LLM escalation (default 0.75)
}

func NewHybridClassifier(hook *Hook, provider llm.Provider, model string) *HybridClassifier {
	return &HybridClassifier{
		pattern:   NewPatternClassifier(hook),
		llmCls:    NewLLMClassifier(provider, model, hook),
		llmThresh: 0.75,
	}
}

func (c *HybridClassifier) Name() string { return "hybrid" }

func (c *HybridClassifier) Classify(ctx context.Context, command string, args map[string]any) ClassifierResult {
	// Phase 1: fast pattern check
	r := c.pattern.Classify(ctx, command, args)

	// High-confidence pattern verdict → return immediately
	if r.Confidence >= c.llmThresh {
		r.Reason = "[pattern] " + r.Reason
		return r
	}

	// Phase 2: LLM escalation for low-confidence cases
	slog.Info(fmt.Sprintf("[classifier/hybrid] escalating to LLM (pattern conf=%.2f): %s", r.Confidence, command))
	llmR := c.llmCls.Classify(ctx, command, args)
	llmR.Reason = fmt.Sprintf("[hybrid→llm] %s (pattern: %s)", llmR.Reason, r.Reason)
	return llmR
}

// ═══════════════════════════════════════════
// Factory: NewClassifier
// ═══════════════════════════════════════════

// NewClassifier creates the correct strategy based on mode config.
// mode: "pattern" | "llm" | "hybrid"
// provider: nil → forces PatternClassifier even if mode=llm/hybrid
func NewClassifier(mode string, hook *Hook, provider llm.Provider, model string) Classifier {
	switch mode {
	case "llm":
		if provider == nil {
			slog.Info("[classifier] mode=llm but no provider — falling back to pattern")
			return NewPatternClassifier(hook)
		}
		return NewLLMClassifier(provider, model, hook)
	case "hybrid":
		if provider == nil {
			slog.Info("[classifier] mode=hybrid but no provider — falling back to pattern")
			return NewPatternClassifier(hook)
		}
		return NewHybridClassifier(hook, provider, model)
	default: // "pattern" or empty
		return NewPatternClassifier(hook)
	}
}
