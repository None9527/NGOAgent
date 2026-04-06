"""
Stage 2 — LLM Listwise Reranker.

Single LLM call scores ~10 pre-filtered results and classifies query intent.
This is the ONLY LLM call in the entire pipeline.

Output per result:
  - score: 0.0-1.0 normalized relevance
  - reason: why this result matters for the query
  - result_type: article|code|docs|news|forum|media|academic

Output per query:
  - intent: factual|tutorial|comparison|news|research|troubleshoot
"""

import asyncio
import json
import logging
from typing import Any

import httpx

from config import LLM_URL, LLM_API_KEY, LLM_MODEL
from search.models import InternalResult

log = logging.getLogger("agent-search.reranker")


# ---------------------------------------------------------------------------
# Prompt
# ---------------------------------------------------------------------------

_SYSTEM = """\
You are a search result relevance evaluator.
Given a query and numbered search results, evaluate each one.

For each result, provide:
1. score — a relevance score from 0.0 to 1.0 (normalized, not integers)
2. reason — a brief explanation of why this result is relevant (or not)
3. result_type — classify the content: article|code|docs|news|forum|media|academic

Also classify the overall query intent as ONE of:
factual, tutorial, comparison, news, research, troubleshoot

CRITICAL LANGUAGE RULE:
- If the query is in Chinese, results in English or other non-Chinese languages
  MUST be scored ≤0.3, unless they are the ONLY available results.
- Chinese query → Chinese results should get the highest scores.
- If no Chinese results exist, score the best available results fairly but cap at 0.6.

Respond with ONLY valid JSON. No markdown fences, no explanation."""


def _build_user_prompt(query: str, results: list[InternalResult]) -> str:
    items: list[str] = []
    for i, r in enumerate(results):
        engines_str = ", ".join(r.engines) if r.engines else "unknown"
        date_str = r.published_date or "unknown"
        items.append(
            f"[{i}] {r.title}\n"
            f"    URL: {r.url}\n"
            f"    Domain: {r.domain}\n"
            f"    Snippet: {r.snippet[:200]}\n"
            f"    Engines: {engines_str} | Date: {date_str}"
        )

    return (
        f"Query: {query}\n\n"
        f"Results:\n" + "\n".join(items) + "\n\n"
        f"Respond in this exact JSON format:\n"
        f'{{"intent":"<intent>",'
        f'"rankings":[{{"index":0,"score":0.85,'
        f'"reason":"brief explanation",'
        f'"result_type":"docs"}},...]}}' 
    )


# ---------------------------------------------------------------------------
# Core reranking logic
# ---------------------------------------------------------------------------

async def _call_llm(query: str, results: list[InternalResult]) -> dict[str, Any]:
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

    if content.startswith("```"):
        lines = content.split("\n")
        lines = [l for l in lines if not l.startswith("```")]
        content = "\n".join(lines).strip()

    return json.loads(content)


def _apply_scores(
    results: list[InternalResult],
    rankings: list[dict],
) -> list[InternalResult]:
    """Apply LLM scores to results. Simple: direct score, no weight profiles."""
    score_map: dict[int, dict] = {}
    for rank in rankings:
        idx = rank.get("index", -1)
        if 0 <= idx < len(results):
            score_map[idx] = rank

    for i, r in enumerate(results):
        if i in score_map:
            s = score_map[i]
            raw_score = float(s.get("score", 0))
            # Clamp to 0-1 range (handle models that output 0-10)
            if raw_score > 1.0:
                raw_score = raw_score / 10.0
            r.rerank_score = max(0.0, min(1.0, raw_score))
            r.reason = s.get("reason", "")
            r.result_type = s.get("result_type", "article")
        else:
            # Not scored by LLM — normalize composite as fallback
            r.rerank_score = min(r.composite_score / 3.0, 1.0)

    results.sort(key=lambda r: r.rerank_score, reverse=True)
    return results


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

async def rerank(
    query: str,
    results: list[InternalResult],
    timeout_s: float = 120.0,
) -> tuple[list[InternalResult], str]:
    """
    LLM listwise reranking with graceful degradation.

    Returns (reranked_results, intent).
    On any failure, returns the original order with intent="general".
    """
    if not results:
        return results, "general"

    def _fallback():
        for r in results:
            r.rerank_score = min(r.composite_score / 3.0, 1.0)
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

        reranked = _apply_scores(results, rankings)
        log.info(
            "LLM reranked %d results, intent=%s, top=%s (score=%.2f)",
            len(reranked), intent,
            reranked[0].title[:40] if reranked else "?",
            reranked[0].rerank_score if reranked else 0,
        )
        return reranked, intent

    except asyncio.TimeoutError:
        log.warning("LLM rerank timed out after %.1fs, using fallback", timeout_s)
        return _fallback()

    except json.JSONDecodeError as e:
        log.warning("LLM rerank JSON parse error: %s, using fallback", e)
        return _fallback()

    except Exception as e:
        log.error("LLM rerank unexpected error: %s", e)
        return _fallback()
