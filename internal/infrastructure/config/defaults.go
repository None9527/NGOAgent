package config

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort: 8080,
			GRPCPort: 19998,
			Mode:     "release",
			Timezone: "system",
		},
		Agent: AgentConfig{
			PlanningMode:    false,
			MaxSteps:        200,
			MaxSubagents:    3,
			Workspace:       "~/.ngoagent/workspace",
			Temperature:     0.7,
			TopP:            0.9,
			MaxOutputTokens: 8192,
			ContextWindow:   32768,
			CompactRatio:    0.7,
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
		Evo: EvoConfig{
			MaxRetries:      2,
			CooldownSeconds: 30,
			CleanupDays:     30,
			ScoreThreshold:  0.7,
			EvalModel:       "",
			AutoEval:        true,
		},
		Embedding: EmbeddingConfig{
			Provider:            "", // disabled by default
			Model:               "text-embedding-v3",
			Dimensions:          1024,
			SimilarityThreshold: 0.85, // M3a: raised from 0.75—stricter dedup prevents redundant KIs
			MinKIForEmbedding:   30,
			TopK:                5,
			KIBudgetChars:       6000,
		},
		Memory: MemoryConfig{
			HalfLifeDays: 30,
			MaxFragments: 0, // unlimited
		},
	}
}

// DefaultConfigYAML returns the default config.yaml content for bootstrap.
const DefaultConfigYAML = `# NGOAgent Configuration

server:
  http_port: 8080
  grpc_port: 19998
  mode: "release"
  timezone: "system"        # "system" or IANA name like "Asia/Shanghai"
  auth_token: ""            # Auto-generated on first run

agent:
  planning_mode: false
  max_steps: 200
  max_subagents: 3              # max concurrent sub-agents per session
  workspace: "~/.ngoagent/workspace"
  # LLM hyperparameters
  temperature: 0.7          # 0.0-2.0, lower = more deterministic
  top_p: 0.9                # 0.0-1.0, nucleus sampling threshold
  max_output_tokens: 8192   # max tokens per LLM response
  context_window: 32768     # fallback context window for unknown models
  compact_ratio: 0.7        # trigger context compaction at 70% usage

llm:
  providers:
    - name: "default"
      type: "openai"
      base_url: "https://api.openai.com/v1"
      api_key: "${OPENAI_API_KEY}"
      models: ["gpt-4"]
      # model_config:         # optional per-model overrides
      #   gpt-4:
      #     context_window: 128000
      #     max_output_tokens: 16384

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

evo:
  max_retries: 2              # max auto-repair retries per session
  cooldown_seconds: 30        # min interval between repairs
  cleanup_days: 30            # trace/eval/repair data retention days
  score_threshold: 0.7        # evaluation pass threshold (0.0-1.0)
  eval_model: ""              # evaluator model (empty = use main model)
  auto_eval: true             # auto-evaluate in evo mode

memory:
  half_life_days: 30        # time-decay half-life for memory fragments
  max_fragments: 0          # 0 = unlimited

embedding:
  provider: ""              # "dashscope" | "openai" | "" (disabled)
  # base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
  # api_key: "${DASHSCOPE_API_KEY}"
  # model: "text-embedding-v3"
  # dimensions: 1024
  # similarity_threshold: 0.75
  # min_ki_for_embedding: 30
  # top_k: 5

# MCP servers: configured in ~/.ngoagent/mcp.json (CC-compatible format).
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
