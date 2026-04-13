# NGOAgent 深度内核源码级架构剖析全书

*本指南逐行穿透核心源码实现，深入解析 NGOAgent 作为生产级智能体框架的内部运转逻辑与并发防御机制。*

## 全书目录
1. [Phase 1: 核心结构与请求入口机制 (Entry & Core Data)](#phase-1-核心结构与请求入口机制-entry--core-data)
2. [Phase 2: Agent 重心脏机与 ReAct 调度主循环 (The State Machine)](#phase-2-agent-重心脏机与-react-调度主循环-the-state-machine)
3. [Phase 3: 上下文拼装、注入与防御性压缩 (Context & Compression)](#phase-3-上下文拼装注入与防御性压缩-context--compression)
4. [Phase 4: 工作流护栏与安全性沙盒 (Behavior Guard & Security)](#phase-4-工作流护栏与安全性沙盒-behavior-guard--security)
5. [Phase 5: 工具路由并发与事件回调系统 (Execution & Ephemeral Routing)](#phase-5-工具路由并发与事件回调系统-execution--ephemeral-routing)

---

## Phase 1: 核心结构与请求入口机制 (Entry & Core Data)

此阶段探讨整个应用程序从启动、依赖注入到用户请求如何最终落地到单一调度循环的源码级过程。

### 1. 启动装配线: `internal/application/builder.go`
NGOAgent 在应用层的入口完全由 `builder.go` 负责装载。有别于扁平的初始化，它实现了高度解耦的 **8 个装载阶段 (Phases)** 设计：

- **Phase 1-3 (资源层)**：数据库持久化、大语言模型路由(`llm.Router`)注册、及上下文层(BrainStore / KnowledgeStore / Memory Vector Index) 的注入准备。
- **Phase 4 (核心设施层)**：`ToolRegistry` 的注册。有趣的是，这里通过 `agentSearchURLB` 将网络搜索和深度检索逻辑(DeepResearch)代理给外部统一端点。
- **Phase 5 (调度引擎组装)**：核心代码 `hookChain.Add(...)`，加载了状态分离机制，如知识萃取(`KIDistillHook`) 和 会话摘要(`DiaryHook`)。最为核心的是挂载了 `spawnTool`，用于处理 Subagent 异步分化的并存态，这里采用了 `Service.NewSubagentBarrier` 处理子代返回结果的聚合唤醒（后有专章详解）。
- **Phase 6-8 (网络与动态配置层)**：基于 `cfgMgr.Subscribe(...)` 启用配置文件热更新重载(Hot-Reload) 且不断电，完成 `cron`、`mcp` 以及 `AgentAPI` HTTP/gRPC 网关的实例化。

### 2. 网关控制阵列: `internal/application/api.go` 的 `AgentAPI`
`AgentAPI` 是所有外围网络请求进入底层 Engine 的唯一单例总闸。
核心调用入口是 `ChatStream(...)`。它并没有把每一个请求扔进新线程，而是利用了以下防护：

```go
// 从 Session 池中提取长驻环境
loop := a.loopPool.Get(sessionID)

// [机制保护] 基于原子级别的排他锁，强防 "兵分两路思维踩踏"
if !loop.TryAcquire() {
	return ErrBusy
}
defer loop.ReleaseAcquire()
```
`TryAcquire()` 会尝试夺取底层 `AgentLoop` 里的专有 `runMu` 锁。如果用户在此刻疯狂重复提交，系统直接弹回强硬的 `ErrBusy` (Agent is busy)，杜绝并发 LLM 流对同一片 Workspace 代码上下文造成的混乱读写。

此外，该接口利用 `a.histQuery.LoadAll(sessionID)` 处理了服务器热重启后的短路恢复，其中不仅组装原始对话历史，也借由 `RebuildContentParts` 把跨周期的**多模态附件 (Image/Audio)** 的视觉句柄给完整拼装回来。

### 3. 多路复用缓存淘汰: `internal/domain/service/loop_pool.go`
当支持多客户端通过 Telegram 或 Web 入口时，长驻的上下文大块对象不能无节操疯涨。
- **`LoopPool` 设计**：内部存储维护 `map[string]*managedLoop`，基于 `sync.RWMutex` 实现线程安全的读写。
- **双重限制阈值**：系统设置拥有 `maxLoops = 8` (全局最大活动线程上限) 和 `perUserMax = 3` (单用户活动上限)。
- **淘汰逻辑 (LRU)**：当阈值越界时（例如用户发起第 4 个 Session 时），`evictUserOldestLocked` 优先杀死当前用户下最古老的 **Idle(空闲状态)** 线程，强制其 `Stop()` 以回收内存；若是全局线程池满载，则借助 `evictOldestLocked` 清理无人问津的长尾上下文。这种双层级 LRU 算法是保证廉价云主机承载中型用户群集不 OOM 的关键。

### 4. 骨相领域实体: `internal/domain/entity/entity.go`
在这里，所有实体全部斩断了对于底层持久化细节(JSON/Gorm)的外骨骼依赖。
- **`Conversation` / `Message`**：其中 `Message` 引入了 `Reasoning` 字段以支持 DeepSeek 等带有思考流的新一代推理模型标准。
- **`Skill` (能力描述体)**：不再仅仅存储插件脚本，还通过自动解析打上了 `light` (直接内联执行) / `heavy` (外部进程挂载) 权重标，内部携带近期特为 RAG 设计的 `Category` 与 `KIRef` 属性来做技能树寻址定位优化。

---

## Phase 2: Agent 重心脏机与 ReAct 调度主循环 (The State Machine)

在接管用户的 `ChatStream` 请求后，系统并未采用常规的线性控制流处理，而是将其拆解为了具备确切生命周期的 10 阶段状态机 (`state.go`)，并将底层循环实现在核心结构 `AgentLoop` (定义于 `loop.go` 和 `run.go`) 中。

### 1. 结构大盘: `AgentLoop` 隔离锁机制 (`loop.go`)
`AgentLoop` 是隔离单次会话请求生命周期的状态堡垒，内置多路监听器与多重锁机制保护核心字段：
- **`mu sync.Mutex`**: 用户保护轻快数据结构的并发读写，如代理状态 (`state`)、对话历史 (`history`)、任务统计记录 (`task` 包含步骤计数与边界状态记录)、以及用于拦截系统短暂停驻的消息 `ephemerals`。
- **`runMu sync.Mutex`**: 绝对严格的排他防御门。配合 `TryAcquire()` 防止对同一个请求入口的多点敲击引起的并行执行，也就是防御通常 Agent 架构极易触发的“僵尸分支踩踏”情况。
- **内部辅机部件**：同时装载着 `BehaviorGuard`（行为守护网关，下一章详解）、`PhaseDetector`（探针判断代码正在编纂还是在验收测试）及利用闲置 CPU 去构建向量碎片的 `DreamTask`。

### 2. 确切流转：10 态定义图谱 (`state.go`)
这里摒弃了黑盒式的堆叠代码，用清晰的状态集接管进程，让哪怕一次极端的强制终止都能追溯断点。核心流转包含：
1. **`StateIdle` (0)**: 空闲。系统进入长效待机，触发后置钩子如 `KIDistillHook` 的异步蒸馏，也唤醒 `DreamTask` 在后台默默补齐分析文档块。
2. **`StatePrepare` (1)**: 依据 PromptEngine 建立最新的长文本 System Prompt 注入环境。
3. **`StateGenerate` (2)**: 跨出对端发起 LLM 请求，解析出 Content与 提取多个并存的 `ToolCall` 指令集。
4. **`StateToolExec` (3)**: 实施具体代理行径。
5. **`StateGuardCheck` (4)**: **[极其关键的流转站]**。每一次沙盒执行完毕都会折返该节点，校验请求有没有超限（超 Steps），有没有处于低智能复读循环阶段，及**预估 Token**是否达到危险峰值需要分流到强制压缩态。
6. **`StateCompact` (5)**: 接到流转命令启动三梯次精兵简政策略。
7. **`StateError` / `StateFatal`**: 分级重试和异常挂起。
8. **`StateDone` (8)**: 单回旋结束点，写入底层 Snapshot。

### 3. 主循环拦截、退避与重试 (`run.go`)
`runInner` 借助 `for {}` 引擎驱动全机运作。核心特性如下：
- **退避阻断 (Backoff with Jitter)**: `doGenerate` 接到 `llm.ErrorOverload` 或 `ErrorTransient` 后触发重试栈计算。如果在限定重试 2 次后仍败北，执行**智能驱逐策略 (Phase 4 Failover)**，把此故障节点扔进 `excludedProviders` 中并且清空计数器，由 `llm.Router` 去寻找下一个健康模型厂牌发起平滑接力，前端零感知。
- **暴力拆解上下文 (PTL Retry)**: 对于遇到极端恶劣报错 `ErrorContextOverflow` 的流，先利用 `StateCompact` 把中间啰嗦的长 Tool 结果抽干。若仍然不能平复，直接呼唤最后防线 `forceTruncate(6)` 一脚将大部分对话踢入垃圾堆后强行推进，防止大段长文导致全会话当机罢工。
- **协议同步底层控制**: 方法尾部的 `syncLoopState` 尤为精巧。当 ToolExec 感知到模型派发了特定的计划任务，它直接借道共用指针在 `a.guard.SetForceToolName("notify_user")` 中硬编码拦截，这导致大语言模型无论多聪慧，在下一次生成中都必须百分百调用向宿主沟通汇报的 `notify_user` 函数，形成强因果锁链。

---

## Phase 3: 上下文拼装、注入与防御性压缩 (Context & Compression)

在此阶段，我们将关注负责 LLM 对话窗口生命周期管理的模块，核心源码由 `prepare.go`, `run_helpers.go` 以及 `compact.go` 承载。这是保障大模型长效运行不崩溃及保持敏锐指令跟随能力的中枢神经。

### 1. 四层动态按预算注入机制 (`prepare.go`)
当引擎进入 prepare 节点（历史实现名为 `doPrepare`）时，它将针对系统的 `ephemerals`（临时语境提示词）执行基于 `TokenEst` (Token预算) 的注入判断：
- **第一层 (核心策略层)**：判断进入自主权 Agentic 模式时注入策略性语句如：`"For complex, multi-step... use task_boundary..."`；亦或是协调团队并行的子代调度指引 (Leader Prompt)。
- **第二层 (边界与汇报频控层)**：检测到 `StepsSinceUpdate >= 5` 时，高优注入逼迫大语言模型进行停顿汇报的标语 `<ephemeral_message>`。
- **第三层 (人工计划与预警层)**：结合当前 `ArtifactLastStep` 的陈旧程度抛出任务进度落后提醒，以及结合动态 Token 探测发现超标（60%及以上），下发自我删减提醒。
- **第四层 (RAG及技能库提示层)**：每 8 个 Step 静默推送向量库中的关键知识 (`GenerateKIIndex`) 以及根据触发加载本地 `SKILL.md` 指令说明。

最终，所有的组装文本全部流转到 `SelectWithBudget()` 进行依优先级过滤。当上下文处于极度紧缺状态（`pct > 80`）时，分配给这些伴随指令的配额直接对半砍，死保核心逻辑空间。

### 2. 懒加载调控与工具分层机制 (`run_helpers.go`)
当系统内部配置了多达几十个重型插件和控制工具时，为了节约高达数千字的 Function Calling 元数据耗费，核心引擎对于它们执行了**层级式懒加载 (Lazy Loading)**：
- **Tier 0 (系统核心基底)**：无条件必须下发系统管理级大门，包含 `read_file`, `write_file`, `task_boundary`, `run_command` 等 15 款核心护身符系统。
- **Tier 1 (外围搜索探针)**: 借由粗糙的语料扫描，仅仅当用户问题中包含了 `search, find, look, url` 时才将 `web_search / deep_research` 脱水释出加入。
- **Tier 2 (巨型复杂调用操作)**: 如重型记忆存库 `save_memory`（`save_knowledge` 仅为兼容旧名）, 多模特征提取 `view_media`, 和代码版本回档工具 `git_*`。命中关键字后加载挂载体。
**[突破限制策略]**: 仅仅在聊天的首次启动期控制，当任务超过两步 LLM 基本已经明晰意图并确立主逻辑场，此后系统将全景 ToolDef 完整吐给它，这是一种“前轻后重”的极致推算。

### 3. 三重长文压缩与灾难恢复法界 (`compact.go`)
`compact.go` 源码全盘包揽了避免模型丧失注意力机制的各种解法组合，包含：

- **(1) 宏观摘要重塑 (`doCompact`)**
底层系统通过密度探测，抽离中间的长段杂波。引入了一套 **7D 分析提示架构 (7-dimensional checkpoint)**：要求模型严格产出 `<analysis>` 草稿反思出 `user_intent`, `learned_facts`, `errors_and_fixes` 等至少 7 个核心维度的结构文并直接置换过去的长文记录，同时它还拥刺一个强大的伴随技 `{extractRecentFiles}` 记录并把修剪前阅读的路径放回最底部提醒 AI。
- **(2) 工具病态过载清除 (`toolHeavyCompact`)**
嗅探流转中被 `Tool` 长文本撑爆了整体空间 >60% 份额的回调，执行超重型减负：切去一切过长（>10KB）终端反馈内容中除了头 500 个字符与尾端 1500 字符内文，将无意义海量控制台输出进行硬剪。
- **(3) 分形梯度注入剥离 (`processToolResult`)**
基于控制台文字的总大小定义了处理分流：`< 2K` 直送喂进缓存； `< 32K` 带头描述注射进模型窗口； 当爆发巨量文字 `> 32K` 会将冗长信息外挂到外部磁盘 `/tmp` 文件中并通过文件引址发给代理让其后续自行动手捞取查阅。这一连串手段使得 NGOAgent 在持续作战百步以上仍能在上下文游刃有余。

---

## Phase 4: 工作流护栏与安全性沙盒 (Behavior Guard & Security)

NGOAgent 使用双层拦截网，除了基础设施层对文件级和命令级的物理隔离外，核心引擎通过 `guard.go` 中的 `BehaviorGuard` 建立了一张抵御“幻觉死循环”的逻辑大网。它分段驻守在每轮对话（Turn-level）和每个工具调用（Step-level）之中。

### 1. 响应生命周期级拦截 (Turn-Level Check)
大模型在完成一次 `Generate` 生成后，不管带有多少工具指令，都会先被 `guard.Check` 进行过滤：
- **纯粹幻觉与空白 (empty_response)**: 若不输出文字且未调用任何工具，尝试下发警告，若连续 3 轮如此，直接剥夺执行权强行终止（Terminate）。
- **复读机坠落拦截 (Repetition & Jaccard)**: 内部缓存最后 5 次的大模型全量主回复。若遇到前后输出完全一致（Exact Match），第一次严厉警告并自动下挂 `_loop_breaker` 救援套件说明；若连续 3 次依然一致立即拔掉电源挂断。如果不是精确相同而是轻微修改（Near-repeat），统计算法引入了 N-gram 级别的 Jaccard 相似度探测因子，一旦相似度 > 85%，触发高危警告迫使其换路执行。
- **死结环路检测 (Tool Cycle Detection)**: 在近期的 10 个操作中，若探测到了规律模式长度在 2-4 的子序列打转（如 A→B→A→B的无限翻抽），模型往往自我麻痹毫无知觉，引擎此时会刺破壁垒，强制给进 `_stuck_recovery` 救援锦囊（要大模型去尝试搜索新文件重置结构）。
- **总控安全阀 (step_limit)**: 一旦超出沙盒设置上限 MaxSteps (通常为 200 步)，判定为逻辑暴走，无条件摘停，避免拖垮后道集群。

### 2. 工具执行海关仪 (Step-Level Pre-Tool Check)
Step-level 是更为底层的高频海关验证器：
- **越权越轨阻杀 (Rule 6 - Planning Violation)**: 如果 Agent 现处 `Planning` 规划态内，但是文件系统中并不存在一份名为 `plan.md` 的纲领文件，且它在这时尝试越轨调用产生修改外界影响的动作（如 `write_file`, `run_command`），`BehaviorGuard` 将毫不犹豫返回 `Block` 状态将其物理隔绝，且给到模型大字报错误。
- **实操偷跑警告 (Rule 8 - Execution Violation)**: 在进行代码爆破的 `Execution` 执行态时，如果没有按规定建立工单节点日志 `task.md` 便发起了文件编配，模型会收到黄牌。
- **协议切断 (Rule 7)**: 若模型发函了 `notify_user`，却并未老实等待转交权标，再次在接下来的派发单带了其他工具，预查机制会即刻拦停。

### 3. 多模态物理沙盒机制 (Sandbox)
虽然此处核心谈论的是逻辑沙盒，但 `builder.go` 阶段就植入的 `sandbox.NewManager(workspaceDir)` 被作为命令环境的外部隔离防线封装。所有的终端执行（Terminal Commands）不越雷池地受限在这层工作区，与 `guard.go` 一齐构筑起了“内防智障，外防破坏”的安全基建双塔。

---

## Phase 5: 工具路由并发与事件回调系统 (Execution & Ephemeral Routing)

在解析到大模型的 `tool_calls` 请求后，系统并未采用简单的 `for` 循环串行执行，而是通过 `tool_exec.go` 引入了细粒度的并发控制和多重拦截层，从而大幅榨取了系统闲置算力与提升响应时效。

### 1. 动静分离的并发调度策略 (Mixed Batch Splitting)
在真正进入执行环节前，引擎会运行 `splitToolCalls()` 进行工具属性判定。
系统将本轮的所有工具请求拆分为 **“无副作用组” (ReadOnly/Network)** 和 **“写操作副作用组” (Write)**：
- 对于没有任何修改的查询工具组（如跨目录列举文件、向并行的外网发起搜索请求），系统直接采用 `execToolsConcurrent` 转派给内部 Goroutines 列阵同时狂飙，并通过 `sync.WaitGroup` 收集整合。
- 对于涉及覆写工作区和调兵遣将这类危险写操作，系统严格降级到 `execToolsSerial`，按时序一步步稳健落实。
两种模式下都对 `ErrApprovalDenied` (人类拒绝审批) 设计了熔断通道，一旦遇阻直接砍断队列。

### 2. 超级管线执行路由 (The `doToolExec` Pipeline)
这是一个囊括了追踪、防御、拦截、通知于一身的闭环流转通道：
1. **探针打点 (PhaseDetector)**: 推送执行记录给模式探测仪判定当前任务是处于摸索初期还是到了收尾期。
2. **海关守卫 (PreToolCheck & SecurityHook)**: 先由上文提到的 Logic Guard 处理工作流违规，然后再由底层外挂的安全网（可能是独立的小模型 Classifier）鉴定是否属于恶意攻击软阻断。
3. **插件拦截器 (BeforeTool Hook)**: 供拓展层深度改变参数或者提前 `Skip`。
4. **共享沙盘挂载 (BrainStore Injection)**: 借由 `brain.ContextWithBrainStore(ctx)`，将持有 `SessionID` 及当前物理落盘目录的内存沙盘上下文塞入执行栈，供子模块随存随取大内存文件。
5. **防爆重置盾 (safeToolExec)**: 即便是内置插件代码意外产生了内存越界溢出，此层包裹的 `recover()` 会将 Panic 硬转换为带堆栈信息的字符串反馈给大语言模型，阻止引擎核心主循环心搏停止。
6. **阶梯消像切割 (processToolResult)**: 使用三档式容量消减术限制超长命令行输出的吞噬。
7. **广播放大 (Webhook & Evo Trace)**: 对外部广播执行事件（钉钉、企业微信通知），并收集 Evo 高频度日志。
8. **跨界通信同传 (Protocol Dispatch & State Sync)**: 当工具（比如 `task_boundary` 或者自定义 `tool`) 想命令宿主干预下一次行为路线时（如变更状态模式、索取视觉文件），结果会透过共享指针的 `LoopState` 更新，被 `syncLoopState` 注射回大模型上下文共享池，达成 "引擎 → 工具 → 引擎" 神经交互环绕。
