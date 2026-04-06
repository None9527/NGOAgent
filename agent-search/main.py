"""
Agent Search Service — FastAPI application.

Two endpoints:
  POST /api/search   — unified search with depth control (quick/standard/deep)
  POST /api/extract  — extract content from known URLs

Pipeline:
  SearXNG (30 results)
    → Stage 1: algorithmic prefilter (→ 10)
    → Stage 2: LLM reranker (→ ranked, only for standard/deep)
    → Stage 3: concurrent stealth fetch (top N, only for deep)
    → Stage 4: algorithmic de-noising (only for deep)
"""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from search.models import (
    SearchRequest,
    SearchResponse,
    ExtractRequest,
    ExtractResponse,
)
from search.pipeline import search_pipeline, extract_pipeline
from search.searxng import close_client
from search.cache import search_cache

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(name)s] %(levelname)s %(message)s",
)
log = logging.getLogger("agent-search")


@asynccontextmanager
async def lifespan(app: FastAPI):
    log.info("agent-search starting up")
    yield
    log.info("agent-search shutting down, closing connections...")
    await close_client()


app = FastAPI(
    title="Agent Search Service",
    description="SearXNG + LLM Reranker + Stealth Fetch + De-noising",
    version="2.1.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health():
    return {"status": "ok", "cache": search_cache.stats}


@app.post("/api/search", response_model=SearchResponse)
async def api_search(req: SearchRequest):
    """
    Unified search endpoint.

    depth controls pipeline depth:
      quick:    SearXNG → prefilter                        (~200ms)
      standard: SearXNG → prefilter → LLM rerank           (~2s)
      deep:     SearXNG → prefilter → LLM rerank → fetch → clean (~5-15s)
    """
    return await search_pipeline(req)


@app.post("/api/extract", response_model=ExtractResponse)
async def api_extract(req: ExtractRequest):
    """
    Extract content from known URLs.

    Automatically bypasses anti-bot protections via L1 (curl_cffi)
    and L2 (Camoufox stealth browser). Binary assets are cached locally.
    """
    return await extract_pipeline(req)
