<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black" />
  <img src="https://img.shields.io/badge/Architecture-DDD-blueviolet?style=for-the-badge" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" />
</p>

# NGOAgent

**自主式本地 AI Agent** — 运行在你自己机器上的 AI 编程助手，具备文件操作、Shell 执行、知识管理、定时任务等完整能力。

> ~30K LOC · 23 个内置工具 · React Web UI · gRPC + HTTP SSE 双协议

---

## 核心特性

| 特性 | 描述 |
|------|------|
| 🧠 **ReAct 循环** | 10 状态机驱动的自主决策循环，支持工具调用、上下文压缩、自动重试 |
| 🔧 **23 个内置工具** | 文件读写编辑、Shell 执行、Grep/Glob 搜索、Web 抓取、知识管理、Brain Artifact |
| 📝 **Prompt Assembly** | U 形注意力布局的系统提示词组装，4 级预算裁剪，支持可插拔组件 |
| 🛡️ **安全决策链** | Allow / Auto / Ask 三级权限，命令黑名单，工具级审批，审计日志 |
| 🧩 **技能 + 锻造** | Skill 热加载 + Forge 沙箱验证，MCP 协议集成 |
| 💾 **跨会话知识** | Brain Artifact (会话级) + KI Store (全局级)，LLM 自动蒸馏 |
| ⏰ **定时任务** | Cron 引擎，独立会话隔离，支持自主巡检 |
| 🌐 **双协议接口** | HTTP SSE (Web UI) + gRPC (CLI/Bot 集成)，统一 DeltaSink 流式协议 |

---

## 架构

```mermaid
graph TD
    Main["cmd/ngoagent<br/>Builder.Build()"] --> Server
    Main --> Engine
    Main --> Tool

    subgraph Interfaces["接口层"]
        Server["server<br/>gRPC / HTTP SSE"]
        Web["webui<br/>React + TypeScript"]
    end

    subgraph Core["核心引擎 · domain/service"]
        Engine["AgentLoop<br/>ReAct 状态机"]
    end

    subgraph Infra["基础设施 · infrastructure"]
        Tool["tool · 23 个"]
        Prompt["prompt · Assembly"]
        LLM["llm · Provider Router"]
        Security["security · Hook Chain"]
        Brain["brain · Artifact"]
        KI["knowledge · KI Store"]
    end

    Web -->|SSE| Server
    Server --> Engine
    Engine --> Tool
    Engine --> Prompt
    Engine --> LLM
    Engine --> Security
    Engine --> Brain
    Brain --> KI

    style Core fill:#1a1a2e,stroke:#e94560,color:#fff
    style Interfaces fill:#16213e,stroke:#0f3460,color:#fff
    style Infra fill:#0f3460,stroke:#533483,color:#fff
```

---

## 快速开始

### 前置依赖

- **Go** ≥ 1.24
- **Node.js** ≥ 18 (Web UI)
- **ripgrep** (`rg`) — grep_search 工具依赖
- **fd** — glob 工具 (可选, 自动降级到 `find`)

### 构建 & 启动

```bash
# 1. 克隆
git clone https://github.com/ngoclaw/ngoagent.git
cd ngoagent

# 2. 构建后端
go build -o ngoagent ./cmd/ngoagent

# 3. 首次启动 (自动初始化 ~/.ngoagent/ 目录)
./ngoagent serve

# 4. 构建并启动 Web UI
cd webui && npm install && npm run dev
# 访问 http://localhost:5173
```

### 配置

首次启动自动生成 `~/.ngoagent/config.yaml`，参考 [config.example.yaml](config.example.yaml)：

```yaml
agent:
  planning_mode: false
  workspace: "~/.ngoagent/workspace"

llm:
  providers:
    - name: "default"
      type: "openai"
      base_url: "https://api.openai.com/v1"
      api_key: "${OPENAI_API_KEY}"
      models: ["gpt-4"]

security:
  mode: "auto"            # allow / auto / ask
  block_list: ["rm", "rmdir", "mkfs", "dd", "shutdown"]

server:
  http_port: 19997
  grpc_port: 19998
```

> **提示**：API Key 通过环境变量注入，不会写入配置文件。支持所有 OpenAI 兼容 API。

---

## 自动初始化

首次启动时，NGOAgent 自动创建 `~/.ngoagent/` 目录：

```
~/.ngoagent/
├── config.yaml       配置文件
├── user_rules.md     用户规则 (Agent 行为)
├── .state.json       启动状态 (new → ready)
├── data/             SQLite 数据库
├── brain/            会话级 Artifact
├── knowledge/        全局知识 (KI Store)
├── skills/           技能目录 (热加载)
├── cron/             定时任务
├── workspace/        默认工作目录
├── forge/            锻造沙箱
├── prompts/          提示词变体
├── mcp/              MCP 配置
└── logs/             运行日志
```

---

## 工具清单

| 类别 | 工具 | 描述 |
|------|------|------|
| **文件** | `read_file` | 读取文件，自动行号标注，二进制检测 |
| | `write_file` | 创建/覆盖文件，自动建目录 |
| | `edit_file` | 精确字符串替换，模糊匹配降级 |
| | `undo_edit` | 撤销文件编辑 |
| **Shell** | `run_command` | Bash 执行，后台模式，超时控制 |
| | `command_status` | 查询后台命令状态 |
| **搜索** | `grep_search` | 基于 ripgrep 的代码搜索 |
| | `glob` | 基于 fd 的文件查找 (自动降级 find) |
| **Web** | `web_search` | SearXNG 搜索引擎查询 |
| | `web_fetch` | URL 内容抓取 |
| **知识** | `save_memory` | 写入全局 KI Store |
| | `update_project_context` | 写入项目级 context.md |
| **协作** | `task_boundary` | Planning/Execution/Verification 模式切换 |
| | `task_plan` | Brain Artifact (plan/task/walkthrough) |
| | `notify_user` | 审批请求 + 用户通知 |
| | `spawn_agent` | 子代理委派 |
| | `send_message` | 跨会话消息发送 |
| **扩展** | `script_tool` | 自定义脚本工具 (Skill 自动注册) |
| | `mcp_adapter` | MCP 协议桥接 |
| | `forge` | 能力锻造 (沙箱验证) |
| | `manage_cron` | 定时任务 CRUD |

---

## 项目结构

```
NGOAgent/
├── cmd/ngoagent/           程序入口
├── internal/
│   ├── domain/service/     核心引擎 (AgentLoop · DeltaSink · Guard)
│   ├── infrastructure/
│   │   ├── tool/           23 个内置工具
│   │   ├── llm/            LLM Provider Router
│   │   ├── prompt/         Prompt Assembly Pipeline
│   │   ├── security/       安全决策链
│   │   ├── brain/          会话 Artifact
│   │   ├── knowledge/      跨会话知识 (KI)
│   │   ├── skill/          技能系统
│   │   ├── cron/           定时任务引擎
│   │   ├── mcp/            MCP 协议
│   │   ├── config/         配置管理 + 热重载
│   │   ├── persistence/    SQLite 持久化
│   │   └── sandbox/        命令沙箱
│   ├── application/        API 层 (Builder · AgentAPI)
│   └── interfaces/
│       ├── server/         HTTP SSE 服务器
│       └── grpc/           gRPC 服务器
├── webui/                  React + TypeScript 前端
├── api/proto/              gRPC Protobuf 定义
├── docs/                   设计文档
└── config.example.yaml     配置示例
```

---

## 流式协议

所有输出通过统一的 `DeltaSink` 接口流式传输:

```
LLM → AgentLoop → DeltaSink → Server (SSE/gRPC) → Client
```

| 事件 | 字段 | 描述 |
|------|------|------|
| `text_delta` | `content` | 文本流 |
| `thinking` | `content` | 推理过程 |
| `tool_start` | `name`, `args` | 工具开始 |
| `tool_result` | `call_id`, `output`, `error` | 工具结果 |
| `progress` | `task_name`, `status`, `mode` | 任务进度 |
| `approval_request` | `approval_id`, `tool_name`, `reason` | 审批请求 |
| `plan_review` | `message`, `paths` | 方案审查 |
| `error` | `message` | 错误 |
| `step_done` | — | 步骤完成 |

---

## gRPC API

NGOAgent 提供完整的 gRPC API（`:19998`），支持 CLI 和 Bot 集成：

```bash
# 示例：列出会话
grpcurl -plaintext localhost:19998 agent.AgentService/ListSessions

# 示例：发送消息
grpcurl -plaintext -d '{"message":"hello","session_id":"..."}' \
  localhost:19998 agent.AgentService/SendMessage
```

完整 RPC 定义见 [agent_service.proto](api/proto/agent_service.proto)。

---

## 开发

```bash
# 后端编译
go build -o ngoagent ./cmd/ngoagent

# 后端测试
go test ./internal/...

# 前端开发 (热重载)
cd webui && npm run dev

# 前端生产构建
cd webui && npm run build
```

---

## 设计文档

- [**design.md**](docs/design.md) — 完整后端设计
- [**architecture.md**](docs/architecture.md) — 架构蓝图

---

## License

MIT
