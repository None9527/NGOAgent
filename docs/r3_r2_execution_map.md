# R3 + R2 并轨执行图

> Updated: 2026-04-11
> Status: Phase 5 接线完成 ✅
> Strategy: 以 `R3 编排网络化` 为主线推进，同时在执行过程中收敛 `R2` 剩余的 decision / schema / resume 合同。

---

## 目标

本阶段的目标不是继续补单点功能，而是把 NGOAgent 从单 run graph 推进成可恢复、可编排、可观测的运行网络。

同时，所有仍然停留在 `R2` 的局部智能合同债，必须在 `R3` 主线中并轨收掉，而不是单独再开一轮整理。

---

## 总图

### Phase 1：编排模型定型

定义四类一等对象：

- `RunGraph`
- `DecisionContract`
- `OrchestrationState`
- `OrchestrationEvent`

这一步的产出不是行为增强，而是让 runtime/snapshot 能表达 parent-child-barrier-handoff 关系。

### Phase 2：Graph 并轨

把编排节点放进主 graph：

- `orchestrate`
- `spawn`
- `barrier_wait`
- `merge`

目标是让后续的 subagent/barrier 行为有正式 node 承载面，而不是继续依附在 loop 边缘逻辑上。

### Phase 3：Decision 合同统一

把三类 decision 并成同一种合同：

- `planning`
- `reflection`
- `evaluation`

统一字段：

- `schema`
- `decision`
- `reason`
- `feedback`
- `applied_at`
- `resume_action`

### Phase 4：持久化并轨

让数据库承载编排网络的最小真相：

- parent run / child run
- barrier relation
- wake source
- handoff payload
- orchestration event

### Phase 5：API 并轨

把外部入口收成正式 orchestration API：

- `ReviewPlan`
- `ApplyDecision`
- `ResumeRun`
- `ListPendingRuns`
- `ListChildRuns`

---

## 实施顺序

1. 先把 orchestration state 和 graph node 骨架进入 runtime
2. 再把 decision contract 抽象统一
3. 再把 barrier/subagent 并进 graph 主链
4. 再做 persistence 和 API 的正式承接

---

## 完成标志

- parent / child / barrier 不再只是 loop 侧结构，而是 graph/runtime 一等对象
- decision wait / apply / resume 不再为每一类智能行为分别实现
- reconnect 可以解释当前等待的是 approval、plan review、barrier 还是 orchestration merge
- runtime snapshot 可以恢复单 run 以外的编排关系
- API 能显式操作 pending decision 和 pending run，而不是靠空消息探测
