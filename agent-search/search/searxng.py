"""
SearXNG client — calls the local SearXNG instance and parses full result fields.
"""

import logging
from typing import Optional

import httpx

from config import SEARXNG_URL, MAX_RESULTS_FROM_SEARXNG
from search.models import SearchResult

log = logging.getLogger("agent-search.searxng")


async def search(
    query: str,
    *,
    categories: str = "general",
    time_range: Optional[str] = None,
    limit: int = MAX_RESULTS_FROM_SEARXNG,
) -> list[SearchResult]:
    """
    Query SearXNG and return results with FULL metadata.

    Unlike devlabz-search which only extracted title/url/snippet,
    we parse score, engines, publishedDate, and category — these
    signals are critical for algorithmic pre-filtering.
    """
    params: dict = {
        "q": query,
        "format": "json",
        "pageno": 1,
        "categories": categories,
    }
    if time_range:
        params["time_range"] = time_range

    try:
        async with httpx.AsyncClient(timeout=15) as client:
            resp = await client.get(f"{SEARXNG_URL}/search", params=params)
            resp.raise_for_status()
            data = resp.json()
    except Exception as e:
        log.error("SearXNG request failed: %s", e)
        return []

    raw = data.get("results", [])[:limit]
    results: list[SearchResult] = []

    for r in raw:
        results.append(
            SearchResult(
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
        "SearXNG returned %d results for query=%r (categories=%s)",
        len(results), query, categories,
    )
    return results
