# Phase 6: 会话/存储/记忆层深度对标分析

> CC `sessionStorage.ts` (5106 行) + `SessionMemory/` (1026 行) + `memdir/` (727 行)
> vs NGO `brain/store.go` (423 行) + `knowledge/` (712 行) + `memory/` (509 行) + `persistence/history.go` (108 行) + Hook 层 (173 行)

---

## 目录

- [1. 架构概览](#1-架构概览)
- [2. 会话持久化](#2-会话持久化)
- [3. 会话记忆系统](#3-会话记忆系统)
- [4. 知识存储 (跨会话记忆)](#4-知识存储-跨会话记忆)
- [5. 向量记忆 (语义召回)](#5-向量记忆-语义召回)
- [6. 时间轴记忆 (日记系统)](#6-时间轴记忆-日记系统)
- [7. 会话产物存储 (Brain)](#7-会话产物存储-brain)
- [8. 差距总览矩阵](#8-差距总览矩阵)
- [9. 可移植设计建议](#9-可移植设计建议)

---

## 1. 架构概览

### 记忆系统架构模型对比

```
CC — 两层记忆模型                    NGO — 四层记忆模型
┌─────────────────────┐             ┌─────────────────────────┐
│ Session Memory      │             │ KI Store (knowledge/)   │
│ (会话内 markdown)    │             │ 跨会话持久知识           │
│ forked agent 异步    │             │ 3 级注入 + 语义检索      │
│ 提取→compact替代     │             ├─────────────────────────┤
├─────────────────────┤             │ Vector Memory (memory/) │
│ MEMORY.md (memdir/) │             │ 对话片段向量存储          │
│ 4 类型分类法          │             │ 时间衰减 + 容量淘汰      │
│ 跨会话 per-project   │             ├─────────────────────────┤
│ private + team 双目录 │             │ Diary (memory/diary/)   │
├─────────────────────┤             │ 每日 markdown 时间轴     │
│ JSONL Transcript    │             │ LLM 可选 consolidation  │
│ sessionStorage.ts    │             ├─────────────────────────┤
│ 5106 行              │             │ Brain (brain/store.go)  │
│ Project class 管理   │             │ 会话级 artifact 存储     │
└─────────────────────┘             │ 版本链 + 解析管线        │
                                    ├─────────────────────────┤
                                    │ DB Persistence          │
                                    │ (GORM history.go)       │
                                    │ 增量/事务写入            │
                                    └─────────────────────────┘
```

### 代码量对比

| 存储关注点 | CC | NGO | 说明 |
|-----------|-----|-----|------|
| 会话持久化 | 5106 行 (sessionStorage.ts) | 108 行 (history.go) | CC 用 JSONL / NGO 用 GORM |
| 会话记忆 | 1026 行 (SessionMemory/) | **无直接等价** | NGO 用 Vector Memory 替代 |
| 跨会话记忆 | 727 行 (memdir/) | 712 行 (knowledge/) | 架构不同但量相当 |
| 向量召回 | **无** | 509 行 (memory/) | NGO 独有 |
| 时间轴记忆 | **无** | 185 行 (diary.go) + 94 行 (diary_hook.go) | NGO 独有 |
| 产物存储 | 分散在 sessionStorage | 423 行 (brain/store.go) | NGO 更集中 |
| **合计** | ~6859 行 | ~2031 行 | 3.4x |

---

## 2. 会话持久化

### CC — JSONL Transcript (5106 行)

CC 的 `sessionStorage.ts` 是一个**企业级 JSONL 持久化引擎**：

| 特性 | 实现 |
|------|------|
| 格式 | JSONL（每行一条 JSON，追加写入） |
| 写入队列 | `writeQueues` Map + `scheduleDrain()` (100ms 批次) |
| 最大块 | `MAX_CHUNK_BYTES = 100MB` (防 OOM) |
| 最大读取 | `MAX_TRANSCRIPT_READ_BYTES = 50MB` |
| 墓碑重写 | `MAX_TOMBSTONE_REWRITE_BYTES = 50MB` |
| Project 类 | 单例管理：tag, title, agentName, mode, worktree... |
| Session 元数据 | `reAppendSessionMetadata()` — 确保 tail 窗口可读 |
| 子代理 | `getAgentTranscriptPath()` — 每个 subagent 独立 JSONL |
| 远程代理 | `RemoteAgentMetadata` — CCR v2 事件写入 |
| 链式 UUID | `parentUuid` 链保证消息顺序不丢失 |
| 清理注册 | `registerCleanup(flush + reAppendMetadata)` |

**CC 的关键设计**：
- 会话文件路径基于 `sanitizePath(projectDir)` + `sessionId.jsonl`
- `pendingEntries` 缓冲 → `materializeSessionFile` 延迟写入
- `isChainParticipant()` — progress 消息不参与 parentUuid 链
- 后台写入追踪：`pendingWriteCount` + `flushResolvers`

### NGO — GORM ORM (108 行)

```go
type HistoryMessage struct {
    ID         uint   `gorm:"primarykey"`
    SessionID  string `gorm:"index"`
    Role       string // user / assistant / tool / system
    Content    string `gorm:"type:text"`
    ToolCalls  string `gorm:"type:text"` // JSON-encoded
    ToolCallID string
    Reasoning  string `gorm:"type:text"` // Thinking content
    CreatedAt  time.Time
}
```

| 操作 | 方法 | 说明 |
|------|------|------|
| 全量替换 | `SaveAll()` | 事务删除+批量插入 (compact 后) |
| 增量追加 | `AppendBatch()` | 事务批量 INSERT (正常轮次) |
| 加载全量 | `LoadSession()` | `ORDER BY created_at ASC` |
| 加载最近 | `LoadSessionRecent(limit)` | 子查询 + ASC |
| 截断 | `TruncateSession(keep)` | 保留最后 N 条 |
| 删除 | `DeleteSession()` | 全量清除 |

**NGO 优势**：
- 关系数据库 + ORM → 结构化查询能力强（按时间/角色/session 过滤）
- `AutoMigrate` 自动 DDL
- 批量 INSERT 减少锁竞争
- 增量持久化：`persistedCount` 跟踪基线，仅写入新消息

**NGO 独有：增量持久化逻辑**:
```go
// loop.go
func (a *AgentLoop) persistHistory() {
    newMsgs := a.history[a.persistedCount:]  // 仅新增部分
    historyStore.AppendBatch(sessionID, exports)
    a.persistedCount = len(a.history)
}
```

---

## 3. 会话记忆系统

### CC — Session Memory (496 行 + 324 行 + 207 行)

CC 的 Session Memory 是一个**异步并行运行的会话内信息提取器**：

```
流程:
主会话 → postSamplingHook → shouldExtractMemory?
  ├── YES → markExtractionStarted()
  │         setupSessionMemoryFile()
  │         runForkedAgent('session_memory')
  │         markExtractionCompleted()
  └── NO  → return
```

**触发条件** (必须同时满足):
```typescript
const shouldExtract =
  (hasMetTokenThreshold && hasMetToolCallThreshold) ||
  (hasMetTokenThreshold && !hasToolCallsInLastTurn)
```

| 阈值 | 默认值 | 说明 |
|------|--------|------|
| `minimumMessageTokensToInit` | GrowthBook 配置 | 首次初始化 token 阈值 |
| `minimumTokensBetweenUpdate` | GrowthBook 配置 | 更新间隔 token 最小值 |
| `toolCallsBetweenUpdates` | GrowthBook 配置 | 更新间隔工具调用最小数 |

**关键实现**:
- 使用 `runForkedAgent` 复用主会话 prompt cache → 零额外 cache 成本
- `createMemoryFileCanUseTool()` — 仅允许对 memory 文件使用 FileEdit
- 提取结果写入 markdown 文件至 `~/.claude/memory/`
- 可作为 **compact 的直接替代** (Phase 5 Session Memory Compact)

### NGO — 无直接等价

NGO 没有 "会话内异步提取" 机制，但通过 **3 个组件覆盖了部分功能**:
1. `MemoryCompactHook.BeforeCompact()` — compact 时保存到向量记忆
2. `DiaryHook.OnRunComplete()` — 每次 run 结束记录日记
3. `KI Store` — 跨会话知识持久化（由 agent 工具主动触发）

---

## 4. 知识存储 (跨会话记忆)

### CC — MEMORY.md + memdir/ (727 行)

CC 的跨会话记忆使用 **Markdown 文件 + 4 类型分类法**：

**4 种记忆类型** (memoryTypes.ts, 272 行):

| 类型 | 用途 | Scope |
|------|------|-------|
| `user` | 用户角色/偏好/知识背景 | always private |
| `feedback` | 用户对 agent 行为的反馈（纠正+确认） | default private |
| `project` | 项目进度/目标/deadline/事件 | bias team |
| `reference` | 外部系统指针（Linear/Grafana/Slack） | usually team |

**关键设计**:
- **双目录**: private + team（private 覆盖 team 同名记忆）
- **Frontmatter**: `name`, `description`, `type` → 决定召回相关性
- **Memory Age**: `memoryAge.ts` — 记忆过期检测
- **Memory Scan**: `memoryScan.ts` — 扫描+聚合记忆文件
- **记忆漂移警告**: MEMORY_DRIFT_CAVEAT — 使用前验证! ("memory says X exists ≠ X exists now")
- **信任校验**: `TRUSTING_RECALL_SECTION` — 如果记忆提到文件/函数，必须 grep 确认存在
- **负面过滤**: `WHAT_NOT_TO_SAVE_SECTION` — 不保存代码模式/git 历史/临时任务

### NGO — KI Store (348 行) + Retriever (154 行) + Vector Index (210 行)

NGO 的知识系统采用 **3 层分级注入架构**：

```
┌──────────────────────────────────────────────────┐
│ L0: Preference KI (全量注入)                      │
│   tag: "preference" → GeneratePreferenceKI()     │
│   完整 overview.md 内容 → 系统 prompt             │
│   等价: CC MEMORY.md 中的 user/feedback 类型      │
├──────────────────────────────────────────────────┤
│ L1: KI Index (发现层)                             │
│   GenerateKIIndex() → title + summary + 📄 paths │
│   Agent 通过 read_file 按需读取完整内容           │
│   等价: CC Memory 的 description 字段             │
├──────────────────────────────────────────────────┤
│ L2: Semantic Retrieval (语义层)                   │
│   Retriever.RetrieveForPrompt(query, topK, budget)│
│   embedding → cosine similarity → budget裁切     │
│   等价: CC 无直接等价                              │
└──────────────────────────────────────────────────┘
```

**KI Store 核心操作**:

| 方法 | 功能 | 说明 |
|------|------|------|
| `Save(item)` | 创建/更新 KI | ID = sanitize(title) + timestamp |
| `GetWithContent(id)` | 读取 KI + overview.md | 确保内容新鲜 |
| `UpdateMerge(id, append, summary)` | 追加合并 | 保留旧内容 |
| `ReplaceMerge(id, newContent, summary)` | 全量替换合并 | LLM 整合后使用 |
| `SaveDistilled(title, summary, content, tags, sources)` | 自动蒸馏接口 | Hook 调用 |

**VectorIndex (210 行)** — 纯 Go 向量引擎:
- **brute-force** 余弦相似度 (< 1000 items 不需要 HNSW)
- 持久化: `mapping.json` + `vectors.bin` (raw float32 binary)
- 线程安全: `sync.RWMutex`
- 增量构建: `BuildIndex()` 仅索引未收录的 KI

---

## 5. 向量记忆 (语义召回)

### CC — 无

CC 没有独立的对话片段向量记忆系统。其记忆依赖:
- MEMORY.md 文件（文本匹配 + 手动管理）
- Session Memory markdown（LLM 提取）

### NGO — Memory Store (324 行)

NGO 拥有完整的**对话片段向量记忆系统**:

```go
type Store struct {
    embedder     knowledge.Embedder
    index        *knowledge.VectorIndex
    contents     map[string]fragmentMeta // id → metadata
    halfLifeDays int    // 时间衰减半衰期 (默认 30 天)
    maxFragments int    // 容量限制 (0=无限)
}
```

**核心能力**:

| 特性 | 实现 |
|------|------|
| 保存 | `Save()` — 段落感知切分 (500 char) + 批量嵌入 |
| 搜索 | `Search()` — overfetch 2x → 时间衰减重排序 |
| 时间衰减 | `finalScore = cosine × 1/(1 + ageDays/halfLife)` |
| 容量淘汰 | `evictIfNeeded()` — 按创建时间驱逐最旧片段 |
| Prompt 注入 | `FormatForPrompt()` — 带 score/age 的格式化输出 |
| 持久化 | `fragments.json` + `vectors.bin` |

**时间衰减公式**:
```
finalScore = cosine_similarity × 1/(1 + age_in_days / half_life_days)
```
- 默认半衰期 30 天 → 30 天前的记忆权重减半
- 兼顾相关性与新鲜度

**NGO 独有 — 与 CompactHook 联动**:
```
doCompact() → Hooks.FireBeforeCompact(middle)
  → MemoryCompactHook.BeforeCompact()
    → KI 去重检查 (cosine > 0.75 → skip)
    → memory.Store.Save(sessionID, content)
      → splitChunks(500) → embedder.EmbedBatch → index.Add + persist
```

---

## 6. 时间轴记忆 (日记系统)

### CC — 无

CC 没有日记/时间轴记忆系统。

### NGO — Diary Store (185 行) + Diary Hook (94 行)

NGO 拥有独有的**每日 Markdown 日记系统**:

```
~/.ngoagent/memory/diary/
├── 2026-03-29.md    # 日记 2026-03-29
├── 2026-03-30.md    # 日记 2026-03-30
└── 2026-03-31.md    # 日记 2026-03-31
```

**日记条目格式**:
```markdown
# 日记 2026-03-31

### 19:41 | abc12345
- **任务**: 深度分析CC源代码和NGOAgent架构 (steps=15, tools=8)
- **步骤**: 15 steps, 8 tools
- **结果**: ✅ 完成
```

**Diary Hook** — 自动记录:
```go
func (h *DiaryHook) OnRunComplete(ctx context.Context, info RunInfo) {
    // 跳过纯 Q&A (无工具使用)
    if info.Steps < 1 { return }
    // 异步写入日记
    go h.diary.Append(DiaryEntry{...})
}
```

**高级功能**:
- `ReadRange(from, to)` — 日期范围查询
- `ReadRecent(days)` — 最近 N 天 (默认 7) → prompt 注入
- `Consolidate(date, summarizer)` — LLM 日报摘要压缩
- `ListDates()` — 所有可用日期（最新优先）

---

## 7. 会话产物存储 (Brain)

### CC — 分散在 sessionStorage

CC 的会话产物分散在 `sessionStorage.ts` 中：
- `saveTranscriptMessage()` — 对话消息存储
- `saveContentReplacement()` — 内容替换记录
- `saveAttributionSnapshot()` — 归因快照
- `saveFileHistorySnapshot()` — 文件历史快照
- `Project` 类管理元数据（title, tag, agent, mode, worktree, PR...）

### NGO — Brain ArtifactStore (423 行)

NGO 的 Brain 是一个**版本化的 session-scoped 产物存储系统**:

```go
type ArtifactStore struct {
    baseDir      string // ~/.ngoagent/brain/<session_id>/
    workspaceDir string // workspace root for file resolution
}
```

**核心能力**:

| 特性 | 方法 | 说明 |
|------|------|------|
| 版本链 | `rotateVersion()` | .md → .resolved.0, .resolved.1... |
| 解析管线 | `resolveLinks()` | 文件名→file:// URI 自动转换 |
| Snapshot | `Snapshot(label)` | 全量产物快照 |
| Metadata | `.metadata.json` | Summary, Type, Version, Timestamps |
| Write | `WriteArtifact()` | 带类型+摘要的写入 |
| Context 注入 | `ContextWithBrainStore()` | context.Context 传递 |

**Resolution Pipeline** (独有):
```go
// Phase 1: backtick-quoted → [name](file://abs_path)
// Phase 2: [MODIFY|NEW|DELETE] tag → clickable link
// Phase 3: bare absolute paths → clickable link
```

3 阶段 regex 管线自动将产物中的文件引用转换为可点击的 `file://` URI。

---

## 8. 差距总览矩阵

| 维度 | CC | NGO | 评估 |
|------|-----|-----|------|
| **会话持久化** | JSONL 5106行 + Project 类 | GORM 108行 | ≡ 各有优势 |
| UUID 链式完整性 | ✅ parentUuid chain | ❌ | ⚠️ |
| 写入队列+批次 | ✅ 100ms drain | ✅ AppendBatch | ≡ |
| 子代理 transcript | ✅ per-agent JSONL | ❌ | ⚠️ |
| 远程代理元数据 | ✅ CCR v2 | ❌ | — (场景不同) |
| Session 元数据 tail 保持 | ✅ reAppendSessionMetadata | ❌ | ⚠️ |
| **会话记忆** | ✅ forked agent 异步提取 | ❌ 无直接等价 | ❌ **重大** |
| postSamplingHook | ✅ | ❌ | ❌ |
| compact 替代能力 | ✅ SM → 无 LLM compact | ❌ | ❌ |
| **跨会话记忆** | ✅ MEMORY.md 4 类型 | ✅ KI 3 层注入 | ≡ 各有优势 |
| 记忆类型分类 | ✅ user/feedback/project/reference | ⚠️ tags (preference/...) | ⚠️ |
| Private/Team 双目录 | ✅ | ❌ | ⚠️ |
| 漂移警告 (使用前验证) | ✅ MEMORY_DRIFT_CAVEAT | ❌ | ⚠️ |
| 负面过滤 (不该存什么) | ✅ WHAT_NOT_TO_SAVE | ❌ | ⚠️ |
| 信任校验 (grep 验证) | ✅ TRUSTING_RECALL | ❌ | ⚠️ |
| 3 层分级注入 (L0/L1/L2) | ❌ | ✅ Preference / Index / Semantic | ✅ **NGO独有** |
| 语义检索 (embedding) | ❌ | ✅ VectorIndex + Retriever | ✅ **NGO独有** |
| KI 去重 (cosine 0.75) | ❌ | ✅ FindDuplicate() | ✅ **NGO独有** |
| KI 合并 (append/replace) | ❌ | ✅ UpdateMerge/ReplaceMerge | ✅ **NGO独有** |
| 自动蒸馏接口 | ❌ | ✅ SaveDistilled() | ✅ **NGO独有** |
| **向量记忆** | ❌ | ✅ 324行 完整系统 | ✅ **NGO独有** |
| 时间衰减 | ❌ | ✅ halfLife=30d | ✅ **NGO独有** |
| 容量淘汰 | ❌ | ✅ maxFragments | ✅ **NGO独有** |
| **时间轴记忆** | ❌ | ✅ 185行 Diary | ✅ **NGO独有** |
| 日报 consolidation | ❌ | ✅ LLM summarizer | ✅ **NGO独有** |
| **产物存储** | 分散 | ✅ Brain 423行 集中式 | ✅ NGO优 |
| 版本链 | ❌ | ✅ .resolved.N | ✅ **NGO独有** |
| Resolution Pipeline | ❌ | ✅ 文件名→file:// | ✅ **NGO独有** |
| Snapshot 全量快照 | ❌ | ✅ .snapshots/ | ✅ **NGO独有** |

---

## 9. 可移植设计建议

### P0 — 即刻收益

1. **会话记忆异步提取** — 实现 postSamplingHook 机制，在每次 LLM 响应后异步提取关键信息至 markdown 文件，作为 compact 的零 LLM 成本替代路径
2. **记忆漂移警告** — 在 KI 召回时注入 CC 的 `MEMORY_DRIFT_CAVEAT` + `TRUSTING_RECALL_SECTION` 逻辑: 如果记忆提到文件/函数 → agent 必须先验证存在性
3. **负面过滤规则** — 在 KI 保存接口添加 CC 的 `WHAT_NOT_TO_SAVE` 规则: 不保存代码模式、git 历史、已文档化的内容

### P1 — 架构增强

4. **记忆类型分类法** — 将 KI tags 扩展为结构化类型: `user` (用户画像), `feedback` (行为反馈), `project` (进度追踪), `reference` (外部指针)
5. **Private/Team 双层** — 支持用户级 + 项目级知识分离（类似 CC 的 private/team 双目录）
6. **子代理 Transcript** — 为每个 subagent 创建独立的持久化通道，便于回溯和恢复

### P2 — 长期演进

7. **Diary Consolidation 自动化** — 每日自动触发 `Consolidate()` 生成日报摘要，过期详细记录可归档
8. **Memory Frontmatter** — 在 KI overview.md 中引入 YAML frontmatter (name, description, type)，提高发现效率
9. **会话恢复增强** — 参考 CC 的 Session 元数据 tail 保持策略，确保 `--resume` 时元数据（title, tags）始终可达
