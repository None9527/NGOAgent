# R4 域能力外化执行图

> Updated: 2026-04-13
> Status: R4 completed

## 目标

R4 的目标不是先补插件功能，而是把 runtime、application assembly、domain capability 的边界拆清楚，让 NGOAgent 从“编程助手本体”继续转向“runtime + plugins”。

## 执行顺序

1. 先把 `application.Build` 里的局部装配块拆成明确 assembly helpers。
2. 再把 coding/browser/media/research 等工具组从主 builder 中外移到能力装配面。
3. 再把 plugin 边界变成 runtime 可发现、可编排的能力源。
4. 最后再收敛 transport 对具体 facade 的依赖。

## 完整 Plan

### P1：Builder 只保留生命周期编排

目标：`Build` 只表达启动顺序、错误返回和生命周期控制，不承载具体领域构造细节。

已完成：

- foundation / core / storage / tools / engine / hot-reload / cron / runtime capability / orchestration / transport / app result 都已有明确 assembly helper。
- assembly result 开始收敛，`Build` 不再大量展开中间字段。
- `assembleApp` 集中维护公开 `App` 字段映射。

后续优化：

- 继续降低跨阶段参数散落，优先把同一阶段共享输入收敛成 result/input。
- `Build` 中的生命周期动作保留，但不再新增领域构造逻辑。

完成标准：

- 新增能力只能落到明确 assembly/provider/source 面，不能直接塞回 `Build`。
- `Build` 可按阶段快速审阅启动顺序。

### P2：内置能力 provider 化

目标：coding / filesystem / research / media / git / knowledge / runtime helper 等能力成为可命名 provider，而不是匿名工具清单。

已完成：

- 内置工具按 provider 分组。
- provider 具有稳定 `Name()`。
- provider 通过 `toolAssemblyInput.Register` 显式注册工具。
- provider manifest 记录每组注册工具，并输出启动摘要。
- 回归测试锁住默认工具集合、provider 名称顺序、runtime handle 映射和 manifest。

后续优化：

- 按能力继续细分 provider 命名时，不得破坏当前 provider identity。
- 动态 provider 注入如果引入，必须复用当前 manifest/identity 边界。

完成标准：

- 内置能力能通过 provider 名称定位、观测和测试。
- 新工具加入时必须进入某个 provider，且 manifest 能看见归属。

### P3：Runtime capability source 化

目标：MCP / skill / plugin / cron-like runtime 能力都通过明确 source 进入 runtime，而不是散落在 builder 生命周期段。

已完成：

- `startRuntimeCapabilities` 已抽象为 `capabilitySource` 列表。
- skill watcher / MCP / skill promotion 仍保持原启动顺序。

已收口补充：

- source 已具备稳定 identity 与启动摘要。
- MCP register、skill promotion 已进入可观测 source 合同。
- cron runtime 仍后置，不属于 R4 最小完成面；后续拆分必须保留 isolated deps 策略和启动容错。

完成标准：

- runtime 动态能力来源可列举、可观测、可测试。
- 新动态能力不再直接改 builder 生命周期段。

### P4：Discovery / plugin 边界

目标：plugin 不只是目录概念，而是 runtime 可发现、可编排的 capability source。

已完成：

- builtin provider manifest 接入 discovery，builtin capability 能暴露 provider source。
- MCP capability 继续暴露 server source，skill capability 暴露稳定 skill identity，并保留 path metadata。
- discovery 聚合层现在能回答能力来源：builtin provider / MCP server / skill identity。

已收口补充：

- plugin/provider/source 的最小 identity 合同已经固定在 runtime source / discovery source 语义上。
- 当前仍避免过早引入插件执行模型；R4 只固定发现和注册边界。

完成标准：

- runtime 能回答“能力来自哪里、属于哪个 provider/source、如何被 discovery 暴露”。
- plugin 能以同一入口接入 registry/discovery，而不是特例接线。

### P5：Transport contract 收敛

目标：HTTP/gRPC transport 依赖 capability contract，不继续依赖具体 facade 或 legacy shell。

已完成：

- capability 包已有 Chat / Runtime / Session / Admin / Cost / HTTP / GRPC contract。
- transport assembly 通过 `ApplicationServices.HTTPTransport()` / `GRPCTransport()` 构造 transport caps。
- transport/server 构造路径没有依赖 `LegacyAPI`，A2A handler 作为 HTTP capability 补充挂载。

已收口补充：

- legacy facade 保留给兼容路径和兼容测试使用，但不作为新 transport 构造入口。
- API handler 只依赖 transport capability，不回穿 application 内部结构。
- route shape 变化前仍必须使用 GitNexus API impact。

完成标准：

- transport 层只知道 capability contract。
- application 内部 assembly result 可替换而不影响 handler 合同。

### P6：R4 收尾验收

必须满足：

- `Build` 不新增领域构造细节。
- 内置 provider、runtime source、discovery/plugin 边界都有名称、manifest 或等价观测面。
- 默认行为、工具集合、启动顺序保持兼容。
- `go test ./...` 通过。
- `gitnexus_detect_changes({scope: "all"})` 只显示预期启动/assembly 影响面，无 HIGH/CRITICAL 被忽略。

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

第十三刀从 builtin tool provider 分组开始：

- `assembleBuiltinTools` 不再直接维护完整内置工具清单，而是按 filesystem / research / planning / knowledge / runtime / git / media / workspace utility 分组成 provider。
- 注册顺序保持不变，`spawn_agent` 与 `skill` 继续作为 runtime wiring 需要的显式 handles 返回。
- 这一步先只建立 provider 边界，不引入配置开关或动态发现语义，避免改变现有工具可用性。

第十四刀从 builtin tool provider 收敛开始：

- provider 实现移动到独立 `builtin_tool_providers.go`，`tool_assembly.go` 只保留装配入口、共享输入和 handle 合并。
- 新增工具装配回归测试，锁住内置工具注册集合和 `spawn_agent` / `skill` handle 映射。
- 后续 coding/browser/media/research 能继续拆成独立 capability provider，而不用再扩大主装配入口。

第十五刀从 tool provider set 开始：

- 默认内置工具清单收敛成 `toolProviderSet`，provider 执行循环从 `assembleBuiltinTools` 中外移。
- `toolProviderSet.Register` 负责顺序执行 provider 并合并 runtime handles，给后续追加动态 provider 留出单一组合点。
- 新增 provider set handle 合并测试，避免后续多个 provider 返回 runtime handle 时互相覆盖。

第十六刀从 provider identity 开始：

- `builtinToolProvider` 增加 `Name()`，默认 provider 清单拥有稳定名称和顺序。
- 新增默认 provider 名称测试，后续配置化、日志观测或动态 provider 注入可以基于 provider identity，而不是依赖匿名类型。
- 当前 `Name()` 只作为边界元数据使用，不改变工具注册行为。

第十七刀从 provider manifest 开始：

- `toolProviderSet.Register` 生成内部 manifest，记录每个 provider 实际新增的工具名。
- `assembledTools` 携带 provider manifest，后续可用于启动观测、配置开关校验或动态 provider 调试。
- manifest 通过 registry 前后差异生成，当前不参与运行时决策，避免改变默认工具集合。

第十八刀从 provider manifest observability 开始：

- 内置工具装配完成后输出 provider manifest 摘要，包括 provider 数、工具总数和每组工具数量。
- 摘要逻辑独立成 helper 并有单元测试，避免启动日志格式散落在装配函数中。
- 日志只用于启动观测，不改变 registry 内容、工具启停或 runtime handles。

第十九刀从 explicit provider registration 开始：

- provider 不再直接调用 `registry.Register`，而是通过 `toolAssemblyInput.Register` 统一注册工具。
- manifest 改为记录 provider 显式注册的工具名，不再依赖 registry 前后差异反推。
- 这为后续检测覆盖、重复注册或动态 provider 调试保留准确的注册意图。

第二十刀从 provider overwrite diagnostics 开始：

- `toolAssemblyInput.Register` 在保留 registry 覆盖语义的前提下记录覆盖注册。
- provider manifest 增加 `Overwrites`，启动摘要输出覆盖总数。
- 新增覆盖诊断测试，后续动态 provider 接入时能看见同名工具覆盖来源。

第二十一刀从 runtime capability source identity 开始：

- `capabilitySource` 增加 `Name()`，skill watcher / MCP / skill promotion 有稳定 source identity。
- `startRuntimeCapabilities` 输出 source 启动摘要，保持原启动顺序和行为不变。
- 新增 source 名称顺序测试，后续 plugin/source 接入时避免打乱现有启动链路。

第二十二刀从 discovery builtin source mapping 开始：

- `AggregatedToolDiscovery` 支持可选 builtin tool source 映射，builtin capability 能暴露 provider 来源。
- `assembleOrchestration` 从 `assembledTools.manifest` 生成 builtin tool -> provider 映射并注入 discovery。
- 旧 discovery 构造路径保持兼容；未提供映射时 builtin source 仍为空。

第二十三刀从 transport contract closure 开始：

- 复查 HTTP/gRPC transport 构造链路，当前通过 `ApplicationServices.HTTPTransport()` / `GRPCTransport()` 输出 capability bundle。
- `server.NewServer` 和 `grpc.NewServer` 接收 transport capability，不依赖 `LegacyAPI`。
- P5 不做 handler 合同改动，避免在 R4 收尾阶段引入 API shape 风险。

## 完成标志

- `Build` 保留依赖顺序和生命周期控制，不继续承载所有领域构造细节。
- LLM / tools / discovery / transports 各自有可命名的 assembly 边界。
- 新能力优先进入 capability/provider 装配面，而不是继续塞进主 builder。

## R4 收尾验收

- `Build` 已收敛为阶段流水线：foundation -> core -> storage -> tools -> engine -> hot-reload -> cron/runtime capabilities -> orchestration -> transports -> app result。
- 内置工具已 provider 化，provider 有名称、manifest、覆盖诊断和启动摘要。
- runtime capability source 已具备 source identity 和启动摘要。
- discovery 已保留 builtin/MCP/skill 来源信息，builtin 来源来自 provider manifest。
- transport 构造依赖 capability bundle，不依赖 legacy facade。
- 默认工具集合、runtime capability 启动顺序和 transport contract 保持兼容。
