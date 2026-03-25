# OpenClaw 架构研究 — NGOAgent 优化路线图

> 分析时间：2026-03-23
> 源码版本：OpenClaw latest (git pull 2026-03-22)
> 分析范围：`src/` 下 51 个子模块，约 4000+ 文件

---

## 一、OpenClaw vs NGOAgent 信息流对比

### OpenClaw 信息流（6层）

```
用户消息 → Channel Adapter(协议标准化)
         → Gateway Server(鉴权+FIFO队列+负载均衡)
         → pi-embedded-runner(Auth轮转+Prompt组装+上下文守卫)
         → ReAct Loop(LLM调用→工具执行→Compaction)
         → pi-embedded-subscribe(流式状态机: think剥离/节流/分块)
         → server-chat(事件总线→SSE增量推送→回推原平台)
```

### NGOAgent 信息流（5层）

```
用户消息 → AgentChannel(Chat/Subagent/Forge)
         → StatePrepare(4层临界注入: planning/boundary/artifact/KI)
         → StateGenerate(15-Section Prompt组装+4级剪枝→LLM流式调用)
         → StateToolExec(4级安全决策→工具执行→协议分发)
         → StateGuardCheck(三级上下文防御) → DeltaSink(SSE推流)
```

### 核心差异

| 维度 | OpenClaw | NGOAgent |
|------|----------|---------|
| 状态机 | 无显式状态机，事件驱动 | 10态显式状态机 |
| Prompt | 13模块动态组装 `promptMode=full/minimal/none` | 15-Section + 4级预算剪枝 |
| Auth | profiles[] FIFO轮换 + 冷却 + 160次重试上限 | 线性 fallback 链 |
| 流式输出 | 6层状态机（think剥离/150ms节流/段落分块/去重） | BufferedDelta 基础缓冲 |
| 上下文管理 | Compaction + overflow检测 + 自动重试 | 三级防御（>70%压缩/>95%强截断）|
| 安全 | 工具审批 + sandbox Docker | 4级决策链 + 5min阻塞审批 |
| 子Agent | 深度限制 + 孤儿恢复 + ACP runtime | Channel抽象 + announceFn回调 |

### NGOAgent 独有优势（OpenClaw 没有的）

- **4层临界注入系统**：planning/boundary/artifact staleness/KI 步骤感知注入
- **BehaviorGuard**：`force_tool` 强制工具 + `terminate/warn` 行为控制
- **步骤追踪**：`StepsSinceBoundary` + `artifactLastStep` 文档过期提醒

---

## 二、全架构学习清单

### A级 — 完全缺失，价值最大

#### 1. Memory 向量记忆系统

**OpenClaw 实现**（`memory/` 105个文件）：
- Embedding 提供商：OpenAI / Gemini / Voyage / Ollama / Mistral
- 向量存储：sqlite-vec 本地数据库
- 检索策略：MMR（最大边际相关性）+ 时间衰减权重
- 会话文件追踪：自动索引当前涉及的文件
- 查询扩展：`query-expansion.ts`(14K) 自动扩展搜索词

**NGOAgent 现状**：
- `knowledge.Store` 只有 KI 文本索引
- `KISemanticRetriever` 接口已定义但未实现 embedding

**优化方向**：
```
infrastructure/memory/
├── embedder.go        ← Embedding API 统一接口
├── store.go           ← sqlite-vec 向量存储
├── search.go          ← MMR 检索 + 时间衰减
└── indexer.go         ← 文件/会话内容自动索引
```

---

#### 2. Plugin 生态系统

**OpenClaw 实现**（`plugins/` 165个文件）：
- 插件发现：npm/git/本地路径安装
- 8阶段钩子：`before_agent_start / after_tool_call / on_compaction / on_message`
- Provider 认证：OAuth流 / API Key / 令牌存储 / 冷却轮转
- Marketplace：ClawhHub 在线插件市场
- MCP 桥接：任意 MCP server 注册为插件

**NGOAgent 现状**：
- `skill.Manager` 有基础发现/加载
- MCP 有基础支持
- 无插件安装/更新/marketplace

**优化方向**：先做 Hook 系统扩展，再做 MCP 扩展注册

---

#### 3. Channel 频道适配器

**OpenClaw 实现**（`channels/` 63个文件）：
- 平台：Telegram / Discord / Slack / WhatsApp / Signal / Web
- 打字状态：`typing-lifecycle.ts` — 流式输出时发送"正在输入"
- 输入去抖：`inbound-debounce-policy.ts` — 快速消息合并
- 线程绑定：`thread-bindings-policy.ts`(9K) — 会话绑定群组/线程
- 状态反应：`status-reactions.ts`(12K) — emoji 回应表示处理状态
- 回复前缀：`reply-prefix.ts` — 跨平台引用回复标准化

**NGOAgent 现状**：
- `bot/handler.go` 仅 Telegram 基础消息收发
- 无 typing/reaction/debounce/thread-binding

**优化方向**：
```
interfaces/adapter/
├── types.go           ← ChannelMessage / Attachment 统一类型
├── telegram.go        ← Telegram 输入解析 + 输出格式化
├── web.go             ← Web SSE（已有，封装）
├── discord.go         ← Discord（预留）
├── normalize.go       ← 附件下载/路径统一
├── typing.go          ← 打字状态管理
└── output.go          ← 长消息分块 + Markdown转换
```

---

### B级 — 已有基础，可增强

#### 4. Auto-Reply Pipeline

**OpenClaw 实现**（`auto-reply/` 69个文件）：
- 命令注册表：声明式注册，非 if-else
- Think 标签：`<think>/<final>` 识别+剥离
- 分块推送：`chunk.ts`(15K) 长回复智能分段
- Token 管理：`SILENT_REPLY_TOKEN / HEARTBEAT_OK`
- 消息封装：`envelope.ts`(8K) metadata/routing

**NGOAgent 优化方向**：
- P0：`<think>` 标签过滤 → `buffered_delta.go`
- P0：150ms 流式节流 → `buffered_delta.go`
- P1：声明式命令注册替代 `execSlash()` switch-case
- P2：长回复分块（Telegram 4096字符限制）

---

#### 5. Context Engine 上下文引擎

**OpenClaw 实现**（`context-engine/` 7个文件）：
- 注册表模式：`registry.ts`(13K) — ContextProvider 注册/优先级排序
- 委托模式：`delegate.ts` — 运行时按需加载
- 统一类型：`types.ts`(7K) — 上下文片段类型定义

**NGOAgent 现状**：`prompt.Engine.buildSections()` 硬编码 15 个 section

**优化方向**：把 PromptEngine 重构为注册表模式，各 section 变成 `ContextProvider` 实现

---

#### 6. Hooks 生命周期钩子

**OpenClaw 实现**（`hooks/` 40个文件）：
- 内部钩子：before/after model call, on error, on compaction（8个点位）
- 消息钩子映射：消息进出时的转换管道
- 安全策略：Hook 执行沙箱
- 外部集成：Gmail watcher 邮件驱动

**NGOAgent 现状**：`hooks.go` 只有 `PostRunHookChain`（运行后钩子）

**优化方向**：扩展钩子点位 → `before_tool / after_tool / on_error / on_compact / on_message_in`

---

#### 7. Provider 多模型适配

**OpenClaw 实现**（`providers/` + `plugins/provider-*`）：
- 模型元数据：能力/限制/价格/vision/function calling 支持
- 认证分离：auth 和 model 完全解耦
- 错误分类：7类（RateLimit/Billing/ContextOverflow/Overload/Auth...）
- 退避策略：每类独立参数 `{ initialMs, maxMs, factor, jitter }`

**NGOAgent 现状**：
- `llm.Policy` 只有 `ContextWindow` 一个字段
- `llm.LLMError` 只有 `Transient/Fatal` 两级
- 退避无 jitter

**优化方向**：
```go
// 扩展 Policy
type ModelMeta struct {
    ContextWindow   int
    MaxOutput       int
    SupportsVision  bool
    SupportsTool    bool
    PriceInput1K    float64
    PriceOutput1K   float64
}

// 细化错误
const (
    ErrorRateLimit      ErrorLevel = "rate_limit"
    ErrorOverload       ErrorLevel = "overload"
    ErrorContextOverflow ErrorLevel = "context_overflow"
    ErrorBilling        ErrorLevel = "billing"
    ErrorAuth           ErrorLevel = "auth"
)
```

---

### A+级 — 多租户核心基础（系统方向决定）

> NGOAgent 明确向多用户、多消息租户方向发展，以下模块对标 OpenClaw 多租户架构。

#### 8. 多租户会话路由

**OpenClaw 实现**：
- **SessionKey 三元组**：`{channel}:{userId}:{conversationId}` 全局唯一
- **Run State Machine**（`channels/run-state-machine.ts`）：`activeRuns` 计数 + 60s 心跳广播
- **Session 路由记录**（`channels/session.ts`）：`lastRoute` 记录每条消息的回复路径
- **Inbound Debounce**（`channels/inbound-debounce-policy.ts`）：按会话级去抖，控制命令不去抖

**NGOAgent 当前硬伤**：
```go
// 硬伤1: SessionManager 全局只有一个 active 指针
type SessionManager struct { active string }  // ← 用户A和B互相覆盖

// 硬伤2: LoopPool 无租户感知，LRU 不区分用户
maxLoops: 8   // ← 第9个用户直接淘汰最早的

// 硬伤3: ChatEngine 无用户上下文
func (ce *ChatEngine) Chat(ctx, sessionID, message string) // ← 没有 userID/tenantID
```

**优化方向**：
```go
// SessionKey 重构
type SessionKey struct {
    Channel string  // "telegram" | "web" | "discord"
    UserID  string  // 平台用户ID
    ConvID  string  // 会话ID
}

// SessionManager → 按用户隔离
type SessionManager struct {
    activeByUser map[string]string  // userKey → sessionID
}

// LoopPool → 用户级配额
type LoopPool struct {
    perUserMax int                           // 每用户最大并发
    userLoops  map[string]map[string]*loop   // userKey → {sid → loop}
}
```

---

#### 9. Thread Binding 群组线程绑定

**OpenClaw 实现**（`channels/thread-bindings-policy.ts` 9K）：
- 群组中每个话题线程绑定独立 session
- @mention 触发新线程绑定
- 线程超时自动解绑

**NGOAgent 现状**：Telegram 群组中多人 @bot 共享同一上下文，无法区分各自会话。

---

#### 10. Inbound Claim 消息抢占

**OpenClaw 实现**（`plugins/hooks.ts` Claiming 模式）：
- 多插件竞争处理同一条消息，第一个 `handled=true` 的获胜
- 用于：自动回复 / CRM / 工单系统 / 多 Bot 实例路由

**NGOAgent 现状**：无消息路由决策层，每条消息直接送入唯一 Agent。

---

#### 11. Status Reactions emoji状态反馈

**OpenClaw 实现**（`channels/status-reactions.ts` 12K）：
- 收到消息 → 👁️（已读）→ 🔄（处理中）→ ✅（完成）/❌（失败）
- 群组用户可实时看到处理进度

**NGOAgent 现状**：Telegram Bot 无任何状态反馈。

---

### B+级 — 深度对比结论

#### 5. Context Engine 上下文引擎

**对比结论**：**NGOAgent 方案更优，不需要改**。

OpenClaw 的 `context-engine/registry.ts` 共 428 行，其中 300+ 行是 legacy sessionKey 兼容代码。
其核心 `assemble()` 只返回消息列表，不管 system prompt 内部段落选择。

NGOAgent 的 15-Section + 4 级预算剪枝更精确：
- Level 0: < 50% → 全保留
- Level 1: 50-70% → 长 section 截断
- Level 2: 70-85% → 按 Priority 丢弃
- Level 3: > 85% → 只保留 Priority=0

**唯一可借鉴**：OpenClaw 的 `afterTurn()` 后处理钩子 — 每轮结束后触发后台压缩决策。

---

#### 6. Hooks 生命周期钩子

**深度对比后的精确建议**（OpenClaw 有 20 个钩子点位，按 3 种模式分类）：

值得加的 4 个点位：

| 钩子 | 执行模式 | 价值 |
|------|----------|------|
| `before_tool_call` | Modifying（串行，可修改参数/阻止） | 安全策略重写工具参数、路径转换 |
| `after_tool_call` | Void（并行通知） | 执行日志、用量统计、审计推送 |
| `before/after_compact` | Void（并行通知） | 压缩前保存到 KI/向量记忆，避免信息丢失 |
| `message_sending` | Modifying（串行，可修改/取消） | 平台格式转换、敏感词过滤、长消息截断 |

不需要的：`before_model_resolve`（Router 已够）、`llm_input/output`（日志已够）、
`inbound_claim`（需要但属于多租户路由层，不是纯钩子）。

---

### C级 — 选择性学习

| OpenClaw 模块 | 功能 | 是否需要 |
|---|---|---|
| `gateway/` | 外部网关+心跳+负载均衡 | ✅ 多实例部署时需要（中期） |
| `daemon/` | 后台服务管理 | ⚪ 已有 systemd，可后期考虑 |
| `tts/` | 语音合成 | ⚪ 看场景 |
| `canvas-host/` | Canvas 画布 | ❌ 不在核心路径 |
| `pairing/` | 设备配对 | ❌ 不在核心路径 |
| `node-host/` | 远程节点控制 | ⚪ 多机部署时可考虑 |
| `i18n/` | 国际化 | ⚪ 中期可考虑 |
| `link-understanding/` | URL内容理解 | ⚪ 已有 web_fetch |
| `image-generation/` | 图像生成 | ⚪ 已有基础支持 |

---

## 三、实施路线图

### Phase 1 — 流式优化（1-2天）

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| `<think>` 标签实时剥离 | `buffered_delta.go` | `auto-reply/thinking.ts` |
| 流式节流 150ms 去抖 | `buffered_delta.go` | `server-chat.ts:emitChatDelta` |
| LLM 错误类型细化 | `llm/errors.go` + `run.go` | `pi-embedded-runner/run.ts:FailoverError` |

### Phase 2 — 多租户基础（3-5天）★

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| SessionKey 三元组重构 | `domain/service/facades.go` | `channels/session.ts` |
| 用户级 active 隔离 | `domain/service/facades.go` | `channels/session-key-utils.ts` |
| LoopPool 租户感知+配额 | `domain/service/loop_pool.go` | `channels/run-state-machine.ts` |
| RunStateMachine 心跳 | `domain/service/loop_pool.go` | `channels/run-state-machine.ts` |
| Inbound Debounce 消息合并 | `interfaces/adapter/debounce.go` | `channels/inbound-debounce-policy.ts` |

### Phase 3 — 接口适配层（3-5天）

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| `ChannelMessage` 统一类型 | `interfaces/adapter/types.go` | `channels/session-envelope.ts` |
| Telegram 输入标准化 | `interfaces/adapter/telegram.go` | `channels/plugins/telegram/` |
| Thread Binding 线程绑定 | `interfaces/adapter/thread.go` | `channels/thread-bindings-policy.ts` |
| Status Reactions 状态反馈 | `interfaces/adapter/status.go` | `channels/status-reactions.ts` |
| 输出格式化+分块 | `interfaces/adapter/output.go` | `auto-reply/chunk.ts` |
| 打字状态管理 | `interfaces/adapter/typing.go` | `channels/typing-lifecycle.ts` |

### Phase 4 — 钩子扩展（2-3天）

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| before_tool / after_tool | `domain/service/hooks.go` | `plugins/hooks.ts` |
| before/after_compact | `domain/service/hooks.go` | `plugins/hooks.ts` |
| message_sending 拦截 | `domain/service/hooks.go` | `plugins/hooks.ts` |
| inbound_claim 消息路由 | `domain/service/hooks.go` | `plugins/hooks.ts` |

### Phase 5 — 记忆+生态（1-2周）

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| Embedding API 接入 | `infrastructure/memory/embedder.go` | `memory/embeddings-openai.ts` |
| sqlite-vec 向量存储 | `infrastructure/memory/store.go` | `memory/sqlite-vec.ts` |
| MMR 检索+时间衰减 | `infrastructure/memory/search.go` | `memory/mmr.ts` + `temporal-decay.ts` |
| Auth Key 租户级轮转池 | `infrastructure/llm/keypool.go` | `plugins/provider-auth-*.ts` |
| 模型元数据 | `infrastructure/llm/model_meta.go` | `plugins/provider-model-definitions.ts` |
| 声明式命令注册 | `interfaces/server/commands.go` | `auto-reply/commands-registry.ts` |

### Phase 6 — 多实例部署（中期）

| 任务 | 文件 | 来源参考 |
|------|------|---------|
| Gateway 心跳 + 负载均衡 | `infrastructure/gateway/` | `gateway/` |
| 实例健康检查 | `infrastructure/gateway/health.go` | `gateway/heartbeat.ts` |
| 跨实例会话迁移 | `domain/service/session_migrate.go` | `channels/session.ts` |
