package service

import (
	"sort"
	"strings"
)

// ═══════════════════════════════════════════
// Ephemeral Budget System
// ═══════════════════════════════════════════

// maxEphemeralBudget limits total ephemeral injection to prevent
// attention dilution. 400 tokens ≈ 10% of a typical system prompt.
const maxEphemeralBudget = 400

// EphemeralCandidate represents a candidate ephemeral message for injection.
// Priority 0 = critical (always included), 3 = low (dropped first).
// Dimension groups related messages — only the highest-priority candidate per dimension is kept.
type EphemeralCandidate struct {
	Content   string
	Priority  int    // 0=critical, 1=high, 2=normal, 3=low
	Dimension string // e.g. "context", "planning", "skill", "guard", "ki"
	Tokens    int    // estimated token count (set by EstimateTokens if 0)
}

// EstimateTokens sets the token estimate if not already set.
func (c *EphemeralCandidate) EstimateTokens() {
	if c.Tokens > 0 {
		return
	}
	c.Tokens = estimateEphTokens(c.Content)
}

// estimateEphTokens estimates token count for an ephemeral string.
// Uses a simple heuristic: CJK chars ≈ 1.5 tokens, ASCII ≈ 0.25 tokens per char.
func estimateEphTokens(s string) int {
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

// SelectWithBudget selects ephemeral candidates within a token budget.
//
// Algorithm:
//  1. Sort by priority (lower = higher priority)
//  2. For same-dimension candidates, keep only the highest-priority one
//  3. Fill budget greedily in priority order
//  4. Merge candidates with the same dimension into a single message
func SelectWithBudget(candidates []EphemeralCandidate, budget int) []string {
	if len(candidates) == 0 {
		return nil
	}
	if budget <= 0 {
		budget = maxEphemeralBudget
	}

	// Ensure all candidates have token estimates
	for i := range candidates {
		candidates[i].EstimateTokens()
	}

	// Sort by priority (ascending = most important first)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	// Deduplicate by dimension: keep highest-priority (first after sort)
	seen := make(map[string]bool)
	deduplicated := make([]EphemeralCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Dimension != "" {
			if seen[c.Dimension] {
				continue // already have a higher-priority candidate for this dimension
			}
			seen[c.Dimension] = true
		}
		deduplicated = append(deduplicated, c)
	}

	// Fill budget greedily
	var selected []EphemeralCandidate
	remaining := budget
	for _, c := range deduplicated {
		if c.Tokens > remaining && c.Priority > 0 {
			continue // skip non-critical candidates that don't fit
		}
		selected = append(selected, c)
		remaining -= c.Tokens
	}

	// Merge same-dimension candidates into single messages
	dimMessages := make(map[string]*strings.Builder)
	var noDimResults []string

	for _, c := range selected {
		if c.Dimension == "" {
			noDimResults = append(noDimResults, c.Content)
			continue
		}
		if _, ok := dimMessages[c.Dimension]; !ok {
			dimMessages[c.Dimension] = &strings.Builder{}
		}
		b := dimMessages[c.Dimension]
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(c.Content)
	}

	// Build final result: dimension-merged first, then standalone
	result := make([]string, 0, len(dimMessages)+len(noDimResults))
	for _, b := range dimMessages {
		result = append(result, b.String())
	}
	result = append(result, noDimResults...)

	return result
}
