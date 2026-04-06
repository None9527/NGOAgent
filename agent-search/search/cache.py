"""
Application-level LRU cache for search results.

Eliminates redundant SearXNG queries for repeated/similar searches.
TTL-based expiry ensures freshness for time-sensitive queries.
"""

import hashlib
import time
from collections import OrderedDict
from typing import Any

import logging

log = logging.getLogger("agent-search.cache")


class SearchCache:
    """Thread-safe LRU cache with TTL expiry."""

    def __init__(self, max_size: int = 256, ttl_seconds: int = 3600):
        self._cache: OrderedDict[str, tuple[float, Any]] = OrderedDict()
        self._max_size = max_size
        self._ttl = ttl_seconds
        self._hits = 0
        self._misses = 0

    def _key(self, query: str, categories: str, time_range: str | None) -> str:
        raw = f"{query}|{categories}|{time_range or ''}"
        return hashlib.sha256(raw.encode()).hexdigest()[:16]

    def get(self, query: str, categories: str = "general",
            time_range: str | None = None) -> Any | None:
        key = self._key(query, categories, time_range)
        item = self._cache.get(key)
        if item is None:
            self._misses += 1
            return None
        ts, value = item
        if time.monotonic() - ts > self._ttl:
            del self._cache[key]
            self._misses += 1
            return None
        self._cache.move_to_end(key)
        self._hits += 1
        return value

    def set(self, query: str, categories: str, time_range: str | None,
            value: Any):
        key = self._key(query, categories, time_range)
        self._cache[key] = (time.monotonic(), value)
        self._cache.move_to_end(key)
        while len(self._cache) > self._max_size:
            self._cache.popitem(last=False)

    @property
    def stats(self) -> dict:
        total = self._hits + self._misses
        return {
            "size": len(self._cache),
            "hits": self._hits,
            "misses": self._misses,
            "hit_rate": f"{self._hits / total * 100:.1f}%" if total else "N/A",
        }


# Global singleton
search_cache = SearchCache(max_size=256, ttl_seconds=3600)
