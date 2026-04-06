# Phase 7: API/网络/认证层深度对标分析

> CC `services/api/` (~8826 行核心) + `utils/auth.ts` (2002 行)
> vs NGO `infrastructure/llm/` (1257 行全量) — **7x 代码量差**

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. API 请求组装](#2-api-请求组装)
- [3. 重试与弹性](#3-重试与弹性)
- [4. 错误分类与处理](#4-错误分类与处理)
- [5. Provider 抽象与路由](#5-provider-抽象与路由)
- [6. SSE 流式处理](#6-sse-流式处理)
- [7. 认证与密钥管理](#7-认证与密钥管理)
- [8. Prompt Cache 管理](#8-prompt-cache-管理)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. 架构概览

```
CC API 层                              NGO API 层
┌───────────────────────────┐         ┌────────────────────────────┐
│ claude.ts (3419行)         │         │ provider.go (146行)         │
│ queryModel*() 主入口       │         │ Provider 接口 + 类型定义    │
│ prompt cache 管理          │         ├────────────────────────────┤
│ effort/taskBudget/advisor  │         │ adapter.go (315行)          │
│ tool_search/deferred       │         │ StreamAdapter 通用引擎      │
├───────────────────────────┤         │ thinkParser 状态机          │
│ withRetry.ts (822行)       │         │ ChunkMapper 接口            │
│ AsyncGenerator<Retry,T>    │         ├────────────────────────────┤
│ 529/429/401 策略            │         │ openai/client.go (316行)    │
│ Fast mode fallback         │         │ OpenAI-compatible 实现      │
│ Persistent retry           │         │ DashScope/DeepSeek/Ollama  │
├───────────────────────────┤         ├────────────────────────────┤
│ errors.ts (1207行)         │         │ errors.go (206行)           │
│ 40+ 错误条件处理            │         │ 6 级 ErrorLevel 分类        │
├───────────────────────────┤         │ BackoffWithJitter           │
│ promptCacheBreakDetection  │         │ ModelPolicy 注册表          │
│ (727行)                    │         ├────────────────────────────┤
│ 2-phase 缓存监控            │         │ router.go (149行)           │
├───────────────────────────┤         │ model→provider 映射         │
│ client.ts (389行)          │         │ 回退链                      │
│ auth.ts (2002行)           │         │ 热重载                      │
│ OAuth/API Key/Bedrock/GCP  │         ├────────────────────────────┤
└───────────────────────────┘         │ providers.go (115行)        │
                                      │ Anthropic/Google/Codex 桩   │
                                      └────────────────────────────┘
```

### 代码量对比

| 关注点 | CC | NGO | 倍率 |
|-------|-----|-----|------|
| API 主入口 (queryModel) | 3419 行 | 316 行 (client.go) | 10.8x |
| 重试引擎 | 822 行 | 206 行 (BackoffWithJitter in errors.go) | 4.0x |
| 错误处理 | 1207+260 行 | 206 行 | 7.1x |
| Cache 监控 | 727 行 | **0** | ∞ |
| 认证 | 2002 行 | ~20 行 (Bearer token) | 100x |
| Provider 路由 | 分散 | 149 行 (router.go) | — |
| SSE 适配器 | SDK 内部 | 315 行 (adapter.go) | — |
| **合计** | ~8826 行 | ~1257 行 | **7x** |

---

## 2. API 请求组装

### CC — 高度参数化请求 (claude.ts)

CC 的 `queryModel()` 组装过程极其复杂：

| 参数/特性 | 说明 |
|----------|------|
| `model` | 运行时选择 (Sonnet/Opus/Haiku, custom) |
| `toolChoice` | auto / 强制特定工具 |
| `enablePromptCaching` | 3 层控制 (全局/per-model/per-source) |
| `cache_control` | `{type: 'ephemeral', ttl?: '1h', scope?: 'global'}` |
| `effort` | string 或 numeric (ant-only override) |
| `taskBudget` | `{type: 'tokens', total, remaining}` — API 侧预算感知 |
| `fastMode` | 高速推理模式 (独立模型名) |
| `advisor` | 附加顾问模型 |
| `outputFormat` | JSON 结构化输出 |
| `extraBodyParams` | `CLAUDE_CODE_EXTRA_BODY` env 注入 |
| `betas` | 15+ beta header 管理 |
| `metadata` | device_id, account_uuid, session_id |
| `anti_distillation` | 防蒸馏注入 (fake_tools) |
| `tool_search` | 延迟工具加载 |
| `deferred tools` | LSP 未初始化时的工具延迟 |
| `MCP instructions delta` | MCP 指令增量注入 |
| `connector text` | 连接器文本块处理 |

### NGO — 简洁统一请求 (client.go)

```go
type Request struct {
    Model       string    `json:"model"`
    Messages    []Message `json:"messages"`
    Tools       []ToolDef `json:"tools,omitempty"`
    Temperature float64   `json:"temperature,omitempty"`
    TopP        float64   `json:"top_p,omitempty"`
    MaxTokens   int       `json:"max_tokens,omitempty"`
    Stream      bool      `json:"stream"`
    ToolChoice  string    `json:"-"` // force specific tool
}
```

**NGO 的设计哲学**: OpenAI-compatible 通用协议，一个 Client 兼容 DashScope/DeepSeek/Ollama 等所有 OpenAI 兼容 API。

---

## 3. 重试与弹性

### CC — withRetry AsyncGenerator (822 行)

CC 的重试系统是一个**带心跳的 AsyncGenerator 重试引擎**，是目前审计过的最复杂组件之一：

| 特性 | 实现 |
|------|------|
| 最大重试 | `DEFAULT_MAX_RETRIES = 10` (可 env 配置) |
| 529 专项 | `MAX_529_RETRIES = 3` → 自动 fallback 模型 |
| 前台/后台分流 | `FOREGROUND_529_RETRY_SOURCES` — 后台任务 529 直接放弃 |
| Fast mode 弹性 | 短 retry-after → 保持 fast mode; 长 delay → 冷却降级 |
| 持久重试 | `CLAUDE_CODE_UNATTENDED_RETRY` — 429/529 无限重试 |
| 心跳保活 | `HEARTBEAT_INTERVAL_MS = 30s` — 避免宿主环境判定 idle |
| OAuth 刷新 | 401/403 → `handleOAuth401Error` → fresh client |
| Bedrock/Vertex | 凭证过期自动清理+重建 |
| 连接池故障 | `ECONNRESET/EPIPE` → `disableKeepAlive()` → 重连 |
| Context 溢出 | 400 → `parseMaxTokensContextOverflowError` → 动态调整 max_tokens |
| 用户中断 | `signal.aborted` → `APIUserAbortError` |

**CC 重试策略决策树**：
```
Error → is529?
  ├── YES + background source → BAIL (no retry amplification)
  ├── YES + foreground → consecutive529++
  │     ├── >= 3 && fallbackModel → FallbackTriggeredError
  │     └── < 3 → exponential backoff → retry
  ├── is429? + fast mode active
  │     ├── short retry-after (<20s) → keep fast mode → retry
  │     └── long/unknown → triggerFastModeCooldown → standard mode
  ├── is401? → refreshOAuth → fresh client → retry
  ├── isBedrock/Vertex auth? → clearCache → retry
  ├── isECONNRESET? → disableKeepAlive → reconnect
  └── shouldRetry(status)?
        ├── 408/409/429/5xx → getRetryDelay(exponential + jitter) → retry
        └── other → CannotRetryError
```

### NGO — 6 级错误分类 + BackoffWithJitter (errors.go)

```go
const (
    ErrorTransient       // 429 → 60s base, 3 retries
    ErrorOverload        // 503/529 → 10s base, 5 retries
    ErrorContextOverflow // 400 context_length → compact then retry once
    ErrorRecoverable     // network/5xx → user /retry
    ErrorBilling         // 402 → try fallback provider
    ErrorFatal           // 401/403 → terminate
)
```

| 特性 | 实现 |
|------|------|
| 分类 | `ClassifyHTTPError(status)` → `ClassifyByBody(status, body)` (2 级精细化) |
| 退避 | `BackoffWithJitter(base, attempt)` — 指数退避 + ±25% 抖动 |
| 上限 | 5 分钟封顶 |
| Context 溢出 | `ErrorContextOverflow` → 由 loop.go 发起 compact 后重试 |

**NGO 的 Body 精细化分类**:
```go
// 429 + "insufficient_quota" | "billing" → ErrorBilling (not retryable)
// 400 + "context_length" | "max_tokens" → ErrorContextOverflow (compact)
// 503 + "overloaded" → ErrorOverload (confirm)
```

---

## 4. 错误分类与处理

### CC — 40+ 错误条件 (errors.ts, 1207 行)

CC 维护了极其丰富的错误消息和用户引导矩阵：

| 错误类型 | 用户消息 | 操作 |
|---------|---------|------|
| 认证错误 (401) | 引导用户运行 `claude login` | 终止 |
| 配额耗尽 (429+quota) | 显示等待时间 | 终止 |
| 模型不可用 | 推荐替代模型 | 终止 |
| 自定义模型关闭 | `CUSTOM_OFF_SWITCH_MESSAGE` | 终止 |
| 内容拒绝 | 提取 refusal 消息 | 返回 assistant message |
| API 错误前缀 | `API_ERROR_MESSAGE_PREFIX` | 超时/速率限制/过载标签 |
| overloaded (529) | `REPEATED_529_ERROR_MESSAGE` | fallback 或重试 |

### NGO — 6 级分类 (errors.go, 206 行)

NGO 更注重**结构化分类**而非用户消息：
- `ModelPolicy` 注册表提供 per-model 元信息 (contextWindow, maxOutputTokens, capabilities)
- `GetPolicyWithOverrides` 支持配置级覆盖

---

## 5. Provider 抽象与路由

### CC — 单 Provider 锁定 (Anthropic SDK)

CC 直接使用 `@anthropic-ai/sdk`，Provider 切换通过环境变量:
- `CLAUDE_CODE_USE_BEDROCK` → AWS Bedrock
- `CLAUDE_CODE_USE_VERTEX` → Google Vertex
- `getAPIProvider()` → 运行时选择

**没有通用 Provider 接口** — 所有逻辑硬编码在 claude.ts 中。

### NGO — 多 Provider 路由架构

```go
type Provider interface {
    GenerateStream(ctx, req, ch) (*Response, error)
    Name() string
    Models() []string
}
```

**Router** (149 行):

| 能力 | 方法 |
|------|------|
| 模型解析 | `Resolve(model)` → Provider |
| 带回退 | `ResolveWithFallback(model)` → Provider + 回退链 |
| 模型切换 | `SwitchModel(model)` |
| 热重载 | `Reload(providers)` — 配置变更不重启 |
| 注册表 | `modelMap: model → providerName` |

**Provider 注册** (providers.go):
- `openai` — 生产就绪 (DashScope/DeepSeek/Ollama)
- `anthropic` / `google` / `codex` / `ollama` — 接口桩位

**NGO 独有 — ChunkMapper 插件模式**:
```go
type ChunkMapper interface {
    MapChunk(data []byte) NormalizedChunk  // 仅需实现此方法
    DoneSignal() string                   // "[DONE]" 等
}
```
添加新 Provider 只需实现 1 个接口 (2 个方法)，所有 SSE 解析/工具缓冲/think 标签处理由 `StreamAdapter` 统一处理。

---

## 6. SSE 流式处理

### CC — SDK 内部处理

CC 使用 `@anthropic-ai/sdk` 的内置流式处理，在 `claude.ts` 中消费 `BetaRawMessageStreamEvent`。

### NGO — StreamAdapter 通用引擎 (adapter.go, 315 行)

NGO 实现了一个**完整的通用 SSE 流式处理引擎**：

| 组件 | 功能 |
|------|------|
| `StreamAdapter` | 主控制器：SSE 行解析 → ChunkMapper → 分发 |
| `thinkParser` | **跨 chunk 的 `<think>` 标签状态机** |
| `toolCallArgs` | 增量工具参数缓冲 (支持 OpenAI 增量 + Ollama 单次) |
| `sanitizeToolName()` | 清洗原始 token 标记 `<\|tool_call_argument_begin\|>` |
| `isCompleteJSON()` | 区分增量片段 vs 完整 JSON (Ollama vs OpenAI) |
| `partialPrefixLen()` | 检测跨 chunk 边界的局部标签 |

**thinkParser 状态机** — 处理 `<think>` 跨 chunk 边界：
```
Chunk 1: "some text <thi"     → text:"some text ", pending:"<thi"
Chunk 2: "nk>reasoning"       → pending→"<think>" → inThink=true, reasoning:"reasoning"
Chunk 3: "more </thi"         → reasoning:"more ", pending:"</thi"
Chunk 4: "nk> back to text"   → inThink=false, text:" back to text"
```

**NGO 独有 — Ollama 兼容性处理**：
```go
if buf.Len() > 0 && isCompleteJSON(tc.Arguments) {
    // Ollama 发送完整 JSON 但 buffer 有旧内容 → 替换而非追加
    buf.Reset()
}
```

---

## 7. 认证与密钥管理

### CC — 5 路认证 (auth.ts, 2002 行)

| 路径 | 说明 |
|------|------|
| API Key | `ANTHROPIC_API_KEY` env / keychain file |
| OAuth (claude.ai) | OAuth2 token + refresh + revocation |
| AWS Bedrock | AWS SDK CredentialsProvider + STS |
| GCP Vertex | google-auth-library + service account |
| Enterprise | SSO / custom JWT |

**关键能力**：
- `handleOAuth401Error(failedToken)` — 自动 token refresh
- `clearAwsCredentialsCache()` + `clearGcpCredentialsCache()` — 凭证过期重建
- `getClaudeAIOAuthTokens()` — OAuth token pair 管理
- `isClaudeAISubscriber()` / `isEnterpriseSubscriber()` — 订阅级别检测

### NGO — Bearer Token (简约路径)

```go
httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
```
- 从 YAML 配置文件读取 API Key
- 无 OAuth/Bedrock/Vertex 支持
- 无 token 自动刷新

---

## 8. Prompt Cache 管理

### CC — 2-Phase Cache Break Detection (727 行)

CC 实现了一个**企业级 prompt cache 监控系统**，这是整个 API 层最独特的组件：

**Phase 1 (Pre-call)** — `recordPromptState()`:
- Hash: systemPrompt, toolSchemas, cacheControl, model, betas, fastMode, effort, extraBody
- 保存前次状态快照 (最多 10 个 source)
- 计算 pending changes (system/tools/model/betas/effort 等 12 维度差异)

**Phase 2 (Post-call)** — `checkResponseForCacheBreak()`:
- 比对 `cacheReadTokens` 下降 >5% AND 绝对值 > 2000 tokens
- 归因分析 (哪个维度变化导致 cache break)
- TTL 过期检测 (5min/1h)
- 生成可读的原因报告 + diff 文件

```typescript
// Cache break 归因输出示例:
"[PROMPT CACHE BREAK] system prompt changed (+500 chars), tools changed (+1/-0 tools)"
"[PROMPT CACHE BREAK] possible 5min TTL expiry (prompt unchanged)"
"[PROMPT CACHE BREAK] likely server-side (prompt unchanged, <5min gap)"
```

**关键防御**:
- `notifyCacheDeletion()` — micro compact 后抑制误报
- `notifyCompaction()` — compaction 后重置 baseline
- `cleanupAgentTracking()` — agent 结束清理追踪

### NGO — 无 Cache 监控

NGO 不使用 Anthropic API 的 prompt caching，因此没有相关监控。但对于 OpenAI-compatible API 的 prefix caching 控制，也没有相关能力。

---

## 9. 差距总览矩阵

| 维度 | CC | NGO | 评估 |
|------|-----|-----|------|
| **API 请求组装** | 15+ 参数/特性 | 8 个基础参数 | ⚠️ 但 NGO 够用 |
| **重试引擎** | ✅ AsyncGenerator + 心跳 | ⚠️ BackoffConfig 定义但重试逻辑在 loop.go | ⚠️ |
| 529 前台/后台分流 | ✅ | ❌ | ⚠️ |
| 模型 Fallback | ✅ FallbackTriggeredError | ✅ ResolveWithFallback | ≡ |
| Fast mode 弹性 | ✅ 短/长 delay 自适应 | ❌ | — (场景不同) |
| 持久重试 (unattended) | ✅ 无限重试+心跳 | ❌ | ⚠️ |
| Context 溢出自动调整 | ✅ max_tokens 动态缩减 | ✅ ErrorContextOverflow → compact | ≡ |
| **错误分类** | 40+ 条件 string-matching | ✅ 6 级 + body 精细化 | ≡ 各有优势 |
| ErrorLevel 分类体系 | ❌ (分散在 shouldRetry) | ✅ 结构化 enum | ✅ **NGO优** |
| 用户友好错误消息 | ✅ 详细引导 | ⚠️ 原始错误 | ⚠️ |
| **Provider 抽象** | ❌ 硬编码 Anthropic SDK | ✅ Provider 接口 + Router | ✅ **NGO优** |
| 多模型路由 | ❌ (单 model 参数) | ✅ modelMap + fallback chain | ✅ **NGO优** |
| 热重载 | ❌ | ✅ Reload(providers) | ✅ **NGO独有** |
| ChunkMapper 插件 | ❌ | ✅ 2 方法接口 | ✅ **NGO独有** |
| **SSE 流式** | SDK 内部 | ✅ 315行 StreamAdapter | ✅ **NGO独有** |
| thinkParser 状态机 | ❌ (无需, native reasoning) | ✅ 跨 chunk 边界支持 | ✅ **NGO独有** |
| sanitizeToolName | ❌ | ✅ 原始 token 清洗 | ✅ **NGO独有** |
| Ollama 兼容 | ❌ | ✅ isCompleteJSON 适配 | ✅ **NGO独有** |
| **认证** | ✅ 5 路 (OAuth/Bedrock/GCP/Key/Enterprise) | ⚠️ Bearer only | ⚠️ |
| Token 自动刷新 | ✅ | ❌ | ⚠️ |
| **Cache 监控** | ✅ 727行 2-phase 检测 | ❌ | ❌ **重大差距** |
| Cache break 归因 | ✅ 12 维度 | ❌ | ❌ |
| **ModelPolicy** | 分散 | ✅ 注册表 + 配置覆盖 | ✅ **NGO优** |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **重试引擎增强** — 在 loop.go 的重试路径中增加:
   - 前台/后台分流 (后台 compact/title 等 429/529 直接放弃，不做重试放大)
   - 用户中断检测 (`context.Done()`)
   - 专项 529 计数 → 超阈值触发模型 fallback

2. **Context 溢出自适应** — 当收到 400 context_length 错误时，解析 `inputTokens + maxTokens > contextLimit`，动态缩减 `max_tokens` 后重试（补充现有 compact 路径）

3. **用户友好错误消息** — 参考 CC 的 `errors.ts`，为每种 ErrorLevel 添加用户可读的行动建议（如 "API 配额耗尽，请等待 X 分钟"）

### P1 — 架构增强

4. **Cache 监控 (Lite)** — 即使不用 Anthropic API，为 OpenAI prefix caching 实现轻量监控：记录 prompt hash → 检测 prompt 变化频率 → 提示优化空间

5. **OAuth / Token Refresh** — 在 Provider 接口中增加 `RefreshAuth()` 方法，支持 token 自动续约

6. **持久重试模式** — 为无人值守场景 (cron 任务/自动化) 添加 429/529 无限重试 + 心跳保活

### P2 — 长期演进

7. **API Telemetry** — 请求/响应的结构化遥测（延迟、token 消耗、cache 命中率），支持成本分析
8. **多租户 Provider** — 支持 per-session 不同的 API Key + 模型配置 (SaaS 场景)
