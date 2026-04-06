# Phase 3：工具系统层深度对标分析

> CC `Tool.ts + tools/*` (42 个工具目录, 580+ KB)
> vs NGO `tool/*.go` (27 个文件, ~120 KB)

---

## 目录

- [1. 工具接口设计](#1-工具接口设计)
  - [1.1 CC Tool Interface](#11-cc-tool-interface)
  - [1.2 NGO Tool Interface](#12-ngo-tool-interface)
  - [1.3 接口能力差异矩阵](#13-接口能力差异矩阵)
- [2. 工具注册与发现](#2-工具注册与发现)
- [3. 工具清单对比](#3-工具清单对比)
- [4. 权限系统](#4-权限系统)
- [5. 核心工具逐项对比](#5-核心工具逐项对比)
  - [5.1 Shell 执行 (BashTool vs run_command)](#51-shell-执行)
  - [5.2 文件编辑 (FileEditTool vs edit_file)](#52-文件编辑)
  - [5.3 文件读取](#53-文件读取)
  - [5.4 Agent / Sub-agent](#54-agent--sub-agent)
- [6. 工具协议层 (Signal / Protocol)](#6-工具协议层)
- [7. 独有工具分析](#7-独有工具分析)
- [8. 差距总览矩阵](#8-差距总览矩阵)
- [9. 可移植设计建议](#9-可移植设计建议)

---

## 1. 工具接口设计

### 1.1 CC Tool Interface

CC 的 `Tool.ts` (793 行) 定义了极其丰富的工具元数据接口：

```typescript
type Tool<Input, Output, P> = {
  name: string
  aliases?: string[]              // 向后兼容别名
  searchHint?: string             // ToolSearch 关键词匹配（3-10 词）
  inputSchema: Input              // Zod schema（强类型验证）
  inputJSONSchema?: JSONSchema    // MCP 工具直接提供 JSON Schema
  outputSchema?: z.ZodType        // 可选输出 schema
  maxResultSizeChars: number      // 结果超限阈值 → 持久化到磁盘
  strict?: boolean                // 严格模式（精确参数匹配）
  shouldDefer?: boolean           // 延迟加载（需 ToolSearch 才能调用）
  alwaysLoad?: boolean            // 永不延迟

  // ─── 核心方法 ───
  call(args, context, canUseTool, parentMessage, onProgress): Promise<ToolResult<Output>>
  description(input, options): Promise<string>  // 动态描述
  prompt(options): Promise<string>              // 工具专属 prompt

  // ─── 权限 ───
  validateInput?(input, context): Promise<ValidationResult>
  checkPermissions(input, context): Promise<PermissionResult>
  preparePermissionMatcher?(input): Promise<(pattern: string) => boolean>

  // ─── 行为分类 ───
  isConcurrencySafe(input): boolean    // 并发安全？
  isReadOnly(input): boolean           // 只读？
  isDestructive?(input): boolean       // 破坏性？
  isSearchOrReadCommand?(input): { isSearch, isRead, isList }
  isOpenWorld?(input): boolean
  requiresUserInteraction?(): boolean
  interruptBehavior?(): 'cancel' | 'block'
  toAutoClassifierInput(input): unknown  // 安全分类器输入

  // ─── UI 渲染 ───
  userFacingName(input): string
  getToolUseSummary?(input): string | null
  getActivityDescription?(input): string | null
  renderToolUseMessage, renderToolResultMessage, renderToolUseProgressMessage  // React JSX
  extractSearchText?(out): string       // 搜索索引

  // ─── API 映射 ───
  mapToolResultToToolResultBlockParam(content, toolUseID): ToolResultBlockParam
  backfillObservableInput?(input): void
  inputsEquivalent?(a, b): boolean
}
```

**关键设计**：
- `buildTool()` 工厂函数提供 safe defaults（`isEnabled: () => true`, `isConcurrencySafe: () => false` 等）
- `ToolUseContext` (293 行 struct) 包含完整的会话状态、权限、文件缓存、MCP 连接等
- 每个工具目录包含独立的 `prompt.ts` (工具专属的 system prompt 片段)
- React JSX 渲染方法使工具可直接在 UI 中自定义展示

### 1.2 NGO Tool Interface

```go
// domain/tool/tool.go (33 行)
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any
    Execute(ctx context.Context, args map[string]any) (ToolResult, error)
}

// domain/tool/protocol.go (236 行)
type ToolResult struct {
    Output  string
    Signal  Signal          // 6 种协议信号
    Payload map[string]any  // 信号携带数据
}
```

**关键设计**：
- 4 方法最小接口 — `Name()`, `Description()`, `Schema()`, `Execute()`
- 信号系统替代 UI 回调 — `Signal` 枚举 + `Dispatch()` 路由
- `BoundaryState` 共享指针 — 无锁状态追踪
- `kind` 枚举分类（File, Search, Exec, Network, Know, Agent）但接口不强制

### 1.3 接口能力差异矩阵

| 能力 | CC | NGO | 差距 |
|------|-----|-----|------|
| **输入验证** | Zod schema（编译期类型安全） | `map[string]any`（运行时手动） | ❌ |
| **输出 Schema** | ✅ Zod 输出类型 | ❌ 无 | ⚠️ |
| **权限接口** | `validateInput` + `checkPermissions` + `preparePermissionMatcher` | ❌ 通过 BehaviorGuard 外部拦截 | ⚠️ |
| **并发安全标记** | `isConcurrencySafe(input)` | ❌ 无 | ❌ |
| **只读标记** | `isReadOnly(input)` | ❌ 无 | ❌ |
| **破坏性标记** | `isDestructive(input)` | ❌ 无 | ❌ |
| **中断行为** | `interruptBehavior(): 'cancel' | 'block'` | ❌ 无 | ⚠️ |
| **UI 渲染** | React JSX 渲染方法 | ❌ API-only | - |
| **工具搜索** | `shouldDefer` + `alwaysLoad` + `searchHint` | ❌ 全量加载 | ❌ |
| **动态描述** | `description(input, options)` | 静态 `Description()` | ⚠️ |
| **工具专属 Prompt** | `prompt(options)` per-tool | 全局 `prompttext.go` | ⚠️ |
| **协议信号** | 无（通过 newMessages + contextModifier） | ✅ 6-Signal Dispatch | ✅ NGO优 |
| **别名** | `aliases?: string[]` | ❌ | ⚠️ |
| **结果大小控制** | `maxResultSizeChars` → 磁盘持久化 | ❌ 无限制 | ❌ |
| **安全分类器** | `toAutoClassifierInput(input)` | ❌ | ❌ |
| **路径解析** | 工具内部 `expandPath()` | ✅ Registry 统一 `resolveToolPaths()` | ✅ NGO优 |
| **启禁控制** | 无 Registry 动态启禁 | ✅ `Enable()/Disable()` | ✅ NGO优 |
| **子 Agent 隔离** | 无 Registry clone | ✅ `CloneWithDisabled()` | ✅ NGO优 |

---

## 2. 工具注册与发现

### CC — 手动组装 + 动态加载

CC 没有 Registry 模块。工具列表在 `getTools()` 函数中手动组装，通过 feature flag 控制可用性：

```typescript
// 伪代码 — 遍布 commands.ts 和 queryEngine
const tools: Tool[] = [
  BashTool, FileReadTool, FileEditTool, FileWriteTool,
  GlobTool, GrepTool, WebSearchTool, WebFetchTool,
  ...(feature('AGENT_TOOL') ? [AgentTool] : []),
  ...(feature('SKILL_TOOL') ? [SkillTool] : []),
  ...getMcpTools(),  // 动态 MCP 工具
]
```

**ToolSearch 系统** (`ToolSearchTool/`)：
- 工具可标记 `shouldDefer: true` → API 调用时设置 `defer_loading: true`
- 模型找不到工具时可调用 `ToolSearch` → 通过 `searchHint` 关键词匹配启用所需工具
- `alwaysLoad: true` 阻止延迟加载

### NGO — Registry + 动态启禁

```go
// registry.go (232 行)
r := NewRegistry()
r.Register(NewRunCommandTool(sandbox))
r.Register(NewEditFileTool())
r.Register(NewReadFileTool())
// ...

// 子 agent 隔离
subRegistry := r.CloneWithDisabled([]string{"spawn_agent", "task_plan"})

// 运行时工具热插拔
r.Enable("forge")
r.Disable("evo")
```

**差距分析**：
- CC 的 ToolSearch 是一个**显著的优化** — 200 KB 上下文窗口中，40+ 工具的 schema 占据大量 token。延迟加载仅在需要时注入 schema。
- NGO 的 Registry 提供了 CC 缺少的**动态管理能力**——启禁、克隆、统一路径解析。

---

## 3. 工具清单对比

### CC 工具 (42 目录)

| 分类 | 工具名 | 代码量 |
|------|--------|--------|
| **Shell** | BashTool (1144+行), PowerShellTool | 636KB |
| **文件** | FileReadTool, FileEditTool, FileWriteTool, NotebookEditTool | 192KB |
| **搜索** | GlobTool, GrepTool, ToolSearchTool | 52KB |
| **网络** | WebSearchTool, WebFetchTool | 56KB |
| **Agent** | AgentTool (580KB!), SendMessageTool, TeamCreateTool, TeamDeleteTool | 680KB+ |
| **任务** | TaskCreateTool, TaskGetTool, TaskListTool, TaskUpdateTool, TaskStopTool, TaskOutputTool, TodoWriteTool | ~200KB |
| **计划** | EnterPlanModeTool, ExitPlanModeTool | ~30KB |
| **MCP** | MCPTool, McpAuthTool, ListMcpResourcesTool, ReadMcpResourceTool | 80KB |
| **IDE** | LSPTool | 96KB |
| **其他** | ConfigTool, REPLTool, SkillTool, SleepTool, BriefTool, SyntheticOutputTool, ScheduleCronTool, RemoteTriggerTool, EnterWorktreeTool, ExitWorktreeTool | ~200KB |

### NGO 工具 (27 文件)

| 分类 | 文件 | 代码量 |
|------|------|--------|
| **Shell** | run_command.go, command_status.go | 7.2KB |
| **文件** | read_file.go, write_file.go, edit_file.go, edit_fuzzy.go, undo_edit.go, filestate.go | 33KB |
| **搜索** | glob.go, grep_search.go | 12KB |
| **网络** | web_tools.go (web_search + web_fetch) | 7.3KB |
| **Agent** | agent_tools.go (spawn_agent + evo) | 10.4KB |
| **记忆** | knowledge_tools.go (save_memory + update_project_context + recall), recall_tool.go | 16.2KB |
| **任务** | task_boundary.go, brain_artifact.go (task_plan) | 10.1KB |
| **通知** | notify_user.go, send_message.go | 5.3KB |
| **Cron** | manage_cron.go | 4.2KB |
| **MCP** | mcp_adapter.go | 4KB |
| **Media** | view_media_tool.go, resize_image.go | 7.6KB |
| **Skill** | script_tool.go | 2.7KB |

### 映射矩阵

| 能力 | CC 工具 | NGO 工具 | 覆盖 |
|------|---------|---------|------|
| Shell 执行 | BashTool (160KB!) | run_command (5KB) | ⚠️ |
| 命令状态 | (BashTool 内建) | command_status (2.2KB) | ✅ |
| 文件读取 | FileReadTool | read_file | ✅ |
| 文件写入 | FileWriteTool | write_file | ✅ |
| 文件编辑 | FileEditTool (20KB) | edit_file (7.7KB) + edit_fuzzy (9.8KB) | ✅ |
| Glob 搜索 | GlobTool | glob | ✅ |
| Grep 搜索 | GrepTool | grep_search | ✅ |
| Web 搜索 | WebSearchTool | web_search (合并) | ✅ |
| Web 获取 | WebFetchTool | web_fetch (合并) | ✅ |
| 子 Agent | AgentTool (580KB!) | spawn_agent | ⚠️ |
| MCP 工具 | MCPTool + Auth + List + Read | mcp_adapter | ⚠️ |
| 任务管理 | 5 个独立工具 | task_boundary + brain_artifact | ✅ |
| 计划模式 | EnterPlan + ExitPlan | (通过 Ephemeral 管理) | ✅ |
| Todo | TodoWriteTool | (合并到 brain_artifact) | ✅ |
| Evo | ❌ | evo (agent_tools.go) | ✅ NGO独有 |
| Undo | ❌ | undo_edit | ✅ NGO独有 |
| 文件状态 | readFileState (内嵌) | filestate | ✅ NGO独有 |
| Media 查看 | ❌ | view_media_tool | ✅ NGO独有 |
| 记忆保存 | ❌ (MEMORY.md) | save_memory | ✅ NGO独有 |
| 项目上下文 | ❌ | update_project_context | ✅ NGO独有 |
| Recall | ❌ | recall_tool | ✅ NGO独有 |
| Cron 管理 | ScheduleCronTool | manage_cron | ✅ |
| LSP | LSPTool | ❌ | ❌ |
| REPL | REPLTool | ❌ | ❌ |
| Notebook | NotebookEditTool | ❌ | ❌ |
| PowerShell | PowerShellTool | ❌ | ⚠️ |
| Config | ConfigTool | ❌ | ⚠️ |
| Skill | SkillTool | script_tool | ⚠️ |
| Sleep | SleepTool | ❌ | ⚠️ |
| Brief | BriefTool | ❌ | ❌ |
| ToolSearch | ToolSearchTool | ❌ | ❌ |
| Worktree | Enter/ExitWorktreeTool | ❌ | ❌ |
| 团队 | TeamCreate/Delete | ❌ | ❌ |

---

## 4. 权限系统

### CC — 3 层权限 + 安全分类器

CC 的 BashTool 权限系统极其复杂 (bashPermissions.ts: 98KB, bashSecurity.ts: 102KB):

```
Layer 1: validateInput()    — 语法检查、sleep 模式检测
Layer 2: checkPermissions() — bashToolHasPermission():
  ├── readOnlyValidation    — 68KB! 只读命令检测
  ├── sedValidation         — 21KB sed 编辑安全
  ├── pathValidation        — 43KB 路径沙盒检查
  ├── destructiveWarning    — 破坏性命令警告
  └── modeValidation        — 模式特定规则
Layer 3: SecurityAST        — bash AST 解析 + 分类
```

**三种权限模式**：
- `default` — 需要用户确认
- `auto` — 自动分类器判断安全性
- `bypass` — 全部允许（dangerously）

### NGO — BehaviorGuard + 信任链

NGO 的权限管理不在工具层，而在 Agent 循环层的 `BehaviorGuard`：

```go
// run.go — Agent 循环中
result := behaviorGuard.Check(toolName, args)
switch result.Action {
case "allow":  // 执行
case "ask":    // 通知用户确认
case "deny":   // 直接拒绝
}
```

**特点**：
- 集中式策略而非分散到每个工具
- 规则可在 YAML 配置中声明
- Security Layer Delegation：工具总是尝试执行，安全层决定允许/拒绝

**差距**：CC 的逐工具权限粒度远高于 NGO 的集中式守卫，但 NGO 的模式更清晰、可维护。

---

## 5. 核心工具逐项对比

### 5.1 Shell 执行

**CC BashTool** (160KB, 1144+ 行) — 工业级 Shell 工具：

| 特性 | 实现 |
|------|------|
| AST 解析 | `parseForSecurity()` — bash 语法树安全分析 |
| 命令分类 | 搜索/读取/列表/静默命令集合（UI 折叠） |
| sleep 检测 | `detectBlockedSleepPattern()` — 阻止 polling, 建议 Monitor |
| 进度报告 | AsyncGenerator `onProgress` 流式进度 |
| Sed 拦截 | `parseSedEditCommand()` → 预览 + `applySedEdit()` 直写 |
| 沙盒 | `SandboxManager` — 可选沙盒执行 |
| 输出管理 | `EndTruncatingAccumulator` + 磁盘持久化（>30K字符） |
| Claude Code Hints | `<claude-code-hint />` 零 token 侧信道解析 |
| 后台任务 | `LocalShellTask` 完整生命周期管理 |
| 超时 | 可配 timeout + 自动后台化 |
| CWD 管理 | 命令后验证 CWD 在项目内，自动重置 |

**NGO run_command** (150 行, 5KB)：

| 特性 | 实现 |
|------|------|
| 执行模式 | 同步 / 后台 / Hybrid（等 N ms 后自动后台） |
| 沙盒 | `sandbox.Manager` — 统一沙盒层 |
| 后台 | `sandbox.RunBackground()` + `GetStatus()` |
| 超时 | `timeout_ms` 参数 |
| 输出格式 | stdout + stderr + exit code |

**差距分析**：
- CC 的 BashTool 是其**最复杂的工具**（160KB），安全性极高但维护成本巨大
- NGO 的 `run_command` 精简有效，安全性委托给 BehaviorGuard
- 关键缺失：AST 安全分析、流式进度、输出大小控制、sed 拦截

### 5.2 文件编辑

**CC FileEditTool** (20KB)：
- Zod 强类型输入
- `old_string` + `new_string` 替换模型
- 文件状态缓存（`readFileState`）+ 过时内容检测
- VS Code 通知 (`notifyVscodeFileUpdated`)
- 文件历史跟踪 (`fileHistoryTrackEdit`)
- 行尾风格检测/保持 (`detectLineEndings`)
- 编码检测/保持 (`detectFileEncoding`)

**NGO edit_file** (206 行) + **edit_fuzzy** (303 行)：

| 特性 | CC | NGO | 差距 |
|------|-----|-----|------|
| **输入验证** | Zod schema | 手动解析 | ⚠️ |
| **文件状态追踪** | `readFileState` 缓存 | ✅ `globalFileState` | ✅ |
| **过时检测** | ✅ | ✅ `HasChanged()` — Error code 7 | ✅ |
| **必须先读检测** | ❌ | ✅ `WasRead()` — Error code 6 | ✅ NGO优 |
| **工作区限制** | 无显式检查 | ✅ `WorkDir` 边界 — Error code 5 | ✅ NGO优 |
| **Lazy 注释检测** | ❌ | ✅ `lazyPatterns` — Error code 10 | ✅ NGO独有 |
| **模糊匹配** | ❌ | ✅ 3级级联: L1 Unicode → L2 LineTrim → L3 BlockAnchor | ✅ NGO独有 |
| **相似建议** | ❌ | ✅ `findSimilarLines()` — Aider 风格 | ✅ NGO独有 |
| **行尾保持** | ✅ | ✅ 归一化 `\r\n` | ✅ |
| **编码保持** | ✅ | ❌ | ❌ |
| **文件历史** | ✅ `fileHistory` | ✅ `FileHistory.TrackEdit()` | ✅ |
| **VS Code 通知** | ✅ | ❌ | ❌ |
| **Error Code 体系** | 无（文字描述） | ✅ Code 1-10 结构化 | ✅ NGO优 |

**NGO edit_fuzzy.go 亮点**：
- **L1 Unicode 归一化** — 映射 30+ 种 Unicode 变体到 ASCII（解决 GPT/Claude 生成 curly quotes 问题）
- **L2 Line-Trim Match** — 3 pass 渐进放松（exact → trimEnd → normalize+trimEnd）
- **L3 Block Anchor** — 首行+末行锚定（≥3行块匹配），类似 Cline
- **Lazy Comment 检测** — 10+ 种 "// ... rest" 模式阻断（防止 LLM 偷懒省略代码）

### 5.3 文件读取

两者逻辑接近：读取文件内容、行号标注、大小限制。CC 额外支持：
- 图片读取（作为 multimodal 内容）
- PDF 引用
- Token 预算控制

### 5.4 Agent / Sub-agent

**CC AgentTool** (580KB!) — 工业级多 Agent 系统：
- 完整的 Agent 定义文件 (`loadAgentsDir.ts`)
- 团队协作 (`TeamCreate`, `TeamDelete`, `SendMessage`)
- 任务分配 (`TaskCreate/Update/Stop`)
- 并发控制 + mailbox 系统
- Git worktree 隔离

**NGO spawn_agent** (96 行)：
- 简洁的异步 spawn → barrier 等待 → 结果注入
- SpawnFunc 注入模式（延迟绑定）
- 自动注入工作目录
- SSE 进度推送

**差距**：CC 的 Agent 系统是一个**完整的多 Agent 协作框架**，NGO 仅实现了 spawn/barrier 基础。

---

## 6. 工具协议层

### CC — 内联信号

CC 工具通过 `ToolResult` 的 `newMessages` 和 `contextModifier` 传递副作用：

```typescript
type ToolResult<T> = {
  data: T
  newMessages?: (UserMessage | AssistantMessage | SystemMessage)[]
  contextModifier?: (context: ToolUseContext) => ToolUseContext
  mcpMeta?: { _meta?, structuredContent? }
}
```

副作用分散在各工具内部处理。

### NGO — 集中式 Signal Dispatch

```go
// 6 种信号类型
const (
    SignalNone        = 0  // 无副作用
    SignalProgress    = 1  // task_boundary 进度
    SignalYield       = 2  // notify_user 暂停
    SignalSkillLoaded = 3  // SKILL.md 读取触发
    SignalMediaLoaded = 4  // 多模态媒体注入
    SignalSpawnYield  = 5  // 子 Agent spawn 暂停
)

// 声明式终止条件
var TerminalSignals = map[Signal]bool{
    SignalYield: true,
}

// 统一分发 — 替代 run.go 中的 switch/case
func Dispatch(result ToolResult, sink DeltaSink, state *LoopState)
```

**优势**：
- **声明式终止** — 新信号只需加入 `TerminalSignals` map，无需修改循环代码
- **DeltaSink 接口** — 解耦工具协议和 UI/状态层
- **LoopState 共享指针** — `BoundaryState` 通过指针共享，避免状态拷贝
- **ForceNextTool** — 确定性工具链（如 plan → notify_user）

---

## 7. 独有工具分析

### CC 独有

| 工具 | 价值 | 移植建议 |
|------|------|----------|
| **ToolSearchTool** | 减少 50%+ 工具 schema token 占用 | P1 — 高价值优化 |
| **LSPTool** | IDE 级代码分析 | P2 — 需 LSP 基础设施 |
| **REPLTool** | 交互式 REPL 执行 | P2 |
| **NotebookEditTool** | Jupyter notebook 编辑 | P3 — 低频 |
| **BriefTool** | 简洁模式切换 | P3 |
| **EnterWorktreeTool** | Git worktree 管理 | P3 |
| **SleepTool** | 自治模式定时 | P2 — 如实现 proactive |

### NGO 独有

| 工具 | 价值 | CC 可对照 |
|------|------|----------|
| **evo** | 自我进化 + 质量断言 | ❌ CC 无等价 |
| **view_media_tool** | 多模态感知 | ❌ CC 无独立工具（内嵌 FileRead） |
| **undo_edit** | 编辑回撤 | ❌ CC 通过 fileHistory 仅做跟踪 |
| **save_memory / recall** | 跨会话记忆 | ❌ CC 通过 MEMORY.md 文件 |
| **update_project_context** | 项目知识持久化 | ❌ CC 无持久化机制 |
| **edit_fuzzy** (3 级模糊匹配) | 容错编辑 | ❌ CC 精确匹配 |
| **Lazy Comment 检测** | 防止代码省略 | ❌ CC 无 |
| **Error Code 体系** | 结构化错误反馈 | ❌ CC 文字描述 |

---

## 8. 差距总览矩阵

| 维度 | CC | NGO | Level |
|------|-----|-----|-------|
| 工具接口丰富度 | 30+ 方法/属性 | 4 方法最小接口 | ⚠️ |
| 类型安全 | Zod schema 编译期 | map[string]any 运行时 | ❌ |
| 权限粒度 | 逐工具 3 层验证 | 集中式 BehaviorGuard | ⚠️ |
| AST 安全分析 | ✅ bash AST 解析 | ❌ | ❌ |
| ToolSearch 延迟加载 | ✅ | ❌ | ❌ |
| 输出大小控制 | ✅ maxResultSizeChars + 磁盘 | ❌ | ❌ |
| Registry 动态管理 | ❌ | ✅ Enable/Disable/Clone | ✅ NGO优 |
| 统一路径解析 | ❌ (工具内部) | ✅ resolveToolPaths() | ✅ NGO优 |
| Signal 协议分发 | ❌ (内联) | ✅ 6-Signal Dispatch | ✅ NGO优 |
| 声明式终止条件 | ❌ | ✅ TerminalSignals map | ✅ NGO优 |
| 模糊编辑匹配 | ❌ | ✅ 3 级级联 | ✅ NGO独有 |
| Lazy Comment 检测 | ❌ | ✅ 10+ 模式 | ✅ NGO独有 |
| Edit Error Code 体系 | ❌ | ✅ Code 1-10 | ✅ NGO独有 |
| Evo 自进化 | ❌ | ✅ | ✅ NGO独有 |
| Undo 回撤 | ❌ | ✅ | ✅ NGO独有 |
| 多模态感知 | ❌ | ✅ view_media_tool | ✅ NGO独有 |
| UI React 渲染 | ✅ | ❌ | - |
| 工具数量 | 42 | 27 | ⚠️ |
| BashTool 安全深度 | 400+ KB 安全代码 | 委托 BehaviorGuard | ❌ |

---

## 9. 可移植设计建议

### P0 — 即刻收益

1. **输出大小控制** — 在 `ToolResult` 中增加截断机制，>50KB 输出保存到磁盘并返回预览
2. **并发安全标记** — 扩展 Tool 接口增加 `IsConcurrencySafe()` 用于 StreamingToolExecutor
3. **只读/破坏性标记** — 丰富工具元数据用于自动权限分级

### P1 — 架构增强

4. **ToolSearch 机制** — 实现延迟加载 Schema，仅在需要时注入工具定义（减少 prompt token）
5. **输入 Schema 强化** — 从 `map[string]any` 迁移到 struct-based 验证（Go 泛型 or code-gen）
6. **BashTool 安全增强** — 实现命令 AST 解析（可用 `mvdan.cc/sh` Go 库）

### P2 — 长期演进

7. **多 Agent 协作** — 参考 CC AgentTool 实现团队协作（mailbox + 任务分配）
8. **LSP 集成** — 添加 Language Server Protocol 工具用于代码诊断
9. **编码/行尾保持** — 在 edit_file 中增加文件编码检测和保持
