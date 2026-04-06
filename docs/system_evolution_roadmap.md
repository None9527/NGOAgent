# NGOAgent 系统级进化路线图

> 生成时间: 2026-04-02
> 目标: 从顶级代码助手 → 系统级通用 Agent 框架

---

## 一、当前能力基线

| 模块 | 当前水平 | 核心实现 |
|------|---------|---------|
| Agent Loop | 10-state FSM | `run.go` doGenerate 循环 |
| Prompt 工程 | 19-section Registry + CacheTier + U-shape | `engine.go` + `registry.go` |
| 多代理 | Barrier 异步协调 + 4-phase protocol | `spawn_agent.go` + barrier |
| 工具系统 | Interface + Registry + MCP | 25+ 内建工具 |
| 安全 | 3-strategy AI 分类器 + Sandbox | `security/` |
| 记忆 | KI 持久化 + 向量检索 + Diary + time-decay | `knowledge/` + `memory/` |
| LLM 基建 | Router + 降级 + 健康检测 + 多段 cache | `llm/` |
| 通道 | CLI + Web + Telegram + gRPC | 4 通道统一 session |
| 上下文 | 4D compact + token 预估 + 自动裁剪 | `prompt/prune` |
| 定时任务 | Cron 系统 (2 active jobs) | `cron/` |
| Webhook | 通知系统 | `webhook/` |
| Skills | 53 个 skill + budget 降级 + 触发注入 | `skill/` |

---

## 二、系统级进化候选项

### A. 架构内核层

| ID | 候选项 | 描述 | 当前状态 | 竞品参考 | 预估工作量 | 决策 |
|----|--------|------|---------|---------|-----------|------|
| A1 | **Domain Profile 层** | 将代码专属内容（DoingTasks/工具集/行为规则）从内核拆分到可插拔的 DomainProfile 接口，内核变为 domain-agnostic | ❌ 代码焊死 | 无（NGO 独创） | 1-2天 | ☐ |
| A2 | **Workflow/DAG 引擎** | StateGraph 条件分支编排：if A→B else→C，支持循环、中断、恢复 | ❌ 线性 FSM | LangGraph, Google ADK | 2-3周 | ☐ |
| A3 | **Event/Trigger 系统** | 外部事件（webhook 回调、文件变更、消息到达）自动启动新 agent session | ⚠️ Cron 有基础 | AutoGPT, n8n | 1周 | ☐ |
| A4 | **Plugin 热加载** | 运行时动态加载/卸载 Go plugin 或 WASM 模块，无需重编译 | ❌ 编译时注册 | Terraform, HashiCorp | 2周 | ☐ |
| A5 | **Session 持久化中断/恢复** | agent 可中断→持久化到 DB→跨进程/跨重启恢复执行 | ⚠️ blocked_on_user 非持久 | LangGraph checkpoint | 3-5天 | ☐ |
| A6 | **多租户/多用户隔离** | auth + session 隔离，多人共用同一个实例 | ❌ 单用户 | Devin, OpenAI API | 1周 | ☐ |

---

### B. IO 能力层

| ID | 候选项 | 描述 | 当前状态 | 竞品参考 | 预估工作量 | 决策 |
|----|--------|------|---------|---------|-----------|------|
| B1 | **结构化输出 (JSON Schema)** | 支持 `response_format: json_object` + schema 强制 + 输出验证重试 | ❌ 仅自由文本 | OpenAI structured output | 3-5天 | ☐ |
| B2 | **Multi-modal 输入** | user message 接受图片/音频/视频 base64 或 URL 引用 | ❌ 仅文本 | GPT-4V, Google ADK | 3天 | ☐ |
| B3 | **Multi-modal 输出** | agent 主动生成图片（DALL-E/SD）、音频（TTS）、视频 | ❌ | Devin, AutoGPT | 1周 | ☐ |
| B4 | **通用 HTTP/API 工具** | 独立的 http_request 工具，支持任意 REST/GraphQL 调用 + auth header + response schema | ⚠️ http_fetch 绑定 agent-search | AutoGPT plugins | 2-3天 | ☐ |
| B5 | **Browser 自动化** | Playwright/Puppeteer 集成，支持截图、表单填写、页面导航 | ❌ | Devin, Browser-use | 1-2周 | ☐ |
| B6 | **文件格式扩展** | 原生支持 Excel/PDF/Word/CSV 读写 | ⚠️ 通过 run_command 间接 | AutoGPT | 3-5天 | ☐ |

---

### C. 智能增强层

| ID | 候选项 | 描述 | 当前状态 | 竞品参考 | 预估工作量 | 决策 |
|----|--------|------|---------|---------|-----------|------|
| C1 | **自动化验证循环** | run test → 分析失败 → auto-fix → re-test 闭环 | ❌ 手动 | CC verify-fix loop | 3-5天 | ☐ |
| C2 | **Reflection/Self-critique** | agent 执行后自动 review 输出质量，低于阈值则 retry | ❌ | Reflexion, AutoGPT | 3天 | ☐ |
| C3 | **Tool Result 转换层** | 工具结果 → 结构化 Observation → 可选 Reflection 步骤 | ⚠️ 直接拼入 context | ReAct pattern | 2-3天 | ☐ |
| C4 | **Long-term Planning** | 超长任务（跨小时/跨天）的持久化计划 + 进度恢复 | ⚠️ task.md 基础 | Devin | 1周 | ☐ |
| C5 | **学习/自我进化** | 从历史成功/失败中提取 few-shot examples，动态注入 prompt | ⚠️ Evo 系统有基础 | AutoGPT | 1周 | ☐ |
| C6 | **Reasoning 模式切换** | CoT/ToT/ReAct 动态切换，简单任务跳过推理直接执行 | ❌ 固定模式 | Google ADK | 3-5天 | ☐ |

---

### D. 代码助手增强层（CodingProfile 专属）

| ID | 候选项 | 描述 | 当前状态 | 竞品参考 | 预估工作量 | 决策 |
|----|--------|------|---------|---------|-----------|------|
| D1 | **LSP/语言服务集成** | 类型推断、引用查找、符号跳转 — 语义级代码理解 | ❌ | Cursor, CC (via IDE) | 2-3周 | ☐ |
| D2 | **Unified Diff 预览/Apply** | 编辑使用 unified diff 格式 + apply/reject UI | ❌ string match | CC search/replace, Cursor apply model | 1周 | ☐ |
| D3 | **Git 深度集成** | 自动 commit message、PR 描述生成、branch workflow、conflict resolution | ⚠️ gitCommitSection 基础 | CC, GitHub Copilot | 1周 | ☐ |
| D4 | **代码质量门控** | lint → fix → build → test 自动 pipeline | ❌ | CC, Cursor | 3-5天 | ☐ |
| D5 | **Notebook 支持** | .ipynb 原生读写 + cell 级编辑 | ❌ | CC NotebookEdit | 3天 | ☐ |

---

### E. 基建运维层

| ID | 候选项 | 描述 | 当前状态 | 竞品参考 | 预估工作量 | 决策 |
|----|--------|------|---------|---------|-----------|------|
| E1 | **OpenTelemetry 集成** | 分布式追踪 + Metrics + 日志 → Grafana/Datadog | ⚠️ 内部 TelemetryEvent | 行业标准 | 3-5天 | ☐ |
| E2 | **Rate Limiter** | 按 provider/model/user 的速率控制 + 令牌桶 | ⚠️ 仅 429 重试 | 行业标准 | 2天 | ☐ |
| E3 | **Configuration Hot-reload** | 配置变更不需重启进程 | ⚠️ ConfigManager 有基础 | 行业标准 | 1-2天 | ☐ |
| E4 | **Health API** | `/healthz` `/readyz` + 依赖检查（DB/LLM/MCP） | ❌ | K8s 标准 | 1天 | ☐ |
| E5 | **Docker/K8s 部署** | Dockerfile + Helm chart + CI/CD pipeline | ❌ | 行业标准 | 2-3天 | ☐ |

---

## 三、统计摘要

| 层 | 候选数 | 已有基础 | 完全缺失 |
|----|--------|---------|---------|
| A. 架构内核 | 6 | 2 (A3,A5) | 4 |
| B. IO 能力 | 6 | 1 (B4) | 5 |
| C. 智能增强 | 6 | 3 (C3,C4,C5) | 3 |
| D. 代码助手 | 5 | 1 (D3) | 4 |
| E. 基建运维 | 5 | 2 (E2,E3) | 3 |
| **合计** | **28** | **9** | **19** |

---

## 四、决策区

> 请在上方表格的「决策」列标注：
> - ✅ 要做
> - ❌ 不做
> - 🔄 以后再议
> - ⭐ 优先做
>
> 标注完成后，我会根据你的选择生成执行计划和依赖关系图。

---

*此文档由 NGOAgent 架构审计自动生成，基于当前代码库状态和竞品分析。*
