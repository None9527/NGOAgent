# NGOAgent — 架构设计方案

Go 语言实现的自主式 AI Agent 后端核心 + React/TypeScript 前端 WebUI。

## 一、设计目标

| 目标 | 方案 |
|------|------|
| 避免 God interface | **组合式小接口**（5 个 < 10 method 的接口）|
| 避免 init 顺序 panic | **延迟注入 + Builder 模式**，运行时校验 |
| 零死代码 | **模块化 — 独立包，不用就不编译** |
| 清晰的代码组织 | **一个 struct 一个文件，方法按职责拆到独立文件** |
| 单一入口 | **cmd/ngoagent 纯后端，CLI/Bot 通过 gRPC 连接** |
| Proto 单一来源 | **api/proto/ 统一输出路径** |

## 二、核心技术要素

- ✅ 4D Checkpoint 压缩（User Intent / Session Summary / Code Changes / Learned Facts）
- ✅ Ephemeral Message 注入（task reminder, mode transition, edit verification）
- ✅ PLANNING → EXECUTION → VERIFICATION 三模式状态机
- ✅ KI 知识系统（全局跨会话知识蒸馏）
- ✅ 21 核心工具（Bash, Read, Write, Edit, Glob, Grep, WebSearch, Task, Agent, Cron, Forge, Notify 等）
- ✅ 3 层 Permission（Allow / Auto / Ask）
- ✅ FileEditTool 的 9 种错误状态处理 + 模糊匹配降级
- ✅ Prompt Assembly Pipeline
- ✅ BehaviorGuard（规则守卫 + ViolationTracker）
- ✅ Token-only Compaction 触发
- ✅ Security Hook 链 + 审批机制

---

## 三、目录结构

```
ngoagent/
├── cmd/
│   └── ngoagent/
│       └── main.go              # 入口: Builder.Build() → Serve()
│
├── internal/
│   ├── domain/service/          # 核心引擎 (DDD Domain)
│   │   ├── loop.go              # DeltaSink 接口 + LoopState
│   │   ├── run.go               # AgentLoop 主循环 + 工具执行
│   │   ├── delta.go             # Delta struct (DeltaSink 实现)
│   │   ├── runstate.go          # DeltaEvent + ToolCallDelta
│   │   ├── channel.go           # Channel 抽象 + LogSink
│   │   ├── guard.go             # BehaviorGuard 4 条规则
│   │   └── factory.go           # RunDeps 构建
│   │
│   ├── infrastructure/
│   │   ├── config/              # YAML 配置 + 热更新
│   │   ├── llm/                 # LLM Provider 路由
│   │   │   └── openai/          # OpenAI-compatible
│   │   ├── prompt/              # Prompt Assembly
│   │   │   └── prompttext/      # 所有提示词文本
│   │   ├── tool/                # 工具系统
│   │   ├── sandbox/             # 进程沙箱
│   │   ├── security/            # 安全决策链
│   │   ├── persistence/         # SQLite + GORM
│   │   ├── brain/               # 会话 Artifact
│   │   ├── knowledge/           # 跨会话知识
│   │   ├── skill/               # 技能系统
│   │   ├── mcp/                 # MCP 集成
│   │   ├── workspace/           # 项目知识
│   │   └── cron/                # 定时任务引擎
│   │
│   ├── application/             # API 层 (Builder + AgentAPI)
│   └── interfaces/
│       ├── server/              # HTTP Server (SSE)
│       └── grpc/                # gRPC Server
│
├── webui/                       # React/TypeScript 前端 (14,700+ 行)
├── api/proto/                   # gRPC Protobuf 定义
└── docs/                        # 设计文档
```

> **代码规模**: Go 后端 ~32,500 行 / 89 文件，前端 ~14,700 行 / 80 文件。

## 四、核心接口设计（组合式，非 God interface）

```go
// 5 个小接口，按职责拆分

type ChatEngine interface {
    Chat(ctx context.Context, sessionID string, msg string, opts ChatOptions) (<-chan Delta, error)
    StopChat(sessionID string) error
}

type SessionManager interface {
    NewSession(ctx context.Context) (string, error)
    GetSession(ctx context.Context, id string) (*Session, error)
    ListSessions(ctx context.Context, opts ListOpts) ([]SessionSummary, error)
    DeleteSession(ctx context.Context, id string) error
}

type ModelManager interface {
    ListModels() []ModelInfo
    SwitchModel(sessionID string, modelID string) error
    GetCurrentModel(sessionID string) string
}

type ToolManager interface {
    ListTools() []ToolInfo
    EnableTool(name string) error
    DisableTool(name string) error
}

type SystemAdmin interface {
    GetStatus() SystemStatus
    GetConfig() map[string]any
    Restart() error
}
```

精简到 **5 个接口、共 ~15 个方法**。HTTP/gRPC handler 只依赖需要的接口（ISP 原则）。

## 五、初始化设计（Builder 模式）

```go
func main() {
    app, err := application.Build()
    // Build() 内部 8 个阶段按正确顺序初始化所有组件
    // 外部调用者不关心依赖顺序
    app.Server.Start(ctx)
}
```

**Build() 内部**按正确顺序注入，外部调用者不关心先后。
