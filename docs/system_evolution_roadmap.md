# NGOAgent 总路线图

> **Version**: 5.1
> **Updated**: 2026-04-13
> **Positioning**: NGOAgent 从“强编程助手”演进为“通用 Agent Runtime”的总路线  
> **原则**: 只保留总路线，不展开局部任务树，不用功能清单替代架构方向

---

## 一、核心判断

NGOAgent 当前最重要的问题，不是功能不够多，也不是工具不够强。

真正的问题曾经是：

- runtime 没有彻底成为唯一内核
- intelligence 没有成为统一合同
- orchestration 网络层已经完成基础接线，但仍依赖旧内核承载
- 域能力外化已经完成 closure pass，但还缺少严格边界与观测合同

所以 NGOAgent 的主任务，不应再被描述成“继续补若干功能点”，而应被定义为一条系统级演进路线：

> **先完成内核，再完成合同，再完成编排，再完成外化，最后完成生产化。**

这条路线压缩为五段：

- `R1 内核成形`
- `R2 智能合同化`
- `R3 编排网络化`
- `R4 域能力外化`
- `R5 生产化`

这五段不是并列功能包，而是有明确依赖关系的总路线。

当前实现顺序并不是严格线性推进：`R3` 和 `R4` 的外围接线先于 `R1/R2` 收口完成。这个事实不推翻总路线，只说明外围层可以先整理边界；路线表达仍然必须尊重内核、合同、编排、外化的依赖关系。

---

## 二、总路线

### R1 内核成形

这是 NGOAgent 成为 Agent Runtime 的起点，也是当前唯一硬阻塞阶段。

这一阶段只解决四个问题：

- Graph Runtime 成为唯一主执行内核
- Typed State 成为唯一真相源
- Checkpoint / Resume 成为稳定能力
- Node / Route / Wait / Checkpoint 合同定型

这一阶段完成之前，NGOAgent 仍然只是“带 graph 雏形的产品”，还不是完整意义上的 runtime。

一句话说，`R1` 要解决的是：

> 让 NGOAgent 先真正拥有“执行骨架”。

### R2 智能合同化

当内核稳定后，第二阶段不是继续堆功能，而是把智能行为变成 runtime 原生合同。

这一阶段解决三类问题：

- Structured Output 成为统一能力
- Reflection / Review 成为标准节点
- Planner / Evaluator / Repair 进入统一合同

这一阶段的目标不是“更聪明”，而是：

> 让 intelligence 不再散落在局部模块技巧里，而成为 runtime 可以承载的标准能力。

### R3 编排网络化 ✅ P5 接线完成 (2026-04-11)

第三阶段是把 NGOAgent 从单体运行时推向编排层。

这一阶段解决三类问题：

- Event / Trigger 成为统一入口抽象 ✅ EventBus (channel-based, 256 buffer, 4 workers)
- A2A 成为跨 Agent 协同协议 ✅ Handler + 7 HTTP 路由 (agent.json discovery 免 auth)
- Tool Discovery 成为运行时能力路由的一部分 ✅ AggregatedToolDiscovery (builtin + MCP + skill)

这一阶段完成后，NGOAgent 才不只是“单体 Agent”，而是一个可协同、可委托、可联网编排的系统。

### R4 域能力外化 ✅ completed (2026-04-13)

第四阶段要解决的是框架与产品的边界。

这一阶段的核心动作不是“加插件目录”，而是：

- runtime 与 app/assembly 拆层
- coding/browser/media/research 等能力从核心中外移
- builder 不再承担总装配器角
色

这一阶段完成后，NGOAgent 才能从“编程助手本体”转为“runtime + plugins”。

### R5 生产化

最后一阶段才是生产化。

包括但不限于：

- telemetry
- health / readiness
- rate limiting
- deployment packaging
- tenant isolation

这一步必须后置，因为在内核、合同、编排、插件边界都不稳定时，生产化只会放大混乱。

---

## 三、阶段依赖关系

这五段的逻辑依赖关系是单向的：

`R1 -> R2 -> R3 -> R4 -> R5`

但当前实现顺序已经不是严格线性推进：`R3` 和 `R4` 的外围接线先于 `R1/R2` 收口完成。这不是路线错误，而是说明外围层可以先整理边界；真正的主阻塞仍然是 `R1` 内核所有权和 `R2` 智能合同化。

当前实际状态证明：

- `R3` 可以先完成 EventBus / A2A / AggregatedToolDiscovery 的外围接线
- `R4` 可以先完成 provider/source/discovery 边界和 assembly 收口
- 但 `R3/R4` 的完成不能反向证明 `R1/R2` 已经完成
- 只要 `AgentLoop` 仍是主执行路径，graph runtime 就还不是唯一内核
- 只要 reflection/evaluation/planner 仍未统一为 graph node 合同，R2 就还没有收口

所以依赖关系仍然成立：

- `R1` 没完成，`R2` 会失去承载面
- `R2` 没完成，`R3` 只能编排脆弱行为
- `R3` 没完成，`R4` 只会做出静态插件目录，而不是可编排插件体系
- `R4` 没完成，`R5` 只能把一个边界混乱的系统包装上线

现在的风险不是 `R3/R4` 没做，而是它们已经先做完一部分外围能力，随后必须回到 `R1/R2`，否则系统会长期保持“外层清楚、内核双轨”的状态。

所以 NGOAgent 不适合再写成“多条路线同时推进的功能矩阵”。

更合理的表达只能是：

> 这是一个由内向外展开的系统级演进序列。

---

## 四、当前所处位置

当前 NGOAgent 已完成 `R1-R4` 的主线收口。

当前准确位置是：

> **R1/R2 内核与智能合同已经完成，R3/R4 编排与外化边界也已定型。**

阶段状态如下：

| 阶段 | 当前状态 | 判断依据 |
| --- | --- | --- |
| `R1 内核成形` | ✅ completed | Graph Runtime 已是唯一执行主链；graph node 统一经 node service 持有语义；graph runtime 主路径已禁止重新回流到 `AgentLoop.handle* / do*` |
| `R2 智能合同化` | ✅ completed | `DecisionContractState` 已进入 `TurnState.Intelligence.Decision`；planning/reflection/evaluation 已写入统一 contract；runtime projection 优先读取显式 contract，并保留兼容 fallback |
| `R3 编排网络化` | ✅ P5 接线完成 | EventBus、A2A 7 路由、AggregatedToolDiscovery 三来源聚合已接线 |
| `R4 域能力外化` | ✅ completed | Builder 流水线、tool providers、capability source、discovery 边界已经收口；transport 已以 capability bundle 为主合同，legacy facade 退回兼容壳 |
| `R5 生产化` | ⚠️ 只能局部推进 | health/readiness 等生产化能力可以补，但不能替代 R1/R2 收口 |

原因是：

- Graph Runtime 已成为唯一执行骨架
- 用户消息主控制流已经进入 Graph Runtime
- graph node wrapper 已经统一为 node service 持有节点语义
- tool_exec、prepare、generate、guard/compact/repair/done 等节点语义已经从旧 loop wrapper 收口
- Checkpoint / Resume 主链路已接上
- Typed State 与 Intelligence decision contract 已成为运行时真相源的一部分
- R3/R4 的编排与外化边界已经定型

所以当前不是“从 0 开始做 runtime”，而是：

> **在完成 `R1-R4` 后，把重心转入 `R5`。**

---

## 五、每一阶段的完成标志

### R1 完成标志

- graph runtime 成为唯一主执行路径
- runtime state 成为执行语义唯一真相源
- checkpoint/resume 可以稳定保留运行意图
- node/route/wait/checkpoint 语义不再依赖旧 loop 心智

### R2 完成标志

- structured output 成为 runtime 标准能力
- reflection 成为标准 graph node
- planner/evaluator/repair 不再各写各的局部逻辑

### R3 完成标志

- event trigger 成为统一入口层
- A2A 具备基础协议能力
- tool discovery 成为编排层能力而不是手工拼装

### R4 完成标志

- coding 不再定义系统内核
- plugin 成为明确边界，而不是目录概念
- core/runtime 可以在不绑定 coding 假设下运行

### R5 完成标志

- 系统具备可观测、可部署、可隔离、可运维的生产形态

---

## 六、路线边界

为了保持路线纯度，以下内容不应再主导总路线表达：

- 单个工具能力增强
- coding 专属功能项平铺
- 局部模块优化
- 孤立的 UX 改善
- 不依附于阶段目标的点状能力扩充

这些都可以做，但它们不应该再被写成 NGOAgent 的主路线。

主路线只回答一个问题：

> NGOAgent 如何从“强产品”进化为“强 runtime”。

---

## 七、一句话版本

NGOAgent 的总路线不是“继续补功能”，而是：

> **先把内核做实，再把智能合同化，再把编排做成网络层，再把域能力外化成插件，最后再做生产化。**

如果只保留一句战略判断，那就是：

> **当前全部工作的中心是守住 `R1-R4` 已完成的统一 runtime 心智，并把重心推进到 `R5 生产化`。**

---

## 八、为什么当前转入 R5

很多看起来“也很重要”的方向，比如：

- 更强的 planner
- 更丰富的 tool discovery
- 更完整的 A2A
- 更彻底的 plugin 体系
- 更成熟的 observability

现在都可以推进，但必须建立在已完成的 `R1-R4` 之上。

原因不是这些方向突然变重要了，而是它们终于拥有了统一前提：

> 系统已经具备稳定、统一、可恢复的执行内核与明确边界。

因此当前阶段最需要避免的，不是“继续收口 R1”，而是：

> **在 `R1-R4` 已定型后，又重新引回旧 loop 心智、兼容壳主导心智或边界混用。**

当前更合理的推进顺序是：

- observability / telemetry
- health / readiness
- rate limiting
- deployment packaging
- tenant / isolation 边界

也就是说，`R5` 现在不是“越级插队”，而是这条路线的自然下一段。

---

## 九、R1 的已满足收口标准

`R1` 完成，不等于“graph runtime 已经能跑”。

本阶段已经满足的判断包括：

### 1. 执行归一

系统不再同时维护“两套主执行心智”。

换句话说：

- graph 不是旁路能力
- 旧 loop 不再定义最终执行语义
- 节点推进、等待、恢复、结束，都由 runtime 合同统一表达

### 2. 状态归一

系统不再依赖“对象字段 + 临时局部变量 + 隐式上下文”的混合状态模式来维持运行。

换句话说：

- runtime state 能表达运行中的关键语义
- resume 恢复的是执行现场，而不只是历史消息
- 节点之间传递的是显式状态，而不是约定俗成的内存假设

### 3. 恢复归一

checkpoint / resume 不再只是“能继续”，而是“按相同语义继续”。

这意味着恢复之后：

- route 判断不漂移
- wait reason 不漂移
- approval / barrier / wake 语义不漂移
- tool 选择约束与 continuation 意图不丢失

### 4. 合同归一

Node / Route / Wait / Checkpoint 必须成为稳定合同，而不是实现细节的投影。

也就是说：

- node result 不是随实现习惯不断变形的结构
- route 不是旧状态机分支名的别名
- wait 不是“先停一下”的临时描述
- checkpoint 不是“顺手存一下现场”的副产品

### 5. 认知归一

团队在讨论系统时，已经自然使用 runtime 语言，而不再主要使用旧 loop 语言。

当以下表达开始自然成立时，才说明 `R1` 真的收口：

- “这个节点应该返回什么 route”
- “这个 wait 由什么事件唤醒”
- “这个状态是否应进入 checkpoint”
- “这段逻辑属于 node contract 还是 runtime state”

如果大家重新开始主要在说：

- “这个阶段切哪个 state”
- “这个字段临时塞到 loop 上”
- “先在 handler 里绕过去”

那么说明系统正在回退到旧心智，而不是继续建立在已完成的 `R1` 之上。

---

## 十、这份路线图如何指导日常决策

这份文档不是需求池，也不是季度任务表。

它的用途只有三个：

### 1. 用来判断优先级

当两个任务都看起来重要时，先问：

> 它是在巩固已完成的 `R1-R4`，还是在给未定型的 `R5` 提前加重量？

凡是前者，优先级更高。

### 2. 用来判断是否偏题

一个改动即使本身有价值，只要它：

- 不推动当前阶段的完成标志
- 反而增加新的耦合面
- 提前固化后续阶段的边界

那它就不属于当前主路线。

### 3. 用来判断“完成”是什么意思

NGOAgent 后续每完成一个阶段，都不应以“功能数量”宣布完成，而应以：

- 是否完成本阶段的结构性目标
- 是否减少系统核心歧义
- 是否为下一阶段建立稳定承载面

来判断是否真正完成。

---

## 十一、当前阶段的工作口径

在 `R1-R4` 完成之后，所有工作都应该尽量落到同一个口径上：

> **凡是不能增强 production readiness，同时又会破坏 runtime 统一性、状态统一性、恢复统一性、合同统一性的改动，都不应抢占主线。**

这不意味着别的事完全不能做。

更准确地说：

- 可以做必要修复，但不要借修复扩张边界
- 可以做局部增强，但不要让增强反向固化旧模型
- 可以做外围能力，但不要让外围能力反向破坏已定型的内核与合同边界

这就是当前阶段最重要的执行纪律。

---

## 十二、结论

NGOAgent 已经不是一个从零开始的系统。

它已经拥有足够多的能力、足够厚的产品表层、足够复杂的行为层。

因此下一步不是“继续长更多东西”，而是：

> **在已完成的 runtime 基础上，把系统推进到 production 形态。**

只有完成这一步，后续的 intelligence、orchestration、plugins、production 才不会继续建立在混合心智和分裂边界之上。

所以这份路线图最终只表达一个判断：

> **`R1-R4` 已完成，后续工作必须建立在这套统一 runtime 心智之上。**
