# Phase 8: CLI/UX/Commands 层深度对标分析

> CC `commands/` (66 个 slash commands, 9798 行) + `skills/` (20 个 bundled skills)
> vs NGO `interfaces/` (2901 行) — 4 层接入架构 (HTTP+WS+gRPC+Telegram)

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. 交互模型](#2-交互模型)
- [3. 命令系统](#3-命令系统)
- [4. WebSocket / 实时通信](#4-websocket-实时通信)
- [5. REST API 层](#5-rest-api-层)
- [6. gRPC 层](#6-grpc-层)
- [7. 多通道接入 (Bot)](#7-多通道接入-bot)
- [8. Skills 体系](#8-skills-体系)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. 架构概览

### 架构模型对比

```
CC — 纯 CLI 单通道架构              NGO — 4 层多通道架构
┌──────────────────────┐          ┌──────────────────────────────┐
│ Terminal REPL         │          │ React WebUI (SPA)            │
│ ├── Ink/React 渲染    │          │ ↓ WebSocket                  │
│ ├── /slash commands   │          ├──────────────────────────────┤
│ │   (66 个)           │          │ HTTP Server (760行)           │
│ ├── vim mode          │          │ ├── REST API (509行, 40+ routes)│
│ ├── voice input       │          │ ├── WS Handler (227行)        │
│ ├── keybindings       │          │ ├── SSE fallback             │
│ ├── color/theme       │          │ └── Token auth               │
│ └── stickers          │          ├──────────────────────────────┤
├──────────────────────┤          │ gRPC Server (714行)           │
│ SDK mode              │          │ ├── 40+ RPC methods          │
│ (non-interactive)     │          │ ├── Proto-first contracts    │
│ --print / pipe input  │          │ └── Server-side streaming    │
├──────────────────────┤          ├──────────────────────────────┤
│ Skills (20 bundled)   │          │ Telegram Bot (588行)          │
│ batch, loop, debug,   │          │ ├── /start /new /stop /help  │
│ remember, stuck...    │          │ ├── Inline approval buttons  │
└──────────────────────┘          │ └── StreamHandler → SSE 桥接 │
                                  ├──────────────────────────────┤
                                  │ AgentAPI Facade (统一)        │
                                  │ 所有通道共享同一业务接口        │
                                  └──────────────────────────────┘
```

### 代码量对比

| 关注点 | CC | NGO | 说明 |
|-------|-----|-----|------|
| 命令系统 | 66 commands (9798 行) | /slash 拦截 (12 个) | CC 10x |
| Skills | 20 bundled (1200+ 行) | Skill Manager (单独模块) | 架构不同 |
| 终端渲染 | Ink/React | 无 CLI | — |
| WebSocket | ❌ | 227 行 ws_handler.go | NGO 独有 |
| REST API | ❌ (CLI 直调) | 509 行 (40+ routes) | NGO 独有 |
| gRPC | ❌ | 714 行 (40+ RPCs) | NGO 独有 |
| Telegram | ❌ | 588 行 (4 文件) | NGO 独有 |
| **合计** | ~10998 行 | ~2901 行 | 3.8x |

---

## 2. 交互模型

### CC — 终端 REPL

CC 是一个**纯 CLI 应用**，使用 Ink (React for CLI) 渲染终端界面：

| 模式 | 说明 |
|------|------|
| Interactive REPL | 默认交互模式，全功能 |
| SDK / Print | `--print` 非交互输出 |
| Pipe | 管道输入 `echo "deploy" \| claude` |
| Resume | `--resume <session>` 恢复会话 |
| Vim mode | `/vim` toggle 按键绑定 |
| Voice | `/voice` 语音输入 |
| Headless | 无 TTY 纯文本输出 |

### NGO — 多通道 Daemon

NGO 是一个**后台 Daemon 进程**，支持 4 种接入通道：

| 通道 | 协议 | 特点 |
|------|------|------|
| WebUI | HTTP + WebSocket | React SPA, 全功能 |
| REST API | HTTP/JSON | 40+ 标准 endpoints |
| gRPC | protobuf | 类型安全, server-streaming |
| Telegram | Bot API + HTTP | 移动端接入 |

**统一 API Facade** — `AgentAPI` 接口被所有 4 个通道共享：
```go
type API interface {
    ChatStream(ctx, sessionID, message, mode, delta) error
    SessionID(sessionID) string
    StopRun(sessionID)
    Approve(approvalID, approved) error
    NewSession(title) SessionResponse
    ListModels() ModelListResponse
    // ... 40+ methods
}
```

---

## 3. 命令系统

### CC — 66 个 Slash Commands

CC 维护了当前审计过的**最庞大的命令注册表**：

| 类别 | 命令 | 说明 |
|------|------|------|
| **会话** | `/compact` `/clear` `/resume` `/rewind` `/rename` `/session` | 会话管理 |
| **代码/Git** | `/commit` `/diff` `/review` `/branch` `/pr_comments` `/security-review` | SCM 集成 |
| **模型** | `/model` `/fast` `/effort` `/advisor` | 模型控制 |
| **记忆** | `/memory` `/context` | 记忆管理 |
| **配置** | `/config` `/permissions` `/privacy-settings` `/rate-limit-options` | 设置 |
| **外观** | `/color` `/theme` `/stickers` | 主题定制 |
| **特殊** | `/vim` `/voice` `/keybindings` `/thinkback` `/thinkback-play` | 交互增强 |
| **调试** | `/doctor` `/heapdump` `/stats` `/usage` `/cost` | 诊断 |
| **集成** | `/mcp` `/chrome` `/desktop` `/ide` `/install-github-app` `/install-slack-app` | 外部集成 |
| **Evo** | `/bridge` `/bridge-kick` `/agents` `/tasks` `/plan` | 高级编排 |
| **其他** | `/feedback` `/version` `/upgrade` `/export` `/copy` `/btw` `/brief` `/insights` | 杂项 |

### NGO — 12 个 Slash Commands

NGO 在 WebSocket `onChat` 中拦截 slash commands：

```go
knownCmds := map[string]bool{
    "/model": true, "/models": true, "/set": true, "/evo": true,
    "/plan": true, "/status": true, "/help": true, "/skill": true,
    "/clear": true, "/compact": true, "/cron": true,
}
```

NGO 的命令更少但覆盖了核心场景。其他功能（会话管理、模型切换、配置等）通过 REST API / gRPC 暴露而非 slash command。

---

## 4. WebSocket / 实时通信

### CC — 无 WebSocket

CC 作为 CLI 应用，不需要 WebSocket。实时流通过终端标准输出直接渲染。

### NGO — 持久 WS 连接 (227 行)

NGO 的 WebSocket 是**前端实时通信的核心通道**：

```
Client → WS → readLoop → dispatch
                ├── "chat"    → onChat → ChatStream(delta=wsWriter)
                ├── "stop"    → StopRun(sessionID)
                ├── "approve" → Approve(approvalID, approved)
                └── "ping"    → pong
```

**关键设计决策**:

| 特性 | 实现 |
|------|------|
| Writer 生命周期 | WS 连接生命周期 = Writer 生命周期 (非 per-request) |
| 无 MarkDone | 与 SSE 不同，不需要标记完成 (auto-wake 可复用同一 writer) |
| 无 RunTracker | WS 不需要 SSE 的断线重连缓冲 |
| 认证 | URL query `?token=<auth_token>` |
| 读取限制 | 4MB message limit |
| 写超时 | 5 秒 |
| 线程安全 | `sync.Mutex` 保护 closed 状态 |

**Auto-wake 支持**: WS writer 在 chat turn 完成后仍保持有效，允许 `barrier callback → loopPool.Run` 推送新事件到前端。

---

## 5. REST API 层

### CC — 无 REST API

CC 所有操作通过 CLI 命令或 SDK 调用完成。

### NGO — 40+ REST Routes (api.go, 509 行)

NGO 的 REST API 是一个**完整的管理面板后端**：

| 资源 | Endpoints | 方法 |
|------|-----------|------|
| **Session** | `/session/new` `/session/list` `/session/title` `/session/delete` | POST/GET |
| **History** | `/history` `/history/clear` `/history/compact` | GET/POST |
| **Tools** | `/tools` `/tools/enable` `/tools/disable` | GET/POST |
| **Skills** | `/skills/list` `/skills/read` `/skills/refresh` `/skills/delete` | GET/POST |
| **MCP** | `/mcp/servers` `/mcp/tools` | GET |
| **Config** | `/config` `/config/provider/add` `/config/provider/remove` | POST |
| **MCP管理** | `/config/mcp/add` `/config/mcp/remove` | POST |
| **Security** | `/security` | GET |
| **Stats** | `/stats` `/system` | GET |
| **Brain** | `/brain/list` `/brain/read` | GET |
| **KI** | `/ki/list` `/ki/get` `/ki/delete` `/ki/artifacts` `/ki/artifact/read` | GET/POST |
| **Cron** | `/cron/list` `/cron/create` `/cron/delete` `/cron/enable` `/cron/disable` `/cron/run` `/cron/logs` `/cron/log/read` | GET/POST |

共计 **40+ 独立 endpoints**，覆盖系统完整的 CRUD 操作。

---

## 6. gRPC 层

### CC — 无 gRPC

### NGO — 全功能 gRPC Server (714 行)

NGO 实现了完整的 **proto-first gRPC 服务**：

```protobuf
service AgentService {
    rpc Chat(AgentChatRequest) returns (stream AgentChatEvent);
    rpc StopRun(SessionRequest) returns (CommandResponse);
    rpc ApproveToolCall(ApproveToolCallRequest) returns (CommandResponse);
    rpc HealthCheck(...) returns (HealthCheckResponse);
    rpc ListModels(...) returns (ListModelsResponse);
    rpc SwitchModel(...) returns (CommandResponse);
    // ... 35+ RPCs
}
```

**gRPC Chat Streaming** — Protocol-specific Delta 适配:
```go
delta := &service.Delta{
    OnTextFunc: func(text string) {
        stream.Send(&pb.AgentChatEvent{Type: "text_delta", Text: text})
    },
    OnToolStartFunc: func(callID, name string, args map[string]any) {
        stream.Send(&pb.AgentChatEvent{Type: "tool_call", ...})
    },
    OnApprovalRequestFunc: func(approvalID, toolName string, ...) {
        stream.Send(&pb.AgentChatEvent{Type: "approval_request", ...})
    },
}
```

**gRPC 的架构价值**:
- 类型安全 (protobuf schema)
- 高性能 binary 序列化
- 双向 streaming 支持
- 跨语言客户端生成 (Python/JS/Rust 等)
- 适合嵌入式 / SDK 场景

---

## 7. 多通道接入 (Bot)

### CC — 无 Bot 集成

CC 有 `/install-slack-app` 命令但仅用于安装 GitHub Actions CI，非直接 Bot 通道。

### NGO — Telegram Bot (588 行)

NGO 实现了完整的 **Telegram Bot 接入通道**：

**架构**: Handler → StreamHandler → HTTP+SSE → Backend API

| 组件 | 文件 | 行数 | 职责 |
|------|------|------|------|
| `bot.go` | 初始化 | 57 | Bot 启动 + Update 分发 |
| `handler.go` | 路由 | 188 | 命令/消息/回调分发 |
| `stream_handler.go` | 流式 | 285 | SSE 桥接 + 增量 Telegram 消息更新 |
| `session.go` | 会话 | 58 | userID → sessionID 映射 |

**核心能力**:
- `/start` `/new` `/stop` `/status` `/help` — 5 个 Bot 命令
- 增量消息更新 — `StreamToTelegram()` 使用 EditMessageText 实时更新
- 权限控制 — `IsAllowed(userID)` 白名单
- 工具审批 — InlineKeyboard 按钮 (`approve:<id>:1/0`)

**独特设计 — 审批回调**:
```go
func (h *Handler) handleCallback(cb *tgbotapi.CallbackQuery) {
    // 解析 "approve:<approvalID>:<1|0>"
    approved := parts[2] == "1"
    h.stream.Approve(approvalID, approved)
    // 更新按钮状态为 "✅ 已允许" 或 "❌ 已拒绝"
}
```

---

## 8. Skills 体系

### CC — 20 个 Bundled Skills

CC 的 Skills 是**内置的 prompt+工具组合**:

| Skill | 功能 |
|-------|------|
| `batch` | 批量执行任务 |
| `loop` | 循环执行直到满足条件 |
| `debug` | 调试辅助 |
| `remember` | 记忆管理 |
| `stuck` | 卡住时自动重试 |
| `simplify` | 简化复杂输出 |
| `skillify` | 将当前任务转换为可复用 skill |
| `claudeApi` | Claude API 内容生成 |
| `claudeInChrome` | Chrome 集成 |
| `keybindings` | 快捷键管理 |
| `scheduleRemoteAgents` | 远程 agent 调度 |
| `loremIpsum` | 占位文本生成 |

### NGO — Skill Manager (独立模块)

NGO 的 Skill 系统通过 `SKILL.md` frontmatter + 文件系统管理：
- REST API: `/skills/list` `/skills/read` `/skills/refresh` `/skills/delete`
- gRPC: `ListSkills` `ReadSkillContent` `RefreshSkills` `DeleteSkill`
- 动态加载 + 热刷新
- Prompt 注入 (匹配 trigger → 注入 SKILL.md 内容)

---

## 9. 差距总览矩阵

| 维度 | CC | NGO | 评估 |
|------|-----|-----|------|
| **交互模型** | CLI REPL | Multi-channel Daemon | ≡ 根本不同 |
| Slash commands | ✅ 66 个 | ⚠️ 12 个 | ⚠️ |
| Terminal 渲染 | ✅ Ink/React | ❌ 无 CLI | — (架构不同) |
| Vim mode | ✅ | ❌ | — |
| Voice input | ✅ | ❌ | — |
| Theme/Color | ✅ `/color` `/theme` `/stickers` | ❌ (WebUI 主题) | — |
| **WebSocket** | ❌ | ✅ 持久连接 + auto-wake | ✅ **NGO独有** |
| **REST API** | ❌ | ✅ 40+ routes (509行) | ✅ **NGO独有** |
| **gRPC** | ❌ | ✅ 40+ RPCs (714行) | ✅ **NGO独有** |
| Proto 合约 | ❌ | ✅ agent_service.proto | ✅ **NGO独有** |
| **Telegram Bot** | ❌ | ✅ 完整集成 (588行) | ✅ **NGO独有** |
| Bot 审批 | ❌ | ✅ InlineKeyboard | ✅ **NGO独有** |
| **统一 API Facade** | ❌ (分散) | ✅ AgentAPI 接口 | ✅ **NGO独有** |
| **Cron 管理** | ❌ | ✅ CRUD + logs (REST+gRPC) | ✅ **NGO独有** |
| KI 管理 API | ❌ (CLI 工具) | ✅ CRUD (REST+gRPC) | ✅ **NGO独有** |
| Brain 管理 API | ❌ | ✅ list/read | ✅ **NGO独有** |
| Provider 热管理 | ❌ | ✅ add/remove (REST+gRPC) | ✅ **NGO独有** |
| **Skills 内置** | ✅ 20 个 bundled | ⚠️ 文件系统+Trigger | ⚠️ |
| Git 集成命令 | ✅ /commit /diff /review /branch | ❌ | ⚠️ |
| diag 命令 | ✅ /doctor /heapdump /stats | ⚠️ /status | ⚠️ |
| 会话恢复 | ✅ /resume --resume | ❌ (session 列表) | ⚠️ |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **Git 集成命令** — 增加 `/commit` `/diff` 等 slash command，利用 agent 工具执行 git 操作并格式化输出
2. **诊断命令** — 增加 `/doctor` 命令：检查 LLM 连通性、配置有效性、环境依赖、磁盘使用等
3. **Cost 追踪** — 增加 `/cost` 命令：显示累计 token 消耗和估算费用 (利用 ModelPolicy.PriceInput1K)

### P1 — 架构增强

4. **HTTP API SDK** — 为 REST API 生成 TypeScript/Python SDK 包 (基于 OpenAPI spec + 代码生成)
5. **会话恢复增强** — 支持 `resume` 语义：WebSocket 重连后自动恢复到上次会话 + 重放最近 N 条消息
6. **Skills Bundled 模式** — 引入类似 CC 的 `loop` `stuck` `debug` 等内建 skill，无需用户手动安装

### P2 — 长期演进

7. **WeChat/Discord Bot** — 复用 Telegram Bot 的 StreamHandler 架构，添加新的 Bot 适配器
8. **GraphQL API** — 为 WebUI 提供 GraphQL 查询 (替代多次 REST 调用的 over-fetching)
9. **CLI 客户端** — 基于 gRPC 构建轻量 CLI 客户端，实现 CC 式终端交互
