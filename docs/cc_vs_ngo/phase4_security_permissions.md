# Phase 4：安全与权限层深度对标分析

> CC `utils/permissions/` (24 文件, 350+ KB) + `utils/hooks.ts` (5022 行, 159 KB)
> vs NGO `domain/service/guard.go` (298 行) + `infrastructure/security/hook.go` (412 行)

---

## 目录

- [1. 安全架构概览](#1-安全架构概览)
- [2. 权限决策引擎](#2-权限决策引擎)
  - [2.1 CC 多层权限链](#21-cc-多层权限链)
  - [2.2 NGO BehaviorGuard + SecurityHook](#22-ngo-behaviorguard--securityhook)
- [3. AI 安全分类器 (Auto Mode)](#3-ai-安全分类器)
- [4. Hook 系统](#4-hook-系统)
  - [4.1 CC Hook 系统 (5022 行)](#41-cc-hook-系统)
  - [4.2 NGO SecurityHook (6 方法接口)](#42-ngo-securityhook)
- [5. 行为守卫 (BehaviorGuard)](#5-行为守卫)
- [6. 用户审批系统](#6-用户审批系统)
- [7. 审计追踪](#7-审计追踪)
- [8. 差距总览矩阵](#8-差距总览矩阵)
- [9. 可移植设计建议](#9-可移植设计建议)

---

## 1. 安全架构概览

### CC — 分布式 + AI 增强

```
┌─────────────────────────────────────────────────────────────┐
│                    CC Security LayerCake                      │
├─────────────────────────────────────────────────────────────┤
│ L1: validateInput()      → 语法/语义验证 (per-tool)          │
│ L2: checkPermissions()   → 权限规则匹配 (per-tool)          │
│ L3: Rule Engine          → allow/deny/ask 规则管理 (24 文件) │
│ L4: YOLO Classifier      → AI 安全分类器 (52KB, 2-stage)    │
│ L5: Hook Chain           → 20+ 外部 Hook 类型 (5022 行)     │
│ L6: Sandbox              → macOS seatbelt / Linux namespace │
│ L7: Path Validation      → 62KB 文件系统安全检查            │
└─────────────────────────────────────────────────────────────┘
```

### NGO — 集中式 2 层守卫

```
┌─────────────────────────────────────────────────────────┐
│              NGO Security Architecture                   │
├─────────────────────────────────────────────────────────┤
│ L1: BehaviorGuard       → 行为规则引擎 (298 行)         │
│    ├── Turn Level: Check()        — 响应质量守卫         │
│    └── Step Level: Pre/PostTool() — 工具使用合规         │
│ L2: SecurityHook        → 6 方法 AgentHook (412 行)     │
│    ├── BeforeToolCall    — 安全决策链                    │
│    ├── AfterToolCall     — 后置审计                      │
│    ├── Before/AfterLLM   — LLM 请求拦截                 │
│    └── RequestApproval   — 多通道批准                    │
└─────────────────────────────────────────────────────────┘
```

**代码量对比**：

| 系统 | CC | NGO | 倍率 |
|------|-----|-----|------|
| 权限核心 | 52 KB (`permissions.ts`) | 10 KB (`guard.go`) | 5.2x |
| AI 分类器 | 52 KB (`yoloClassifier.ts`) | **无** | ∞ |
| Hook 系统 | 159 KB (`hooks.ts`) | 13 KB (`hook.go`) | 12.2x |
| 路径验证 | 62 KB (`filesystem.ts`) | **无** | ∞ |
| BashTool 安全 | 98+102 KB (permissions+security) | **无** | ∞ |
| **合计** | **~525 KB** | **~23 KB** | **~23x** |

---

## 2. 权限决策引擎

### 2.1 CC 多层权限链

CC 的 `hasPermissionsToUseTool()` (1487 行) 实现了一个 **7 步决策瀑布**：

```
Step 1: Rule Match (deny → ask → allow)
   1a. Tool-level deny rule  → Deny (immediate)
   1b. Tool-level allow rule → Allow (fast path)
   1c. Tool-level ask rule   → Ask (queue for user)
   1d. Content-level rules (e.g. Bash(git *)) → granular match

Step 2: Tool-specific checkPermissions()
   2a. BashTool → bashToolHasPermission() (98KB!)
       ├── readOnlyValidation (68KB)
       ├── sedValidation (21KB)
       ├── pathValidation (43KB)
       └── destructiveWarning
   2b. FileEditTool → existing file + stale content
   2c. AgentTool → agent type allow/deny

Step 3: Mode-based decision
   3a. default    → ask user
   3b. auto       → AI classifier (Step 4)
   3c. acceptEdits → allow file edits, ask shell
   3d. dontAsk    → auto-deny
   3e. bypass     → allow all (kill-switch guarded)

Step 4: AI Classifier (auto mode only)
   4a. Fast-path: safe-tool allowlist → allow
   4b. Fast-path: acceptEdits would pass → allow
   4c. 2-Stage XML Classifier → block/allow

Step 5: Denial Tracking
   5a. Track consecutive denials
   5b. After DENIAL_LIMITS → fallback to prompting

Step 6: User Prompting
   6a. Interactive: show dialog
   6b. Headless: PermissionRequest hooks → auto-deny

Step 7: Permission Persistence
   7a. "Remember" → persist to settings
   7b. "Session" → session-scoped override
```

**权限规则语法** (`permissionRuleParser.ts`, 7.2KB)：

```
Bash                   // All Bash commands
Bash(git *)            // git subcommands only
Bash(prefix:npm run *)  // npm run subcommands
Bash(regex:^git\s+)   // Regex match
mcp__server1           // All tools from MCP server
mcp__server1__*        // Wildcard MCP
Agent(Explore)         // Specific agent type
```

**多来源规则** (`permissionSetup.ts`, 53KB)：

| 来源 | 优先级 | 持久性 |
|------|--------|--------|
| `managed` | 最高 (组织策略) | /etc/claude-code/settings.json |
| `project` | 高 | .claude/settings.json |
| `user` | 中 | ~/.claude/settings.json |
| `cliArg` | 中 | 命令行参数 |
| `session` | 低 | 当前会话 |
| `command` | 最低 | /command 命令 |

### 2.2 NGO BehaviorGuard + SecurityHook

NGO 采用 **2 层分离架构**：

**Layer 1: BehaviorGuard** (guard.go, 298 行) — 行为合规守卫

| 规则 | 级别 | 检查点 | 行为 |
|------|------|--------|------|
| R1: empty_response | Turn | 空响应+无工具 | warn(3次→terminate) |
| R2: repetition | Turn | 3-gram Jaccard > 0.85 | warn(3次完全相同→terminate) |
| R3: tool_cycle | Turn | 长度 2-4 子序列重复 | warn |
| R4: step_limit | Turn | 超过 maxSteps | terminate |
| R5: post_notify | Step | notify 后继续工具 | warn |
| R6: planning_code_modify | Step | 计划模式无 plan 修改代码 | warn |
| R7: execution_no_task_md | Step | 执行模式无任务 | warn |

**关键创新**：
- **渐进干预** — 空响应先 warn，3 次后才 terminate
- **n-gram Jaccard 相似度** — 比 exact match 更精确检测重复
- **工具序列循环检测** — 检查长度 2-4 的重复子序列（如 `read_file→edit_file→read_file→edit_file`）
- **ForceToolName** — 确定性工具链（plan 后强制 notify_user）

**Layer 2: SecurityHook** (hook.go, 412 行) — 安全决策+审批

```go
// 6 方法 AgentHook 接口
BeforeToolCall(ctx, toolName, args)    → (Decision, reason)
AfterToolCall(ctx, toolName, result)   → audit
BeforeLLMCall(ctx, req, step)          → rate limiting
AfterLLMCall(ctx, resp, step)          → content validation
BeforeRespond(ctx, resp)               → output sanitization
RequestApproval(toolName, args, reason) → async approval
```

**2 种决策模式**：

| 模式 | 行为 |
|------|------|
| `allow` | 全部自动通过，仅 blocklist 命令 Ask |
| `ask` | 全部需要用户确认 |

**Evo 沙盒策略** — 独立的 `evoDecide()`：
- evo 工具自身 → Allow
- 文件操作 → 必须在 `/tmp/ngoagent-evo/` 内
- 读取工具 → Allow
- 命令执行 → Allow (audited)

---

## 3. AI 安全分类器

### CC YOLO Classifier (52KB, 1496 行)

CC 的 **auto mode** 的核心是一个 **2-Stage XML AI 分类器**：

**架构**：
```
Stage 1: Fast (max_tokens=64, stop_sequences=[</block>])
  → <block>yes</block>  → escalate to Stage 2
  → <block>no</block>   → Allow (终止)

Stage 2: Thinking (max_tokens=256, chain-of-thought)
  → <thinking>...</thinking><block>yes</block><reason>...</reason> → Block
  → <thinking>...</thinking><block>no</block>                     → Allow
```

**输入构建**：
```
System Prompt = BASE_PROMPT + PERMISSIONS_TEMPLATE
User Prompt   = <transcript> + compact_action + XML_SUFFIX
```

- **Transcript 构建** (`buildTranscriptEntries()`)：从消息历史提取 user text + assistant tool_use （排除 assistant text 防范注入）
- **紧凑格式** (`toCompactBlock()`)：`{"Bash": "git push"}` / `{"user": "部署到生产环境"}`
- **CLAUDE.md 注入**：分类器接收用户配置作为意图信号

**安全特性**：
- `stripThinking()` — 防止 `<block>` 标签出现在思考内容中
- 只包含 `tool_use` 块，排除 assistant text（防 prompt injection）
- Denial tracking — 连续拒绝后 fallback 到用户提示
- Error dump — 错误时保存完整 prompt 到临时目录

**成本追踪**：分类器有独立的 token/USD 开销追踪 → 不计入主会话成本

### NGO — 无 AI 分类器

NGO 完全缺少 AI 安全分类器。安全决策基于静态规则（blocklist + mode）。

**差距评级**：❌ **重大差距** — AI 分类器是 CC auto mode 的核心支柱，它让 CC 能够在不需要用户确认的情况下安全执行大部分操作。

---

## 4. Hook 系统

### 4.1 CC Hook 系统 (5022 行)

CC 的 Hook 系统是一个**完整的事件驱动扩展框架**：

**20+ Hook 事件类型**：

| 类别 | 事件 | 触发时机 |
|------|------|----------|
| **工具** | PreToolUse | 工具执行前 |
| | PostToolUse | 工具执行后（成功） |
| | PostToolUseFailure | 工具执行后（失败） |
| | PermissionDenied | 权限被拒绝 |
| | PermissionRequest | 权限请求（headless） |
| **会话** | SessionStart | 会话开始 |
| | SessionEnd | 会话结束 |
| | Setup | 首次初始化 |
| **Agent** | SubagentStart | 子 Agent 启动 |
| | SubagentStop | 子 Agent 结束 |
| | TeammateIdle | 团队成员空闲 |
| **任务** | TaskCreated | 任务创建 |
| | TaskCompleted | 任务完成 |
| **配置** | ConfigChange | 配置变更 |
| | InstructionsLoaded | CLAUDE.md 加载 |
| **用户** | UserPromptSubmit | 用户提交 prompt |
| **MCP** | Elicitation | MCP 授权请求 |
| | ElicitationResult | MCP 授权结果 |
| **系统** | Notification | 通知事件 |
| | Stop | 停止事件 |
| | StopFailure | 停止失败 |

**Hook 执行模式**：
1. **Command Hook** — 执行 shell 命令（bash/PowerShell）
2. **HTTP Hook** — 发送 HTTP 请求
3. **Prompt Hook** — 使用 LLM 生成回应
4. **Agent Hook** — 启动子 Agent 处理
5. **Function Hook** — 内存中函数回调

**Hook JSON 输出协议**：
```json
{
  "continue": true/false,        // 是否继续
  "stopReason": "string",         // 停止原因
  "decision": "approve|block",    // 权限决策
  "reason": "string",             // 决策原因
  "systemMessage": "string",      // 系统消息
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow|deny|ask",
    "updatedInput": {...},         // 修改工具输入
    "additionalContext": "string"  // 上下文注入
  }
}
```

**安全特性**：
- **Workspace Trust** — 所有 Hook 需要工作区信任
- **Managed-Only 模式** — 只允许组织管理的 Hook
- **超时控制** — 10 分钟默认，SessionEnd 1.5 秒
- **Async Background** — Hook 可异步执行并异步唤醒
- **Plugin Options** — Hook 可使用插件配置变量替换

### 4.2 NGO SecurityHook (6 方法接口)

```go
// 6 方法接口（412 行）
BeforeToolCall  → normalDecide() / evoDecide()
AfterToolCall   → audit recording
BeforeLLMCall   → step count monitoring
AfterLLMCall    → content validation (stub)
BeforeRespond   → output sanitization (<system> tag stripping)
RequestApproval → async approval via SSE
```

**审批系统** (PendingApproval):
```go
type PendingApproval struct {
    ID       string
    ToolName string
    Args     map[string]any
    Reason   string
    Result   chan bool       // buffered(1) — non-blocking resolution
    Created  time.Time
}

// HTTP POST /v1/approve → h.Resolve(id, true/false)
// SSE event ← pending approval notification
```

**独有特性**：
- **多通道审批** — `RegisterApprovalFunc(channel, fn)` 支持不同通道的审批逻辑
- **过期清理** — `CleanupExpired(maxAge)` 防止挂起请求泄漏
- **会话覆盖** — `AddOverride(toolName)` 允许用户为当前会话覆盖拒绝
- **Shell 注入检测** — `hasShellInjection()` 正则检测 `; & | \` $ ()`
- **安全区域** — `isInSafeZone()` 白名单 `/tmp/` + workspace
- **依赖安装检测** — `isDependencyInstall()` 识别 pip/npm/go/brew 安装

---

## 5. 行为守卫 (BehaviorGuard)

### CC — 无等价系统

CC **没有** BehaviorGuard 等价。它依赖：
- CLAUDE.md 中的行为指导
- Prompt 中的 Ant-only 规则（"Report outcomes faithfully"）
- 用户中断（Ctrl+C/Escape）
- `maxTurns` 限制（相当于 `step_limit`）

### NGO — 完整行为守卫

NGO 的 BehaviorGuard 是其**独有的核心安全创新**：

**1. 渐进干预机制**：
```
空响应 → warn("请提供有意义响应") → 3次 → terminate("重复空响应")
重复响应 → warn("尝试不同方法") → 3次精确重复 → terminate("重复循环")
```

**2. n-gram Jaccard 相似度**：
- 3-gram 提取 → Set 交集/并集 → 相似度 > 0.85 → warn
- 成本极低：O(n) 时间/空间，无需 LLM 调用

**3. 工具序列循环检测**：
- 维护最近 10 个工具名
- 检查长度 2-4 的重复子序列
- 例如：`read_file → edit_file → read_file → edit_file` → "Detected cycle, try different approach"

**4. 计划模式合规**：
- planning 模式 + 无 plan.md → 修改代码 → warn
- execution 模式 + 无 task.md → 修改代码 → warn
- notify_user 后 → 继续工具调用 → warn

**差距评级**：✅ **NGO 独有** — CC 完全依赖用户和 prompt 来约束行为，NGO 有程序化的自动干预。

---

## 6. 用户审批系统

### CC — 交互式 + Headless

CC 的审批系统分两条路径：

| 模式 | 实现 |
|------|------|
| **Interactive REPL** | TUI 对话框 → Y/N/Always/Session/Deny |
| **Headless/SDK** | PermissionRequest Hook → 外部系统决策 |
| **dontAsk** | 自动拒绝所有需要确认的操作 |

**记忆型审批**：
- "Always Allow" → 持久化到 `~/.claude/settings.json`
- "Session Allow" → 仅当前会话
- Rule 支持 wildcard: `Bash(npm run *)` → 所有 npm run 命令

### NGO — SSE + HTTP

```go
// 1. Agent 调用 WaitForApproval() — 阻塞等待
approvalID, approved := hook.WaitForApproval(ctx, toolName, args, reason)

// 2. 前端收到 SSE event:
{"type": "approval_required", "id": "abc12345", "tool": "run_command", ...}

// 3. 用户通过 HTTP POST 决策:
POST /v1/approve {"id": "abc12345", "approved": true}

// 4. hook.Resolve() → 唤醒 WaitForApproval goroutine
```

**差距**：CC 的审批更细粒度（支持规则记忆、wildcard、多来源优先级），NGO 的更简洁（单一审批/拒绝）。

---

## 7. 审计追踪

### CC — 分散式 Analytics

CC 使用 `logEvent()` 遍布整个 codebase（数百个调用点），但**无集中审计日志**。

### NGO — 集中式 AuditEntry

```go
type AuditEntry struct {
    Timestamp time.Time
    ToolName  string
    Args      map[string]any
    Decision  Decision       // Allow/Deny/Ask
    Level     DecisionLevel  // block/user/system/policy
    Reason    string
    Mode      string         // chat/evo
}

// 自动保留最近 500 条（超 1000 时裁剪）
// 可通过 GetAuditLog(limit) 查询
```

**差距评级**：✅ NGO优 — 集中式审计日志比分散式 analytics 更适合安全审计。

---

## 8. 差距总览矩阵

| 维度 | CC | NGO | Level |
|------|-----|-----|-------|
| **安全代码量** | ~525 KB | ~23 KB | 23x |
| AI 安全分类器 (auto mode) | ✅ 2-Stage XML Classifier (52KB) | ❌ | ❌ **重大** |
| 权限规则语法 | ✅ 复杂（wildcard/regex/prefix） | ⚠️ 简单（blocklist） | ❌ |
| 多来源规则优先级 | ✅ 6 级来源 | ❌ 单一 config | ❌ |
| Hook 事件类型 | ✅ 20+ 类型 | ⚠️ 6 方法接口 | ❌ |
| Hook 执行模式 | ✅ 5 种 (command/HTTP/prompt/agent/function) | ❌ | ❌ |
| 工作区信任 | ✅ | ❌ | ⚠️ |
| 沙盒 | ✅ macOS seatbelt (5KB) | ❌ | ⚠️ |
| 路径验证 | ✅ 62KB filesystem.ts | ⚠️ 基本 isInSafeZone | ❌ |
| 记忆型审批 | ✅ Always/Session/Rule | ⚠️ Session override | ⚠️ |
| Denial Tracking | ✅ | ❌ | ⚠️ |
| Shadowed Rule Detection | ✅ 8KB | ❌ | ⚠️ |
| BehaviorGuard | ❌ | ✅ 298 行 | ✅ NGO独有 |
| 渐进干预 | ❌ | ✅ warn→terminate | ✅ NGO独有 |
| n-gram 重复检测 | ❌ | ✅ Jaccard > 0.85 | ✅ NGO独有 |
| 工具序列循环检测 | ❌ | ✅ 长度 2-4 子序列 | ✅ NGO独有 |
| 计划模式合规 | ❌ | ✅ code-in-plan 检测 | ✅ NGO独有 |
| ForceToolName 确定性链 | ❌ | ✅ | ✅ NGO独有 |
| 集中式审计日志 | ❌ | ✅ AuditEntry | ✅ NGO独有 |
| 多通道审批 | ❌ | ✅ RegisterApprovalFunc | ✅ NGO独有 |
| 过期审批清理 | ❌ | ✅ CleanupExpired | ✅ NGO独有 |
| Shell 注入检测 | ❌ 委托 AST | ✅ regex | ✅ |
| Evo 沙盒策略 | ❌ | ✅ evoDecide() | ✅ NGO独有 |
| 输出消毒 | ❌ | ✅ `<system>` 标签剥离 | ✅ NGO独有 |

---

## 9. 可移植设计建议

### P0 — 即刻收益

1. **权限规则语法** — 扩展 NGO 的 blocklist 支持 `ToolName(pattern)` 语法（如 `run_command(rm *)`, `run_command(prefix:sudo *)`）
2. **规则持久化** — 将 "Always Allow" 决策持久化到用户配置，减少重复审批
3. **Denial Tracking** — 记录连续拒绝次数，达阈值后降级到 ask 模式

### P1 — 架构增强

4. **AI 安全分类器** — 实现基于 sideQuery 的安全分类器（可用小模型如 Claude Haiku）
5. **Hook 扩展** — 增加 PreToolUse/PostToolUse Hook 支持外部安全策略
6. **路径验证增强** — 实现 symlink 解析、路径遍历检测

### P2 — 长期演进

7. **多来源规则** — 支持 managed (组织) → project → user → session 优先级链
8. **规则冲突检测** — 实现 shadowed rule detection（CC 8KB 专门处理）
9. **Workspace Trust** — 增加首次使用信任确认机制
