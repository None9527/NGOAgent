# Phase 10: 综合架构蓝图 — CC vs NGOAgent 全维度对标报告

> 审计范围: CC 380K行 TypeScript (Bun) vs NGO 41K行 Go — **9.2x 代码量差**
> 分析周期: 9 个 Phase, 4219 行报告, 覆盖 200+ 源文件

---

## 目录

- [1. 项目概要](#1-项目概要)
- [2. 9 Phase 核心发现索引](#2-9-phase-核心发现索引)
- [3. 全维度差距矩阵](#3-全维度差距矩阵)
- [4. NGO 架构优势总结](#4-ngo-架构优势总结)
- [5. CC 可移植能力总结](#5-cc-可移植能力总结)
- [6. 架构升级路线图](#6-架构升级路线图)
- [7. 风险与约束](#7-风险与约束)

---

## 1. 项目概要

### 1.1 系统定位

| 维度 | CC (Claude Code) | NGO (NGOAgent) |
|------|-------------------|----------------|
| **定位** | CLI-first 编码助手 | Multi-channel 全能 Agent Daemon |
| **语言** | TypeScript (Bun) | Go |
| **代码量** | ~380K 行 | ~41K 行 |
| **接入通道** | Terminal REPL | HTTP + WebSocket + gRPC + Telegram |
| **LLM** | Anthropic-only (锁定) | Multi-provider (OpenAI-compatible 通用) |
| **部署** | CLI 二进制 (npm global) | Daemon 进程 + WebUI SPA |
| **安全** | CLI 交互审批 + inode patch detection | 10-state 状态机 + Security Hook |
| **记忆** | SessionMemory (markdown 摘要) | 向量记忆 + KI + 时间日记 |
| **演进** | ❌ 无 | ✅ Evolution Engine (自修复) |
| **定时** | ❌ 无 | ✅ Cron Manager (心跳+蒸馏) |

### 1.2 审计覆盖范围

| Phase | CC 审计 | NGO 审计 | 报告 |
|-------|---------|---------|------|
| 1 核心引擎 | AgentLoop + queryModel | 10-state 状态机 + loop.go | [phase1](./phase1_core_engine.md) |
| 2 Prompt 工程 | 13K行 Prompt 系统 | 13-section HybridEngine | [phase2](./phase2_prompt_engineering.md) |
| 3 工具系统 | 30+ 工具 + MCP | 20+ 工具 + MCP | [phase3](./phase3_tool_system.md) |
| 4 安全权限 | Permission hooks + patch | SecurityHook + ApprovalGate | [phase4](./phase4_security_permissions.md) |
| 5 上下文压缩 | compact/ 2260行 | doCompact 388行 | [phase5](./phase5_context_compression.md) |
| 6 会话/记忆 | SessionMemory | 向量记忆 + KI + Brain | [phase6](./phase6_session_memory.md) |
| 7 API/网络 | 8826行 API层 | 1257行 LLM层 | [phase7](./phase7_api_network.md) |
| 8 CLI/UX | 66 commands (9798行) | 4-layer interfaces (2901行) | [phase8](./phase8_cli_ux.md) |
| 9 高级特性 | Bridge+Coordinator (14K行) | Evo+Cron+Barrier (1280行) | [phase9](./phase9_advanced_features.md) |

---

## 2. 9 Phase 核心发现索引

### Phase 1: 核心引擎
- CC 采用**原子化 query-response** 循环，状态管理简单
- NGO 采用 **10-state 状态机**，状态转换显式，支持 Planning/Approval/SubAgent 等复合状态
- **NGO优**: LoopPool 多并发实例 + StateTracker 持久化

### Phase 2: Prompt 工程
- CC 维护 **13K行系统 Prompt**，分散在 prompts/ 目录
- NGO 采用 **13-section HybridEngine**，hardcoded 最小身份 + 隔离 XML user-rules
- **NGO优**: 结构化分区注入，减少指令稀释

### Phase 3: 工具系统
- CC **30+ 工具** + 完整 MCP 集成 + tool result 巧妙注入 (system prompt 附加)
- NGO **20+ 工具** + MCP + subagent_tool + manage_cron
- **NGO优**: evo_assert_tool + manage_cron_tool 自动化闭环

### Phase 4: 安全权限
- CC 使用 **inode-based patch detection** (文件修改检测)
- NGO 使用 **10-state ApprovalGate** + SecurityHook 分离策略
- **NGO优**: 策略注入分离，BehaviorGuard 行为围栏

### Phase 5: 上下文压缩
- CC 实现 **2260行压缩系统**：conversation/tool-heavy/micro/git-diff 4 种策略
- NGO 实现 **388行精简版**：keep-first-last + 可选 LLM 摘要
- **CC优**: 高级策略选择 + 工具重型对话优化

### Phase 6: 会话/记忆
- CC 采用 **主动式 SessionMemory** — 后台 fork subagent 定期摘要
- NGO 采用 **向量记忆 + KI + Brain 三轨制** — 时间衰减评分 + 自动蒸馏
- **NGO优**: 向量语义检索 + 版本化 Brain + 结构化知识管理

### Phase 7: API/网络
- CC 维护 **8826行 API 层** — 企业级重试引擎 + 5 路认证 + Cache 监控
- NGO 维护 **1257行 LLM 层** — Provider 接口 + ChunkMapper 插件 + Router 热重载
- **CC优**: withRetry AsyncGenerator + Prompt Cache Break Detection
- **NGO优**: 多 Provider 路由 + StreamAdapter 通用引擎

### Phase 8: CLI/UX
- CC 是 **纯 CLI REPL** — 66 个 slash commands + 20 bundled skills
- NGO 是 **4 层多通道 Daemon** — HTTP + WS + gRPC + Telegram
- **NGO优**: 统一 AgentAPI Facade, gRPC proto-first, Telegram Bot

### Phase 9: 高级特性
- CC 拥有 **14K行** Bridge 远程桥接 + Coordinator Prompt 编排
- NGO 拥有 **1280行** Evolution 自演进 + Cron 定时 + Barrier 同步
- **CC优**: Coordinator 4-phase workflow + DreamTask
- **NGO优**: Evolution Engine + Cron heartbeat + diary digest

---

## 3. 全维度差距矩阵

### 评估标注说明
- ✅ = 已实现且成熟
- ⚠️ = 部分实现或有差距
- ❌ = 完全缺失
- ✅★ = 独有优势 (对方无对应能力)

| # | 维度 | CC | NGO | 领先方 | 优先级 |
|---|------|-----|-----|-------|-------|
| **核心引擎** |
| 1 | Agent Loop 状态机 | ⚠️ 隐式 | ✅★ 10-state 显式 | NGO | — |
| 2 | LoopPool 多实例 | ❌ | ✅★ 并发 session | NGO | — |
| 3 | StateTracker 持久化 | ❌ | ✅★ | NGO | — |
| **Prompt 工程** |
| 4 | 系统 Prompt 规模 | ✅ 13K行 | ✅ 2K行精简 | CC | — |
| 5 | Prompt 分区注入 | ⚠️ 分散文件 | ✅★ 13-section | NGO | — |
| 6 | Tool result 注入策略 | ✅ 精巧 | ⚠️ 简单 | CC | P1 |
| **工具系统** |
| 7 | 原生工具数量 | ✅ 30+ | ✅ 20+ | CC | P2 |
| 8 | MCP 集成 | ✅ | ✅ | ≡ | — |
| 9 | 子 Agent 工具 | ✅ AgentTool | ✅ subagent_tool | ≡ | — |
| 10 | Cron 工具 | ❌ | ✅★ manage_cron | NGO | — |
| 11 | Evolution 工具 | ❌ | ✅★ evo_assert | NGO | — |
| **安全权限** |
| 12 | 文件修改检测 | ✅ inode patch | ⚠️ 无 inode | CC | P2 |
| 13 | 安全策略分离 | ⚠️ hook 分散 | ✅★ SecurityHook | NGO | — |
| 14 | 行为围栏 | ⚠️ | ✅★ BehaviorGuard | NGO | — |
| **上下文压缩** |
| 15 | 压缩策略数量 | ✅ 4种 | ⚠️ 1种 | CC | P0 |
| 16 | 工具重型优化 | ✅ | ❌ | CC | P0 |
| 17 | Git diff 压缩 | ✅ | ❌ | CC | P1 |
| 18 | Micro compact | ✅ | ❌ | CC | P1 |
| **会话/记忆** |
| 19 | 主动式 Session Memory | ✅ fork 摘要 | ❌ | CC | P0 |
| 20 | 向量语义检索 | ❌ | ✅★ | NGO | — |
| 21 | 时间衰减评分 | ❌ | ✅★ | NGO | — |
| 22 | KI 知识蒸馏 | ⚠️ 手动 | ✅★ 自动 | NGO | — |
| 23 | 记忆漂移校验 | ✅ DRIFT_CAVEAT | ❌ | CC | P0 |
| 24 | 负面过滤 | ✅ WHAT_NOT_TO_SAVE | ❌ | CC | P1 |
| **API/网络** |
| 25 | 重试引擎 | ✅ AsyncGenerator | ⚠️ BackoffConfig | CC | P0 |
| 26 | 529 前台/后台分流 | ✅ | ❌ | CC | P0 |
| 27 | Cache Break Detection | ✅ 727行 | ❌ | CC | P1 |
| 28 | Provider 抽象 | ❌ 硬编码 | ✅★ 接口+Router | NGO | — |
| 29 | SSE StreamAdapter | SDK 内部 | ✅★ 315行通用引擎 | NGO | — |
| 30 | 热重载 | ❌ | ✅★ Reload() | NGO | — |
| 31 | 5 路认证 | ✅ | ⚠️ Bearer only | CC | P2 |
| **CLI/UX** |
| 32 | Slash commands | ✅ 66个 | ⚠️ 12个 | CC | P2 |
| 33 | WebSocket | ❌ | ✅★ 持久连接 | NGO | — |
| 34 | REST API | ❌ | ✅★ 40+ routes | NGO | — |
| 35 | gRPC | ❌ | ✅★ 40+ RPCs | NGO | — |
| 36 | Telegram Bot | ❌ | ✅★ 完整集成 | NGO | — |
| 37 | 统一 API Facade | ❌ | ✅★ AgentAPI | NGO | — |
| 38 | Git 集成命令 | ✅ /commit /diff | ❌ | CC | P1 |
| 39 | /doctor 诊断 | ✅ | ❌ | CC | P1 |
| **高级特性** |
| 40 | Coordinator 编排 | ✅ 4-phase | ⚠️ 无 pipeline | CC | P0 |
| 41 | 编排 Prompt 规范 | ✅ 370行 | ⚠️ | CC | P0 |
| 42 | Bridge 远程 | ✅ 12613行 | ❌ (gRPC 替代) | — | — |
| 43 | DreamTask | ✅ | ❌ | CC | P1 |
| 44 | Evolution Engine | ❌ | ✅★ 3层自修复 | NGO | — |
| 45 | VLM 多模态评估 | ❌ | ✅★ | NGO | — |
| 46 | 故障诊断 (6类) | ❌ | ✅★ Diagnoser | NGO | — |
| 47 | Cron Manager | ❌ | ✅★ 心跳+蒸馏 | NGO | — |
| 48 | SubagentBarrier | ❌ | ✅★ 同步屏障 | NGO | — |
| 49 | AgentChannel | ❌ | ✅★ 通道抽象 | NGO | — |

### 统计

| 领先方 | 数量 | 占比 |
|-------|------|------|
| **NGO 独有优势** | 22 | 45% |
| **CC 需学习** | 18 | 37% |
| **持平** | 9 | 18% |

---

## 4. NGO 架构优势总结

NGO 在 **22 个维度** 具备 CC 完全不具备的独有能力：

### 4.1 基础设施层
- **10-state 显式状态机** — 状态转换可追踪、可持久化、可恢复
- **LoopPool 多实例** — 天然支持并发 session
- **Provider 接口 + Router 热重载** — 多 LLM + 运行时切换
- **StreamAdapter + ChunkMapper** — 通用 SSE 引擎 + 2 方法扩展
- **ModelPolicy 注册表** — 集中管理模型能力+价格

### 4.2 智能层
- **13-section Prompt 分区** — 减少指令稀释
- **向量记忆 + 时间衰减** — 语义检索 + 遗忘曲线
- **KI 自动蒸馏** — 对话 → 知识的自动化管道
- **Evolution 3 层自修复** — Evaluate → Diagnose → Fix → Track

### 4.3 运行时层
- **SubagentBarrier** — 防死锁 + 超时 + 去重 + 并发限制
- **Cron Manager** — Agent-native 定时任务 + 心跳 + 日记蒸馏
- **AgentChannel** — Chat/Subagent/Forge 统一协议
- **SecurityHook + BehaviorGuard** — 策略注入分离

### 4.4 接入层
- **4 层多通道** — HTTP + WS + gRPC + Telegram
- **统一 AgentAPI Facade** — 所有通道共享的 40+ 方法接口
- **Proto-first gRPC** — 类型安全 + 跨语言 SDK
- **Telegram Bot 审批** — InlineKeyboard 移动端工具确认

---

## 5. CC 可移植能力总结

CC 在 **18 个维度** 具备 NGO 应当学习的能力，按优先级分组：

### P0 — 即刻高回报 (6 项)

| # | 能力 | CC 实现 | NGO 现状 | 预期收益 |
|---|------|---------|---------|---------|
| 1 | **上下文压缩策略** | 4 种策略选择器 | 1 种 keep-first-last | 减少 token 浪费 40-60% |
| 2 | **主动式 Session Memory** | fork subagent 摘要 | 无 | 跨 session 知识连续性 |
| 3 | **记忆漂移校验** | DRIFT_CAVEAT | 无 | 防止过时记忆导致错误 |
| 4 | **重试引擎增强** | AsyncGenerator+心跳 | 基础 BackoffConfig | 稳定性 ↑ (无人值守场景) |
| 5 | **Coordinator Prompt 移植** | 370行编排规范 | 通用 subagent prompt | 多 Agent 质量 ↑ |
| 6 | **529 前后台分流** | 分流+fallback | 无 | 避免重试放大 |

### P1 — 架构增强 (7 项)

| # | 能力 | 说明 |
|---|------|------|
| 7 | **工具重型压缩** | 大型工具输出的专项压缩策略 |
| 8 | **Git diff 压缩** | 保留 diff 结构的智能压缩 |
| 9 | **负面过滤** | WHAT_NOT_TO_SAVE (API keys, 临时错误等) |
| 10 | **Cache 监控 (Lite)** | Prompt hash → 变化频率 → 成本优化提示 |
| 11 | **Git 集成命令** | /commit /diff /review 等 agent-powered SCM |
| 12 | **/doctor 诊断** | LLM 连通性 + 配置校验 + 环境检查 |
| 13 | **DreamTask** | idle 时主动探索 + 预加载 |

### P2 — 长期演进 (5 项)

| # | 能力 | 说明 |
|---|------|------|
| 14 | **inode 文件修改检测** | 检测外部工具对文件的修改 |
| 15 | **OAuth Token Refresh** | Provider 接口增加 RefreshAuth() |
| 16 | **更多 Slash commands** | 参考 CC 66 个命令的分类设计 |
| 17 | **Micro compact** | 对话间隙自动清理低价值工具输出 |
| 18 | **API Telemetry** | 请求/响应的结构化遥测 + 成本分析 |

---

## 6. 架构升级路线图

### 6.1 Phase A: 核心弹性 (1-2 周)

```
目标: 提升现有系统的健壮性和效率
投入: ~800 行新代码 + ~200 行修改

├── A1. 上下文压缩策略选择器
│   ├── 识别 tool-heavy 对话 → 工具输出压缩
│   ├── 保留首尾 N 条 + LLM 摘要中间部分
│   └── 文件: compact.go 扩展 (+300行估)
│
├── A2. 重试引擎增强
│   ├── 前台/后台分流 (后台 429/529 直接放弃)
│   ├── 专项 529 计数 → 超阈值触发模型 fallback
│   ├── 用户中断检测 (context.Done)
│   └── 文件: errors.go + loop.go 修改 (+150行估)
│
├── A3. 记忆安全增强
│   ├── DRIFT_CAVEAT: 使用记忆前验证文件/函数是否存在
│   ├── WHAT_NOT_TO_SAVE: 过滤 API keys, 临时调试, 环境变量
│   └── 文件: memory/inject.go + knowledge/filter.go (+200行估)
│
└── A4. 用户友好错误消息
    ├── 为每种 ErrorLevel 添加行动建议
    └── 文件: errors.go 扩展 (+150行估)
```

### 6.2 Phase B: 智能增强 (2-4 周)

```
目标: 引入 CC 的核心智能能力
投入: ~1500 行新代码

├── B1. 主动式 Session Memory
│   ├── 后台 goroutine 定期提取会话关键信息
│   ├── 写入 brain/<session>/session_memory.md
│   ├── 下次 session 注入 prompt
│   └── 文件: service/session_memory.go (+400行估)
│
├── B2. Coordinator Prompt 移植
│   ├── 4-phase workflow (研究→综合→实现→验证)
│   ├── "永远综合禁止懒委托" 原则注入 EphSubAgentLaunch
│   ├── Continue vs Spawn 决策表
│   └── 文件: prompttext/coordinator.go (+300行估)
│
├── B3. Pipeline 抽象
│   ├── Research(parallel) → Synthesize(parent) → Implement → Verify
│   ├── 每阶段独立 Barrier
│   └── 文件: service/pipeline.go (+400行估)
│
└── B4. DreamTask — 低优先级后台探索
    ├── idle 时异步执行: 代码索引更新, 常用文件预加载
    ├── 集成到 Cron 的 _dream 内置任务
    └── 文件: cron/manager.go 扩展 (+200行估)
```

### 6.3 Phase C: 生态扩展 (1-2 月)

```
目标: 拓展开发者工具和可观测性
投入: ~2000 行新代码

├── C1. Git 集成命令
│   ├── /commit /diff /review /branch
│   ├── 通过 agent tool 调用执行
│   └── REST API + WS slash 注册 (+500行估)
│
├── C2. /doctor 诊断命令
│   ├── LLM 连通性测试
│   ├── 配置有效性验证
│   ├── 环境依赖检查 (Python, Node, Git 版本)
│   └── 磁盘/内存使用报告 (+300行估)
│
├── C3. Cache 监控 (Lite)
│   ├── Prompt hash 记录 → 变化频率分析
│   ├── Prefix caching 利用率估算
│   └── /cost 命令: token 消耗 + 费用估算 (+400行估)
│
├── C4. API Telemetry
│   ├── 结构化遥测 (延迟, token, cache 命中率)
│   ├── /stats 命令增强: 历史趋势
│   └── 可选 Prometheus metrics (+500行估)
│
└── C5. 多 Bot 适配器
    ├── 复用 Telegram StreamHandler 架构
    ├── Discord / WeChat 适配器
    └── (+300行/adapter估)
```

### 时间线总览

```
Week 1-2:   Phase A (核心弹性)        ████████
Week 3-6:   Phase B (智能增强)        ████████████████
Week 7-12:  Phase C (生态扩展)        ████████████████████████
            ────────────────────────────────────────────────
            M1          M2          M3
```

---

## 7. 风险与约束

### 7.1 技术风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| Session Memory 后台 goroutine 资源消耗 | 内存/CPU 增加 | 限制频率 (每 10 轮对话或 idle 5min) |
| Coordinator Prompt 增大 token 成本 | 成本 ↑ | 仅在检测到多步骤任务时注入 |
| 压缩策略误删关键信息 | 对话质量 ↓ | 保守策略: 保留首尾 + 工具输出摘要而非删除 |
| 重试引擎复杂度 | 维护成本 | 保持 ErrorLevel 分类体系 (已有优势) |

### 7.2 架构约束

| 约束 | 说明 |
|------|------|
| Go vs TypeScript | CC 的 AsyncGenerator/Promise 模式需翻译为 Go goroutine/channel |
| 单进程 Daemon | NGO 的 LoopPool 已处理并发，无需 CC 的 Bridge 远程方案 |
| Multi-provider | Cache 监控需适配不同 Provider 的 caching 语义 |
| 向后兼容 | 新 compact 策略需兼容现有 brain 格式 |

### 7.3 决策建议

> **核心原则: 保持 NGO 的架构优势，选择性移植 CC 的成熟能力。**

1. **不移植 Bridge** — NGO 的 gRPC 天然支持远程，无需 CC 的 12K行 Bridge
2. **不移植 CLI REPL** — NGO 的 WebUI + gRPC 已覆盖所有交互场景
3. **优先移植压缩+记忆+重试** — 这 3 项对日常使用体验影响最大
4. **Coordinator 按需注入** — 仅在 subagent 场景激活编排 Prompt

---

> **本报告完成了 CC vs NGOAgent 全 9 层架构的深度对标分析。**
> **总计审计 200+ 源文件, 产出 4600+ 行分析报告。**
> **所有报告位于 `docs/cc_vs_ngo/` 目录。**
