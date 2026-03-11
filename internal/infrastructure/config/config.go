// Package config provides the unified configuration system for NGOAgent.
// Supports YAML configuration, fsnotify-based hot-reload, and section-level subscriptions.
package config

import (
	"time"
)

// Config is the root configuration structure, mapped from config.yaml.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Agent     AgentConfig     `yaml:"agent"`
	LLM       LLMConfig       `yaml:"llm"`
	Security  SecurityConfig  `yaml:"security"`
	Storage   StorageConfig   `yaml:"storage"`
	Heartbeat HeartbeatConfig `yaml:"heartbeat"`
	Forge     ForgeConfig     `yaml:"forge"`
	MCP       MCPConfig       `yaml:"mcp"`
	Search    SearchConfig    `yaml:"search"`
}

// SearchConfig defines web search provider settings.
type SearchConfig struct {
	Endpoint string `yaml:"endpoint"` // SearXNG endpoint URL, e.g. http://localhost:8888
}

// ServerConfig defines HTTP/gRPC server settings.
type ServerConfig struct {
	HTTPPort int    `yaml:"http_port"`
	GRPCPort int    `yaml:"grpc_port"`
	Mode     string `yaml:"mode"` // debug / release
}

// AgentConfig controls user-facing agent behavior.
type AgentConfig struct {
	DefaultModel string `yaml:"default_model"`
	PlanningMode bool   `yaml:"planning_mode"` // true=force plan, false=auto-detect
	MaxSteps     int    `yaml:"max_steps"`     // Max agent loop steps (default: 200, Anti's MAX_INVOCATIONS)
}

// LLMConfig defines LLM provider connections.
type LLMConfig struct {
	Providers []ProviderDef `yaml:"providers"`
}

// ProviderDef describes one LLM provider endpoint.
type ProviderDef struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	BaseURL string   `yaml:"base_url"`
	APIKey  string   `yaml:"api_key"`
	Models  []string `yaml:"models"`
}

// SecurityConfig defines the security policy.
type SecurityConfig struct {
	Mode         string   `yaml:"mode"` // allow / auto / ask
	BlockList    []string `yaml:"block_list"`
	SafeCommands []string `yaml:"safe_commands"`
}

// StorageConfig defines paths for data storage.
type StorageConfig struct {
	DBPath       string `yaml:"db_path"`
	BrainDir     string `yaml:"brain_dir"`
	KnowledgeDir string `yaml:"knowledge_dir"`
	SkillsDir    string `yaml:"skills_dir"`
}

// HeartbeatConfig defines the background heartbeat engine settings.
type HeartbeatConfig struct {
	Enabled       bool            `yaml:"enabled"`
	Interval      time.Duration   `yaml:"interval"`
	MaxSteps      int             `yaml:"max_steps"`
	NotifyChannel string          `yaml:"notify_channel"`
	Security      HeartbeatSecCfg `yaml:"security"`
}

// HeartbeatSecCfg defines the security policy for heartbeat mode.
type HeartbeatSecCfg struct {
	AllowedTools []string `yaml:"allowed_tools"`
	BlockedTools []string `yaml:"blocked_tools"`
}

// ForgeConfig defines settings for the capability forging engine.
type ForgeConfig struct {
	SandboxDir         string `yaml:"sandbox_dir"`
	MaxRetries         int    `yaml:"max_retries"`
	AutoForgeOnInstall bool   `yaml:"auto_forge_on_install"`
	HistoryLimit       int    `yaml:"history_limit"`
}

// MCPConfig defines MCP server configurations.
type MCPConfig struct {
	Servers []MCPServerDef `yaml:"servers"`
}

// MCPServerDef describes one MCP server to launch.
type MCPServerDef struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}
