"""
Stage 2 — LLM Listwise Reranker.

Replaces the TEI bge-reranker-v2-m3 container with a single LLM call.

Advantages over the cross-encoder:
- 4-dimension scoring (relevance / density / authority / freshness)
- Query intent classification → dynamic weight adjustment
- Explainable: each result gets a reason string
- Graceful degradation: timeout/error falls back to Stage 1 ordering

The LLM scores 10 pre-filtered results in one call (~500ms, ~1k tokens).
"""

import asyncio
import json
import logging
from typing import Any

import httpx

from config import LLM_URL, LLM_API_KEY, LLM_MODEL
from search.models import SearchResult

log = logging.getLogger("agent-search.reranker")

# ---------------------------------------------------------------------------
# Intent-aware weight profiles
# ---------------------------------------------------------------------------

INTENT_WEIGHTS: dict[str, dict[str, float]] = {
    "factual":      {"relevance": 0.5, "density": 0.2, "authority": 0.2, "freshness": 0.1},
    "tutorial":     {"relevance": 0.4, "density": 0.3, "authority": 0.2, "freshness": 0.1},
    "comparison":   {"relevance": 0.4, "density": 0.3, "authority": 0.1, "freshness": 0.2},
    "news":         {"relevance": 0.3, "density": 0.1, "authority": 0.2, "freshness": 0.4},
    "research":     {"relevance": 0.3, "density": 0.3, "authority": 0.3, "freshness": 0.1},
    "troubleshoot": {"relevance": 0.5, "density": 0.2, "authority": 0.1, "freshness": 0.2},
}

# ---------------------------------------------------------------------------
# Prompts
# ---------------------------------------------------------------------------

_SYSTEM = """\
You are a search result relevance evaluator.
Given a query and numbered search results, score each on 4 dimensions (0-10):
1. relevance — how directly does this answer the query?
2. density   — does the snippet suggest rich, detailed content?
3. authority — is this an authoritative source for the topic?
4. freshness — is the information current?

Also classify the query intent as ONE of:
factual, tutorial, comparison, news, research, troubleshoot

Respond with ONLY valid JSON. No markdown fences, no explanation."""


def _build_user_prompt(query: str, results: list[SearchResult]) -> str:
    items: list[str] = []
    for i, r in enumerate(results):
        engines_str = ", ".join(r.engines) if r.engines else "unknown"
        date_str = r.published_date or "unknown"
        items.append(
            f"[{i}] {r.title}\n"
            f"    URL: {r.url}\n"
            f"    Snippet: {r.snippet[:200]}\n"
            f"    Engines: {engines_str} | Date: {date_str}"
        )

    return (
        f"Query: {query}\n\n"
        f"Results:\n" + "\n".join(items) + "\n\n"
        f"Respond in this exact JSON format:\n"
        f'{{"intent":"<intent>",'
        f'"rankings":[{{"index":0,"relevance":8,"density":7,'
        f'"authority":9,"freshness":6,"reason":"brief"}},...]}}'
    )


# ---------------------------------------------------------------------------
# Core reranking logic
# ---------------------------------------------------------------------------

async def _call_llm(query: str, results: list[SearchResult]) -> dict[str, Any]:
    """Single LLM call to rerank results."""
    messages = [
        {"role": "system", "content": _SYSTEM},
        {"role": "user", "content": _build_user_prompt(query, results)},
    ]

    async with httpx.AsyncClient(timeout=120) as client:
        resp = await client.post(
            f"{LLM_URL}/chat/completions",
            json={
                "model": LLM_MODEL,
                "messages": messages,
                "temperature": 0.1,
                "max_tokens": 2048,
            },
            headers={"Authorization": f"Bearer {LLM_API_KEY}"},
        )
        resp.raise_for_status()
        data = resp.json()

    content = data["choices"][0]["message"]["content"].strip()

    # Strip markdown code fences if the model wraps its response
    if content.startswith("```"):
        lines = content.split("\n")
        lines = [l for l in lines if not l.startswith("```")]
        content = "\n".join(lines).strip()

    return json.loads(content)


def _apply_scores(
    results: list[SearchResult],
    rankings: list[dict],
    intent: str,
) -> list[SearchResult]:
    """Apply LLM scores to results using intent-aware weights."""
    weights = INTENT_WEIGHTS.get(intent, INTENT_WEIGHTS["factual"])

    score_map: dict[int, dict] = {}
    for rank in rankings:
        idx = rank.get("index", -1)
        if 0 <= idx < len(results):
            score_map[idx] = rank

    for i, r in enumerate(results):
        if i in score_map:
            s = score_map[i]
            r.relevance = float(s.get("relevance", 0))
            r.density = float(s.get("density", 0))
            r.authority = float(s.get("authority", 0))
            r.freshness_score = float(s.get("freshness", 0))
            r.rerank_reason = s.get("reason", "")
            r.rerank_score = (
                r.relevance * weights["relevance"]
                + r.density * weights["density"]
                + r.authority * weights["authority"]
                + r.freshness_score * weights["freshness"]
            )
        else:
            # Result not scored by LLM — use composite as fallback
            r.rerank_score = r.composite_score

    results.sort(key=lambda r: r.rerank_score, reverse=True)
    return results


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

async def rerank(
    query: str,
    results: list[SearchResult],
    timeout_s: float = 120.0,
) -> tuple[list[SearchResult], str]:
    """
    LLM listwise reranking with graceful degradation.

    Returns (reranked_results, intent).
    On any failure, returns the original order with intent="general".
    """
    if not results:
        return results, "general"

    def _fallback():
        for r in results:
            r.rerank_score = r.composite_score
        return results, "general"

    try:
        parsed = await asyncio.wait_for(
            _call_llm(query, results),
            timeout=timeout_s,
        )
        intent = parsed.get("intent", "general")
        rankings = parsed.get("rankings", [])

        if not rankings:
            log.warning("LLM returned empty rankings, using fallback")
            return _fallback()

        reranked = _apply_scores(results, rankings, intent)
        log.info(
            "LLM reranked %d results, intent=%s, top=%s (score=%.2f)",
            len(reranked), intent,
            reranked[0].title[:40] if reranked else "?",
            reranked[0].rerank_score if reranked else 0,
        )
        return reranked, intent

    except asyncio.TimeoutError:
        log.warning("LLM rerank timed out after %.1fs, using algorithmic order", timeout_s)
        return _fallback()

    except json.JSONDecodeError as e:
        log.warning("LLM rerank JSON parse error: %s, using fallback", e)
        return _fallback()

    except Exception as e:
        log.error("LLM rerank unexpected error: %s", e)
        return _fallback()
