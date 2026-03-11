package config

import "time"

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Agent: AgentConfig{
			PlanningMode: false,
			MaxSteps:     200,
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
		Heartbeat: HeartbeatConfig{
			Enabled:  false,
			Interval: 30 * time.Minute,
			MaxSteps: 5,
			Security: HeartbeatSecCfg{
				AllowedTools: []string{
					"read_file", "glob", "grep_search",
					"web_search", "web_fetch",
					"update_project_context", "save_memory",
				},
				BlockedTools: []string{
					"run_command", "edit_file", "write_file", "forge",
				},
			},
		},
		Forge: ForgeConfig{
			SandboxDir:         "/tmp/ngoagent-forge",
			MaxRetries:         5,
			AutoForgeOnInstall: true,
			HistoryLimit:       20,
		},
	}
}

// DefaultConfigYAML returns the default config.yaml content for bootstrap.
const DefaultConfigYAML = `# NGOAgent Configuration

agent:
  planning_mode: false

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

heartbeat:
  enabled: false
  interval: 30m

server:
  http_port: 8080
`

// DefaultUserRules is the initial user_rules.md content.
const DefaultUserRules = `你是 NGOAgent，一个运行在用户本地的自主 AI Agent。

## 核心特质
- 有判断力的技术搭档，不是聊天机器人
- 先查资料再问问题，带着答案来而不是带着疑问来
- 不确定就说不确定，绝不编造 API、库或数据
- 简洁直接——跳过客套，直接干活
`

// DefaultHeartbeat is the initial heartbeat.md content.
const DefaultHeartbeat = `# Heartbeat Tasks

<!-- Add tasks below. The heartbeat engine will process them periodically. -->
<!-- Format: Markdown checklist items. Example: -->
<!-- - [ ] Check if tests pass -->
`
