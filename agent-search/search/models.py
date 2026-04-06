"""
Data models for the Agent Search pipeline.

API-facing models (SearchRequest/Response, ExtractRequest/Response)
and internal pipeline model (InternalResult).
"""

from __future__ import annotations

from typing import Literal, Optional
from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Shared sub-models
# ---------------------------------------------------------------------------

class ImageResult(BaseModel):
    """A discovered image asset."""
    url: str = ""
    local_path: Optional[str] = None
    description: str = ""
    byte_size: int = 0


# ---------------------------------------------------------------------------
# Internal pipeline model (NOT exposed via API)
# ---------------------------------------------------------------------------

class InternalResult(BaseModel):
    """
    Internal representation flowing through the pipeline stages.
    Carries raw signals that get transformed into the clean SearchResult.
    """
    title: str = ""
    url: str = ""
    raw_url: str = ""
    snippet: str = ""
    score: float = 0.0
    engines: list[str] = Field(default_factory=list)
    published_date: Optional[str] = None
    category: str = "general"
    image_url: Optional[str] = None
    thumbnail_url: Optional[str] = None

    # Stage 1: prefilter
    domain: str = ""
    composite_score: float = 0.0

    # Stage 2: reranker
    rerank_score: float = 0.0
    reason: str = ""
    result_type: str = "article"

    # Stage 3: fetcher
    raw_content: Optional[str] = None
    content_type: str = "text"
    fetch_method: Optional[str] = None
    fetch_time_ms: Optional[int] = None
    local_path: Optional[str] = None
    byte_size: int = 0

    # Stage 4: cleaner
    content: Optional[str] = None
    truncated: bool = False
    images: list[ImageResult] = Field(default_factory=list)


# ---------------------------------------------------------------------------
# API: SearchResult (agent-facing, zero redundancy)
# ---------------------------------------------------------------------------

class SearchResult(BaseModel):
    """Every field is useful to the agent. Zero internal pipeline noise."""

    # Identity
    title: str = ""
    url: str = ""
    domain: str = ""
    published_date: Optional[str] = None

    # Relevance (from Stage 2 LLM reranker — the only LLM output)
    score: float = 0.0
    reason: str = ""
    result_type: str = "article"

    # Preview (always present, zero cost)
    snippet: str = ""

    # Deep content (only depth=deep, algorithmically de-noised)
    content: Optional[str] = None
    fetch_method: Optional[str] = None
    fetch_time_ms: Optional[int] = None
    truncated: bool = False

    # Assets (binary resources)
    local_path: Optional[str] = None
    images: list[ImageResult] = Field(default_factory=list)


# ---------------------------------------------------------------------------
# API request models
# ---------------------------------------------------------------------------

class SearchRequest(BaseModel):
    query: str
    depth: Literal["quick", "standard", "deep"] = "standard"
    max_results: int = 5
    fetch_top: int = 3
    categories: str = "general"
    time_range: Optional[str] = None
    include_images: bool = False
    max_content_length: int = 8000
    force_stealth: bool = False


class ExtractRequest(BaseModel):
    urls: list[str]
    max_length: int = 10000
    force_stealth: bool = False


# ---------------------------------------------------------------------------
# API response models
# ---------------------------------------------------------------------------

class SearchResponse(BaseModel):
    query: str = ""
    intent: str = "general"
    results: list[SearchResult] = Field(default_factory=list)
    images: list[ImageResult] = Field(default_factory=list)
    total: int = 0
    depth: str = "standard"
    query_time_ms: int = 0


class ExtractResult(BaseModel):
    url: str
    title: str = ""
    content: Optional[str] = None
    content_type: str = "text"
    local_path: Optional[str] = None
    images: list[ImageResult] = Field(default_factory=list)
    byte_size: int = 0
    truncated: bool = False
    fetch_method: str = "direct"
    fetch_time_ms: int = 0


class ExtractResponse(BaseModel):
    results: list[ExtractResult] = Field(default_factory=list)


# ---------------------------------------------------------------------------
# Backward compat aliases (will remove after Go-side migration)
# ---------------------------------------------------------------------------

FetchRequest = ExtractRequest
FetchResponse = ExtractResult
