// Package config provides the unified configuration system for NGOAgent.
// Supports YAML configuration, fsnotify-based hot-reload, and section-level subscriptions.
package config

import (
	"strings"
	"time"
)

// Sanitized returns the full config as a map with sensitive fields masked.
func (c *Config) Sanitized() map[string]any {
	providers := make([]map[string]any, len(c.LLM.Providers))
	for i, p := range c.LLM.Providers {
		providers[i] = map[string]any{
			"name":     p.Name,
			"type":     p.Type,
			"base_url": p.BaseURL,
			"api_key":  maskKey(p.APIKey),
			"models":   p.Models,
		}
	}

	mcpServers := make([]map[string]any, len(c.MCP.Servers))
	for i, s := range c.MCP.Servers {
		mcpServers[i] = map[string]any{
			"name":    s.Name,
			"command": s.Command,
			"args":    s.Args,
		}
	}

	grpcPort := c.Server.GRPCPort
	if grpcPort == 0 {
		grpcPort = 19998
	}

	return map[string]any{
		"server": map[string]any{
			"http_port": c.Server.HTTPPort,
			"grpc_port": grpcPort,
			"mode":      c.Server.Mode,
			"timezone":  c.Server.Timezone,
		},
		"agent": map[string]any{
			"default_model": c.Agent.DefaultModel,
			"planning_mode": c.Agent.PlanningMode,
			"max_steps":     c.Agent.MaxSteps,
			"workspace":     c.Agent.Workspace,
		},
		"llm": map[string]any{
			"providers": providers,
		},
		"security": map[string]any{
			"mode":          c.Security.Mode,
			"block_list":    c.Security.BlockList,
			"safe_commands": c.Security.SafeCommands,
		},
		"storage": map[string]any{
			"db_path":       c.Storage.DBPath,
			"brain_dir":     c.Storage.BrainDir,
			"knowledge_dir": c.Storage.KnowledgeDir,
			"skills_dir":    c.Storage.SkillsDir,
		},
		"cron": map[string]any{
			"enabled": c.Cron.Enabled,
		},
		"evo": map[string]any{
			"max_retries":      c.Evo.MaxRetries,
			"cooldown_seconds": c.Evo.CooldownSeconds,
			"cleanup_days":     c.Evo.CleanupDays,
			"score_threshold":  c.Evo.ScoreThreshold,
			"eval_model":       c.Evo.EvalModel,
			"auto_eval":        c.Evo.AutoEval,
		},
		"mcp": map[string]any{
			"servers": mcpServers,
		},
		"search": map[string]any{
			"endpoint": c.Search.Endpoint,
		},
		"embedding": map[string]any{
			"provider":             c.Embedding.Provider,
			"base_url":             c.Embedding.BaseURL,
			"api_key":              maskKey(c.Embedding.APIKey),
			"model":                c.Embedding.Model,
			"dimensions":           c.Embedding.Dimensions,
			"similarity_threshold": c.Embedding.SimilarityThreshold,
			"min_ki_for_embedding": c.Embedding.MinKIForEmbedding,
			"top_k":                c.Embedding.TopK,
		},
	}
}

// maskKey returns a masked version of an API key for safe display.
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "${") {
		return key // Environment variable reference, show as-is
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Config is the root configuration structure, mapped from config.yaml.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Agent         AgentConfig         `yaml:"agent"`
	LLM           LLMConfig           `yaml:"llm"`
	Security      SecurityConfig      `yaml:"security"`
	Storage       StorageConfig       `yaml:"storage"`
	Cron          CronConfig          `yaml:"cron"`
	Evo           EvoConfig           `yaml:"evo"`
	MCP           MCPConfig           `yaml:"mcp"`
	Search        SearchConfig        `yaml:"search"`
	Embedding     EmbeddingConfig     `yaml:"embedding"`
	Memory        MemoryConfig        `yaml:"memory"`
	Notifications NotificationsConfig `yaml:"notifications"` // P3 M1
}

// NotificationsConfig holds outbound webhook targets (P3 M1).
type NotificationsConfig struct {
	Webhooks []WebhookTargetConfig `yaml:"webhooks"`
}

// WebhookTargetConfig is one webhook endpoint in config.yaml.
type WebhookTargetConfig struct {
	URL    string   `yaml:"url"`
	Events []string `yaml:"events,omitempty"` // nil = all
	Secret string   `yaml:"secret,omitempty"` // HMAC-SHA256 signing key
	Retry  int      `yaml:"retry,omitempty"`  // extra retries (def: 0)
}

// MemoryConfig defines settings for the vector memory and diary subsystem.
type MemoryConfig struct {
	HalfLifeDays int `yaml:"half_life_days" json:"half_life_days"` // Time-decay half-life in days (default: 30, 0=no decay)
	MaxFragments int `yaml:"max_fragments" json:"max_fragments"`   // Capacity limit (default: 0=unlimited)
	MaxAgeDays   int `yaml:"max_age_days" json:"max_age_days"`     // P2 F2: Auto-prune fragments older than this (default: 90, 0=no prune)
}

// SearchConfig defines web search provider settings.
type SearchConfig struct {
	Endpoint   string `yaml:"endpoint"`    // agent-search service URL (curl_cffi+Camoufox gateway), e.g. http://localhost:8890
	SearXNGURL string `yaml:"searxng_url"` // SearXNG direct URL for fast web_search, e.g. http://localhost:8080
}

// EmbeddingConfig defines the embedding model configuration for KI vector search.
type EmbeddingConfig struct {
	Provider            string  `yaml:"provider"`             // "dashscope" | "openai" | "" (disabled)
	BaseURL             string  `yaml:"base_url"`             // API base URL
	APIKey              string  `yaml:"api_key"`              // API key (supports ${ENV_VAR})
	Model               string  `yaml:"model"`                // Model name, e.g. "text-embedding-v3"
	Dimensions          int     `yaml:"dimensions"`           // Vector dimensions, e.g. 1024
	SimilarityThreshold float64 `yaml:"similarity_threshold"` // Dedup threshold, default 0.75
	MinKIForEmbedding   int     `yaml:"min_ki_for_embedding"` // KI count threshold to activate embedding retrieval; below this, full injection is used
	TopK                int     `yaml:"top_k"`                // Number of KIs to retrieve via embedding search
	KIBudgetChars       int     `yaml:"ki_budget_chars"`      // Character budget for L1 semantic KI injection (default 6000)
}

// ServerConfig defines HTTP/gRPC server settings.
type ServerConfig struct {
	HTTPPort  int    `yaml:"http_port"`
	GRPCPort  int    `yaml:"grpc_port"`
	Mode      string `yaml:"mode"`       // debug / release
	Timezone  string `yaml:"timezone"`   // "system" or IANA name like "Asia/Shanghai"
	AuthToken string `yaml:"auth_token"` // Bearer token for API authentication (empty = no auth)
}

// LoadLocation returns the *time.Location for the configured timezone.
// - empty or "system": uses time.Local (OS timezone)
// - IANA name (e.g. "Asia/Shanghai"): loads that timezone
func (c *Config) LoadLocation() *time.Location {
	tz := strings.TrimSpace(c.Server.Timezone)
	if tz == "" || strings.EqualFold(tz, "system") {
		return time.Local
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}

// AgentConfig controls user-facing agent behavior and LLM hyperparameters.
type AgentConfig struct {
	DefaultModel    string  `yaml:"default_model"`
	PlanningMode    bool    `yaml:"planning_mode"`     // true=force plan, false=auto-detect
	MaxSteps        int     `yaml:"max_steps"`         // Max agent loop steps (default: 200)
	MaxSubagents    int     `yaml:"max_subagents"`     // Max concurrent sub-agents per session (default: 3)
	Workspace       string  `yaml:"workspace"`         // Default working directory for shell commands
	Temperature     float64 `yaml:"temperature"`       // LLM sampling temperature, 0.0-2.0 (default: 0.7)
	TopP            float64 `yaml:"top_p"`             // Nucleus sampling threshold, 0.0-1.0 (default: 0.9)
	MaxOutputTokens int     `yaml:"max_output_tokens"` // Max tokens per LLM response (default: 8192)
	ContextWindow   int     `yaml:"context_window"`    // Default context window for unknown models (default: 32768)
	CompactRatio    float64 `yaml:"compact_ratio"`     // Trigger context compact at this usage ratio (default: 0.7)
}

// LLMConfig defines LLM provider connections.
type LLMConfig struct {
	Providers []ProviderDef `yaml:"providers"`
}

// ProviderDef describes one LLM provider endpoint.
type ProviderDef struct {
	Name        string                   `yaml:"name"`
	Type        string                   `yaml:"type"`
	BaseURL     string                   `yaml:"base_url"`
	APIKey      string                   `yaml:"api_key"`
	Models      []string                 `yaml:"models"`
	ModelConfig map[string]ModelOverride `yaml:"model_config,omitempty"` // Per-model capability overrides
}

// ModelOverride allows per-model configuration of LLM parameters.
// Values here take priority over AgentConfig globals and hardcoded fallback.
// Resolution order: ModelOverride > AgentConfig > hardcoded fallback.
// P3-1: Pointer types for Temperature/TopP to distinguish 0.0 from unset.
type ModelOverride struct {
	ContextWindow   int      `yaml:"context_window" json:"context_window"`
	MaxOutputTokens int      `yaml:"max_output_tokens" json:"max_output_tokens"`
	Temperature     *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	TopP            *float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`
	// Capability flags — pointer types to distinguish unset (nil) from explicit false.
	SupportsTools    *bool `yaml:"supports_tools,omitempty" json:"supports_tools,omitempty"`
	SupportsThinking *bool `yaml:"supports_thinking,omitempty" json:"supports_thinking,omitempty"`
	SupportsVision   *bool `yaml:"supports_vision,omitempty" json:"supports_vision,omitempty"`
	SupportsCache    *bool `yaml:"supports_cache,omitempty" json:"supports_cache,omitempty"`
}

// ModelParams holds resolved parameters for a specific model.
type ModelParams struct {
	Temperature     float64
	TopP            float64
	MaxOutputTokens int
	ContextWindow   int
	CompactRatio    float64
}

// ResolveModelParams resolves LLM parameters for a specific model.
// Priority: model_config[model] > agent global > hardcoded fallback.
func (c *Config) ResolveModelParams(model string) ModelParams {
	// Start with agent-level defaults (or hardcoded fallback)
	p := ModelParams{
		Temperature:     c.Agent.Temperature,
		TopP:            c.Agent.TopP,
		MaxOutputTokens: c.Agent.MaxOutputTokens,
		ContextWindow:   c.Agent.ContextWindow,
		CompactRatio:    c.Agent.CompactRatio,
	}
	// Apply hardcoded fallbacks for zero values
	if p.Temperature == 0 {
		p.Temperature = 0.7
	}
	if p.TopP == 0 {
		p.TopP = 0.9
	}
	if p.MaxOutputTokens == 0 {
		p.MaxOutputTokens = 8192
	}
	if p.ContextWindow == 0 {
		p.ContextWindow = 32768
	}
	if p.CompactRatio == 0 {
		p.CompactRatio = 0.7
	}

	// Override with per-model config (highest priority)
	for _, prov := range c.LLM.Providers {
		if mc, ok := prov.ModelConfig[model]; ok {
			// P3-1: Pointer check — nil means unset, non-nil 0.0 is intentional
			if mc.Temperature != nil {
				p.Temperature = *mc.Temperature
			}
			if mc.TopP != nil {
				p.TopP = *mc.TopP
			}
			if mc.MaxOutputTokens > 0 {
				p.MaxOutputTokens = mc.MaxOutputTokens
			}
			if mc.ContextWindow > 0 {
				p.ContextWindow = mc.ContextWindow
			}
			break
		}
	}
	return p
}

// SecurityConfig defines the security policy.
type SecurityConfig struct {
	Mode         string   `yaml:"mode"` // allow / auto / ask
	BlockList    []string `yaml:"block_list"`
	SafeCommands []string `yaml:"safe_commands"`
	Workspace    string   `yaml:"-"` // populated from AgentConfig.Workspace at startup (not persisted)

	// P3 K1: AI Safety Classifier strategy
	// ClassifierMode: "pattern" (default) | "llm" | "hybrid"
	// ClassifierModel: small fast model for llm/hybrid (e.g., "claude-haiku-4-5")
	ClassifierMode  string `yaml:"classifier_mode,omitempty"`
	ClassifierModel string `yaml:"classifier_model,omitempty"`
}

// StorageConfig defines paths for data storage.
type StorageConfig struct {
	DBPath       string `yaml:"db_path"`
	BrainDir     string `yaml:"brain_dir"`
	KnowledgeDir string `yaml:"knowledge_dir"`
	SkillsDir    string `yaml:"skills_dir"`
}

// CronConfig controls the global cron scheduler on/off switch.
// Individual job schedules are managed by the agent via the manage_cron tool.
type CronConfig struct {
	Enabled bool `yaml:"enabled"`
}

// EvoConfig defines settings for the evolution (self-repair) engine.
type EvoConfig struct {
	MaxRetries      int     `yaml:"max_retries"`      // Max auto-repair retries per session (default: 2)
	CooldownSeconds int     `yaml:"cooldown_seconds"` // Min interval between repairs in seconds (default: 30)
	CleanupDays     int     `yaml:"cleanup_days"`     // Trace/Eval/Repair data retention days (default: 30)
	ScoreThreshold  float64 `yaml:"score_threshold"`  // Evaluation pass threshold 0.0-1.0 (default: 0.7)
	EvalModel       string  `yaml:"eval_model"`       // Evaluator model (empty = use main model)
	AutoEval        bool    `yaml:"auto_eval"`        // Auto-evaluate in evo mode (default: true)
}

// MCPConfig defines MCP server configurations.
type MCPConfig struct {
	Servers []MCPServerDef `yaml:"servers"`
}

// MCPServerDef describes one MCP server to launch.
type MCPServerDef struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env,omitempty"` // extra environment variables injected into the subprocess
}
