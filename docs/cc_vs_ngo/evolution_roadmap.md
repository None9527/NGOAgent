# NGOAgent 全量演进计划 (DDD 架构分类)

> 将 CC 9-Phase 对标的 49 维度 + 81 项可移植能力，按 `internal/` DDD 三层架构重组
> 已完成项标 ✅，待做项标优先级 P0/P1/P2/P3

---

## 架构总览

```
internal/
├── domain/                    ← 核心业务 (0 外部依赖)
│   ├── service/  (25 files)   ← AgentLoop 状态机 + Guard + TokenTracker
│   ├── tool/     (2 files)    ← ToolMeta 抽象定义
│   └── entity/   (3 files)    ← Skill/Session/EvoRun 实体
│
├── infrastructure/            ← 技术实现
│   ├── llm/      (7 files)    ← Provider/Router/Errors/StreamAdapter
│   ├── tool/     (26 files)   ← 27 个工具实现
│   ├── security/ (1 file)     ← SecurityHook + ApprovalGate
│   ├── prompt/   (3 files)    ← PromptEngine + Discovery
│   ├── skill/    (2 files)    ← SkillManager + Bundled
│   ├── config/   (5 files)    ← YAML 加载 + HotReload
│   ├── knowledge/(4 files)    ← KI Store + VectorIndex
│   ├── memory/   (2 files)    ← 向量记忆 + Diary
│   └── brain/    (1 file)     ← ArtifactStore
│
├── interfaces/                ← 外部接入
│   ├── server/   (4 files)    ← HTTP/WS + Doctor + Slash
│   ├── apitype/  (1 file)     ← 响应类型
│   ├── grpc/                  ← gRPC proto-first
│   └── bot/                   ← Telegram
│
├── application/               ← 组装层
│   └── builder.go (648 lines) ← DI 容器 + 8-phase 启动
│
└── testing/                   ← 集成测试
```

---

## 🏛️ Layer 1: domain/ — 核心业务逻辑

### 📦 domain/service — AgentLoop 状态机

> 最核心的包，所有运行时行为的源头。当前 run.go 1653 行。

#### ✅ 已完成

| # | 能力 | CC 来源 | 文件 | 审计 |
|---|------|---------|------|------|
| 1 | 并发工具执行 (ReadOnly errgroup) | Ph1 | run.go | ✅ |
| 2 | 混合批次拆分 (ReadOnly 并发 + Write 串行) | Ph1 | run.go | ✅ |
| 3 | splitToolCalls 分割器 | — | run.go | ✅ |
| 4 | 磁盘溢出 (大输出 /tmp 落盘) | Ph1 | run.go | ✅ |
| 5 | 微压缩 (已消化 tool result clearing) | Ph5 | run.go | ✅ |
| 6 | compact 后注入文件路径 | Ph5 | run.go | ✅ |
| 7 | 7 维摘要 prompt (从 4D 升级) | Ph5 | run.go | ✅ |
| 8 | Analysis CoT (`<analysis>` 草稿区) | — | run.go | ✅ |
| 9 | Max Output 续写 (StopReason=length, 3 轮) | Ph7 | run.go | ✅ |
| 10 | PTL 两级恢复 (compact → forceTruncate) | Ph7 | run.go | ✅ |
| 11 | Fallback 切换 (ResolveWithFallback) | Ph7 | run.go | ✅ |
| 12 | outputContinuations 计数器 | — | loop.go | ✅ |
| 13 | Guard repetition_near → _loop_breaker 注入 | — | guard.go | ✅ |
| 14 | Guard tool_cycle → _stuck_recovery 注入 | — | guard.go | ✅ |
| 15 | scrubSensitiveContent 负面过滤 | Ph6 | hooks.go | ✅ |
| 16 | USD 成本追踪 (TokenTracker) | Ph1 | token_tracker.go | ✅ |
| 17 | 多模型 Usage 累积 | Ph1 | token_tracker.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 18 | **拆分 run.go** → compact.go + tool_exec.go | P1 | — | 0 (重构) | 🟢 低 |
| 19 | **主动式 Session Memory** — 后台 goroutine 定期摘要 | P1 | Ph6 #19 | +400 | 🟡 中 (goroutine 泄漏) |
| 20 | **Coordinator 4-Phase Pipeline** — Research→Synthesize→Implement→Verify | P1 | Ph9 #40 | +400 | 🟡 中 (prompt 膨胀) |
| 21 | **DreamTask** — idle 时后台预加载/索引 | P2 | Ph9 #43 | +200 | 🟢 低 |
| 22 | **Worker Transcript** — subagent 独立 history 持久化 | P2 | Ph9 #42 | +150 | 🟢 低 |
| 23 | **前台/后台分流** — 后台 429 直接放弃不重试 | P1 | Ph7 #26 | +60 | 🟢 低 |
| 24 | **工具重型压缩** — 大型 tool output 专项压缩策略 | P1 | Ph5 #16 | +200 | 🟡 中 |
| 25 | **Git diff 压缩** — 保留 diff 结构的智能压缩 | P2 | Ph5 #17 | +150 | 🟢 低 |

---

### 📦 domain/tool — 工具元数据

#### ✅ 已完成

| # | 能力 | 文件 | 审计 |
|---|------|------|------|
| 26 | ToolMeta 体系 (AccessLevel + ConcurrencySafe) | tool.go | ✅ |
| 27 | DefaultMeta 表 (27 工具全覆盖) | tool.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | 估行 | 风险 |
|---|------|--------|------|------|
| 28 | **ToolSearch 延迟加载** — 大工具集 token 压缩 | P2 | +100 | 🟡 中 |
| 29 | **全量 Schema 强化** — map[string]any → typed struct 代码生成 | P3 | +300 | 🟢 低 |

---

## ⚙️ Layer 2: infrastructure/ — 技术实现

### 📦 infrastructure/llm — LLM 供应商层

#### ✅ 已完成

| # | 能力 | CC 来源 | 文件 | 审计 |
|---|------|---------|------|------|
| 30 | 持久重试 (429×8/15s, 500×5/30s) | Ph7 #25 | errors.go | ✅ |
| 31 | ContextOverflow maxRetries 1→2 | Ph7 | errors.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 32 | **Cache Break Detection (Lite)** — prompt hash 变化频率 | P2 | Ph7 #27 | +200 | 🟡 中 |
| 33 | **OAuth Token Refresh** — Provider.RefreshAuth() | P3 | Ph7 #31 | +100 | 🟢 低 |
| 34 | **Provider 健康检查** — Resolve 前 ping | P2 | Ph7 | +80 | 🟢 低 |
| 35 | **API Telemetry** — 结构化延迟/token/cache 遥测 | P3 | Ph10 #18 | +300 | 🟡 中 |

---

### 📦 infrastructure/tool — 工具实现 (27 个)

#### ✅ 已完成

| # | 能力 | 文件 | 审计 |
|---|------|------|------|
| 36 | ValidatePath (symlink 解析 + 敏感路径) | path_validation.go [新] | ✅ |
| 37 | write_file Schema 强化 (minLength + 描述) | write_file.go | ✅ |
| 38 | edit_file Schema 强化 | edit_file.go | ✅ |
| 39 | run_command Schema 强化 | run_command.go | ✅ |
| 40 | write_file ValidatePath 集成 | write_file.go | ✅ |
| 41 | edit_file ValidatePath 集成 | edit_file.go | ✅ |
| 42 | MCP MetaProvider 映射 | mcp_adapter.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 43 | **read_file ValidatePath** — 对称性集成 | P1 | — | +7 | 🟢 低 |
| 44 | **inode 文件修改检测** — 外部编辑感知 | P2 | Ph4 #12 | +200 | 🟡 中 |
| 45 | **工具数量扩充** — CC 30+ vs NGO 20+ | P2 | Ph3 #7 | +500 | 🟢 低 |

---

### 📦 infrastructure/security — 安全策略

#### ✅ 已完成

| # | 能力 | CC 来源 | 文件 | 审计 |
|---|------|---------|------|------|
| 46 | Pattern BlockList `ToolName(argPattern)` | Ph4 | hook.go | ✅ |
| 47 | ReadOnly 自动放行 (ask mode) | Ph4 | hook.go | ✅ |
| 48 | extractSubCommands AST-lite | Ph4 | hook.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 49 | **AI 安全分类器** — sideQuery 小模型判断操作风险 | P3 | Ph4 | +300 | 🟡 中 |
| 50 | **Bash 引号/转义感知** — AST-lite → mvdan.cc/sh 升级 | P3 | Ph4 | +150 | 🟢 低 |

---

### 📦 infrastructure/prompt — Prompt 工程

#### ✅ 已完成

| # | 能力 | CC 来源 | 文件 | 审计 |
|---|------|---------|------|------|
| 51 | DRIFT_CAVEAT semantic_memory 漂移警告 | Ph6 #23 | engine.go | ✅ |
| 52 | discoverUpward 向上遍历 | Ph2 | discovery.go | ✅ |
| 53 | EphCompactionNotice 常量 | Ph5 | prompttext.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 54 | **Coordinator Prompt 规范** — 370 行编排指令移植 | P1 | Ph9 #41 | +300 | 🟡 prompt 膨胀 |
| 55 | **Tool result 注入策略优化** — CC 式 system prompt 附加 | P2 | Ph2 #6 | +100 | 🟢 低 |
| 56 | **CC Code Style 规则吸收** — "no comments", "verify before report" | P1 | Ph2 #7 | +40 | 🟢 低 |
| 57 | **@include 递归** — user_rules 引用外部文件 | P3 | Ph2 | +80 | 🟢 低 |
| 58 | **Attachment 引擎** — IDE 结构化文件附件 | P3 | Ph2 | +200 | 🟡 中 |

---

### 📦 infrastructure/skill — 技能系统

#### ✅ 已完成

| # | 能力 | 文件 | 审计 |
|---|------|------|------|
| 59 | Bundled Skills (loop_breaker + stuck_recovery + debug_helper) | bundled.go [新] | ✅ |
| 60 | RegisterBundled() 启动链接入 | builder.go + builder_phases.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 61 | **Guard 动态读取 bundled content** — 非硬编码字符串 | P2 | — | +30 | 🟢 低 |
| 62 | **Skill KI 分类** — 类型分类法 (当 KI>50 时) | P3 | — | +100 | 🟢 低 |

---

### 📦 infrastructure/knowledge + memory — 记忆系统

#### ✅ 已完成

| # | 能力 | CC 来源 | 审计 |
|---|------|---------|------|
| 63 | scrubSensitiveContent (负面过滤) | Ph6 #24 | ✅ |
| 64 | KI Distill nil-safety (mock distiller) | Ph6 | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 65 | **Private/Team 双层 namespace** | P3 | Ph6 | +200 | 🟡 中 |
| 66 | **customInstructions** — 用户自定义压缩重点 | P3 | Ph6 | +100 | 🟢 低 |
| 67 | **记忆过期清理** — 超过 N 天的低权重记忆自动归档 | P2 | — | +80 | 🟢 低 |

---

### 📦 infrastructure/config — 配置

> 暂无来自 CC 对标的改动需求。HotReload 已有。

---

## 🌐 Layer 3: interfaces/ — 外部接入

### 📦 interfaces/server — HTTP/WS

#### ✅ 已完成

| # | 能力 | CC 来源 | 文件 | 审计 |
|---|------|---------|------|------|
| 68 | /doctor 5 项诊断 | Ph8 #39 | doctor.go [新] | ✅ |
| 69 | /cost token+USD 展示 | Ph8 | doctor.go | ✅ |
| 70 | /cost per-model input/output 分拆 | — | doctor.go | ✅ |

#### 📋 待做

| # | 能力 | 优先级 | CC 来源 | 估行 | 风险 |
|---|------|--------|---------|------|------|
| 71 | **Git 集成命令** — /commit /diff /review | P1 | Ph8 #38 | +500 | 🟡 中 |
| 72 | **更多 Slash commands** — CC 66 个 vs NGO 12 个 | P2 | Ph8 #32 | +300 | 🟢 低 |
| 73 | **Webhook 通知** — 长任务完成推送 | P2 | — | +100 | 🟢 低 |
| 74 | **/cost 历史查询** — 按 session_id 分别统计 | P2 | — | +80 | 🟢 低 |

---

## 🧪 Layer 4: testing/

#### ✅ 已完成

| # | 文件 | 审计 |
|---|------|------|
| 75 | p0a_core_test.go — TokenTracker 精度 | ✅ |
| 76 | hook_test.go — extractSubCommands 安全 | ✅ |
| 77 | evolution_test.go — KI + 过滤增强 | ✅ |
| 78 | completeness_test.go — DefaultMeta 全覆盖 | ✅ |

---

## 📊 统计总览

| 分类 | 已完成 | 待做 P1 | 待做 P2 | 待做 P3 | 总计 |
|------|--------|---------|---------|---------|------|
| **domain/service** | 17 | 5 | 2 | 0 | 24 |
| **domain/tool** | 2 | 0 | 1 | 1 | 4 |
| **infra/llm** | 2 | 0 | 2 | 2 | 6 |
| **infra/tool** | 7 | 1 | 2 | 0 | 10 |
| **infra/security** | 3 | 0 | 0 | 2 | 5 |
| **infra/prompt** | 3 | 2 | 1 | 2 | 8 |
| **infra/skill** | 2 | 0 | 1 | 1 | 4 |
| **infra/knowledge+memory** | 2 | 0 | 1 | 2 | 5 |
| **interfaces/server** | 3 | 1 | 3 | 0 | 7 |
| **testing** | 4 | 0 | 0 | 0 | 4 |
| **合计** | **45** | **9** | **13** | **10** | **77** |

---

## 🗺️ 执行节奏建议

### P1 执行批次 (按包内聚, 减少跨层)

| 批次 | 包 | 项目 | 估行 |
|------|-----|------|------|
| **A** | domain/service | run.go 拆分 + 前台/后台分流 + 工具重型压缩 | +260 |
| **B** | infrastructure/prompt | Coordinator Prompt + CC Code Style | +340 |
| **C** | domain/service | Session Memory + 4-Phase Pipeline | +800 |
| **D** | interfaces/server + infra/tool | Git 集成命令 + read_file ValidatePath | +507 |

### P2 执行批次

| 批次 | 包 | 项目 | 估行 |
|------|-----|------|------|
| **E** | infra/llm | Cache Break + 健康检查 | +280 |
| **F** | domain/service + tool | DreamTask + Worker Transcript + Git diff 压缩 | +500 |
| **G** | infra/tool + security | inode 检测 + 工具扩充 | +700 |
| **H** | interfaces/server | Slash 扩充 + Webhook + /cost 历史 | +480 |

> [!IMPORTANT]
> **原则**: 每批次改 ≤ 2 个包。同包改动一个 PR。不跨 3 层。

> [!TIP]
> **P1-A** 最先做 — run.go 拆分是后续所有改动的基础，降低文件膨胀风险。
