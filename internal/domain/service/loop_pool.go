package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
)

// LoopPool manages per-session AgentLoop instances with concurrency limits.
// Each session gets an independent loop with its own history and brain dir.
type LoopPool struct {
	mu       sync.RWMutex
	loops    map[string]*managedLoop
	factory  func(sid string) *AgentLoop
	brainDir string
	maxLoops int // max concurrent sessions (0 = unlimited)
}

// managedLoop wraps AgentLoop with access tracking for LRU eviction.
type managedLoop struct {
	loop       *AgentLoop
	lastAccess time.Time
}

// NewLoopPool creates a loop pool with a factory function for creating new loops.
// maxLoops limits concurrent sessions; 0 means unlimited (dangerous in production).
func NewLoopPool(factory func(sid string) *AgentLoop, brainDir string) *LoopPool {
	return &LoopPool{
		loops:    make(map[string]*managedLoop),
		factory:  factory,
		brainDir: brainDir,
		maxLoops: 8, // safe default
	}
}

// SetMaxLoops sets the maximum number of concurrent session loops.
func (p *LoopPool) SetMaxLoops(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxLoops = n
}

// Get returns an existing loop or creates a new one for the session.
// If maxLoops is reached, the least recently accessed idle loop is evicted.
func (p *LoopPool) Get(sid string) *AgentLoop {
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

	// Evict if at capacity
	if p.maxLoops > 0 && len(p.loops) >= p.maxLoops {
		p.evictOldestLocked()
	}

	loop := p.factory(sid)
	// Set session-scoped brain
	brainStore := brain.NewArtifactStore(p.brainDir, sid)
	// BUG-11 fix: propagate workspaceDir so Resolution Pipeline can generate file:// links
	if loop.deps.Workspace != nil {
		brainStore.SetWorkspaceDir(loop.deps.Workspace.WorkDir())
	}
	loop.deps.Brain = brainStore
	p.loops[sid] = &managedLoop{loop: loop, lastAccess: time.Now()}
	return loop
}

// evictOldestLocked removes the least recently accessed idle loop.
// Caller must hold p.mu write lock.
func (p *LoopPool) evictOldestLocked() {
	var oldestSid string
	var oldestTime time.Time

	for sid, ml := range p.loops {
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

// ErrPoolFull is returned when the pool is at max capacity and no idle loops can be evicted.
var ErrPoolFull = fmt.Errorf("session pool is full: all sessions are active")
