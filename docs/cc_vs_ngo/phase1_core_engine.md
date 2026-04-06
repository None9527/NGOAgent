# Phase 1：核心引擎层深度对标分析

> Claude Code `QueryEngine + query.ts + Tool.ts + context.ts + cost-tracker.ts`
> vs NGOAgent `run.go + state.go + token_tracker.go`

---

## 目录

- [1. 架构概览对比](#1-架构概览对比)
  - [1.1 CC 架构：AsyncGenerator 流式管道](#11-cc-架构asyncgenerator-流式管道)
  - [1.2 NGO 架构：显式有限状态机](#12-ngo-架构显式有限状态机)
  - [1.3 架构设计哲学差异](#13-架构设计哲学差异)
- [2. Agent Loop 主循环对比](#2-agent-loop-主循环对比)
  - [2.1 CC query() 循环](#21-cc-query-循环)
  - [2.2 NGO runInner() 循环](#22-ngo-runinner-循环)
  - [2.3 循环控制流差异矩阵](#23-循环控制流差异矩阵)
- [3. 工具执行对比](#3-工具执行对比)
  - [3.1 CC 工具执行：StreamingToolExecutor + runTools](#31-cc-工具执行streamingtoolexecutor--runtools)
  - [3.2 NGO 工具执行：串行 doToolExec](#32-ngo-工具执行串行-dotoolexec)
  - [3.3 差距评级](#33-差距评级)
- [4. Token 估算与成本追踪对比](#4-token-估算与成本追踪对比)
  - [4.1 CC cost-tracker.ts](#41-cc-cost-trackersts)
  - [4.2 NGO TokenTracker](#42-ngo-tokentracker)
  - [4.3 差距评级](#43-差距评级)
- [5. 上下文窗口管理对比](#5-上下文窗口管理对比)
  - [5.1 CC 7 层压缩管道](#51-cc-7-层压缩管道)
  - [5.2 NGO 3 级防御](#52-ngo-3-级防御)
  - [5.3 差距评级](#53-差距评级)
- [6. 会话持久化对比](#6-会话持久化对比)
- [7. 错误处理与恢复对比](#7-错误处理与恢复对比)
  - [7.1 CC 错误恢复矩阵](#71-cc-错误恢复矩阵)
  - [7.2 NGO 错误处理](#72-ngo-错误处理)
  - [7.3 差距评级](#73-差距评级)
- [8. Feature Flag 系统](#8-feature-flag-系统)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. 架构概览对比

### 1.1 CC 架构：AsyncGenerator 流式管道

CC 的核心引擎由两层 AsyncGenerator 组成：

```
QueryEngine.submitMessage()
    │
    ├──► processUserInput()       // 输入预处理 + slash 命令解析
    ├──► fetchSystemPromptParts() // System prompt 组装
    ├──► query()                  // 核心循环入口
    │       │
    │       └──► queryLoop()      // while(true) 无限循环
    │               │
    │               ├──► applyToolResultBudget()    // 工具结果预算
    │               ├──► snipCompactIfNeeded()       // Snip 压缩
    │               ├──► deps.microcompact()          // 微压缩
    │               ├──► applyCollapsesIfNeeded()     // 上下文折叠
    │               ├──► deps.autocompact()            // 自动压缩
    │               ├──► deps.callModel()              // API 调用（流式）
    │               │       └── for await (message of stream)
    │               │               ├── yield message → QueryEngine
    │               │               └── StreamingToolExecutor.addTool()
    │               ├──► runTools() / getRemainingResults()  // 工具执行
    │               ├──► getAttachmentMessages()       // 附件注入
    │               ├──► handleStopHooks()              // 停止钩子
    │               └──► state = next; continue          // 循环继续
    │
    └──► yield result
```

**关键特征**：
- **AsyncGenerator (`async function*`)** 作为核心通信原语 — 所有消息通过 `yield` 流出
- **无显式状态机** — 控制流由 `while(true) + continue/return` 语义表达
- `QueryEngine` 是 thin wrapper，负责 SDK 协议适配（消息格式、transcript 持久化、usage 累计）
- `query()` → `queryLoop()` 是真正的引擎，纯函数式（所有副作用通过 `deps` 注入）

### 1.2 NGO 架构：显式有限状态机

```
AgentLoop.Run()
    │
    ├──► runMu.TryLock()           // 并发互斥
    └──► runInner()
            │
            for {
              switch CurrentState() {
              ├── StatePrepare   → doPrepare() → Generate
              ├── StateGenerate  → doGenerate() → ToolExec | Done
              │       ├── PromptEngine.Assemble()
              │       ├── provider.GenerateStream()
              │       └── BehaviorGuard.Check()
              ├── StateToolExec  → doToolExec() (串行循环) → GuardCheck
              │       ├── Security.BeforeToolCall() (4 级决策)
              │       ├── Hooks.FireBeforeTool()
              │       ├── safeToolExec() (panic 恢复)
              │       └── Hooks.FireAfterTool()
              ├── StateGuardCheck → 3 级上下文防御 → Generate | Compact | Done
              ├── StateCompact   → doCompact() → Generate
              ├── StateDone      → persistHistory() + fireHooks
              │       └── pendingWake → auto-continue
              └── StateFatal     → return error
              }
            }
```

**关键特征**：
- **显式 10 态状态机** (`state.go`) + `CanTransition()` 合法性校验
- **`sync.Mutex` 保护** 所有共享状态（history, ephemerals, pendingMedia）
- **SSE Delta** 接口驱动前端更新 (`OnText`, `OnToolStart`, `OnToolResult`)
- Go goroutine 天然适合后台任务（`go a.fireHooks()`）

### 1.3 架构设计哲学差异

| 维度 | CC | NGO |
|------|-----|-----|
| **控制流** | AsyncGenerator 隐式 | 显式 FSM + switch |
| **类型安全** | TypeScript 联合类型 | Go 接口 + 值类型 |
| **并发模型** | 单线程事件循环 + Promise | goroutine + Mutex |
| **副作用注入** | `deps: QueryDeps` 依赖注入 | `a.deps` 构造器注入 |
| **消息协议** | yield SDKMessage (结构化) | Delta.OnText/OnTool (事件) |
| **状态管理** | `let state: State` 局部变量 | `a.state` 实例字段 |
| **可测试性** | `deps` mock 替换即可 | 需要完整 DI 容器 |

**评价**：两种架构各有优势。CC 的 Generator 管道更适合流式场景和组合；NGO 的显式 FSM 更易于调试和推理状态转移。但 CC 的 `deps` 注入模式在可测试性上明显更优。

---

## 2. Agent Loop 主循环对比

### 2.1 CC query() 循环

`query.ts:219-1729` — **1511 行**，单函数实现完整 agent loop。

**循环一次迭代的执行流程**：

```
1. [PreProcessing] 工具结果预算 → Snip → Microcompact → 上下文折叠 → AutoCompact
2. [API Call]       callModel() — 流式接收 + StreamingToolExecutor 并发启动
3. [PostStreaming]  收集 tool_use blocks → 处理被扣留的错误 (prompt-too-long / max-output-tokens)
4. [ToolExec]       StreamingToolExecutor.getRemainingResults() 或 runTools()
5. [Attachments]    getAttachmentMessages() — 注入 Memory / Skill / 队列命令
6. [StopHooks]      handleStopHooks() — 验证是否需要继续
7. [Continue]       state = {...next}; continue
```

**关键设计**：
- **continue 驱动**的 7 种循环继续原因 (`next_turn` / `reactive_compact_retry` / `max_output_tokens_recovery` / `stop_hook_blocking` / `token_budget_continuation` / `collapse_drain_retry` / `max_output_tokens_escalate`)
- **task_budget** 跨 compact 追踪：`taskBudgetRemaining` 在 compact 边界处扣减
- **tombstone 消息**：模型 fallback 时清理孤儿消息（防止 thinking block 签名不匹配）

### 2.2 NGO runInner() 循环

`run.go:54-295` — **241 行**，switch-case FSM。

**循环一次迭代**：
```
1. StatePrepare:    doPrepare(ctx) — 注入 planning reminder
2. StateGenerate:   doGenerate(ctx) — 组装 prompt + API 调用
3. StateToolExec:   for tc := range toolCalls { doToolExec() } — 串行执行
4. StateGuardCheck: 3 级上下文检查 → Generate/Compact/Done
5. StateCompact:    doCompact() → Generate
6. StateDone:       persistHistory + fireHooks + pendingWake check
```

### 2.3 循环控制流差异矩阵

| 能力 | CC | NGO | 评级 |
|------|-----|-----|------|
| **循环继续条件数** | 7 种（`state.transition.reason`） | 3 种（Generate/Compact/Done） | ⚠️ |
| **最大轮数控制** | `maxTurns` 参数 + attachment 通知 | `BehaviorGuard.maxSteps` | ✅ |
| **预算控制** | `maxBudgetUsd` 实时检查 + break | 无 | ❌ |
| **Max Output 恢复** | 3 次自动 resume（`MAX_OUTPUT_TOKENS_RECOVERY_LIMIT`） | 无 | ❌ |
| **模型 Fallback** | `FallbackTriggeredError` → 自动切换备用模型 | 无 | ❌ |
| **中断处理** | `abortController.signal` → 清理 tool_result | `stopCh + runCancel()` | ⚠️ |
| **子 agent 唤醒** | 无（子 agent 独立上下文） | `pendingWake` → 自动 continue | ✅ NGO独有 |
| **结构化输出** | `jsonSchema + SyntheticOutputTool` | 无 | ❌ |
| **Streaming 工具并发** | `StreamingToolExecutor` 边流边执行 | 流式接收但串行执行 | ❌ |

---

## 3. 工具执行对比

### 3.1 CC 工具执行：StreamingToolExecutor + runTools

CC 实现了两种工具执行模式，runtime 通过 feature gate 选择：

**模式 A — StreamingToolExecutor（默认开启时）**：

```typescript
// query.ts:563
let streamingToolExecutor = useStreamingToolExecution
  ? new StreamingToolExecutor(tools, canUseTool, toolUseContext)
  : null

// 流式接收期间注册工具：
for await (const message of deps.callModel(...)) {
  if (message.type === 'assistant') {
    for (const toolBlock of msgToolUseBlocks) {
      streamingToolExecutor.addTool(toolBlock, message)  // 立即开始执行
    }
    // 收集已完成的结果
    for (const result of streamingToolExecutor.getCompletedResults()) {
      yield result.message
    }
  }
}

// 流式结束后收集剩余
for await (const update of streamingToolExecutor.getRemainingResults()) {
  yield update.message
}
```

**模式 B — runTools（回退模式）**：

```typescript
// toolOrchestration.ts
for (const { isConcurrencySafe, blocks } of partitionToolCalls(...)) {
  if (isConcurrencySafe) {
    // 读-only 工具并发执行（最多 10 个）
    for await (const update of runToolsConcurrently(blocks, ...)) { ... }
    // 延迟应用 context modifiers
    for (const modifier of queuedContextModifiers) {
      currentContext = modifier(currentContext)
    }
  } else {
    // 写操作串行
    for await (const update of runToolsSerially(blocks, ...)) { ... }
  }
}
```

**CC Tool 接口关键方法**（`Tool.ts:362-695`）：

| 方法 | 用途 | NGO 等价物 |
|------|------|------------|
| `isConcurrencySafe(input)` | 声明是否可并发 | ❌ 不存在 |
| `isReadOnly(input)` | 声明是否只读 | ❌ 不存在 |
| `isDestructive(input)` | 声明是否破坏性 | ❌ 不存在 |
| `validateInput(input, ctx)` | 运行前参数校验 | ❌ 不存在 |
| `checkPermissions(input, ctx)` | 工具级权限检查 | ⚠️ Security.BeforeToolCall |
| `interruptBehavior()` | 中断时 cancel/block | ❌ 不存在 |
| `isSearchOrReadCommand(input)` | UI 折叠分类 | ❌ 不存在 |
| `maxResultSizeChars` | 结果大小上限 | ⚠️ 硬编码 50KB |
| `backfillObservableInput(input)` | 为观察者补全字段 | ❌ 不存在 |
| `preparePermissionMatcher(input)` | Hook 条件匹配器 | ❌ 不存在 |
| `toAutoClassifierInput(input)` | YOLO 分类器特征 | ❌ 不存在 |
| `searchHint` | ToolSearch 关键词 | ❌ 不存在 |
| `shouldDefer` | 延迟加载标记 | ❌ 不存在 |
| `strict` | 严格模式标记 | ❌ 不存在 |

### 3.2 NGO 工具执行：串行 doToolExec

```go
// run.go:170-213
case StateToolExec:
    for i, tc := range lastMsg.ToolCalls {
        result, err := a.doToolExec(runCtx, tc)
        if errors.Is(err, ErrApprovalDenied) {
            // 填充剩余 tool results + 中止
            for j := i + 1; j < len(lastMsg.ToolCalls); j++ {
                a.AppendMessage(llm.Message{Content: "Cancelled..."})
            }
            a.transition(StateDone)
            break
        }
        a.AppendMessage(llm.Message{Role: "tool", Content: output})
    }
```

**doToolExec 内部**（`run.go:596-721`）：
1. `guard.PreToolCheck()` — 步骤级预检
2. `Security.BeforeToolCall()` — 4 级决策（Auto/Allow/Ask/Deny）
3. `Hooks.FireBeforeTool()` — 可修改参数或跳过
4. `safeToolExec()` — panic 恢复 wrapper
5. 输出截断 `50KB` 限制
6. `Security.AfterToolCall()` + `Hooks.FireAfterTool()`
7. `Dispatch(result, delta, ps)` — 协议分发

### 3.3 差距评级

| 维度 | 评级 | 说明 |
|------|------|------|
| 并发执行 | ❌ | NGO 全串行；CC 可并发 10 个只读工具 |
| Streaming 期间预执行 | ❌ | CC 边接收 API 流边启动工具 |
| 工具元数据声明 | ❌ | CC 有 14 个工具接口方法，NGO 只有 Execute |
| 工具结果预算 | ⚠️ | NGO 硬编码 50KB；CC 按工具粒度配置 |
| 中断行为控制 | ❌ | CC 每个工具可声明 cancel/block |
| 延迟加载 | ❌ | CC 支持 `shouldDefer` + ToolSearch 发现 |
| 安全前置检查 | ✅ | NGO 的 4 级 Security 等价于 CC 的 checkPermissions |
| panic 恢复 | ✅ | NGO 有 safeToolExec；CC 无需（JS 无 panic） |

---

## 4. Token 估算与成本追踪对比

### 4.1 CC cost-tracker.ts

`cost-tracker.ts` (324 行) 实现**精确的多模型 USD 成本追踪**：

```typescript
// 按模型分离的 Usage 累计
type ModelUsage = {
  inputTokens, outputTokens,
  cacheReadInputTokens, cacheCreationInputTokens,
  webSearchRequests, costUSD,
  contextWindow, maxOutputTokens
}

// 多维成本计算
function addToTotalSessionCost(cost, usage, model) {
  // 1. 累计到模型级 usage
  addToTotalModelUsage(cost, usage, model)
  // 2. 全局状态更新（bootstrap/state.ts 中央状态）
  addToTotalCostState(cost, modelUsage, model)
  // 3. OpenTelemetry Counter 指标
  getCostCounter()?.add(cost, attrs)
  getTokenCounter()?.add(usage.input_tokens, { type: 'input' })
  // 4. Advisor（嵌套模型）成本递归累计
  for (const advisorUsage of getAdvisorUsage(usage)) {
    totalCost += addToTotalSessionCost(advisorCost, advisorUsage, advisorUsage.model)
  }
}
```

**额外能力**：
- 会话恢复时从 `projectConfig` 加载上次成本状态
- 支持 `fast mode`（速度优先模式）的差异化计费
- `formatTotalCost()` 包含行数变更统计
- `hasUnknownModelCost()` 标记未知模型价格

### 4.2 NGO TokenTracker

`token_tracker.go` (101 行) — **混合精确+估算** 的 Token 追踪：

```go
type TokenTracker struct {
    lastAPIPromptTokens int  // API 返回的精确值
    deltaEstimate       int  // 估算增量
    systemPromptTokens  int  // 系统 prompt 大小
}

func (t *TokenTracker) CurrentEstimate() (int, bool) {
    return t.lastAPIPromptTokens + t.deltaEstimate, t.hasAPIData
}
```

### 4.3 差距评级

| 维度 | CC | NGO | 评级 |
|------|-----|-----|------|
| Token 计数精度 | API 精确值（分模型累计） | 混合精确+估算 | ⚠️ |
| USD 成本计算 | 按模型费率实时计算 | 无 | ❌ |
| 多模型追踪 | 按模型名分离 | 单一计数 | ❌ |
| Cache Token 追踪 | `cacheRead + cacheCreation` | 无 | ❌ |
| 预算控制 | `maxBudgetUsd` 实时检查 | 无 | ❌ |
| 会话恢复 | 从 projectConfig 加载 | 无 | ❌ |
| OTel 指标 | Counter 导出 | 无 | ❌ |
| 嵌套模型成本 | Advisor usage 递归累计 | 无 | ❌ |
| 行数变更统计 | `linesAdded/Removed` | 无 | ❌ |

---

## 5. 上下文窗口管理对比

### 5.1 CC 7 层压缩管道

CC 在 query 循环的**每次迭代入口**依序执行：

```
Input: messagesForQuery (full history after compact boundary)
  │
  ├─[1] applyToolResultBudget()       // 工具结果预算裁剪（按 UUID 替换）
  │     - 旧结果替换为 "[result stored: UUID, preview: ...]"
  │     - 跳过 maxResultSizeChars == Infinity 的工具（如 Read）
  │
  ├─[2] snipCompactIfNeeded()          // HISTORY_SNIP (直接截断)
  │     - 超过窗口 90% 时直接丢弃旧消息
  │     - 插入 snip boundary marker
  │     - 返回 tokensFreed 供后续计算
  │
  ├─[3] deps.microcompact()            // CACHED_MICROCOMPACT (缓存微压缩)
  │     - 保留最近 N 个 tool results，旧的替换为摘要
  │     - 利用 API 的 cache_deleted_input_tokens 反馈
  │
  ├─[4] applyCollapsesIfNeeded()       // CONTEXT_COLLAPSE (上下文折叠)
  │     - 读取/搜索操作折叠为摘要（isSearchOrReadCommand）
  │     - 投影式：不修改原数组，overlay 替换
  │
  ├─[5] deps.autocompact()             // 自动压缩 (forked agent)
  │     - 超过窗口 ~80% 时触发
  │     - 起一个 forked agent 专门执行总结
  │     - 产出 CompactBoundaryMessage
  │     - 支持 pre/post compact hooks
  │
  ├─[6] prompt-too-long 恢复           // 413 错误反应式
  │     - collapse drain → reactive compact → error surface
  │
  └─[7] max-output-tokens 恢复         // 输出截断恢复
        - escalate (8k→64k) → multi-turn resume (3次) → error surface
```

### 5.2 NGO 3 级防御

```go
// StateGuardCheck — run.go:228-254
usage := float64(tokenEstimate) / float64(policy.ContextWindow)

if usage > 0.95 {
    // Level 3: 强制截断到最后 8 条
    a.forceTruncate(8)
    a.InjectEphemeral(prompttext.EphCompactionNotice)
} else if usage > 0.70 {
    // Level 1-2: LLM 摘要压缩
    a.doCompact(runCtx)
} else {
    // 正常继续
}
```

**doCompact 实现**（`run.go:792-956`）：
- 4D 总结（user_intent / session_summary / code_changes / learned_facts）
- 密度感知切割（按 information density 评分 turn）
- 深度保护：>3 次连续 compact 退化为原始截断
- 前后 hooks（BeforeCompact → vector memory 保存；AfterCompact → 通知）

### 5.3 差距评级

| 层级 | CC | NGO | 评级 |
|------|-----|-----|------|
| 工具结果预算 | ✅ 按 UUID 精确管理 | ⚠️ 硬编码 50KB 截断 | ⚠️ |
| Snip 截断 | ✅ 90% 触发 | ⚠️ 95% forceTruncate | ⚠️ |
| 微压缩 | ✅ 缓存感知 | ❌ 无 | ❌ |
| 上下文折叠 | ✅ 投影式 | ❌ 无 | ❌ |
| 自动压缩 | ✅ Forked Agent | ⚠️ 主线程 LLM 总结 | ⚠️ |
| 反应式恢复 | ✅ 413→drain→compact→retry | ⚠️ 仅重试 1 次 | ⚠️ |
| Max Output 恢复 | ✅ escalate+resume 3 次 | ❌ 无 | ❌ |
| 密度感知 | ❌ 等密度对待 | ✅ density scoring | ✅ NGO独有 |
| 深度保护 | ❌ 无 | ✅ compactCount > 3 | ✅ NGO独有 |
| 4D 总结 | ❌ 单维 summary | ✅ 4 维结构化 | ✅ NGO独有 |

---

## 6. 会话持久化对比

| 维度 | CC | NGO |
|------|-----|-----|
| 存储形式 | JSONL 文件 (`sessionStorage.ts:5105行`) + 异步写队列 | DB 增量 append (`persistHistory`) |
| 写入策略 | 用户消息同步写 + assistant 消息 fire-and-forget | 每次状态变更同步写 |
| 恢复机制 | `--resume` 加载 transcript + 成本状态 | DB 加载完整 history |
| 格式 | 带 UUID、timestamp、subtype 的结构化 Entry | llm.Message 数组 |
| flush 策略 | EAGER_FLUSH 环境变量 + 延迟 100ms jsonStringify | 直接同步写 |
| 会话切换 | `switchSession()` 保存成本并重置 | SessionID 天然隔离 |

**差距评级**：⚠️ NGO 的 DB 持久化更简洁但缺少成本恢复和 transcript 格式。

---

## 7. 错误处理与恢复对比

### 7.1 CC 错误恢复矩阵

CC 的 `query.ts` 处理了 **11 种** 不同的错误/边界情况：

| 错误类型 | 处理策略 | 恢复次数 |
|----------|----------|----------|
| API 429 (Rate Limit) | `withRetry.ts` 无限重试 + exp backoff | ∞ |
| API 529 (Overload) | 有限重试，区分前台/后台 | 3 |
| API 413 (Prompt Too Long) | → collapse drain → reactive compact → surface | 1+1 |
| max_output_tokens | → escalate 8k→64k → resume 3 次 | 1+3 |
| 模型 Fallback | `FallbackTriggeredError` → 切换模型 | 1 |
| Image Size Error | 用户友好提示 | 0 |
| Image Resize Error | 用户友好提示 | 0 |
| Streaming Fallback | tombstone 清理 + 重建 executor | 1 |
| 用户中断 (Ctrl+C) | abort signal → synthetic tool_result 填充 | 0 |
| USD 预算超限 | `maxBudgetUsd` 检查 → surface | 0 |
| 结构化输出失败 | `MAX_STRUCTURED_OUTPUT_RETRIES` 限制 | 5 |

### 7.2 NGO 错误处理

`run.go:106-143` — 5 种错误级别：

| 错误级别 | 处理 | 重试 |
|----------|------|------|
| `ErrorTransient` | 指数退避重试 | `BackoffConfig` 定义 |
| `ErrorOverload` | 指数退避重试 | `BackoffConfig` 定义 |
| `ErrorContextOverflow` | → StateCompact → 重试 | 1 |
| `ErrorBilling` | → StateFatal | 0 |
| `ErrorFatal` | → StateFatal | 0 |

### 7.3 差距评级

| 维度 | CC | NGO | 评级 |
|------|-----|-----|------|
| 错误分类数 | 11 种 | 5 种 | ⚠️ |
| 反应式压缩恢复 | ✅ 413→compact→retry | ⚠️ 仅 context overflow 重试 1 次 | ⚠️ |
| Max Output 恢复 | ✅ 4 步恢复链 | ❌ 无 | ❌ |
| 模型 Fallback | ✅ 自动切换 | ❌ 无 | ❌ |
| 预算控制 | ✅ USD 实时检查 | ❌ 无 | ❌ |
| 中断清理 | ✅ 孤儿 tool_result 填充 | ✅ ErrApprovalDenied 填充 | ✅ |

---

## 8. Feature Flag 系统

CC 使用 `bun:bundle` 的 `feature()` 函数实现**编译期 DCE (Dead Code Elimination)**：

```typescript
import { feature } from 'bun:bundle'

// 编译时常量 — 当 feature 关闭时，整个 if/require 被删除
if (feature('HISTORY_SNIP')) {
  const snipModule = require('./services/compact/snipCompact.js')
  // ...
}
if (feature('CONTEXT_COLLAPSE')) { ... }
if (feature('REACTIVE_COMPACT')) { ... }
if (feature('CACHED_MICROCOMPACT')) { ... }
if (feature('TOKEN_BUDGET')) { ... }
if (feature('COORDINATOR_MODE')) { ... }
if (feature('CHICAGO_MCP')) { ... }  // Computer Use
if (feature('BG_SESSIONS')) { ... }   // Background sessions
if (feature('EXPERIMENTAL_SKILL_SEARCH')) { ... }
if (feature('TEMPLATES')) { ... }
```

**query.ts 中的 feature flags**（仅核心引擎层）：

| Flag | 用途 | 默认 |
|------|------|------|
| `HISTORY_SNIP` | 启用 snip 截断 | ✅ |
| `CONTEXT_COLLAPSE` | 启用上下文折叠 | Gates |
| `REACTIVE_COMPACT` | 启用反应式压缩 | Gates |
| `CACHED_MICROCOMPACT` | 启用缓存微压缩 | Gates |
| `TOKEN_BUDGET` | 启用 token 预算自动 continue | Gates |
| `BG_SESSIONS` | 启用后台会话 task summary | Gates |
| `COORDINATOR_MODE` | 多 agent 协调器模式 | Gates |
| `CHICAGO_MCP` | Computer Use MCP 集成 | Gates |
| `BREAK_CACHE_COMMAND` | 缓存破坏注入（ant-only） | 🐜 |

**NGO 等价**：无编译期 feature flag 系统。运行时配置通过 `config.yaml` + 环境变量控制。

---

## 9. 差距总览矩阵

| 能力项 | CC | NGO | 差距级别 | 移植难度 |
|--------|-----|-----|----------|----------|
| 显式状态机 | ❌ 隐式 | ✅ 10 态 FSM | ✅ NGO优 | - |
| 密度感知压缩 | ❌ | ✅ density scoring | ✅ NGO优 | - |
| 4D 摘要 | ❌ 1 维 | ✅ 4 维结构化 | ✅ NGO优 | - |
| 压缩深度保护 | ❌ | ✅ compactCount | ✅ NGO优 | - |
| 子 agent 唤醒 | ❌ | ✅ pendingWake | ✅ NGO优 | - |
| panic 恢复 | ❌ (JS) | ✅ safeToolExec | ✅ NGO优 | - |
| 流式工具并发 | ✅ StreamingToolExecutor | ❌ 全串行 | ❌ | 中 |
| 工具元数据接口 | ✅ 14 方法 | ❌ 1 方法 | ❌ | 高 |
| 7 层压缩 | ✅ | ❌ 3 层 | ❌ | 高 |
| USD 成本追踪 | ✅ | ❌ | ❌ | 低 |
| 模型 Fallback | ✅ | ❌ | ❌ | 中 |
| Max Output 恢复 | ✅ 4 步链 | ❌ | ❌ | 中 |
| 结构化输出 | ✅ jsonSchema | ❌ | ❌ | 中 |
| Feature Flag DCE | ✅ 编译期 | ❌ | ❌ | N/A |
| OTel 可观测性 | ✅ | ❌ | ❌ | 中 |
| 多模型 Usage | ✅ 按模型分离 | ❌ 单一 | ❌ | 低 |
| 行数变更追踪 | ✅ | ❌ | ❌ | 低 |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **USD 成本追踪** — 在 `TokenTracker` 中增加 `CostUSD` 字段，按模型费率计算
2. **工具结果预算** — `doToolExec` 中替换硬编码 50KB 为按工具 `maxResultSize` 配置
3. **多模型 Usage** — 扩展 `TokenTracker` 为 `map[model]Usage`

### P1 — 架构增强

4. **Streaming 工具并发** — 在 `StateToolExec` 中区分只读/写操作，只读 goroutine 并发
5. **Max Output 恢复** — 在 `StateGenerate` 增加 `outputOverflow` 检测 + auto-resume
6. **模型 Fallback** — `LLMRouter.Resolve()` 增加 fallback 模型参数

### P2 — 长期演进

7. **工具元数据接口** — 在 `dtool.Definition` 中增加 `IsConcurrencySafe()`, `IsReadOnly()` 等声明
8. **微压缩 + 上下文折叠** — 在 `StateGuardCheck` 和 `doCompact` 之间插入两层
9. **Feature Flag** — Go build tags 或 runtime config 实现类似能力
