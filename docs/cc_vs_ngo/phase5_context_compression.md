# Phase 5：上下文管理与压缩层深度对标分析

> CC `services/compact/` (11 文件, 145 KB) + `utils/messages.ts` (5512 行)
> vs NGO `service/run.go::doCompact` (~160 行) + `service/sanitize.go` (129 行) + `service/token_tracker.go` (101 行) + `service/memory_hook.go` (79 行)

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. 触发机制](#2-触发机制)
- [3. 压缩策略](#3-压缩策略)
  - [3.1 CC 4 级压缩级联](#31-cc-4-级压缩级联)
  - [3.2 NGO 3 级上下文防御](#32-ngo-3-级上下文防御)
- [4. 摘要生成](#4-摘要生成)
- [5. 压缩后恢复](#5-压缩后恢复)
- [6. Token 追踪](#6-token-追踪)
- [7. 消息清洗](#7-消息清洗)
- [8. 向量记忆集成](#8-向量记忆集成)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. 架构概览

### CC — 11 文件多层压缩体系

```
services/compact/
├── compact.ts              (60 KB, 1706 行) — 核心 compactConversation
├── autoCompact.ts          (13 KB, 352 行) — 自动触发+阈值
├── microCompact.ts         (20 KB, 531 行) — 微压缩 (工具结果清理)
├── sessionMemoryCompact.ts (21 KB, 631 行) — Session Memory 压缩
├── prompt.ts               (16 KB, 375 行) — 9 段式摘要 prompt
├── grouping.ts             (3 KB)  — API round 分组
├── postCompactCleanup.ts   (4 KB)  — 压缩后清理
├── apiMicrocompact.ts      (5 KB)  — API 层微压缩
├── compactWarningHook.ts   (1 KB)  — 压缩警告
├── compactWarningState.ts  (1 KB)  — 压缩警告状态
└── timeBasedMCConfig.ts    (2 KB)  — 时间触发配置
```

### NGO — 内嵌式紧凑实现

```
service/
├── run.go::doCompact()       (~160 行) — 4 维度摘要 + 密度感知裁切
├── run.go::forceTruncate()    (~35 行)  — 紧急截断
├── sanitize.go                (129 行) — 孤儿工具对修复 + 轮次顺序执行
├── token_tracker.go           (101 行) — 混合精确+估算 Token 追踪
└── memory_hook.go             (79 行)  — 向量记忆保存 + KI 去重
```

**代码量对比**：

| 关注点 | CC | NGO | 倍率 |
|--------|-----|-----|------|
| 压缩核心 | 60 KB + 21 KB | ~5 KB | 16x |
| 微压缩 | 20 KB + 5 KB | **无** | ∞ |
| 触发逻辑 | 13 KB | ~30 行 | 14x |
| 摘要 Prompt | 16 KB (375 行 9 段式) | ~20 行 (4 维度) | 19x |
| 消息清洗 | 5512 行 (messages.ts 部分) | 129 行 | ~15x |
| Token 追踪 | 分散 (估算库) | 101 行 (集中式) | — |
| 记忆保存 | 21 KB (Session Memory) | 79 行 (Vector + KI 去重) | 4x |
| **合计** | **~145 KB** | **~7 KB** | **~21x** |

---

## 2. 触发机制

### CC — 多阈值 + 断路器

```typescript
// autoCompact.ts — 复杂触发逻辑
effectiveContextWindow = contextWindowForModel - MAX_OUTPUT_TOKENS_FOR_SUMMARY(20K)
autoCompactThreshold   = effectiveContextWindow - AUTOCOMPACT_BUFFER(13K)

// 5 级状态
percentLeft                 → 剩余百分比
isAboveWarningThreshold     → 超警告阈值 (context - 20K)
isAboveErrorThreshold       → 超错误阈值 (context - 20K) 
isAboveAutoCompactThreshold → 超自动压缩阈值
isAtBlockingLimit           → 超阻断限制 (context - 3K)

// 断路器: 连续失败 3 次后停止重试
MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3
```

**递归防护**：
- `querySource === 'session_memory' || 'compact'` → 跳过（防止 forked agent 死锁）
- `querySource === 'marble_origami'` → 跳过（防止 Context Collapse 竞争）
- `DISABLE_COMPACT` / `DISABLE_AUTO_COMPACT` 环境变量
- GrowthBook feature flags (`tengu_cobalt_raccoon`, `REACTIVE_COMPACT`, `CONTEXT_COLLAPSE`)

### NGO — 3 级上下文防御

```go
// run.go — 简洁的 3 级触发
tokenEstimate := tokenTracker.CurrentEstimate() // API基线+增量估算
policy := config.ResolveModelParams(model)
usage := float64(tokenEstimate) / float64(policy.ContextWindow)

switch {
case usage > 0.95: // Level 3: 紧急截断
    forceTruncate(8)      // 仅保留最后 8 条消息
    InjectEphemeral(EphCompactionNotice)
case usage > 0.70: // Level 1-2: LLM 摘要压缩
    transition(StateCompact)
    doCompact(runCtx)
default:            // 安全区
    transition(StateGenerate)
}
```

**关键差异**：
- CC 使用百分比阈值 + 绝对 token 值 + 断路器
- NGO 使用简洁的 0.70/0.95 占比阈值，无断路器
- NGO 的 `StateCompact` 是状态机一级状态，更结构化

---

## 3. 压缩策略

### 3.1 CC 4 级压缩级联

CC 拥有 4 种并行/级联的压缩策略：

```
┌─────────────────────────────────────────────────────────────┐
│ Level 0: Micro Compact (无 LLM 调用)                        │
│   ├── Time-based MC: 超时清理旧工具结果                      │
│   ├── Cached MC:     API cache_edits 删除工具结果            │
│   └── Content-clear: 替换工具输出为 [Old tool result cleared] │
├─────────────────────────────────────────────────────────────┤
│ Level 1: Session Memory Compact (无 LLM 调用)               │
│   ├── 使用已有的 Session Memory 作为摘要                      │
│   ├── calculateMessagesToKeepIndex() 保留最近消息            │
│   └── adjustIndexToPreserveAPIInvariants() 保证工具对完整    │
├─────────────────────────────────────────────────────────────┤
│ Level 2: Full Compact (LLM 调用)                            │
│   ├── 完整消息历史 → LLM 生成 9 段式摘要                     │
│   ├── forked agent (复用主会话 prompt cache)                 │
│   ├── PTL retry: 超大时裁剪最旧 API round 重试              │
│   └── 压缩后恢复: 文件/plan/skill/deferred tools 重新注入    │
├─────────────────────────────────────────────────────────────┤
│ Level 3: Partial Compact (LLM 调用)                         │
│   ├── 'from': 摘要 pivot 之后的消息，保留之前的              │
│   └── 'up_to': 摘要 pivot 之前的消息，保留之后的             │
└─────────────────────────────────────────────────────────────┘
```

**Micro Compact 特性** (microCompact.ts, 531 行)：
- **Time-based**: 如果距上次 assistant 消息超过阈值分钟 → 清理旧工具结果（缓存已过期，不需保护）
- **Cached MC**: 用 API `cache_edits` 在服务端删除旧工具结果，不修改本地消息→缓存不失效
- **COMPACTABLE_TOOLS**: 仅操作 `FileRead/Shell/Grep/Glob/WebSearch/WebFetch/FileEdit/FileWrite`
- **keepRecent**: 始终保留最近 N 个工具结果

### 3.2 NGO 3 级上下文防御

```
┌─────────────────────────────────────────────────────────┐
│ Level 1 (usage > 0.70): LLM 摘要压缩                    │
│   ├── 密度感知裁切点选择                                 │
│   ├── 4 维度结构化摘要                                   │
│   ├── 嵌套摘要替换 (防 summary-of-summary 嵌套)         │
│   └── Vector Memory 钩子 (BeforeCompact → save)         │
├─────────────────────────────────────────────────────────┤
│ Level 2 (usage > 0.95): 紧急截断                        │
│   ├── forceTruncate(8) — 仅保留最后 8 条                 │
│   ├── 安全裁切点 (不切断 tool role)                      │
│   └── 丢弃内容 → Vector Memory                          │
├─────────────────────────────────────────────────────────┤
│ Level 3 (compact > 3次): 原始提取模式                   │
│   ├── 绕过 LLM → 直接提取 assistant 内容前 300 字       │
│   └── 标记 "[Compact limit reached — raw extraction]"   │
└─────────────────────────────────────────────────────────┘
```

**NGO 独有：密度感知裁切** (doCompact):
```go
type turnInfo struct {
    start   int
    density int // len(content) + toolCalls*200
}
// 密度 = 内容长度 + 工具调用数×200
// 按密度选择裁切点，保留高密度 turn
```

**NGO 独有：递归压缩深度守卫**:
```go
a.compactCount++
if a.compactCount > 3 {
    // 跳过 LLM → 原始提取，防递归摘要损失
    summary = "[Compact limit reached — raw extraction]" + raw_snippets
}
```

---

## 4. 摘要生成

### CC — 9 段式结构化 Prompt (375 行)

CC 的摘要 prompt 是一个精心设计的 9 段式模板：

| 段 | 内容 |
|---|------|
| 1 | Primary Request and Intent |
| 2 | Key Technical Concepts |
| 3 | Files and Code Sections (含完整代码片段) |
| 4 | Errors and Fixes |
| 5 | Problem Solving |
| 6 | All User Messages (非工具结果) |
| 7 | Pending Tasks |
| 8 | Current Work |
| 9 | Optional Next Step (含原文引用) |

**关键特性**：
- `<analysis>` scratchpad → 生成后 `formatCompactSummary()` 剥离
- `NO_TOOLS_PREAMBLE` — 强制 LLM 不使用工具
- `NO_TOOLS_TRAILER` — 二次提醒
- 部分压缩有单独的 `PARTIAL_COMPACT_PROMPT` 和 `PARTIAL_COMPACT_UP_TO_PROMPT`
- 支持自定义 `customInstructions`（用户可配置压缩重点）
- 续接提示："Continue the conversation from where it left off without asking the user any further questions"

### NGO — 4 维度检查点 (~20 行)

```go
// 4 维度结构化摘要 (mirrors Anti's CortexStepCheckpoint)
summaryPrompt := `You are a conversation summarizer. Extract:

## user_intent       — 用户目标 + 当前进度
## session_summary   — 本次执行的操作 + 结果
## code_changes      — 修改的文件/函数名/变更点
## learned_facts     — 架构信息/约束/决策

CRITICAL: <preference_knowledge> 和 <semantic_knowledge> 标签内容必须完整保留。

每维度 2-3 句，总计 ≤500 words。`
```

**差异评估**：
- CC 9 段比 NGO 4 维度更详细，特别是 "All User Messages" 和 "Optional Next Step (含原文引用)" 防止任务漂移
- NGO 的 `learned_facts` + KI 保护指令（`<preference_knowledge>` / `<semantic_knowledge>`）是独有设计

---

## 5. 压缩后恢复

### CC — 完整的上下文重建

CC 在压缩后执行一系列恢复操作：

| 恢复项 | 方法 | 说明 |
|--------|------|------|
| 文件状态 | `createPostCompactFileAttachments()` | 最多 5 个文件, 每个 5K token |
| Plan | `createPlanAttachmentIfNeeded()` | 恢复当前 plan.md |
| Plan Mode | `createPlanModeAttachmentIfNeeded()` | 恢复 plan 模式指令 |
| Skills | `createSkillAttachmentIfNeeded()` | 恢复已调用 skill (per-skill 5K cap) |
| Deferred Tools | `getDeferredToolsDeltaAttachment()` | 重新公告延迟工具 |
| Agent Listing | `getAgentListingDeltaAttachment()` | 重新公告 agent 列表 |
| MCP Instructions | `getMcpInstructionsDeltaAttachment()` | 重新注入 MCP 指令 |
| Session Hooks | `processSessionStartHooks('compact')` | 执行 SessionStart hooks |
| Transcript | `reAppendSessionMetadata()` | 重新附加会话元数据 |
| PostCompact Hooks | `executePostCompactHooks()` | 执行 PostCompact hooks |

**Token 预算**:
- `POST_COMPACT_TOKEN_BUDGET = 50,000` (全部恢复项)
- `POST_COMPACT_MAX_FILES_TO_RESTORE = 5`
- `POST_COMPACT_MAX_TOKENS_PER_FILE = 5,000`
- `POST_COMPACT_SKILLS_TOKEN_BUDGET = 25,000`

### NGO — 最小恢复

NGO 的压缩后恢复较简单:
- 保留首条消息
- 注入 `EphCompactionNotice` 提醒 agent 已压缩
- 触发 `AfterCompact` hook (go routine, 异步)
- 重置 `tokenTracker` 基线

**差距**：CC 的压缩后恢复系统确保 agent 在压缩后不丢失关键上下文（文件、plan、skills、工具列表）。NGO 无此能力。

---

## 6. Token 追踪

### CC — 分散式多源估算

CC 使用多种 token 估算方式，分散在各文件：
- `tokenCountWithEstimation()` — 基于 API usage + 本地估算
- `roughTokenCountEstimation(text)` — 字符串 → ~token 转换
- `estimateMessageTokens()` — 遍历消息内容块估算
- `tokenCountFromLastAPIResponse()` — API 返回的精确 token counts

### NGO — 集中式混合追踪器 (101 行)

```go
type TokenTracker struct {
    lastAPIPromptTokens int // API 返回的精确 prompt_tokens
    deltaEstimate       int // 自上次 API 调用以来估算的新增 tokens
    systemPromptTokens  int // 系统 prompt token 数
}

// 估算函数: CJK ≈ 1.5 tokens/char, ASCII ≈ 0.25 tokens/char
func estimateStringTokensHybrid(s string) int {
    for _, r := range s {
        if r >= 0x2E80 { tokens += 1.5 } // CJK
        else           { tokens += 0.25 } // ASCII
    }
}
```

**NGO 优势**:
- 集中式管理，单一数据源
- **CJK 感知估算** — 中文字符 1.5 token (更精确)
- **混合精度**: API 基线精确 + 增量估算 (误差 ±5% vs CC 的 ±30%)
- `Reset()` 在 compact 后调用
- `SetSystemPromptSize()` 记录实际系统 prompt 大小

---

## 7. 消息清洗

### CC — messages.ts (5512 行, 部分)

CC 的消息清洗分散在 messages.ts 中，包括：
- `normalizeMessagesForAPI()` — 合并同 message.id 的流式消息
- `ensureToolResultPairing()` — 确保 tool_use/tool_result 配对
- `stripImagesFromMessages()` — 压缩前剥离图片 (节省 token)
- `stripReinjectedAttachments()` — 剥离将被重新注入的附件
- `getMessagesAfterCompactBoundary()` — 获取边界后的消息

### NGO — sanitize.go (129 行)

NGO 的清洗更集中，2 个核心函数：

| 函数 | 功能 | CC 等价 |
|------|------|---------|
| `sanitizeMessages()` | 3-pass 孤儿修复 (callIDs → resultIDs → rebuild) | `ensureToolResultPairing()` |
| `enforceTurnOrdering()` | 合并连续同角色消息 → strict alternation | `normalizeMessagesForAPI()` |

```go
// 3-pass 孤儿修复
Pass 1: 收集所有 tool_call IDs → callIDs map
Pass 2: 收集所有 tool result IDs → resultIDs map
Pass 3: 重建 — 丢弃 orphan results, 剥离 orphan tool_calls
```

**NGO 独有**：
- `enforceTurnOrdering()` — 合并连续同角色消息（CC 在 normalizeMessagesForAPI 中处理）
- **保护 tool_calls**: "Never merge assistant messages with tool_calls"

---

## 8. 向量记忆集成

### CC — Session Memory (21 KB)

CC 的 Session Memory 是一个**并行运行的异步提取系统**：
- 在消息流中异步提取 session memory
- `waitForSessionMemoryExtraction()` — compact 时等待提取完成
- Session Memory 可以**直接替代** full compact（无需 LLM 调用）
- 粒度控制：`minTokens: 10K`, `minTextBlockMessages: 5`, `maxTokens: 40K`
- `truncateSessionMemoryForCompact()` — 避免 SM 占满 post-compact token budget

### NGO — Vector Memory + KI 去重 (79 行)

```go
type MemoryCompactHook struct {
    store     MemoryStorer         // Vector DB
    kiDedup   KIDuplicateChecker   // KI 重复检查器
    threshold float64              // 余弦相似度阈值 (0.75)
}

// BeforeCompact: 压缩前保存到向量记忆
for _, msg := range history {
    // KI 去重: 跳过已有知识覆盖的内容
    if kiDedup.FindDuplicate(msg.Content, 0.75) {
        skipped++ // "KI covers it, score=0.85"
        continue
    }
    content.WriteString("[" + msg.Role + "]: " + msg.Content)
}
store.Save(sessionID, content.String())
```

**NGO 独有：KI 去重**:
- 对每条即将丢弃的消息，检查 KI（知识库）中是否已有相似内容
- 余弦相似度 > 0.75 → 跳过保存（因为 KI 已覆盖）
- 防止 Memory Store 与 KI Store 的信息重复

---

## 9. 差距总览矩阵

| 维度 | CC | NGO | Level |
|------|-----|-----|-------|
| **代码量** | ~145 KB | ~7 KB | 21x |
| 微压缩 (工具结果清理) | ✅ 3 种策略 | ❌ | ❌ **重大** |
| API Cache Editing | ✅ 服务端删除 | ❌ | ❌ |
| 时间触发微压缩 | ✅ 缓存过期后 | ❌ | ⚠️ |
| Session Memory 替代 Compact | ✅ 无需 LLM | ❌ | ❌ |
| 部分压缩 (from/up_to) | ✅ | ❌ | ⚠️ |
| 9 段式摘要 Prompt | ✅ 375 行 | ⚠️ 4 维度 | ⚠️ |
| `<analysis>` 草稿区 | ✅ 提高摘要质量 | ❌ | ⚠️ |
| 用户自定义压缩指令 | ✅ customInstructions | ❌ | ⚠️ |
| 压缩后文件/Plan/Skill 恢复 | ✅ 完整重建 | ❌ | ❌ **重大** |
| PTL 重试 (prompt-too-long) | ✅ 3 次裁剪重试 | ❌ | ⚠️ |
| 断路器 (连续失败停止) | ✅ 3 次 | ❌ | ⚠️ |
| 图片剥离 | ✅ image→[image] | ❌ | ⚠️ |
| 密度感知裁切 | ❌ | ✅ 内容长度+工具×200 | ✅ NGO独有 |
| 递归压缩深度守卫 | ❌ | ✅ compactCount>3→raw | ✅ NGO独有 |
| 嵌套摘要替换 | ❌ | ✅ [对话摘要] 前缀检测 | ✅ NGO独有 |
| CJK Token 估算 | ❌ 统一比率 | ✅ CJK 1.5 / ASCII 0.25 | ✅ NGO独有 |
| 混合精确+估算 Token | ⚠️ 分散实现 | ✅ 集中 TokenTracker | ✅ NGO优 |
| Vector Memory + KI 去重 | ❌ | ✅ 0.75 余弦阈值 | ✅ NGO独有 |
| 3 级上下文防御 (状态机) | ❌ | ✅ 0.70/0.95 | ✅ NGO独有 |
| 紧急截断 (forceTruncate) | ❌ 仅降级压缩 | ✅ 保留 8 条 | ✅ NGO独有 |
| 集中式消息清洗 | ❌ 分散 5512 行 | ✅ 129 行 2 函数 | ✅ NGO优 |
| 孤儿工具对修复 | ✅ | ✅ 3-pass | ≡ |
| Hook 集成 (Before/AfterCompact) | ✅ Pre/PostCompact hooks | ✅ BeforeCompact→memory | ≡ |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **微压缩 — 工具结果清理** — 在 NGO 的 doGenerate 前增加 microcompact pass，清理旧的 tool result 内容为 `[cleared]`，减少 30-50% 上下文占用而无需 LLM 调用
2. **压缩后文件恢复** — compact 完成后，重新注入最近读取/修改的文件内容（最多 5 个文件），防止 agent 在 compact 后 "忘记" 工作上下文
3. **增强摘要 Prompt** — 从 4 维度扩展到 7- 9 段，增加 "All User Messages"（防漂移）、"Current Work"（续接点）和 "Errors and Fixes"（避免重犯）

### P1 — 架构增强

4. **`<analysis>` 草稿区** — 在摘要 prompt 中增加 `<analysis>` 预分析步骤，生成后剥离，提高摘要质量
5. **PTL 重试** — 当 compact LLM 调用本身碰到 prompt-too-long 时，裁剪最旧 API round 重试（最多 3 次）
6. **断路器** — 连续 compact 失败 3 次后停止重试，避免浪费 API 调用
7. **customInstructions** — 支持用户在配置中自定义压缩重点（如 "focus on test output and code changes"）

### P2 — 长期演进

8. **Session Memory 替代 Compact** — 开发并行的 session memory 提取系统，在有 SM 数据时直接使用（跳过 LLM 调用）
9. **Partial Compact** — 支持 from/up_to 方向的部分压缩，保留 prompt cache
10. **缓存感知压缩** — 在 API 层使用 `cache_edits` 删除旧工具结果而不破坏缓存前缀
