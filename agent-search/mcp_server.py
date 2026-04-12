"""
MCP Server for Agent-Search — wraps the search pipeline as MCP tools.

Supports two transport modes:
  - stdio: Standard MCP protocol via stdin/stdout
  - http:  Streamable HTTP transport on configurable port

Tools exposed:
  - web_search(query, max_results, categories, time_range)
  - deep_research(query, max_results, fetch_top, categories, time_range, max_content_length, force_stealth)
  - extract_content(urls, max_length, force_stealth)
  - search_health()
"""

import os
import sys
import time

# Add project root to path so we can import search modules
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from fastmcp import FastMCP

from search.models import SearchRequest, ExtractRequest
from search.pipeline import search_pipeline, extract_pipeline

# ---------------------------------------------------------------------------
# MCP Server instance
# ---------------------------------------------------------------------------
mcp = FastMCP(
    "agent-search",
    instructions="Web search and deep research MCP server powered by SearXNG + LLM reranking",
)


# ---------------------------------------------------------------------------
# Tool: web_search
# ---------------------------------------------------------------------------
@mcp.tool()
async def web_search(
    query: str,
    max_results: int = 5,
    categories: str = "general",
    time_range: str | None = None,
) -> str:
    """Perform a lightweight web search. Returns ranked results without deep content extraction.

    Args:
        query: Search query (required).
        max_results: Max results to return (default 5).
        categories: Search categories comma-separated (default "general").
        time_range: Time filter ("day", "week", "month", "year", or None).
    """
    start = time.monotonic()

    try:
        request = SearchRequest(
            query=query,
            depth="quick",
            max_results=max_results,
            fetch_top=0,
            categories=categories,
            time_range=time_range,
        )
        response = await search_pipeline(request)
        elapsed = int((time.monotonic() - start) * 1000)

        if not response.results:
            return f"No results found for '{query}' ({elapsed}ms)"

        lines = [
            f"Search results for '{query}' ({response.total} results, {response.query_time_ms}ms):",
            "",
        ]
        for i, r in enumerate(response.results, 1):
            lines.append(f"{i}. {r.title}")
            lines.append(f"   URL: {r.url}")
            if r.domain:
                lines.append(f"   Domain: {r.domain}")
            if r.score is not None:
                lines.append(f"   Score: {r.score:.2f}")
            if r.snippet:
                lines.append(f"   Snippet: {r.snippet[:200]}")
            if r.result_type and r.result_type != "article":
                lines.append(f"   Type: {r.result_type}")
            lines.append("")

        if response.images:
            lines.append(f"Images ({len(response.images)}):")
            for img in response.images:
                lines.append(f"  - {img.description or img.url}")

        return "\n".join(lines)
    except Exception as e:
        return f"Search failed: {type(e).__name__}: {e}"


# ---------------------------------------------------------------------------
# Tool: deep_research
# ---------------------------------------------------------------------------
@mcp.tool()
async def deep_research(
    query: str,
    max_results: int = 5,
    fetch_top: int = 3,
    categories: str = "general",
    time_range: str | None = None,
    max_content_length: int = 8000,
    force_stealth: bool = False,
) -> str:
    """Perform deep research with LLM reranking, content extraction, and AI summaries.

    This tool does a full 4-stage pipeline: search → rerank → extract → summarize.
    Slower than web_search but returns much richer results with full page content.

    Args:
        query: Research query (required).
        max_results: Max results to return (default 5).
        fetch_top: Max pages to extract content from (default 3).
        categories: Search categories comma-separated (default "general").
        time_range: Time filter ("day", "week", "month", "year", or None).
        max_content_length: Max content length per result (default 8000).
        force_stealth: Use stealth browser for anti-bot sites (default False).
    """
    start = time.monotonic()

    try:
        request = SearchRequest(
            query=query,
            depth="deep",
            max_results=max_results,
            fetch_top=fetch_top,
            categories=categories,
            time_range=time_range,
            max_content_length=max_content_length,
            force_stealth=force_stealth,
        )
        response = await search_pipeline(request)
        elapsed = int((time.monotonic() - start) * 1000)

        if not response.results:
            return f"No results found for '{query}' ({elapsed}ms)"

        lines = [
            f"Deep research results for '{query}' ({response.total} results, {response.query_time_ms}ms):",
            "",
        ]
        for i, r in enumerate(response.results, 1):
            lines.append(f"{'=' * 60}")
            lines.append(f"{i}. {r.title}")
            lines.append(f"   URL: {r.url}")
            if r.domain:
                lines.append(f"   Domain: {r.domain}")
            if r.score is not None:
                lines.append(f"   Relevance Score: {r.score:.2f}")
            if r.reason:
                lines.append(f"   Reason: {r.reason}")
            if r.snippet:
                lines.append(f"   Snippet: {r.snippet[:200]}")
            if r.content:
                content_preview = r.content[:500]
                lines.append(f"   Content: {content_preview}{'...' if len(r.content) > 500 else ''}")
            if r.fetch_method:
                lines.append(f"   Fetch Method: {r.fetch_method}")
            lines.append("")

        return "\n".join(lines)
    except Exception as e:
        return f"Research failed: {type(e).__name__}: {e}"


# ---------------------------------------------------------------------------
# Tool: extract_content
# ---------------------------------------------------------------------------
@mcp.tool()
async def extract_content(
    urls: str,
    max_length: int = 10000,
    force_stealth: bool = False,
) -> str:
    """Extract clean text content from one or more web page URLs.

    Args:
        urls: Comma-separated list of URLs to extract content from.
        max_length: Max content length per URL (default 10000).
        force_stealth: Use stealth browser for anti-bot sites (default False).
    """
    url_list = [u.strip() for u in urls.split(",") if u.strip()]
    if not url_list:
        return "Error: No URLs provided."

    try:
        request = ExtractRequest(
            urls=url_list,
            max_length=max_length,
            force_stealth=force_stealth,
        )
        response = await extract_pipeline(request)

        lines = []
        for r in response.results:
            lines.append(f"Extracted from: {r.url}")
            lines.append(f"Title: {r.title}")
            lines.append(f"Content length: {len(r.content or '')} chars")
            lines.append(f"Fetch method: {r.fetch_method}")
            lines.append(f"Fetch time: {r.fetch_time_ms}ms")
            lines.append("")
            if r.content:
                lines.append(r.content[:3000])
                if len(r.content) > 3000:
                    lines.append(f"\n... (truncated, total {len(r.content)} chars)")
            lines.append("")

        return "\n".join(lines) if lines else "No content extracted."
    except Exception as e:
        return f"Extraction failed: {type(e).__name__}: {e}"


# ---------------------------------------------------------------------------
# Tool: search_health
# ---------------------------------------------------------------------------
@mcp.tool()
async def search_health() -> str:
    """Check health status of the search service and its dependencies (SearXNG, LLM)."""
    try:
        from search import searxng

        health = {"searxng": "unknown"}
        try:
            results = await searxng.search("test", limit=1)
            health["searxng"] = "healthy"
        except Exception as e:
            health["searxng"] = f"unhealthy: {e}"

        lines = ["Search Service Health Check:", ""]
        for component, status in health.items():
            icon = "✅" if status == "healthy" else "❌"
            lines.append(f"{icon} {component}: {status}")

        return "\n".join(lines)
    except Exception as e:
        return f"Health check failed: {type(e).__name__}: {e}"


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
def main():
    import argparse

    parser = argparse.ArgumentParser(description="Agent-Search MCP Server")
    parser.add_argument(
        "--transport",
        choices=["stdio", "http"],
        default="stdio",
        help="Transport mode (default: stdio)",
    )
    parser.add_argument("--host", default="0.0.0.0", help="HTTP listen host")
    parser.add_argument("--port", type=int, default=8891, help="HTTP listen port")
    args = parser.parse_args()

    if args.transport == "http":
        print(f"Starting MCP server on http://{args.host}:{args.port}")
        mcp.run(transport="streamable-http", host=args.host, port=args.port)
    else:
        mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
