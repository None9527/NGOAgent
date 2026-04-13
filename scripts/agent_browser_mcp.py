#!/usr/bin/env python3
"""MCP stdio wrapper for the local agent-browser CLI."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from typing import Any


SERVER_NAME = "agent-browser"
SERVER_VERSION = "0.1.0"


def tool_schema() -> list[dict[str, Any]]:
    common_session = {
        "session": {
            "type": "string",
            "description": "Optional isolated agent-browser session name.",
        },
        "timeout_ms": {
            "type": "integer",
            "description": "Command timeout in milliseconds.",
            "default": 25000,
        },
        "allowed_domains": {
            "type": "string",
            "description": "Optional comma-separated domain allowlist for navigation.",
        },
    }
    return [
        {
            "name": "browser_open",
            "description": "Open a URL in agent-browser and return the CLI output.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "url": {"type": "string", "description": "URL to open."},
                    **common_session,
                },
                "required": ["url"],
            },
            "annotations": {"readOnlyHint": False, "openWorldHint": True},
        },
        {
            "name": "browser_snapshot",
            "description": "Return the current accessibility snapshot from agent-browser.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "interactive": {"type": "boolean", "default": False},
                    "compact": {"type": "boolean", "default": False},
                    "depth": {"type": "integer"},
                    "selector": {"type": "string"},
                    **common_session,
                },
            },
            "annotations": {"readOnlyHint": True, "openWorldHint": False},
        },
        {
            "name": "browser_click",
            "description": "Click an element selector or @ref in agent-browser.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "selector": {"type": "string", "description": "CSS selector or @ref."},
                    **common_session,
                },
                "required": ["selector"],
            },
            "annotations": {"readOnlyHint": False, "openWorldHint": False},
        },
        {
            "name": "browser_fill",
            "description": "Clear and fill an element selector or @ref in agent-browser.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "selector": {"type": "string", "description": "CSS selector or @ref."},
                    "text": {"type": "string", "description": "Text to fill."},
                    **common_session,
                },
                "required": ["selector", "text"],
            },
            "annotations": {"readOnlyHint": False, "openWorldHint": False},
        },
        {
            "name": "browser_get",
            "description": "Read page data via agent-browser get.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "what": {
                        "type": "string",
                        "enum": ["text", "html", "value", "title", "url", "count"],
                    },
                    "selector": {"type": "string"},
                    **common_session,
                },
                "required": ["what"],
            },
            "annotations": {"readOnlyHint": True, "openWorldHint": False},
        },
        {
            "name": "browser_console",
            "description": "Return browser console logs from agent-browser.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "clear": {"type": "boolean", "default": False},
                    **common_session,
                },
            },
            "annotations": {"readOnlyHint": True, "openWorldHint": False},
        },
        {
            "name": "browser_errors",
            "description": "Return page errors from agent-browser.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "clear": {"type": "boolean", "default": False},
                    **common_session,
                },
            },
            "annotations": {"readOnlyHint": True, "openWorldHint": False},
        },
    ]


def timeout_seconds(args: dict[str, Any]) -> float:
    value = args.get("timeout_ms", 25000)
    try:
        return max(1.0, min(float(value) / 1000.0, 120.0))
    except (TypeError, ValueError):
        return 25.0


def base_env(args: dict[str, Any]) -> dict[str, str]:
    env = os.environ.copy()
    if session := args.get("session"):
        env["AGENT_BROWSER_SESSION"] = str(session)
    if domains := args.get("allowed_domains"):
        env["AGENT_BROWSER_ALLOWED_DOMAINS"] = str(domains)
    return env


def run_agent_browser(argv: list[str], args: dict[str, Any]) -> str:
    completed = subprocess.run(
        ["agent-browser", *argv],
        check=False,
        capture_output=True,
        env=base_env(args),
        text=True,
        timeout=timeout_seconds(args),
    )
    output = completed.stdout.strip()
    error = completed.stderr.strip()
    if completed.returncode != 0:
        text = error or output or f"agent-browser exited with code {completed.returncode}"
        raise RuntimeError(text)
    if error:
        return f"{output}\n{error}".strip()
    return output


def call_tool(name: str, args: dict[str, Any]) -> str:
    if name == "browser_open":
        return run_agent_browser(["open", str(args["url"])], args)
    if name == "browser_snapshot":
        argv = ["snapshot"]
        if args.get("interactive"):
            argv.append("--interactive")
        if args.get("compact"):
            argv.append("--compact")
        if "depth" in args:
            argv.extend(["--depth", str(args["depth"])])
        if selector := args.get("selector"):
            argv.extend(["--selector", str(selector)])
        return run_agent_browser(argv, args)
    if name == "browser_click":
        return run_agent_browser(["click", str(args["selector"])], args)
    if name == "browser_fill":
        return run_agent_browser(["fill", str(args["selector"]), str(args["text"])], args)
    if name == "browser_get":
        argv = ["get", str(args["what"])]
        if selector := args.get("selector"):
            argv.append(str(selector))
        return run_agent_browser(argv, args)
    if name == "browser_console":
        argv = ["console"]
        if args.get("clear"):
            argv.append("--clear")
        return run_agent_browser(argv, args)
    if name == "browser_errors":
        argv = ["errors"]
        if args.get("clear"):
            argv.append("--clear")
        return run_agent_browser(argv, args)
    raise ValueError(f"unknown tool {name!r}")


def write_response(message: dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(message, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def success(request_id: Any, result: dict[str, Any]) -> None:
    write_response({"jsonrpc": "2.0", "id": request_id, "result": result})


def failure(request_id: Any, code: int, message: str) -> None:
    write_response({"jsonrpc": "2.0", "id": request_id, "error": {"code": code, "message": message}})


def handle(request: dict[str, Any]) -> None:
    request_id = request.get("id")
    method = request.get("method")
    params = request.get("params") or {}

    if request_id is None:
        return
    if method == "initialize":
        success(
            request_id,
            {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
            },
        )
        return
    if method == "tools/list":
        success(request_id, {"tools": tool_schema()})
        return
    if method == "tools/call":
        name = params.get("name")
        args = params.get("arguments") or {}
        try:
            output = call_tool(name, args)
            success(request_id, {"content": [{"type": "text", "text": output}]})
        except Exception as exc:  # noqa: BLE001 - tool errors must be returned as MCP content
            success(
                request_id,
                {
                    "content": [{"type": "text", "text": str(exc)}],
                    "isError": True,
                },
            )
        return
    if method in {"resources/list", "prompts/list"}:
        key = "resources" if method == "resources/list" else "prompts"
        success(request_id, {key: []})
        return
    failure(request_id, -32601, f"method not found: {method}")


def main() -> int:
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            handle(json.loads(line))
        except json.JSONDecodeError as exc:
            failure(None, -32700, f"parse error: {exc}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
