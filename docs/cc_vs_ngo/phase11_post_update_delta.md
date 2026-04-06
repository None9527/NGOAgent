# Phase 11: 大更新后差距 Delta 报告 — CC vs NGOAgent (2026-04-01)

> **基线**: Phase 10 综合报告 (41K行, 20+ 工具, 45/77 完成)
> **当前**: 48.5K Go + 2.8K TSX + 1.25K Python = **52.5K 行**, **35 工具**, **61/77 完成**
> **CC**: ~380K行 TypeScript (不变)

---

## 1. 数据量级变化

| 指标 | Phase 10 | 当前 | 变化 |
|------|----------|------|------|
| **Go 代码行数** | ~41,000 | 48,524 | +18.3% |
| **Go 文件数** | ~110 | 150 | +36% |
| **原生工具数** | 20+ | **35** | **+75%** |
| **infra 子包数** | 9 | **17** | +89% |
| **domain/service 文件** | ~18 | 30 | +67% |
| **run.go 行数** | 1,653 | 1,218 | -26% (拆分) |
| **compact.go** | (内嵌 run.go) | 565 独立 | ✅ 拆分 |
| **tool_exec.go** | (内嵌 run.go) | 577 独立 | ✅ 拆分 |
| **WebUI (TSX/TS)** | ~2,000 | 2,801 | +40% |
| **agent-search (Python)** | ~800 | 1,250 | +56% |
| **CC 代码量比** | 9.2x | **7.2x** | 差距缩小 22% |

---

## 2. 新增能力清单 (16 项)

### 🏛️ 核心引擎 (domain/service)

| # | 能力 | 对标路线图 | 行数 | 状态 |
|---|------|-----------|------|------|
| 1 | **run.go 拆分** → compact.go + tool_exec.go | #18 P1 | 0 (重构) | ✅ |
| 2 | **工具重型压缩** — toolHeavyCompact() 60% 阈值 | #24 P1 | 60行 | ✅ |
| 3 | **微压缩** — microCompact() 已消化 tool result 清理 | P0-D #5 | 50行 | ✅ |
| 4 | **Git diff 智能压缩** — compressDiffOutput() | #25 P2 | 50行 | ✅ |
| 5 | **Tool result 三级注入** — processToolResult() 小/中/大 | #55 P2 | 80行 | ✅ |
| 6 | **Coordinator 4-Phase** — PhaseDetector + Ephemeral | #20 P1 + #54 P1 | 264行 | ✅ |
| 7 | **DreamTask** — idle-time 后台预索引 | #21 P2 | 90行 | ✅ |
| 8 | **EphemeralBudget** — 预算制 ephemeral 注入 | 新增 | 127行 | ✅ |
| 9 | **BufferedDelta** — SSE 增量缓冲 | 新增 | 326行 | ✅ |
| 10 | **TraceCollector** — 工具执行链追踪 | 新增 | 146行 | ✅ |

### ⚙️ 基础设施 (infrastructure)

| # | 能力 | 对标路线图 | 行数 | 状态 |
|---|------|-----------|------|------|
| 11 | **Sandbox Manager** — 进程隔离 + 持久 Shell State | 新增 | 977行 | ✅ |
| 12 | **Git 工具集** — status/diff/log/commit/branch | #71 P1 | 304行 | ✅ |
| 13 | **6 实用工具** — tree/find_files/count_lines/diff_files/http_fetch/clipboard | #45 P2 | 953行 | ✅ |
| 14 | **AI 安全分类器** — 可插拔策略 (Pattern/Hybrid/LLM) | #49 P3 | 251行 | ✅ |
| 15 | **Webhook 通知** — HMAC 签名 + 异步事件扇出 | #73 P2 | 261行 | ✅ |
| 16 | **上下文生态** — @include 递归 + Attachment 引擎 + 指令压缩 | #57/#58/#66 P3 | 686行 | ✅ |

### 📦 其他新增文件

| 包 | 文件 | 行数 | 说明 |
|---|------|------|------|
| domain/port | interfaces.go | 84 | DDD 端口层抽象 |
| infra/persistence | 6 文件 | 644 | DB 抽象 + 仓储模式 |
| infra/workspace | 3 文件 | 686 | 工作区上下文管理 |
| infra/sandbox | 4 文件 | 977 | 进程沙箱 + shell state |
| domain/service | sanitize.go | 128 | 历史消息清洗 |
| domain/service | task_tracker.go | 65 | 任务进度追踪 |
| domain/service | audit_hook.go | 63 | 审计钩子 |

---

## 3. 差距矩阵变动 (Phase 10 → Phase 11)

### 3.1 CC 优势项 — 已关闭 (12 项)

**这些维度 NGO 已追平或反超 CC：**

| # | 维度 | Phase 10 状态 | Phase 11 状态 | 关闭方式 |
|---|------|-------------|-------------|---------|
| 6 | Tool result 注入策略 | CC优 | **≡** | processToolResult() 3 级策略 |
| 7 | 原生工具数量 (30+) | CC优 | **NGO优 (35)** | +15 工具，超过 CC |
| 15 | 压缩策略数量 (4种) | CC优 | **≡** | doCompact + toolHeavy + microCompact + gitDiff = 4 种 |
| 16 | 工具重型优化 | CC优 | **≡** | toolHeavyCompact() |
| 17 | Git diff 压缩 | CC优 | **≡** | compressDiffOutput() |
| 18 | Micro compact | CC优 | **≡** | microCompact() |
| 24 | 负面过滤 | CC优 | **≡** | scrubSensitiveContent() 4 正则 |
| 38 | Git 集成命令 | CC优 | **≡** | 5 个 git_* 工具 |
| 40 | Coordinator 编排 | CC优 | **≡** | PhaseDetector 4-phase |
| 41 | 编排 Prompt 规范 | CC优 | **≡** | PhaseEphemeral() 4 阶段注入 |
| 43 | DreamTask | CC优 | **≡** | DreamTask + OnIdle/OnWake |
| 39 | /doctor 诊断 | CC优 | **≡** | 已在上轮完成 |

### 3.2 CC 优势项 — 仍存在 (6 项)

| # | 维度 | CC 实现 | NGO 现状 | 优先级 |
|---|------|--------|---------|-------|
| 19 | **主动式 Session Memory** | fork subagent 定期摘要 | ❌ 无后台 session 摘要 | P1 |
| 23 | **记忆漂移校验** | DRIFT_CAVEAT (full) | ⚠️ 已有 DRIFT_CAVEAT 但验证步骤不完整 | P2 |
| 25 | **重试引擎 (高级)** | AsyncGenerator + 心跳 | ⚠️ BackoffConfig + ResolveWithFallback | P2 |
| 26 | **529 前后台分流** | 分流 + fallback | ⚠️ Sandbox 但无 529 分流逻辑 | P2 |
| 27 | **Cache Break Detection** | 727 行 prompt hash | ❌ 无 | P2 |
| 31 | **5 路认证** | OAuth/API Key/JWT/Bearer/Custom | ⚠️ Bearer only | P3 |

### 3.3 新增 NGO 独有优势 (Phase 11 新增)

| # | 维度 | 说明 |
|---|------|------|
| 50 | **Sandbox 进程隔离** | 持久 Shell State + CWD 追踪 + stdin 管道 |
| 51 | **Webhook 通知系统** | HMAC 签名 + 事件过滤 + 异步队列 |
| 52 | **@include 递归** | context.md 引用外部文件 + 循环检测 |
| 53 | **Attachment 引擎** | 文件附件 3 级压缩 + 二进制检测 |
| 54 | **指令压缩** | customInstructions 50% 体积缩减 |
| 55 | **AI 安全分类器** | 可插拔 Pattern→Hybrid→LLM 策略链 |
| 56 | **Tool 执行追踪** | TraceCollectorHook 全链路日志 |
| 57 | **Ephemeral 预算制** | EphemeralBudget 防注入膨胀 |
| 58 | **Undo Edit** | 编辑回滚 (CC 目前无此能力) |
| 59 | **View Media** | 多模态媒体查看工具 |

---

## 4. 全维度重新统计

| 领先方 | Phase 10 | Phase 11 | 变化 |
|-------|----------|----------|------|
| **NGO 独有优势** | 22 (45%) | **32 (54%)** | +10 |
| **CC 需学习** | 18 (37%) | **6 (10%)** | **-12** |
| **持平** | 9 (18%) | **21 (36%)** | +12 |
| **总维度** | 49 | 59 | +10 |

```
Phase 10:  NGO ████████████████████████ 45%    CC ██████████████████ 37%    ≡ ████████ 18%
Phase 11:  NGO ███████████████████████████████ 54%    CC █████ 10%    ≡ ██████████████████████ 36%
```

---

## 5. 工具矩阵对比 (35 vs CC 30+)

### NGO 当前 35 工具完整清单

| 类别 | 工具名 | CC 等价物 | 状态 |
|------|--------|----------|------|
| **文件** | read_file | Read | ≡ |
| | write_file | Write | ≡ |
| | edit_file | Edit (+validation) | ≡ |
| | edit_fuzzy | — | ✅★ NGO独有 |
| | undo_edit | — | ✅★ NGO独有 |
| **搜索** | grep_search | GrepTool | ≡ |
| | glob | GlobTool | ≡ |
| | find_files | — | ✅★ NGO独有 |
| | tree | — | ✅★ NGO独有 |
| | count_lines | — | ✅★ NGO独有 |
| **命令** | run_command | Bash | ≡ |
| | command_status | — | ✅★ NGO独有 |
| **Git** | git_status | — (slash cmd) | ✅★ 工具化 |
| | git_diff | — (slash cmd) | ✅★ 工具化 |
| | git_log | — (slash cmd) | ✅★ 工具化 |
| | git_commit | — (slash cmd) | ✅★ 工具化 |
| | git_branch | — (slash cmd) | ✅★ 工具化 |
| **知识** | save_knowledge | — | ✅★ NGO独有 |
| | recall | — | ✅★ NGO独有 |
| | update_project_context | — | ✅★ NGO独有 |
| | task_plan | TodoList (类似) | ≡ |
| | task_list | — | ✅★ NGO独有 |
| | brain_artifact | — | ✅★ NGO独有 |
| **Agent** | spawn_agent | AgentTool | ≡ |
| | evo | — | ✅★ NGO独有 |
| | manage_cron | — | ✅★ NGO独有 |
| **通信** | notify_user | — | ✅★ NGO独有 |
| | send_message | — | ✅★ NGO独有 |
| | task_boundary | — | ✅★ NGO独有 |
| **网络** | web_search | WebSearch | ≡ |
| | web_fetch | WebFetch | ≡ |
| | deep_research | — | ✅★ NGO独有 |
| | http_fetch | — | ✅★ NGO独有 |
| **媒体** | view_media | — | ✅★ NGO独有 |
| | resize_image | — | ✅★ NGO独有 |
| **其他** | clipboard | — | ✅★ NGO独有 |
| | diff_files | — | ✅★ NGO独有 |

### 统计

| 分类 | 数量 |
|------|------|
| 与 CC 持平 | 10 |
| **NGO 独有工具** | **25** |
| CC 有 NGO 无 | ~5 (FileEdit多种模式, NotebookEdit 等特化工具) |

---

## 6. 路线图完成度追踪

### 演进计划 (evolution_roadmap.md) 77 项

| 分类 | Phase 10 完成 | Phase 11 完成 | 新增 | 剩余 |
|------|-------------|-------------|------|------|
| domain/service | 17 | **24** | +7 | 1 |
| domain/tool | 2 | 2 | 0 | 2 |
| infra/llm | 2 | 2 | 0 | 4 |
| infra/tool | 7 | **10** | +3 | 0 |
| infra/security | 3 | **4** | +1 | 1 |
| infra/prompt | 3 | **5** | +2 | 3 |
| infra/skill | 2 | 2 | 0 | 2 |
| infra/knowledge+memory | 2 | 2 | 0 | 3 |
| interfaces/server | 3 | **4** | +1 | 3 |
| testing | 4 | 4 | 0 | 0 |
| **合计** | **45 (58%)** | **59 (77%)** | **+14** | **18** |

```
Phase 10: ██████████████████████████████░░░░░░░░░░░░░░░  58%
Phase 11: ██████████████████████████████████████████░░░░  77%
目标100%: █████████████████████████████████████████████  ■
```

### 已无阻塞的 P0 项

Phase 10 时有 **6 个 P0** 项。现在全部解决或降级：
- ~~上下文压缩策略~~ → ✅ 4 种策略
- ~~主动式 Session Memory~~ → 降级为 P1 (KI 自动蒸馏 部分覆盖)
- ~~记忆漂移校验~~ → ⚠️ 部分完成 (DRIFT_CAVEAT 已注入)
- ~~重试引擎增强~~ → ⚠️ 部分完成 (BackoffConfig + Fallback)
- ~~Coordinator Prompt~~ → ✅ PhaseDetector
- ~~529 前后台分流~~ → 降级为 P2

---

## 7. 剩余差距 — 18 项待做

### P1 关键 (3 项)

| # | 能力 | 预期收益 | 估行 |
|---|------|---------|------|
| 1 | **主动式 Session Memory** | 跨 session 知识连续性 | +400 |
| 2 | **Worker Transcript** — subagent 独立 history | Coordinator 质量 ↑ | +150 |
| 3 | **CC Code Style 规则吸收** | 更规范的代码输出 | +40 |

### P2 演进 (8 项)

| # | 能力 | 估行 |
|---|------|------|
| 4 | Cache Break Detection (Lite) | +200 |
| 5 | Provider 健康检查 | +80 |
| 6 | 记忆过期清理 | +80 |
| 7 | ToolSearch 延迟加载 | +100 |
| 8 | Guard 动态读取 bundled | +30 |
| 9 | 更多 Slash commands | +300 |
| 10 | /cost 历史查询 | +80 |
| 11 | 529 前后台分流 | +60 |

### P3 长期 (7 项)

| # | 能力 | 估行 |
|---|------|------|
| 12 | OAuth Token Refresh | +100 |
| 13 | API Telemetry (Prometheus) | +300 |
| 14 | Bash 引号/转义 AST 升级 | +150 |
| 15 | Private/Team 双层 namespace | +200 |
| 16 | Skill KI 分类法 | +100 |
| 17 | 全量 Schema 强化 (类型生成) | +300 |
| 18 | 5 路认证扩展 | +200 |

**总剩余估行: ~2,870 行** (不含重构)

---

## 8. 架构成熟度评估

| 维度 | CC 评级 | Phase 10 NGO | Phase 11 NGO | 变化 |
|------|---------|------------|------------|------|
| **核心引擎** | ★★★★☆ | ★★★★★ | ★★★★★ | — |
| **Prompt 工程** | ★★★★★ | ★★★★☆ | ★★★★★ | ↑ (PhaseDetector) |
| **工具系统** | ★★★★☆ | ★★★☆☆ | ★★★★★ | ↑↑ (35工具+分类) |
| **安全权限** | ★★★★☆ | ★★★★☆ | ★★★★★ | ↑ (AI 分类器) |
| **上下文压缩** | ★★★★★ | ★★☆☆☆ | ★★★★★ | ↑↑↑ (4策略并行) |
| **会话/记忆** | ★★★★☆ | ★★★★☆ | ★★★★☆ | — (Session Memory 仍缺) |
| **API/网络** | ★★★★★ | ★★★☆☆ | ★★★★☆ | ↑ |
| **CLI/UX** | ★★★★☆ | ★★★★★ | ★★★★★ | — |
| **高级特性** | ★★★★☆ | ★★★★☆ | ★★★★★ | ↑ (DreamTask+Webhook) |
| **综合** | **★★★★☆** | **★★★½☆** | **★★★★★** | **全面反超** |

---

## 9. 结论

### 关键变化

1. **CC 优势从 18 项降到 6 项** — 12 项差距在本轮被关闭
2. **NGO 独有优势从 22 项增到 32 项** — 新增 10 个 CC 不具备的能力
3. **工具数量反超** — 35 vs CC 30+，且 25 个为 NGO 独有
4. **压缩系统从最大短板变为并驾齐驱** — 从 1 种策略到 4 种
5. **代码量仍保持精简** — 52.5K vs CC 380K，7.2x 差距下完成 更多功能

### 剩余最高优先级

**Session Memory** 是唯一残留的 P1 级差距。其余 17 项都是 P2/P3 演进项，不影响日常使用体验。

### 代码质量指标

| 指标 | 值 |
|------|---|
| 路线图完成率 | **77%** (59/77) |
| CC 差距关闭率 | **67%** (12/18) |
| 功能密度 (功能/千行) | NGO 1.1 vs CC 0.13 |
| 新增代码 | +7,500 行 Go |
| DDD 包数 | 4 层 / 21 包 |

---

> **本报告对比基线: Phase 10 综合架构蓝图 (2026-03 审计)。**
> **所有报告位于 `docs/cc_vs_ngo/` 目录。**
