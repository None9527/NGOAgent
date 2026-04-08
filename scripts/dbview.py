#!/usr/bin/env python3
"""
NGOAgent DB Inspector — 快速查看/查询 ngoagent.db
用法:
  python3 dbview.py                    # 交互式 REPL
  python3 dbview.py sessions           # 最近会话
  python3 dbview.py history <sid>      # 某会话的对话历史
  python3 dbview.py cost               # Token 用量排行
  python3 dbview.py evo [N]            # 最近 N 次 Evo 评估
  python3 dbview.py tools [N]          # 工具使用排行
  python3 dbview.py sql "SELECT ..."   # 直接执行 SQL
"""
import sqlite3
import sys
import json
import os
from datetime import datetime
from pathlib import Path

DB_PATH = os.environ.get("NGOAGENT_DB", str(Path.home() / ".ngoagent/data/ngoagent.db"))

# ─── Colors ──────────────────────────────────────────────────
C_RESET = "\033[0m"
C_BOLD  = "\033[1m"
C_DIM   = "\033[2m"
C_CYAN  = "\033[36m"
C_GREEN = "\033[32m"
C_YELLOW= "\033[33m"
C_RED   = "\033[31m"
C_BLUE  = "\033[34m"
C_MAGENTA = "\033[35m"

def conn():
    db = sqlite3.connect(DB_PATH, timeout=5)
    db.row_factory = sqlite3.Row
    return db

def fmt_time(ts):
    if not ts: return "—"
    try:
        dt = datetime.fromisoformat(str(ts).replace("Z", "+00:00"))
        return dt.strftime("%m-%d %H:%M")
    except:
        return str(ts)[:16]

def trunc(s, n=80):
    if not s: return ""
    s = str(s).replace("\n", " ").strip()
    return s[:n] + "…" if len(s) > n else s

# ─── Commands ────────────────────────────────────────────────

def cmd_tables():
    """列出所有表及行数"""
    db = conn()
    tables = db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").fetchall()
    print(f"\n{C_BOLD}{C_CYAN}═══ Tables ════════════════════════════════════{C_RESET}")
    for t in tables:
        name = t["name"]
        cnt = db.execute(f"SELECT COUNT(*) as c FROM [{name}]").fetchone()["c"]
        print(f"  {C_GREEN}{name:<30}{C_RESET} {cnt:>6} rows")
    db.close()

def cmd_sessions(limit=20):
    """最近会话 (含标题和时间)"""
    db = conn()
    rows = db.execute("""
        SELECT c.id, c.channel, c.title, c.status, c.created_at, c.updated_at,
               COALESCE(s.total_calls, 0) as calls,
               COALESCE(s.total_prompt_tok + s.total_complete_tok, 0) as tokens,
               COALESCE(s.total_cost_usd, 0) as cost
        FROM conversations c
        LEFT JOIN session_token_usages s ON s.session_id = c.id
        ORDER BY c.updated_at DESC LIMIT ?
    """, (limit,)).fetchall()
    print(f"\n{C_BOLD}{C_CYAN}═══ Recent Sessions ({len(rows)}) ═══════════════════{C_RESET}")
    for r in rows:
        status_color = C_GREEN if r["status"] == "active" else C_DIM
        cost_str = f"${r['cost']:.4f}" if r['cost'] > 0 else "—"
        print(f"  {C_YELLOW}{r['id'][:12]}{C_RESET} "
              f"{status_color}{r['status']:>6}{C_RESET}  "
              f"{C_DIM}{r['channel'] or '?':>4}{C_RESET}  "
              f"{fmt_time(r['updated_at'])}  "
              f"{C_BLUE}{r['calls']:>3}c{C_RESET} "
              f"{C_MAGENTA}{r['tokens']:>7}tok{C_RESET} "
              f"{C_GREEN}{cost_str:>8}{C_RESET}  "
              f"{trunc(r['title'], 50)}")
    db.close()

def cmd_history(session_id, limit=50):
    """某会话的对话历史"""
    db = conn()
    # 模糊匹配 session_id
    rows = db.execute("""
        SELECT id, role, content, tool_calls, tool_call_id, reasoning, token_count, created_at
        FROM history_messages
        WHERE session_id LIKE ?
        ORDER BY id ASC LIMIT ?
    """, (f"{session_id}%", limit)).fetchall()
    if not rows:
        print(f"{C_RED}No messages found for session '{session_id}'{C_RESET}")
        db.close()
        return
    print(f"\n{C_BOLD}{C_CYAN}═══ History ({len(rows)} msgs) ═════════════════════{C_RESET}")
    for r in rows:
        role = r["role"]
        role_colors = {"user": C_GREEN, "assistant": C_CYAN, "tool": C_YELLOW, "system": C_MAGENTA}
        rc = role_colors.get(role, C_DIM)
        content = trunc(r["content"], 120)
        tok = f" [{r['token_count']}tok]" if r["token_count"] else ""
        tc_info = ""
        if r["tool_calls"]:
            try:
                calls = json.loads(r["tool_calls"])
                names = [c.get("function", {}).get("name", "?") for c in calls]
                tc_info = f" → {C_YELLOW}{'|'.join(names)}{C_RESET}"
            except:
                tc_info = f" → {C_DIM}[tool_calls]{C_RESET}"
        if r["tool_call_id"]:
            tc_info = f" {C_DIM}(reply to {r['tool_call_id'][:8]}){C_RESET}"
        print(f"  {C_DIM}{r['id']:>5}{C_RESET} {rc}{role:>9}{C_RESET}{C_DIM}{tok}{C_RESET}{tc_info}  {content}")
    db.close()

def cmd_cost(limit=15):
    """Token 用量排行"""
    db = conn()
    rows = db.execute("""
        SELECT s.session_id, s.model, s.total_prompt_tok, s.total_complete_tok,
               s.total_calls, s.total_cost_usd, s.updated_at,
               c.title
        FROM session_token_usages s
        LEFT JOIN conversations c ON c.id = s.session_id
        ORDER BY s.total_cost_usd DESC LIMIT ?
    """, (limit,)).fetchall()
    print(f"\n{C_BOLD}{C_CYAN}═══ Token Usage Top {limit} ══════════════════════{C_RESET}")
    total_cost = 0
    for r in rows:
        total_tok = (r["total_prompt_tok"] or 0) + (r["total_complete_tok"] or 0)
        cost = r["total_cost_usd"] or 0
        total_cost += cost
        print(f"  {C_YELLOW}{r['session_id'][:12]}{C_RESET} "
              f"{C_GREEN}${cost:>7.4f}{C_RESET}  "
              f"{C_MAGENTA}{total_tok:>8}tok{C_RESET}  "
              f"{C_BLUE}{r['total_calls'] or 0:>3}c{C_RESET}  "
              f"{C_DIM}{r['model'] or '?':>20}{C_RESET}  "
              f"{trunc(r['title'], 40)}")
    print(f"\n  {C_BOLD}Total: ${total_cost:.4f}{C_RESET}")
    db.close()

def cmd_evo(limit=10):
    """最近 Evo 评估"""
    db = conn()
    rows = db.execute("""
        SELECT e.id, e.session_id, e.score, e.passed, e.error_type, e.feedback, e.model, e.created_at,
               t.summary as trace_summary, t.tokens_in, t.tokens_out
        FROM evo_evaluations e
        LEFT JOIN evo_traces t ON t.id = e.trace_id
        ORDER BY e.created_at DESC LIMIT ?
    """, (limit,)).fetchall()
    print(f"\n{C_BOLD}{C_CYAN}═══ Evo Evaluations ({len(rows)}) ═══════════════════{C_RESET}")
    for r in rows:
        passed = r["passed"]
        score_color = C_GREEN if passed else C_RED
        score = r["score"] or 0
        print(f"  {C_DIM}{r['id']:>4}{C_RESET} "
              f"{score_color}{'✓' if passed else '✗'} {score:.2f}{C_RESET}  "
              f"{C_YELLOW}{r['session_id'][:12]}{C_RESET}  "
              f"{fmt_time(r['created_at'])}  "
              f"{C_DIM}{r['error_type'] or '—':>12}{C_RESET}  "
              f"{trunc(r['feedback'], 60)}")
    db.close()

def cmd_tools(limit=20):
    """工具使用排行"""
    db = conn()
    rows = db.execute("""
        SELECT tool_name,
               COUNT(*) as cnt,
               SUM(CASE WHEN is_error THEN 1 ELSE 0 END) as errs,
               AVG(duration_ms) as avg_ms,
               SUM(tokens_used) as total_tok
        FROM evo_tool_usages
        GROUP BY tool_name
        ORDER BY cnt DESC LIMIT ?
    """, (limit,)).fetchall()
    print(f"\n{C_BOLD}{C_CYAN}═══ Tool Usage Ranking ═════════════════════════{C_RESET}")
    for r in rows:
        err_rate = (r["errs"] or 0) / r["cnt"] * 100 if r["cnt"] else 0
        err_color = C_RED if err_rate > 10 else C_DIM
        print(f"  {C_GREEN}{r['tool_name']:<30}{C_RESET} "
              f"{C_BLUE}{r['cnt']:>5}x{C_RESET}  "
              f"{err_color}{err_rate:>5.1f}% err{C_RESET}  "
              f"{C_DIM}{r['avg_ms'] or 0:>6.0f}ms avg{C_RESET}  "
              f"{C_MAGENTA}{r['total_tok'] or 0:>8}tok{C_RESET}")
    db.close()

def cmd_sql(query):
    """直接执行 SQL"""
    db = conn()
    try:
        rows = db.execute(query).fetchall()
        if not rows:
            print(f"{C_DIM}(0 rows){C_RESET}")
            db.close()
            return
        cols = rows[0].keys()
        # Header
        header = "  ".join(f"{C_BOLD}{c:<20}{C_RESET}" for c in cols)
        print(f"\n{header}")
        print(f"  {'─' * 20}" * len(cols))
        for r in rows[:100]:
            line = "  ".join(f"{trunc(str(r[c]), 20):<20}" for c in cols)
            print(f"  {line}")
        if len(rows) > 100:
            print(f"\n  {C_DIM}... and {len(rows)-100} more rows{C_RESET}")
    except Exception as e:
        print(f"{C_RED}Error: {e}{C_RESET}")
    db.close()

def cmd_repl():
    """交互式 REPL"""
    print(f"""
{C_BOLD}{C_CYAN}╔══════════════════════════════════════════╗
║   NGOAgent DB Inspector (REPL mode)      ║
╚══════════════════════════════════════════╝{C_RESET}
{C_DIM}DB: {DB_PATH}{C_RESET}

Commands:
  {C_GREEN}tables{C_RESET}          — 列出所有表
  {C_GREEN}sessions{C_RESET}  [N]   — 最近 N 个会话
  {C_GREEN}history{C_RESET}   <sid>  — 会话历史 (支持前缀匹配)
  {C_GREEN}cost{C_RESET}      [N]   — Token 用量排行
  {C_GREEN}evo{C_RESET}       [N]   — Evo 评估记录
  {C_GREEN}tools{C_RESET}     [N]   — 工具使用排行
  {C_GREEN}sql{C_RESET} <query>     — 执行任意 SQL
  {C_GREEN}q / exit{C_RESET}        — 退出
""")
    while True:
        try:
            line = input(f"{C_CYAN}db❯ {C_RESET}").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            break
        if not line:
            continue
        if line in ("q", "exit", "quit"):
            break
        parts = line.split(maxsplit=1)
        cmd = parts[0].lower()
        arg = parts[1] if len(parts) > 1 else ""
        if cmd == "tables":
            cmd_tables()
        elif cmd == "sessions":
            cmd_sessions(int(arg) if arg.isdigit() else 20)
        elif cmd == "history":
            if not arg:
                print(f"{C_RED}Usage: history <session_id>{C_RESET}")
            else:
                cmd_history(arg)
        elif cmd == "cost":
            cmd_cost(int(arg) if arg.isdigit() else 15)
        elif cmd == "evo":
            cmd_evo(int(arg) if arg.isdigit() else 10)
        elif cmd == "tools":
            cmd_tools(int(arg) if arg.isdigit() else 20)
        elif cmd == "sql":
            if not arg:
                print(f"{C_RED}Usage: sql SELECT ...{C_RESET}")
            else:
                cmd_sql(arg)
        else:
            # 尝试当 SQL 执行
            cmd_sql(line)

# ─── Main ────────────────────────────────────────────────────
if __name__ == "__main__":
    if not Path(DB_PATH).exists():
        print(f"{C_RED}DB not found: {DB_PATH}{C_RESET}")
        sys.exit(1)

    args = sys.argv[1:]
    if not args:
        cmd_repl()
    elif args[0] == "tables":
        cmd_tables()
    elif args[0] == "sessions":
        cmd_sessions(int(args[1]) if len(args) > 1 else 20)
    elif args[0] == "history":
        if len(args) < 2:
            print(f"{C_RED}Usage: dbview.py history <session_id>{C_RESET}")
        else:
            cmd_history(args[1], int(args[2]) if len(args) > 2 else 50)
    elif args[0] == "cost":
        cmd_cost(int(args[1]) if len(args) > 1 else 15)
    elif args[0] == "evo":
        cmd_evo(int(args[1]) if len(args) > 1 else 10)
    elif args[0] == "tools":
        cmd_tools(int(args[1]) if len(args) > 1 else 20)
    elif args[0] == "sql":
        cmd_sql(" ".join(args[1:]))
    else:
        print(f"{C_RED}Unknown command: {args[0]}{C_RESET}")
        print(__doc__)
