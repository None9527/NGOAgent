#!/usr/bin/env python3
"""Kernel Graph Performance Bench — multi-turn continuous conversation latency profiler.

Measures:
  - TTFB (Time To First Byte): time from request to first SSE text_delta
  - Full latency: time from request to [DONE]
  - Token throughput: estimated tokens/sec based on text output
  - History growth: message count per turn
  - Per-turn breakdown with P50/P95/P99 stats

Usage:
  python3 scripts/bench_perf.py
  python3 scripts/bench_perf.py --turns 10
  python3 scripts/bench_perf.py --turns 5 --sessions 3
"""

from __future__ import annotations

import argparse
import json
import os
import re
import statistics
import sys
import time
from pathlib import Path
from typing import Any

try:
    import requests
except ImportError:
    sys.exit("requests is required: pip install requests")


# ═══════════════════════════════════════════
# Config
# ═══════════════════════════════════════════

def load_config_text() -> str:
    config_path = Path(os.environ.get("NGOAGENT_CONFIG", "~/.ngoagent/config.yaml")).expanduser()
    return config_path.read_text(encoding="utf-8")


def extract_scalar(config_text: str, key: str) -> str:
    match = re.search(rf"^\s*{re.escape(key)}:\s*(.+?)\s*$", config_text, flags=re.MULTILINE)
    if not match:
        raise SystemExit(f"missing `{key}` in config")
    return os.path.expandvars(match.group(1).strip().strip('"').strip("'"))


def resolve_base_url(config_text: str) -> str:
    base = os.environ.get("NGOAGENT_BASE_URL")
    if base:
        return base.rstrip("/")
    return f"http://127.0.0.1:{extract_scalar(config_text, 'http_port')}"


def resolve_token(config_text: str) -> str:
    token = os.environ.get("NGOAGENT_AUTH_TOKEN")
    return token if token else extract_scalar(config_text, "auth_token")


# ═══════════════════════════════════════════
# Conversation prompts — designed to be cheap but exercise graph paths
# ═══════════════════════════════════════════

TURN_PROMPTS = [
    "你好，用一句话介绍你自己。",
    "列举3个Go语言的优点，每个一句话。",
    "Python的GIL是什么？一句话回答。",
    "写一个fibonacci函数的伪代码，不超过5行。",
    "比较REST和gRPC各一个优缺点。",
    "解释什么是CAP定理，一句话。",
    "Docker和虚拟机的核心区别是什么？",
    "什么是领域驱动设计(DDD)？一句话回答。",
    "解释SOLID原则中的单一职责原则。",
    "总结这次对话的要点，两句话以内。",
    "WebSocket和SSE的区别？一句话。",
    "什么是事件溯源(Event Sourcing)？",
    "解释乐观锁和悲观锁的区别。",
    "什么是CircuitBreaker模式？",
    "回复ok表示你还在。",
]


# ═══════════════════════════════════════════
# Core bench logic
# ═══════════════════════════════════════════

class TurnMetrics:
    def __init__(self, turn: int, prompt: str):
        self.turn = turn
        self.prompt = prompt
        self.ttfb: float = 0.0          # seconds
        self.full_latency: float = 0.0  # seconds
        self.output_chars: int = 0
        self.output_tokens_est: int = 0
        self.tokens_per_sec: float = 0.0
        self.history_count: int = 0
        self.event_count: int = 0
        self.error: str = ""


def run_turn(
    base_url: str,
    headers: dict[str, str],
    session_id: str,
    prompt: str,
    turn_num: int,
) -> TurnMetrics:
    metrics = TurnMetrics(turn_num, prompt)
    t_start = time.perf_counter()
    first_text_time = None

    try:
        with requests.post(
            f"{base_url}/v1/chat",
            headers={**headers, "Accept": "text/event-stream"},
            json={
                "session_id": session_id,
                "message": prompt,
                "stream": True,
            },
            stream=True,
            timeout=(10, 120),
        ) as resp:
            resp.raise_for_status()
            text_parts: list[str] = []
            event_count = 0

            for raw_line in resp.iter_lines(decode_unicode=True):
                if raw_line is None:
                    continue
                line = raw_line.strip("\r")
                if not line or not line.startswith("data: "):
                    continue

                payload = line[6:]
                event_count += 1
                if payload == "[DONE]":
                    break

                try:
                    event = json.loads(payload)
                except json.JSONDecodeError:
                    continue
                if not isinstance(event, dict):
                    continue

                if event.get("type") == "text_delta":
                    if first_text_time is None:
                        first_text_time = time.perf_counter()
                    text = event.get("delta") or event.get("text") or event.get("content") or ""
                    if text:
                        text_parts.append(str(text))

            t_end = time.perf_counter()

            full_text = "".join(text_parts)
            metrics.output_chars = len(full_text)
            # Rough CJK-aware token estimate: ~1.5 chars per token for mixed CJK/ASCII
            metrics.output_tokens_est = max(1, len(full_text) * 2 // 3)
            metrics.full_latency = t_end - t_start
            metrics.ttfb = (first_text_time - t_start) if first_text_time else metrics.full_latency
            metrics.event_count = event_count

            gen_time = t_end - (first_text_time or t_start)
            if gen_time > 0:
                metrics.tokens_per_sec = metrics.output_tokens_est / gen_time

    except Exception as e:
        metrics.error = f"{type(e).__name__}: {e}"
        metrics.full_latency = time.perf_counter() - t_start

    # Fetch history count
    try:
        resp = requests.get(
            f"{base_url}/api/v1/history",
            headers=headers,
            params={"session_id": session_id},
            timeout=10,
        )
        if resp.status_code == 200:
            data = resp.json()
            msgs = data.get("messages", []) if isinstance(data, dict) else data
            metrics.history_count = len(msgs)
    except Exception:
        pass

    return metrics


def percentile(data: list[float], p: float) -> float:
    if not data:
        return 0.0
    sorted_data = sorted(data)
    k = (len(sorted_data) - 1) * p / 100
    f = int(k)
    c = f + 1
    if c >= len(sorted_data):
        return sorted_data[f]
    return sorted_data[f] + (k - f) * (sorted_data[c] - sorted_data[f])


def run_session_bench(
    base_url: str,
    headers: dict[str, str],
    num_turns: int,
    session_idx: int,
) -> list[TurnMetrics]:
    # Create session
    resp = requests.post(
        f"{base_url}/api/v1/session/new",
        headers=headers,
        json={"title": f"perf-bench-{session_idx}"},
        timeout=10,
    )
    resp.raise_for_status()
    session_id = resp.json()["session_id"]

    results: list[TurnMetrics] = []
    prompts = TURN_PROMPTS[:num_turns] if num_turns <= len(TURN_PROMPTS) else (
        TURN_PROMPTS * (num_turns // len(TURN_PROMPTS) + 1)
    )[:num_turns]

    print(f"\n{'─' * 70}")
    print(f"  Session {session_idx + 1} | ID: {session_id} | Turns: {num_turns}")
    print(f"{'─' * 70}")
    print(f"  {'Turn':>4}  {'TTFB':>7}  {'Total':>7}  {'Tok/s':>7}  {'Chars':>6}  {'Hist':>5}  {'Status'}")
    print(f"  {'─'*4}  {'─'*7}  {'─'*7}  {'─'*7}  {'─'*6}  {'─'*5}  {'─'*8}")

    for i, prompt in enumerate(prompts):
        m = run_turn(base_url, headers, session_id, prompt, i + 1)
        results.append(m)

        status = "✅" if not m.error else f"❌ {m.error[:30]}"
        print(
            f"  {m.turn:>4}  {m.ttfb:>6.2f}s  {m.full_latency:>6.2f}s  "
            f"{m.tokens_per_sec:>6.1f}  {m.output_chars:>6}  {m.history_count:>5}  {status}"
        )

    # Cleanup
    try:
        requests.post(
            f"{base_url}/api/v1/session/delete",
            headers=headers,
            json={"id": session_id},
            timeout=10,
        )
    except Exception:
        pass

    return results


def print_summary(all_results: list[list[TurnMetrics]]) -> None:
    flat = [m for session in all_results for m in session if not m.error]
    if not flat:
        print("\n⚠ No successful turns to analyze.")
        return

    ttfbs = [m.ttfb for m in flat]
    latencies = [m.full_latency for m in flat]
    throughputs = [m.tokens_per_sec for m in flat if m.tokens_per_sec > 0]
    histories = [m.history_count for m in flat if m.history_count > 0]

    total_errors = sum(1 for session in all_results for m in session if m.error)
    total_turns = sum(len(s) for s in all_results)

    print(f"\n{'═' * 70}")
    print(f"  PERFORMANCE SUMMARY")
    print(f"{'═' * 70}")
    print(f"  Sessions:      {len(all_results)}")
    print(f"  Total turns:   {total_turns}  (✅ {len(flat)}  ❌ {total_errors})")
    print()
    print(f"  {'Metric':<20} {'Min':>8} {'P50':>8} {'P95':>8} {'P99':>8} {'Max':>8} {'Avg':>8}")
    print(f"  {'─'*20} {'─'*8} {'─'*8} {'─'*8} {'─'*8} {'─'*8} {'─'*8}")

    def row(name: str, data: list[float], fmt: str = ".2f") -> None:
        if not data:
            print(f"  {name:<20} {'n/a':>8}")
            return
        print(
            f"  {name:<20} "
            f"{min(data):>8{fmt}} "
            f"{percentile(data, 50):>8{fmt}} "
            f"{percentile(data, 95):>8{fmt}} "
            f"{percentile(data, 99):>8{fmt}} "
            f"{max(data):>8{fmt}} "
            f"{statistics.mean(data):>8{fmt}}"
        )

    row("TTFB (s)", ttfbs)
    row("Full Latency (s)", latencies)
    row("Tok/s (est)", throughputs, ".1f")

    if histories:
        print()
        print(f"  History growth:  {histories[0]} → {histories[-1]} messages ({histories[-1] - histories[0]:+d})")

    # Per-turn latency trend
    if len(all_results) == 1 and len(flat) > 2:
        print()
        print(f"  Latency Trend (per turn):")
        for m in flat:
            bar_len = int(m.full_latency * 4)
            bar = "█" * min(bar_len, 50)
            print(f"    T{m.turn:>2}: {m.full_latency:>6.2f}s  {bar}")

    print(f"\n{'═' * 70}")


# ═══════════════════════════════════════════
# Main
# ═══════════════════════════════════════════

def main() -> int:
    parser = argparse.ArgumentParser(description="Kernel Graph Performance Bench")
    parser.add_argument("--turns", type=int, default=8, help="Number of conversation turns per session")
    parser.add_argument("--sessions", type=int, default=1, help="Number of parallel sessions")
    args = parser.parse_args()

    config_text = load_config_text()
    base_url = resolve_base_url(config_text)
    token = resolve_token(config_text)
    headers = {"Authorization": f"Bearer {token}"}

    # Health check
    try:
        resp = requests.get(f"{base_url}/v1/health", timeout=5)
        health = resp.json()
        model = health.get("model", "?")
        tools = health.get("tools", 0)
    except Exception as e:
        sys.exit(f"Server not ready at {base_url}: {e}")

    print(f"Kernel Graph Performance Bench")
    print(f"  target:    {base_url}")
    print(f"  model:     {model}")
    print(f"  tools:     {tools}")
    print(f"  turns:     {args.turns}")
    print(f"  sessions:  {args.sessions}")

    all_results: list[list[TurnMetrics]] = []
    for i in range(args.sessions):
        results = run_session_bench(base_url, headers, args.turns, i)
        all_results.append(results)

    print_summary(all_results)
    return 0


if __name__ == "__main__":
    sys.exit(main())
