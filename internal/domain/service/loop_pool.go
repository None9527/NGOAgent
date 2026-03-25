package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
)

// LoopPool manages per-session AgentLoop instances with concurrency limits.
// Each session gets an independent loop with its own history and brain dir.
// Supports per-user fairness: a single user cannot monopolize all loop slots.
type LoopPool struct {
	mu         sync.RWMutex
	loops      map[string]*managedLoop
	factory    func(sid string) *AgentLoop
	brainDir   string
	maxLoops   int // max concurrent sessions (0 = unlimited)
	perUserMax int // max loops per user (0 = unlimited)
}

// managedLoop wraps AgentLoop with access tracking for LRU eviction.
type managedLoop struct {
	loop       *AgentLoop
	lastAccess time.Time
	userKey    string // extracted user identifier
}

// NewLoopPool creates a loop pool with a factory function for creating new loops.
// maxLoops limits concurrent sessions; 0 means unlimited (dangerous in production).
func NewLoopPool(factory func(sid string) *AgentLoop, brainDir string) *LoopPool {
	return &LoopPool{
		loops:      make(map[string]*managedLoop),
		factory:    factory,
		brainDir:   brainDir,
		maxLoops:   8, // safe default
		perUserMax: 3, // per-user default
	}
}

// SetMaxLoops sets the maximum number of concurrent session loops.
func (p *LoopPool) SetMaxLoops(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxLoops = n
}

// SetPerUserMax sets the maximum loops per user. 0 = unlimited.
func (p *LoopPool) SetPerUserMax(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.perUserMax = n
}

// GetIfExists returns an existing loop without creating a new one.
// Returns nil if the session has no active loop in memory.
func (p *LoopPool) GetIfExists(sid string) *AgentLoop {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if ml, ok := p.loops[sid]; ok {
		return ml.loop
	}
	return nil
}

// Get returns an existing loop or creates a new one for the session.
// Eviction priority: same-user idle loops first, then global LRU.
func (p *LoopPool) Get(sid string) *AgentLoop {
	userKey := ExtractUserKey(sid)

	p.mu.RLock()
	if ml, ok := p.loops[sid]; ok {
		ml.lastAccess = time.Now()
		p.mu.RUnlock()
		return ml.loop
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check after write lock
	if ml, ok := p.loops[sid]; ok {
		ml.lastAccess = time.Now()
		return ml.loop
	}

	// Per-user limit: evict same-user oldest idle if over limit
	if p.perUserMax > 0 {
		userCount := p.countUserLocked(userKey)
		if userCount >= p.perUserMax {
			p.evictUserOldestLocked(userKey)
		}
	}

	// Global limit: evict if at capacity
	if p.maxLoops > 0 && len(p.loops) >= p.maxLoops {
		p.evictOldestLocked("")
	}

	loop := p.factory(sid)
	// Set session-scoped brain
	brainStore := brain.NewArtifactStore(p.brainDir, sid)
	// BUG-11 fix: propagate workspaceDir so Resolution Pipeline can generate file:// links
	if loop.deps.Workspace != nil {
		brainStore.SetWorkspaceDir(loop.deps.Workspace.WorkDir())
	}
	loop.deps.Brain = brainStore
	p.loops[sid] = &managedLoop{loop: loop, lastAccess: time.Now(), userKey: userKey}
	return loop
}

// countUserLocked returns how many loops belong to a user. Caller must hold p.mu.
func (p *LoopPool) countUserLocked(userKey string) int {
	count := 0
	for _, ml := range p.loops {
		if ml.userKey == userKey {
			count++
		}
	}
	return count
}

// evictUserOldestLocked removes the least recently accessed idle loop for a specific user.
// Caller must hold p.mu write lock.
func (p *LoopPool) evictUserOldestLocked(userKey string) {
	var oldestSid string
	var oldestTime time.Time

	for sid, ml := range p.loops {
		if ml.userKey != userKey {
			continue
		}
		// Only evict idle loops (not currently running)
		if ml.loop.runMu.TryLock() {
			ml.loop.runMu.Unlock()
			if oldestSid == "" || ml.lastAccess.Before(oldestTime) {
				oldestSid = sid
				oldestTime = ml.lastAccess
			}
		}
	}

	if oldestSid != "" {
		if ml, ok := p.loops[oldestSid]; ok {
			ml.loop.Stop()
			delete(p.loops, oldestSid)
		}
	}
}

// evictOldestLocked removes the least recently accessed idle loop globally.
// If preferUserKey is non-empty, prefer evicting from that user first.
// Caller must hold p.mu write lock.
func (p *LoopPool) evictOldestLocked(preferUserKey string) {
	var oldestSid string
	var oldestTime time.Time

	// Pass 1: prefer same-user idle loops
	if preferUserKey != "" {
		for sid, ml := range p.loops {
			if ml.userKey != preferUserKey {
				continue
			}
			if ml.loop.runMu.TryLock() {
				ml.loop.runMu.Unlock()
				if oldestSid == "" || ml.lastAccess.Before(oldestTime) {
					oldestSid = sid
					oldestTime = ml.lastAccess
				}
			}
		}
	}

	// Pass 2: fall back to global LRU
	if oldestSid == "" {
		for sid, ml := range p.loops {
			if ml.loop.runMu.TryLock() {
				ml.loop.runMu.Unlock()
				if oldestSid == "" || ml.lastAccess.Before(oldestTime) {
					oldestSid = sid
					oldestTime = ml.lastAccess
				}
			}
		}
	}

	if oldestSid != "" {
		if ml, ok := p.loops[oldestSid]; ok {
			ml.loop.Stop()
			delete(p.loops, oldestSid)
		}
	}
}

// Remove removes a session loop (e.g., on session end).
func (p *LoopPool) Remove(sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ml, ok := p.loops[sid]; ok {
		ml.loop.Stop()
		delete(p.loops, sid)
	}
}

// List returns all active session IDs.
func (p *LoopPool) List() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sids := make([]string, 0, len(p.loops))
	for sid := range p.loops {
		sids = append(sids, sid)
	}
	return sids
}

// Len returns the number of active loops.
func (p *LoopPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.loops)
}

// CountForUser returns the number of active loops for a given user key.
func (p *LoopPool) CountForUser(userKey string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, ml := range p.loops {
		if ml.userKey == userKey {
			count++
		}
	}
	return count
}

// SetPlanModeAll broadcasts a plan mode change to all active loops.
func (p *LoopPool) SetPlanModeAll(mode string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ml := range p.loops {
		ml.loop.SetPlanMode(mode)
	}
}

// ErrPoolFull is returned when the pool is at max capacity and no idle loops can be evicted.
var ErrPoolFull = fmt.Errorf("session pool is full: all sessions are active")

// ExtractUserKey derives a user identifier from a session ID.
// Convention: "tg-{userId}-{hash}" → "tg-{userId}", "web-{uuid}" → "web-{uuid}"
func ExtractUserKey(sid string) string {
	// Telegram format: tg-12345-abcdef
	if strings.HasPrefix(sid, "tg-") {
		parts := strings.SplitN(sid, "-", 3)
		if len(parts) >= 2 {
			return parts[0] + "-" + parts[1] // "tg-12345"
		}
	}
	// Web/CLI format: full sessionID is the user key (single user per session)
	return sid
}

