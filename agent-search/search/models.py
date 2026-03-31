"""
Data models for the Agent Search pipeline.
"""

from __future__ import annotations

from typing import Optional
from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Internal pipeline model
# ---------------------------------------------------------------------------

class SearchResult(BaseModel):
    """A single search result flowing through the pipeline."""

    title: str = ""
    url: str = ""
    snippet: str = ""
    score: float = 0.0
    engines: list[str] = Field(default_factory=list)
    published_date: Optional[str] = None
    category: str = "general"
    image_url: Optional[str] = None
    thumbnail_url: Optional[str] = None

    # Set by prefilter
    composite_score: float = 0.0

    # Set by LLM reranker
    relevance: float = 0.0
    density: float = 0.0
    authority: float = 0.0
    freshness_score: float = 0.0
    rerank_score: float = 0.0
    rerank_reason: str = ""

    # Set by fetcher (deep-crawled content)
    content: Optional[str] = None


# ---------------------------------------------------------------------------
# API request models
# ---------------------------------------------------------------------------

class SearchRequest(BaseModel):
    query: str
    limit: int = 10
    categories: str = "general"
    time_range: Optional[str] = None


class FetchRequest(BaseModel):
    url: str
    max_length: int = 50_000


class SearchAndFetchRequest(BaseModel):
    query: str
    limit: int = 10
    fetch_top: int = 3
    categories: str = "general"
    time_range: Optional[str] = None
    max_content_length: int = 30_000


# ---------------------------------------------------------------------------
# API response models
# ---------------------------------------------------------------------------

class SearchResponse(BaseModel):
    results: list[SearchResult]
    total: int = 0
    intent: str = "general"
    query_time_ms: int = 0


class FetchResponse(BaseModel):
    url: str
    title: str = ""
    content: str = ""
    content_type: str = "text"
    byte_size: int = 0
    truncated: bool = False
    fetch_time_ms: int = 0


class SearchAndFetchResponse(BaseModel):
    results: list[SearchResult]
    total: int = 0
    fetched: int = 0
    intent: str = "general"
    query_time_ms: int = 0
