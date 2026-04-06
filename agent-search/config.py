"""
Agent Search Service — Configuration.
All settings via environment variables with sane defaults.
"""

import os

SEARXNG_URL = os.getenv("SEARXNG_URL", "http://localhost:8080")

LLM_URL = os.getenv(
    "LLM_URL",
    "https://dashscope.aliyuncs.com/compatible-mode/v1",
)
LLM_API_KEY = os.getenv("LLM_API_KEY") or os.getenv("DASHSCOPE_API_KEY", "")
LLM_MODEL = os.getenv("LLM_MODEL", "qwen-turbo")

# Asset cache — binary downloads (images, video, audio) land here instead of being decoded to text
# Structure: data/cache/{type}/{YYYY-MM}/{sha256[:16]}_{filename}.{ext}
CACHE_DIR = os.getenv("CACHE_DIR", "data/cache")

# Prefilter tuning
MAX_RESULTS_FROM_SEARXNG = 30
PREFILTER_KEEP = 10
MAX_PER_DOMAIN = 2

# Fetcher
FETCH_TIMEOUT_S = 30
MAX_CONTENT_LENGTH = 50_000

# High-authority domains — composite score ×1.2
HIGH_AUTHORITY_DOMAINS: set[str] = {
    "github.com",
    "stackoverflow.com",
    "arxiv.org",
    "docs.python.org",
    "go.dev",
    "developer.mozilla.org",
    "en.wikipedia.org",
    "zh.wikipedia.org",
    "learn.microsoft.com",
    "pytorch.org",
    "huggingface.co",
    "openai.com",
    "rust-lang.org",
    "doc.rust-lang.org",
}

# Low-quality content farms — composite score ×0.5
LOW_QUALITY_DOMAINS: set[str] = {
    "pinterest.com",
}

# ---------------------------------------------------------------------------
# Startup validation
# ---------------------------------------------------------------------------
import logging as _logging
_log = _logging.getLogger("agent-search.config")

if not LLM_API_KEY:
    _log.warning(
        "⚠ LLM_API_KEY / DASHSCOPE_API_KEY not set — "
        "LLM reranker (Stage 2) will fail, search will degrade to prefilter-only"
    )
