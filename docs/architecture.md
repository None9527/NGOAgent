# NGOAgent — 架构设计方案

Go 语言新项目，基于 Antigravity / CC / NGOClaw 三套情报设计的 AI Agent 后端核心。

## 一、NGOClaw 的坑（必须避免）

| 坑 | 根因 | NGOAgent 方案 |
|----|------|-------------|
| 109-method Engine interface | 所有功能塞进一个 God interface | **组合式小接口**（5 个 < 10 method 的接口）|
| init 顺序 panic | 构造函数链依赖 → nil 指针 | **延迟注入 + Builder 模式**，运行时校验 |
| 29% 死代码 | 功能塞进去后废弃不删 | **模块化 — 独立包，不用就不编译** |
| LocalEngine 三文件拆分混乱 | 一个 struct 跨 3 文件 | **一个 struct 一个文件，方法按职责拆到独立文件** |
| Go CLI + Node CLI 共存 | 入口点耦合 | **Gateway 纯后端，CLI 是独立项目** |
| proto 重复两套 | 生成路径不一致 | **单一 proto 输出路径** |

## 二、三套情报的最佳实践提取

### 从 Antigravity 拿
- ✅ 4D Checkpoint 压缩（User Intent / Session Summary / Code Changes / Learned Facts）
- ✅ Ephemeral Message 注入（task reminder, mode transition, edit verification）
- ✅ PLANNING → EXECUTION → VERIFICATION 三模式状态机
- ✅ KI 知识系统概念

### 从 CC 拿
- ✅ 16 核心工具设计（Bash, Read, Write, Edit, Glob, Grep, WebSearch, Task, Agent）
- ✅ 3 层 Permission（Always Allow / Always Deny / Always Ask）
- ✅ 6 Agent Mode（default, plan, bypassPermissions, acceptEdits, delegate, dontAsk）
- ✅ FileEditTool 的 9 种错误状态处理

### 从 NGOClaw 搬（清理后迁移）
- ✅ 12-Section Prompt Assembly Pipeline
- ✅ BehaviorGuard（ClaimAlignment 中间件 + ViolationTracker）
- ✅ Token-only Compaction 触发（不按消息数）
- ✅ Model Policy per-model 覆写
- ✅ Security Hook 链 + CLI Approval 回调

---

## 三、目录结构

```
ngoagent/
├── cmd/
│   └── ngoagent/
│       └── main.go              # 唯一入口: ngoagent serve
│
├── internal/
│   ├── config/                  # 配置加载 (YAML → struct)
│   │   ├── config.go            # 主配置结构体
│   │   └── defaults.go          # 默认值
│   │
│   ├── engine/                  # 核心引擎层 (Domain)
│   │   ├── loop.go              # AgentLoop — ReAct 状态机
│   │   ├── loop_run.go          # Run 执行循环
│   │   ├── loop_generate.go     # LLM 调用 + planning 决策
│   │   ├── loop_execute.go      # 工具执行分发
│   │   ├── compaction.go        # 4D Checkpoint 压缩
│   │   ├── history.go           # HistoryStore (内存 + 持久化)
│   │   ├── state.go             # 10-state 状态机定义
│   │   ├── guardrails.go        # BehaviorGuard + ViolationTracker
│   │   ├── ephemeral.go         # Ephemeral message 注入
│   │   └── model_policy.go      # Per-model policy 覆写
│   │
│   ├── llm/                     # LLM 提供商 (Infrastructure)
│   │   ├── provider.go          # Provider interface
│   │   ├── router.go            # Multi-provider 路由
│   │   ├── openai/              # OpenAI-compatible (Bailian/MiniMax/Ollama)
│   │   │   ├── provider.go
│   │   │   └── sse.go
│   │   └── codex/               # Codex GPT-5.3
│   │       ├── provider.go
│   │       └── sse.go
│   │
│   ├── prompt/                  # Prompt 组装 (Infrastructure)
│   │   ├── engine.go            # 12-section 组装核心
│   │   ├── sections.go          # Section 定义 + 优先级
│   │   ├── variants.go          # Model-specific 变体
│   │   └── budget.go            # pruneByBudget 裁剪
│   │
│   ├── tool/                    # 工具系统 (Infrastructure)
│   │   ├── registry.go          # 工具注册中心
│   │   ├── executor.go          # 统一执行器
│   │   ├── file_ops.go          # read_file, write_file, edit_file
│   │   ├── bash.go              # run_command, command_status, send_input
│   │   ├── search.go            # glob, grep_search
│   │   ├── web.go               # web_fetch, web_search
│   │   ├── task_plan.go         # task_plan 工具
│   │   ├── memory.go            # save_memory 工具
│   │   ├── subagent.go          # spawn_agent 工具
│   │   ├── mcp.go               # MCP 桥接
│   │   └── edit_repair.go       # 编辑修复逻辑
│   │
│   ├── sandbox/                 # 进程沙箱
│   │   ├── sandbox.go           # Sandbox interface
│   │   └── process.go           # Process-based 实现
│   │
│   ├── security/                # 安全系统
│   │   ├── hook.go              # SecurityHook 链
│   │   ├── policy.go            # 权限策略
│   │   └── approval.go          # Approval 回调
│   │
│   ├── persistence/             # 持久化
│   │   ├── sqlite.go            # SQLite (GORM)
│   │   ├── conversation.go      # 会话存储
│   │   └── history.go           # 聊天历史
│   │
│   └── server/                  # 接口层
│       ├── grpc.go              # gRPC Server (Node CLI 兼容)
│       ├── grpc_rpcs.go         # RPC 实现
│       ├── http.go              # HTTP Server (Gin)
│       ├── openai_handler.go    # OpenAI 兼容 API
│       └── agent_handler.go     # 管理 API
│
├── pkg/                         # 公共库
│   ├── ctxutil/                 # Context 工具 (SessionID, TraceID)
│   ├── idgen/                   # ID 生成
│   └── stringutil/              # 字符串工具
│
├── proto/
│   └── agent.proto              # 唯一 proto 定义
│
├── go.mod
├── go.sum
└── 文件说明.md
```

## 四、核心接口设计（组合式，非 God interface）

```go
// engine/interfaces.go — 5 个小接口，按职责拆分

// ChatEngine 处理对话
type ChatEngine interface {
    Chat(ctx context.Context, sessionID string, msg string, opts ChatOptions) (<-chan Delta, error)
    StopChat(sessionID string) error
}

// SessionManager 管理会话
type SessionManager interface {
    NewSession(ctx context.Context) (string, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    ListSessions(ctx context.Context, opts ListOpts) ([]SessionSummary, error)
    DeleteSession(ctx context.Context, id string) error
}

// ModelManager 管理模型
type ModelManager interface {
    ListModels() []ModelInfo
    SwitchModel(sessionID string, modelID string) error
    GetCurrentModel(sessionID string) string
}

// ToolManager 管理工具
type ToolManager interface {
    ListTools() []ToolInfo
    EnableTool(name string) error
    DisableTool(name string) error
}

// SystemAdmin 系统管理
type SystemAdmin interface {
    GetStatus() SystemStatus
    GetConfig() map[string]any
    Restart() error
}
```

对比 NGOClaw 的 109-method `Engine` interface，精简到 **5 个接口、共 ~15 个方法**。HTTP/gRPC handler 只依赖需要的接口（ISP 原则）。

## 五、初始化设计（避免 init 顺序 panic）

```go
// cmd/ngoagent/main.go

func main() {
    cfg := config.Load("~/.ngoagent/config.yaml")
    
    // Builder 模式 — 顺序无关，最后 Build() 统一校验
    app, err := ngoagent.NewBuilder().
        WithConfig(cfg).
        WithLogger(logger).
        WithLLM(llmRouter).        // 先构建独立组件
        WithTools(toolRegistry).
        WithSandbox(sandbox).
        WithSecurity(securityHook).
        Build()                     // Build() 内部处理依赖注入顺序
    
    app.Serve()  // 启动 gRPC + HTTP
}
```

**Build() 内部**按正确顺序注入，外部调用者不关心先后。

## 六、从 NGOClaw 迁移策略

| 模块 | 策略 | 预估行数 |
|------|------|---------|
| Agent Loop (ReAct) | **清理后迁移** — 核心逻辑验证过，去掉注释和死代码 | ~800 |
| Compaction (4D) | **清理后迁移** | ~300 |
| Prompt Engine (12-section) | **重写** — 原版 906 行过大，拆成 4 文件 | ~600 |
| Tool 系统 | **精选迁移** — 保留 10 个核心工具 | ~2000 |
| LLM Router | **清理后迁移** — openai + codex | ~800 |
| Security Hook | **清理后迁移** | ~300 |
| gRPC Server | **重写** — 原版 command_rpcs 1208 行 | ~500 |
| HTTP Server | **清理后迁移** | ~400 |
| Persistence | **清理后迁移** | ~500 |
| Config | **重写** — 简化 | ~200 |
| **预估总计** | | **~6,400** |

目标：从 51,695 行 → **~6,500 行**，减 87%。

## 七、执行策略

1. **Phase 1 (骨架)**: `go mod init` + 目录 + config + logger
2. **Phase 2 (核心)**: Agent Loop + LLM + Prompt + Compaction + History — 核心工作
3. **Phase 3 (工具)**: 10 个核心工具迁移
4. **Phase 4 (接口)**: gRPC + HTTP，复用 Node CLI proto
5. **Phase 5 (安全)**: Security Hook + Sandbox
6. **Phase 6 (验证)**: build + test + E2E

每个 Phase 完成后 `go build` + smoke test，绝不跳步。
