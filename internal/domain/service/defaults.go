package service

import "github.com/ngoclaw/ngoagent/internal/infrastructure/config"

// Fallback defaults — used when config values are zero (not set).
const (
	fallbackTemperature    = 0.7
	fallbackTopP           = 0.9
	fallbackMaxOutputTokens = 8192
	fallbackContextWindow  = 32768
	fallbackCompactRatio   = 0.7
)

// ResolveTemperature returns the configured temperature or fallback.
func ResolveTemperature(cfg *config.Config) float64 {
	if cfg != nil && cfg.Agent.Temperature > 0 {
		return cfg.Agent.Temperature
	}
	return fallbackTemperature
}

// ResolveTopP returns the configured top_p or fallback.
func ResolveTopP(cfg *config.Config) float64 {
	if cfg != nil && cfg.Agent.TopP > 0 {
		return cfg.Agent.TopP
	}
	return fallbackTopP
}

// ResolveMaxOutputTokens returns the configured max_output_tokens or fallback.
func ResolveMaxOutputTokens(cfg *config.Config) int {
	if cfg != nil && cfg.Agent.MaxOutputTokens > 0 {
		return cfg.Agent.MaxOutputTokens
	}
	return fallbackMaxOutputTokens
}

// ResolveContextWindow returns the configured context_window or fallback.
func ResolveContextWindow(cfg *config.Config) int {
	if cfg != nil && cfg.Agent.ContextWindow > 0 {
		return cfg.Agent.ContextWindow
	}
	return fallbackContextWindow
}

// ResolveCompactRatio returns the configured compact_ratio or fallback.
func ResolveCompactRatio(cfg *config.Config) float64 {
	if cfg != nil && cfg.Agent.CompactRatio > 0 {
		return cfg.Agent.CompactRatio
	}
	return fallbackCompactRatio
}
