# NGOAgent Graph Runtime 设计草案

> **状态**: Draft  
> **更新时间**: 2026-04-08  
> **关联文档**: [system_evolution_roadmap.md](/home/none/ngoclaw/NGOAgent/docs/system_evolution_roadmap.md)  
> **目标**: 为 `P0 Graph Runtime` 与 `P1 Checkpoint Persistence` 提供可开工的实现设计

---

## 一、设计目标

本设计解决的是 runtime 升维问题，不是单点功能增强。

当前系统的问题不是“没有 enough tools”，而是：

- 执行流硬编码在 `runInner()` 的 switch 中
- Session 恢复只恢复 history，不恢复运行态
- approval / barrier / wake / compact / repair 等流程缺统一执行模型
- 运行态分散在 `AgentLoop` 字段与局部结构里，无法序列化为稳定 checkpoint

本设计的目标是引入一套最小 Graph Runtime，使 NGOAgent 具备：

- 可声明的执行图
- 可持久化的执行游标
- 可恢复的运行态
- 节点级输入输出契约
- 后续扩展 reflection / A2A / event-trigger 的稳定承载面

---

## 二、非目标

第一阶段不做以下内容：

- 不做图形化 DAG 编辑器
- 不做跨机器分布式调度
- 不做完整 LangGraph 对标能力
- 不在第一版引入复杂 DSL
- 不立即拆完所有 plugin/toolpack
- 不在第一版重写所有工具执行逻辑

第一阶段只做一件事：

> 用 Graph Runtime 承载现有 `Prepare / Generate / ToolExec / GuardCheck / Compact / Done` 逻辑，并让它支持 checkpoint-resume。

---

## 三、现状映射

当前主循环入口：

- [api.go](/home/none/ngoclaw/NGOAgent/internal/application/api.go) `ChatStream()`
- [run.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run.go) `RunWithoutAcquire() -> runInner()`

当前状态处理：

- [state.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/state.go)
- [run_states.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run_states.go)

当前可复用资产：

- `doPrepare()`
- `handleGenerate()`
- `handleToolExec()`
- `handleGuardCheck()`
- `handleCompact()`
- `handleDone()`

第一阶段不应重写这些逻辑，而应把它们包装成 graph node。

---

## 四、核心概念

### 1. GraphDefinition

描述一张执行图的静态结构。

建议字段：

```go
type GraphDefinition struct {
	ID        string
	Version   string
	EntryNode string
	Nodes     map[string]Node
	Edges     []Edge
}
```

要求：

- 节点命名稳定，可用于 checkpoint 恢复
- 版本号明确，用于未来迁移旧快照
- 第一阶段支持单 entry point 即可

### 2. Node

执行图中的最小运行单元。

建议接口：

```go
type Node interface {
	Name() string
	Kind() NodeKind
	Execute(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error)
}
```

说明：

- `RuntimeContext` 提供 services、deps、logger、clock、registry 等运行时依赖
- `TurnState` 是节点读写的主要状态载体
- 节点不直接控制跳转，只返回 `NodeResult`

### 3. Edge

描述节点间转移规则。

建议结构：

```go
type Edge struct {
	From      string
	To        string
	Condition string
	Priority  int
}
```

第一阶段建议：

- `Condition` 不用做成表达式语言
- 先用枚举式 condition key，例如 `ok`, `has_tool_calls`, `needs_compact`, `done`, `fatal`
- 由 `NodeResult.RouteKey` 决定匹配哪条边

### 4. ExecutionCursor

描述当前执行位置。

建议结构：

```go
type ExecutionCursor struct {
	GraphID      string
	GraphVersion string
	CurrentNode  string
	Step         int
	RouteKey     string
}
```

要求：

- checkpoint 里必须持久化 cursor
- cursor 是 execution resume 的核心，不可省略

### 5. NodeResult

节点执行返回结果。

建议结构：

```go
type NodeResult struct {
	RouteKey         string
	Status           NodeStatus
	StateMutations   []StateMutation
	NeedsCheckpoint  bool
	WaitReason       WaitReason
	OutputSchemaName string
}
```

第一阶段简化原则：

- `RouteKey` 决定下一条边
- `Status` 用于区分 `continue / wait / complete / fatal`
- `NeedsCheckpoint` 控制是否立即刷盘

---

## 五、状态模型

### 1. 状态分层原则

不要再把所有运行态都挂在 `AgentLoop` 私有字段上。  
需要拆成以下三层：

### 2. SessionState

跨多个 turn 持续存在的状态。

建议包含：

- `SessionID`
- `ConversationHistory`
- `SessionMetadata`
- `ActiveOverlays`
- `MemoryPointers`
- `TokenUsageSummary`

特点：

- 可长期持久化
- 与单次执行图不强绑定

### 3. TurnState

单次用户请求对应的一次执行态。

建议包含：

- `RunID`
- `UserMessage`
- `Attachments`
- `Ephemerals`
- `TaskState`
- `Mode`
- `CurrentPlan`
- `LastLLMResponse`
- `ToolCalls`
- `ToolResults`
- `OutputDraft`
- `CompactState`
- `ReflectionState`

特点：

- checkpoint 恢复的核心状态
- graph node 主要读写对象

### 4. ExecutionState

描述调度与控制面的运行态。

建议包含：

- `Cursor`
- `StartedAt`
- `UpdatedAt`
- `Status`
- `PendingApproval`
- `PendingBarrier`
- `PendingWake`
- `RetryState`
- `ContinuationState`
- `LastError`

特点：

- 负责“执行到哪了”和“为什么卡住”
- 与业务内容分离

### 5. Durable / Ephemeral 划分

必须持久化：

- `ExecutionCursor`
- `PendingApproval`
- `PendingBarrier`
- `RetryState`
- `CurrentPlan`
- `ToolCalls` 与已完成 `ToolResults`
- 当前 turn 的关键中间产物引用

可以不持久化或按需重建：

- logger
- provider clients
- live channels
- in-memory callback closures
- streaming sink handles

---

## 六、Checkpoint 设计

### 1. 为什么不能继续只用 HistoryStore

当前 [history.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/persistence/history.go) 只适合做对话记录。  
它不表达：

- 当前执行节点
- 待审批状态
- 屏障等待状态
- 当前 run 的局部状态

所以必须新增独立存储，而不是继续向 `HistoryMessage` 塞字段。

### 2. 建议数据模型

建议新增：

```go
type RunSnapshot struct {
	RunID           string
	SessionID       string
	GraphID         string
	GraphVersion    string
	Status          string
	CursorJSON      string
	TurnStateJSON   string
	ExecStateJSON   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
```

如果要支持多次恢复与审计，可再拆：

- `run_snapshots` 当前最新态
- `run_snapshot_events` 增量事件流

第一阶段建议先做 latest snapshot，不做 event sourcing。

### 3. Checkpoint 触发时机

必须刷 checkpoint 的时机：

- 节点执行完成且产生 durable state mutation 后
- 进入 wait 状态前
- approval 发起后
- subagent barrier 建立后
- compact 完成后
- run 完成或 fatal 退出时

可延后刷盘：

- 单纯 text delta streaming
- 无状态日志更新

### 4. 恢复语义

恢复流程建议：

1. 读取最新 `RunSnapshot`
2. 校验 `GraphID + GraphVersion`
3. 反序列化 `TurnState + ExecutionState`
4. 重建 runtime dependencies
5. 从 `ExecutionCursor.CurrentNode` 继续执行

如果版本不兼容：

- 拒绝恢复
- 标记 snapshot stale
- 允许 fallback 到 history-only resume

---

## 七、节点映射方案

第一阶段建议把现有 FSM 直接映射成以下节点：

| 旧状态 | 新节点 | 说明 |
|---|---|---|
| `StatePrepare` | `PrepareNode` | 组装 prompt deps、激活 overlays、准备回合上下文 |
| `StateGenerate` | `GenerateNode` | 调 LLM，解析 tool calls / content / stop reason |
| `StateToolExec` | `ToolExecNode` | 处理并发只读工具与串行写工具 |
| `StateGuardCheck` | `GuardCheckNode` | 判断 compact / continue / done |
| `StateCompact` | `CompactNode` | 执行压缩并更新 state |
| `StateDone` | `DoneNode` | 完成收尾、hook、持久化 |

可选保留为内部子路径：

- `ErrorHandling`
- `Retry`
- `AutoContinue`
- `EvoAsyncDispatch`

第一阶段图结构基本仍是线性的，但“线性 graph”和“硬编码 FSM”不是一回事。  
前者已经具备未来加边、加节点、插 reflection 的能力。

---

## 八、运行时接口建议

### 1. GraphRuntime

```go
type GraphRuntime interface {
	Run(ctx context.Context, req RunRequest) error
	Resume(ctx context.Context, runID string) error
}
```

### 2. SnapshotStore

```go
type SnapshotStore interface {
	Save(ctx context.Context, snap *RunSnapshot) error
	LoadLatest(ctx context.Context, runID string) (*RunSnapshot, error)
	Delete(ctx context.Context, runID string) error
}
```

### 3. RuntimeContext

```go
type RuntimeContext struct {
	Deps          Deps
	Session       *SessionState
	Execution     *ExecutionState
	SnapshotStore SnapshotStore
}
```

原则：

- 节点拿到的是 `RuntimeContext`，不是整个 `AgentLoop`
- `AgentLoop` 后续应降级为兼容层或 facade

---

## 九、兼容迁移策略

### 第一阶段最重要的原则

不要一边做 Graph Runtime，一边把所有行为一起改掉。  
迁移必须分层进行。

### 建议迁移路径

1. 新建 `graphruntime` 包，不影响现有 `AgentLoop`
2. 用 adapter node 包装现有 `doPrepare/handleGenerate/...`
3. 让 `ChatStream()` 先支持切换到新 runtime
4. 新 runtime 跑通后，再逐步瘦身旧 `runInner()`
5. 最后把旧 FSM 降为 compatibility path 或删除

### 不建议的做法

- 不要直接在 `runInner()` 里继续加 if-else 模拟 graph
- 不要先做 A2A 再补 checkpoint
- 不要把 checkpoint 继续塞进 brain artifact 文本文件

---

## 十、最小可交付切片

### Slice 1: 线性图承载化

交付：

- `GraphDefinition`
- `Node`
- `Edge`
- `ExecutionCursor`
- `GraphRuntime.Run`
- 6 个 adapter nodes

验收：

- 现有主流程可不改行为地跑在 graph runtime 上

### Slice 2: Snapshot 持久化

交付：

- `RunSnapshot`
- `SnapshotStore`
- wait/approval/barrier 前 checkpoint

验收：

- kill 进程后可恢复到 pending execution

### Slice 3: Chat Resume 升级为 Execution Resume

交付：

- API 优先恢复 snapshot，再 fallback history

验收：

- `ChatStream()` 对活跃 run 可从 cursor 继续，而不是重新从用户消息起跑

### Slice 4: Reflection 插槽预留

交付：

- graph 中可插入 `ReflectionNode`

验收：

- 不要求默认开启，但必须具备插拔式接入能力

---

## 十一、验收标准

Graph Runtime 第一阶段完成的标准不是“代码更优雅”，而是下面这些事情成立：

- 同一条主流程可以通过 graph 定义运行
- 当前执行位置可以被稳定持久化
- approval / barrier / compact 之后可以恢复
- 新增节点不需要改 `runInner()` 的核心 switch
- API 层可以区分 history-only resume 和 execution resume

如果这五条没有同时成立，就说明还没有真正完成 runtime 升级。

---

## 十二、建议的实现顺序

建议开发顺序：

1. `graphruntime` 基础结构
2. adapter nodes
3. snapshot store
4. API resume path 改造
5. approval / barrier checkpoint
6. reflection slot

明确不建议的顺序：

1. A2A
2. 动态工具发现
3. 图形化编辑器
4. 分布式调度

这些都应建立在 Graph Runtime 稳定之后。

---

## 十三、后续文档建议

本设计稿之后，建议继续补两份短文档：

- `graph_runtime_api_contract.md`
  - 列出接口、结构体字段、序列化约束
- `graph_runtime_migration_plan.md`
  - 列出从 `AgentLoop` 到 `GraphRuntime` 的分批改造步骤

做到这一步，就足够正式开工。
