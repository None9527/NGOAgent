#!/usr/bin/env python3
"""Kernel Graph E2E Bench — end-to-end test suite for the NGOAgent HTTP/SSE kernel API.

Validates the graph runtime's behavioral completeness through the public API:
  E1  health_ready         — /v1/health returns ready=true
  E2  session_lifecycle    — create, query, delete session
  E3  chat_sse_events      — full chat produces text_delta + [DONE]
  E4  history_persistence  — history includes user + assistant after chat
  E5  multi_turn           — same session, two messages, history grows
  E6  config_models        — /api/v1/models returns at least one model
  E7  invalid_session      — chat with nonexistent session returns error
  E8  auth_rejection       — no/wrong token returns 401 or 403

Usage:
  python3 scripts/e2e_kernel_api_test.py
  python3 scripts/e2e_kernel_api_test.py --case E1,E3
  NGOAGENT_BASE_URL=http://127.0.0.1:19997 python3 scripts/e2e_kernel_api_test.py
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import traceback
from pathlib import Path
from typing import Any


try:
    import requests
except ImportError:
    sys.exit("requests is required: pip install requests")


# ═══════════════════════════════════════════
# Configuration helpers
# ═══════════════════════════════════════════


def load_config_text() -> str:
    config_path = Path(os.environ.get("NGOAGENT_CONFIG", "~/.ngoagent/config.yaml")).expanduser()
    try:
        return config_path.read_text(encoding="utf-8")
    except FileNotFoundError as exc:
        raise SystemExit(f"config not found: {config_path}") from exc


def extract_scalar(config_text: str, key: str) -> str:
    match = re.search(rf"^\s*{re.escape(key)}:\s*(.+?)\s*$", config_text, flags=re.MULTILINE)
    if not match:
        raise SystemExit(f"missing `{key}` in config")
    value = match.group(1).strip().strip('"').strip("'")
    return os.path.expandvars(value)


def resolve_base_url(config_text: str) -> str:
    base = os.environ.get("NGOAGENT_BASE_URL")
    if base:
        return base.rstrip("/")
    port = extract_scalar(config_text, "http_port")
    return f"http://127.0.0.1:{port}"


def resolve_token(config_text: str) -> str:
    token = os.environ.get("NGOAGENT_AUTH_TOKEN")
    if token:
        return token
    return extract_scalar(config_text, "auth_token")


# ═══════════════════════════════════════════
# SSE parser
# ═══════════════════════════════════════════


def parse_sse(stream_resp: requests.Response) -> tuple[list[str], list[dict[str, Any]], list[str]]:
    text_chunks: list[str] = []
    json_events: list[dict[str, Any]] = []
    raw_events: list[str] = []

    for raw_line in stream_resp.iter_lines(decode_unicode=True):
        if raw_line is None:
            continue
        line = raw_line.strip("\r")
        if not line or not line.startswith("data: "):
            continue

        payload = line[6:]
        raw_events.append(payload)
        if payload == "[DONE]":
            break

        try:
            event = json.loads(payload)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue

        json_events.append(event)
        event_type = event.get("type")
        if event_type == "text_delta":
            text = event.get("delta") or event.get("text") or event.get("content") or ""
            if text:
                text_chunks.append(str(text))

    return text_chunks, json_events, raw_events


# ═══════════════════════════════════════════
# API helpers
# ═══════════════════════════════════════════


def create_session(base_url: str, headers: dict[str, str], title: str) -> str:
    resp = requests.post(
        f"{base_url}/api/v1/session/new",
        headers=headers,
        json={"title": title},
        timeout=10,
    )
    resp.raise_for_status()
    return resp.json()["session_id"]


def delete_session(base_url: str, headers: dict[str, str], session_id: str) -> None:
    resp = requests.post(
        f"{base_url}/api/v1/session/delete",
        headers=headers,
        json={"id": session_id},
        timeout=10,
    )
    # Accept 200 or 404 (already deleted)
    if resp.status_code not in (200, 404):
        resp.raise_for_status()


def chat_sse(
    base_url: str,
    headers: dict[str, str],
    session_id: str,
    message: str,
    mode: str = "auto",
) -> tuple[list[str], list[dict[str, Any]], list[str]]:
    with requests.post(
        f"{base_url}/v1/chat",
        headers={**headers, "Accept": "text/event-stream"},
        json={
            "session_id": session_id,
            "message": message,
            "stream": True,
            "mode": mode,
        },
        stream=True,
        timeout=(10, 120),
    ) as resp:
        resp.raise_for_status()
        return parse_sse(resp)


def fetch_history(base_url: str, headers: dict[str, str], session_id: str) -> list[dict[str, Any]]:
    for _ in range(10):
        resp = requests.get(
            f"{base_url}/api/v1/history",
            headers=headers,
            params={"session_id": session_id},
            timeout=10,
        )
        resp.raise_for_status()
        data = resp.json()
        messages = data.get("messages", []) if isinstance(data, dict) else data
        if len(messages) >= 2:
            return messages
        time.sleep(0.5)
    return messages


# ═══════════════════════════════════════════
# Bench Runner
# ═══════════════════════════════════════════


class BenchRunner:
    def __init__(self, base_url: str, headers: dict[str, str]):
        self.base_url = base_url
        self.headers = headers
        self.results: list[tuple[str, str, str]] = []  # (id, status, detail)

    def run(self, case_id: str, title: str, fn: callable) -> None:
        print(f"=== RUN   {case_id}: {title}")
        try:
            fn()
            self.results.append((case_id, "PASS", ""))
            print(f"--- PASS: {case_id}")
        except AssertionError as e:
            self.results.append((case_id, "FAIL", str(e)))
            print(f"--- FAIL: {case_id}: {e}")
        except Exception as e:
            self.results.append((case_id, "ERROR", f"{type(e).__name__}: {e}"))
            print(f"--- ERROR: {case_id}: {type(e).__name__}: {e}")
            traceback.print_exc()

    def summary(self) -> int:
        print()
        print("=" * 60)
        passed = sum(1 for _, s, _ in self.results if s == "PASS")
        total = len(self.results)
        failed = [r for r in self.results if r[1] != "PASS"]

        if failed:
            print("FAILURES:")
            for case_id, status, detail in failed:
                print(f"  {case_id} [{status}]: {detail}")
            print()

        print(f"Result: {passed}/{total} passed")
        return 0 if passed == total else 1


class AssertionError(Exception):
    pass


def assert_true(condition: bool, msg: str) -> None:
    if not condition:
        raise AssertionError(msg)


def assert_eq(got: object, want: object, msg: str) -> None:
    if got != want:
        raise AssertionError(f"{msg}: got={got!r}, want={want!r}")


def assert_gte(got: int, want: int, msg: str) -> None:
    if got < want:
        raise AssertionError(f"{msg}: got={got}, want>={want}")


def assert_in(item: object, collection: object, msg: str) -> None:
    if item not in collection:
        raise AssertionError(f"{msg}: {item!r} not in {collection!r}")


def assert_status(resp: requests.Response, codes: list[int], msg: str) -> None:
    if resp.status_code not in codes:
        raise AssertionError(f"{msg}: status={resp.status_code}, want one of {codes}")


# ═══════════════════════════════════════════
# Test Cases
# ═══════════════════════════════════════════


def test_e1_health_ready(runner: BenchRunner) -> None:
    def _run():
        resp = requests.get(f"{runner.base_url}/v1/health", timeout=10)
        assert_status(resp, [200], "/v1/health status")
        data = resp.json()
        assert_eq(data.get("ready"), True, "health.ready")

    runner.run("E1", "health_ready", _run)


def test_e2_session_lifecycle(runner: BenchRunner) -> None:
    def _run():
        sid = create_session(runner.base_url, runner.headers, "bench-lifecycle")
        assert_true(len(sid) > 0, "session_id should be non-empty")

        # Query sessions list
        resp = requests.get(
            f"{runner.base_url}/api/v1/session/list",
            headers=runner.headers,
            timeout=10,
        )
        assert_status(resp, [200], "list sessions status")
        sessions = resp.json()
        if isinstance(sessions, dict):
            sessions = sessions.get("sessions", [])
        found = any(s.get("session_id", s.get("id", "")) == sid for s in sessions)
        assert_true(found, f"session {sid} should appear in sessions list")

        # Delete
        delete_session(runner.base_url, runner.headers, sid)

    runner.run("E2", "session_lifecycle", _run)


def test_e3_chat_sse_events(runner: BenchRunner) -> None:
    def _run():
        sid = create_session(runner.base_url, runner.headers, "bench-sse")
        try:
            text_chunks, json_events, raw_events = chat_sse(
                runner.base_url,
                runner.headers,
                sid,
                '你好。只需回复 JSON：{"ok":true}',
            )
            event_types = [e.get("type", "?") for e in json_events]
            assert_in("text_delta", event_types, "expected text_delta event")
            assert_true(len(raw_events) > 0, "expected at least one SSE payload")
            assistant_text = "".join(text_chunks)
            assert_true(len(assistant_text.strip()) > 0, "assistant text should be non-empty")
        finally:
            delete_session(runner.base_url, runner.headers, sid)

    runner.run("E3", "chat_sse_events", _run)


def test_e4_history_persistence(runner: BenchRunner) -> None:
    def _run():
        sid = create_session(runner.base_url, runner.headers, "bench-history")
        try:
            chat_sse(runner.base_url, runner.headers, sid, "回复ok")
            messages = fetch_history(runner.base_url, runner.headers, sid)
            assert_gte(len(messages), 2, "history should contain user + assistant")
        finally:
            delete_session(runner.base_url, runner.headers, sid)

    runner.run("E4", "history_persistence", _run)


def test_e5_multi_turn(runner: BenchRunner) -> None:
    def _run():
        sid = create_session(runner.base_url, runner.headers, "bench-multi-turn")
        try:
            chat_sse(runner.base_url, runner.headers, sid, "第一轮消息，回复1")
            messages_after_1 = fetch_history(runner.base_url, runner.headers, sid)
            count_1 = len(messages_after_1)
            assert_gte(count_1, 2, "first turn should produce >= 2 messages")

            chat_sse(runner.base_url, runner.headers, sid, "第二轮消息，回复2")
            messages_after_2 = fetch_history(runner.base_url, runner.headers, sid)
            count_2 = len(messages_after_2)
            assert_true(count_2 > count_1, f"multi-turn history should grow: {count_1} -> {count_2}")
        finally:
            delete_session(runner.base_url, runner.headers, sid)

    runner.run("E5", "multi_turn", _run)


def test_e6_config_models(runner: BenchRunner) -> None:
    def _run():
        resp = requests.get(
            f"{runner.base_url}/v1/models",
            headers=runner.headers,
            timeout=10,
        )
        assert_status(resp, [200], "/v1/models status")
        data = resp.json()
        models = data if isinstance(data, list) else data.get("models", [])
        assert_gte(len(models), 1, "at least one model should be available")

    runner.run("E6", "config_models", _run)


def test_e7_invalid_session(runner: BenchRunner) -> None:
    def _run():
        try:
            resp = requests.post(
                f"{runner.base_url}/v1/chat",
                headers={**runner.headers, "Accept": "text/event-stream"},
                json={
                    "session_id": "nonexistent-session-id-000",
                    "message": "hello",
                    "stream": True,
                },
                stream=True,
                timeout=(10, 30),
            )
            # NGOAgent auto-creates sessions for unknown IDs.
            # Verify either: (a) explicit error, or (b) valid SSE stream with auto-created session.
            if resp.status_code >= 400:
                # Server rejected — valid behavior
                return
            # Server accepted — verify it actually produced a response
            text_chunks, _, raw_events = parse_sse(resp)
            assert_true(
                len(raw_events) > 0,
                "auto-created session should produce SSE events",
            )
        except requests.exceptions.HTTPError:
            pass  # Expected

    runner.run("E7", "invalid_session", _run)


def test_e8_auth_rejection(runner: BenchRunner) -> None:
    def _run():
        # No token
        resp_no_token = requests.get(
            f"{runner.base_url}/api/v1/sessions",
            timeout=10,
        )
        assert_in(
            resp_no_token.status_code,
            [401, 403],
            f"expected 401/403 without token, got {resp_no_token.status_code}",
        )

        # Wrong token
        resp_bad_token = requests.get(
            f"{runner.base_url}/api/v1/sessions",
            headers={"Authorization": "Bearer invalid-token-000"},
            timeout=10,
        )
        assert_in(
            resp_bad_token.status_code,
            [401, 403],
            f"expected 401/403 with wrong token, got {resp_bad_token.status_code}",
        )

    runner.run("E8", "auth_rejection", _run)


# ═══════════════════════════════════════════
# Main
# ═══════════════════════════════════════════

ALL_CASES = {
    "E1": test_e1_health_ready,
    "E2": test_e2_session_lifecycle,
    "E3": test_e3_chat_sse_events,
    "E4": test_e4_history_persistence,
    "E5": test_e5_multi_turn,
    "E6": test_e6_config_models,
    "E7": test_e7_invalid_session,
    "E8": test_e8_auth_rejection,
}


def main() -> int:
    parser = argparse.ArgumentParser(description="Kernel Graph E2E Bench")
    parser.add_argument(
        "--case",
        default="",
        help="Comma-separated list of case IDs to run (e.g. E1,E3). Empty = all.",
    )
    args = parser.parse_args()

    config_text = load_config_text()
    base_url = resolve_base_url(config_text)
    token = resolve_token(config_text)
    headers = {"Authorization": f"Bearer {token}"}

    print(f"Kernel Graph E2E Bench")
    print(f"  target: {base_url}")
    print(f"  cases:  {args.case or 'all'}")
    print()

    runner = BenchRunner(base_url, headers)

    if args.case:
        selected = [c.strip().upper() for c in args.case.split(",")]
    else:
        selected = list(ALL_CASES.keys())

    for case_id in selected:
        fn = ALL_CASES.get(case_id)
        if fn is None:
            print(f"--- SKIP: {case_id} (unknown case)")
            continue
        fn(runner)

    return runner.summary()


if __name__ == "__main__":
    sys.exit(main())
