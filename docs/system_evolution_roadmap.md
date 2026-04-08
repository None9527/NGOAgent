# NGOAgent 系统级进化路线图 (System Evolution Roadmap)

> **版本**: 3.0  
> **更新时间**: 2026-04-08  
> **定位**: 基于 GitNexus 与代码实态校验后的通用 Agent 演进路线图  
> **目标**: 将 NGOAgent 从“强工程化的代码助手”演进为“可编排、可恢复、可扩展的通用 Agent Runtime”

---

## 一、执行摘要

当前 NGOAgent 的优势不在编排层，而在以下几个方向：

- 记忆与知识治理已经形成体系化能力
- 多模态输入链路已经具备生产可用基础
- Prompt 组装、上下文裁剪、行为 Overlay 已经较成熟
- Evo 异步评估与修复闭环已经具备雏形
- MCP 与内建工具注册机制已经具备较好的扩展基础

但从通用 Agent 框架视角看，当前系统的核心短板也非常明确：

- 运行时内核仍然是线性 10-state FSM，而不是图执行引擎
- Session 恢复目前只恢复历史消息，不恢复执行中的运行态
- 尚未形成贯穿运行时的 Typed State Schema
- 结构化输出能力没有上升为统一 runtime 能力
- 编排层、领域能力、工具包边界仍然混合在同一装配层

结论很直接：

> NGOAgent 现在更接近“具备记忆、自修复和工具生态的高级 Agent 产品”，还不是“通用 Agent Runtime”。  
> 下一阶段唯一的一级阻塞项不是再加工具，而是补齐 Graph Runtime + Checkpoint Persistence。

配套设计稿：

- [graph_runtime_design.md](/home/none/ngoclaw/NGOAgent/docs/graph_runtime_design.md)

---

## 二、代码实态校验

本节结论基于 GitNexus 查询与源码交叉校验。

### 1. 当前内核不是 DAG Runtime，而是线性 FSM

证据：

- [state.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/state.go) 定义了 `StatePrepare -> StateGenerate -> StateToolExec -> StateGuardCheck -> StateCompact -> StateDone` 等固定状态。
- [run.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run.go) 中 `runInner()` 通过 `switch a.CurrentState()` 驱动整个回合执行。
- [run_states.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run_states.go) 将各状态拆分为 handler，但本质仍是固定状态机，不是可声明图。

这意味着当前系统支持的是：

- 单回合循环
- 有限回退
- 工具批处理并发
- 结束后异步后处理

而不原生支持：

- 显式条件分支图
- 子图复用
- 图级中断恢复
- 并行分支 join
- 节点级 checkpoint

### 2. 当前 Session 恢复不是执行恢复，只是 History 恢复

证据：

- [api.go](/home/none/ngoclaw/NGOAgent/internal/application/api.go) 的 `ChatStream()` 在 loop 内 history 为空时调用 `LoadAll -> RestoreHistory -> SetHistory`。
- [persistence_ops.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/persistence_ops.go) 只做 history append/full replace。
- [history.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/persistence/history.go) 持久化的核心对象仍是 `HistoryMessage`。

这意味着当前恢复能力只覆盖：

- 用户消息
- assistant 消息
- tool call 文本记录
- reasoning 文本
- multimodal attachment 引用

但不会恢复：

- 当前执行到哪个节点
- pending approval / waiting gate
- subagent barrier 状态
- pending ephemerals
- 运行模式与图游标
- 中间产物依赖关系

因此，当前系统具备 `chat resume`，不具备 `execution resume`。

### 3. Typed State Schema 尚未形成

证据：

- [loop.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/loop.go) 中运行态分散在 `AgentLoop` 的多个字段里。
- [run_states.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run_states.go) 另有局部 `runState`。
- `TaskTracker`、ephemerals、history、mode、token tracker 等状态由不同结构分别承载。

当前并不是完全动态的 `map[string]any` 模式，但也不是可组合的 graph state schema。  
它更接近“多组强类型字段并存”，而不是“统一的强类型状态流”。

### 4. 结构化输出存在局部实现，但不是系统级能力

证据：

- [registry.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/tool/registry.go) 的 schema 主要服务于工具参数定义。
- [evo_evaluator.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/evo_evaluator.go) 会要求模型输出 JSON，然后本地解析。
- 主 loop 中没有统一的 `response_format + schema validation + retry` 机制。

因此，“结构化输出已覆盖”这个判断不成立。  
更准确的说法是：系统已有若干局部结构化输出实践，但尚未沉淀成统一 runtime contract。

### 5. 自我反思能力已有异步雏形，但未进入主图执行

证据：

- [evo_controller.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/evo_controller.go) 在回合结束后异步触发评估与修复。
- [evo_evaluator.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/evo_evaluator.go) 输出结构化评估结果。

这说明系统已有“执行后评估”能力，但没有：

- 统一的 reflection node
- destructive action 前的同步审查点
- final answer 前的可配置审校节点

因此，`Self-Critique` 的表述应从“已有基础”调整为“已有异步后评估基础，缺同步图节点能力”。

### 6. 领域边界仍然混杂在装配层

证据：

- [builder.go](/home/none/ngoclaw/NGOAgent/internal/application/builder.go) 中直接注册 read/write/edit/git/diff/tree/http/MCP/skill 等大量工具。
- [coding.go](/home/none/ngoclaw/NGOAgent/internal/domain/profile/coding.go) 的 coding overlay 已经把软件开发行为规则显式化。
- [engine.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/prompt/engine.go) 默认 overlays 仍以 CodingOverlay 为默认激活集的一部分。

这说明 prompt 层已经开始做通用化，但 runtime 装配层仍明显偏向 coding assistant。

---

## 三、当前能力分层

### Ring 0: 运行时内核

当前具备：

- 单回合 AgentLoop
- 10-state FSM
- 工具执行、紧凑化、有限错误恢复
- 部分并发工具批处理

当前缺失：

- Graph Runtime
- Typed State Schema
- 节点级 checkpoint
- 图级恢复

### Ring 1: 智能层

当前具备：

- Overlay 驱动的行为差异化
- 记忆 / KI / Diary / Recall
- 异步 Evo 评估与 repair
- 多模态输入构建

当前缺失：

- 统一结构化输出 contract
- 同步 reflection node
- 面向任务复杂度的推理模式切换框架

### Ring 2: 编排层

当前具备：

- subagent spawn
- barrier / wake continuation
- cron / MCP / webhook 等外围触点

当前缺失：

- A2A 协议
- 事件驱动 runtime 入口抽象
- tool discovery as runtime capability

### Ring 3: 领域插件层

当前事实上已经存在：

- coding overlay
- git / diff / file / tree / browser-like tooling
- skill / script / MCP tool adapters

但还没有形成明确的 plugin packaging 与生命周期边界。

---

## 四、下一步演进优先级

### P0. Graph Runtime 内核化

这是唯一的一级阻塞项。

目标：

- 将当前 `runInner()` 的固定状态机重构为 Graph Runtime
- 保留现有状态处理逻辑，但迁移为 graph node
- 让执行流程从硬编码 switch 过渡为“图定义 + 执行器”

建议交付物：

- `Node` 接口
- `Edge` / `TransitionRule`
- `GraphDefinition`
- `ExecutionCursor`
- `GraphRuntime`
- `NodeResult`

第一阶段不追求功能爆炸，只做“旧逻辑的图承载化”。

### P1. Run Snapshot / Checkpoint Persistence

目标：

- 引入独立于 conversation history 的运行态持久化
- 支持跨重启恢复 pending execution
- 为 approval、barrier、auto-wake、tool continuation 提供统一恢复点

建议至少持久化：

- 当前 graph/node cursor
- 当前 turn 的 typed state
- pending approvals
- pending wakes / barrier handles
- mode / agent type / active overlays
- intermediate artifacts references
- token/context counters

没有这一层，DAG 也只是“更复杂但不可恢复的线性循环”。

### P2. Typed State Schema

目标：

- 将 `history + ephemerals + task + mode + runtime counters + orchestration state` 抽象为统一状态模型
- 明确区分 durable state 与 ephemeral state
- 为节点输入输出、schema 校验、checkpoint 序列化提供统一模型

建议方向：

- `TurnState`
- `SessionState`
- `ExecutionState`
- `ArtifactRefs`
- `ApprovalState`

### P3. Core / Plugin 拆层

目标：

- 将通用 runtime 与 coding-specific toolpacks 拆开
- 降低 `builder.go` 的装配耦合
- 让 NGOAgent 从“产品总装配”演进成“runtime + plugins”

建议结构：

- `core/runtime`
- `core/prompt`
- `core/orchestration`
- `plugins/coding`
- `plugins/browser`
- `plugins/media`
- `plugins/research`

### P4. 统一结构化输出

目标：

- 不再让 JSON parse 只存在于个别模块
- 让 graph node 可以声明 output schema
- 对输出失败自动重试、纠偏或降级

适用范围：

- planner node
- eval node
- repair node
- route selection node
- final report node

### P5. Reflection Node

目标：

- 将异步 Evo 能力前移一部分到同步主流程
- 在关键节点前增加审查点

优先插入点：

- destructive tool execution 前
- final answer 前
- plan commit 前

### P6. 编排层增强

在完成 P0-P5 后，再做下面这些才有意义：

- event trigger runtime abstraction
- A2A protocol
- dynamic tool discovery
- external agent delegation over RPC/gRPC

---

## 五、推荐阶段规划

### 阶段 α: Runtime 重构

目标：

- Graph Runtime 落地
- Typed State 初版
- Checkpoint Snapshot 初版

建议周期：

- 4-8 周

完成标志：

- 现有 `Prepare/Generate/ToolExec/GuardCheck/Compact/Done` 均可由 graph node 表达
- agent 可从 checkpoint 恢复到 pending execution

### 阶段 β: 智能层统一化

目标：

- 统一结构化输出
- Reflection node
- 将 Evo 的部分能力前移到主流程

建议周期：

- 1-3 周

完成标志：

- 关键节点输出均可 schema 校验
- destructive tool 前存在可配置审查节点

### 阶段 γ: 插件边界化

目标：

- core/runtime 与 coding plugin 解耦
- builder 从“全注册器”收缩为“runtime assembler”

建议周期：

- 1-2 周

完成标志：

- Git / Diff / 文件编辑等能力可以作为 plugin/toolpack 装载
- 默认 runtime 不再天然绑定 coding 场景

### 阶段 δ: 编排网络化

目标：

- A2A
- 事件驱动 session 启动
- 跨进程 agent delegation

建议周期：

- 2-4 周

完成标志：

- 外部 agent/runtime 可以发现、委托、回传结果

---

## 六、需要修正的旧判断

以下旧路线图表述建议废弃或调整：

- “A1 Workflow/DAG 执行引擎 [开发中]”
  - 应改为：尚未落地，仍处于架构级缺口
- “A2 会话级持久化快照 [优先做]”
  - 保留优先级，但应强调当前只有 history persistence，不是 runtime checkpoint
- “B3 通用结构化输出约束 [已覆盖]”
  - 应改为：仅局部存在，未统一为系统能力
- “C1 自动化验证闭环 [优先做]”
  - 可以保留，但它不应高于 Graph Runtime
- “B2 无头浏览器集群 [优先做]”
  - 对通用 Agent Runtime 不是一级优先项

---

## 七、最终判断

NGOAgent 当前最值钱的积累不是“更多工具”，而是：

- Memory / KI / Diary 治理
- Evo 异步评估与修复
- Overlay 驱动的行为差异化
- Prompt 装配与上下文预算能力
- MCP 与工具生态兼容性

但这些能力现在仍然挂载在一个线性 FSM 上。

因此，未来路线图的核心句子应该是：

> 先把运行时做成 Graph Runtime，再谈 A2A、动态工具发现和通用插件生态。  
> 没有 Graph Runtime 与 Checkpoint Persistence，其他增强都只是把当前产品能力继续堆厚，而不是把系统升维成通用 Agent 平台。

---

## 八、附录：高信号代码入口

- 运行时主循环: [run.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run.go)
- 状态定义: [state.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/state.go)
- 状态处理器: [run_states.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/run_states.go)
- 会话恢复入口: [api.go](/home/none/ngoclaw/NGOAgent/internal/application/api.go)
- 历史持久化: [persistence_ops.go](/home/none/ngoclaw/NGOAgent/internal/domain/service/persistence_ops.go)
- History 存储: [history.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/persistence/history.go)
- Prompt Overlay: [engine.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/prompt/engine.go)
- Behavior Overlay: [profile.go](/home/none/ngoclaw/NGOAgent/internal/domain/profile/profile.go)
- Coding Overlay: [coding.go](/home/none/ngoclaw/NGOAgent/internal/domain/profile/coding.go)
- MCP 管理: [manager.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/mcp/manager.go)
- MCP 工具适配: [mcp_adapter.go](/home/none/ngoclaw/NGOAgent/internal/infrastructure/tool/mcp_adapter.go)
- 系统装配入口: [builder.go](/home/none/ngoclaw/NGOAgent/internal/application/builder.go)
