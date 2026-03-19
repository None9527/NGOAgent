package config

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Agent: AgentConfig{
			PlanningMode: false,
			MaxSteps:     200,
			Workspace:    "~/.ngoagent/workspace",
		},
		LLM: LLMConfig{},
		Security: SecurityConfig{
			Mode:         "auto",
			BlockList:    []string{"rm", "rmdir", "mkfs", "dd", "shutdown"},
			SafeCommands: []string{"ls", "cat", "grep", "find", "go", "npm", "git"},
		},
		Storage: StorageConfig{
			DBPath:       "~/.ngoagent/data/ngoagent.db",
			BrainDir:     "~/.ngoagent/brain",
			KnowledgeDir: "~/.ngoagent/knowledge",
			SkillsDir:    "~/.ngoagent/skills",
		},
		Cron: CronConfig{
			Enabled: true,
		},
		Forge: ForgeConfig{
			SandboxDir:         "/tmp/ngoagent-forge",
			MaxRetries:         5,
			AutoForgeOnInstall: true,
			HistoryLimit:       20,
		},
		Embedding: EmbeddingConfig{
			Provider:            "", // disabled by default
			Model:               "text-embedding-v3",
			Dimensions:          1024,
			SimilarityThreshold: 0.75,
			MinKIForEmbedding:   30,
			TopK:                5,
			KIBudgetChars:       6000,
		},
	}
}

// DefaultConfigYAML returns the default config.yaml content for bootstrap.
const DefaultConfigYAML = `# NGOAgent Configuration

agent:
  planning_mode: false
  workspace: "~/.ngoagent/workspace"  # default cwd for shell commands

llm:
  providers:
    - name: "default"
      type: "openai"
      base_url: "https://api.openai.com/v1"
      api_key: "${OPENAI_API_KEY}"
      models: ["gpt-4"]

security:
  mode: "auto"
  block_list: ["rm", "rmdir", "mkfs", "dd", "shutdown"]
  safe_commands: ["ls", "cat", "grep", "find", "go", "npm", "git"]

storage:
  db_path: "~/.ngoagent/data/ngoagent.db"
  brain_dir: "~/.ngoagent/brain"
  knowledge_dir: "~/.ngoagent/knowledge"
  skills_dir: "~/.ngoagent/skills"

cron:
  enabled: true

server:
  http_port: 8080
  auth_token: ""               # Auto-generated on first run / 首次启动自动生成

embedding:
  provider: ""           # "dashscope" | "openai" | "" (disabled)
  # base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
  # api_key: "${DASHSCOPE_API_KEY}"
  # model: "text-embedding-v3"
  # dimensions: 1024
  # similarity_threshold: 0.85
  # min_ki_for_embedding: 30   # KI < 30: full injection; >= 30: embedding retrieval
  # top_k: 5                   # retrieve top-K relevant KIs

# MCP servers are configured in ~/.ngoagent/mcp.json (CC-compatible format).
# Use mcp.json to add/manage MCP servers. See mcp.json for format reference.
# Inline servers below are merged at lowest priority (mcp.json wins on name collision).
mcp:
  servers: []
`

// DefaultUserRules is the initial user_rules.md content.
const DefaultUserRules = `你是 NGOAgent，一个运行在用户本地的自主 AI Agent。

## 核心特质
- 有判断力的技术搭档，不是聊天机器人
- 先查资料再问问题，带着答案来而不是带着疑问来
- 不确定就说不确定，绝不编造 API、库或数据
- 简洁直接——跳过客套，直接干活
`

// DefaultMCPJSON is the initial mcp.json content for bootstrap.
// CC-compatible format: servers as a named map with command/args/env.
const DefaultMCPJSON = `{
  "servers": {}
}
`
