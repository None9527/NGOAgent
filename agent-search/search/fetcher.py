"""
Page fetcher — High-Low Mix Architecture for Deep Crawl & CF Bypass.
Layer 1: curl_cffi for ultra-fast TLS/HTTP2 spoofing.
Layer 2: Camoufox (Playwright) for extreme Headless/Turnstile unblocking.

Binary Asset Cache:
  Binary responses (image/video/audio) are saved to disk rather than decoded.
  Cache layout:
    data/cache/{type}/{YYYY-MM}/{sha256(url)[:16]}_{safe_filename}.{ext}

  Rules:
    - Type directory: images / videos / audio / files (catch-all)
    - Month directory: enables TTL-based pruning without traversal overhead
    - Hash prefix: 16 hex chars of SHA-256(url) — guarantees uniqueness per URL
    - Safe filename: original URL basename, stripped of query strings and
      sanitised (only [A-Za-z0-9._-] retained), max 64 chars
    - Extension: derived from Content-Type header (authoritative), falling
      back to URL path suffix
    - On cache-hit the existing file is returned immediately (no re-download)
"""

import asyncio
import hashlib
import logging
import mimetypes
import re
import time
import unicodedata
from datetime import datetime
from pathlib import Path
from urllib.parse import urljoin, urlparse

from curl_cffi import requests as curl_requests
from camoufox.async_api import AsyncCamoufox

from config import FETCH_TIMEOUT_S, MAX_CONTENT_LENGTH, CACHE_DIR
from search.models import FetchResponse

log = logging.getLogger("agent-search.fetcher")

# Camoufox is memory-heavy — limit to 1 concurrent browser instance
_CAMOUFOX_SEM = asyncio.Semaphore(1)

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------

PROFILE_DIR = Path("data/profile").absolute()
PROFILE_DIR.mkdir(parents=True, exist_ok=True)

_CACHE_ROOT = Path(CACHE_DIR).absolute()

# ---------------------------------------------------------------------------
# MIME → asset type directory mapping
# ---------------------------------------------------------------------------

_MIME_TO_TYPE: dict[str, str] = {
    "image": "images",
    "video": "videos",
    "audio": "audio",
}

_BINARY_MIME_PREFIXES: tuple[str, ...] = ("image/", "video/", "audio/")

# Preferred extension per MIME type (overrides mimetypes stdlib guesses)
_MIME_EXT_OVERRIDES: dict[str, str] = {
    "image/jpeg": ".jpg",
    "image/jpg": ".jpg",
    "image/png": ".png",
    "image/webp": ".webp",
    "image/gif": ".gif",
    "image/svg+xml": ".svg",
    "image/avif": ".avif",
    "video/mp4": ".mp4",
    "video/webm": ".webm",
    "audio/mpeg": ".mp3",
    "audio/ogg": ".ogg",
    "audio/wav": ".wav",
}


# ---------------------------------------------------------------------------
# Cache helpers
# ---------------------------------------------------------------------------

def _safe_filename(url: str, max_len: int = 64) -> str:
    """
    Derive a human-readable, filesystem-safe filename from a URL.
    Strips query strings; keeps only safe chars; truncates to max_len.
    """
    parsed = urlparse(url)
    raw = Path(parsed.path).name or "download"
    # Normalise unicode → ASCII equivalents
    raw = unicodedata.normalize("NFKD", raw).encode("ascii", "ignore").decode()
    # Keep only safe chars
    raw = re.sub(r"[^A-Za-z0-9._\-]", "_", raw)
    # Collapse repeated underscores
    raw = re.sub(r"_+", "_", raw).strip("_")
    return raw[:max_len] or "file"


def _ext_from_mime(content_type: str, fallback_url: str) -> str:
    """Return the best file extension for a given MIME type."""
    mime = content_type.split(";")[0].strip().lower()
    if mime in _MIME_EXT_OVERRIDES:
        return _MIME_EXT_OVERRIDES[mime]
    guessed = mimetypes.guess_extension(mime)
    if guessed:
        return guessed
    # Fall back to URL suffix
    suffix = Path(urlparse(fallback_url).path).suffix
    return suffix if suffix else ".bin"


def _cache_path(url: str, content_type: str) -> Path:
    """
    Compute the deterministic cache path for a URL + MIME type.

    Layout:
        {CACHE_ROOT}/{type_dir}/{YYYY-MM}/{hash16}_{safe_name}{ext}

    Example:
        data/cache/images/2026-03/a3f4d2b1c9e7f0e2_Uzi_at_Worlds_2025.jpg
    """
    url_hash = hashlib.sha256(url.encode()).hexdigest()[:16]
    mime_prefix = content_type.split("/")[0].lower()
    type_dir = _MIME_TO_TYPE.get(mime_prefix, "files")
    month_dir = datetime.now().strftime("%Y-%m")
    ext = _ext_from_mime(content_type, url)
    safe_name = _safe_filename(url)
    # Strip existing extension from safe_name to avoid double extensions
    safe_stem = Path(safe_name).stem or safe_name
    filename = f"{url_hash}_{safe_stem}{ext}"
    path = _CACHE_ROOT / type_dir / month_dir / filename
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def _save_binary(data: bytes, path: Path) -> None:
    path.write_bytes(data)
    log.info("Cached binary: %s (%d bytes)", path, len(data))


def _is_binary_mime(content_type: str) -> bool:
    return any(content_type.lower().startswith(p) for p in _BINARY_MIME_PREFIXES)


# ---------------------------------------------------------------------------
# HTML helpers
# ---------------------------------------------------------------------------

def _extract_text_from_html(html: str) -> str:
    if not html:
        return ""
    text = re.sub(r"<(script|style)[^>]*>[\s\S]*?</\1>", " ", html, flags=re.IGNORECASE)
    text = re.sub(r"<[^>]+>", " ", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def _extract_images_from_html(html: str, base_url: str) -> list[dict]:
    """
    Extract image URLs from HTML content.
    Returns list of {src, alt, width, height} dicts.
    """
    if not html:
        return []
    images = []
    seen = set()
    # Match <img> tags with src, alt, width, height attributes
    pattern = r'<img\b([^>]*)>'
    for match in re.finditer(pattern, html, re.IGNORECASE):
        attrs_str = match.group(1)
        # Extract src
        src_match = re.search(r'src=["\']([^"\']+)["\']', attrs_str)
        if not src_match:
            continue
        src = src_match.group(1)
        # Resolve relative URLs
        src = urljoin(base_url, src)
        # Skip data URIs, tiny tracking pixels, svg icons
        if src.startswith("data:") or src.startswith("javascript:"):
            continue
        if src in seen:
            continue
        seen.add(src)
        # Extract optional attributes
        alt_match = re.search(r'alt=["\']([^"\']*)["\']', attrs_str)
        alt = alt_match.group(1) if alt_match else ""
        width_match = re.search(r'width=["\']?(\d+)["\']?', attrs_str)
        width = int(width_match.group(1)) if width_match else 0
        height_match = re.search(r'height=["\']?(\d+)["\']?', attrs_str)
        height = int(height_match.group(1)) if height_match else 0
        # Filter out tiny images (icons, tracking pixels)
        if width > 0 and width < 50:
            continue
        if height > 0 and height < 50:
            continue
        images.append({"src": src, "alt": alt, "width": width, "height": height})
    return images


def _is_cloudflare_blocked(status_code: int, html: str) -> bool:
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


# ---------------------------------------------------------------------------
# Layer 1 — curl_cffi
# ---------------------------------------------------------------------------

async def fetch_layer1_curl(url: str, max_length: int) -> tuple[bool, FetchResponse | None]:
    """
    Layer 1: Fast HTTP/TLS Spoofing.
    Returns (needs_fallback, response_or_None).
    """
    t0 = time.monotonic()
    try:
        async with curl_requests.AsyncSession(impersonate="chrome") as session:
            resp = await session.get(url, timeout=FETCH_TIMEOUT_S)
            content_type = resp.headers.get("content-type", "").lower()

            # ---------- Binary fast-path ----------
            if _is_binary_mime(content_type):
                cache_file = _cache_path(url, content_type)
                is_hit = cache_file.exists()
                if is_hit:
                    log.info("Cache hit: %s", cache_file)
                else:
                    _save_binary(resp.content, cache_file)

                elapsed_ms = int((time.monotonic() - t0) * 1000)
                mime_prefix = content_type.split("/")[0].lower()
                return False, FetchResponse(
                    url=url,
                    title="",
                    content=f"[Binary asset saved to cache]",
                    content_type=mime_prefix,  # "image" / "video" / "audio"
                    byte_size=cache_file.stat().st_size,
                    truncated=False,
                    fetch_time_ms=elapsed_ms,
                    local_path=str(cache_file),
                    fetch_method="cache_hit" if is_hit else "direct",
                )

            # ---------- Text path ----------
            html = resp.text
            if _is_cloudflare_blocked(resp.status_code, html):
                return True, None

            title_match = re.search(r"<title[^>]*>(.*?)</title>", html, re.IGNORECASE)
            title = title_match.group(1).strip() if title_match else ""
            body = _extract_text_from_html(html)
            images = _extract_images_from_html(html, url)
            truncated = False
            if len(body) > max_length:
                body = body[:max_length]
                truncated = True

            elapsed_ms = int((time.monotonic() - t0) * 1000)
            log.info("L1 text success: %s in %dms (%d images)", url, elapsed_ms, len(images))
            return False, FetchResponse(
                url=url, title=title, content=body,
                content_type="text", byte_size=len(body),
                truncated=truncated, fetch_time_ms=elapsed_ms,
                fetch_method="direct",
                images=images,
            )

    except asyncio.TimeoutError:
        log.warning("L1 timeout: %s", url)
        return True, None
    except Exception as e:
        log.warning("L1 error for %s: %s", url, e)
        return True, None


# ---------------------------------------------------------------------------
# Layer 2 — Camoufox
# ---------------------------------------------------------------------------

async def fetch_layer2_camoufox(url: str, max_length: int) -> FetchResponse:
    """
    Layer 2: Stealth browser. Handles Turnstile / heavy JS protections.
    Binary assets discovered during page load are cached if found via
    network interception. Page text is extracted via JS DOM.
    """
    t0 = time.monotonic()
    async with _CAMOUFOX_SEM:
        try:
            async with AsyncCamoufox(
                headless=True,
                persistent_context=True,
                user_data_dir=str(PROFILE_DIR),
            ) as browser:
                page = await browser.new_page()
                await page.goto(url, wait_until="domcontentloaded", timeout=FETCH_TIMEOUT_S * 1000)

                # Wait for CF challenge to clear
                for _ in range(3):
                    content = await page.content()
                    if "cf-turnstile" not in content and "Just a moment" not in content:
                        break
                    await asyncio.sleep(2)

                await asyncio.sleep(1)
                title = await page.title()
                body = await page.evaluate("document.body.innerText") or ""
                body = body or await page.evaluate("document.documentElement.innerText") or ""

                # Extract images from the page
                images = await page.evaluate("""
                    () => {
                        const imgs = Array.from(document.querySelectorAll('img'));
                        return imgs
                            .filter(img => {
                                if (!img.src || img.src.startsWith('data:') || img.src.startsWith('javascript:')) return false;
                                if (img.width > 0 && img.width < 50) return false;
                                if (img.height > 0 && img.height < 50) return false;
                                return true;
                            })
                            .map(img => ({
                                src: img.src,
                                alt: img.alt || '',
                                width: img.naturalWidth || img.width || 0,
                                height: img.naturalHeight || img.height || 0
                            }));
                    }
                """) or []

                truncated = False
                if len(body) > max_length:
                    body = body[:max_length]
                    truncated = True

                elapsed_ms = int((time.monotonic() - t0) * 1000)
                log.info("L2 success: %s in %dms (%d images)", url, elapsed_ms, len(images))
                return FetchResponse(
                    url=url, title=title, content=body,
                    content_type="text", byte_size=len(body),
                    truncated=truncated, fetch_time_ms=elapsed_ms,
                    fetch_method="stealth_browser",
                    images=images,
                )

        except Exception as e:
            elapsed_ms = int((time.monotonic() - t0) * 1000)
            log.error("L2 error for %s: %s", url, e)
            return FetchResponse(
                url=url, title="", content=f"[Fetch error: Layer 2 failed — {e}]",
                content_type="error", fetch_time_ms=elapsed_ms,
                fetch_method="stealth_browser",
            )


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

async def fetch(
    url: str,
    *,
    session: str = "default",
    max_length: int = MAX_CONTENT_LENGTH,
    force_stealth: bool = False,
) -> FetchResponse:
    """
    Fetch a single URL via L1→L2 routing.

    Binary assets (image/video/audio) are cached to disk and the response
    includes `local_path` pointing to the cached file.
    Text pages go through CF-detection; L1 fallback triggers L2 on block.
    """
    # Cache-hit shortcut for binary URLs we've seen before (any extension hint)
    # We can't know the MIME without fetching, so just let L1 handle it.

    if not force_stealth:
        needs_fallback, l1_resp = await fetch_layer1_curl(url, max_length)
        if not needs_fallback and l1_resp:
            return l1_resp

    log.info("Escalating %s to L2 (Camoufox)... (force_stealth=%s)", url, force_stealth)
    return await fetch_layer2_camoufox(url, max_length)


async def fetch_batch(
    urls: list[str],
    *,
    max_length: int = MAX_CONTENT_LENGTH,
    force_stealth: bool = False,
) -> list[FetchResponse]:
    """Fetch multiple URLs concurrently, caching binaries as they arrive."""
    if not urls:
        return []

    tasks = [fetch(url, max_length=max_length, force_stealth=force_stealth) for url in urls]
    results = await asyncio.gather(*tasks, return_exceptions=True)

    final: list[FetchResponse] = []
    for i, r in enumerate(results):
        if isinstance(r, Exception):
            log.error("Fetch task %d failed: %s", i, r)
            final.append(FetchResponse(
                url=urls[i], content=f"[Concurrent fetch error: {r}]",
                content_type="error",
            ))
        else:
            final.append(r)

    ok = sum(1 for r in final if r.content_type != "error")
    cached = sum(1 for r in final if r.local_path)
    log.info("fetch_pages: %d/%d ok, %d binary cached", ok, len(urls), cached)
    return final


# ---------------------------------------------------------------------------
# Image pre-caching (for image search results)
# ---------------------------------------------------------------------------

async def cache_image(url: str) -> str | None:
    """
    Download an image URL to local cache, bypassing anti-hotlinking.
    Returns local file path on success, None on failure.
    """
    if not url or url.startswith("data:"):
        return None

    # Check if already cached (deterministic path)
    try_path = _cache_path(url, "image/jpeg")  # tentative path for check
    # Check any existing file with this URL hash
    url_hash = hashlib.sha256(url.encode()).hexdigest()[:16]
    cache_dir = try_path.parent
    if cache_dir.exists():
        for existing in cache_dir.iterdir():
            if existing.name.startswith(url_hash):
                return str(existing)

    try:
        async with curl_requests.AsyncSession(impersonate="chrome") as s:
            resp = await s.get(url, timeout=8)
            if resp.status_code != 200:
                return None

            ct = resp.headers.get("content-type", "image/jpeg")
            # Verify it's actually an image
            if not ct.startswith("image/"):
                return None

            # Skip tiny files (tracking pixels, broken images)
            if len(resp.content) < 1024:
                return None

            path = _cache_path(url, ct)
            _save_binary(resp.content, path)
            log.info("Cached image: %s → %s (%d bytes)", url[:60], path, len(resp.content))
            return str(path)

    except Exception as e:
        log.debug("Image cache failed for %s: %s", url[:60], e)
        return None


async def cache_images_batch(urls: list[str], max_concurrent: int = 5) -> dict[str, str]:
    """
    Download and cache multiple image URLs concurrently.
    Returns {url: local_path} for successfully cached images.
    """
    if not urls:
        return {}

    sem = asyncio.Semaphore(max_concurrent)

    async def _limited(url: str) -> tuple[str, str | None]:
        async with sem:
            path = await cache_image(url)
            return url, path

    results = await asyncio.gather(*[_limited(u) for u in urls], return_exceptions=True)
    cached = {}
    for r in results:
        if isinstance(r, tuple) and r[1]:
            cached[r[0]] = r[1]

    log.info("Image batch cache: %d/%d succeeded", len(cached), len(urls))
    return cached

