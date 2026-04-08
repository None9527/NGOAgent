// Package service — agent_registry.go provides the AgentRegistry that manages
// AgentDefinition templates. Definitions are loaded from YAML files at startup
// and resolved by type when spawn_agent is invoked.
package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
	"gopkg.in/yaml.v3"
)

// AgentRegistry manages a set of AgentDefinition templates.
// Thread-safe: all reads/writes go through a RWMutex.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*model.AgentDefinition
}

// NewAgentRegistry creates an empty registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*model.AgentDefinition),
	}
}

// Register adds or replaces a definition. Later registrations override earlier ones.
func (r *AgentRegistry) Register(def *model.AgentDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Apply defaults
	if def.ContextLevel == "" {
		def.ContextLevel = model.ContextWithSummary // default: L2
	}
	if def.Memory == "" {
		def.Memory = model.MemoryNone
	}
	if def.MaxTimeout == 0 {
		def.MaxTimeout = 5 * time.Minute // default: 5min
	}
	r.agents[def.AgentType] = def
}

// Resolve looks up a definition by agent type.
// Returns the "general" fallback if the type is not found.
// Returns an error only if no definitions are registered at all.
func (r *AgentRegistry) Resolve(agentType string) (*model.AgentDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if def, ok := r.agents[agentType]; ok {
		return def, nil
	}
	// Fallback to "general"
	if def, ok := r.agents["general"]; ok {
		slog.Info(fmt.Sprintf("[agent-registry] type %q not found, falling back to general", agentType))
		return def, nil
	}
	if len(r.agents) == 0 {
		return nil, fmt.Errorf("agent registry is empty — no definitions loaded")
	}
	// Last resort: return the first available
	for _, def := range r.agents {
		slog.Info(fmt.Sprintf("[agent-registry] type %q not found, using %q as fallback", agentType, def.AgentType))
		return def, nil
	}
	return nil, fmt.Errorf("agent type %q not found", agentType)
}

// List returns all registered definitions sorted by AgentType.
func (r *AgentRegistry) List() []*model.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*model.AgentDefinition, 0, len(r.agents))
	for _, def := range r.agents {
		result = append(result, def)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].AgentType < result[j].AgentType
	})
	return result
}

// LoadFromDir loads YAML agent definitions from a directory.
// Each .yaml/.yml file must contain a single AgentDefinition.
// Source is set to the provided value (e.g. "built-in", "user", "project").
func (r *AgentRegistry) LoadFromDir(dir string, source string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist yet — not an error
		}
		return fmt.Errorf("read agent dir %s: %w", dir, err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Info(fmt.Sprintf("[agent-registry] skip %s: %v", path, err))
			continue
		}

		var def model.AgentDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			slog.Info(fmt.Sprintf("[agent-registry] skip %s: invalid yaml: %v", path, err))
			continue
		}
		if def.AgentType == "" {
			slog.Info(fmt.Sprintf("[agent-registry] skip %s: missing agent_type", path))
			continue
		}

		def.Source = source
		r.Register(&def)
		loaded++
		slog.Info(fmt.Sprintf("[agent-registry] loaded %s (%s) from %s", def.AgentType, source, entry.Name()))
	}

	slog.Info(fmt.Sprintf("[agent-registry] loaded %d definitions from %s (%s)", loaded, dir, source))
	return nil
}

// TypeNames returns a sorted list of all registered agent type names.
// Used by spawn_agent schema to populate the enum.
func (r *AgentRegistry) TypeNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
