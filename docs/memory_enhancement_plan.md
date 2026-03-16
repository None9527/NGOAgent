# NGOAgent 记忆系统增强计划

> 借鉴 MemOS 核心思路，保持 NGOAgent **嵌入式零依赖** 设计原则。

## 现状分析

**当前架构（4 层存储）**:

| 存储 | 检索方式 | 弱点 |
|------|---------|------|
| Brain (`brain/{sid}/`) | 文件读取 | 仅当前会话，无跨会话关联 |
| KI Store (`knowledge/`) | `strings.Contains` 暴力匹配 | O(n) 全扫，无语义、无全文索引 |
| Workspace (`context.md`) | 全量注入 prompt | 无版本、无结构化查询 |
| Forge (`forge/`) | 文件读取 | 仅锻造记录 |

**核心问题**: KI 检索是 `strings.Contains` 暴力扫描（[store.go:103-124](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/store.go#L103-L124)），向量检索框架已有（[retriever.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/retriever.go)）但未接入 Embedder 实现。

---

## 分期实施

### Phase 1: FTS5 混合检索（高 ROI，~600 行）

> 🎯 目标: KI 搜索从 O(n) 暴力匹配升级到 SQLite FTS5 + 向量双路检索

#### [MODIFY] [db.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/persistence/db.go)

新增 `KnowledgeItem` 表 + FTS5 虚拟表：

```sql
CREATE TABLE knowledge_items (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    summary    TEXT,
    content    TEXT,
    tags       TEXT,  -- JSON array
    sources    TEXT,  -- JSON array
    embedding  BLOB,  -- float32 向量序列化
    created_at DATETIME,
    updated_at DATETIME
);

CREATE VIRTUAL TABLE ki_fts USING fts5(
    title, summary, content, tags,
    content='knowledge_items', content_rowid='rowid'
);
```

#### [MODIFY] [store.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/store.go)

- `Save()`: 写入 SQLite 表 + 同步 FTS5 索引（触发器自动）
- `Search()`: **双路检索** → FTS5 `MATCH` 全文 + 向量余弦 → 合并排序
- `List()` / `Get()`: 从 DB 读取代替文件遍历
- 保留 `knowledge/{id}/artifacts/` 目录结构（大文件不入 DB）
- 迁移: 启动时检测旧 JSON 文件 → 自动导入 DB

#### [MODIFY] [retriever.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/retriever.go)

- `HybridSearch(query, topK)`: FTS5 分数 * α + 向量分数 * (1-α)
- α 默认 0.6（全文优先，编码场景关键词匹配更精准）
- 向量检索: 复用现有 `VectorIndex` + `Embedder` 接口

#### [NEW] [embedder_local.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/embedder_local.go)

本地 Embedder 实现（调用已配置的 LLM Provider 的 embedding 端点）：
- 通过 `llm.Router` 路由到支持 embedding 的 Provider
- Fallback: 关键词 TF-IDF 向量（纯 Go 实现，零外部依赖）

---

### Phase 2: 记忆反馈 + 生命周期（~400 行）

> 🎯 目标: 记忆不只是追加，还能修正/合并/遗忘

#### [MODIFY] [store.go](file:///home/none/ngoclaw/NGOAgent/internal/infrastructure/knowledge/store.go)

新增能力:

```go
// 反馈修正 — 用自然语言修改已有记忆
func (s *Store) Feedback(id string, feedback string) error
// Agent 调用后 LLM 生成修改建议 → 自动 patch 记忆内容

// 合并重复 — 相似度 > 0.85 的 KI 自动合并
func (s *Store) MergeIfDuplicate(item *Item) (merged bool, existingID string, err error)

// 衰减遗忘 — 长期未访问的 KI 降权
func (s *Store) DecayUnused(threshold time.Duration) (decayedCount int)
```

#### KI 生命周期状态

```
active → stale (>30d 未引用) → archived (>90d) → deleted (用户确认)
```

- `GenerateSummaries()` 优先返回 active，stale 降权，archived 不注入
- 心跳可定期运行 `DecayUnused` 清理

---

### Phase 3: WebUI 记忆面板（~800 行前端）

> 🎯 目标: 用户可以在 WebUI 可视化浏览/搜索/编辑/删除记忆

#### 后端 API

```
GET    /api/v1/knowledge          — 列表（支持分页、搜索 query）
GET    /api/v1/knowledge/:id      — 详情
PUT    /api/v1/knowledge/:id      — 编辑
DELETE /api/v1/knowledge/:id      — 删除
POST   /api/v1/knowledge/search   — 混合搜索
GET    /api/v1/knowledge/stats    — 统计（总数、标签分布、生命周期分布）
```

#### 前端组件

| 组件 | 功能 |
|------|------|
| `KnowledgePanel.tsx` | 侧边栏面板（类似 BrainPanel） |
| `KnowledgeList.tsx` | 列表 + 搜索 + 标签筛选 |
| `KnowledgeDetail.tsx` | Markdown 渲染详情 + 内联编辑 |
| `KnowledgeStats.tsx` | 统计图表（标签分布饼图、时间线） |

---

## 优先级决策

| Phase | 收益 | 复杂度 | 依赖 |
|-------|------|--------|------|
| **Phase 1** | ⭐⭐⭐ 检索精度质变 | 中 (~600行) | SQLite FTS5（已内置） |
| **Phase 2** | ⭐⭐ 记忆质量提升 | 低 (~400行) | Phase 1 |
| **Phase 3** | ⭐⭐ 用户体验 | 中 (~800行) | Phase 1 |

> [!IMPORTANT]
> **Phase 1 建议优先实施** — SQLite 已内置 FTS5 模块，无需引入任何新依赖。当前 `strings.Contains` 暴力匹配在 KI 数量增长后会成为瓶颈。

## 不做的事情（与 MemOS 差异化）

| MemOS 能力 | 不引入原因 |
|-----------|----------|
| Redis Streams 异步调度 | 引入外部依赖，NGOAgent 定位是零依赖嵌入式 |
| PostgreSQL / NebulaGraph | 同上，SQLite 足够覆盖单用户场景 |
| 多模态记忆（图片/图表） | 编码 Agent 场景优先级极低 |
| 云托管 SaaS | NGOAgent 定位是本地部署 |
| MemCube 多租户隔离 | 单用户系统无需 |

## Verification Plan

### Phase 1 验证
- 单元测试: FTS5 中英文搜索、向量检索精度、混合排序
- 集成测试: 旧 JSON KI 自动迁移 → DB → 搜索验证
- 性能: 1000 条 KI 下搜索延迟 < 10ms

### Phase 2 验证
- 反馈修正端到端: save → feedback → 验证内容改变
- 衰减: mock 时间 → 验证 stale/archived 状态转换

### Phase 3 验证
- 浏览器测试: 列表/搜索/编辑/删除全流程
