"""
SearXNG client — persistent connection pool + LRU cache + concurrency control.

v2 optimizations:
  - Module-level httpx.AsyncClient singleton (connection reuse)
  - LRU cache hit skips network entirely
  - Semaphore prevents SearXNG overload under burst
"""

import asyncio
import logging
from typing import Optional

import httpx

from config import SEARXNG_URL, MAX_RESULTS_FROM_SEARXNG
from search.models import InternalResult
from search.cache import search_cache

log = logging.getLogger("agent-search.searxng")

# ---------------------------------------------------------------------------
# Connection pool singleton
# ---------------------------------------------------------------------------

_limits = httpx.Limits(
    max_keepalive_connections=20,
    max_connections=50,
)
_timeout = httpx.Timeout(
    timeout=10.0,
    connect=3.0,
    read=8.0,
    pool=5.0,
)

_client: httpx.AsyncClient | None = None

# Concurrency control — max 5 simultaneous SearXNG requests
_semaphore = asyncio.Semaphore(5)


def _get_client() -> httpx.AsyncClient:
    global _client
    if _client is None or _client.is_closed:
        _client = httpx.AsyncClient(
            base_url=SEARXNG_URL,
            limits=_limits,
            timeout=_timeout,
        )
    return _client


async def close_client():
    """Call on app shutdown to release connections."""
    global _client
    if _client and not _client.is_closed:
        await _client.aclose()
        _client = None


# ---------------------------------------------------------------------------
# Search function
# ---------------------------------------------------------------------------

async def search(
    query: str,
    *,
    categories: str = "general",
    time_range: Optional[str] = None,
    limit: int = MAX_RESULTS_FROM_SEARXNG,
) -> list[InternalResult]:
    """
    Query SearXNG with connection reuse, caching, and concurrency control.
    """
    # 1. Cache check
    cached = search_cache.get(query, categories, time_range)
    if cached is not None:
        log.info("Cache HIT for query=%r (categories=%s)", query, categories)
        return cached

    # 2. Build params
    has_chinese = any("\u4e00" <= c <= "\u9fff" for c in query)
    params: dict = {
        "q": query,
        "format": "json",
        "pageno": 1,
        "categories": categories,
    }
    if has_chinese:
        params["language"] = "zh"
    if time_range:
        params["time_range"] = time_range

    # 3. Fetch with concurrency control
    try:
        async with _semaphore:
            client = _get_client()
            resp = await client.get("/search", params=params)
            resp.raise_for_status()
            data = resp.json()
    except Exception as e:
        log.error("SearXNG request failed: %s", e)
        return []

    # 4. Parse results
    raw = data.get("results", [])[:limit]
    results: list[InternalResult] = []

    for r in raw:
        results.append(
            InternalResult(
                title=r.get("title", ""),
                url=r.get("url", ""),
                snippet=r.get("content", ""),
                score=float(r.get("score", 0)),
                engines=r.get("engines", []),
                published_date=r.get("publishedDate"),
                category=r.get("category", "general"),
                image_url=r.get("img_src"),
                thumbnail_url=r.get("thumbnail"),
            )
        )

    log.info(
        "SearXNG returned %d results for query=%r (categories=%s) [cache MISS]",
        len(results), query, categories,
    )

    # 5. Store in cache
    search_cache.set(query, categories, time_range, results)
    return results
