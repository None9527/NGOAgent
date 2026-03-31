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
LLM_API_KEY = os.getenv("LLM_API_KEY", "")
LLM_MODEL = os.getenv("LLM_MODEL", "qwen-turbo")

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
