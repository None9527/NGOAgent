"""
Stage 1 — Algorithmic pre-filter.

Reduces SearXNG results from ~30 to ~10 using pure heuristics.
No LLM calls, runs in <1ms.

Signals used:
- SearXNG fusion score
- Multi-engine consensus (more engines = more trustworthy)
- Domain diversity (max N per domain)
- URL normalisation + dedup
- Freshness weighting (recent content boosted)
- Domain quality lists (authority boost / content-farm penalty)
"""

import logging
from datetime import datetime, timezone
from urllib.parse import urlparse, urlunparse, parse_qs, urlencode

from config import (
    PREFILTER_KEEP,
    MAX_PER_DOMAIN,
    HIGH_AUTHORITY_DOMAINS,
    LOW_QUALITY_DOMAINS,
)
from search.models import SearchResult

log = logging.getLogger("agent-search.prefilter")

# Tracking parameters to strip during URL normalisation
_TRACKING_PARAMS = {
    "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
    "ref", "fbclid", "gclid", "mc_cid", "mc_eid",
}


def _normalise_url(url: str) -> str:
    """Strip tracking params and trailing slashes for dedup."""
    parsed = urlparse(url)
    params = parse_qs(parsed.query, keep_blank_values=False)
    clean = {k: v for k, v in params.items() if k.lower() not in _TRACKING_PARAMS}
    clean_query = urlencode(clean, doseq=True) if clean else ""
    normalised = urlunparse((
        parsed.scheme,
        parsed.hostname or "",
        parsed.path.rstrip("/") or "/",
        parsed.params,
        clean_query,
        "",  # drop fragment
    ))
    return normalised


def _extract_domain(url: str) -> str:
    """Get the registrable domain from a URL."""
    host = urlparse(url).hostname or ""
    parts = host.split(".")
    if len(parts) > 2:
        return ".".join(parts[-2:])
    return host


def _freshness_boost(published_date: str | None) -> float:
    """
    Boost recently published content.

    Returns a multiplier (0.85–1.3).
    """
    if not published_date:
        return 1.0
    try:
        dt = datetime.fromisoformat(published_date.replace("Z", "+00:00"))
        days = (datetime.now(timezone.utc) - dt).days
    except (ValueError, TypeError):
        return 1.0

    if days < 7:
        return 1.3
    if days < 30:
        return 1.15
    if days < 365:
        return 1.0
    return 0.85


def _compute_composite(r: SearchResult) -> float:
    """
    Composite score combining all algorithmic signals.

    composite = searxng_score × engine_boost × freshness × domain_quality
    """
    base = max(r.score, 0.1)  # Avoid zero-score results disappearing

    # Multi-engine consensus: each extra engine adds 15%
    engine_count = len(r.engines)
    engine_boost = 1.0 + max(engine_count - 1, 0) * 0.15

    # Freshness
    freshness = _freshness_boost(r.published_date)

    # Domain quality
    domain = _extract_domain(r.url)
    domain_boost = 1.0
    if domain in HIGH_AUTHORITY_DOMAINS:
        domain_boost = 1.2
    elif domain in LOW_QUALITY_DOMAINS:
        domain_boost = 0.5

    return base * engine_boost * freshness * domain_boost


def prefilter(query: str, results: list[SearchResult]) -> list[SearchResult]:
    """
    Algorithmic pre-filter: 30 results → top PREFILTER_KEEP.

    Steps:
    1. URL normalisation + dedup
    2. Domain diversity enforcement
    3. Composite scoring
    4. Sort + truncate
    """
    if not results:
        return results

    # 1. URL dedup
    seen_urls: set[str] = set()
    deduped: list[SearchResult] = []
    for r in results:
        norm = _normalise_url(r.url)
        if norm not in seen_urls:
            seen_urls.add(norm)
            deduped.append(r)

    # 2. Domain diversity — keep at most MAX_PER_DOMAIN per domain
    domain_count: dict[str, int] = {}
    diverse: list[SearchResult] = []
    for r in deduped:
        domain = _extract_domain(r.url)
        count = domain_count.get(domain, 0)
        if count < MAX_PER_DOMAIN:
            diverse.append(r)
            domain_count[domain] = count + 1

    # 3. Composite scoring
    for r in diverse:
        r.composite_score = _compute_composite(r)

    # 4. Sort + truncate
    diverse.sort(key=lambda r: r.composite_score, reverse=True)
    kept = diverse[:PREFILTER_KEEP]

    log.info(
        "Prefilter: %d → %d → %d (dedup → diversity → top-k)",
        len(results), len(diverse), len(kept),
    )
    return kept
