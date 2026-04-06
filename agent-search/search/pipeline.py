"""
Pipeline orchestrator — routes search/extract requests through the staged pipeline.

Search pipeline stages (controlled by depth):
  quick:    Stage 1 (prefilter)
  standard: Stage 1 → Stage 2 (LLM rerank)
  deep:     Stage 1 → Stage 2 → Stage 3 (fetch) → Stage 4 (clean)

Extract pipeline:
  Stage 3 (fetch) → Stage 4 (clean)
"""

import logging
import time

from search.models import (
    SearchRequest,
    SearchResponse,
    SearchResult,
    ExtractRequest,
    ExtractResponse,
    ExtractResult,
    InternalResult,
    ImageResult,
)
from search import searxng, prefilter, reranker, fetcher
from search.cleaner import clean_content

log = logging.getLogger("agent-search.pipeline")


# ---------------------------------------------------------------------------
# Internal → API conversion
# ---------------------------------------------------------------------------

def _normalize_image(img) -> ImageResult:
    """Convert a raw image (dict or ImageResult) to a clean ImageResult."""
    if isinstance(img, ImageResult):
        return img
    if isinstance(img, dict):
        return ImageResult(
            url=img.get("src", "") or img.get("url", ""),
            description=img.get("alt", "") or img.get("description", ""),
        )
    return ImageResult(url=str(img))


def _to_search_result(ir: InternalResult) -> SearchResult:
    """Convert internal pipeline result to clean API-facing SearchResult."""
    imgs = [_normalize_image(i) for i in ir.images]
    if ir.image_url and not any(i.url == ir.image_url for i in imgs):
        imgs.append(ImageResult(url=ir.image_url, description=ir.title))

    return SearchResult(
        title=ir.title,
        url=ir.url,
        domain=ir.domain,
        published_date=ir.published_date,
        score=ir.rerank_score,
        reason=ir.reason,
        result_type=ir.result_type,
        snippet=ir.snippet,
        content=ir.content,
        fetch_method=ir.fetch_method,
        fetch_time_ms=ir.fetch_time_ms,
        truncated=ir.truncated,
        local_path=ir.local_path,
        images=imgs,
    )


async def _collect_top_images(results: list[InternalResult]) -> list[ImageResult]:
    """Collect image URLs, pre-cache them to local disk, and return with local_path."""
    seen: set[str] = set()
    images: list[ImageResult] = []
    urls_to_cache: list[str] = []

    for r in results:
        if r.image_url and r.image_url not in seen:
            seen.add(r.image_url)
            images.append(ImageResult(url=r.image_url, description=r.title))
            urls_to_cache.append(r.image_url)
        if r.thumbnail_url and r.thumbnail_url not in seen:
            seen.add(r.thumbnail_url)
            images.append(ImageResult(url=r.thumbnail_url, description=r.title))
            urls_to_cache.append(r.thumbnail_url)
        for img in r.images:
            url = img.url if isinstance(img, ImageResult) else (img.get("src", "") if isinstance(img, dict) else "")
            if url and url not in seen:
                seen.add(url)
                images.append(_normalize_image(img))
                urls_to_cache.append(url)

    # Pre-cache images to local disk (bypasses anti-hotlinking)
    if urls_to_cache:
        cached = await fetcher.cache_images_batch(urls_to_cache, max_concurrent=5)
        for img in images:
            if img.url in cached:
                img.local_path = cached[img.url]

    return images


# ---------------------------------------------------------------------------
# Search pipeline
# ---------------------------------------------------------------------------

async def search_pipeline(req: SearchRequest) -> SearchResponse:
    """
    Main search pipeline. Depth controls how many stages run:

      quick:    Stage 1 only      (~200ms, ~300 tokens)
      standard: Stage 1 + 2       (~2s,    ~600 tokens)
      deep:     Stage 1 + 2 + 3+4 (~5-15s, ~8K tokens)
    """
    t0 = time.monotonic()

    # ── Stage 1: SearXNG + Prefilter (always) ──
    raw = await searxng.search(
        req.query,
        categories=req.categories,
        time_range=req.time_range,
        limit=30,
    )
    filtered = prefilter.prefilter(req.query, raw)

    intent = "general"

    if req.depth == "quick":
        results = filtered[:req.max_results]
        elapsed = int((time.monotonic() - t0) * 1000)
        log.info(
            "search[quick]: query=%r results=%d time=%dms",
            req.query, len(results), elapsed,
        )
        # Collect top-level images for image category searches
        top_images = (await _collect_top_images(results)) if req.categories == "images" else []
        return SearchResponse(
            query=req.query,
            intent=intent,
            results=[_to_search_result(r) for r in results],
            images=top_images,
            total=len(results),
            depth="quick",
            query_time_ms=elapsed,
        )

    # ── Stage 2: LLM Rerank (standard + deep) ──
    ranked, intent = await reranker.rerank(req.query, filtered)

    if req.depth == "standard":
        results = ranked[:req.max_results]
        elapsed = int((time.monotonic() - t0) * 1000)
        log.info(
            "search[standard]: query=%r intent=%s results=%d time=%dms",
            req.query, intent, len(results), elapsed,
        )
        top_images = (await _collect_top_images(results)) if req.categories == "images" else []
        return SearchResponse(
            query=req.query,
            intent=intent,
            results=[_to_search_result(r) for r in results],
            images=top_images,
            total=len(results),
            depth="standard",
            query_time_ms=elapsed,
        )

    # ── Stage 3: Concurrent deep-fetch (deep only) ──
    fetch_count = min(req.fetch_top, len(ranked))
    top_urls = [r.url for r in ranked[:fetch_count]]
    fetched = await fetcher.fetch_batch(top_urls, max_length=req.max_content_length * 2, force_stealth=req.force_stealth)

    for i, fr in enumerate(fetched):
        if i >= len(ranked):
            break
        if fr.content_type != "error":
            ranked[i].raw_content = fr.content
            ranked[i].content_type = fr.content_type
            ranked[i].fetch_method = fr.fetch_method
            ranked[i].fetch_time_ms = fr.fetch_time_ms
            ranked[i].local_path = fr.local_path
            ranked[i].byte_size = fr.byte_size
            # Pass through discovered images (normalized)
            if fr.images:
                ranked[i].images = [_normalize_image(img) for img in fr.images]

    # ── Stage 4: Algorithmic de-noising (deep only) ──
    for r in ranked[:fetch_count]:
        if r.raw_content and r.content_type == "text":
            cleaned, truncated = clean_content(r.raw_content, req.max_content_length)
            r.content = cleaned
            r.truncated = truncated
        elif r.raw_content:
            r.content = r.raw_content

    results = ranked[:req.max_results]
    elapsed = int((time.monotonic() - t0) * 1000)

    fetched_ok = sum(1 for r in ranked[:fetch_count] if r.fetch_method)
    log.info(
        "search[deep]: query=%r intent=%s results=%d fetched=%d/%d time=%dms",
        req.query, intent, len(results), fetched_ok, fetch_count, elapsed,
    )

    top_images = (await _collect_top_images(results)) if req.categories == "images" else []
    return SearchResponse(
        query=req.query,
        intent=intent,
        results=[_to_search_result(r) for r in results],
        images=top_images,
        total=len(results),
        depth="deep",
        query_time_ms=elapsed,
    )


# ---------------------------------------------------------------------------
# Extract pipeline
# ---------------------------------------------------------------------------

async def extract_pipeline(req: ExtractRequest) -> ExtractResponse:
    """
    Extract content from known URLs via Stage 3 (fetch) + Stage 4 (clean).
    """
    if not req.urls:
        return ExtractResponse(results=[])

    # Stage 3: Fetch
    fetched = await fetcher.fetch_batch(
        req.urls[:5],
        max_length=req.max_length * 2,
        force_stealth=req.force_stealth,
    )

    # Stage 4: Clean + build response
    results: list[ExtractResult] = []
    for fr in fetched:
        content = fr.content
        truncated = fr.truncated

        if fr.content_type == "text" and content:
            content, truncated = clean_content(content, req.max_length)

        images = [_normalize_image(img) for img in (fr.images or [])]

        results.append(ExtractResult(
            url=fr.url,
            title=fr.title,
            content=content,
            content_type=fr.content_type,
            local_path=fr.local_path,
            images=images,
            byte_size=fr.byte_size,
            truncated=truncated,
            fetch_method=fr.fetch_method,
            fetch_time_ms=fr.fetch_time_ms,
        ))

    return ExtractResponse(results=results)
