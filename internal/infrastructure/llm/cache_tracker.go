package llm

import (
	"hash/fnv"
	"sync"
)

// CacheTracker monitors system prompt stability to estimate cache hit rates.
// LLM providers with prompt caching benefit from stable system prompts.
// If the prompt hash changes frequently, caching is ineffective.
type CacheTracker struct {
	mu          sync.Mutex
	lastHash    uint64
	totalCalls  int
	cacheBreaks int // hash changed between consecutive calls
}

// CacheStats holds the cache tracking statistics.
type CacheStats struct {
	TotalCalls  int     `json:"total_calls"`
	CacheBreaks int     `json:"cache_breaks"`
	HitRate     float64 `json:"hit_rate"` // 0.0-1.0
}

// RecordCall records a system prompt hash. If it differs from the previous
// call, it counts as a cache break.
func (ct *CacheTracker) RecordCall(systemPromptHash uint64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.totalCalls++
	if ct.lastHash != 0 && ct.lastHash != systemPromptHash {
		ct.cacheBreaks++
	}
	ct.lastHash = systemPromptHash
}

// Stats returns a snapshot of cache tracking metrics.
func (ct *CacheTracker) Stats() CacheStats {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	s := CacheStats{
		TotalCalls:  ct.totalCalls,
		CacheBreaks: ct.cacheBreaks,
	}
	if ct.totalCalls > 1 {
		s.HitRate = 1.0 - float64(ct.cacheBreaks)/float64(ct.totalCalls-1)
		if s.HitRate < 0 {
			s.HitRate = 0
		}
	}
	return s
}

// HashString computes a FNV-1a hash for a string (fast, non-crypto).
func HashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
