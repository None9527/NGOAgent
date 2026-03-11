package service

import (
	"sync"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
)

// LoopPool manages per-session AgentLoop instances.
// Each session gets an independent loop with its own history and brain dir.
type LoopPool struct {
	mu       sync.RWMutex
	loops    map[string]*AgentLoop
	factory  func(sid string) *AgentLoop
	brainDir string
}

// NewLoopPool creates a loop pool with a factory function for creating new loops.
func NewLoopPool(factory func(sid string) *AgentLoop, brainDir string) *LoopPool {
	return &LoopPool{
		loops:    make(map[string]*AgentLoop),
		factory:  factory,
		brainDir: brainDir,
	}
}

// Get returns an existing loop or creates a new one for the session.
func (p *LoopPool) Get(sid string) *AgentLoop {
	p.mu.RLock()
	if loop, ok := p.loops[sid]; ok {
		p.mu.RUnlock()
		return loop
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check after write lock
	if loop, ok := p.loops[sid]; ok {
		return loop
	}

	loop := p.factory(sid)
	// Set session-scoped brain
	loop.deps.Brain = brain.NewArtifactStore(p.brainDir, sid)
	p.loops[sid] = loop
	return loop
}

// Remove removes a session loop (e.g., on session end).
func (p *LoopPool) Remove(sid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if loop, ok := p.loops[sid]; ok {
		loop.Stop()
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
