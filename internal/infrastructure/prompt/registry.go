// Package prompt — Section Registry (Sprint 1-6).
// Replaces the monolithic buildSections switch-case with a
// declarative, composable factory registry. Each section registers
// its own factory function; the Engine assembles based on order + priority.
package prompt

import (
	"sort"
	"sync"
)

// SectionFactory produces a Section from runtime Deps.
// Returning an empty Content causes the section to be silently skipped.
type SectionFactory func(deps Deps) Section

// CacheTier classifies section stability for multi-breakpoint caching.
// DashScope supports up to 4 cache_control markers per request.
const (
	CacheTierDynamic = 0 // Per-request content (KI, Memory, Runtime, Ephemeral)
	CacheTierCore    = 1 // Immutable framework content (Identity, Behavior, Safety, ToolProtocol)
	CacheTierSession = 2 // Session-stable content (UserRules, Tools, Skills, ProjectContext)
)

// SectionMeta holds registration metadata independent of content.
type SectionMeta struct {
	Name      string         // Unique section identifier (e.g. "Identity")
	Order     int            // Assembly position (1-18+)
	Priority  int            // 0=required, 1=high, 2=medium, 3=low
	Cacheable bool           // Deprecated: use CacheTier > 0. Kept for backward compat.
	CacheTier int            // 0=dynamic, 1=core(immutable), 2=session(stable)
	Factory   SectionFactory // Content producer
}

// Registry is a thread-safe collection of named section factories.
// Supports registration, lookup, and ordered assembly.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]SectionMeta
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]SectionMeta)}
}

// Register adds or replaces a section factory.
// Safe to call concurrently (e.g. from init() functions).
func (r *Registry) Register(meta SectionMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[meta.Name] = meta
}

// Unregister removes a section factory by name.
// No-op if the name is not registered.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

// Build materializes all registered sections for the given Deps,
// returning them sorted by Order.
func (r *Registry) Build(deps Deps) []Section {
	r.mu.RLock()
	metas := make([]SectionMeta, 0, len(r.entries))
	for _, m := range r.entries {
		metas = append(metas, m)
	}
	r.mu.RUnlock()

	// Sort deterministically by Order (then Name as tiebreaker)
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Order != metas[j].Order {
			return metas[i].Order < metas[j].Order
		}
		return metas[i].Name < metas[j].Name
	})

	sections := make([]Section, 0, len(metas))
	for _, m := range metas {
		s := m.Factory(deps)
		// Guarantee metadata consistency — factory only sets Content
		s.Order = m.Order
		s.Name = m.Name
		s.Priority = m.Priority
		// CacheTier takes precedence; fall back to Cacheable bool for backward compat
		if m.CacheTier > 0 {
			s.CacheTier = m.CacheTier
			s.Cacheable = true
		} else {
			s.Cacheable = m.Cacheable
			if m.Cacheable {
				s.CacheTier = CacheTierSession // default for legacy Cacheable=true
			}
		}
		sections = append(sections, s)
	}
	return sections
}

// Names returns registered section names sorted alphabetically.
// Useful for introspection and testing.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Size returns the number of registered sections.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
