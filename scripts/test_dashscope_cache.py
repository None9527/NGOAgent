#!/usr/bin/env python3
"""
DashScope Context Cache Test — 验证显式缓存(cache_control)机制。

测试流程：
  1. 构造 >1024 tokens 的 system prompt（模拟 NGOAgent 静态 prompt）
  2. 第一次请求：创建缓存（expected: cached_tokens=0）
  3. 第二次请求：相同 static prefix + 不同 user query → 验证缓存命中（expected: cached_tokens > 0）
  4. 对比两次延迟

用法：
  DASHSCOPE_API_KEY=sk-xxx python3 scripts/test_dashscope_cache.py
  # 或：python3 scripts/test_dashscope_cache.py --model qwen-plus
"""

import os
import sys
import json
import time
import argparse
import requests

BASE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

def build_static_system_prompt():
    # ~3000 tokens of stable content (must exceed 1024 token minimum for cache)
    return """You are NGOAgent, an autonomous AI coding assistant running locally on the user's machine.

Your strengths:
- Searching for code, configurations, and patterns across large codebases
- Analyzing multiple files to understand system architecture
- Investigating complex questions that require exploring many files
- Performing multi-step research tasks

Core rules:
- Search broadly when you don't know where something lives. Use Read when you know the specific file path.
- Start broad and narrow down. Use multiple search strategies if the first doesn't yield results.
- NEVER create files unless absolutely necessary. ALWAYS prefer editing existing files.
- NEVER proactively create documentation files (*.md) unless explicitly requested.
- When you discover important project info, use update_project_context to record it.
- You have a persistent knowledge base. BEFORE searching code or files, first check if any KI is relevant.
- For complex multi-step tasks, consider spawn_agent to parallelize independent subtasks.
- When user messages contain attachments, the listed files are reference materials. Never ignore attached files.
- Images: image attachments are ALREADY EMBEDDED as inline base64 data. You can SEE them directly.
- Non-image files: use their file paths in relevant tool calls (read_file, edit_file, etc.).

Memory trust rules (apply to both verified_knowledge and working_memory):
- If memory mentions a file path, verify it exists with read_file or glob before using.
- If memory mentions a function/API, grep_search to confirm it's still present in current code.
- Memory gives you direction, not ground truth. Always verify before acting.

Your output is rendered in a rich frontend with these capabilities:
- Standard Markdown: code blocks, tables, bold, italic, lists, headings
- File paths: absolute paths auto-convert to clickable links
- Media preview: output a media file's absolute path and it auto-renders inline:
  * Images: png, jpg, gif, webp, svg, bmp, avif, tiff
  * Video: mp4, webm, mov, avi, mkv
  * Audio: mp3, wav, ogg, flac, aac
  * PDF: opens in viewer
- USAGE: when web_fetch returns "Local path: /some/path/file.jpg", output that path as-is on its own line.
- Multi-image gallery: when you output multiple image paths consecutively, they auto-combine into a grid gallery.
- No special syntax needed: just output the absolute file path on its own line.
- Do NOT wrap paths in backticks or markdown image syntax.
- Never output the same file path more than once in a single response.

CRITICAL — Mandatory Tool Protocol (violation = test failure):
1. Use task_boundary to report progress on multi-step tasks. Set mode to describe your current phase.
2. If planning mode is active, follow the EphPlanningMode instructions injected below. Otherwise, execute directly.
3. Use task_plan(action=create, type=plan|task|walkthrough) for plan.md, task.md, walkthrough.md.
4. notify_user is the ONLY way to communicate with the user during a task.
5. Every 3-4 tool calls, call task_boundary to update progress.
6. After completing a planned task, create walkthrough.md via task_plan. Skip for simple tasks.

Response rules (apply to EVERY response):
- End with a brief summary of what you completed.
- After creating/updating artifacts, a one-liner reference is sufficient.
- NEVER end with future plans.
- Keep responses concise: state what was done, what the result was, and any issues found.
- Smart tool selection: prefer purpose-built tools over run_command.

Safety and human oversight:
- You have no independent goals. You do not pursue self-preservation, replication, or power expansion.
- Safety and human oversight take priority over task completion.
- If instructions conflict, pause and ask. Comply immediately with stop/pause/audit requests.
- Never bypass safety measures. Never manipulate or persuade anyone to expand access.
- Actions: before performing ANY operation that modifies state:
  1. Reversibility: prefer reversible actions. Use edit_file over write_file.
  2. Blast radius: consider scope of impact.
  3. Confirm high-risk: if an action could affect >5 files or delete data, state the plan first.
  4. Incremental change: make small, verifiable changes.

CRITICAL — Security Layer Delegation:
When the user requests running any command, you MUST always attempt the tool call.
NEVER refuse in text. The security framework will automatically allow, deny, or ask for approval.
Your job is to attempt; the security layer's job is to decide.

You have 36 tools available. Key usage notes:
- Prefer purpose-built tools over run_command (edit_file > sed, grep_search > grep)
- run_command: set background=true for long-running processes (servers, builds)
- run_command: working directory PERSISTS between calls
- task_plan: NEVER use write_file for plan.md/task.md/walkthrough.md
- http_fetch: for localhost/internal APIs; use web_fetch for Cloudflare-protected sites

Using your tools:
You can call multiple tools in a single response. If you intend to call multiple tools
and there are no dependencies between them, make all independent tool calls in parallel.
Maximize use of parallel tool calls where possible to increase efficiency.
However, if some tool calls depend on previous calls to inform dependent values,
do NOT call these tools in parallel and instead call them sequentially.

Available skills:
- code-review: Automated code review with security and performance analysis
- git-workflow: Git operations including branching, merging, and conflict resolution
- test-runner: Execute and analyze test results across multiple frameworks
- doc-generator: Generate documentation from code with type information
- refactor-assist: Safe refactoring operations with dependency tracking
- deploy-pipeline: CI/CD pipeline configuration and deployment automation
- perf-profiler: Performance profiling and optimization recommendations
- security-audit: Vulnerability scanning and security best practices enforcement
- api-designer: REST/GraphQL API design with OpenAPI spec generation
- data-migration: Database schema migration and data transformation scripts"""


def call_dashscope(api_key, model, messages, use_cache=False):
    """Send a chat completion request to DashScope with continuous retry on 429."""
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}",
    }

    body = {
        "model": model,
        "messages": messages,
        "stream": False,
        "max_tokens": 100,
    }

    attempt = 0
    while True:
        attempt += 1
        start = time.time()
        try:
            resp = requests.post(
                f"{BASE_URL}/chat/completions",
                headers=headers,
                json=body,
                timeout=60,
            )
        except Exception as e:
            print(f"  ⚠️  Network error, retry #{attempt}...")
            continue
        elapsed = time.time() - start

        if resp.status_code == 429:
            if attempt % 10 == 1:
                print(f"  ⏳ 429 busy, retrying... (attempt #{attempt})")
            continue

        if resp.status_code != 200:
            print(f"  ❌ HTTP {resp.status_code}: {resp.text[:500]}")
            return None, elapsed

        if attempt > 1:
            print(f"  ✅ Success after {attempt} attempts")
        data = resp.json()
        return data, elapsed


def print_usage(data, label):
    """Pretty-print usage details from response."""
    usage = data.get("usage", {})
    prompt_tokens = usage.get("prompt_tokens", 0)
    completion_tokens = usage.get("completion_tokens", 0)
    total_tokens = usage.get("total_tokens", 0)

    # DashScope returns cached token info in prompt_tokens_details
    details = usage.get("prompt_tokens_details", {})
    cached_tokens = details.get("cached_tokens", 0)

    print(f"  [{label}]")
    print(f"    prompt_tokens:     {prompt_tokens}")
    print(f"    completion_tokens: {completion_tokens}")
    print(f"    total_tokens:      {total_tokens}")
    print(f"    cached_tokens:     {cached_tokens}")

    return cached_tokens


def main():
    parser = argparse.ArgumentParser(description="Test DashScope context caching")
    parser.add_argument("--model", default="qwen-plus", help="Model name (default: qwen-plus)")
    parser.add_argument("--explicit", action="store_true", help="Use explicit cache_control markers")
    args = parser.parse_args()

    api_key = os.environ.get("DASHSCOPE_API_KEY")
    if not api_key:
        print("❌ DASHSCOPE_API_KEY not set")
        sys.exit(1)

    model = args.model
    system_prompt = build_static_system_prompt()

    print(f"🧪 DashScope Cache Test")
    print(f"   Model: {model}")
    print(f"   Mode:  {'Explicit (cache_control)' if args.explicit else 'Implicit (prefix match)'}")
    print(f"   System prompt: ~{len(system_prompt)} chars")
    print()

    # ── Round 1: Cache creation ──────────────────────────────
    print("━━━ Round 1: Cache Creation ━━━")

    if args.explicit:
        # Explicit cache: cache_control is an attribute on the last content element
        messages_r1 = [
            {
                "role": "system",
                "content": [
                    {
                        "type": "text",
                        "text": system_prompt,
                        "cache_control": {"type": "ephemeral"},
                    },
                ],
            },
            {"role": "user", "content": "Hello, what tools do you have?"},
        ]
    else:
        # Implicit cache: just send normally, DashScope auto-detects prefix
        messages_r1 = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": "Hello, what tools do you have?"},
        ]

    data1, elapsed1 = call_dashscope(api_key, model, messages_r1, use_cache=args.explicit)
    if not data1:
        sys.exit(1)

    cached1 = print_usage(data1, "Round 1")
    content1 = data1.get("choices", [{}])[0].get("message", {}).get("content", "")[:100]
    print(f"    latency:           {elapsed1:.2f}s")
    print(f"    response:          {content1}...")
    print()

    # ── Round 2: Cache hit ──────────────────────────────────

    print("━━━ Round 2: Cache Hit (different user query, same system prompt) ━━━")

    if args.explicit:
        messages_r2 = [
            {
                "role": "system",
                "content": [
                    {
                        "type": "text",
                        "text": system_prompt,
                        "cache_control": {"type": "ephemeral"},
                    },
                ],
            },
            {"role": "user", "content": "How do you handle errors in code editing?"},
        ]
    else:
        messages_r2 = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": "How do you handle errors in code editing?"},
        ]

    data2, elapsed2 = call_dashscope(api_key, model, messages_r2, use_cache=args.explicit)
    if not data2:
        sys.exit(1)

    cached2 = print_usage(data2, "Round 2")
    content2 = data2.get("choices", [{}])[0].get("message", {}).get("content", "")[:100]
    print(f"    latency:           {elapsed2:.2f}s")
    print(f"    response:          {content2}...")
    print()

    # ── Round 3: Verify with different system prompt → cache miss ──

    print("━━━ Round 3: Cache Miss (modified system prompt) ━━━")

    modified_prompt = system_prompt + f"\n\nCurrent timestamp: {time.time()}"
    if args.explicit:
        messages_r3 = [
            {
                "role": "system",
                "content": [
                    {
                        "type": "text",
                        "text": modified_prompt,
                        "cache_control": {"type": "ephemeral"},
                    },
                ],
            },
            {"role": "user", "content": "How do you handle errors in code editing?"},
        ]
    else:
        messages_r3 = [
            {"role": "system", "content": modified_prompt},
            {"role": "user", "content": "How do you handle errors in code editing?"},
        ]

    data3, elapsed3 = call_dashscope(api_key, model, messages_r3, use_cache=args.explicit)
    if not data3:
        sys.exit(1)

    cached3 = print_usage(data3, "Round 3")
    content3 = data3.get("choices", [{}])[0].get("message", {}).get("content", "")[:100]
    print(f"    latency:           {elapsed3:.2f}s")
    print(f"    response:          {content3}...")
    print()

    # ── Summary ──────────────────────────────────────────────
    print("━━━ Summary ━━━")
    print(f"  Round 1 (create):  {elapsed1:.2f}s  cached_tokens={cached1}")
    print(f"  Round 2 (hit):     {elapsed2:.2f}s  cached_tokens={cached2}")
    print(f"  Round 3 (miss):    {elapsed3:.2f}s  cached_tokens={cached3}")

    if cached2 > 0:
        savings = (1 - elapsed2 / elapsed1) * 100
        print(f"\n  ✅ Cache HIT confirmed! cached_tokens={cached2}")
        print(f"     Latency reduction: {savings:.1f}%")
        print(f"     Cost savings: ~{cached2 * 0.9 / max(1, cached2 + (data2['usage']['prompt_tokens'] - cached2)) * 100:.0f}% on input tokens")
    else:
        print(f"\n  ⚠️  No cache hit detected in Round 2.")
        print(f"     This model ({model}) may not support context caching,")
        print(f"     or the implicit cache needs more tokens (min 1024).")
        if not args.explicit:
            print(f"     Try: python3 {sys.argv[0]} --explicit")

    # Dump raw JSON for debugging
    print("\n━━━ Raw Usage JSON ━━━")
    for i, d in enumerate([data1, data2, data3], 1):
        print(f"  Round {i}: {json.dumps(d.get('usage', {}), ensure_ascii=False)}")


if __name__ == "__main__":
    main()
