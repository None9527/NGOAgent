# NGOAgent 内核架构设计报告

## 目录
1. [架构概览](#1-架构概览)
2. [领域实体模型 (Entity)](#2-领域实体模型)
3. [状态机定义 (State Machine)](#3-状态机定义)
4. [核心引擎与运行机制 (Agent Loop & ReAct)](#4-核心引擎与运行机制)
5. [安全守护与资源控制 (Guard & Security)](#5-安全守护与资源控制)
6. [API 与外部适配层 (Ports & API)](#6-api-与外部适配层)

---

## 1. 架构概览

NGOAgent 使用基于领域驱动设计 (DDD) 和整洁架构 (Clean Architecture) 的分层结构。内核代码主要集中在 `internal/` 目录下，严格区分了领域逻辑、应用控制与基础设施。

### 1.1 分层结构
- **Domain Layer (`internal/domain`)**：包含最核心的业务逻辑，不依赖于任何外部存储或框架。
  - `entity/`: 基础数据结构与模型（如 Conversation, Message, Skill）。
  - `port/`: 与外部基础设施交互的接口定义（依赖倒置），如 `LLMProvider`, `HistoryStore`。
  - `service/`: 服务层，包含 AgentLoop 核心执行引擎。
- **Application Layer (`internal/application`)**：包含应用的门面层（Facade）。
  - `api.go`: `AgentAPI` 提供了统一的 API 层抽象。所有的接入端（CLI, HTTP, gRPC）均通过这个结构体进入内核逻辑。
- **Infrastructure Layer (`internal/infrastructure`)**：存储、LLM 以及工具的底层实现。

### 1.2 核心特性
- **10 态状态机**：Agent 运行不依赖线性长代码，而是基于严密的 10 种状态扭转。
- **动态上下文注入**：运用 EPHEMERAL_MESSAGE 进行无痕拦截与短时干预。
- **双重锁与池化**：支持单例循环防并发，以及基于 Session 的 `LoopPool` 多路复用。

---

## 2. 领域实体模型

实体层 (`internal/domain/entity/entity.go`) 沉淀了系统中轻量化但不可或缺的结构。

- **Conversation & Message**
  消息被严格限定了 `Role` 体系（system / user / assistant / tool），其中 `Message` 天然携带了对 `ToolCallID` 和并发生成的 `Reasoning` 的支持，迎合了最新的模型推理标准。
- **Skill**
  管理插件化能力的基石，包含类型 (`pipeline` 等) 与激活权重 (`light`, `heavy`)。近期演进中加入了 `KIRef` 属性，与全局知识索引直接映射。
- **EvoRun**
  用于进化模型在后台异步重试、迭代的追踪态实体。

---

## 3. 状态机定义 (State Machine)

NGOAgent 的核心是一个 10 态状态机 (`StateIdle` 到 `StateEvaluating`)，取代了传统的线性长代码执行流，支持严密的流转与断点恢复：
- **`StateIdle` (0)**: 空闲，等待用户输入。
- **`StatePrepare` (1)**: 构建系统 Prompt 及注入动态上下文（如计划/规则）。
- **`StateGenerate` (2)**: 发起 LLM 流式调用，支持自动的 Token 微型压缩。
- **`StateToolExec` (3)**: 执行模型请求的工具集合。
- **`StateGuardCheck` (4)**: 执行行为护栏校验（检查 MaxSteps、上下文上限等）。
- **`StateCompact` (5)**: 上下文压缩，分级抛弃过长记忆。
- **`StateError` (6)/`StateFatal` (7)**: 错误重试与致命降级控制。
- **`StateDone` (8)**: 单轮交互完成。
- **`StateEvaluating` (9)**: （Evo 保留）用于后台评估代理逻辑质量。

---

## 4. 核心引擎与运行机制 (Agent Loop & ReAct)

`AgentLoop` （位于 `run.go`）包揽了真正的业务执行编排。

### 4.1 多级上下文防爆 (Context Overflow Recovery)
系统监测 LLM Token 使用率：
- 当上下文达到 70% 警告线，自动进入 `StateCompact` 实施工具级的减负（Tool-Heavy Compact），尝试把长工具输出转为精简形式。
- 当突破 95% 时，采取 Level 3 应对，强制实施 `forceTruncate`（极端截断至末尾少量交互）。

### 4.2 工具并/串行混合调度 (Mixed Batch Tool Execution)
位于 `tool_exec.go` 的逻辑区分工具是否包含副作用，实装了混合批处理：
1. 分析并抽离**ReadOnly / Network**类工具（无副作用）。
2. 将抽离的读工具分派给 Goroutine 并发执行（通过 `sync.WaitGroup` 同步）。
3. 剩余具有写入冲突风险的工具做串行执行调度，最大化吞吐效率。

### 4.3 动态 Overlay 注入
代理运行时引擎会在每次响应前，使用 `ActivateOverlays` 基于当前的工作目录资产及用户指令动态覆盖增强配置（如增加 coding 细节防呆提示），调整系统的指令侧重重点。

---

## 5. 安全守护与资源控制 (Guard & Security)

`BehaviorGuard` 机制是 NGOAgent 相对于普通大模型应用具有深度防御特性的关键体现。

### 5.1 双层防御体系
- **Turn-level（单轮响应级）**：追踪最新多次代理回复，利用 N-gram Jaccard 计算相似度以防范大模型的“无脑道歉复读”。当命中相似性风控，启用梯度干涉（提醒 -> 强烈警告并载入 `_loop_breaker` 救援策略 -> 无效后由内核终止）。
- **Step-level（工具单步级）**：利用工具执行前/后置探测器进行逻辑控制。例如防止未建立 `plan.md` 之前就贸然使用 `edit_file` 去肆意修改代码。

### 5.2 策略型拦截反馈
一旦判定工具访问危险，不会将请求发出而是拦截，使用 `[BLOCKED_BY_POLICY]` 通知拦截面；同时虚构一条文本向大模型抛出反馈，诱导大模型修改方式执行。

---

## 6. API 与外部适配层 (Ports & API)

应用门面层主要位于 `api.go` 的 `AgentAPI`：
- **资源抽象与锁定**：对外隔绝了并发细节。提供统一流接口和 `TryAcquire()` 与 `ReleaseAcquire()` 的原子操作保障同一个 SessionID 的处理循环绝对隔离。
- **池化设计互通**：将 Web/CLI 请求转换为池分配行为 `loopPool.Get(sessionID)`，利用 LRU 进行生命期衰减，不再使用短时创建导致下文丢失的范例。
- **历史缝合**：挂载持久层数据复苏机制（含对于已缓存媒体库附件重加载功能），以最干净的方式保证每一次请求宕机重开都是断点续租状态。
