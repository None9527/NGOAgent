package llm

import (
	"fmt"
	"sync"
)

// Router manages multiple LLM providers and routes requests by model name.
type Router struct {
	mu        sync.RWMutex
	providers map[string]Provider
	modelMap  map[string]string // model → provider name
	fallback  []string          // fallback chain of provider names
	current   string            // current active model
	health    *HealthChecker    // P2 E1: provider health probing (nil = skip)
}

// NewRouter creates a router from config.
func NewRouter(providers []Provider) *Router {
	r := &Router{
		providers: make(map[string]Provider),
		modelMap:  make(map[string]string),
	}
	for _, p := range providers {
		r.providers[p.Name()] = p
		for _, model := range p.Models() {
			r.modelMap[model] = p.Name()
			if r.current == "" {
				r.current = model
			}
		}
		r.fallback = append(r.fallback, p.Name())
	}
	return r
}

// Resolve returns the Provider for the given model name.
func (r *Router) Resolve(model string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if model == "" {
		model = r.current
	}

	provName, ok := r.modelMap[model]
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", model)
	}

	prov, ok := r.providers[provName]
	if !ok {
		return nil, fmt.Errorf("provider %s not found for model %s", provName, model)
	}
	return prov, nil
}

// CurrentModel returns the active default model.
func (r *Router) CurrentModel() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// SwitchModel sets the default model.
func (r *Router) SwitchModel(model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.modelMap[model]; !ok {
		return fmt.Errorf("unknown model: %s", model)
	}
	r.current = model
	return nil
}

// ListModels returns all registered models.
func (r *Router) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.modelMap))
	for model := range r.modelMap {
		models = append(models, model)
	}
	return models
}

// SetDefault sets the default model (alias for SwitchModel).
func (r *Router) SetDefault(model string) error {
	return r.SwitchModel(model)
}

// ResolveWithFallback tries the requested model first, then walks the fallback chain.
// P2 E1: skips providers marked unhealthy by the HealthChecker.
func (r *Router) ResolveWithFallback(model string) (Provider, string, error) {
	return r.ResolveWithExclusions(model, nil)
}

// ResolveWithExclusions behaves like ResolveWithFallback but skips any explicitly excluded provider names.
func (r *Router) ResolveWithExclusions(model string, excluded []string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if model == "" {
		model = r.current
	}

	isExcluded := func(p string) bool {
		for _, ex := range excluded {
			if ex == p {
				return true
			}
		}
		return false
	}

	// Try primary (if healthy and not excluded)
	if provName, ok := r.modelMap[model]; ok && !isExcluded(provName) {
		if prov, ok := r.providers[provName]; ok {
			if r.health == nil || r.health.IsHealthy(provName) {
				return prov, model, nil
			}
		}
	}

	// Fallback: try each provider's first model (skip unhealthy and excluded)
	for _, provName := range r.fallback {
		if isExcluded(provName) {
			continue
		}
		if r.health != nil && !r.health.IsHealthy(provName) {
			continue
		}
		prov, ok := r.providers[provName]
		if !ok {
			continue
		}
		models := prov.Models()
		if len(models) > 0 {
			return prov, models[0], nil
		}
	}

	return nil, "", fmt.Errorf("no available provider for model %s and fallback exhausted", model)
}

// SetHealthChecker injects a health checker (call after construction, before use).
func (r *Router) SetHealthChecker(hc *HealthChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.health = hc
}

// ProviderMap returns the internal providers map (for HealthChecker startup).
func (r *Router) ProviderMap() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Provider, len(r.providers))
	for k, v := range r.providers {
		out[k] = v
	}
	return out
}

// Reload replaces providers (for hot-reload from config change).
func (r *Router) Reload(providers []Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers = make(map[string]Provider)
	r.modelMap = make(map[string]string)
	r.fallback = nil

	for _, p := range providers {
		r.providers[p.Name()] = p
		for _, model := range p.Models() {
			r.modelMap[model] = p.Name()
		}
		r.fallback = append(r.fallback, p.Name())
	}

	// Validate current model still exists
	if _, ok := r.modelMap[r.current]; !ok && len(r.modelMap) > 0 {
		for model := range r.modelMap {
			r.current = model
			break
		}
	}
}
