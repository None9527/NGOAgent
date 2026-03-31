"""
Page fetcher — High-Low Mix Architecture for Deep Crawl & CF Bypass.
Layer 1: curl_cffi for ultra-fast TLS/HTTP2 spoofing.
Layer 2: Camoufox (Playwright) for extreme Headless/Turnstile unblocking.
"""

import asyncio
import logging
import time
import re
from pathlib import Path

from curl_cffi import requests as curl_requests
from camoufox.async_api import AsyncCamoufox

from config import FETCH_TIMEOUT_S, MAX_CONTENT_LENGTH
from search.models import FetchResponse

log = logging.getLogger("agent-search.fetcher")

# Create persistent profile directory for Camoufox
PROFILE_DIR = Path("data/profile").absolute()
PROFILE_DIR.mkdir(parents=True, exist_ok=True)

def _extract_text_from_html(html: str) -> str:
    """Fast extraction of plaintext from HTML for Layer 1 fallback."""
    if not html:
        return ""
    # Remove script and style elements
    text = re.sub(r'<(script|style)[^>]*>[\s\S]*?</\1>', ' ', html, flags=re.IGNORECASE)
    # Remove HTML tags
    text = re.sub(r'<[^>]+>', ' ', text)
    # Condense whitespace
    text = re.sub(r'\s+', ' ', text).strip()
    return text

def _is_cloudflare_blocked(status_code: int, html: str) -> bool:
    """Detect if the page is currently blocked by Cloudflare Turnstile or similar."""
    if status_code in (403, 503):
        return True
    html_lower = html.lower()
    if "just a moment" in html_lower and "cloudflare" in html_lower:
        return True
    if "cf-turnstile" in html_lower or "cf-browser-verification" in html_lower:
        return True
    if "datadome" in html_lower and "captcha" in html_lower:
        return True
    return False

async def fetch_layer1_curl(url: str, max_length: int) -> tuple[bool, FetchResponse]:
    """
    Layer 1: Fast HTTP/TLS Spoofing. Fast but vulnerable to active JS checks.
    Returns (needs_fallback: bool, response: FetchResponse)
    """
    t0 = time.monotonic()
    try:
        async with curl_requests.AsyncSession(impersonate="chrome") as session:
            resp = await session.get(url, timeout=FETCH_TIMEOUT_S)
            
            # Check for binary image response directly (fast-path for images)
            content_type = resp.headers.get("content-type", "").lower()
            if content_type.startswith("image/"):
                # If it's a real image, we just flag success to bypass text filtering
                elapsed_ms = int((time.monotonic() - t0) * 1000)
                return False, FetchResponse(
                    url=url, title="[Binary Image]", content="[Binary Image Content]",
                    content_type="image", byte_size=len(resp.content),
                    truncated=False, fetch_time_ms=elapsed_ms
                )

            html = resp.text
            
            # Determine if blocked
            if _is_cloudflare_blocked(resp.status_code, html):
                return True, None

            # Attempt extraction
            title_match = re.search(r'<title[^>]*>(.*?)</title>', html, re.IGNORECASE)
            title = title_match.group(1).strip() if title_match else ""
            
            body = _extract_text_from_html(html)
            truncated = False
            if len(body) > max_length:
                body = body[:max_length]
                truncated = True
                
            elapsed_ms = int((time.monotonic() - t0) * 1000)
            log.info("L1 Success: %s in %dms", url, elapsed_ms)
            return False, FetchResponse(
                url=url,
                title=title,
                content=body,
                content_type="text",
                byte_size=len(body),
                truncated=truncated,
                fetch_time_ms=elapsed_ms
            )

    except asyncio.TimeoutError:
        log.warning("L1 Timeout: %s", url)
        return True, None
    except Exception as e:
        log.warning("L1 Error for %s: %s", url, e)
        return True, None

async def fetch_layer2_camoufox(url: str, max_length: int) -> FetchResponse:
    """
    Layer 2: Heavy-duty Stealth Browser with full JS / Canvas spoofing.
    Used for bypassing Turnstile and rigid protections.
    """
    t0 = time.monotonic()
    try:
        async with AsyncCamoufox(
            headless=True,
            persistent_context=True,
            user_data_dir=str(PROFILE_DIR)
        ) as browser:
            page = await browser.new_page()
            
            # Wait for content to load, handling initial redirects/challenges
            await page.goto(url, wait_until="domcontentloaded", timeout=FETCH_TIMEOUT_S * 1000)
            
            # Wait loop to clear Cloudflare Turnstile if present
            for _ in range(3):
                content = await page.content()
                if "cf-turnstile" not in content and "Just a moment" not in content:
                    break
                await asyncio.sleep(2)
                
            # Extra wait for any target site dynamically injected elements
            await asyncio.sleep(1)
            
            title = await page.title()
            
            # Native JS text extraction
            body = await page.evaluate("document.body.innerText")
            if not body:
                body = await page.evaluate("document.documentElement.innerText") or ""
                
            truncated = False
            if len(body) > max_length:
                body = body[:max_length]
                truncated = True
                
            elapsed_ms = int((time.monotonic() - t0) * 1000)
            log.info("L2 Success: %s in %dms", url, elapsed_ms)
            return FetchResponse(
                url=url,
                title=title,
                content=body,
                content_type="text",
                byte_size=len(body),
                truncated=truncated,
                fetch_time_ms=elapsed_ms
            )
            
    except Exception as e:
        elapsed_ms = int((time.monotonic() - t0) * 1000)
        log.error("L2 Error for %s: %s", url, e)
        return FetchResponse(
            url=url,
            title="",
            content=f"[Fetch error: Layer2 failed - {e}]",
            content_type="error",
            fetch_time_ms=elapsed_ms
        )

async def fetch_page(
    url: str,
    *,
    session: str = "default",  # kept for compatibility with existing caller API params
    max_length: int = MAX_CONTENT_LENGTH,
) -> FetchResponse:
    """
    Fetch a single page using High-Low Mix dual engines.
    """
    # Try Layer 1 (curl_cffi)
    needs_fallback, l1_resp = await fetch_layer1_curl(url, max_length)
    if not needs_fallback and l1_resp:
        return l1_resp
        
    # Fallback Layer 2 (camoufox)
    log.info("L1 Blocked or Failed. Escalating URL %s to Layer 2 (Camoufox)...", url)
    return await fetch_layer2_camoufox(url, max_length)


async def fetch_pages(
    urls: list[str],
    *,
    max_length: int = MAX_CONTENT_LENGTH,
) -> list[FetchResponse]:
    """
    Fetch multiple pages concurrently using High-Low Mix routing.
    """
    if not urls:
        return []

    tasks = [
        fetch_page(url, max_length=max_length)
        for url in urls
    ]

    results = await asyncio.gather(*tasks, return_exceptions=True)

    final: list[FetchResponse] = []
    for i, r in enumerate(results):
        if isinstance(r, Exception):
            log.error("Fetch task %d failed: %s", i, r)
            final.append(FetchResponse(
                url=urls[i],
                content=f"[Concurrent fetch error: {r}]",
                content_type="error",
            ))
        else:
            final.append(r)

    log.info("Fetched %d pages concurrently, %d succeeded",
             len(urls), sum(1 for r in final if r.content_type != "error"))
    return final
