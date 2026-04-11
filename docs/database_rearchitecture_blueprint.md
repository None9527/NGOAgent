# NGOAgent 数据重构蓝图 v1

> 目标：一次性建立可持续的数据架构，支持 `R2-R5` 持续演进，而不是在现有持久化层上反复修补。

---

## 1. 结论

当前数据库实现不适合继续作为长期架构基础直接扩展。

原因不是“SQLite 不行”，而是当前数据边界没有被正确建模：

- 业务真相数据、运行现场数据、分析/实验数据混放
- 数据完整性主要依赖应用代码，没有数据库级约束
- 迁移策略分散在各 `NewXxxStore()` 的 `AutoMigrate`
- `run_snapshot` 被当成恢复关键数据，但又采用宽松 JSON 模型，没有语义版本管理
- 删除链不完整，`run_snapshot` 会残留孤儿记录

因此，本蓝图的方向不是“全部推倒重做”，而是：

1. 保留主业务数据主线
2. 重建运行态持久化边界
3. 重建分析态持久化边界
4. 引入统一 migration / compatibility / retention 体系

---

## 2. 设计目标

新的数据架构必须同时满足以下约束：

1. 用户可感知数据稳定
   例如会话、消息、知识、产物，不因 runtime 演进频繁变更。

2. 运行恢复语义正确
   `checkpoint / resume / approval / barrier / reconnect` 依赖的数据必须可验证、可版本化。

3. 分析与实验数据可独立演进
   `evo trace / transcript / token usage` 不应绑死主业务库。

4. 迁移可控
   所有 schema 和数据升级通过统一 migration runner 执行。

5. 生命周期清晰
   每类数据都明确是长期保留、短期缓存、还是定期归档/清理。

---

## 3. 核心原则

### 3.1 三层数据分域

系统数据按职责分为三层：

1. Core Data
   业务真相层。用户长期关心的数据。

2. Runtime Data
   运行现场层。只为执行恢复服务。

3. Analytics Data
   分析演化层。可重建、可归档、非关键路径。

### 3.2 结构化优先，JSON 只用于正确的地方

允许 JSON 的地方：

- checkpoint state
- wait payload
- event payload
- 少量扩展 metadata

不允许 JSON 承担主业务关系：

- 消息主模型
- 附件关系
- 工具调用关系
- 关键索引字段
- 关键一致性字段

### 3.3 业务真相与运行现场强分离

禁止以下反模式：

- 用历史消息推断运行现场
- 用运行快照替代主业务记录
- 把恢复所需字段塞回 conversation/history 表

### 3.4 恢复合同必须版本化

每个可恢复运行必须同时具备：

- `graph_id`
- `graph_version`
- `runtime_schema_version`

恢复逻辑必须判断：

- 是否可反序列化
- 是否语义兼容
- 是否允许降级恢复

---

## 4. 目标数据架构

即使物理上仍使用一个 SQLite 文件，也必须逻辑上拆成三层。

### 4.1 Core Data

#### `conversations`

会话主实体。

建议字段：

- `id`
- `channel`
- `title`
- `status`
- `created_at`
- `updated_at`
- `archived_at`
- `metadata_json`

说明：

- `metadata_json` 只放低风险扩展字段
- 不放运行态
- 不放分析态

#### `messages`

规范消息表，替代当前“一个表混合文本、工具、附件、推理”的弱模型。

建议字段：

- `id`
- `conversation_id`
- `seq`
- `role`
- `message_type`
- `content_text`
- `reasoning_text`
- `tool_call_id`
- `parent_message_id`
- `created_at`

约束：

- `UNIQUE(conversation_id, seq)`
- `FK(conversation_id -> conversations.id)`
- `FK(parent_message_id -> messages.id)`

#### `message_tool_calls`

单独建表表达 assistant 发起的工具调用。

建议字段：

- `id`
- `message_id`
- `call_id`
- `tool_name`
- `args_json`
- `created_at`

#### `message_attachments`

单独表达多模态附件和外部引用。

建议字段：

- `id`
- `message_id`
- `attachment_type`
- `path`
- `mime_type`
- `display_name`
- `metadata_json`

#### `artifacts`

会话级结构化产物表，承接 `plan/task/brain note/scratchpad`。

建议字段：

- `id`
- `conversation_id`
- `kind`
- `path`
- `version`
- `content_hash`
- `updated_at`

用途：

- 让 `R2` 的 planner / reflection / protocol contract 有稳定实体可依赖

### 4.2 Runtime Data

#### `agent_runs`

一次执行的主记录。

建议字段：

- `id`
- `conversation_id`
- `parent_run_id`
- `entry_type`
- `status`
- `current_node`
- `current_route`
- `wait_reason`
- `graph_id`
- `graph_version`
- `runtime_schema_version`
- `started_at`
- `updated_at`
- `finished_at`

说明：

- `entry_type` 例如 `chat / retry / reconnect / subagent`
- 这是恢复域的主索引表，不再靠扫描 JSON 定位 run

#### `run_checkpoints`

可恢复检查点表。

建议字段：

- `id`
- `run_id`
- `checkpoint_no`
- `status`
- `runtime_schema_version`
- `turn_state_json`
- `execution_state_json`
- `created_at`

策略：

- latest 用于恢复
- 最近 N 个用于调试/回溯
- wait/completion/failure 节点强制保存

#### `run_waits`

显式等待态表。

建议字段：

- `id`
- `run_id`
- `wait_type`
- `status`
- `payload_json`
- `created_at`

---

## 5. Runtime 切换策略

`run_snapshot_records` 已退出主合同。当前 runtime 恢复只依赖新表，旧表仅在 migration 中被识别并回填后删除。

### 5.1 主恢复来源

主恢复来源统一为：

- `agent_runs`
- `run_checkpoints`
- `run_waits`
- `run_events`

语义要求：

- `LoadLatest(run_id)` 以 `agent_runs + latest checkpoint` 为主
- `LoadLatestBySession(session_id)` 优先返回 pending wait 对应的 run，而不是单纯 `updated_at` 最新的 run
- approval/barrier/reconnect 依赖 wait 语义，不依赖 legacy snapshot 表存在

### 5.2 当前状态

当前仓库已经完成 runtime 切换：

- 新写入只进入 runtime 新表
- `run_snapshot_records` 在 migration 中自动回填到 runtime 新表
- 主读路径只读 runtime 表
- migration 完成后删除 `run_snapshot_records`

### 5.3 完成标志

当前状态满足以下条件：

- 新 runtime 表承接全部恢复场景
- reconnect/approval/barrier 测试通过
- 旧库升级后可自动 backfill
- 会话删除只需要清理新 runtime 数据

---

## 6. Runtime 生命周期与 Retention

### 6.1 `agent_runs`

- `running` / `waiting` 为活跃执行
- `completed` / `failed` / `aborted` 为终态
- `finished_at` 只在终态写入

### 6.2 `run_waits`

- `pending` 表示仍可恢复
- `resolved` 表示等待已结束
- `expired` / `cancelled` 预留给未来的超时和显式终止语义

规则：

- run 进入终态时，相关 wait 必须转为非 pending
- reconnect 只关注 `pending` wait

### 6.3 `run_checkpoints`

- wait / completion / failure 节点必须保留
- 普通中间 checkpoint 可以做裁剪
- 每个 run 至少保留 latest checkpoint

### 6.4 清理策略

- 删除 conversation 时，必须清理 `agent_runs / run_checkpoints / run_waits / run_events`
- resolved waits 可按 retention 定期清理
- completed/failed runs 的旧 checkpoints 可按“保留最近 N 个”裁剪
- 运行态只需要清理新 runtime 表
- `resolved_at`
- `expires_at`

`wait_type` 例如：

- `approval`
- `barrier`
- `external`

#### `run_events`

追加型运行事件表。

建议字段：

- `id`
- `run_id`
- `seq`
- `event_type`
- `node`
- `route`
- `payload_json`
- `created_at`

作用：

- 排障
- 审计
- replay/reasoning trace
- 不替代 checkpoint

### 4.3 Analytics Data

#### `token_usage_sessions`

保留会话级 token/cost 汇总。

建议字段：

- `id`
- `conversation_id`
- `primary_model`
- `prompt_tokens`
- `completion_tokens`
- `total_calls`
- `total_cost_usd`
- `by_model_json`
- `updated_at`

#### `worker_transcripts`

保留，但降级为分析表，不再混入主业务/恢复语义。

#### `evo_runs`
#### `evo_evaluations`
#### `evo_repairs`
#### `evo_tool_usage`

建议继续拆表，但明确属于 analytics 域，可独立 retention。

---

## 5. 数据库约束与索引

### 5.1 必开项

数据库初始化必须开启：

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA synchronous = NORMAL`

### 5.2 必要约束

所有主要关系都必须落到数据库约束，而不是只靠应用层删表顺序。

必须有：

- `messages.conversation_id -> conversations.id`
- `message_tool_calls.message_id -> messages.id`
- `message_attachments.message_id -> messages.id`
- `artifacts.conversation_id -> conversations.id`
- `agent_runs.conversation_id -> conversations.id`
- `agent_runs.parent_run_id -> agent_runs.id`
- `run_checkpoints.run_id -> agent_runs.id`
- `run_waits.run_id -> agent_runs.id`
- `run_events.run_id -> agent_runs.id`

### 5.3 核心索引

最低建议：

- `conversations(updated_at DESC)`
- `messages(conversation_id, seq)`
- `messages(conversation_id, created_at)`
- `agent_runs(conversation_id, status, updated_at)`
- `run_checkpoints(run_id, checkpoint_no)`
- `run_waits(status, wait_type, expires_at)`
- `run_events(run_id, seq)`

---

## 6. JSON 使用规范

### 6.1 允许使用 JSON 的对象

- `run_checkpoints.turn_state_json`
- `run_checkpoints.execution_state_json`
- `run_waits.payload_json`
- `run_events.payload_json`
- 少量 metadata

### 6.2 JSON 结构要求

所有 runtime JSON 必须包含：

- `runtime_schema_version`
- `graph_id`
- `graph_version`

并且遵守规则：

1. 新字段默认可缺省
2. 缺省后会导致恢复漂移的字段，必须在恢复前显式校验
3. 不兼容时不能静默恢复

---

## 7. 兼容性与恢复策略

恢复结果只允许三类：

1. `recoverable`
   可直接恢复

2. `recoverable_with_degrade`
   可恢复，但要记录降级说明

3. `not_recoverable`
   不可恢复，只能终止该 run，不得污染会话主数据

### 7.1 不可恢复条件

以下情况应判定为 `not_recoverable`：

- JSON 无法反序列化
- `graph_id` 不匹配
- `runtime_schema_version` 不兼容且无升级器
- 缺失关键 wait/approval/barrier 信息

### 7.2 可降级恢复条件

以下通常可降级：

- 缺失 `PendingMedia`
- 缺失 `ActiveSkills`
- 缺失部分非关键 task metadata

### 7.3 恢复失败处理

恢复失败时必须：

1. 标记 run 为 `not_recoverable`
2. 保留 conversation/messages
3. 向上层返回明确错误类型
4. 可选地归档旧 checkpoint 供排障

---

## 8. 生命周期与清理策略

### 8.1 Core Data

- 默认长期保留
- 支持会话归档
- 删除 conversation 时数据库级 cascade

### 8.2 Runtime Data

- `running/waiting` 保留
- `completed/failed/aborted` 仅保留最近窗口
- 旧 checkpoint 定时清理
- 过期 wait 自动标记 `expired`

### 8.3 Analytics Data

- 明确 retention
- 建议支持 7/30/90 天策略
- 可导出归档

---

## 9. 对现有表的处置

### 9.1 保留并迁移

- `conversations`
- `history_messages` 的历史数据内容

说明：

- 不建议保留 `history_messages` 作为最终长期模型
- 但可以迁移其现有数据进入 `messages + message_tool_calls + message_attachments`

### 9.2 已重做

- `run_snapshot_records`
- `worker_transcripts`
- `session_token_usages`
- `evo_*`

说明：

- `run_snapshot_records` 已完成迁移并从活动 schema 中移除
- 其余 analytics 表已纳入统一 migration，但是否继续重做取决于后续 analytics 域方案

### 9.3 必须立即修正的问题

即使重构前，也必须先承认以下缺陷：

1. 删除 conversation 时必须同时删除 run snapshot 数据
2. 不能继续依赖分散 `AutoMigrate`
3. 必须引入 schema/version 管理

---

## 10. 迁移结果

当前已经完成的迁移：

1. `history_messages -> messages/message_tool_calls/message_attachments`
2. `run_snapshot_records -> agent_runs/run_checkpoints/run_waits`
3. 统一 `schema_migrations`
4. 主业务读路径切到新 core/runtime 表

当前未完成但独立于本次收口的部分：

1. analytics 域是否进一步拆分
2. analytics retention 的长期运营策略

### Phase E：冻结旧表

- 旧表只读
- 禁止新写入
- 保留导出窗口

### Phase F：清理旧表

确认无回滚需求后再删除旧表。

---

## 11. 迁移机制

必须建立统一 migration runner。

### 11.1 新增表：`schema_migrations`

字段建议：

- `version`
- `name`
- `applied_at`
- `checksum`

### 11.2 migration 类型

1. `DDL migration`
   建表、加列、加索引、加外键

2. `Backfill migration`
   旧数据回填到新表

3. `Semantic migration`
   升级 runtime JSON 语义版本，或将旧 checkpoint 标记为不可恢复

### 11.3 禁止项

禁止继续在 `NewXxxStore()` 内部调用 `AutoMigrate` 作为主迁移方式。

---

## 12. 对 `run_snapshot` 的最终定位

旧模型的核心问题不是技术细节，而是定位错误。

`run_snapshot` 不应该继续扮演“长期关键数据库合同”的角色。

正确定位：

- 它是 runtime 恢复缓存
- 可以存 JSON
- 必须版本化
- 必须可清理
- 不应承担主业务展示责任

如果系统未来继续增长，`run_checkpoints` 甚至可以独立迁到单独数据库或对象存储。

---

## 13. 推荐实施顺序

### 第一优先级

1. 建立统一 migration runner
2. 引入 FK/约束体系
3. 建立 `agent_runs + run_checkpoints + run_waits + run_events`
4. 修复 conversation 删除链

### 第二优先级

5. 重构消息模型
6. 引入 `artifacts`
7. 迁移历史消息和附件关系

### 第三优先级

8. 迁移 analytics 域
9. 建立 retention/archival 任务

---

## 14. 最终决策

### 决策 1

不再沿用“单层 SQLite + 分散 AutoMigrate + 手工级联删除”的持久化方式继续扩展。

### 决策 2

采用三层数据分域：

- Core Data
- Runtime Data
- Analytics Data

### 决策 3

保留核心业务数据，但重建运行态与分析态边界。

### 决策 4

数据库变更必须进入统一 migration 体系，不再由 store 构造函数隐式迁移。

---

## 15. 一句话版本

这次数据库重构的目标不是“把表改漂亮”，而是把 NGOAgent 的数据合同一次性定型：

**让业务真相稳定、运行恢复可靠、分析数据可演进、迁移受控、生命周期清晰。**
