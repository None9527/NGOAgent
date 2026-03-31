package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
)

// RepairStrategy defines the type of repair to attempt.
type RepairStrategy string

const (
	StrategyParamFix RepairStrategy = "param_fix"  // Fix parameters, retry same tool
	StrategyToolSwap RepairStrategy = "tool_swap"  // Use different tool
	StrategyReRoute  RepairStrategy = "re_route"   // Re-interpret user intent
	StrategyIterate  RepairStrategy = "iterate"    // Refine existing output
	StrategyEscalate RepairStrategy = "escalate"   // Give up, ask user
)

// RepairPlan describes the repair action to take.
type RepairPlan struct {
	Strategy    RepairStrategy
	Ephemeral   string // Ephemeral message to inject before re-run
	Description string // Human-readable description
}

// RepairRouter selects a repair strategy based on the evaluation error type.
type RepairRouter struct {
	breaker *CircuitBreaker
	store   *persistence.EvoStore
}

// NewRepairRouter creates a repair router.
func NewRepairRouter(cfg config.EvoConfig, store *persistence.EvoStore) *RepairRouter {
	return &RepairRouter{
		breaker: NewCircuitBreaker(cfg.MaxRetries, time.Duration(cfg.CooldownSeconds)*time.Second),
		store:   store,
	}
}

// Route selects a repair strategy based on the error type from evaluation.
func (r *RepairRouter) Route(eval *EvalResult) RepairPlan {
	switch eval.ErrorType {
	case "param_wrong":
		return RepairPlan{
			Strategy:    StrategyParamFix,
			Description: "Parameters were incorrect. Adjusting and retrying.",
			Ephemeral: fmt.Sprintf(
				"[EVO REPAIR] Previous execution had parameter errors. Issues: %s\n"+
					"Fix the parameters and retry. Do NOT repeat the same mistake.",
				formatIssues(eval.Issues),
			),
		}

	case "tool_wrong":
		return RepairPlan{
			Strategy:    StrategyToolSwap,
			Description: "Wrong tool was used. Switching to correct tool.",
			Ephemeral: fmt.Sprintf(
				"[EVO REPAIR] Previous execution used the wrong tool. Issues: %s\n"+
					"Select the correct tool for this task and retry.",
				formatIssues(eval.Issues),
			),
		}

	case "intent_mismatch":
		return RepairPlan{
			Strategy:    StrategyReRoute,
			Description: "User intent was misunderstood. Re-interpreting request.",
			Ephemeral: fmt.Sprintf(
				"[EVO REPAIR] You misunderstood the user's intent. Issues: %s\n"+
					"Re-read the user request carefully and execute the correct task.",
				formatIssues(eval.Issues),
			),
		}

	case "quality_low":
		return RepairPlan{
			Strategy:    StrategyIterate,
			Description: "Output quality insufficient. Iterating to improve.",
			Ephemeral: fmt.Sprintf(
				"[EVO REPAIR] Output quality was below threshold (score: %.2f). Issues: %s\n"+
					"Improve the output quality based on the feedback. Build on existing work, don't start over.",
				eval.Score, formatIssues(eval.Issues),
			),
		}

	case "capability_gap":
		return RepairPlan{
			Strategy:    StrategyEscalate,
			Description: "Task requires capabilities not available. Escalating to user.",
			Ephemeral: fmt.Sprintf(
				"[EVO ESCALATE] This task requires capabilities you don't have. Issues: %s\n"+
					"Inform the user about the limitation and suggest alternatives.",
				formatIssues(eval.Issues),
			),
		}

	default:
		// Unknown error type — try iteration as default
		return RepairPlan{
			Strategy:    StrategyIterate,
			Description: "Unclassified error. Attempting iterative improvement.",
			Ephemeral: fmt.Sprintf(
				"[EVO REPAIR] Previous execution did not pass quality evaluation (score: %.2f). Issues: %s\n"+
					"Review the issues and improve your output.",
				eval.Score, formatIssues(eval.Issues),
			),
		}
	}
}

// CanRepair checks if a repair attempt is allowed by the circuit breaker.
func (r *RepairRouter) CanRepair(sessionID string) (bool, string) {
	return r.breaker.Allow(sessionID)
}

// RecordRepair records a repair attempt in both the circuit breaker and the store.
func (r *RepairRouter) RecordRepair(sessionID string, evalID uint, plan RepairPlan, success bool, newScore float64, tokensUsed, durationMs int) error {
	r.breaker.Record(sessionID)

	if r.store != nil {
		repair := &persistence.EvoRepair{
			SessionID:  sessionID,
			EvalID:     evalID,
			Strategy:   string(plan.Strategy),
			Success:    success,
			NewScore:   newScore,
			TokensUsed: tokensUsed,
			Duration:   durationMs,
		}
		return r.store.SaveRepair(repair)
	}
	return nil
}

// formatIssues converts EvalIssues into a readable string.
func formatIssues(issues []EvalIssue) string {
	if len(issues) == 0 {
		return "(no specific issues)"
	}
	var result string
	for i, iss := range issues {
		if i > 0 {
			result += "; "
		}
		result += fmt.Sprintf("[%s] %s", iss.Severity, iss.Description)
	}
	return result
}

// ═══════════════════════════════════════════
// CircuitBreaker
// ═══════════════════════════════════════════

// CircuitBreaker prevents infinite repair loops.
type CircuitBreaker struct {
	mu         sync.Mutex
	maxRetries int
	cooldown   time.Duration
	attempts   map[string][]time.Time // sessionID → attempt timestamps
}

// NewCircuitBreaker creates a circuit breaker.
func NewCircuitBreaker(maxRetries int, cooldown time.Duration) *CircuitBreaker {
	if maxRetries <= 0 {
		maxRetries = 2
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{
		maxRetries: maxRetries,
		cooldown:   cooldown,
		attempts:   make(map[string][]time.Time),
	}
}

// Allow checks if a repair attempt is permitted.
// Returns (allowed, reason).
func (cb *CircuitBreaker) Allow(sessionID string) (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	attempts := cb.attempts[sessionID]

	// Check max retries
	if len(attempts) >= cb.maxRetries {
		return false, fmt.Sprintf("max retries reached (%d/%d)", len(attempts), cb.maxRetries)
	}

	// Check cooldown
	if len(attempts) > 0 {
		last := attempts[len(attempts)-1]
		elapsed := time.Since(last)
		if elapsed < cb.cooldown {
			remaining := cb.cooldown - elapsed
			return false, fmt.Sprintf("cooldown active (%.0fs remaining)", remaining.Seconds())
		}
	}

	return true, ""
}

// Record registers a repair attempt.
func (cb *CircuitBreaker) Record(sessionID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.attempts[sessionID] = append(cb.attempts[sessionID], time.Now())
}

// Reset clears attempts for a session (e.g., when user starts a new task).
func (cb *CircuitBreaker) Reset(sessionID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.attempts, sessionID)
}
