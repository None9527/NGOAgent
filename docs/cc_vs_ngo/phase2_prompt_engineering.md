# Phase 2：Prompt 工程层深度对标分析

> CC `prompts.ts + systemPromptSections.ts + claudemd.ts + context.ts + attachments.ts`
> vs NGO `engine.go + discovery.go + component.go + prompttext.go`

---

## 目录

- [1. System Prompt 架构概览](#1-system-prompt-架构概览)
  - [1.1 CC：Section-Array + Cache Boundary 模型](#11-ccsection-array--cache-boundary-模型)
  - [1.2 NGO：18-Section Priority Engine 模型](#12-ngo18-section-priority-engine-模型)
  - [1.3 架构哲学差异](#13-架构哲学差异)
- [2. System Prompt Section 逐项对比](#2-system-prompt-section-逐项对比)
  - [2.1 CC Section 清单](#21-cc-section-清单)
  - [2.2 NGO Section 清单](#22-ngo-section-清单)
  - [2.3 映射矩阵](#23-映射矩阵)
- [3. CLAUDE.md / User Rules 发现系统](#3-claudemd--user-rules-发现系统)
  - [3.1 CC 5 层 Memory Discovery](#31-cc-5-层-memory-discovery)
  - [3.2 NGO 3 层 Discovery](#32-ngo-3-层-discovery)
  - [3.3 差距评级](#33-差距评级)
- [4. Attachment 系统对比](#4-attachment-系统对比)
  - [4.1 CC 50+ 类型 Attachment 引擎](#41-cc-50-类型-attachment-引擎)
  - [4.2 NGO Ephemeral 注入机制](#42-ngo-ephemeral-注入机制)
  - [4.3 差距评级](#43-差距评级)
- [5. 缓存优化策略](#5-缓存优化策略)
- [6. Prompt 预算管理](#6-prompt-预算管理)
- [7. 上下文注入（Context Injection）](#7-上下文注入context-injection)
- [8. Ant-Only 内容分析](#8-ant-only-内容分析)
- [9. 差距总览矩阵](#9-差距总览矩阵)
- [10. 可移植设计建议](#10-可移植设计建议)

---

## 1. System Prompt 架构概览

### 1.1 CC：Section-Array + Cache Boundary 模型

CC 的 `getSystemPrompt()` 返回 `string[]`，每个元素是一个独立 section。关键创新在于 **Cache Boundary**：

```typescript
// prompts.ts:560-577
return [
  // --- Static content (cacheable, scope: 'global') ---
  getSimpleIntroSection(outputStyleConfig),        // Identity
  getSimpleSystemSection(),                         // System rules
  getSimpleDoingTasksSection(),                     // Task guidelines
  getActionsSection(),                              // Action safety
  getUsingYourToolsSection(enabledTools),            // Tool usage
  getSimpleToneAndStyleSection(),                   // Style
  getOutputEfficiencySection(),                     // Output control
  // === BOUNDARY MARKER ===
  ...(shouldUseGlobalCacheScope()
    ? [SYSTEM_PROMPT_DYNAMIC_BOUNDARY]
    : []),
  // --- Dynamic content (user/session-specific) ---
  ...resolvedDynamicSections,                       // Registry-managed
]
```

**Cache 分区**：
- **Boundary 之前**：所有用户共享的静态内容 → `cacheScope: 'global'`（跨组织复用）
- **Boundary 之后**：用户/会话特定的动态内容 → 不缓存

**Section 管理机制**（`systemPromptSections.ts`）：

| 类型 | 函数 | 缓存行为 |
|------|------|----------|
| `systemPromptSection()` | 普通 Section | Memoized — 计算一次，整个 session 缓存 |
| `DANGEROUS_uncachedSystemPromptSection()` | 易变 Section | 每 turn 重新计算，**会破坏 prompt cache** |

CC 明确标记只有 `mcp_instructions` 使用 `DANGEROUS_uncached`（因为 MCP 服务器可能中途连接/断开），并附带原因说明。

### 1.2 NGO：18-Section Priority Engine 模型

```go
// engine.go:131-155
func (e *Engine) buildSections(deps Deps) []Section {
    return []Section{
        // ═══ Head Peak (HIGH attention) ═══
        {Order: 1,  Name: "Identity",         Priority: 0},  // 不可裁剪
        {Order: 2,  Name: "CoreBehavior",      Priority: 0},
        {Order: 3,  Name: "OutputCapabilities", Priority: 1},
        {Order: 4,  Name: "Safety",             Priority: 0},
        {Order: 5,  Name: "UserRules",          Priority: 1},
        // ═══ Mid Valley ═══
        {Order: 7,  Name: "Tooling",            Priority: 0},
        {Order: 8,  Name: "Skills",             Priority: 1},
        {Order: 9,  Name: "ToolProtocol",        Priority: 0},
        {Order: 10, Name: "ToolCalling",         Priority: 0},
        {Order: 11, Name: "ProjectContext",      Priority: 2},
        {Order: 12, Name: "Variants",            Priority: 3},
        // ═══ Tail Peak (HIGH attention) ═══
        {Order: 13, Name: "ResponseFormat",      Priority: 0},
        {Order: 14, Name: "KnowledgeIndex",      Priority: 0},
        {Order: 15, Name: "SemanticMemory",      Priority: 2},
        {Order: 16, Name: "Runtime",             Priority: 1},
        {Order: 17, Name: "Focus",               Priority: 2},
        {Order: 18, Name: "Ephemeral",           Priority: 0},
    }
}
```

**核心创新**：
- **Head-Mid-Tail 注意力设计**：利用 LLM "U-shape" 注意力分布，将高优先级内容放在头尾
- **4 级 Priority 裁剪**（`prune()`）：根据 token budget 渐进式裁剪
- **Dynamic Priority**（`EffectivePriority(step)`）：KnowledgeIndex 随 step 增加而降优先级，Focus 随 step 增加而升优先级
- **CJK-aware 预算估算**：`estimateCharBudget()` 按 CJK/ASCII 比例混合计算

### 1.3 架构哲学差异

| 维度 | CC | NGO |
|------|-----|-----|
| **Prompt 结构** | `string[]`（扁平数组） | `[]Section`（带元数据的结构体数组） |
| **优先级系统** | 无（所有内容平等） | 4 级 Priority（0=required, 3=low） |
| **注意力优化** | 无显式设计 | Head-Mid-Tail U-shape 布局 |
| **缓存策略** | Boundary Marker + Blake2b Hash | 无 prompt cache |
| **裁剪机制** | 无（依赖上下文压缩） | 4 级 prune（50%→70%→85%→100%） |
| **动态调整** | 无 | EffectivePriority(step) |
| **Section 注册** | 工厂函数 + memoize | 硬编码 struct 数组 |
| **CJK 支持** | 无 | CJK-aware budget estimator |

**评价**：NGO 的 Prompt Engine 在 **注意力优化、预算管理、动态优先级** 三方面明显领先 CC。CC 的优势在于 **prompt cache 优化**（全局缓存分区减少 API 重复计算成本）。

---

## 2. System Prompt Section 逐项对比

### 2.1 CC Section 清单

CC `getSystemPrompt()` 在 `prompts.ts:560-577` 中组装的完整 section 列表：

**静态 Sections（Cache Boundary 之前）**：

| # | Section 名 | 函数 | 行数 | 内容概述 |
|---|-----------|------|------|----------|
| S1 | **Intro** | `getSimpleIntroSection()` | 4 | 身份声明 + CYBER_RISK_INSTRUCTION + URL 安全 |
| S2 | **System** | `getSimpleSystemSection()` | 6 | 输出格式 + 权限模式 + system-reminder 标签 + hooks + 自动压缩 |
| S3 | **DoingTasks** | `getSimpleDoingTasksSection()` | 20+ | 软件工程任务指导 + 代码风格 + 安全性 + 反模式告诫 |
| S4 | **Actions** | `getActionsSection()` | 15 | 可逆性 / 爆炸半径 / 破坏性操作确认 + 示例 |
| S5 | **UsingTools** | `getUsingYourToolsSection()` | 10 | 专用工具优先 + 并行调用 + Task 工具 |
| S6 | **ToneAndStyle** | `getSimpleToneAndStyleSection()` | 5 | Emoji 限制 + 文件引用格式 + GitHub 链接格式 |
| S7 | **OutputEfficiency** | `getOutputEfficiencySection()` | 12 | Ant: 流畅散文输出 / 3P: "Go straight to the point" |

**动态 Sections（Cache Boundary 之后）**：

| # | Section 名 | Registry | Cache | 概述 |
|---|-----------|----------|-------|------|
| D1 | **session_guidance** | `systemPromptSection` | ✅ | AgentTool 指导 + !command 提示 + SkillSearch |
| D2 | **memory** | `systemPromptSection` | ✅ | `loadMemoryPrompt()` — 自动记忆 |
| D3 | **ant_model_override** | `systemPromptSection` | ✅ | Ant-only 模型覆盖 |
| D4 | **env_info_simple** | `systemPromptSection` | ✅ | 工作目录 + Git + 平台 + 模型 + 知识截止 |
| D5 | **language** | `systemPromptSection` | ✅ | 语言偏好 |
| D6 | **output_style** | `systemPromptSection` | ✅ | 自定义输出风格 |
| D7 | **mcp_instructions** | `DANGEROUS_uncached` | ❌ | MCP 服务器指令（每 turn 重算） |
| D8 | **scratchpad** | `systemPromptSection` | ✅ | 临时文件目录指引 |
| D9 | **frc** | `systemPromptSection` | ✅ | Function Result Clearing |
| D10 | **summarize_tool_results** | `systemPromptSection` | ✅ | "记下重要信息" 提示 |
| D11 | **numeric_length_anchors** | `systemPromptSection` | ✅ | Ant-only: ≤25 words 工具间 / ≤100 words 末 |
| D12 | **token_budget** | `systemPromptSection` | ✅ | Token budget 持续工作指令 |
| D13 | **brief** | `systemPromptSection` | ✅ | Kairos Brief 辅助 |

### 2.2 NGO Section 清单

| Order | Name | Priority | 内容来源 | 概述 |
|-------|------|----------|----------|------|
| 1 | **Identity** | 0 | `prompttext.Identity` | 一句话身份声明 |
| 2 | **CoreBehavior** | 0 | `prompttext.CoreBehavior` | 搜索策略 + 核心规则 + KI 使用 + 附件处理 |
| 3 | **OutputCapabilities** | 1 | `prompttext.OutputCapabilities` | 前端渲染能力声明 |
| 4 | **Safety** | 0 | `prompttext.Safety` | 安全规则 + Security Layer Delegation |
| 5 | **UserRules** | 1 | `discovery.LoadUserRules()` | `<user_rules>` XML 封装 |
| 7 | **Tooling** | 0 | `buildTooling(descs)` | 工具列表摘要 + 使用注意 |
| 8 | **Skills** | 1 | `buildSkills(infos)` | Skill 列表（按 weight 分类） |
| 9 | **ToolProtocol** | 0 | `prompttext.ToolProtocol` | 必须遵守的工具使用协议 |
| 10 | **ToolCalling** | 0 | `prompttext.ToolCalling` | 并行/串行工具调用指导 |
| 11 | **ProjectContext** | 2 | `<project_context>` | 项目上下文 |
| 12 | **Variants** | 3 | `LoadVariants()` | 可选覆盖层 |
| 13 | **ResponseFormat** | 0 | `prompttext.ResponseFormat` | 回复规则："不以未来计划结尾" |
| 14 | **KnowledgeIndex** | 0 | `<knowledge_items>` | 知识库索引 |
| 15 | **SemanticMemory** | 2 | `<semantic_memory>` | 向量检索记忆 |
| 16 | **Runtime** | 1 | 运行时信息 | OS/时间/模型/工作区 |
| 17 | **Focus** | 2 | 焦点文件路径 | 当前编辑焦点 |
| 18 | **Ephemeral** | 0 | `<EPHEMERAL_MESSAGE>` | 临时指令注入 |

### 2.3 映射矩阵

| CC Section | NGO Equivalent | 覆盖度 | 备注 |
|------------|---------------|--------|------|
| S1 Intro | Identity(1) | ⚠️ | CC 含 CYBER_RISK_INSTRUCTION，NGO 无 |
| S2 System | Safety(4) 部分 | ⚠️ | CC 含 hooks + system-reminder 标签说明 |
| S3 DoingTasks | CoreBehavior(2) | ⚠️ | CC 20+ 子项，NGO 精简 6 项 |
| S4 Actions | Safety(4) 部分 | ❌ | CC 的可逆性/爆炸半径分析无 NGO 等价 |
| S5 UsingTools | Tooling(7) + ToolCalling(10) | ✅ | |
| S6 ToneAndStyle | **无** | ❌ | NGO 无 Emoji 限制等 |
| S7 OutputEfficiency | ResponseFormat(13) | ⚠️ | NGO 更简洁但缺少 Ant 的散文指导 |
| D1 session_guidance | **无** | ❌ | NGO 无 !command 提示 |
| D2 memory | SemanticMemory(15) | ⚠️ | CC 用 memdir，NGO 用向量检索 |
| D4 env_info | Runtime(16) | ✅ | 两者覆盖相同信息 |
| D5 language | **无** | ❌ | NGO 无语言偏好 section |
| D7 mcp_instructions | **无** | ❌ | NGO 的 MCP 指令无 prompt 注入机制 |
| D9 frc | **无** | ❌ | NGO 无 Function Result Clearing |
| D12 token_budget | **无** | ❌ | NGO 无 token budget 指令 |
| **无** | OutputCapabilities(3) | ✅ NGO独有 | CC 无前端渲染能力声明 |
| **无** | ToolProtocol(9) | ✅ NGO独有 | CC 无显式工具协议 |
| **无** | KnowledgeIndex(14) | ✅ NGO独有 | CC 无知识库索引注入 |
| **无** | Focus(17) | ✅ NGO独有 | CC 无焦点文件 section |
| **无** | Ephemeral(18) | ✅ NGO独有 | CC 用 attachment 替代 |

---

## 3. CLAUDE.md / User Rules 发现系统

### 3.1 CC 5 层 Memory Discovery

CC 的 `claudemd.ts` (1480 行) 实现了**企业级 5 层配置发现**：

```
Layer 1: Managed   — /etc/claude-code/CLAUDE.md (组织策略)
                   + /etc/claude-code/.claude/rules/*.md
Layer 2: User      — ~/.claude/CLAUDE.md (个人全局)
                   + ~/.claude/rules/*.md
Layer 3: Project   — CLAUDE.md (项目根) ← 从 CWD 向上遍历到根
                   + .claude/CLAUDE.md + .claude/rules/*.md
Layer 4: Local     — CLAUDE.local.md (私有, gitignored)
Layer 5: AutoMem   — MEMORY.md (自动记忆入口)
         TeamMem   — TEAM_MEMORY.md (团队共享记忆)
```

**关键特性**：
1. **@include 递归**：`@path`, `@./relative`, `@~/home`, `@/absolute` 支持引用其他文件（最大深度 5）
2. **Frontmatter paths**：`paths: ["src/**", "!test/**"]` 条件匹配（仅匹配的文件路径才注入该规则）
3. **HTML 注释剥离**：`<!-- comment -->` 在 markdown 块级别自动剥离（代码块内保留）
4. **symlink 安全**：`safeResolvePath()` + 循环路径检测 + EACCES 权限错误日志
5. **Git Worktree 去重**：检测嵌套 worktree，避免同一 CLAUDE.md 被加载两次
6. **排除模式**：`claudeMdExcludes` 支持 glob 排除（含 symlink 解析）
7. **Hook 触发**：`executeInstructionsLoadedHooks()` 在加载后执行用户 hook
8. **memoize 缓存**：整个发现过程 memoized，session 内只执行一次

### 3.2 NGO 3 层 Discovery

`discovery.go` (80 行) — 简洁有效：

```
Layer 1: Global    — ~/.ngoagent/user_rules.md
Layer 2: Project   — .ngoagent/user_rules.md  (工作区)
Layer 3: Overlay   — ~/.ngoagent/prompts/variants/*.md
```

**特点**：
- `user_rules.md` 用 `"---"` 分割符连接
- `LoadVariants()` 发现 `variants/` 目录下所有 `.md` 文件
- `LoadProjectContext()` 读取 `.ngoagent/context.md`

### 3.3 差距评级

| 维度 | CC | NGO | 评级 |
|------|-----|-----|------|
| 发现层数 | 5 层（Managed→User→Project→Local→AutoMem） | 3 层（Global→Project→Overlay） | ⚠️ |
| 向上遍历 | ✅ CWD → root 逐层发现 | ❌ 仅当前工作区 | ❌ |
| @include 递归 | ✅ 深度 5 | ❌ | ❌ |
| 条件匹配 (glob) | ✅ frontmatter paths | ❌ | ❌ |
| HTML 注释剥离 | ✅ | ❌ | ❌ |
| 排除模式 | ✅ claudeMdExcludes | ❌ | ❌ |
| Symlink 安全 | ✅ | ❌ | ⚠️ |
| Hook 触发 | ✅ instructionsLoaded | ❌ | ❌ |
| PromptComponent 系统 | ❌ | ✅ `RequiresClause` 条件注入 | ✅ NGO独有 |
| Variants overlay | ❌ | ✅ | ✅ NGO独有 |

---

## 4. Attachment 系统对比

### 4.1 CC 50+ 类型 Attachment 引擎

`attachments.ts` (3998 行) 是 CC 非常核心的上下文注入系统。Attachment 类型高达 **50+**：

**大类分组**：

| 类别 | 类型计数 | 代表性类型 |
|------|----------|-----------|
| 文件引用 | 7 | `file`, `compact_file_reference`, `pdf_reference`, `already_read_file`, `edited_text_file`, `edited_image_file`, `directory` |
| IDE 集成 | 2 | `selected_lines_in_ide`, `opened_file_in_ide` |
| 任务管理 | 5 | `todo_reminder`, `task_reminder`, `task_status`, `verify_plan_reminder`, `max_turns_reached` |
| Memory | 4 | `nested_memory`, `relevant_memories`, `current_session_memory`, `compaction_reminder` |
| MCP/Skill | 6 | `mcp_resource`, `dynamic_skill`, `skill_listing`, `skill_discovery`, `invoked_skills`, `deferred_tools_delta` |
| 计划模式 | 4 | `plan_mode`, `plan_mode_reentry`, `plan_mode_exit`, `plan_file_reference` |
| 自动模式 | 2 | `auto_mode`, `auto_mode_exit` |
| Hook | 8 | `hook_cancelled`, `hook_blocking_error`, `hook_success`, `hook_non_blocking_error`, `hook_error_during_execution`, `hook_stopped_continuation`, `hook_additional_context`, `hook_permission_decision` |
| 用户输入 | 3 | `queued_command`, `output_style`, `agent_mention` |
| Telemetry | 4 | `token_usage`, `budget_usd`, `output_token_usage`, `diagnostics` |
| 安全 | 1 | `command_permissions` |
| 团队 | 4 | `teammate_mailbox`, `team_context`, `teammate_shutdown_batch`, `agent_listing_delta` |
| 系统 | 5 | `critical_system_reminder`, `context_efficiency`, `date_change`, `ultrathink_effort`, `mcp_instructions_delta` |
| 其他 | 3 | `structured_output`, `bagel_console`, `companion_intro` |

**关键设计**：
- `getAttachments()` 在**每个 turn** 被调用，产出与当前上下文相关的 attachment
- 所有 attachment 被序列化为 `user` 类型消息中的 `<system-reminder>` 标签注入
- `getAttachmentMessages()` 是 `query.ts` 循环中在工具执行后调用的——这意味着 attachment 可以包含工具执行后的新信息

### 4.2 NGO Ephemeral 注入机制

NGO 使用 `Ephemeral` section + `InjectEphemeral()` 方法：

```go
// run.go
func (a *AgentLoop) InjectEphemeral(msg string) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.ephemeralMessages = append(a.ephemeralMessages, msg)
}
```

**Ephemeral 类型**（`prompttext.go`）：

| 常量名 | 触发场景 | 概述 |
|--------|----------|------|
| `EphPlanningMode` | 进入计划模式 | 3 阶段工作流程 |
| `EphPlanningNoPlanReminder` | 计划模式但无 plan | 提示创建 plan |
| `EphPlanModifiedReminder` | plan 被修改 | 提示 notify_user |
| `EphEnteringPlanningMode` | 切换到计划 | 状态转移指导 |
| `EphExitingPlanningMode` | 退出计划 | 执行→验证流程 |
| `EphActiveTaskReminder` | 有活跃任务 | 任务名+状态 |
| `EphArtifactReminder` | 有产出物 | artifact 列表 |
| `EphContextStatus` | 每 turn | 上下文使用率 |
| `EphCompactionNotice` | compact 后 | 提示可能需要重读 |
| `EphAgenticSelfReview` | 无人模式 | 自动审批 |
| `EphEvoMode` | Evo 模式 | 自我进化流程 |
| `EphEditValidation` | 编辑失败 | 错误信息 |
| `EphSecurityNotice` | 工具被拒 | 安全策略通知 |
| `EphSkillInstruction` | 技能触发 | 技能指令注入 |
| `EphSubAgentContext` | 子 agent | 执行上下文 |
| `EphSubAgentResults` | 子 agent 完成 | 结果注入 |

### 4.3 差距评级

| 维度 | CC | NGO | 评级 |
|------|-----|-----|------|
| 注入类型数 | 50+ 种结构化 Attachment | 16 种模板化 Ephemeral | ⚠️ |
| IDE 集成 | ✅ 选中代码 + 打开文件 | ❌ | ❌ |
| 文件变更检测 | ✅ edited_text_file + diff snippet | ❌ | ❌ |
| Todo/Task 提醒 | ✅ 定期注入 | ⚠️ EphActiveTaskReminder | ⚠️ |
| Memory 表面化 | ✅ relevant_memories + session_memory | ⚠️ SemanticMemory section | ⚠️ |
| 计划模式 | ✅ 4 种 plan attachment | ✅ 6 种 Eph* 替代 | ✅ |
| Hook 反馈 | ✅ 8 种 hook attachment | ⚠️ EphSecurityNotice | ⚠️ |
| Token 使用率 | ✅ token_usage + budget_usd | ✅ EphContextStatus | ✅ |
| Evo 自进化 | ❌ | ✅ EphEvoMode + EphEvoEvalPrompt | ✅ NGO独有 |
| 子 Agent 结果注入 | ❌ (inline) | ✅ EphSubAgentResults | ✅ NGO独有 |

---

## 5. 缓存优化策略

### CC Prompt Cache 分区

CC 使用 **Blake2b prefixed hash** 实现 prompt cache：

```
System Prompt Array
  ├── [Global prefix]  ← 所有用户共享 → cacheScope: 'global'
  │      ↓ Blake2b hash → API cache prefix
  ├── SYSTEM_PROMPT_DYNAMIC_BOUNDARY  ← 分界标记
  └── [Dynamic suffix]  ← 用户/session 特定 → 不缓存
```

**设计要求**：
- **Boundary 前的内容绝不能包含 session 特定值**（否则会产生 2^N 种 hash 变体，缓存命中率归零）
- `isForkSubagentEnabled()`、`getIsNonInteractiveSession()` 等 session 条件必须放到 Boundary 后
- `DANGEROUS_uncachedSystemPromptSection` 需要明确的 `_reason` 参数

### NGO 的缺失

NGO 当前无 prompt cache 机制。每次 `Assemble()` 都重新构建完整 prompt。

**差距评级**：❌ — 这意味着 NGO 每次 API 调用都需要重新处理完整的 system prompt token，增加延迟和成本。

---

## 6. Prompt 预算管理

### CC — 无显式预算

CC 的 system prompt 不进行裁剪。它依赖**下游的上下文压缩系统**（autocompact / microcompact / snip）来管理总体 token 预算。

### NGO — 4 级渐进裁剪

```go
// engine.go:261-326
func (e *Engine) prune(sections []Section, budget int, step int) (string, int) {
    pct := float64(totalChars) / float64(charBudget) * 100

    // Level 0: <50% → 无裁剪
    // Level 1: 50-70% → Priority≥2 截断 50%
    // Level 2: 70-85% → Priority≥2 丢弃, Priority=1 截断 1000 字
    // Level 3: >85% → 仅 Priority=0 + UserRules 保留
}
```

**独特能力**：
- `EffectivePriority(step)` — KnowledgeIndex 在 step>5 后降级，Focus 在 step>10 后升级
- `estimateCharBudget()` — CJK 比例检测自适应预算
- UserRules **永不丢弃**（显式豁免）

**差距评级**：✅ NGO独有 — CC 完全没有 prompt 级预算管理。

---

## 7. 上下文注入（Context Injection）

### CC — Git Status + Date Snapshot

`context.ts` 提供两个 memoized 上下文源：

```typescript
getSystemContext():
  - gitStatus: branch + mainBranch + userName + status(2K限) + log(5条)
  - cacheBreaker: ant-only 调试注入

getUserContext():
  - claudeMd: 合并后的 CLAUDE.md 内容
  - currentDate: "Today's date is YYYY-MM-DD"
```

### NGO — Runtime + Focus + Ephemeral

NGO 通过 `Deps` 结构注入：

```go
type Deps struct {
    Runtime        string   // OS/time/model/workspace
    FocusFile      string   // 当前编辑焦点
    Ephemeral      []string // 临时指令
    ConvSummary    string   // KI 索引
    MemoryContent  string   // 向量记忆
    ProjectContext string   // 项目上下文
    CurrentStep    int      // 当前步骤（影响 EffectivePriority）
}
```

**差距对比**：

| 上下文项 | CC | NGO |
|----------|-----|-----|
| Git Status | ✅ 详细快照 | ⚠️ 需要工具调用获取 |
| 日期 | ✅ memoized | ✅ Runtime 中 |
| 知识库索引 | ❌ | ✅ KnowledgeIndex section |
| 向量记忆 | ❌ (relevant_memories attachment) | ✅ SemanticMemory section |
| Focus 文件 | ❌ | ✅ Focus section |
| 当前步骤 | ❌ | ✅ CurrentStep |

---

## 8. Ant-Only 内容分析

CC 的 `prompts.ts` 包含大量 `process.env.USER_TYPE === 'ant'` 条件内容。这些是 **Anthropic 内部员工专用的增强指导**，对外部构建会被 DCE（Dead Code Elimination）移除：

| 区域 | Ant-Only 内容 | 价值评估 |
|------|--------------|----------|
| **Code Style** | "Default to writing no comments" + "Don't reference current task in comments" | ✅ 高 — 减少废话注释 |
| **Assertiveness** | "If you notice a misconception or adjacent bug, say so" | ✅ 高 — 主动协作 |
| **Thoroughness** | "Before reporting complete, verify it works" | ✅ 高 — 避免虚假完成 |
| **False Claims** | "Report outcomes faithfully" + "Never claim all pass when output shows failures" | ✅ 高 — 诚实性 |
| **Output comm.** | 长段散文风格指导（12 行） | ⚠️ 中 — 风格偏好 |
| **Length anchors** | "≤25 words between tools, ≤100 words final" | ⚠️ 中 — 效率提示 |
| **Bug reporting** | /issue + /share 推荐 | ❌ 低 — 内部流程 |

**可移植建议**：S3 DoingTasks 中的 "Code Style"、"Assertiveness"、"Thoroughness"、"False Claims" 四项应被 NGO 的 CoreBehavior 吸收。

---

## 9. 差距总览矩阵

| 能力项 | CC | NGO | 差距级别 | 移植难度 |
|--------|-----|-----|----------|----------|
| Head-Mid-Tail 注意力布局 | ❌ | ✅ | ✅ NGO优 | - |
| 4 级 Priority 裁剪 | ❌ | ✅ | ✅ NGO优 | - |
| Dynamic Priority | ❌ | ✅ | ✅ NGO优 | - |
| CJK-aware 预算 | ❌ | ✅ | ✅ NGO优 | - |
| PromptComponent 条件注入 | ❌ | ✅ RequiresClause | ✅ NGO优 | - |
| KnowledgeIndex 注入 | ❌ | ✅ | ✅ NGO优 | - |
| SemanticMemory section | ❌ | ✅ | ✅ NGO优 | - |
| Focus 文件 | ❌ | ✅ | ✅ NGO优 | - |
| Evo 自进化 | ❌ | ✅ | ✅ NGO独有 | - |
| OutputCapabilities | ❌ | ✅ | ✅ NGO独有 | - |
| Prompt Cache 分区 | ✅ Blake2b + Boundary | ❌ | ❌ | 高 |
| 50+ Attachment 类型 | ✅ | ❌ 16 种 Ephemeral | ⚠️ | 中 |
| @include 递归 | ✅ | ❌ | ❌ | 中 |
| 条件 Rules (glob matching) | ✅ frontmatter paths | ❌ | ❌ | 中 |
| 5 层 Memory Discovery | ✅ | ⚠️ 3 层 | ⚠️ | 低 |
| Git Status 快照 | ✅ | ❌ | ⚠️ | 低 |
| IDE 集成注入 | ✅ | ❌ | ❌ | 中 |
| 文件变更检测 + Diff | ✅ | ❌ | ❌ | 中 |
| CYBER_RISK_INSTRUCTION | ✅ | ❌ | ⚠️ | 低 |
| Function Result Clearing | ✅ | ❌ | ⚠️ | 低 |
| 散文/简洁输出控制 | ✅ Ant 模式 | ⚠️ 基本 | ⚠️ | 低 |
| 语言偏好 | ✅ | ❌ | ⚠️ | 低 |

---

## 10. 可移植设计建议

### P0 — 即刻收益

1. **吸收 CC 的 Code Style 规则** — 将 "Default to writing no comments"、"Verify before reporting complete"、"Report outcomes faithfully" 加入 `CoreBehavior`
2. **Actions Section** — 在 `Safety` 中增加可逆性/爆炸半径分析（CC 的 `getActionsSection()` 内容几乎可直接翻译）
3. **Git Status 快照** — 在 `Runtime` section 中增加 git branch/status/recent commits 信息

### P1 — 架构增强

4. **向上遍历 Discovery** — 扩展 `discovery.go` 支持从 CWD 向上遍历发现 `.ngoagent/` 目录
5. **@include 递归** — 在 `user_rules.md` 中支持 `@./path` 语法加载外部文件
6. **Attachment 引擎** — 将部分 Ephemeral 升级为结构化 Attachment 类型，支持 IDE 集成和文件变更检测

### P2 — 长期演进

7. **Prompt Cache** — 实现 System Prompt 的 Static/Dynamic 分区 + Hash 缓存
8. **条件 Rules** — 支持 frontmatter `paths:` glob 匹配，仅在匹配文件路径时注入规则
9. **MCP Instructions Delta** — 实现增量式 MCP 指令注入（避免重算破坏缓存）
