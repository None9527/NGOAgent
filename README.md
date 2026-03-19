<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black" />
  <img src="https://img.shields.io/badge/Architecture-DDD-blueviolet?style=for-the-badge" />
  <img src="https://img.shields.io/badge/License-BSL%201.1-blue?style=for-the-badge" />
</p>

# NGOAgent: 边缘计算自治 AI 操作系统 | Edge Autonomous AI OS

**为真实世界的复杂工作流而生：企业级、本地优先的自主智能体架构。**
*Built for real-world complexity: An enterprise-grade, local-first autonomous agent architecture.*

> “不再是聊天机器人，不再是一次性脚本。我们正在定义边缘侧的真正自主行动标杆。”
> *"Beyond chatbots. Beyond scripts. We are defining true autonomous action at the edge."*

在数据隐私成为核心壁垒、企业自动化日益复杂的今天，NGOAgent 作为一款生产级、自主决策的 AI 操作系统应运而生。它由超过 **30,000 行健壮的 Go 生产代码** 驱动，依靠原生的领域驱动设计 (DDD)、10状态 ReAct 引擎和金融级安全护栏，将**认知自动化**直接交付到用户的隐私环境中。

*In an era where data privacy is paramount, NGOAgent emerges as a production-ready, autonomous AI operating system. Driven by over **30,000 lines of robust Go code**, it leverages native Domain-Driven Design (DDD), a 10-state ReAct engine, and bank-grade security to deliver **cognitive automation** directly into your private environment.*

<p align="center">
  <img src="assets/demo.gif" alt="NGOAgent Demo" width="720" />
</p>

---

## 💥 七大核心护城河 | The 7 Core Moats

NGOAgent 从第一天起就以**生产级部署、绝对安全和无限扩展**为核心目标，打破了开源 Agent “玩具级”脚本的脆弱魔咒：
*NGOAgent shatters the fragility of "toy project" Python scripts by prioritizing production-ready deployment, absolute security, and infinite extensibility from day one:*

### 1. 🛡️ 绝对的数据主权 (Local-First Data Sovereignty)

完美接入本地部署的百亿/千亿大模型（Ollama、vLLM）或私有化云端接口。没有任何数据会离开受信任区域，直击金融、医疗研发中“数据出境”的核心痛点。

*Seamlessly integrates with local massive models (Ollama, vLLM) or privatized cloud APIs. Not a single byte leaves your trusted perimeter—directly solving the "data exfiltration" crisis in finance and medical R&D.*

### 2. 🧠 Agentic LoopPool™ & ReAct 引擎 (State Machine Engine)

高度确定性的 **10 状态 ReAct 状态机**，赋予 Agent 人类般的“思考、规划、执行、验证、重试”决策闭环。独创的 **LoopPool** 池化技术实现了千人千面的硬隔离并发，彻底告别状态污染。

*A highly deterministic **10-state ReAct decision engine** equips the Agent with human-like "think, plan, execute, verify, retry" capabilities. The proprietary **LoopPool** technology ensures hard-isolated concurrency, completely eliminating state pollution.*

### 3. 🐝 真实架构级蜂群双轨机制 (True SubAgent Swarm Design)

市面多数“多智能体”仅是在单线程上下文里的 Prompt 杂耍，毫无隔离可言。NGOAgent 依托 Go 的并发中枢，主 Agent 可以瞬间孵化无数个运行在独立协程、拥有私有 10状态机与完全物理隔离记忆区的实体子 Agent。实现了“轻量模型极速出 Plan 拓扑，重型算力全力 Execute 攻坚”的完美双轨解耦，真正打造出抗污染的分布式 AI 军团。

*Most "multi-agent" frameworks are mere Prompt-illusions sharing a single context. Built on Go's powerful concurrency, NGOAgent's primary Agent can instantly hatch dozens of real algorithmic sub-agents running in independent routines with private memory zones. This creates a true dual-track system: lightweight models plan at lightning speed, while heavy-duty models execute concurrently. A genuine, context-isolated distributed AI cluster.*

### 4. 🔌 原生 Skill 插件生态与 Forge 沙箱 (Native Skills & Forge Sandbox)

摒弃笨重的第三方协议，NGOAgent 拥有一套极度轻量的原生 Skill 热加载生态。只需几行代码，即可将企业私有系统封装为专属“数字触角”。所有未经审计的代码均会在独创的 **Forge（锻造沙箱）** 中严格隔离演练，确保零风险上线。

*Ditching bulky 3rd-party protocols, NGOAgent ships with a lightweight, hot-loadable Native Skill ecosystem. Encapsulate any proprietary system into a custom "Skill" with minimal code. Crucially, unverified code is rigorously tested in the proprietary **Forge Sandbox** for zero-risk deployment.*

### 5. 🔗 永不失联的高可用层 (Unbreakable SSE Telemetry)

专为残酷生产环境设计。重度缓冲的 SSE (Server-Sent Events) 网络层，在网络抖动、页面刷新、甚至笔记本合上期间，后台 Agent 仍死磕任务；网络一经恢复，执行进度条与全量日志瞬间残血回退、同步归位。

*Designed for the real world. A heavily buffered SSE network layer ensures the Agent keeps fighting through network drops, page refreshes, or closed laptops. Upon reconnection, progress and full log streams are instantly restored.*

### 6. 💂‍♂️ 跨入深水区的“金融级”安全靶向 (Bank-Grade Security Hooks)

实现了细粒度的 `Allow / Auto / Ask` 权限三叉戟。敏感操作（如大批量删改、高危命令）会自动熔断，并向 Web UI 或 Telegram 发送交互式审批请求。人类永远握着绝对的“一键终止”倒挂开关。

*Implements a granular `Allow / Auto / Ask` permission triad. High-risk operations (e.g., mass deletions, system commands) auto-trip the circuit breaker, sending interactive approval requests cross-platform. Humans retain the ultimate kill switch.*

### 7. 🏗️ 领域驱动设计赋能的极速业务定制 (Agile DDD Customization)

极度的“Model-Agnostic”（模型脱钩）。凭借 Go 语言 **领域驱动设计 (DDD)** 的高内聚、松耦合内核，面对任何特定垂直行业（军工、量化计算）的私有化定制需求，都能以乐高式的敏捷速度进行二次开发与重构，永不崩塌。

*Fiercely model-agnostic. Grounded in a Go-based **Domain-Driven Design (DDD)** core, this highly decoupled architecture allows enterprise clients in highly regulated verticals to rapidly inject custom business logic and swap out reasoning engines with Lego-like agility—without risking system stability.*

---

## 🧩 核心架构图 | Architecture Blueprint

NGOAgent 在内网独立运转了一整套涵盖引擎、持久化、记忆与扩展体系的流式架构：
*An entirely self-contained streaming architecture covering engine, persistence, memory, and extension systems:*

```mermaid
graph TD
    Main["cmd/ngoagent<br/>Builder.Build()"] --> Server
    Main --> Engine
    Main --> Tool

    subgraph Interfaces["接口层 | Interface Layer"]
        Server["server<br/>gRPC / HTTP SSE"]
        Web["webui<br/>React + TypeScript"]
    end

    subgraph Core["核心引擎 | domain/service"]
        Engine["AgentLoop<br/>ReAct 状态机"]
    end

    subgraph Infra["基础设施栈 | infrastructure"]
        Tool["tool · 23 个内置原子工具"]
        Prompt["prompt · 15-Section 重组"]
        LLM["llm · Provider 智能漏斗与降级"]
        Security["security · 安全与 Hook 拦截"]
        Brain["brain · 会话流 Artifacts"]
        KI["knowledge · KI 知识蒸馏"]
        Skill["skill · 热拔插生态"]
    end

    Web -->|SSE 毫秒级流送| Server
    Server --> Engine
    Engine --> Tool
    Engine --> Prompt
    Engine --> LLM
    Engine --> Security
    Engine --> Brain
    Brain --> KI
    Engine --> Skill

    style Core fill:#1a1a2e,stroke:#e94560,color:#fff
    style Interfaces fill:#16213e,stroke:#0f3460,color:#fff
    style Infra fill:#0f3460,stroke:#533483,color:#fff
```

---

## ⚡ 极速开始 | Quick Start

### 前置环境 | Prerequisites

- **Go** ≥ 1.24
- **Node.js** ≥ 18 (运行极地质感的 React Web UI)
- **ripgrep** (`rg`) & **fd** — 底层闪电搜索工具依赖

### 本地构建部署 | Local Build & Deploy

```bash
# 1. 深度克隆基座 | Clone repo
git clone https://github.com/ngoclaw/ngoagent.git
cd ngoagent

# 2. 核心后端构建 | Build backend
go build -o ngoagent ./cmd/ngoagent

# 3. 守护进程启动 | Start daemon (Auto-bootstraps ~/.ngoagent and outputs Auth Token)
./ngoagent serve
# ╔══════════════════════════════════════════════════════════════╗
# ║  AUTH TOKEN GENERATED (save this for frontend connection):   ║
# ║  e.g: a1b2c3d4...64-char-hex...                             ║
# ╚══════════════════════════════════════════════════════════════╝

# 4. 前端空间启动 | Start Web UI
cd webui && npm install && npm run dev
# 浏览器访问 http://localhost:5173，输入生成的 Token 完成安全握手
```

---

## ⚙️ 动态配置注入 | Configuration

首次启动即可自生成带全量注释的中心配置文件 `~/.ngoagent/config.yaml`（支持**热更新重载**）：
*Auto-generates `~/.ngoagent/config.yaml` on first run (supports **Hot Reload**):*

```yaml
agent:
  workspace: "~/.ngoagent/workspace"  # 物理隔离边界 / Workspace boundary
  planning_mode: false                # NLP启发式触发 Plan / Heuristic planning

llm:
  providers:
    - name: "local_ollama"
      type: "openai"
      base_url: "http://localhost:11434/v1"
      models: ["huihui-opus:latest", "qwen2.5-coder"]

security:
  mode: "auto"                        # allow / auto / ask 三级鉴权
  block_list: ["rm", "rmdir", "mkfs", "dd", "shutdown"]
  safe_commands: ["ls", "cat", "grep", "find", "go", "npm", "git"]

server:
  http_port: 19997
  auth_token: "<auto-generated>"      # 高阶熵 SHA-256 安全验证令
```

---

## 🧰 超级挂载套件 | Toolchain Overview

 NGOAgent 配备 **23+ 项原子工具**。 / *Equipped with 23+ atomic tools.*

| 功能象限 | 工具 (Tools) | 核心场景 (Use Case) |
|----------|------------|----------|
| **磁盘 I/O / File System** | `read_file`, `write_file`, `edit_file`, `undo_edit` | 并发文件编辑、多行安全正则替换、代码防崩回撤 |
| **脱管 Shell / Async CLI** | `run_command`, `command_status` | 非阻塞终端执行、防止长进程卡死进程池 |
| **全息探测 / Search & Glob** | `grep_search`, `glob` | 依托 Rust 底层的大型源码极速内容搜寻与目录拓扑嗅探 |
| **广域视觉 / Web Intelligence**| `web_search`, `web_fetch` | 无缝拉起 SearXNG 搜索与 HTML 净化抓取，打破封闭数据孤岛 |
| **持久记忆 / Memory & Ctx**  | `save_memory`, `update_project_context` | 沉淀跨会话 KI (Knowledge Item) 资产与项目永久化上下文 |
| **多体作战 / Swarm Routing**| `task_boundary`, `task_plan`, `spawn_agent` | PEV 任务循环切片、生成 SubAgent 并行突围解决局部重构难题 |

---

## 🛡️ 双轨流式安全握手 | SSE + Auth Security

1. **Token 鉴权链**: 首次起服生成高阶伪随机 SHA256 密钥串进行强制 Bearer 鉴权保护。
2. **断点回溯协议**: API `/v1/chat/reconnect` 支持在浏览器崩溃后强悍重连，并重放流式记录流。

---

## 📖 设计手札与资源 | Documentation

- [**design.md**](docs/design.md) — 2000 行纯粹的后端 DDD 拆解思路
- [**architecture.md**](docs/architecture.md) — 概览、依赖结构、God Interface 抹除技巧

> “这是数字同事的超级办公桌，一个真正能跑通千行代码长链重构、具有物理反制能力的开源 AI 中枢。”

---

## License

[Business Source License 1.1](LICENSE)
