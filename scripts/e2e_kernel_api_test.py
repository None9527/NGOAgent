#!/usr/bin/env python3
"""Minimal end-to-end test for the NGOAgent HTTP/SSE kernel API.

This script simulates a real user flow:
1. Check server health
2. Create a session
3. Send a chat request over SSE
4. Verify assistant output and persisted history

Usage:
  python3 scripts/e2e_kernel_api_test.py
  python3 scripts/e2e_kernel_api_test.py --message "Reply with ok"
  NGOAGENT_BASE_URL=http://127.0.0.1:19997 python3 scripts/e2e_kernel_api_test.py
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Any

import requests


DEFAULT_MESSAGE = '你好。请只回复 JSON：{"ok":true,"sum":2}'


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


def fetch_history(base_url: str, headers: dict[str, str], session_id: str) -> list[dict[str, Any]]:
    history_resp = requests.get(
        f"{base_url}/api/v1/history",
        headers=headers,
        params={"session_id": session_id},
        timeout=10,
    )
    history_resp.raise_for_status()
    history_json = history_resp.json()
    return history_json.get("messages", []) if isinstance(history_json, dict) else history_json


def main() -> int:
    parser = argparse.ArgumentParser(description="Run a Python e2e test against NGOAgent kernel API.")
    parser.add_argument("--message", default=DEFAULT_MESSAGE, help="User message to send")
    parser.add_argument("--title", default="py-e2e-real-user", help="Session title")
    parser.add_argument("--mode", default="auto", help="Chat mode")
    args = parser.parse_args()

    config_text = load_config_text()
    base_url = resolve_base_url(config_text)
    token = resolve_token(config_text)
    headers = {"Authorization": f"Bearer {token}"}

    health = requests.get(f"{base_url}/v1/health", timeout=10)
    health.raise_for_status()
    health_json = health.json()
    print(f"[health] status={health_json.get('status')} ready={health_json.get('ready')}")

    session_resp = requests.post(
        f"{base_url}/api/v1/session/new",
        headers=headers,
        json={"title": args.title},
        timeout=10,
    )
    session_resp.raise_for_status()
    session_id = session_resp.json()["session_id"]
    print(f"[session] id={session_id}")

    with requests.post(
        f"{base_url}/v1/chat",
        headers={**headers, "Accept": "text/event-stream"},
        json={
            "session_id": session_id,
            "message": args.message,
            "stream": True,
            "mode": args.mode,
        },
        stream=True,
        timeout=(10, 120),
    ) as chat_resp:
        chat_resp.raise_for_status()
        text_chunks, json_events, raw_events = parse_sse(chat_resp)

    assistant_text = "".join(text_chunks)
    event_types = [event.get("type", "?") for event in json_events]
    print(f"[chat] events={event_types}")
    print(f"[chat] assistant_text={assistant_text!r}")

    messages: list[dict[str, Any]] = []
    for _ in range(10):
        messages = fetch_history(base_url, headers, session_id)
        if len(messages) >= 2:
            break
        time.sleep(0.5)
    print(f"[history] messages={len(messages)}")

    failures: list[str] = []
    if health_json.get("ready") is not True:
        failures.append("server not ready")
    if not raw_events:
        failures.append("no SSE payloads received")
    if "text_delta" not in event_types:
        failures.append("no assistant text_delta event received")
    if not assistant_text.strip():
        failures.append("assistant output is empty")
    if len(messages) < 2:
        failures.append("history does not include assistant reply")

    if failures:
        print("[result] FAIL")
        for item in failures:
            print(f" - {item}")
        return 1

    print("[result] PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
