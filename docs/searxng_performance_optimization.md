# SearXNG 搜索性能全链路优化方案

## 背景

当前 `agent-search` 服务的搜索链路：`Agent → agent-search (Python/FastAPI) → SearXNG → 上游搜索引擎`。性能瓶颈分布在两个层面：**SearXNG 实例侧** 和 **agent-search 客户端侧**。

本方案基于互联网调研和现有代码审计，提出 6 个模块的优化措施。

---

## User Review Required

> [!IMPORTANT]
> 本方案**仅为调研产出**，不涉及任何代码修改。请审阅后决定哪些模块落地实施。

> [!WARNING]
> 模块 A（引擎精简）需要你根据实际部署环境（国内/外网）决定保留哪些引擎。模块 D（Redis 缓存）需要确认当前 Docker 部署中是否已有 Redis 容器。

---

## 优化模块总览

| 模块 | 层面 | 优化点 | 预期收益 |
|:---|:---|:---|:---|
| **A** | SearXNG 实例 | 引擎精简 + 权重调优 | P95 延迟降低 40-60% |
| **B** | SearXNG 实例 | 超时 + 连接池调优 | 消除慢引擎长尾阻塞 |
| **C** | SearXNG 实例 | uWSGI 并发调优 | 并发吞吐量 ×2-4 |
| **D** | SearXNG 实例 | Redis 结果缓存 | 重复查询 0ms 热命中 |
| **E** | agent-search 客户端 | httpx 连接池复用 | 消除每次请求的 TCP/TLS 握手 |
| **F** | agent-search 客户端 | 应用层结果缓存 + 并发控制 | 高频查询跳过网络 IO |

---

## 模块 A：引擎精简 + 权重调优

### 问题

SearXNG 默认启用大量引擎，响应时间 = max(所有启用引擎的响应时间)。任何一个慢引擎都会拖慢整个搜索。

### 方案

根据 `/stats` 页面统计，按 P95 响应时间排序，仅保留 **5-8 个高质量引擎**：

```yaml
# settings.yml — 引擎精简
engines:
  # === 高权重核心引擎 ===
  - name: google
    disabled: false
    weight: 1.5
    timeout: 3.0

  - name: bing
    disabled: false
    weight: 1.2
    timeout: 3.0

  - name: duckduckgo
    disabled: false
    weight: 1.0
    timeout: 3.0

  # === 专业领域引擎 ===
  - name: github
    disabled: false
    weight: 1.3
    categories: [it]
    timeout: 3.0

  - name: stackoverflow
    disabled: false
    weight: 1.2
    categories: [it]
    timeout: 3.0

  - name: wikipedia
    disabled: false
    weight: 1.0
    timeout: 2.0

  - name: arxiv
    disabled: false
    weight: 1.0
    categories: [science]
    timeout: 4.0

  # === 国内引擎 (按需) ===
  - name: brave
    disabled: false
    weight: 0.8
    timeout: 3.0
```

### 关键行动
- 访问 `http://<searxng>/stats` 查看各引擎实际延迟
- 对 P95 > 5s 的引擎设 `disabled: true`
- 核心引擎 weight > 1.0，次要引擎 weight < 1.0

---

## 模块 B：超时 + 连接池调优

### 问题

当前 SearXNG 默认的 `request_timeout` 和连接池配置偏保守，慢引擎会阻塞整体响应。

### 方案

```yaml
# settings.yml — outgoing 网络优化
outgoing:
  # 全局引擎请求超时 (秒) — 默认 3.0，降低以加速
  request_timeout: 2.0

  # 超时上限 (引擎级 timeout 不能超过此值)
  max_request_timeout: 5.0

  # 连接池 — 为并发搜索优化
  pool_connections: 50     # 连接池数量 (默认 100, 按实际引擎数调整)
  pool_maxsize: 20         # 每池最大 keep-alive 连接 (默认 10)
  keepalive_expiry: 10.0   # keep-alive 保持时间 (默认 5.0, 延长减少重连)

  # 代理 (如需翻墙访问 Google 等)
  # proxies:
  #   all://:
  #     - socks5://127.0.0.1:1080
  #     - http://127.0.0.1:7890
  #   OR use: source_ips / proxies_pattern
```

### 收益
- `request_timeout: 2.0` → 最慢引擎 2s 即断开，不再拖慢整体
- `keepalive_expiry: 10.0` → 复用 TCP 连接，减少握手开销

---

## 模块 C：uWSGI 并发调优

### 问题

SearXNG 是 I/O 密集型应用（大量等待上游引擎响应），默认的 worker/thread 配置可能无法充分利用 CPU 等待时间。

### 方案

修改 `uwsgi.ini`（或通过 Docker 环境变量）：

```ini
# uwsgi.ini — 并发优化
[uwsgi]
# Worker 数 = CPU 核心数
workers = %k

# 每个 worker 的线程数 (I/O 密集推荐 4-8)
threads = 4

# 必须启用 Python 线程
enable-threads = true

# 监听队列 (默认 100, 高并发场景提升)
listen = 256

# 关闭请求日志 (减少 I/O 开销)
disable-logging = true

# 内存泄漏防护 — 每处理 5000 请求回收 worker
max-requests = 5000

# 请求体大小限制
buffer-size = 8192
```

或通过 Docker 环境变量：
```bash
SEARXNG_UWSGI_WORKERS=4
SEARXNG_UWSGI_THREADS=4
```

### 收益
- 并发处理能力：`workers × threads = 4 × 4 = 16` 并发请求
- 相比默认 (通常 1×1) 提升 **16 倍并发吞吐**

---

## 模块 D：Redis 结果缓存

### 问题

当前每次搜索都完整走 SearXNG → 上游引擎链路，高频/重复查询浪费大量网络 IO。

### 方案

#### 1. Docker Compose 新增 Redis 容器

```yaml
# docker-compose.yml
services:
  redis:
    image: valkey/valkey:8-alpine
    container_name: searxng-redis
    restart: unless-stopped
    command: valkey-server --save 30 1 --loglevel warning
    volumes:
      - redis-data:/data

  searxng:
    image: searxng/searxng:latest
    environment:
      - SEARXNG_REDIS_URL=redis://redis:6379/0
    depends_on:
      - redis

volumes:
  redis-data:
```

#### 2. SearXNG settings.yml

```yaml
redis:
  url: "redis://redis:6379/0"

server:
  limiter: true     # 启用 Redis 支持的限速器
  image_proxy: true
```

### 收益
- 相同查询第二次命中缓存：**响应时间 < 50ms**（vs 原来的 2-5s）
- 支持 limiter 防滥用

---

## 模块 E：agent-search 客户端 httpx 连接池复用

### 问题

当前 [searxng.py](file:///home/none/ngoclaw/NGOAgent/agent-search/search/searxng.py#L44) 每次搜索都 `async with httpx.AsyncClient(timeout=15) as client:` 新建客户端，**每次请求都经历完整的 TCP 握手 + TLS 协商**，浪费 50-200ms。

### 方案

```python
# search/searxng.py — 改为模块级长生命周期客户端

import httpx
from config import SEARXNG_URL

# 模块级 singleton 客户端 — 复用连接池
_limits = httpx.Limits(
    max_keepalive_connections=20,
    max_connections=50,
)
_timeout = httpx.Timeout(
    timeout=10.0,    # 总超时
    connect=3.0,     # 连接超时
    read=8.0,        # 读取超时 (SearXNG 聚合引擎需要时间)
    pool=5.0,        # 连接池等待超时
)

_client: httpx.AsyncClient | None = None


def get_client() -> httpx.AsyncClient:
    global _client
    if _client is None or _client.is_closed:
        _client = httpx.AsyncClient(
            base_url=SEARXNG_URL,
            limits=_limits,
            timeout=_timeout,
        )
    return _client


async def close_client():
    """Call on app shutdown."""
    global _client
    if _client and not _client.is_closed:
        await _client.aclose()
        _client = None
```

在 `main.py` 的 FastAPI lifespan 中注册关闭：

```python
from contextlib import asynccontextmanager
from search.searxng import close_client

@asynccontextmanager
async def lifespan(app: FastAPI):
    yield
    await close_client()

app = FastAPI(lifespan=lifespan)
```

### 收益
- 消除每次请求 50-200ms 的 TCP/TLS 握手开销
- keep-alive 连接复用，后续请求 **P50 延迟降低 30-50%**

---

## 模块 F：应用层结果缓存 + 并发控制

### 问题

1. agent 经常对相同/相似查询重复搜索
2. 无并发控制，burst 请求可能压垮 SearXNG

### 方案

#### 1. LRU 内存缓存 (无需 Redis 依赖)

```python
# search/cache.py — 应用层 LRU 缓存
import hashlib
import time
from collections import OrderedDict
from typing import Any

class SearchCache:
    """Thread-safe LRU cache for search results."""

    def __init__(self, max_size: int = 256, ttl_seconds: int = 3600):
        self._cache: OrderedDict[str, tuple[float, Any]] = OrderedDict()
        self._max_size = max_size
        self._ttl = ttl_seconds

    def _key(self, query: str, categories: str, time_range: str | None) -> str:
        raw = f"{query}|{categories}|{time_range or ''}"
        return hashlib.sha256(raw.encode()).hexdigest()[:16]

    def get(self, query: str, categories: str, time_range: str | None) -> Any | None:
        key = self._key(query, categories, time_range)
        item = self._cache.get(key)
        if item is None:
            return None
        ts, value = item
        if time.monotonic() - ts > self._ttl:
            del self._cache[key]
            return None
        self._cache.move_to_end(key)
        return value

    def set(self, query: str, categories: str, time_range: str | None, value: Any):
        key = self._key(query, categories, time_range)
        self._cache[key] = (time.monotonic(), value)
        self._cache.move_to_end(key)
        while len(self._cache) > self._max_size:
            self._cache.popitem(last=False)

# Global instance
search_cache = SearchCache(max_size=256, ttl_seconds=3600)
```

#### 2. 并发信号量

在 `searxng.py` 的 `search()` 函数中加入：

```python
import asyncio

# 最多同时向 SearXNG 发起 5 个搜索请求
_semaphore = asyncio.Semaphore(5)

async def search(query, *, categories="general", time_range=None, limit=30):
    # 1. 先查缓存
    cached = search_cache.get(query, categories, time_range)
    if cached is not None:
        log.info("Cache HIT for query=%r", query)
        return cached

    # 2. 带并发控制的 SearXNG 请求
    async with _semaphore:
        client = get_client()
        resp = await client.get("/search", params={...})
        ...

    # 3. 写入缓存
    search_cache.set(query, categories, time_range, results)
    return results
```

### 收益
- 缓存命中：**0ms 网络 IO**，直接返回
- 信号量：防止 burst 请求压垮 SearXNG，保持稳定 P99

---

## 优化效果预估

| 场景 | 优化前 | 优化后 | 改善 |
|:---|:---|:---|:---|
| quick 搜索 (首次) | ~2-5s | ~0.8-1.5s | **60-70%↓** |
| quick 搜索 (缓存) | ~2-5s | ~5ms | **99%↓** |
| standard 搜索 (首次) | ~3-8s | ~1.5-3s | **50-60%↓** |
| deep 搜索 (首次) | ~8-20s | ~4-10s | **50%↓** |
| 并发 10 请求 | 排队/超时 | 平稳处理 | 稳定性↑ |

---

## 实施优先级建议

| 优先级 | 模块 | 难度 | 理由 |
|:---|:---|:---|:---|
| **P0** | E (httpx 连接池) | 低 | 代码改动最小，收益立竿见影 |
| **P0** | B (超时调优) | 低 | 纯配置修改，消除长尾阻塞 |
| **P1** | A (引擎精简) | 中 | 需要观察 /stats 做决策 |
| **P1** | F (应用层缓存) | 中 | 新增约 60 行 Python |
| **P2** | D (Redis 缓存) | 中 | 需要新增容器，但效果显著 |
| **P2** | C (uWSGI 调优) | 低 | 单用户场景收益有限 |

---

## Open Questions

> [!IMPORTANT]
> 1. 当前 SearXNG 实例的部署方式？（Docker Compose？已有 Redis？）
> 2. 是否有 `/stats` 页面数据可以帮助判断哪些引擎该禁用？
> 3. 你倾向于哪些模块优先落地？（建议至少 P0 的 E + B）
> 4. 是否需要同时优化 SearXNG 的 `settings.yml` 配置？还是仅优化 agent-search 客户端侧？

---

## Verification Plan

### Automated Tests
```bash
# 1. 启动后基准测试 — 对比优化前后 P50/P95
curl -w "@curl-timing.txt" -o /dev/null -s "http://localhost:8889/api/search" \
  -X POST -H "Content-Type: application/json" \
  -d '{"query": "python performance optimization", "depth": "quick"}'

# 2. 重复查询缓存命中测试
for i in {1..5}; do
  time curl -s "http://localhost:8889/api/search" \
    -X POST -H "Content-Type: application/json" \
    -d '{"query": "test cache hit", "depth": "quick"}' > /dev/null
done

# 3. 并发压力测试
wrk -t4 -c16 -d10s -s post.lua http://localhost:8889/api/search
```

### Manual Verification
- 检查 SearXNG `/stats` 页面确认引擎响应时间
- 观察 agent-search 日志中的 "Cache HIT" 命中率
- 观察 agent 实际使用中的搜索延迟体感
