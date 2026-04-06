# Phase 9: 高级特性层深度对标分析

> CC `coordinator/` (369行) + `bridge/` (12613行) + `tasks/` (1102行) — **多 Agent 编排**
> vs NGO `evolution/` (432行) + `barrier.go` (258行) + `cron/` (470行) + `channel.go` (120行) — **自演进 + 自调度**

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. 多 Agent 编排 (Coordinator)](#2-多-agent-编排-coordinator)
- [3. 远程桥接 (Bridge)](#3-远程桥接-bridge)
- [4. 任务系统 (Tasks)](#4-任务系统-tasks)
- [5. 子 Agent 同步 (Barrier)](#5-子-agent-同步-barrier)
- [6. 自演进系统 (Evolution)](#6-自演进系统-evolution)
- [7. 定时任务 (Cron)](#7-定时任务-cron)
- [8. 通道抽象 (Channel)](#8-通道抽象-channel)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. 架构概览

```
CC 高级特性                            NGO 高级特性
┌─────────────────────────────┐      ┌────────────────────────────────┐
│ Coordinator (369行)          │      │ SubagentBarrier (258行)         │
│ ├── 系统 Prompt (370行详细规范)│      │ ├── pending 计数               │
│ ├── worker 生成 + 继续       │      │ ├── auto-wake 回调             │
│ ├── 并行研究 → 综合 → 实现    │      │ ├── maxConcurrent (3)          │
│ └── task-notification XML    │      │ ├── timeout (5min)            │
├─────────────────────────────┤      │ ├── InjectEphemeral(results)  │
│ Bridge (12613行)             │      │ └── progress push (SSE/WS)    │
│ ├── 远程 Agent 调度           │      ├────────────────────────────────┤
│ ├── replBridge (本地<>远程)   │      │ Evolution Engine (432行)       │
│ ├── capacityWake (容量感知)   │      │ ├── EvoEvaluator (283行)       │
│ ├── JWT session auth         │      │ │   ├── VLM 多模态评估         │
│ ├── flushGate (控流)         │      │ │   ├── Dissatisfaction 检测    │
│ ├── sessionRunner (协程)     │      │ │   └── Template-driven prompt │
│ └── trustedDevice (信任)     │      │ ├── Diagnoser (163行)          │
├─────────────────────────────┤      │ │   └── 6 类故障分类            │
│ Tasks (1102行)               │      │ └── RunTracker (82行)          │
│ ├── DreamTask (后台思考)      │      │     └── 20-cap history + 成功率│
│ ├── LocalShellTask           │      ├────────────────────────────────┤
│ ├── LocalAgentTask           │      │ Cron Manager (470行)           │
│ ├── RemoteAgentTask          │      │ ├── 文件持久化 (job.json)       │
│ ├── LocalWorkflowTask        │      │ ├── 心跳巡检 (_heartbeat/30m)  │
│ └── MonitorMcpTask           │      │ ├── 日记整理 (_diary_digest/24h)│
│                              │      │ └── CRUD + RunNow + Logs      │
│                              │      ├────────────────────────────────┤
│                              │      │ AgentChannel (120行)           │
│                              │      │ ├── ChatChannel (SSE sink)    │
│                              │      │ ├── SubagentChannel (collect)  │
│                              │      │ └── ForgeChannel (eval context)│
└─────────────────────────────┘      └────────────────────────────────┘
```

### 代码量对比

| 关注点 | CC | NGO | 说明 |
|-------|-----|-----|------|
| 多 Agent 编排 | 369 行 (Coordinator) | 258 行 (Barrier) | 方向不同 |
| 远程桥接 | 12613 行 (Bridge) | **0** | CC 独有 |
| 任务类型 | 1102 行 (6 种 Task) | 120 行 (3 种 Channel) | CC 3x |
| 自演进 | **0** | 432 行 (Evolution) | NGO 独有 |
| 定时任务 | **0** | 470 行 (Cron) | NGO 独有 |
| **合计** | ~14084 行 | ~1280 行 | 11x |

---

## 2. 多 Agent 编排 (Coordinator)

### CC — Coordinator Mode (369 行系统 Prompt)

CC 的 Coordinator 是一个**纯 Prompt 驱动的编排引擎**：

| 特性 | 实现 |
|------|------|
| 角色定义 | "你是**协调者**，不是执行者" |
| Worker 工具 | `AgentTool` (spawn), `SendMessageTool` (continue), `TaskStopTool` (kill) |
| 结果通知 | `<task-notification>` XML（伪装为 user message 注入） |
| 并发策略 | 读操作并行，写操作串行 per-file-set |

**4 阶段工作流**:
```
Research (并行) → Synthesis (协调者) → Implementation (worker) → Verification (独立 worker)
```

**Prompt 核心原则**:
1. **永远综合** — 协调者必须理解研究结果后再下发任务
2. **禁止懒委托** — 不准写 "based on your findings, fix it"
3. **自包含规范** — 每个 worker prompt 必须包含完整上下文
4. **Continue vs Spawn** — 基于上下文重叠度决策

**Scratchpad**: 跨 worker 共享的持久化工作空间 (`tengu_scratch` feature gate)。

### NGO — SubagentBarrier (258 行)

NGO 的编排是**代码驱动的同步屏障**：

```go
type SubagentBarrier struct {
    pending        int
    results        map[string]subagentResult
    parentLoop     *AgentLoop
    autoWake       func()
    pushProgress   func(runID, taskName, status string, done, total int, errMsg, output string)
    timeout        time.Duration     // 5min
    maxConcurrent  int               // 3
}
```

| 特性 | 实现 |
|------|------|
| 并发限制 | `maxConcurrent = 3` (S5 安全策略) |
| 去重 | `DoneAt.IsZero()` 检测重复 `OnComplete` |
| 超时 | 5 分钟 → 收集部分结果 → 强制唤醒 |
| 结果注入 | `InjectEphemeral(formattedResults)` → 父 loop 自动唤醒 |
| 进度推送 | SSE/WS 实时推送 `(done/total, status)` |
| 结果格式 | `EphSubAgentResults` 权威 header + ✅/❌/⏰ 状态 |

**Barrier 执行流**:
```
Parent spawns A, B, C → barrier.pending = 3
  C completes → pending = 2, push progress (1/3)
  A completes → pending = 1, push progress (2/3)
  B completes → pending = 0 → formatResults() → InjectEphemeral → SignalWake → autoWake()
```

**关键防死锁设计** (S1):
```go
// Release lock BEFORE external calls to prevent deadlock
b.mu.Unlock()
b.parentLoop.InjectEphemeral(summary)  // 外部调用在锁外
b.parentLoop.SignalWake()
```

---

## 3. 远程桥接 (Bridge)

### CC — Bridge (12613 行)

CC 的 Bridge 是整个系统中**代码量最大的单一子系统** (12613 行)：

| 组件 | 行数 | 功能 |
|------|------|------|
| `bridgeMain.ts` | 核心 | 远程 Agent 主控 |
| `bridgeApi.ts` | API | 远程 API 接口 |
| `bridgeMessaging.ts` | 消息 | 远程消息路由 |
| `bridgeConfig.ts` | 配置 | Bridge 环境配置 |
| `bridgePermissionCallbacks.ts` | 权限 | 远程权限回调 |
| `capacityWake.ts` | 容量 | 容量感知唤醒 |
| `flushGate.ts` | 控流 | 消息刷新闸门 |
| `sessionRunner.ts` | 运行 | 远程 session 协程 |
| `replBridge.ts` | REPL | REPL 层桥接 |
| `remoteBridgeCore.ts` | 核心 | 远程核心逻辑 |
| `jwtUtils.ts` | JWT | session JWT 认证 |
| `trustedDevice.ts` | 信任 | 可信设备管理 |
| `createSession.ts` | 会话 | 远程会话创建 |
| `inbound*.ts` | 入站 | 入站消息/附件处理 |
| `codeSessionApi.ts` | Code | Code session API |
| `workSecret.ts` | 密钥 | 工作密钥管理 |

**Bridge 核心场景**: 让 CC 可以在远程服务器/容器中运行 Agent（如 GitHub Actions, CI/CD），通过 Bridge 连回本地 IDE。

### NGO — 无 Bridge

NGO 作为 Daemon 进程，通过 gRPC/WS 天然支持远程访问，不需要专门的 Bridge 机制。

---

## 4. 任务系统 (Tasks)

### CC — 6 种 Task 类型

```typescript
export function getAllTasks(): Task[] {
    return [
        LocalShellTask,      // 本地 shell 命令
        LocalAgentTask,      // 本地子 agent
        RemoteAgentTask,     // 远程 agent
        DreamTask,           // 后台思考任务
        LocalWorkflowTask,   // 本地工作流脚本 (feature gated)
        MonitorMcpTask,      // MCP 监控任务 (feature gated)
    ]
}
```

**DreamTask** — CC 独特的"做梦"机制：后台异步思考任务，在 agent 空闲时主动探索和预加载上下文。

### NGO — 3 种 AgentChannel

```go
type AgentChannel interface {
    Name() string                          // "chat" | "subagent" | "forge"
    DeltaSink() DeltaSink                  // 输出接收器
    OnComplete(runID, result string, err error)  // 完成回调
}
```

| Channel | 用途 | Output Sink |
|---------|------|-------------|
| `ChatChannel` | 用户直接对话 | SSE/WS writer |
| `SubagentChannel` | 子 agent 任务 | OutputCollector → announce to parent |
| `ForgeChannel` | 测试/评估环境 | 可配置 sink |

**SubagentChannel 的 announce-back 机制**:
```go
func (c *SubagentChannel) OnComplete(runID string, _ string, err error) {
    result := c.collector.Result()
    c.announceFn(runID, result, err) // → barrier.OnComplete()
}
```

---

## 5. 子 Agent 同步 (Barrier)

详见 [第 2 节](#2-多-agent-编排-coordinator) SubagentBarrier 分析。

**对比总结**:

| 维度 | CC (Coordinator) | NGO (Barrier) |
|------|-------------------|---------------|
| 编排方式 | Prompt 驱动 (LLM 决策) | 代码驱动 (确定性) |
| 并发控制 | Prompt 指令 "读并行写串行" | `maxConcurrent = 3` 硬限制 |
| 结果收集 | `<task-notification>` XML | `formatResults()` 结构化 |
| 超时处理 | 无明确超时 | 5min 强制 finalize |
| 进度反馈 | worker 自行报告 | SSE/WS 实时 push |
| 错误传播 | worker status=failed | `err error` 真实传播 (S3) |

---

## 6. 自演进系统 (Evolution)

### CC — 无自演进

CC 没有内置的质量评估或自动重试/修复机制。

### NGO — 3 层 Evolution Engine (432 行)

NGO 的 Evolution 系统是**独有的自修复引擎**：

#### 6.1 EvoEvaluator (283 行)

**盲评估 (Blind Assessment)** — 使用独立 LLM 调用评估执行质量：

```go
type EvalResult struct {
    Score     float64     `json:"score"`      // 0.0-1.0
    Passed    bool        `json:"passed"`     // score >= threshold
    ErrorType string      `json:"error_type"` // 错误分类
    Issues    []EvalIssue `json:"issues"`     // 问题列表
}
```

**多模态评估** — 支持 VLM (Vision-Language Model):
```go
// 自动提取 user attachments + trace artifacts 中的图片
// 用户图片 (intent) + 生成物图片 (output) → VLM 评估
// 限制: 2 user + 4 artifacts = 6 max
imageParts := e.extractImages(userRequest, traceJSON)
```

**用户不满检测**:
```go
var dissatisfactionKeywords = []string{
    "不满意", "不行", "不对", "重做", "再来", "太差", "错了",
    "not good", "redo", "wrong", "fix it", "try again",
}
```

#### 6.2 Diagnoser (163 行)

**6 类故障诊断**:

| 类别 | 关键词 | AutoFixable | Confidence |
|------|--------|-------------|------------|
| `missing_dep` | ModuleNotFoundError, npm ERR! | ✅ | high |
| `code_bug` | SyntaxError, TypeError | ✅ | high |
| `env_issue` | permission denied, sudo | ❌ | high |
| `unresolvable` | api key, cuda, hardware | ❌ | high |
| `quality_low` | 太差, too much, blurry | ✅ | low |
| `intent_mismatch` | 搞错了, not what i asked | ✅ | low |

**自动修复建议**:
```go
// missing_dep → pip install <pkg> / npm install <pkg> / go get <pkg>
// code_bug → "Review the error and fix the source code"
// env_issue → "May need elevated permissions. Ask the user."
// quality_low → "Adjust parameters and retry."
// intent_mismatch → "Re-route with clarified intent."
```

#### 6.3 RunTracker (82 行)

**演进历史跟踪**:
```go
type EvoRun struct {
    At         time.Time
    Passed     bool
    Retries    int
    Strategy   string  // param_fix | tool_swap | re_route | iterate | escalate
    FailReason string
    Duration   time.Duration
}
```

- 每个 skill 最多保留 20 条历史
- `GetSuccessRate()` 计算成功率
- 持久化到 `~/.ngoagent/evo/<skill>/history.json`

#### 6.4 Assertion System (187 行)

**多维断言框架**:

| 断言类型 | 说明 |
|---------|------|
| `FileExists` | 文件必须存在 |
| `FileContains` | 文件必须包含特定内容 |
| `ShellCheck` | Shell 命令必须 exit 0 |
| `OutputQuality` | LLM 评分必须 ≥ threshold |
| `UserFeedback` | 用户反馈必须为正面 |

---

## 7. 定时任务 (Cron)

### CC — 无 Cron

CC 没有内置定时任务系统。

### NGO — Cron Manager (470 行)

NGO 实现了一个**完整的 Agent-native 定时任务系统**：

| 特性 | 实现 |
|------|------|
| 持久化 | `~/.ngoagent/cron/<job>/job.json` |
| 日志 | `<job>/logs/<timestamp>_ok.md` or `_fail.md` |
| 调度 | Go duration 格式 (`30s` `5m` `1h` `24h`) |
| 最小间隔 | 10 秒 |
| 执行超时 | 5 分钟 |
| 热读取 | 每次 tick 重新读取 job.json (支持运行时修改) |
| 统计 | `RunCount` `FailCount` `LastRun` |
| 安全 | path traversal 防护 |

**内置心跳任务** (不可删除):
```go
heartbeats := []Job{
    {Name: "_heartbeat", Schedule: "30m",
     Prompt: "执行心跳巡检：检查系统状态、整理过期临时文件、报告异常。"},
    {Name: "_diary_digest", Schedule: "24h",
     Prompt: "整理昨天的日记：读取 memory/diary/ 下昨日文件，汇总所有条目生成精炼摘要。"},
}
```

**`_heartbeat`**: 每 30 分钟系统自检
**`_diary_digest`**: 每 24 小时整理记忆日记 — 将向量碎片蒸馏为结构化知识

---

## 8. 通道抽象 (Channel)

### CC — 无通道抽象

CC 的不同执行上下文（REPL, SDK, pipe等）通过命令行参数和环境变量控制，无统一接口抽象。

### NGO — AgentChannel 接口 (120 行)

参见 [第 4 节](#4-任务系统-tasks) — 3 种 Channel 实现统一接口。

**ForgeChannel 的特殊用途**: 为 EvoEvaluator 提供隔离的评估上下文，避免评估产出污染主对话。

---

## 9. 差距总览矩阵

| 维度 | CC | NGO | 评估 |
|------|-----|-----|------|
| **多 Agent 编排** | ✅ Coordinator (Prompt驱动) | ✅ Barrier (代码驱动) | ≡ 各有优势 |
| 编排系统 Prompt | ✅ 370行详细规范 | ⚠️ 依赖通用 prompt | ⚠️ |
| 并行研究→综合→实现 | ✅ 4-phase workflow | ⚠️ 无 pipeline 抽象 | ⚠️ |
| worker 继续 (continue) | ✅ SendMessageTool | ❌ | ⚠️ |
| 并发限制 | ⚠️ Prompt 指令 (软性) | ✅ maxConcurrent (硬性) | ✅ **NGO优** |
| 超时控制 | ⚠️ 无 | ✅ 5min timeout | ✅ **NGO优** |
| 进度推送 | ⚠️ worker 自行报告 | ✅ 实时 SSE/WS push | ✅ **NGO优** |
| 防死锁 | ⚠️ 无需 (Prompt 层) | ✅ S1 锁外调用 | ✅ **NGO优** |
| 去重保护 | ⚠️ 无 | ✅ DoneAt dedup (S4) | ✅ **NGO优** |
| **远程桥接** | ✅ 12613行 Bridge | ❌ (gRPC 替代) | ⚠️ 架构不同 |
| JWT session | ✅ | ❌ | — |
| 容量感知 | ✅ capacityWake | ❌ | ⚠️ |
| **任务类型** | ✅ 6 种 Task | ✅ 3 种 Channel | ≡ |
| DreamTask (后台思考) | ✅ | ❌ | ⚠️ |
| MonitorMcpTask | ✅ | ❌ | — |
| **自演进** | ❌ | ✅ 3 层 Engine (432行) | ✅ **NGO独有** |
| VLM 多模态评估 | ❌ | ✅ 6 图 VLM | ✅ **NGO独有** |
| 故障诊断 (6类) | ❌ | ✅ Diagnoser | ✅ **NGO独有** |
| 自动修复建议 | ❌ | ✅ FixCommands | ✅ **NGO独有** |
| 历史追踪+成功率 | ❌ | ✅ RunTracker | ✅ **NGO独有** |
| 结构化断言 | ❌ | ✅ 5 种 Check | ✅ **NGO独有** |
| 不满检测 (中英) | ❌ | ✅ keyword + LLM | ✅ **NGO独有** |
| **定时任务** | ❌ | ✅ 完整 Cron (470行) | ✅ **NGO独有** |
| 心跳巡检 | ❌ | ✅ _heartbeat / 30m | ✅ **NGO独有** |
| 日记蒸馏 | ❌ | ✅ _diary_digest / 24h | ✅ **NGO独有** |
| **通道抽象** | ❌ | ✅ AgentChannel 接口 | ✅ **NGO独有** |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **Coordinator 系统 Prompt** — 从 CC 移植编排规范的核心原则：
   - "永远综合，禁止懒委托"
   - "自包含规范 — 每个 subagent prompt 必须完整"
   - "Continue vs Spawn 决策表" — 基于上下文重叠度
   - 将这些规则写入 NGO 的 EphSubAgentLaunch prompt 中

2. **worker 继续 (continue)** — 在 SubagentBarrier 中增加 `Continue(runID, followUpMessage)` 方法，允许复用已完成 subagent 的上下文发送后续任务

3. **DreamTask** — 在 Cron 中增加低优先级"思考"任务：idle 时主动探索代码库、更新知识索引、预加载常用文件

### P1 — 架构增强

4. **4-Phase Pipeline** — 在 SubagentBarrier 上层构建 `Pipeline` 抽象：
   ```
   Research(parallel) → Synthesize(parent) → Implement(workers) → Verify(independent)
   ```
   每个 phase 有独立的 Barrier 实例

5. **Task-Notification** — 参考 CC 的 XML 格式，标准化 subagent 结果通知：
   ```xml
   <task-notification>
     <task-id>...</task-id>
     <status>completed|failed|timeout</status>
     <summary>...</summary>
     <result>...</result>
     <usage>tokens/duration</usage>
   </task-notification>
   ```

6. **Scratchpad** — 实现跨 subagent 共享的临时工作空间目录，持久化到 session brain 中

### P2 — 长期演进

7. **Evolution + Coordinator 融合** — 当 Coordinator 的 subagent 返回失败时，自动调用 Diagnoser → 分类 → 如果 AutoFixable → 自动重试 (iterate/tool_swap/re_route)
8. **容量感知调度** — 在 Cron/Barrier 中增加系统负载感知，高负载时延迟非关键任务
9. **远程 Agent** — 利用 gRPC streaming 实现远程 AgentLoop 执行，替代 CC 的 Bridge 模式
