#!/usr/bin/env python3
"""System E2E Quality Bench — full user-flow validation for NGOAgent.

This is a strict, end-to-end system test runner, not just a report generator.
It simulates real user journeys through the public HTTP/SSE API, validates
persisted runtime traces, and exits non-zero when critical checks fail.

Default mode is deterministic and CI-friendly. Add --with-judge to run the
optional LLM-as-judge quality layer after the system checks pass.

Coverage:
  S1  health_models_auth        health, model list, auth rejection
  S2  session_lifecycle         create, list, title update, delete
  S3  chat_sse_history_runtime  chat SSE, history persistence, runtime runs/graph
  S4  multi_turn_recall         multi-turn context retention
  S5  instruction_format        strict JSON/list/short-answer prompts
  S6  tool_read_trace           file read request and tool-call trace
  S7  retry_flow                retry returns the previous user message
  S8  stop_reconnect_flow       stop endpoint and reconnect endpoint behavior
  A1  repo_forensics            inspect real repo files and return verified JSON
  A2  artifact_writer           read fixtures and write a verified report file
  A3  bugfix_loop               run failing tests, edit code, rerun passing tests
  A4  multi_turn_agent_work     continue stateful tool work across multiple user turns
  A5  save_memory_ki            save explicit memory to KI and recall it in a new session
  Q1-Q7 optional quality cases  multi-domain quality judged by LLM

Usage:
  python3 scripts/bench_quality.py
  python3 scripts/bench_quality.py --case S1,S3,S6
  python3 scripts/bench_quality.py --with-judge --min-overall 4 --min-dim 3
  NGOAGENT_BASE_URL=http://127.0.0.1:19997 python3 scripts/bench_quality.py
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable

try:
    import requests
except ImportError:
    sys.exit("requests is required: pip install requests")


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_DB_PATH = Path("~/.ngoagent/data/ngoagent.db").expanduser()


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------


def load_config_text() -> str:
    config_path = Path(os.environ.get("NGOAGENT_CONFIG", "~/.ngoagent/config.yaml")).expanduser()
    try:
        return config_path.read_text(encoding="utf-8")
    except FileNotFoundError as exc:
        raise SystemExit(f"config not found: {config_path}") from exc


def extract_scalar(config_text: str, key: str, required: bool = True) -> str:
    match = re.search(rf"^\s*{re.escape(key)}:\s*(.+?)\s*$", config_text, flags=re.MULTILINE)
    if not match:
        if required:
            raise SystemExit(f"missing `{key}` in config")
        return ""
    return os.path.expandvars(match.group(1).strip().strip('"').strip("'"))


def resolve_base_url(config_text: str) -> str:
    base = os.environ.get("NGOAGENT_BASE_URL")
    if base:
        return base.rstrip("/")
    return f"http://127.0.0.1:{extract_scalar(config_text, 'http_port')}"


def resolve_token(config_text: str) -> str:
    token = os.environ.get("NGOAGENT_AUTH_TOKEN")
    return token if token else extract_scalar(config_text, "auth_token")


def resolve_db_path(config_text: str) -> Path:
    env_path = os.environ.get("NGOAGENT_DB_PATH")
    if env_path:
        return Path(env_path).expanduser()
    db_path = extract_scalar(config_text, "db_path", required=False)
    if db_path:
        return Path(db_path).expanduser()
    return DEFAULT_DB_PATH


def resolve_knowledge_dir(config_text: str) -> Path:
    env_path = os.environ.get("NGOAGENT_KNOWLEDGE_DIR")
    if env_path:
        return Path(env_path).expanduser()
    knowledge_dir = extract_scalar(config_text, "knowledge_dir", required=False)
    if knowledge_dir:
        return Path(knowledge_dir).expanduser()
    return Path("~/.ngoagent/knowledge").expanduser()


def resolve_judge_config(config_text: str) -> tuple[str, str]:
    base_url = os.environ.get("NGOAGENT_JUDGE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1")
    api_key = os.environ.get("NGOAGENT_JUDGE_API_KEY") or extract_scalar(config_text, "api_key")
    return base_url.rstrip("/"), api_key


# ---------------------------------------------------------------------------
# Assertions and result model
# ---------------------------------------------------------------------------


class CheckFailed(AssertionError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckFailed(message)


def require_status(resp: requests.Response, allowed: set[int], context: str) -> None:
    if resp.status_code not in allowed:
        body = resp.text[:300].replace("\n", " ")
        raise CheckFailed(f"{context}: status={resp.status_code}, expected={sorted(allowed)}, body={body!r}")


@dataclass
class StepRecord:
    name: str
    ok: bool
    detail: str = ""


@dataclass
class JourneyResult:
    case_id: str
    title: str
    session_ids: list[str] = field(default_factory=list)
    temp_paths: list[Path] = field(default_factory=list)
    ki_paths: list[Path] = field(default_factory=list)
    steps: list[StepRecord] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    judgments: list[dict[str, Any]] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.errors and all(step.ok for step in self.steps)

    def add_step(self, name: str, detail: str = "") -> None:
        self.steps.append(StepRecord(name=name, ok=True, detail=detail))

    def fail(self, name: str, detail: str) -> None:
        self.steps.append(StepRecord(name=name, ok=False, detail=detail))
        self.errors.append(f"{name}: {detail}")


@dataclass
class TraceData:
    session_id: str
    messages: list[dict[str, Any]] = field(default_factory=list)
    tool_calls: list[dict[str, Any]] = field(default_factory=list)
    run_events: list[dict[str, Any]] = field(default_factory=list)
    checkpoints: list[dict[str, Any]] = field(default_factory=list)
    agent_runs: list[dict[str, Any]] = field(default_factory=list)


@dataclass
class SSETranscript:
    text: str
    events: list[dict[str, Any]]
    raw_events: list[str]
    done: bool
    status_code: int
    content_type: str

    def event_types(self) -> list[str]:
        return [str(event.get("type", "?")) for event in self.events]


@dataclass
class Runner:
    base_url: str
    token: str
    db_path: Path
    knowledge_dir: Path
    timeout: float
    cleanup: bool

    @property
    def headers(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self.token}"}

    def url(self, path: str) -> str:
        return f"{self.base_url}{path}"

    def get_json(
        self,
        path: str,
        *,
        headers: dict[str, str] | None = None,
        params: dict[str, str] | None = None,
        allowed: set[int] = {200},
    ) -> tuple[int, Any]:
        resp = requests.get(
            self.url(path),
            headers=self.headers if headers is None else headers,
            params=params,
            timeout=self.timeout,
        )
        require_status(resp, allowed, f"GET {path}")
        try:
            return resp.status_code, resp.json()
        except ValueError:
            return resp.status_code, resp.text

    def post_json(
        self,
        path: str,
        body: dict[str, Any] | None = None,
        *,
        headers: dict[str, str] | None = None,
        allowed: set[int] = {200},
    ) -> tuple[int, Any]:
        resp = requests.post(
            self.url(path),
            headers=self.headers if headers is None else headers,
            json=body or {},
            timeout=self.timeout,
        )
        require_status(resp, allowed, f"POST {path}")
        try:
            return resp.status_code, resp.json()
        except ValueError:
            return resp.status_code, resp.text

    def create_session(self, title: str) -> str:
        _, data = self.post_json("/api/v1/session/new", {"title": title})
        session_id = data.get("session_id") if isinstance(data, dict) else ""
        require(bool(session_id), f"session/new returned no session_id: {data!r}")
        return str(session_id)

    def delete_session(self, session_id: str) -> None:
        self.post_json("/api/v1/session/delete", {"id": session_id}, allowed={200, 404})

    def fetch_history(self, session_id: str, min_messages: int = 0, attempts: int = 12) -> list[dict[str, Any]]:
        messages: list[dict[str, Any]] = []
        for _ in range(attempts):
            _, data = self.get_json("/api/v1/history", params={"session_id": session_id})
            messages = data.get("messages", []) if isinstance(data, dict) else []
            if len(messages) >= min_messages:
                return messages
            time.sleep(0.4)
        return messages

    def chat_sse(self, session_id: str, message: str, mode: str = "auto") -> SSETranscript:
        text_parts: list[str] = []
        events: list[dict[str, Any]] = []
        raw_events: list[str] = []
        done = False

        with requests.post(
            self.url("/v1/chat"),
            headers={**self.headers, "Accept": "text/event-stream"},
            json={"session_id": session_id, "message": message, "stream": True, "mode": mode},
            stream=True,
            timeout=(10, 180),
        ) as resp:
            require_status(resp, {200}, "POST /v1/chat")
            content_type = resp.headers.get("Content-Type", "")
            for raw_line in resp.iter_lines(decode_unicode=True):
                if raw_line is None:
                    continue
                line = raw_line.strip("\r")
                if not line or not line.startswith("data: "):
                    continue
                payload = line[6:]
                raw_events.append(payload)
                if payload == "[DONE]":
                    done = True
                    break
                try:
                    event = json.loads(payload)
                except json.JSONDecodeError:
                    continue
                if not isinstance(event, dict):
                    continue
                events.append(event)
                if event.get("type") == "error":
                    raise CheckFailed(f"SSE error event: {event}")
                if event.get("type") == "text_delta":
                    text = event.get("delta") or event.get("text") or event.get("content") or ""
                    if text:
                        text_parts.append(str(text))

        return SSETranscript(
            text="".join(text_parts),
            events=events,
            raw_events=raw_events,
            done=done,
            status_code=200,
            content_type=content_type,
        )

    def reconnect_sse(self, session_id: str, last_seq: int = 0, allowed: set[int] = {200, 404}) -> SSETranscript:
        text_parts: list[str] = []
        events: list[dict[str, Any]] = []
        raw_events: list[str] = []
        done = False

        with requests.get(
            self.url("/v1/chat/reconnect"),
            headers={**self.headers, "Accept": "text/event-stream"},
            params={"session_id": session_id, "last_seq": str(last_seq)},
            stream=True,
            timeout=(10, 60),
        ) as resp:
            require_status(resp, allowed, "GET /v1/chat/reconnect")
            content_type = resp.headers.get("Content-Type", "")
            if resp.status_code == 404:
                return SSETranscript("", [], [], False, 404, content_type)
            for raw_line in resp.iter_lines(decode_unicode=True):
                if raw_line is None:
                    continue
                line = raw_line.strip("\r")
                if not line or not line.startswith("data: "):
                    continue
                payload = line[6:]
                raw_events.append(payload)
                if payload == "[DONE]":
                    done = True
                    break
                try:
                    event = json.loads(payload)
                except json.JSONDecodeError:
                    continue
                if not isinstance(event, dict):
                    continue
                events.append(event)
                if event.get("type") == "text_delta":
                    text = event.get("delta") or event.get("text") or event.get("content") or ""
                    if text:
                        text_parts.append(str(text))

        return SSETranscript("".join(text_parts), events, raw_events, done, 200, content_type)


# ---------------------------------------------------------------------------
# Trace extraction and validation
# ---------------------------------------------------------------------------


def extract_trace(db_path: Path, session_id: str) -> TraceData:
    trace = TraceData(session_id=session_id)
    if not db_path.exists():
        return trace

    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    try:
        trace.messages = [
            dict(row)
            for row in conn.execute(
                """SELECT id, seq, role, message_type, content_text, reasoning_text,
                          tool_call_id, token_count, created_at
                   FROM messages
                   WHERE conversation_id = ?
                   ORDER BY seq""",
                (session_id,),
            ).fetchall()
        ]

        msg_ids = [m["id"] for m in trace.messages if m.get("id")]
        if msg_ids:
            placeholders = ",".join("?" * len(msg_ids))
            trace.tool_calls = [
                dict(row)
                for row in conn.execute(
                    f"""SELECT tool_name, tool_call_id, position, args_json,
                               result_json, status, created_at
                        FROM message_tool_calls
                        WHERE message_id IN ({placeholders})
                        ORDER BY created_at""",
                    msg_ids,
                ).fetchall()
            ]

        trace.agent_runs = [
            dict(row)
            for row in conn.execute(
                """SELECT id, entry_type, status, current_node, current_route,
                          wait_reason, graph_id, graph_version, started_at, finished_at
                   FROM agent_runs
                   WHERE conversation_id = ?
                   ORDER BY started_at""",
                (session_id,),
            ).fetchall()
        ]

        run_ids = [r["id"] for r in trace.agent_runs if r.get("id")]
        if run_ids:
            placeholders = ",".join("?" * len(run_ids))
            trace.run_events = [
                dict(row)
                for row in conn.execute(
                    f"""SELECT run_id, seq, event_type, node, route, payload_json, created_at
                        FROM run_events
                        WHERE run_id IN ({placeholders})
                        ORDER BY run_id, seq""",
                    run_ids,
                ).fetchall()
            ]
            trace.checkpoints = [
                dict(row)
                for row in conn.execute(
                    f"""SELECT run_id, checkpoint_no, status, created_at
                        FROM run_checkpoints
                        WHERE run_id IN ({placeholders})
                        ORDER BY run_id, checkpoint_no""",
                    run_ids,
                ).fetchall()
            ]
    finally:
        conn.close()

    return trace


def validate_trace(
    trace: TraceData,
    *,
    expected_prompts: int,
    require_runs: bool = True,
    require_events: bool = True,
    require_checkpoints: bool = False,
) -> list[str]:
    issues: list[str] = []
    expected_messages = expected_prompts * 2
    if len(trace.messages) < expected_messages:
        issues.append(f"messages persisted {len(trace.messages)} < expected {expected_messages}")
    roles = [m.get("role") for m in trace.messages]
    if "user" not in roles:
        issues.append("no user message persisted")
    if "assistant" not in roles:
        issues.append("no assistant message persisted")
    if require_runs and not trace.agent_runs:
        issues.append("no agent_runs recorded")
    if require_events and trace.agent_runs and not trace.run_events:
        issues.append("no run_events recorded")
    if require_checkpoints and trace.agent_runs and not trace.checkpoints:
        issues.append("no run_checkpoints recorded")
    failed_runs = [
        run for run in trace.agent_runs
        if str(run.get("status", "")).lower() not in {"complete", "completed"}
    ]
    if failed_runs:
        issues.append(f"{len(failed_runs)} non-complete agent_runs")
    return issues


def format_trace_for_judge(trace: TraceData) -> str:
    parts = [
        f"## Conversation {trace.session_id}",
        f"Messages: {len(trace.messages)}",
        f"Tool calls: {len(trace.tool_calls)}",
        f"Graph runs: {len(trace.agent_runs)}",
        f"Graph events: {len(trace.run_events)}",
        f"Checkpoints: {len(trace.checkpoints)}",
        "\n### Message History",
    ]
    for msg in trace.messages:
        content = (msg.get("content_text") or "")[:600]
        parts.append(f"[{msg.get('role', '?')}] ({msg.get('message_type', '')}) {content}")
    if trace.tool_calls:
        parts.append("\n### Tool Calls")
        for call in trace.tool_calls:
            args = (call.get("args_json") or "")[:240]
            result = (call.get("result_json") or "")[:240]
            parts.append(f"{call.get('tool_name', '?')}({args}) -> {result} [{call.get('status', '?')}]")
    if trace.run_events:
        parts.append("\n### Graph Execution Trace")
        for event in trace.run_events[:80]:
            note = " (trigger events may not have node/route)" if event.get("event_type") == "trigger.received" else ""
            parts.append(
                f"[{event.get('event_type', '?')}] node={event.get('node', '')} route={event.get('route', '')}{note}"
            )
    if trace.agent_runs:
        parts.append("\n### Agent Runs")
        for run in trace.agent_runs:
            parts.append(
                f"run={str(run.get('id', '?'))[:16]} status={run.get('status', '')} "
                f"current_node={run.get('current_node', '')} current_route={run.get('current_route', '')} "
                f"graph={run.get('graph_id', '')} started_at={run.get('started_at', '')} "
                f"finished_at={run.get('finished_at', '')}"
            )
    return "\n".join(parts)


# ---------------------------------------------------------------------------
# API helpers and local validators
# ---------------------------------------------------------------------------


def response_contains_any(text: str, needles: list[str]) -> bool:
    lowered = text.lower()
    return any(needle.lower() in lowered for needle in needles)


def extract_json_object(text: str) -> dict[str, Any] | None:
    stripped = text.strip()
    try:
        data = json.loads(stripped)
        return data if isinstance(data, dict) else None
    except json.JSONDecodeError:
        pass
    match = re.search(r"\{[\s\S]*\}", text)
    if not match:
        return None
    try:
        data = json.loads(match.group())
        return data if isinstance(data, dict) else None
    except json.JSONDecodeError:
        return None


def runtime_runs(runner: Runner, session_id: str) -> list[dict[str, Any]]:
    _, data = runner.get_json("/api/v1/runtime/runs", params={"session_id": session_id})
    return data.get("runs", []) if isinstance(data, dict) else []


def runtime_graph(runner: Runner, session_id: str) -> dict[str, Any]:
    _, data = runner.get_json("/api/v1/runtime/graph", params={"session_id": session_id})
    return data if isinstance(data, dict) else {}


def with_session(result: JourneyResult, runner: Runner, title: str) -> str:
    session_id = runner.create_session(title)
    result.session_ids.append(session_id)
    result.add_step("create_session", session_id)
    return session_id


def chat_text_with_history_fallback(runner: Runner, session_id: str, message: str, mode: str = "auto") -> str:
    transcript = runner.chat_sse(session_id, message, mode=mode)
    if transcript.text.strip():
        return transcript.text

    # Tool-heavy agentic runs can persist the final assistant message even when
    # the streamed text uses a non-text_delta event shape. These journeys test
    # agent behavior; S3 remains the dedicated SSE transport check.
    for msg in reversed(runner.fetch_history(session_id, min_messages=2, attempts=8)):
        if str(msg.get("role", "")).lower() != "assistant":
            continue
        content = msg.get("content") or msg.get("content_text") or msg.get("text") or ""
        if str(content).strip():
            return str(content)
    return transcript.text


def make_temp_workspace(result: JourneyResult, prefix: str) -> Path:
    base = Path(tempfile.gettempdir()) / f"ngoagent-{prefix}-{uuid.uuid4().hex[:8]}"
    base.mkdir(parents=True, exist_ok=False)
    result.temp_paths.append(base)
    return base


def tool_names(trace: TraceData) -> list[str]:
    return [str(call.get("tool_name", "")) for call in trace.tool_calls]


def find_ki_path(knowledge_dir: Path, title: str, content_marker: str = "") -> Path | None:
    if not knowledge_dir.exists():
        return None
    for meta_path in knowledge_dir.glob("*/metadata.json"):
        try:
            meta = json.loads(meta_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if meta.get("title") != title:
            continue
        item_dir = meta_path.parent
        overview = item_dir / "artifacts" / "overview.md"
        content = ""
        if overview.exists():
            content = overview.read_text(encoding="utf-8")
        if content_marker and content_marker not in content and content_marker not in json.dumps(meta, ensure_ascii=False):
            continue
        return item_dir
    return None


def cleanup_sessions(runner: Runner, results: list[JourneyResult]) -> None:
    if not runner.cleanup:
        return
    seen: set[str] = set()
    for result in results:
        for session_id in result.session_ids:
            if session_id in seen:
                continue
            seen.add(session_id)
            try:
                runner.delete_session(session_id)
            except Exception as exc:
                result.warnings.append(f"cleanup failed for {session_id}: {type(exc).__name__}: {exc}")
        for path in result.temp_paths:
            try:
                if path.exists():
                    if path.is_dir():
                        shutil.rmtree(path)
                    else:
                        path.unlink()
            except Exception as exc:
                result.warnings.append(f"temp cleanup failed for {path}: {type(exc).__name__}: {exc}")
        for path in result.ki_paths:
            try:
                if path.exists():
                    shutil.rmtree(path)
            except Exception as exc:
                result.warnings.append(f"KI cleanup failed for {path}: {type(exc).__name__}: {exc}")


# ---------------------------------------------------------------------------
# Deterministic system journeys
# ---------------------------------------------------------------------------


def run_journey(result: JourneyResult, fn: Callable[[JourneyResult], None]) -> JourneyResult:
    try:
        fn(result)
    except CheckFailed as exc:
        result.fail("assertion", str(exc))
    except Exception as exc:
        result.fail("exception", f"{type(exc).__name__}: {exc}")
    return result


def journey_health_models_auth(runner: Runner) -> JourneyResult:
    result = JourneyResult("S1", "health, models, auth rejection")

    def _run(r: JourneyResult) -> None:
        _, health = runner.get_json("/v1/health", headers={})
        require(isinstance(health, dict), f"health response not object: {health!r}")
        require(health.get("ready") is True, f"health.ready is not true: {health!r}")
        r.add_step("health_ready", f"status={health.get('status')} model={health.get('model')}")

        _, models = runner.get_json("/v1/models")
        model_list = models if isinstance(models, list) else models.get("models", [])
        require(len(model_list) >= 1, f"models list empty: {models!r}")
        r.add_step("models_available", f"count={len(model_list)}")

        status, _ = runner.get_json("/api/v1/session/list", headers={}, allowed={401, 403})
        r.add_step("auth_rejects_missing_token", f"status={status}")
        status, _ = runner.get_json(
            "/api/v1/session/list",
            headers={"Authorization": "Bearer invalid-token-for-e2e"},
            allowed={401, 403},
        )
        r.add_step("auth_rejects_bad_token", f"status={status}")

    return run_journey(result, _run)


def journey_session_lifecycle(runner: Runner) -> JourneyResult:
    result = JourneyResult("S2", "session lifecycle")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-session-lifecycle")

        _, listed = runner.get_json("/api/v1/session/list")
        sessions = listed.get("sessions", []) if isinstance(listed, dict) else []
        require(any(s.get("id") == sid or s.get("session_id") == sid for s in sessions), "created session not listed")
        r.add_step("session_list_contains_created", f"sessions={len(sessions)}")

        runner.post_json("/api/v1/session/title", {"id": sid, "title": "e2e-renamed"})
        _, listed_after_title = runner.get_json("/api/v1/session/list")
        sessions_after_title = listed_after_title.get("sessions", []) if isinstance(listed_after_title, dict) else []
        target = next((s for s in sessions_after_title if s.get("id") == sid or s.get("session_id") == sid), None)
        require(target is not None, "renamed session missing from list")
        require(target.get("title") == "e2e-renamed", f"title update not visible: {target!r}")
        r.add_step("session_title_persisted")

        runner.delete_session(sid)
        r.add_step("session_delete")

    return run_journey(result, _run)


def journey_chat_sse_history_runtime(runner: Runner) -> JourneyResult:
    result = JourneyResult("S3", "chat SSE, history, runtime trace")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-chat-runtime")
        transcript = runner.chat_sse(sid, '请只回复一句话，内容包含 "E2E-OK"。')
        require("text/event-stream" in transcript.content_type, f"unexpected content type {transcript.content_type!r}")
        require(transcript.done, "SSE did not end with [DONE]")
        require("text_delta" in transcript.event_types(), f"no text_delta event: {transcript.event_types()}")
        require("E2E-OK" in transcript.text or "e2e-ok" in transcript.text.lower(), f"assistant missed marker: {transcript.text!r}")
        r.add_step("chat_sse_done", f"chars={len(transcript.text)} events={len(transcript.raw_events)}")

        history = runner.fetch_history(sid, min_messages=2)
        require(len(history) >= 2, f"history too short: {history!r}")
        require(any(msg.get("role") == "user" for msg in history), "history missing user")
        require(any(msg.get("role") == "assistant" for msg in history), "history missing assistant")
        r.add_step("history_persisted", f"messages={len(history)}")

        runs = runtime_runs(runner, sid)
        graph = runtime_graph(runner, sid)
        require(len(runs) >= 1, "runtime runs API returned no runs")
        require(len(graph.get("nodes", [])) >= 1, f"runtime graph returned no nodes: {graph!r}")
        r.add_step("runtime_api_visible", f"runs={len(runs)} nodes={len(graph.get('nodes', []))}")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        r.add_step("db_trace_integrity", f"msgs={len(trace.messages)} runs={len(trace.agent_runs)} events={len(trace.run_events)}")

    return run_journey(result, _run)


def journey_multi_turn_recall(runner: Runner) -> JourneyResult:
    result = JourneyResult("S4", "multi-turn recall")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-multi-turn")
        prompts = [
            "请记住测试代号：ALPHA-42。只回答已记住。",
            "现在解释一下 TCP 三次握手，用一句话。",
            "我刚才让你记住的测试代号是什么？只回答代号。",
        ]
        responses = [runner.chat_sse(sid, prompt).text for prompt in prompts]
        require("ALPHA-42" in responses[-1] or "alpha-42" in responses[-1].lower(), f"recall failed: {responses[-1]!r}")
        history = runner.fetch_history(sid, min_messages=6)
        require(len(history) >= 6, f"multi-turn history too short: {len(history)}")
        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=3)
        require(not issues, "; ".join(issues))
        r.add_step("multi_turn_context_retained", f"history={len(history)}")

    return run_journey(result, _run)


def journey_instruction_format(runner: Runner) -> JourneyResult:
    result = JourneyResult("S5", "instruction format")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-instruction-format")
        json_text = runner.chat_sse(sid, '只回复这个JSON对象，不要解释：{"name":"test","score":100}').text
        obj = extract_json_object(json_text)
        require(obj is not None, f"no valid JSON object in response: {json_text!r}")
        require(obj.get("name") == "test" and int(obj.get("score", -1)) == 100, f"wrong JSON object: {obj!r}")
        r.add_step("json_format_followed")

        list_text = runner.chat_sse(sid, "列出3种编程范式，必须使用 1. 2. 3. 三行格式。").text
        require(bool(re.search(r"(?m)^\s*1\.", list_text)), f"missing numbered 1.: {list_text!r}")
        require(bool(re.search(r"(?m)^\s*2\.", list_text)), f"missing numbered 2.: {list_text!r}")
        require(bool(re.search(r"(?m)^\s*3\.", list_text)), f"missing numbered 3.: {list_text!r}")
        r.add_step("numbered_list_followed")

        short_text = runner.chat_sse(sid, "用10个汉字以内回答：天空是什么颜色？").text.strip()
        require(len(short_text) <= 20, f"short answer too long: {short_text!r}")
        require(response_contains_any(short_text, ["蓝", "blue"]), f"short answer not about blue sky: {short_text!r}")
        r.add_step("short_answer_followed", short_text)

    return run_journey(result, _run)


def journey_tool_read_trace(runner: Runner) -> JourneyResult:
    result = JourneyResult("S6", "tool read and trace")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-tool-read")
        target = ROOT / "go.mod"
        response = chat_text_with_history_fallback(runner, sid, f"读取 {target} 的前5行内容，并简短说明模块名。")
        require(response_contains_any(response, ["module", "ngoagent", "ngoclaw"]), f"file read response looks wrong: {response!r}")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        tool_names = [str(call.get("tool_name", "")) for call in trace.tool_calls]
        require(tool_names, "expected at least one tool call for file read")
        require(any(response_contains_any(name, ["read", "file", "view"]) for name in tool_names), f"unexpected tool calls: {tool_names}")
        require(any(str(call.get("status", "")).lower() in {"ok", "success", "completed", "complete"} for call in trace.tool_calls), f"no successful tool call: {trace.tool_calls!r}")
        r.add_step("tool_call_recorded", ",".join(tool_names))

    return run_journey(result, _run)


def journey_retry_flow(runner: Runner) -> JourneyResult:
    result = JourneyResult("S7", "retry flow")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-retry")
        prompt = "这是 retry 测试消息，请回复 retry-ok。"
        runner.chat_sse(sid, prompt)
        _, retry = runner.post_json("/v1/retry", {"session_id": sid})
        require(isinstance(retry, dict), f"retry response not object: {retry!r}")
        require(retry.get("status") == "ok", f"retry status not ok: {retry!r}")
        require(retry.get("last_message") == prompt, f"retry last_message mismatch: {retry!r}")
        r.add_step("retry_returns_last_message")

    return run_journey(result, _run)


def journey_stop_reconnect_flow(runner: Runner) -> JourneyResult:
    result = JourneyResult("S8", "stop and reconnect endpoints")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "e2e-stop-reconnect")
        _, stopped = runner.post_json("/v1/stop", {"session_id": sid})
        require(isinstance(stopped, dict) and stopped.get("status") == "stopped", f"bad stop response: {stopped!r}")
        r.add_step("stop_endpoint", "stopped")

        reconnect = runner.reconnect_sse(sid, allowed={200, 404})
        require(reconnect.status_code in {200, 404}, f"unexpected reconnect status {reconnect.status_code}")
        if reconnect.status_code == 200:
            require("text/event-stream" in reconnect.content_type, f"bad reconnect content type {reconnect.content_type!r}")
        r.add_step("reconnect_endpoint", f"status={reconnect.status_code}")

        missing = runner.reconnect_sse("missing-e2e-session", allowed={404})
        require(missing.status_code == 404, f"missing reconnect should 404, got {missing.status_code}")
        r.add_step("reconnect_missing_404")

    return run_journey(result, _run)


def journey_repo_forensics(runner: Runner) -> JourneyResult:
    result = JourneyResult("A1", "natural repo forensics with verified JSON")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "agentic-repo-forensics")
        prompt = f"""
我需要快速确认这个仓库两个事实，方便贴到检查记录里：
- Go module 名是什么。
- history persistence 里现在有没有 tool result backfill 相关实现。

请给我一个简短 JSON，格式如下：
{{"module":"...","has_tool_result_backfill":true,"evidence":["..."]}}
evidence 放 1-2 个能支撑结论的短片段。
"""
        text = chat_text_with_history_fallback(runner, sid, prompt, mode="agentic")
        data = extract_json_object(text)
        require(data is not None, f"agent did not return JSON: {text!r}")
        require(data.get("module") == "github.com/ngoclaw/ngoagent", f"wrong module: {data!r}")
        require(data.get("has_tool_result_backfill") is True, f"missing backfill detection: {data!r}")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        names = tool_names(trace)
        require(any(name in {"read_file", "grep_search"} for name in names), f"expected repo inspection tools, got {names}")
        r.add_step("repo_facts_verified", ",".join(names))

    return run_journey(result, _run)


def journey_artifact_writer(runner: Runner) -> JourneyResult:
    result = JourneyResult("A2", "natural fixture reading and artifact writing")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "agentic-artifact-writer")
        workdir = make_temp_workspace(r, "artifact")
        (workdir / "alpha.txt").write_text("alpha: 3 5 8\nlabel: A2-PASS\n", encoding="utf-8")
        (workdir / "beta.txt").write_text("beta: 13 21\nnote: combine all numbers\n", encoding="utf-8")
        report_path = workdir / "report.json"
        prompt = f"""
我把两份小数据放在这里了：
- {workdir / "alpha.txt"}
- {workdir / "beta.txt"}

帮我合并里面的数字，生成给程序读取的 {report_path}：
字段包括 marker、numbers、sum、max；marker 用 A2-PASS，numbers 保持从文件里读到的顺序。
做完简单告诉我保存好了。
"""
        chat_text_with_history_fallback(runner, sid, prompt, mode="agentic")
        require(report_path.exists(), f"report not written: {report_path}")
        report = json.loads(report_path.read_text(encoding="utf-8"))
        require(report == {"marker": "A2-PASS", "numbers": [3, 5, 8, 13, 21], "sum": 50, "max": 21}, f"wrong report: {report!r}")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        names = tool_names(trace)
        require("read_file" in names, f"expected read_file, got {names}")
        require("write_file" in names or "edit_file" in names, f"expected file write tool, got {names}")
        r.add_step("artifact_verified", str(report_path))

    return run_journey(result, _run)


def journey_bugfix_loop(runner: Runner) -> JourneyResult:
    result = JourneyResult("A3", "natural bugfix loop")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "agentic-bugfix-loop")
        workdir = make_temp_workspace(r, "bugfix")
        calc = workdir / "calc.py"
        test = workdir / "test_calc.py"
        calc.write_text(
            "def add(a, b):\n"
            "    return a - b\n\n"
            "def label():\n"
            "    return 'A3-BUG'\n",
            encoding="utf-8",
        )
        test.write_text(
            "import calc\n\n"
            "assert calc.add(2, 3) == 5\n"
            "assert calc.add(-2, 7) == 5\n"
            "assert calc.label() == 'A3-FIXED'\n"
            "print('A3-PASS')\n",
            encoding="utf-8",
        )
        prompt = f"""
这个临时小项目在 {workdir}，测试现在过不了。
帮我把实现修到测试通过；测试文件是验收标准，不要改测试。
完成后告诉我验证结果。
"""
        chat_text_with_history_fallback(runner, sid, prompt, mode="agentic")
        proc = subprocess.run(
            ["python3", str(test)],
            cwd=str(workdir),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=10,
            check=False,
        )
        require(proc.returncode == 0, f"local verification failed: {proc.stdout}")
        require("A3-FIXED" in calc.read_text(encoding="utf-8"), "calc.py was not fixed as expected")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        names = tool_names(trace)
        require("run_command" in names, f"expected run_command, got {names}")
        require("read_file" in names, f"expected read_file, got {names}")
        require("edit_file" in names or "write_file" in names, f"expected code edit tool, got {names}")
        r.add_step("bugfix_verified", proc.stdout.strip())

    return run_journey(result, _run)


def journey_multi_turn_agent_work(runner: Runner) -> JourneyResult:
    result = JourneyResult("A4", "natural multi-turn stateful agent work")

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, "agentic-multi-turn-work")
        workdir = make_temp_workspace(r, "multi-turn")
        input_one = workdir / "input_round1.txt"
        input_two = workdir / "input_round2.txt"
        state_path = workdir / "state.json"
        summary_path = workdir / "summary.json"
        validator = workdir / "validate_summary.py"

        input_one.write_text("red=4\nblue=6\n", encoding="utf-8")
        validator.write_text(
            "import json\n"
            "from pathlib import Path\n\n"
            "summary = json.loads(Path('summary.json').read_text())\n"
            "assert summary['marker'] == 'A4-DONE'\n"
            "assert summary['rounds'] == [1, 2]\n"
            "assert summary['total'] == 20\n"
            "assert summary['sources'] == ['input_round1.txt', 'input_round2.txt']\n"
            "print('A4-PASS')\n",
            encoding="utf-8",
        )

        turn_one = f"""
我在 {workdir} 放了第一批数据 input_round1.txt，后面还会补第二批。
先帮我整理一个 state.json，给后续程序接着用。
字段包括 marker、round、total、sources；marker 用 A4-STATE，round 是 1，total 从第一批数据计算，sources 记录文件名。
弄好后简单回我。
"""
        chat_text_with_history_fallback(runner, sid, turn_one, mode="agentic")
        require(state_path.exists(), f"round1 state not written: {state_path}")
        state = json.loads(state_path.read_text(encoding="utf-8"))
        require(state == {"marker": "A4-STATE", "round": 1, "total": 10, "sources": ["input_round1.txt"]}, f"bad state: {state!r}")
        r.add_step("round1_state_written")

        input_two.write_text("green=10\n", encoding="utf-8")
        turn_two = """
我又补了 input_round2.txt。继续刚才那个整理任务，把两轮数据合并到 summary.json，内容请是：
marker、rounds、total、sources 四个字段；marker 用 A4-DONE，total 从两轮输入数字相加得出，sources 按轮次列文件名。
不要重新开始，沿用刚才的工作目录和 state。
"""
        chat_text_with_history_fallback(runner, sid, turn_two, mode="agentic")
        require(summary_path.exists(), f"round2 summary not written: {summary_path}")
        summary = json.loads(summary_path.read_text(encoding="utf-8"))
        require(
            summary == {
                "marker": "A4-DONE",
                "rounds": [1, 2],
                "total": 20,
                "sources": ["input_round1.txt", "input_round2.txt"],
            },
            f"bad summary: {summary!r}",
        )
        r.add_step("round2_summary_merged")

        turn_three = """
帮我检查一下刚才的 summary.json 能不能通过 validate_summary.py。
如果不通过，请修好再验证；最后告诉我结果。
"""
        chat_text_with_history_fallback(runner, sid, turn_three, mode="agentic")
        proc = subprocess.run(
            ["python3", str(validator)],
            cwd=str(workdir),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=10,
            check=False,
        )
        require(proc.returncode == 0, f"local validation failed: {proc.stdout}")

        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=3)
        require(not issues, "; ".join(issues))
        names = tool_names(trace)
        require(names.count("read_file") >= 2, f"expected repeated read_file usage, got {names}")
        require("write_file" in names or "edit_file" in names, f"expected write/edit tool, got {names}")
        require("run_command" in names, f"expected validator run_command, got {names}")
        r.add_step("round3_validation_passed", proc.stdout.strip())

    return run_journey(result, _run)


def journey_save_memory_ki(runner: Runner) -> JourneyResult:
    result = JourneyResult("A5", "natural user memory saves KI and recalls")

    def _run(r: JourneyResult) -> None:
        scenario = f"A5-SCEN-{uuid.uuid4().hex[:8]}"
        marker = f"A5-MEM-{uuid.uuid4().hex[:8]}"

        save_sid = with_session(r, runner, "agentic-save-memory")
        save_prompt = f"""
以后请记住我的一个长期偏好：我不接受浅层问答式测试，评估 Agent 时要用真实用户式、多轮、会读写文件和会验证结果的 E2E。
这条偏好的场景标签是 {scenario}，追踪标记是 {marker}，以后我问测试偏好时也应该能想起来。
"""
        chat_text_with_history_fallback(runner, save_sid, save_prompt, mode="agentic")

        trace = extract_trace(runner.db_path, save_sid)
        issues = validate_trace(trace, expected_prompts=1)
        require(not issues, "; ".join(issues))
        names = tool_names(trace)
        require("save_memory" in names, f"natural remember request should use save_memory, got {names}")

        ki_path = None
        for _ in range(8):
            if runner.knowledge_dir.exists():
                for candidate in runner.knowledge_dir.glob("*/metadata.json"):
                    try:
                        meta = json.loads(candidate.read_text(encoding="utf-8"))
                    except (OSError, json.JSONDecodeError):
                        continue
                    overview = candidate.parent / "artifacts" / "overview.md"
                    content = overview.read_text(encoding="utf-8") if overview.exists() else str(meta)
                    if marker in content:
                        ki_path = candidate.parent
                        break
            if ki_path is not None:
                break
            time.sleep(0.4)
        require(ki_path is not None, f"saved KI not found for marker={marker!r}")
        r.ki_paths.append(ki_path)
        r.add_step("save_memory_ki_persisted", str(ki_path))

        recall_sid = with_session(r, runner, "agentic-save-memory-recall")
        recall_prompt = f"""
我们重新开个话题。你还记得我保存过的 Agent 测试方式偏好吗？
我只记得场景标签是 {scenario}。请帮我回忆这个标签对应的追踪标记。
"""
        recall_text = chat_text_with_history_fallback(runner, recall_sid, recall_prompt, mode="agentic")
        require(marker in recall_text, f"recall response missing saved marker {marker!r}: {recall_text!r}")

        recall_trace = extract_trace(runner.db_path, recall_sid)
        recall_issues = validate_trace(recall_trace, expected_prompts=1)
        require(not recall_issues, "; ".join(recall_issues))
        recall_names = tool_names(recall_trace)
        require("recall" in recall_names or recall_trace.agent_runs, f"expected recall-backed answer or runtime trace, got {recall_names}")
        r.add_step("save_memory_recalled_new_session", marker)

    return run_journey(result, _run)


SYSTEM_JOURNEYS: dict[str, Callable[[Runner], JourneyResult]] = {
    "S1": journey_health_models_auth,
    "S2": journey_session_lifecycle,
    "S3": journey_chat_sse_history_runtime,
    "S4": journey_multi_turn_recall,
    "S5": journey_instruction_format,
    "S6": journey_tool_read_trace,
    "S7": journey_retry_flow,
    "S8": journey_stop_reconnect_flow,
    "A1": journey_repo_forensics,
    "A2": journey_artifact_writer,
    "A3": journey_bugfix_loop,
    "A4": journey_multi_turn_agent_work,
    "A5": journey_save_memory_ki,
}


# ---------------------------------------------------------------------------
# Optional LLM-as-judge quality layer
# ---------------------------------------------------------------------------


JUDGE_SYSTEM = """你是一个AI Agent系统的质量评估专家。你会收到完整对话与运行轨迹。
请严格按以下维度打分（1-5）：
1. accuracy: 内容是否准确、无明显幻觉
2. coherence: 多轮上下文是否连贯
3. instruction_following: 是否遵守格式和内容约束
4. topic_switching: 话题切换是否干净
5. trace_integrity: 消息、工具、运行轨迹是否完整

只输出JSON：
{
  "accuracy": {"score": N, "reason": "..."},
  "coherence": {"score": N, "reason": "..."},
  "instruction_following": {"score": N, "reason": "..."},
  "topic_switching": {"score": N, "reason": "..."},
  "trace_integrity": {"score": N, "reason": "..."},
  "overall": N,
  "summary": "一句话总评"
}
"""


@dataclass
class QualityCase:
    case_id: str
    title: str
    description: str
    prompts: list[str]


QUALITY_CASES: dict[str, QualityCase] = {
    "Q1": QualityCase(
        "Q1",
        "same topic coherence",
        "3-turn Python list-comprehension conversation.",
        ["Python的列表推导式是什么？简短回答。", "给一个列表推导式的实际例子。", "和普通for循环相比，它有什么优势？"],
    ),
    "Q2": QualityCase(
        "Q2",
        "hard topic switch",
        "Go HTTP server then cooking; verify clean topic switch.",
        ["用Go写一个HTTP server的最小代码，简短回答。", "番茄炒蛋怎么做？列出3个步骤。", "如果我想加辣椒呢？"],
    ),
    "Q3": QualityCase(
        "Q3",
        "back reference",
        "Remember 42, switch topic, then recall.",
        ["记住这个数字：42。它是什么含义？一句话回答。", "TCP三次握手的过程是什么？简短回答。", "之前我让你记住的数字是什么？"],
    ),
    "Q4": QualityCase(
        "Q4",
        "multi-domain chain",
        "Code, science, daily advice, philosophy.",
        ["JavaScript的闭包是什么？一句话。", "光速是多少？", "感冒了应该怎么办？列2点。", "「我思故我在」是谁说的？"],
    ),
    "Q5": QualityCase(
        "Q5",
        "instruction following",
        "JSON, numbered list, word limit.",
        ['只回复一个JSON对象：{"name":"test","score":100}，不要其他内容。', "列出3种编程范式，用1. 2. 3.格式。", "用10个字以内回答：天空是什么颜色？"],
    ),
    "Q6": QualityCase(
        "Q6",
        "tool usage accuracy",
        "Read go.mod first lines and explain module.",
        [f"读取 {ROOT / 'go.mod'} 的前5行内容。"],
    ),
    "Q7": QualityCase(
        "Q7",
        "reasoning depth",
        "Classic switch/lamp logic problem.",
        ["一个房间里有3盏灯，门外有3个开关分别控制它们。你只能进房间一次。如何确定每个开关控制哪盏灯？请给出完整推理过程。"],
    ),
}


def judge_trace(
    judge_base_url: str,
    judge_api_key: str,
    judge_model: str,
    case: QualityCase,
    trace_text: str,
) -> dict[str, Any]:
    prompt = f"""## 测试场景
标题: {case.title}
描述: {case.description}

## 完整轨迹数据
{trace_text}

请按要求评分。"""
    resp = requests.post(
        f"{judge_base_url}/chat/completions",
        headers={"Authorization": f"Bearer {judge_api_key}", "Content-Type": "application/json"},
        json={
            "model": judge_model,
            "messages": [{"role": "system", "content": JUDGE_SYSTEM}, {"role": "user", "content": prompt}],
            "temperature": 0.1,
            "max_tokens": 2000,
        },
        timeout=90,
    )
    resp.raise_for_status()
    content = resp.json()["choices"][0]["message"]["content"]
    match = re.search(r"\{[\s\S]*\}", content)
    if not match:
        raise CheckFailed(f"judge did not return JSON: {content[:300]!r}")
    data = json.loads(match.group())
    require(isinstance(data, dict), f"judge JSON not object: {data!r}")
    return data


def quality_journey(
    runner: Runner,
    case: QualityCase,
    *,
    judge_enabled: bool,
    judge_base_url: str,
    judge_api_key: str,
    judge_model: str,
    min_overall: float,
    min_dim: float,
) -> JourneyResult:
    result = JourneyResult(case.case_id, case.title)

    def _run(r: JourneyResult) -> None:
        sid = with_session(r, runner, f"quality-{case.case_id}")
        for prompt in case.prompts:
            transcript = runner.chat_sse(sid, prompt)
            require(transcript.done, f"{case.case_id} SSE did not finish")
            require(transcript.text.strip(), f"{case.case_id} empty assistant response")
        trace = extract_trace(runner.db_path, sid)
        issues = validate_trace(trace, expected_prompts=len(case.prompts))
        require(not issues, "; ".join(issues))
        r.add_step("quality_trace_integrity", f"msgs={len(trace.messages)} runs={len(trace.agent_runs)}")

        if not judge_enabled:
            return
        judgment = judge_trace(judge_base_url, judge_api_key, judge_model, case, format_trace_for_judge(trace))
        r.judgments.append(judgment)
        overall = float(judgment.get("overall", 0))
        require(overall >= min_overall, f"judge overall {overall} < {min_overall}: {judgment}")
        for dim in ["accuracy", "coherence", "instruction_following", "topic_switching", "trace_integrity"]:
            score = float(judgment.get(dim, {}).get("score", 0))
            require(score >= min_dim, f"judge {dim} {score} < {min_dim}: {judgment}")
        r.add_step("judge_thresholds", f"overall={overall}")

    return run_journey(result, _run)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def selected_ids(raw: str, default: list[str]) -> list[str]:
    if not raw:
        return default
    return [item.strip().upper() for item in raw.split(",") if item.strip()]


def print_result(result: JourneyResult) -> None:
    status = "PASS" if result.ok else "FAIL"
    print(f"{status:4} {result.case_id:>2}  {result.title}")
    for step in result.steps:
        marker = "ok" if step.ok else "!!"
        detail = f" - {step.detail}" if step.detail else ""
        print(f"      {marker} {step.name}{detail}")
    for warning in result.warnings:
        print(f"      warn {warning}")
    for error in result.errors:
        print(f"      err  {error}")
    for judgment in result.judgments:
        print(f"      judge overall={judgment.get('overall')} summary={str(judgment.get('summary', ''))[:80]}")


def main() -> int:
    parser = argparse.ArgumentParser(description="System E2E Quality Bench")
    parser.add_argument("--case", default="", help="Comma-separated IDs. Defaults to all S* and A* cases, plus Q* when --with-judge.")
    parser.add_argument("--with-judge", action="store_true", help="Run optional LLM-as-judge quality cases.")
    parser.add_argument("--judge-model", default="qwen3.5-plus", help="Judge LLM model.")
    parser.add_argument("--min-overall", type=float, default=4.0, help="Minimum judge overall score.")
    parser.add_argument("--min-dim", type=float, default=3.0, help="Minimum judge dimension score.")
    parser.add_argument("--timeout", type=float, default=10.0, help="HTTP request timeout for non-SSE calls.")
    parser.add_argument("--no-cleanup", action="store_true", help="Keep created sessions for manual inspection.")
    args = parser.parse_args()

    config_text = load_config_text()
    runner = Runner(
        base_url=resolve_base_url(config_text),
        token=resolve_token(config_text),
        db_path=resolve_db_path(config_text),
        knowledge_dir=resolve_knowledge_dir(config_text),
        timeout=args.timeout,
        cleanup=not args.no_cleanup,
    )

    judge_base_url = ""
    judge_api_key = ""
    if args.with_judge:
        judge_base_url, judge_api_key = resolve_judge_config(config_text)

    default_cases = list(SYSTEM_JOURNEYS)
    if args.with_judge:
        default_cases += list(QUALITY_CASES)
    ids = selected_ids(args.case, default_cases)

    print("System E2E Quality Bench")
    print(f"  target:   {runner.base_url}")
    print(f"  database: {runner.db_path}")
    print(f"  cases:    {','.join(ids)}")
    print(f"  judge:    {'on ' + args.judge_model if args.with_judge else 'off'}")
    print()

    results: list[JourneyResult] = []
    started = time.perf_counter()

    try:
        for case_id in ids:
            if case_id in SYSTEM_JOURNEYS:
                result = SYSTEM_JOURNEYS[case_id](runner)
            elif case_id in QUALITY_CASES:
                result = quality_journey(
                    runner,
                    QUALITY_CASES[case_id],
                    judge_enabled=args.with_judge,
                    judge_base_url=judge_base_url,
                    judge_api_key=judge_api_key,
                    judge_model=args.judge_model,
                    min_overall=args.min_overall,
                    min_dim=args.min_dim,
                )
            else:
                result = JourneyResult(case_id, "unknown")
                result.fail("case_selection", "unknown case id")
            results.append(result)
            print_result(result)
            print()
    finally:
        cleanup_sessions(runner, results)

    elapsed = time.perf_counter() - started
    passed = sum(1 for result in results if result.ok)
    failed = len(results) - passed
    print("=" * 72)
    print(f"Result: {passed}/{len(results)} passed, {failed} failed, elapsed={elapsed:.1f}s")
    if failed:
        print("Failed cases:", ", ".join(result.case_id for result in results if not result.ok))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
