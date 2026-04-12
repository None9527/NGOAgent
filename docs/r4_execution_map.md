# R4 域能力外化执行图

> Updated: 2026-04-12
> Status: Phase 1 started

## 目标

R4 的目标不是先补插件功能，而是把 runtime、application assembly、domain capability 的边界拆清楚，让 NGOAgent 从“编程助手本体”继续转向“runtime + plugins”。

## 执行顺序

1. 先把 `application.Build` 里的局部装配块拆成明确 assembly helpers。
2. 再把 coding/browser/media/research 等工具组从主 builder 中外移到能力装配面。
3. 再把 plugin 边界变成 runtime 可发现、可编排的能力源。
4. 最后再收敛 transport 对具体 facade 的依赖。

## Phase 1：Builder 拆层

第一刀从 LLM provider/router 装配开始：

- `builder.go` 不再直接知道 Anthropic / Google / OpenAI-compatible provider 的构造细节。
- 启动装配和 hot-reload 复用同一套 provider assembly。
- 行为保持不变，只收窄 `Build` 的总装配职责。

第二刀从内置工具注册开始：

- `builder.go` 不再逐个注册 read/write/search/knowledge/git/media 等内置工具。
- `spawn_agent` 与 `skill` 这两个需要后续 runtime wiring 的工具由 assembly 显式返回。
- MCP / skill promotion / cron 这类运行时发现能力暂时保留在 builder 生命周期段，后续再拆到 discovery/plugin assembly。

第三刀从 R3 orchestration wiring 开始：

- `builder.go` 不再直接组装 EventBus / ToolDiscovery / A2A AgentCard。
- `assembleOrchestration` 统一返回 runtime 需要持有的 orchestration 句柄。
- discovery adapter 仍复用现有 registry / MCP / skill manager，避免引入新的 plugin 边界语义。

第四刀从 runtime capability 启动开始：

- `builder.go` 不再内联 skill watcher / MCP config merge / MCP tool registration / skill auto-promotion。
- `startRuntimeCapabilities` 先保持现有启动顺序不变，把动态能力来源集中在一个 assembly 面。
- cron manager 仍保留在 builder 生命周期段，因为它依赖 `baseDeps` 克隆和 runner 构造，后续再单独拆。

第五刀从 hot-reload subscriptions 开始：

- `builder.go` 不再内联 LLM / security / MCP / agent 的 config watcher 回调。
- `registerHotReloadSubscriptions` 保留原闭包行为，包括延迟读取 `loopPool` 指针。
- MCP reload 后的 registry sync 仍在同一回调里执行，保持 AgentLoop 工具缓存失效语义。

第六刀从 cron runtime 开始：

- `builder.go` 不再内联 cron manager 初始化、isolated deps 克隆和 `manage_cron` 注册。
- `assembleCronRuntime` 保留 cron runner 的隔离策略：禁用 KI hooks / KI store / KI retriever，并使用 cron log sink。
- cron 初始化失败仍只记录 warning 并返回 nil，保持原启动容错行为。

第七刀从 engine/session assembly 开始：

- `builder.go` 不再内联 hook chain、Evo evaluator、`service.Deps`、LoopFactory、main loop、subagent orchestrator、loop pool、session/chat/model/tool admin。
- `assembleEngine` 保留 `loopPool` 延迟捕获语义，确保 `spawn_agent` runtime parent lookup 仍能在 pool 创建后生效。
- Title distillation 和 bootstrap ready hook 仍在 session manager 创建后注册，保持原顺序。

第八刀从 transport/server assembly 开始：

- `builder.go` 不再内联 `ApplicationServices`、HTTP caps、A2A handler 注入、HTTP server、gRPC server 构造。
- `assembleTransports` 保持 `ApplicationDeps` 字段原样搬移，并继续在 HTTP transport 上挂载 A2A handler。
- gRPC 端口默认值和 config override 语义保持不变。

第九刀从 foundation / core / storage assembly 开始：

- `builder.go` 不再内联 config bootstrap、DB/store 创建、built-in agent registry load、LLM/prompt/workspace/security 初始化、KI/memory/diary/workspace/skill/MCP 初始化。
- `assembleFoundation` 负责配置和持久层基础对象；`assembleCoreInfrastructure` 负责 LLM router、prompt engine、workspace sandbox 与 security hook；`assembleStorage` 负责 brain/KI/memory/workspace/skill/MCP。
- 默认 session ID 仍固定为 `__default__`，保持 CLI/fallback loop 不创建 ghost session 的语义。

第十刀从 App result assembly 开始：

- `builder.go` 不再内联最终 `App` 字段映射。
- `assembleApp` 集中维护 runtime 句柄到公开 `App` 结构的映射，便于后续替换内部 assembly result 类型。

顺手清理：

- 删除无调用方的 `generateSessionID`，固定 `__default__` session ID 语义已由 `assembleStorage` 承载。

第十一刀从 assembly result 收敛开始：

- `builder.go` 减少 foundation / core / storage / engine / orchestration / transports 的中间字段展开，直接把 assembly result 传入后续 helper。
- `assembleTransports` 和 `assembleApp` 改为接收 assembly result，集中维护跨阶段字段映射。
- 目标是让 `Build` 继续向“阶段流水线”靠拢，而不是继续承担字段搬运职责。

第十二刀从 capability source 抽象开始：

- `startRuntimeCapabilities` 不再直接内联 MCP 与 skill 的启动细节，而是启动一组 `capabilitySource`。
- MCP 和 skill auto-promotion 各自成为独立 source，后续 plugin/provider 能按同一入口接入 registry。
- 当前仍保持原启动顺序：skill watcher -> MCP start/register -> skill auto-promotion。

## 完成标志

- `Build` 保留依赖顺序和生命周期控制，不继续承载所有领域构造细节。
- LLM / tools / discovery / transports 各自有可命名的 assembly 边界。
- 新能力优先进入 capability/provider 装配面，而不是继续塞进主 builder。
