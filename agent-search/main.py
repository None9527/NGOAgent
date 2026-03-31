"""
Agent Search Service — FastAPI application.

Exposes three endpoints:
  POST /api/search          — search only (SearXNG + prefilter + LLM rerank)
  POST /api/fetch           — deep-crawl a single URL
  POST /api/search_and_fetch — search + auto deep-crawl top N results

Pipeline:
  SearXNG (30 results)
    → Stage 1: algorithmic prefilter (→ 10)
    → Stage 2: LLM listwise reranker (→ ranked by relevance)
    → agent-browser concurrent fetch (top N → markdown)
    → structured JSON response
"""

import logging
import time

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from search.models import (
    SearchRequest,
    SearchResponse,
    FetchRequest,
    FetchResponse,
    SearchAndFetchRequest,
    SearchAndFetchResponse,
)
from search import searxng, prefilter, reranker, fetcher

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(name)s] %(levelname)s %(message)s",
)
log = logging.getLogger("agent-search")

app = FastAPI(
    title="Agent Search Service",
    description="SearXNG + LLM Reranker + agent-browser deep-fetch",
    version="1.0.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health():
    return {"status": "ok"}


@app.post("/api/search", response_model=SearchResponse)
async def api_search(req: SearchRequest):
    """
    Search pipeline: SearXNG → prefilter → LLM rerank.
    Returns scored and ranked results without deep content.
    """
    t0 = time.monotonic()

    # ① SearXNG
    raw = await searxng.search(
        req.query,
        categories=req.categories,
        time_range=req.time_range,
        limit=30,
    )

    # ② Algorithmic pre-filter (30 → 10)
    filtered = prefilter.prefilter(req.query, raw)

    # ③ LLM rerank (10 → ranked)
    ranked, intent = await reranker.rerank(req.query, filtered)

    # Trim to requested limit
    results = ranked[:req.limit]
    elapsed = int((time.monotonic() - t0) * 1000)

    return SearchResponse(
        results=results,
        total=len(results),
        intent=intent,
        query_time_ms=elapsed,
    )


@app.post("/api/fetch", response_model=FetchResponse)
async def api_fetch(req: FetchRequest):
    """
    Deep-fetch a single URL via agent-browser.
    Returns clean text content.
    """
    result = await fetcher.fetch_page(
        req.url,
        max_length=req.max_length,
    )
    return result


@app.post("/api/search_and_fetch", response_model=SearchAndFetchResponse)
async def api_search_and_fetch(req: SearchAndFetchRequest):
    """
    Combined pipeline: search + rerank + deep-fetch top N.

    This is the primary endpoint for NGOAgent's web_search tool.
    One call does everything — no need for the agent to orchestrate
    multiple search/fetch steps.
    """
    t0 = time.monotonic()

    # ① SearXNG
    raw = await searxng.search(
        req.query,
        categories=req.categories,
        time_range=req.time_range,
        limit=30,
    )

    # ② Algorithmic pre-filter (30 → 10)
    filtered = prefilter.prefilter(req.query, raw)

    # ③ LLM rerank (10 → ranked)
    ranked, intent = await reranker.rerank(req.query, filtered)

    # ④ Concurrent deep-fetch top N
    fetch_count = min(req.fetch_top, len(ranked))
    top_urls = [r.url for r in ranked[:fetch_count]]
    fetched = await fetcher.fetch_pages(
        top_urls,
        max_length=req.max_content_length,
    )

    # ⑤ Merge fetched content into results
    fetched_ok = 0
    for i, fr in enumerate(fetched):
        if fr.content_type != "error":
            ranked[i].content = fr.content
            fetched_ok += 1

    # Trim to requested limit
    results = ranked[:req.limit]
    elapsed = int((time.monotonic() - t0) * 1000)

    log.info(
        "search_and_fetch: query=%r intent=%s results=%d fetched=%d/%d time=%dms",
        req.query, intent, len(results), fetched_ok, fetch_count, elapsed,
    )

    return SearchAndFetchResponse(
        results=results,
        total=len(results),
        fetched=fetched_ok,
        intent=intent,
        query_time_ms=elapsed,
    )
