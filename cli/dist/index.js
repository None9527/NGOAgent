#!/usr/bin/env node

// src/index.tsx
import React13 from "react";
import { render } from "ink";

// src/components/App.tsx
import { useCallback as useCallback6, useEffect as useEffect4 } from "react";
import { Box as Box11, Text as Text13, useApp, useInput as useInput9 } from "ink";

// src/contexts/ConfigContext.tsx
import { createContext, useContext, useState, useEffect, useRef, useCallback } from "react";

// src/api/adapter.ts
function adaptEvent(raw) {
  const base = {
    type: raw.type || "error",
    text: "",
    thinking: "",
    toolName: "",
    toolInput: "",
    toolOutput: "",
    success: false,
    callId: "",
    status: "",
    error: ""
  };
  switch (raw.type) {
    case "text_delta":
      base.text = raw.content || "";
      break;
    case "thinking":
      base.thinking = raw.content || "";
      base.text = raw.content || "";
      break;
    case "tool_start":
      base.type = "tool_call";
      base.toolName = raw.name || "";
      base.toolInput = typeof raw.args === "string" ? raw.args : JSON.stringify(raw.args || {});
      break;
    case "tool_result":
      base.toolName = raw.name || "";
      base.toolOutput = raw.output || "";
      base.success = !raw.error;
      break;
    case "approval_request":
      base.callId = raw.approval_id || "";
      base.toolName = raw.tool_name || "";
      base.toolInput = typeof raw.args === "string" ? raw.args : JSON.stringify(raw.args || {});
      base.text = raw.reason || "\u5F85\u5BA1\u6279";
      break;
    case "progress":
      base.status = raw.status || "";
      base.text = raw.summary || "";
      break;
    case "error":
      base.error = raw.message || raw.error || "";
      break;
    case "done":
    case "step_done":
      break;
  }
  return base;
}

// src/api/client.ts
var DEFAULT_ADDR = "http://localhost:19996";
var AgentClient = class {
  baseUrl;
  sessionId;
  constructor(addr = DEFAULT_ADDR) {
    this.baseUrl = addr.startsWith("http") ? addr : `http://${addr}`;
    this.sessionId = `cli-${Date.now()}`;
  }
  getSessionId() {
    return this.sessionId;
  }
  /** Generic JSON fetch helper */
  async fetchJSON(path, opts) {
    const res = await fetch(`${this.baseUrl}${path}`, {
      headers: { "Content-Type": "application/json" },
      ...opts
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`HTTP ${res.status}: ${text}`);
    }
    return res.json();
  }
  /** POST JSON helper */
  async postJSON(path, body) {
    return this.fetchJSON(path, {
      method: "POST",
      body: JSON.stringify(body)
    });
  }
  // ─── Session ───
  async newSession() {
    const res = await this.postJSON("/api/v1/session/new", { title: "" });
    this.sessionId = res.session_id;
    return { ok: true, message: `Session ${res.session_id}` };
  }
  // ─── Health & Status ───
  async healthCheck() {
    const res = await this.fetchJSON("/v1/health");
    return { healthy: res.status === "ok", version: res.version, model: res.model, tools: res.tools };
  }
  async getStatus() {
    const stats = await this.fetchJSON("/api/v1/stats");
    return {
      model: stats.model,
      runState: "idle",
      sessionId: this.sessionId,
      msgCount: stats.history_count,
      tokenCount: stats.token_estimate
    };
  }
  async getContextStats() {
    const stats = await this.fetchJSON("/api/v1/stats");
    return {
      messageCount: stats.history_count,
      tokenCount: stats.token_estimate,
      maxTokens: 128e3
    };
  }
  // ─── Models ───
  async listModels() {
    const res = await this.fetchJSON("/v1/models");
    const models = res.models.map((m) => ({
      id: m,
      alias: "",
      provider: "",
      description: ""
    }));
    return { models, currentModel: res.current };
  }
  async switchModel(model) {
    await this.postJSON("/v1/model/switch", { model });
    return { ok: true, message: `Switched to ${model}` };
  }
  // ─── Settings ───
  async getSettings() {
    const cfg = await this.fetchJSON("/v1/config");
    const agent = cfg?.agent || {};
    return {
      thinkLevel: agent.think_level || "off",
      verbose: agent.verbose || false,
      reasoning: agent.reasoning || "native",
      usageMode: agent.usage_mode || "full",
      activation: agent.activation || "always",
      sendPolicy: agent.send_policy || "stream",
      delegation: agent.delegation || "off",
      planningMode: agent.planning_mode ? "forge" : "auto"
    };
  }
  async updateSettings(updates) {
    const keyMap = {
      thinkLevel: "agent.think_level",
      verbose: "agent.verbose",
      reasoning: "agent.reasoning",
      usageMode: "agent.usage_mode",
      activation: "agent.activation",
      sendPolicy: "agent.send_policy",
      delegation: "agent.delegation",
      planningMode: "agent.planning_mode"
    };
    for (const [key, value] of Object.entries(updates)) {
      const dotKey = keyMap[key];
      if (dotKey) {
        await this.postJSON("/api/v1/config", { key: dotKey, value });
      }
    }
    return { ok: true, message: "Settings updated" };
  }
  // ─── History ───
  async clearHistory() {
    await this.postJSON("/api/v1/history/clear", {});
    return { ok: true, message: "History cleared" };
  }
  async getHistory() {
    const res = await this.fetchJSON(`/api/v1/history?session_id=${this.sessionId}`);
    return { messages: res.messages || [] };
  }
  async compactContext(instructions = "") {
    await this.postJSON("/api/v1/history/compact", { instructions });
    return { ok: true, message: "Context compacted" };
  }
  // ─── Tools & Skills ───
  async listTools() {
    const res = await this.fetchJSON("/api/v1/tools");
    const tools = (res.tools || []).map((t) => ({
      name: t.name,
      description: "",
      enabled: t.enabled
    }));
    return { tools };
  }
  async listSkills() {
    const res = await this.fetchJSON("/api/v1/skills");
    const skills = (res.skills || []).map((s) => ({
      name: s.name,
      enabled: s.status !== "disabled"
    }));
    return { skills };
  }
  // ─── Stats ───
  async getSessionStats() {
    const stats = await this.fetchJSON("/api/v1/stats");
    return {
      totalTokens: stats.token_estimate,
      inputTokens: Math.ceil(stats.token_estimate * 0.7),
      outputTokens: Math.ceil(stats.token_estimate * 0.3),
      toolCalls: 0,
      turnCount: stats.history_count
    };
  }
  async getGlobalStats() {
    return { totalSessions: 0, totalTokens: 0, totalToolCalls: 0 };
  }
  // ─── System ───
  async getSystemInfo() {
    return this.fetchJSON("/api/v1/system");
  }
  // ─── Security ───
  async getSecurity() {
    const res = await this.fetchJSON("/api/v1/security");
    return {
      approvalMode: res.mode,
      trustedTools: [],
      dangerousTools: res.block_list || [],
      trustedCommands: res.safe_commands || []
    };
  }
  async setApprovalMode(mode) {
    await this.postJSON("/api/v1/config", { key: "security.mode", value: mode });
    return { ok: true, message: `Approval mode: ${mode}` };
  }
  // ─── Cron (stub — not yet implemented in backend) ───
  async listCronJobs() {
    return { jobs: [] };
  }
  async cronRemove(_name) {
    return { ok: false, message: "cron not implemented" };
  }
  async cronEnable(_name) {
    return { ok: false, message: "cron not implemented" };
  }
  async cronDisable(_name) {
    return { ok: false, message: "cron not implemented" };
  }
  async cronRunNow(_name) {
    return { ok: false, message: "cron not implemented" };
  }
  // ─── Control ───
  async stopRun() {
    await this.postJSON("/v1/stop", {});
    return { ok: true, message: "Run stopped" };
  }
  async approveToolCall(callId, approved) {
    await this.postJSON("/v1/approve", { approval_id: callId, approved });
    return { ok: true, message: approved ? "Approved" : "Denied" };
  }
  /**
   * Start a streaming chat via SSE.
   * Returns a cancel handle. Events are adapted to match the ChatEvent interface.
   */
  chat(message, onEvent, onEnd, onError) {
    const controller = new AbortController();
    const run = async () => {
      try {
        const res = await fetch(`${this.baseUrl}/v1/chat`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            message,
            session_id: this.sessionId,
            stream: true
          }),
          signal: controller.signal
        });
        if (!res.ok) {
          const text = await res.text();
          onError(new Error(`HTTP ${res.status}: ${text}`));
          return;
        }
        const contentType = res.headers.get("content-type") || "";
        if (contentType.includes("application/json")) {
          const json = await res.json();
          if (json.result) {
            onEvent(adaptEvent({ type: "text_delta", content: json.result }));
            onEvent(adaptEvent({ type: "done" }));
          }
          onEnd();
          return;
        }
        const reader = res.body?.getReader();
        if (!reader) {
          onError(new Error("No response body"));
          return;
        }
        const decoder = new TextDecoder();
        let buffer = "";
        let sseRecvCount = 0;
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";
          for (const line of lines) {
            if (!line.startsWith("data: ")) continue;
            const payload = line.slice(6).trim();
            if (payload === "[DONE]") {
              onEvent(adaptEvent({ type: "done" }));
              continue;
            }
            try {
              const raw = JSON.parse(payload);
              onEvent(adaptEvent(raw));
            } catch {
            }
          }
        }
        onEnd();
      } catch (err) {
        if (err.name === "AbortError") {
          onEnd();
        } else {
          onError(err);
        }
      }
    };
    run();
    return {
      cancel: () => controller.abort()
    };
  }
};

// src/contexts/ConfigContext.tsx
import { jsx } from "react/jsx-runtime";
var ConfigContext = createContext(null);
function useConfig() {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
}
var DEFAULT_STATS = {
  inputTokens: 0,
  outputTokens: 0,
  tokenCount: 0,
  messageCount: 0,
  maxTokens: 128e3,
  costUsd: 0
};
var ConfigProvider = ({ serverAddr: serverAddr2, children }) => {
  const clientRef = useRef(null);
  const [ready, setReady] = useState(false);
  const [connError, setConnError] = useState(null);
  const [version, setVersion] = useState("?.?.?");
  const [model, setModel] = useState("loading...");
  const [tools, setTools] = useState(0);
  const [approvalMode, setApprovalMode] = useState("auto");
  const [stats, setStats] = useState(DEFAULT_STATS);
  useEffect(() => {
    const client = new AgentClient(serverAddr2);
    clientRef.current = client;
    (async () => {
      try {
        const health = await client.healthCheck();
        setVersion(health.version);
        setTools(health.tools);
        if (health.model) setModel(health.model);
        await client.newSession();
        try {
          const info = await client.listModels();
          const finalModel = info.currentModel || health.model || "unknown";
          setModel(finalModel);
          const st = await client.getContextStats();
          setStats((prev) => ({ ...prev, ...st }));
          const sec = await client.getSecurity();
          setApprovalMode(sec.approvalMode || "auto");
        } catch {
        }
        setReady(true);
      } catch (err) {
        setConnError(`Cannot connect to backend: ${err.message}
Ensure 'ngoclaw serve' is running on ${serverAddr2}`);
      }
    })();
  }, [serverAddr2]);
  const updateStats = useCallback((partial) => {
    setStats((prev) => ({ ...prev, ...partial }));
  }, []);
  const cycleApprovalMode = useCallback(async () => {
    const client = clientRef.current;
    if (!client) return;
    const modes = ["auto", "supervised"];
    const idx = modes.indexOf(approvalMode);
    const nextMode = modes[(idx + 1) % modes.length];
    setApprovalMode(nextMode);
    try {
      await client.setApprovalMode(nextMode);
    } catch {
      setApprovalMode(approvalMode);
    }
  }, [approvalMode]);
  const value = {
    client: clientRef.current,
    ready,
    connError,
    version,
    model,
    tools,
    approvalMode,
    stats,
    setModel,
    setApprovalMode,
    updateStats,
    cycleApprovalMode
  };
  return /* @__PURE__ */ jsx(ConfigContext.Provider, { value, children });
};

// src/contexts/ChatContext.tsx
import { createContext as createContext2, useContext as useContext2, useState as useState2, useRef as useRef2, useCallback as useCallback2 } from "react";

// src/components/MessageList.tsx
import React3 from "react";
import { Box as Box3, Text as Text3, Static } from "ink";
import Spinner from "ink-spinner";

// src/components/Markdown.tsx
import { useMemo } from "react";
import { Box, Text } from "ink";
import { marked } from "marked";
import { markedTerminal } from "marked-terminal";
import chalk from "chalk";
import { jsx as jsx2 } from "react/jsx-runtime";
marked.use(markedTerminal({
  // Headings: bold cyan
  heading: chalk.bold.cyan,
  // Inline code: magenta on dark bg
  codespan: chalk.magenta,
  // Code blocks: gray border
  code: chalk.gray,
  // Block quotes: dim italic
  blockquote: chalk.gray.italic,
  // Links: underline blue
  href: chalk.underline.blueBright,
  // Bold: white bold
  strong: chalk.bold.white,
  // Italic: italic gray
  em: chalk.italic.yellowBright,
  // List bullets: cyan
  listitem: chalk.white,
  // Tables: white
  table: chalk.white,
  // Horizontal rule
  hr: chalk.gray,
  // First heading (h1)
  firstHeading: chalk.bold.cyanBright,
  // Paragraphs
  paragraph: chalk.white,
  // Strikethrough 
  del: chalk.dim.strikethrough
}));
var Markdown = ({ children }) => {
  const rendered = useMemo(() => {
    try {
      const parsed = marked.parse(children);
      return parsed.replace(/\n$/, "");
    } catch {
      return children;
    }
  }, [children]);
  return /* @__PURE__ */ jsx2(Box, { flexDirection: "column", marginBottom: 1, children: /* @__PURE__ */ jsx2(Text, { children: rendered }) });
};

// src/components/Banner.tsx
import { Box as Box2, Text as Text2 } from "ink";
import { jsx as jsx3, jsxs } from "react/jsx-runtime";
function pad(s, width) {
  if (s.length >= width) return s.slice(0, width);
  return s + " ".repeat(width - s.length);
}
function center(s, width) {
  if (s.length >= width) return s.slice(0, width);
  const left = Math.floor((width - s.length) / 2);
  const right = width - s.length - left;
  return " ".repeat(left) + s + " ".repeat(right);
}
var Banner = ({
  version,
  model,
  tools,
  cwd,
  recentActivity = []
}) => {
  const displayCwd = cwd || process.cwd().replace(process.env.HOME || "", "~");
  const shortModel = model.split("/").pop() || model;
  const provider = model.includes("qwen") ? "DashScope" : "API Usage Billing";
  const termW = process.stdout.columns || 80;
  const innerW = termW - 2;
  const leftW = Math.floor(innerW * 0.4);
  const rightW = innerW - leftW - 1;
  const title = ` NGOClaw v${version} `;
  const topDashLen = Math.max(0, termW - 2 - 4 - title.length);
  const topLine = `\u256D\u2500\u2500\u2500 ${title}${"\u2500".repeat(topDashLen)}\u256E`;
  const botLine = `\u2570${"\u2500".repeat(termW - 2)}\u256F`;
  const leftLines = [
    center("Welcome back!", leftW),
    center("", leftW),
    center("\u2571\u2572  \u2571\u2572", leftW),
    center("\u2571  \u2572\u2571  \u2572", leftW),
    center("\u2571\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2572", leftW),
    center(`${shortModel} \xB7 ${provider}`, leftW),
    center(displayCwd, leftW)
  ];
  const rightLines = [
    pad("Tips for getting started", rightW),
    pad("Type a message or use /help for commands", rightW),
    pad("\u2500".repeat(Math.min(rightW, rightW)), rightW),
    pad("Recent activity", rightW)
  ];
  if (recentActivity.length > 0) {
    for (const item of recentActivity.slice(0, 3)) {
      rightLines.push(pad(item, rightW));
    }
  } else {
    rightLines.push(pad("No recent activity", rightW));
  }
  const maxRows = Math.max(leftLines.length, rightLines.length);
  while (leftLines.length < maxRows) leftLines.push(" ".repeat(leftW));
  while (rightLines.length < maxRows) rightLines.push(" ".repeat(rightW));
  return /* @__PURE__ */ jsxs(Box2, { flexDirection: "column", children: [
    /* @__PURE__ */ jsx3(Text2, { dimColor: true, children: topLine }),
    leftLines.map((leftStr, i) => /* @__PURE__ */ jsxs(Text2, { children: [
      /* @__PURE__ */ jsx3(Text2, { dimColor: true, children: "\u2502" }),
      /* @__PURE__ */ jsx3(Text2, { children: leftStr }),
      /* @__PURE__ */ jsx3(Text2, { dimColor: true, children: "\u2502" }),
      /* @__PURE__ */ jsx3(Text2, { children: rightLines[i] }),
      /* @__PURE__ */ jsx3(Text2, { dimColor: true, children: "\u2502" })
    ] }, i)),
    /* @__PURE__ */ jsx3(Text2, { dimColor: true, children: botLine })
  ] });
};

// src/components/MessageList.tsx
import { Fragment, jsx as jsx4, jsxs as jsxs2 } from "react/jsx-runtime";
function MessageList({ history, pending, staticRemountKey }) {
  const historyElements = React3.useMemo(
    () => history.map((msg) => /* @__PURE__ */ jsx4(MessageRow, { msg }, msg.id)),
    [history]
  );
  return /* @__PURE__ */ jsxs2(React3.Fragment, { children: [
    /* @__PURE__ */ jsx4(Static, { items: historyElements, children: (item) => item }, staticRemountKey),
    pending.map((msg) => /* @__PURE__ */ jsx4(MessageRow, { msg }, msg.id))
  ] });
}
var ToolCallBlock = ({ msg }) => {
  const icon2 = msg.success === void 0 ? "\u27F3" : msg.success ? "\u2713" : "\u2717";
  const color = msg.success === false ? "red" : msg.success ? "green" : "yellow";
  const displayName = msg.toolName || "tool";
  const isCommand = displayName === "run_command" || displayName === "shell";
  const expanded = false;
  const summary = React3.useMemo(() => {
    if (!msg.toolInput) return "";
    try {
      const parsed = JSON.parse(msg.toolInput);
      if (isCommand && (parsed.command || parsed.CommandLine)) {
        const cmd = parsed.command || parsed.CommandLine || "";
        return cmd.length > 70 ? cmd.slice(0, 67) + "..." : cmd;
      }
      if (parsed.path || parsed.file_path || parsed.AbsolutePath) {
        const p = parsed.path || parsed.file_path || parsed.AbsolutePath;
        const parts = p.split("/");
        return parts.length > 3 ? ".../" + parts.slice(-2).join("/") : p;
      }
      if (parsed.query || parsed.Query) return parsed.query || parsed.Query;
      if (parsed.Pattern) return parsed.Pattern;
      const vals = Object.entries(parsed).filter(([k]) => !k.startsWith("_"));
      if (vals.length === 0) return "";
      return String(vals[0][1]).slice(0, 50);
    } catch {
      return msg.toolInput.slice(0, 50);
    }
  }, [msg.toolInput, isCommand]);
  const outputLine = React3.useMemo(() => {
    if (!msg.content || msg.isStreaming) return "";
    const lines = msg.content.split("\n").filter((l) => l.trim());
    if (lines.length === 0) return "(no output)";
    const first = lines[0].slice(0, 70);
    return lines.length > 1 ? `${first} (+${lines.length - 1} lines)` : first;
  }, [msg.content, msg.isStreaming]);
  return /* @__PURE__ */ jsxs2(Box3, { flexDirection: "column", marginLeft: 2, children: [
    /* @__PURE__ */ jsxs2(Box3, { children: [
      /* @__PURE__ */ jsxs2(Text3, { color, children: [
        icon2,
        " "
      ] }),
      isCommand ? /* @__PURE__ */ jsxs2(Fragment, { children: [
        /* @__PURE__ */ jsx4(Text3, { color: "cyan", bold: true, children: "$ " }),
        /* @__PURE__ */ jsx4(Text3, { children: summary })
      ] }) : /* @__PURE__ */ jsxs2(Fragment, { children: [
        /* @__PURE__ */ jsxs2(Text3, { color: "cyan", bold: true, children: [
          displayName,
          " "
        ] }),
        /* @__PURE__ */ jsx4(Text3, { dimColor: true, children: summary })
      ] }),
      msg.isStreaming && /* @__PURE__ */ jsxs2(Text3, { color: "yellow", children: [
        " ",
        /* @__PURE__ */ jsx4(Spinner, { type: "dots" })
      ] })
    ] }),
    outputLine && !msg.isStreaming && /* @__PURE__ */ jsx4(Box3, { marginLeft: 2, children: expanded ? /* @__PURE__ */ jsx4(Text3, { color: "gray", children: msg.content }) : /* @__PURE__ */ jsx4(Text3, { dimColor: true, children: outputLine }) })
  ] });
};
var ProgressBlock = ({ msg }) => {
  return /* @__PURE__ */ jsx4(Box3, { marginLeft: 2, children: /* @__PURE__ */ jsxs2(Text3, { dimColor: true, children: [
    msg.isStreaming && /* @__PURE__ */ jsxs2(Fragment, { children: [
      /* @__PURE__ */ jsx4(Spinner, { type: "dots" }),
      " "
    ] }),
    msg.content
  ] }) });
};
var MessageRow = ({ msg }) => {
  if (msg.role === "banner" && msg.bannerData) {
    return /* @__PURE__ */ jsx4(Banner, { version: msg.bannerData.version, model: msg.bannerData.model, tools: msg.bannerData.tools });
  }
  if (msg.role === "tool") {
    return /* @__PURE__ */ jsx4(ToolCallBlock, { msg });
  }
  if (msg.role === "progress") {
    return /* @__PURE__ */ jsx4(ProgressBlock, { msg });
  }
  if (msg.role === "thinking") {
    if (msg.isStreaming) {
      return null;
    }
    return /* @__PURE__ */ jsx4(Box3, { marginLeft: 2, children: /* @__PURE__ */ jsxs2(Text3, { color: "gray", dimColor: true, children: [
      "\u{1F4AD} ",
      msg.content.slice(0, 200)
    ] }) });
  }
  if (msg.role === "error") {
    return /* @__PURE__ */ jsx4(Box3, { children: /* @__PURE__ */ jsxs2(Text3, { color: "red", bold: true, children: [
      "\u274C ",
      msg.content
    ] }) });
  }
  const icons = { user: "\u276F", assistant: "\u25CF" };
  const colors = { user: "blue", assistant: "white" };
  const icon2 = icons[msg.role] || "\u2022";
  const color = colors[msg.role] || "white";
  if (msg.role === "assistant") {
    return /* @__PURE__ */ jsxs2(Box3, { flexDirection: "row", paddingRight: 2, children: [
      /* @__PURE__ */ jsx4(Box3, { marginRight: 1, children: /* @__PURE__ */ jsx4(Text3, { color, children: icon2 }) }),
      /* @__PURE__ */ jsxs2(Box3, { flexShrink: 1, flexDirection: "column", children: [
        /* @__PURE__ */ jsx4(Markdown, { children: msg.content }),
        msg.isStreaming && /* @__PURE__ */ jsx4(Text3, { color: "gray", children: " \u258C" })
      ] })
    ] });
  }
  return /* @__PURE__ */ jsxs2(Box3, { flexDirection: "row", paddingRight: 2, children: [
    /* @__PURE__ */ jsx4(Box3, { marginRight: 1, children: /* @__PURE__ */ jsx4(Text3, { color, children: icon2 }) }),
    /* @__PURE__ */ jsx4(Box3, { flexShrink: 1, children: /* @__PURE__ */ jsxs2(Text3, { color, children: [
      msg.content,
      msg.isStreaming && /* @__PURE__ */ jsx4(Text3, { color: "gray", children: " \u258C" })
    ] }) })
  ] });
};
var nextId = 1;
function genId() {
  return `msg_${Date.now()}_${nextId++}`;
}
var turnStepCounter = 0;
function processEvent(event, messages) {
  const updated = [...messages];
  switch (event.type) {
    case "thinking": {
      const lastThinking = updated.findLastIndex((m) => m.role === "thinking" && m.isStreaming);
      if (lastThinking >= 0) {
        const delta = event.thinking || event.text || "";
        updated[lastThinking] = {
          ...updated[lastThinking],
          content: updated[lastThinking].content + delta
        };
      } else {
        updated.push({ id: genId(), role: "thinking", content: event.thinking || event.text || "", isStreaming: true });
      }
      break;
    }
    case "text_delta": {
      const thinkIdx = updated.findLastIndex((m) => m.role === "thinking" && m.isStreaming);
      if (thinkIdx >= 0) {
        updated[thinkIdx] = { ...updated[thinkIdx], isStreaming: false };
      }
      const lastAssist = updated.findLastIndex((m) => m.role === "assistant" && m.isStreaming);
      if (lastAssist >= 0) {
        updated[lastAssist] = {
          ...updated[lastAssist],
          content: updated[lastAssist].content + event.text
        };
      } else {
        updated.push({ id: genId(), role: "assistant", content: event.text, isStreaming: true });
      }
      break;
    }
    case "tool_call": {
      const HIDDEN_TOOLS = /* @__PURE__ */ new Set([
        "task_boundary",
        "task_plan",
        "notify_user",
        "save_memory",
        "update_project_context"
      ]);
      if (HIDDEN_TOOLS.has(event.toolName)) break;
      const assistIdx = updated.findLastIndex((m) => m.role === "assistant" && m.isStreaming);
      if (assistIdx >= 0) {
        updated[assistIdx] = { ...updated[assistIdx], isStreaming: false };
      }
      turnStepCounter++;
      updated.push({
        id: genId(),
        role: "tool",
        content: "",
        toolName: event.toolName,
        toolInput: event.toolInput,
        isStreaming: true,
        stepNumber: turnStepCounter
      });
      break;
    }
    case "tool_result": {
      const toolIdx = updated.findLastIndex(
        (m) => m.role === "tool" && m.isStreaming
      );
      if (toolIdx >= 0) {
        const output = event.toolOutput || "";
        updated[toolIdx] = {
          ...updated[toolIdx],
          content: output,
          success: event.success,
          isStreaming: false
        };
      }
      break;
    }
    case "progress": {
      if (!event.status && !event.text) break;
      const label = event.status || event.text || "";
      const lastProgress = updated.findLastIndex((m) => m.role === "progress" && m.isStreaming);
      if (lastProgress >= 0) {
        updated[lastProgress] = { ...updated[lastProgress], content: label };
      } else {
        updated.push({ id: genId(), role: "progress", content: label, isStreaming: true });
      }
      break;
    }
    case "approval_request": {
      const pendingIdx = updated.findLastIndex(
        (m) => m.role === "tool" && m.isStreaming
      );
      if (pendingIdx >= 0) {
        updated[pendingIdx] = {
          ...updated[pendingIdx],
          content: event.text || "\u5F85\u5BA1\u6279"
        };
      }
      break;
    }
    case "error": {
      updated.push({ id: genId(), role: "error", content: event.error || "Unknown error" });
      break;
    }
    case "step_done": {
      for (let i = 0; i < updated.length; i++) {
        if (updated[i].role === "progress" && updated[i].isStreaming) {
          updated[i] = { ...updated[i], isStreaming: false };
        }
      }
      break;
    }
    case "done": {
      for (let i = 0; i < updated.length; i++) {
        if (updated[i].isStreaming) {
          updated[i] = { ...updated[i], isStreaming: false };
        }
      }
      turnStepCounter = 0;
      break;
    }
  }
  return updated;
}

// src/contexts/ChatContext.tsx
import { jsx as jsx5 } from "react/jsx-runtime";
var ChatContext = createContext2(null);
function useChat() {
  const ctx = useContext2(ChatContext);
  if (!ctx) throw new Error("useChat must be used within ChatProvider");
  return ctx;
}
var ChatProvider = ({ children }) => {
  const { client, setModel, updateStats } = useConfig();
  const cancelRef = useRef2(null);
  const submittingRef = useRef2(false);
  const [history, setHistory] = useState2([]);
  const [pending, setPending] = useState2([]);
  const [staticRemountKey, setStaticRemountKey] = useState2(0);
  const [isStreaming, setIsStreaming] = useState2(false);
  const [permReq, setPermReq] = useState2(null);
  const pushHistory = useCallback2((role, content) => {
    setHistory((prev) => [...prev, { id: genId(), role, content }]);
  }, []);
  const pushBanner = useCallback2((version, model, tools) => {
    setHistory([{
      id: genId(),
      role: "banner",
      content: "",
      bannerData: { version, model, tools }
    }]);
  }, []);
  const clearHistory = useCallback2(() => {
    setHistory([]);
    setPending([]);
    setStaticRemountKey((k) => k + 1);
    updateStats({ tokenCount: 0 });
  }, [updateStats]);
  const remountStatic = useCallback2(() => {
    setStaticRemountKey((k) => k + 1);
  }, []);
  const cancelStream = useCallback2(() => {
    if (cancelRef.current) {
      cancelRef.current();
      cancelRef.current = null;
    }
    setIsStreaming(false);
    setPending((prev) => {
      const finalized = prev.map((m) => m.isStreaming ? { ...m, isStreaming: false } : m);
      setHistory((h) => [...h, ...finalized, { id: genId(), role: "error", content: "\u26D4 Interrupted" }]);
      return [];
    });
  }, []);
  const startChat = useCallback2((message) => {
    if (!client || isStreaming) return;
    if (submittingRef.current) return;
    submittingRef.current = true;
    setHistory((prev) => [...prev, { id: genId(), role: "user", content: message }]);
    setIsStreaming(true);
    const handle = client.chat(
      message,
      // onEvent
      (event) => {
        setPending((prev) => {
          const updated = processEvent(event, prev);
          const completed = updated.filter((m) => !m.isStreaming);
          const stillPending = updated.filter((m) => m.isStreaming);
          if (completed.length > 0) {
            setHistory((h) => [...h, ...completed]);
            return stillPending;
          }
          return updated;
        });
        if (event.type === "approval_request") {
          setPermReq({
            toolName: event.toolName,
            toolInput: event.toolInput,
            reason: event.text || "Supervised mode: requires approval",
            callId: event.callId
          });
        }
      },
      // onEnd
      () => {
        submittingRef.current = false;
        setIsStreaming(false);
        cancelRef.current = null;
        setPending((prev) => {
          const finalized = prev.map((m) => m.isStreaming ? { ...m, isStreaming: false } : m);
          setHistory((h) => [...h, ...finalized]);
          return [];
        });
      },
      // onError
      (err) => {
        submittingRef.current = false;
        setIsStreaming(false);
        cancelRef.current = null;
        setPending((prev) => {
          const finalized = prev.map((m) => m.isStreaming ? { ...m, isStreaming: false } : m);
          setHistory((h) => [...h, ...finalized, { id: genId(), role: "error", content: err.message }]);
          return [];
        });
      }
    );
    cancelRef.current = handle.cancel;
  }, [client, isStreaming, setModel, updateStats]);
  const resolveApproval = useCallback2(async (approved) => {
    if (client && permReq?.callId) {
      try {
        await client.approveToolCall(permReq.callId, approved);
      } catch {
      }
    }
    setPermReq(null);
  }, [client, permReq]);
  const value = {
    history,
    pending,
    staticRemountKey,
    isStreaming,
    permReq,
    startChat,
    cancelStream,
    clearHistory,
    pushHistory,
    pushBanner,
    resolveApproval,
    remountStatic
  };
  return /* @__PURE__ */ jsx5(ChatContext.Provider, { value, children });
};

// src/contexts/UIContext.tsx
import { createContext as createContext3, useContext as useContext3, useState as useState3, useCallback as useCallback3 } from "react";
import { jsx as jsx6 } from "react/jsx-runtime";
var UIContext = createContext3(null);
function useUI() {
  const ctx = useContext3(UIContext);
  if (!ctx) throw new Error("useUI must be used within UIProvider");
  return ctx;
}
var UIProvider = ({ children }) => {
  const [appState, setAppState] = useState3("idle");
  const [mode, setMode] = useState3("auto");
  const [selectorTitle, setSelectorTitle] = useState3("");
  const [selectorItems, setSelectorItems] = useState3([]);
  const [selectorIndex, setSelectorIndex] = useState3(0);
  const [selectorCmd, setSelectorCmd] = useState3("");
  const [diffs, setDiffs] = useState3([]);
  const openSelector = useCallback3((title, items, cmd) => {
    setSelectorTitle(title);
    setSelectorItems(items);
    setSelectorIndex(0);
    setSelectorCmd(cmd);
    setAppState("selecting");
  }, []);
  const closeSelector = useCallback3(() => {
    setAppState("idle");
  }, []);
  const openDiffs = useCallback3((newDiffs) => {
    setDiffs(newDiffs);
    setAppState("diffing");
  }, []);
  const value = {
    appState,
    selectorTitle,
    selectorItems,
    selectorIndex,
    selectorCmd,
    diffs,
    mode,
    setAppState,
    setMode,
    openSelector,
    closeSelector,
    setSelectorIndex,
    openDiffs
  };
  return /* @__PURE__ */ jsx6(UIContext.Provider, { value, children });
};

// src/components/StatusBar.tsx
import { Text as Text4 } from "ink";
import { jsx as jsx7 } from "react/jsx-runtime";
function fmtTok(n) {
  if (n <= 0) return "0";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}
function fmtCost(usd) {
  if (usd <= 0) return "";
  return usd > 0.5 ? `~$${usd.toFixed(2)}` : `~$${usd.toFixed(4)}`;
}
var StatusBar = ({
  model,
  mode,
  isStreaming,
  inputTokens,
  outputTokens,
  contextUsedPct,
  costUsd,
  duration
}) => {
  const parts = [];
  const cost = fmtCost(costUsd);
  if (cost) parts.push(cost);
  if (inputTokens > 0 || outputTokens > 0) {
    parts.push("in:" + fmtTok(inputTokens) + " out:" + fmtTok(outputTokens));
  }
  if (duration && duration > 0) parts.push(duration.toFixed(1) + "s");
  if (contextUsedPct > 0) parts.push(contextUsedPct + "%");
  const modeLabel = mode.charAt(0).toUpperCase() + mode.slice(1);
  const shortModel = model.split("/").pop() || model;
  parts.push("... " + modeLabel + " /" + shortModel);
  const right = " " + parts.join("  ") + " -";
  const termW = process.stdout.columns || 80;
  const dashes = Math.max(0, termW - right.length - 2);
  return /* @__PURE__ */ jsx7(Text4, { dimColor: true, children: "-".repeat(dashes) + right });
};

// src/components/InputArea.tsx
import React8, { useState as useState6, useCallback as useCallback5 } from "react";
import { Box as Box5, Text as Text7, useInput as useInput3 } from "ink";

// src/commands/index.ts
var section = (title) => `\u25C7 ${title}
`;
var kv = (label, value) => `  ${label.padEnd(12)} ${value}`;
var icon = (enabled) => enabled ? "\u25CF" : "\u25CB";
var slashCommands = [
  {
    name: "/help",
    description: "Show available commands",
    execute: async () => {
      const cmds = slashCommands.map((c) => `  ${c.name.padEnd(18)} ${c.description}`).join("\n");
      const shortcuts = [
        "  Ctrl+C       Interrupt / double-tap to exit",
        "  Ctrl+D       Exit",
        "  Ctrl+L       Clear screen",
        "  \u2191/\u2193          History navigation",
        "  Tab          Autocomplete",
        "  `start       Multi-line input"
      ].join("\n");
      return `${section("Commands")}
${cmds}

${section("Shortcuts")}
${shortcuts}`;
    }
  },
  { name: "/quit", description: "Exit", execute: async (_c, _a, cb) => {
    cb.exit();
    return null;
  } },
  { name: "/exit", description: "Exit (alias)", execute: async (_c, _a, cb) => {
    cb.exit();
    return null;
  } },
  // ─── History & Context ───
  {
    name: "/clear",
    description: "Clear conversation",
    execute: async (client, _a, cb) => {
      await client.clearHistory();
      cb.clearMessages();
      return null;
    }
  },
  {
    name: "/new",
    description: "New session",
    execute: async (client, _a, cb) => {
      await client.newSession();
      cb.clearMessages();
      return "\u{1F504} New session started";
    }
  },
  {
    name: "/compact",
    description: "Compact context",
    execute: async (client, args2) => {
      const res = await client.compactContext(args2);
      return `\u2713 ${res.message}`;
    }
  },
  {
    name: "/history",
    description: "Show conversation history",
    execute: async (client) => {
      const res = await client.getHistory();
      if (!res.messages?.length) return "No messages";
      const lines = res.messages.map((m, i) => {
        const content = m.content.length > 80 ? m.content.slice(0, 77) + "..." : m.content;
        return `  ${i + 1}. [${m.role}] ${content}`;
      });
      return `${section(`History (${res.messages.length})`)}
${lines.join("\n")}`;
    }
  },
  // ─── Models ───
  {
    name: "/models",
    description: "List models",
    execute: async (client) => {
      const res = await client.listModels();
      const lines = res.models.map((m) => {
        const cur = m.id === res.currentModel || m.alias === res.currentModel ? "\u25B8 " : "  ";
        const alias = m.alias ? ` (${m.alias})` : "";
        return `  ${cur}${m.id}${alias} [${m.provider}]`;
      });
      return `${section(`Models (${res.models.length})`)}
${lines.join("\n")}`;
    }
  },
  {
    name: "/model",
    description: "Switch model",
    hasSelector: true,
    execute: async (client, args2, cb) => {
      if (!args2) return null;
      await client.switchModel(args2);
      cb.setModel(args2);
      return `\u2713 Switched to ${args2}`;
    }
  },
  // ─── Mode & Think ───
  {
    name: "/mode",
    description: "Switch planning mode",
    hasSelector: true,
    options: [
      { label: "forge", value: "forge", description: "Deep planning + execution" },
      { label: "rush", value: "rush", description: "Fast, skip planning" },
      { label: "auto", value: "auto", description: "Auto-detect complexity" }
    ],
    execute: async (client, args2, cb) => {
      if (!args2) return null;
      await client.updateSettings({ planningMode: args2 });
      cb.setMode(args2);
      return `\u2713 Mode: ${args2}`;
    }
  },
  {
    name: "/think",
    description: "Set thinking level",
    hasSelector: true,
    options: [
      { label: "off", value: "off", description: "No extended thinking" },
      { label: "low", value: "low", description: "Brief" },
      { label: "medium", value: "medium", description: "Moderate" },
      { label: "high", value: "high", description: "Deep reasoning" }
    ],
    execute: async (client, args2) => {
      if (!args2) return null;
      await client.updateSettings({ thinkLevel: args2 });
      return `\u2713 Think: ${args2}`;
    }
  },
  // ─── Settings ───
  {
    name: "/settings",
    description: "View/edit settings",
    hasSelector: true,
    options: [
      { label: "think", value: "think", description: "off/low/medium/high" },
      { label: "verbose", value: "verbose", description: "true/false" },
      { label: "reasoning", value: "reasoning", description: "native/tag/off" },
      { label: "usage", value: "usage", description: "full/compact/off" },
      { label: "activation", value: "activation", description: "always/mention/command" },
      { label: "send_policy", value: "send_policy", description: "stream/batch" }
    ],
    execute: async (client, args2) => {
      if (!args2) {
        const s = await client.getSettings();
        return [
          section("Settings"),
          kv("think", s.thinkLevel),
          kv("verbose", String(s.verbose)),
          kv("reasoning", s.reasoning),
          kv("usage", s.usageMode),
          kv("activation", s.activation),
          kv("send_policy", s.sendPolicy),
          kv("delegation", s.delegation),
          kv("planning", s.planningMode),
          "",
          "  Usage: /settings <key> <value>"
        ].join("\n");
      }
      const parts = args2.split(/\s+/);
      if (parts.length < 2) return `Usage: /settings ${parts[0]} <value>`;
      const [key, ...rest] = parts;
      const value = rest.join(" ");
      const updates = {};
      const keyMap = {
        think: "thinkLevel",
        verbose: "verbose",
        reasoning: "reasoning",
        usage: "usageMode",
        activation: "activation",
        send_policy: "sendPolicy"
      };
      const mapped = keyMap[key];
      if (!mapped) return `Unknown setting: ${key}`;
      updates[mapped] = value;
      const res = await client.updateSettings(updates);
      return `\u2713 ${res.message}`;
    }
  },
  // ─── Status & Stats ───
  {
    name: "/status",
    description: "Show session status",
    execute: async (client) => {
      const st = await client.getStatus();
      const ctx = await client.getContextStats();
      const pct = ctx.maxTokens > 0 ? Math.round(ctx.tokenCount / ctx.maxTokens * 100) : 0;
      return [
        section("Status"),
        kv("Model", st.model),
        kv("State", st.runState),
        kv("Messages", String(ctx.messageCount)),
        kv("Context", `${ctx.tokenCount.toLocaleString()} / ${ctx.maxTokens.toLocaleString()} (${pct}%)`)
      ].join("\n");
    }
  },
  {
    name: "/stats",
    description: "Usage statistics",
    execute: async (client) => {
      const s = await client.getSessionStats();
      const g = await client.getGlobalStats();
      return [
        section("Statistics"),
        "  Session:",
        kv("  Tokens", `${s.totalTokens} (\u2191${s.inputTokens} \u2193${s.outputTokens})`),
        kv("  Tool calls", String(s.toolCalls)),
        kv("  Turns", String(s.turnCount)),
        "  Global:",
        kv("  Sessions", String(g.totalSessions)),
        kv("  Tokens", String(g.totalTokens)),
        kv("  Tool calls", String(g.totalToolCalls))
      ].join("\n");
    }
  },
  {
    name: "/cost",
    description: "Token cost estimate",
    execute: async (client) => {
      const ctx = await client.getContextStats();
      const cost = ctx.tokenCount * 3e-3 / 1e3;
      const pct = ctx.maxTokens > 0 ? Math.round(ctx.tokenCount / ctx.maxTokens * 100) : 0;
      return [
        section("Cost"),
        kv("Tokens", ctx.tokenCount.toLocaleString()),
        kv("Context", `${pct}%`),
        kv("Est. cost", `~$${cost.toFixed(4)}`)
      ].join("\n");
    }
  },
  // ─── Tools & Skills ───
  {
    name: "/tools",
    description: "List tools",
    execute: async (client) => {
      const res = await client.listTools();
      const lines = res.tools.map((t) => {
        const desc = t.description.length > 50 ? t.description.slice(0, 47) + "..." : t.description;
        return `  ${icon(t.enabled)} ${t.name.padEnd(16)} ${desc}`;
      });
      return `${section(`Tools (${res.tools.length})`)}
${lines.join("\n")}`;
    }
  },
  {
    name: "/skills",
    description: "List skills",
    execute: async (client) => {
      const res = await client.listSkills();
      const lines = res.skills.map((s) => `  ${icon(s.enabled)} ${s.name}`);
      return `${section(`Skills (${res.skills.length})`)}
${lines.join("\n")}`;
    }
  },
  // ─── System ───
  {
    name: "/info",
    description: "System information",
    execute: async (client) => {
      const i = await client.getSystemInfo();
      return [
        section("System Info"),
        kv("Version", i.version),
        kv("Go", i.goVersion),
        kv("Uptime", `${Math.round(i.uptimeMs / 36e5)}h`),
        kv("OS/Arch", `${i.os}/${i.arch}`),
        kv("Models", String(i.models)),
        kv("Tools", String(i.tools)),
        kv("Skills", String(i.skills))
      ].join("\n");
    }
  },
  {
    name: "/version",
    description: "Show version",
    execute: async (client) => {
      const h = await client.healthCheck();
      return `NGOClaw v${h.version}`;
    }
  },
  {
    name: "/doctor",
    description: "Health check",
    execute: async (client) => {
      const h = await client.healthCheck();
      return [
        section("Health Check"),
        kv("Healthy", h.healthy ? "\u2713" : "\u2717"),
        kv("Version", h.version),
        kv("Model", h.model),
        kv("Tools", String(h.tools))
      ].join("\n");
    }
  },
  // ─── Security ───
  {
    name: "/security",
    description: "Security policy",
    execute: async (client) => {
      const s = await client.getSecurity();
      const modeName = {
        eager: "auto (auto-approve)",
        auto: "standard (ask)",
        off: "strict (ask all)"
      };
      return [
        section("Security Policy"),
        kv("Mode", modeName[s.approvalMode] || s.approvalMode),
        kv("Trusted", s.trustedTools?.join(", ") || "none"),
        kv("Dangerous", s.dangerousTools?.join(", ") || "none"),
        kv("Trusted Cmds", s.trustedCommands?.join(", ") || "none")
      ].join("\n");
    }
  },
  // ─── Cron ───
  {
    name: "/cron",
    description: "Cron job management",
    execute: async (client, args2) => {
      if (!args2) {
        const res = await client.listCronJobs();
        if (!res.jobs?.length) return "No cron jobs";
        const lines = res.jobs.map(
          (j) => `  ${icon(j.enabled)} ${j.name.padEnd(16)} ${j.schedule}  runs:${j.runCount} fails:${j.failCount}`
        );
        return `${section(`Cron Jobs (${res.jobs.length})`)}
${lines.join("\n")}`;
      }
      const parts = args2.split(/\s+/);
      const sub = parts[0];
      const name = parts[1];
      if (!name && sub !== "list") return "Usage: /cron <remove|enable|disable|run> <name>";
      switch (sub) {
        case "remove":
        case "rm": {
          const r = await client.cronRemove(name);
          return `\u2713 ${r.message}`;
        }
        case "enable": {
          const r = await client.cronEnable(name);
          return `\u2713 ${r.message}`;
        }
        case "disable": {
          const r = await client.cronDisable(name);
          return `\u2713 ${r.message}`;
        }
        case "run": {
          const r = await client.cronRunNow(name);
          return `\u2713 ${r.message}`;
        }
        default:
          return "Usage: /cron [remove|enable|disable|run] <name>";
      }
    }
  },
  // ─── Tasks & Diff (placeholder) ───
  { name: "/tasks", description: "Background tasks", execute: async () => "No background tasks" },
  { name: "/diff", description: "File changes", execute: async () => "Diff viewer coming in Batch 2" }
];
function findCommand(input) {
  const cmd = input.split(" ")[0]?.toLowerCase();
  return slashCommands.find((c) => c.name === cmd);
}
function getCommandArgs(input) {
  const idx = input.indexOf(" ");
  return idx > 0 ? input.slice(idx + 1).trim() : "";
}
function completeCommand(partial) {
  if (!partial.startsWith("/")) return [];
  return slashCommands.filter((c) => c.name.startsWith(partial.toLowerCase())).map((c) => c.name);
}

// src/components/ViInput.tsx
import { useState as useState4, useEffect as useEffect2 } from "react";
import { Text as Text5, useInput as useInput2 } from "ink";
import { jsx as jsx8, jsxs as jsxs3 } from "react/jsx-runtime";
var ViInput = ({
  value,
  onChange,
  onSubmit,
  onModeChange,
  placeholder = "",
  isStreaming = false,
  onCancel,
  onArrowUp,
  onArrowDown,
  onTab
}) => {
  const [mode, setMode] = useState4("INSERT");
  const [cursor, setCursor] = useState4(0);
  useEffect2(() => {
    if (cursor > value.length) setCursor(value.length);
  }, [value, cursor]);
  useEffect2(() => {
    if (onModeChange) onModeChange(mode);
  }, [mode, onModeChange]);
  useInput2((ch, key) => {
    if (isStreaming) {
      if (key.ctrl && ch === "c" && onCancel) {
        onCancel();
      }
      return;
    }
    if (key.escape) {
      setMode("NORMAL");
      setCursor((prev) => Math.max(0, prev - 1));
      return;
    }
    if (key.ctrl && ch === "c") {
      return;
    }
    if (key.upArrow && onArrowUp) {
      onArrowUp();
      return;
    }
    if (key.downArrow && onArrowDown) {
      onArrowDown();
      return;
    }
    if (key.tab && onTab) {
      onTab();
      return;
    }
    if (mode === "NORMAL") {
      if (key.return) {
        onSubmit(value);
        setCursor(0);
        return;
      }
      if (key.leftArrow || ch === "h") {
        setCursor((p) => Math.max(0, p - 1));
      } else if (key.rightArrow || ch === "l") {
        setCursor((p) => Math.min(value.length > 0 ? value.length - 1 : 0, p + 1));
      } else if (ch === "i") {
        setMode("INSERT");
      } else if (ch === "a") {
        setMode("INSERT");
        setCursor((p) => Math.min(value.length, p + 1));
      } else if (ch === "A") {
        setMode("INSERT");
        setCursor(value.length);
      } else if (ch === "I") {
        setMode("INSERT");
        setCursor(0);
      } else if (ch === "0") {
        setCursor(0);
      } else if (ch === "$") {
        setCursor(value.length > 0 ? value.length - 1 : 0);
      } else if (ch === "w") {
        let newC = cursor;
        while (newC < value.length - 1 && value[newC] !== " ") newC++;
        while (newC < value.length - 1 && value[newC] === " ") newC++;
        setCursor(newC);
      } else if (ch === "b") {
        let newC = cursor;
        if (newC > 0 && value[newC - 1] === " ") newC--;
        while (newC > 0 && value[newC] === " ") newC--;
        while (newC > 0 && value[newC - 1] !== " ") newC--;
        setCursor(newC);
      } else if (ch === "x") {
        if (value.length > 0) {
          const nextVal = value.slice(0, cursor) + value.slice(cursor + 1);
          onChange(nextVal);
          if (cursor >= nextVal.length) setCursor(Math.max(0, nextVal.length - 1));
        }
      } else if (ch === "S" || ch === "s") {
        if (ch === "s") {
          const nextVal = value.slice(0, cursor) + value.slice(cursor + 1);
          onChange(nextVal);
          setMode("INSERT");
        }
      }
      return;
    }
    if (mode === "INSERT") {
      if (key.return) {
        onSubmit(value);
        setCursor(0);
        return;
      }
      if (key.leftArrow) {
        setCursor((p) => Math.max(0, p - 1));
      } else if (key.rightArrow) {
        setCursor((p) => Math.min(value.length, p + 1));
      } else if (key.backspace || key.delete) {
        if (cursor > 0) {
          onChange(value.slice(0, cursor - 1) + value.slice(cursor));
          setCursor((p) => p - 1);
        }
      } else if (ch) {
        onChange(value.slice(0, cursor) + ch + value.slice(cursor));
        setCursor((p) => p + ch.length);
      }
      return;
    }
  });
  if (isStreaming) {
    return /* @__PURE__ */ jsx8(Text5, { color: "gray", children: "Agent working... (Ctrl+C to interrupt)" });
  }
  if (!value && placeholder) {
    return /* @__PURE__ */ jsxs3(Text5, { color: "gray", children: [
      /* @__PURE__ */ jsx8(Text5, { inverse: cursor === 0, color: mode === "NORMAL" ? "yellow" : "white", children: placeholder[0] }),
      placeholder.slice(1)
    ] });
  }
  const before = value.slice(0, cursor);
  const at = value[cursor] || " ";
  const after = value.slice(cursor + 1);
  return /* @__PURE__ */ jsxs3(Text5, { children: [
    before,
    /* @__PURE__ */ jsx8(Text5, { inverse: true, color: mode === "NORMAL" ? "yellow" : "white", children: at }),
    after
  ] });
};

// src/components/Spinner.tsx
import { useState as useState5, useEffect as useEffect3 } from "react";
import { Text as Text6 } from "ink";
import { jsx as jsx9, jsxs as jsxs4 } from "react/jsx-runtime";
var ANIMATIONS = [
  "Thinking\u2026",
  "Pondering\u2026",
  "Percolating\u2026",
  "Crystallizing\u2026",
  "Illuminating\u2026",
  "Synthesizing\u2026",
  "Undulating\u2026",
  "Unfurling\u2026",
  "Incubating\u2026",
  "Spiraling\u2026"
];
var PREFIXES = ["\u25CF", "\u2736", "\u2722", "\xB7", "\u25C6", "\u25CB", "\u25C7", "\u2605"];
var Spinner2 = ({ isActive }) => {
  const [text, setText] = useState5(ANIMATIONS[0]);
  const [prefix, setPrefix] = useState5(PREFIXES[0]);
  useEffect3(() => {
    if (!isActive) return;
    const pick = () => {
      setText(ANIMATIONS[Math.floor(Math.random() * ANIMATIONS.length)]);
      setPrefix(PREFIXES[Math.floor(Math.random() * PREFIXES.length)]);
    };
    pick();
    const timer = setInterval(pick, 2500);
    return () => clearInterval(timer);
  }, [isActive]);
  if (!isActive) return null;
  return /* @__PURE__ */ jsxs4(Text6, { children: [
    /* @__PURE__ */ jsxs4(Text6, { color: "cyan", children: [
      prefix,
      " "
    ] }),
    /* @__PURE__ */ jsx9(Text6, { color: "gray", children: text })
  ] });
};

// src/components/InputArea.tsx
import { jsx as jsx10, jsxs as jsxs5 } from "react/jsx-runtime";
var InputArea = ({
  onSubmit,
  isStreaming,
  onCancel
}) => {
  const [value, setValue] = useState6("");
  const [history, setHistory] = useState6([]);
  const [historyIdx, setHistoryIdx] = useState6(-1);
  const [completions, setCompletions] = useState6([]);
  const [multiLine, setMultiLine] = useState6(false);
  const [multiLineBuf, setMultiLineBuf] = useState6([]);
  const [viMode, setViMode] = useState6("INSERT");
  useInput3((input, key) => {
    if (key.ctrl && input === "c") {
      if (isStreaming) {
        onCancel();
      } else if (multiLine) {
        setMultiLine(false);
        setMultiLineBuf([]);
        setValue("");
      }
      return;
    }
  });
  const handleArrowUp = useCallback5(() => {
    if (!multiLine && history.length > 0) {
      const newIdx = Math.min(historyIdx + 1, history.length - 1);
      setHistoryIdx(newIdx);
      setValue(history[history.length - 1 - newIdx] || "");
    }
  }, [multiLine, history, historyIdx]);
  const handleArrowDown = useCallback5(() => {
    if (!multiLine) {
      const newIdx = Math.max(historyIdx - 1, -1);
      setHistoryIdx(newIdx);
      setValue(newIdx < 0 ? "" : history[history.length - 1 - newIdx] || "");
    }
  }, [multiLine, history, historyIdx]);
  const handleTab = useCallback5(() => {
    if (value.startsWith("/")) {
      const matches = completeCommand(value);
      if (matches.length === 1) {
        setValue(matches[0] + " ");
        setCompletions([]);
      } else if (matches.length > 1) {
        setCompletions(matches);
      }
    }
  }, [value]);
  React8.useEffect(() => {
    if (completions.length > 0) setCompletions([]);
  }, [value]);
  const handleSubmit = useCallback5((text) => {
    const trimmed = text.trim();
    if (!multiLine && trimmed.startsWith("`") && !trimmed.endsWith("`")) {
      setMultiLine(true);
      setMultiLineBuf([trimmed.slice(1)]);
      setValue("");
      return;
    }
    if (multiLine) {
      if (trimmed.endsWith("`")) {
        const lastLine = trimmed.slice(0, -1);
        const full = [...multiLineBuf, lastLine].join("\n");
        setMultiLine(false);
        setMultiLineBuf([]);
        if (full.trim()) {
          setHistory((prev) => [...prev, full.trim()]);
          setHistoryIdx(-1);
          onSubmit(full.trim());
        }
      } else {
        setMultiLineBuf((prev) => [...prev, text]);
      }
      setValue("");
      return;
    }
    if (!trimmed) return;
    setHistory((prev) => [...prev, trimmed]);
    setHistoryIdx(-1);
    setValue("");
    onSubmit(trimmed);
  }, [onSubmit, multiLine, multiLineBuf]);
  if (isStreaming) {
    return /* @__PURE__ */ jsxs5(Box5, { flexDirection: "column", children: [
      /* @__PURE__ */ jsx10(Spinner2, { isActive: true }),
      /* @__PURE__ */ jsx10(Box5, { paddingTop: 1, children: /* @__PURE__ */ jsx10(Text7, { dimColor: true, children: "esc to interrupt" }) })
    ] });
  }
  return /* @__PURE__ */ jsxs5(Box5, { flexDirection: "column", children: [
    completions.length > 0 && /* @__PURE__ */ jsx10(Box5, { children: /* @__PURE__ */ jsxs5(Text7, { color: "gray", children: [
      "  ",
      completions.join("  ")
    ] }) }),
    multiLine && multiLineBuf.length > 0 && /* @__PURE__ */ jsx10(Box5, { flexDirection: "column", children: multiLineBuf.map((line, i) => /* @__PURE__ */ jsxs5(Text7, { color: "gray", children: [
      "  \u2026 ",
      line
    ] }, i)) }),
    /* @__PURE__ */ jsxs5(Box5, { children: [
      /* @__PURE__ */ jsx10(Text7, { color: viMode === "NORMAL" ? "yellow" : "cyan", bold: true, children: viMode === "NORMAL" ? "[N] " : "\u276F " }),
      /* @__PURE__ */ jsx10(
        ViInput,
        {
          value,
          onChange: setValue,
          onSubmit: handleSubmit,
          onModeChange: setViMode,
          placeholder: multiLine ? "End with ` to submit" : "Type your message...",
          isStreaming,
          onCancel,
          onArrowUp: handleArrowUp,
          onArrowDown: handleArrowDown,
          onTab: handleTab
        }
      )
    ] })
  ] });
};

// src/components/PermissionRequest.tsx
import { useState as useState7 } from "react";
import { Box as Box6, Text as Text8, useInput as useInput4 } from "ink";
import { jsx as jsx11, jsxs as jsxs6 } from "react/jsx-runtime";
var PermissionRequest = ({
  toolName,
  toolInput,
  reason,
  approvalMode,
  onCycleMode,
  onDecision
}) => {
  const [decided, setDecided] = useState7(false);
  const [showDetail, setShowDetail] = useState7(false);
  useInput4((input, key) => {
    if (decided) return;
    if (key.ctrl && input === "e") {
      setShowDetail((prev) => !prev);
      return;
    }
    if (key.tab && key.shift) {
      onCycleMode();
      return;
    }
    const lower = input.toLowerCase();
    if (lower === "y") {
      setDecided(true);
      onDecision("allow_once");
    } else if (lower === "n" || key.escape) {
      setDecided(true);
      onDecision("deny");
    }
  });
  const isShell = toolName.toLowerCase().includes("bash") || toolName.toLowerCase().includes("command");
  const preview = showDetail ? toolInput : toolInput.length > 120 ? toolInput.slice(0, 117) + "..." : toolInput;
  return /* @__PURE__ */ jsxs6(
    Box6,
    {
      flexDirection: "column",
      borderStyle: "round",
      borderColor: "yellow",
      paddingX: 1,
      children: [
        /* @__PURE__ */ jsxs6(Box6, { marginBottom: 1, justifyContent: "space-between", children: [
          /* @__PURE__ */ jsx11(Text8, { color: "yellow", bold: true, children: "\u26A0 Permission Required" }),
          /* @__PURE__ */ jsxs6(Box6, { children: [
            /* @__PURE__ */ jsx11(Text8, { color: "gray", children: "Mode: " }),
            /* @__PURE__ */ jsx11(Text8, { color: "magenta", bold: true, children: approvalMode }),
            /* @__PURE__ */ jsx11(Text8, { color: "gray", dimColor: true, children: " (\u21E7Tab to cycle)  Ctrl+E details" })
          ] })
        ] }),
        /* @__PURE__ */ jsxs6(Box6, { children: [
          /* @__PURE__ */ jsx11(Text8, { color: "white", bold: true, children: "Tool: " }),
          /* @__PURE__ */ jsx11(Text8, { color: "cyan", bold: true, children: toolName })
        ] }),
        reason && /* @__PURE__ */ jsxs6(Box6, { children: [
          /* @__PURE__ */ jsx11(Text8, { color: "white", bold: true, children: "Why:  " }),
          /* @__PURE__ */ jsx11(Text8, { color: "gray", children: reason })
        ] }),
        /* @__PURE__ */ jsx11(Box6, { marginTop: 1, flexDirection: "column", children: isShell ? /* @__PURE__ */ jsxs6(Text8, { color: "white", children: [
          /* @__PURE__ */ jsx11(Text8, { color: "green", children: "$ " }),
          preview
        ] }) : /* @__PURE__ */ jsx11(Text8, { color: "gray", children: preview }) }),
        /* @__PURE__ */ jsx11(Box6, { marginTop: 1, children: /* @__PURE__ */ jsxs6(Text8, { children: [
          /* @__PURE__ */ jsx11(Text8, { color: "green", bold: true, children: "y" }),
          /* @__PURE__ */ jsx11(Text8, { color: "gray", children: " Allow  " }),
          /* @__PURE__ */ jsx11(Text8, { color: "red", bold: true, children: "n" }),
          /* @__PURE__ */ jsx11(Text8, { color: "gray", children: " Deny  " }),
          /* @__PURE__ */ jsx11(Text8, { color: "gray", dimColor: true, children: "Esc = Deny" })
        ] }) })
      ]
    }
  );
};

// src/components/Selector.tsx
import { Box as Box7, Text as Text9, useInput as useInput5 } from "ink";
import { jsx as jsx12, jsxs as jsxs7 } from "react/jsx-runtime";
var Selector = ({
  title,
  items,
  selectedIndex,
  onSelect,
  onCancel,
  onMove
}) => {
  useInput5((_input, key) => {
    if (key.upArrow) {
      onMove(Math.max(0, selectedIndex - 1));
    } else if (key.downArrow) {
      onMove(Math.min(items.length - 1, selectedIndex + 1));
    } else if (key.return) {
      const item = items[selectedIndex];
      if (item) onSelect(item.value);
    } else if (key.escape) {
      onCancel();
    }
  });
  return /* @__PURE__ */ jsxs7(Box7, { flexDirection: "column", children: [
    /* @__PURE__ */ jsx12(Text9, { color: "cyan", bold: true, children: title }),
    items.map((item, i) => {
      const isSelected = i === selectedIndex;
      const marker = item.current ? " \u25C0" : "";
      return /* @__PURE__ */ jsxs7(Box7, { children: [
        /* @__PURE__ */ jsxs7(Text9, { color: isSelected ? "green" : "white", children: [
          isSelected ? "\u276F " : "  ",
          item.label,
          marker
        ] }),
        item.description && /* @__PURE__ */ jsxs7(Text9, { color: "gray", children: [
          " \u2014 ",
          item.description
        ] })
      ] }, item.value);
    }),
    /* @__PURE__ */ jsx12(Text9, { color: "gray", dimColor: true, children: "\u2191\u2193 navigate  \u23CE select  Esc cancel" })
  ] });
};

// src/components/HelpDialog.tsx
import { Box as Box8, Text as Text10, useInput as useInput6 } from "ink";
import { jsx as jsx13, jsxs as jsxs8 } from "react/jsx-runtime";
var HelpDialog = ({ onClose }) => {
  useInput6((_input, key) => {
    if (key.escape || key.return) {
      onClose();
    }
  });
  const shortcuts = [
    { key: "Ctrl+C", desc: "Interrupt / double-tap exit" },
    { key: "Ctrl+D", desc: "Exit" },
    { key: "Ctrl+L", desc: "Clear screen" },
    { key: "\u2191 / \u2193", desc: "History navigation" },
    { key: "Ctrl+Up", desc: "Message selector (History)" },
    { key: "Tab", desc: "Autocomplete (/ commands)" },
    { key: "` start", desc: "Multi-line input" },
    { key: "Ctrl+E", desc: "Toggle permission details" },
    { key: "Esc", desc: "Cancel / close dialog / Normal mode" }
  ];
  return /* @__PURE__ */ jsxs8(
    Box8,
    {
      flexDirection: "column",
      borderStyle: "round",
      borderColor: "cyan",
      paddingX: 1,
      paddingY: 0,
      children: [
        /* @__PURE__ */ jsx13(Box8, { marginBottom: 1, children: /* @__PURE__ */ jsx13(Text10, { color: "cyan", bold: true, children: "\u25C7 Commands" }) }),
        slashCommands.map((cmd) => /* @__PURE__ */ jsxs8(Box8, { children: [
          /* @__PURE__ */ jsx13(Text10, { color: "green", children: cmd.name.padEnd(18) }),
          /* @__PURE__ */ jsx13(Text10, { color: "gray", children: cmd.description })
        ] }, cmd.name)),
        /* @__PURE__ */ jsx13(Box8, { marginTop: 1, marginBottom: 1, children: /* @__PURE__ */ jsx13(Text10, { color: "cyan", bold: true, children: "\u25C7 Shortcuts" }) }),
        shortcuts.map((s) => /* @__PURE__ */ jsxs8(Box8, { children: [
          /* @__PURE__ */ jsx13(Text10, { color: "green", children: s.key.padEnd(18) }),
          /* @__PURE__ */ jsx13(Text10, { color: "gray", children: s.desc })
        ] }, s.key)),
        /* @__PURE__ */ jsx13(Box8, { marginTop: 1, children: /* @__PURE__ */ jsx13(Text10, { color: "gray", dimColor: true, children: "Press Esc or Enter to close" }) })
      ]
    }
  );
};

// src/components/MessageSelectorDialog.tsx
import { useState as useState8 } from "react";
import { Box as Box9, Text as Text11, useInput as useInput7 } from "ink";
import { jsx as jsx14, jsxs as jsxs9 } from "react/jsx-runtime";
var MessageSelectorDialog = ({ messages, onSelect, onCancel }) => {
  const navigable = messages.filter((m) => (m.role === "user" || m.role === "assistant") && m.content.trim());
  const [selectedIndex, setSelectedIndex] = useState8(Math.max(0, navigable.length - 1));
  useInput7((ch, key) => {
    if (key.escape) {
      onCancel();
      return;
    }
    if (key.return) {
      if (navigable.length > 0) {
        onSelect(navigable[selectedIndex].content);
      } else {
        onCancel();
      }
      return;
    }
    if (key.upArrow || ch === "k" || ch === "K") {
      setSelectedIndex((prev) => Math.max(0, prev - 1));
    } else if (key.downArrow || ch === "j" || ch === "J") {
      setSelectedIndex((prev) => Math.min(navigable.length > 0 ? navigable.length - 1 : 0, prev + 1));
    } else if (key.ctrl && key.upArrow || ch === "g") {
      setSelectedIndex(0);
    } else if (key.ctrl && key.downArrow || ch === "G") {
      setSelectedIndex(Math.max(0, navigable.length - 1));
    }
  });
  if (navigable.length === 0) {
    return /* @__PURE__ */ jsxs9(Box9, { borderStyle: "round", borderColor: "yellow", paddingX: 1, flexDirection: "column", children: [
      /* @__PURE__ */ jsx14(Text11, { color: "yellow", children: "No message history available to select." }),
      /* @__PURE__ */ jsx14(Text11, { color: "gray", dimColor: true, children: "Press Esc to return." })
    ] });
  }
  const maxVisible = 5;
  let startIdx = selectedIndex - Math.floor(maxVisible / 2);
  if (startIdx < 0) startIdx = 0;
  if (startIdx + maxVisible > navigable.length) {
    startIdx = Math.max(0, navigable.length - maxVisible);
  }
  const visibleItems = navigable.slice(startIdx, startIdx + maxVisible);
  return /* @__PURE__ */ jsxs9(Box9, { borderStyle: "double", borderColor: "cyan", paddingX: 1, flexDirection: "column", children: [
    /* @__PURE__ */ jsx14(Box9, { marginBottom: 1, children: /* @__PURE__ */ jsx14(Text11, { color: "cyan", bold: true, children: "\u25C7 Select Message to Copy (\u2191/\u2193/j/k to move, Enter to copy, Esc to cancel)" }) }),
    visibleItems.map((m, i) => {
      const realIdx = startIdx + i;
      const isSelected = realIdx === selectedIndex;
      const prefix = m.role === "user" ? "\u276F " : "\u2B21 ";
      const color = m.role === "user" ? "blue" : "white";
      const content = m.content.split("\n")[0].slice(0, 100) + (m.content.length > 100 ? "..." : "");
      return /* @__PURE__ */ jsx14(Box9, { children: /* @__PURE__ */ jsxs9(Text11, { color: isSelected ? "yellow" : color, inverse: isSelected, bold: isSelected, children: [
        isSelected ? "\u25B6 " : "  ",
        prefix,
        content
      ] }) }, realIdx);
    })
  ] });
};

// src/components/DiffDialog.tsx
import { useState as useState9 } from "react";
import { Box as Box10, Text as Text12, useInput as useInput8 } from "ink";
import { jsx as jsx15, jsxs as jsxs10 } from "react/jsx-runtime";
var DiffDialog = ({ diffs, onClose }) => {
  const [fileIdx, setFileIdx] = useState9(0);
  useInput8((ch, key) => {
    if (key.escape || key.ctrl && ch === "c") {
      onClose();
      return;
    }
    if (key.leftArrow || ch === "h") {
      setFileIdx((p) => Math.max(0, p - 1));
    } else if (key.rightArrow || ch === "l") {
      setFileIdx((p) => Math.min(diffs.length - 1, p + 1));
    }
  });
  if (!diffs || diffs.length === 0) {
    return /* @__PURE__ */ jsxs10(Box10, { borderStyle: "round", borderColor: "blue", paddingX: 1, flexDirection: "column", children: [
      /* @__PURE__ */ jsx15(Text12, { color: "yellow", children: "No diffs available to display." }),
      /* @__PURE__ */ jsx15(Text12, { color: "gray", dimColor: true, children: "Press Esc to close." })
    ] });
  }
  const currentDiff = diffs[fileIdx];
  const lines = currentDiff.diffText.split("\n");
  const previewLines = lines.slice(0, 20);
  const hiddenCount = lines.length - 20;
  return /* @__PURE__ */ jsxs10(Box10, { borderStyle: "double", borderColor: "magenta", paddingX: 1, flexDirection: "column", children: [
    /* @__PURE__ */ jsxs10(Box10, { justifyContent: "space-between", marginBottom: 1, children: [
      /* @__PURE__ */ jsx15(Text12, { color: "magenta", bold: true, children: ` \u{1F50D} Reviewing Changes (${fileIdx + 1}/${diffs.length}) ` }),
      /* @__PURE__ */ jsx15(Text12, { color: "gray", dimColor: true, children: "\u2190/\u2192: prev/next file, Esc: close" })
    ] }),
    /* @__PURE__ */ jsxs10(Box10, { marginBottom: 1, children: [
      /* @__PURE__ */ jsx15(Text12, { color: "white", bold: true, children: "File: " }),
      /* @__PURE__ */ jsx15(Text12, { color: "cyan", children: currentDiff.file })
    ] }),
    /* @__PURE__ */ jsxs10(Box10, { flexDirection: "column", marginLeft: 2, children: [
      previewLines.map((line, i) => {
        let color = "white";
        let bg = void 0;
        if (line.startsWith("+")) {
          color = "green";
        } else if (line.startsWith("-")) {
          color = "red";
        } else if (line.startsWith("@@")) {
          color = "cyan";
        }
        return /* @__PURE__ */ jsx15(Text12, { color, backgroundColor: bg, children: line }, i);
      }),
      hiddenCount > 0 && /* @__PURE__ */ jsxs10(Text12, { color: "gray", dimColor: true, children: [
        "... ",
        hiddenCount,
        " more lines not shown"
      ] })
    ] })
  ] });
};

// src/components/App.tsx
import { jsx as jsx16, jsxs as jsxs11 } from "react/jsx-runtime";
var App = ({ serverAddr: serverAddr2 = "localhost:50051" }) => {
  return /* @__PURE__ */ jsx16(ConfigProvider, { serverAddr: serverAddr2, children: /* @__PURE__ */ jsx16(UIProvider, { children: /* @__PURE__ */ jsx16(ChatProvider, { children: /* @__PURE__ */ jsx16(AppContent, {}) }) }) });
};
var AppContent = () => {
  const { exit } = useApp();
  const config = useConfig();
  const chat = useChat();
  const ui = useUI();
  useEffect4(() => {
    if (config.ready) {
      chat.pushBanner(config.version, config.model, config.tools);
    }
  }, [config.ready]);
  useInput9((input, key) => {
    if (key.ctrl && input === "c") {
      if (chat.isStreaming) {
        chat.cancelStream();
        ui.setAppState("idle");
        return;
      }
      exit();
    }
    if (key.ctrl && input === "l") {
      process.stdout.write("\x1B[2J\x1B[H");
      chat.remountStatic();
    }
    if (key.ctrl && input === "d") exit();
    if (key.ctrl && input === "\\") {
      if (ui.appState === "idle") handleSubmit("/model");
    }
    if (key.ctrl && input === "r") {
      if (ui.appState === "idle") handleSubmit("/history");
    }
    if (key.ctrl && key.upArrow) {
      if (ui.appState === "idle") ui.setAppState("history");
    }
  });
  const cmdCallbacks = {
    setMode: (m) => ui.setMode(m),
    setModel: (m) => config.setModel(m),
    clearMessages: () => chat.clearHistory(),
    exit
  };
  const handleSelectorSelect = useCallback6(async (value) => {
    ui.closeSelector();
    if (!config.client) return;
    const cmd = findCommand(ui.selectorCmd);
    if (cmd) {
      try {
        const result = await cmd.execute(config.client, value, cmdCallbacks);
        if (result) chat.pushHistory("assistant", result);
      } catch (err) {
        chat.pushHistory("error", err.message);
      }
    }
  }, [ui.selectorCmd, config.client, cmdCallbacks]);
  const handleSubmit = useCallback6(async (text) => {
    if (!config.client || ui.appState !== "idle") return;
    const trimmed = text.trim();
    if (trimmed === "/testdiff") {
      ui.openDiffs([
        { file: "src/main.ts", diffText: '@@ -1,2 +1,3 @@\n+import * as fs from "fs";\n console.log("Init");\n-const a = 1;' },
        { file: "package.json", diffText: '@@ -10,3 +10,4 @@\n   "dependencies": {\n+    "ink": "^4.0.0",\n     "react": "^18.0.0"\n   }' }
      ]);
      return;
    }
    if (text.startsWith("/")) {
      const cmd = findCommand(text);
      const args2 = getCommandArgs(text);
      if (cmd?.name === "/help") {
        ui.setAppState("help");
        return;
      }
      if (cmd) {
        if (cmd.hasSelector && !args2) {
          let items = cmd.options || [];
          if (cmd.name === "/model") {
            try {
              const res = await config.client.listModels();
              items = res.models.map((m) => ({
                label: m.id,
                value: m.id,
                description: m.provider,
                current: m.id === res.currentModel
              }));
            } catch {
              items = [];
            }
          }
          if (cmd.name === "/mode") {
            items = items.map((it) => ({ ...it, current: it.value === ui.mode }));
          }
          ui.openSelector(cmd.description, items, cmd.name);
          return;
        }
        try {
          const result = await cmd.execute(config.client, args2, cmdCallbacks);
          if (result) chat.pushHistory("assistant", result);
        } catch (err) {
          chat.pushHistory("error", err.message);
        }
        return;
      }
    }
    ui.setAppState("streaming");
    chat.startChat(text);
  }, [config.client, ui.appState, ui.mode, cmdCallbacks]);
  useEffect4(() => {
    if (!chat.isStreaming && ui.appState === "streaming") {
      ui.setAppState("idle");
    }
    if (chat.permReq && ui.appState !== "approving") {
      ui.setAppState("approving");
    }
  }, [chat.isStreaming, chat.permReq, ui.appState]);
  if (config.connError) {
    return /* @__PURE__ */ jsx16(Box11, { flexDirection: "column", padding: 1, children: /* @__PURE__ */ jsxs11(Text13, { color: "red", bold: true, children: [
      "\u274C ",
      config.connError
    ] }) });
  }
  if (!config.ready) {
    return /* @__PURE__ */ jsx16(Box11, { padding: 1, children: /* @__PURE__ */ jsx16(Text13, { color: "gray", children: "\u23F3 Connecting to server..." }) });
  }
  const isFullScreen = ui.appState === "diffing" || ui.appState === "history";
  return /* @__PURE__ */ jsxs11(Box11, { flexDirection: "column", height: isFullScreen ? process.stdout.rows - 1 : void 0, children: [
    /* @__PURE__ */ jsx16(Box11, { flexGrow: 1, overflowY: "hidden", flexDirection: "column", children: /* @__PURE__ */ jsx16(MessageList, { history: chat.history, pending: chat.pending, staticRemountKey: chat.staticRemountKey }) }),
    ui.appState === "help" && /* @__PURE__ */ jsx16(HelpDialog, { onClose: () => ui.setAppState("idle") }),
    ui.appState === "approving" && chat.permReq && /* @__PURE__ */ jsx16(
      PermissionRequest,
      {
        toolName: chat.permReq.toolName,
        toolInput: chat.permReq.toolInput,
        reason: chat.permReq.reason,
        approvalMode: config.approvalMode,
        onCycleMode: config.cycleApprovalMode,
        onDecision: async (choice) => {
          const approved = choice === "allow_once" || choice === "always_allow";
          await chat.resolveApproval(approved);
          ui.setAppState("streaming");
        }
      }
    ),
    ui.appState === "selecting" && /* @__PURE__ */ jsx16(
      Selector,
      {
        title: ui.selectorTitle,
        items: ui.selectorItems,
        selectedIndex: ui.selectorIndex,
        onSelect: handleSelectorSelect,
        onCancel: () => ui.closeSelector(),
        onMove: ui.setSelectorIndex
      }
    ),
    ui.appState === "history" && /* @__PURE__ */ jsx16(
      MessageSelectorDialog,
      {
        messages: [...chat.history, ...chat.pending],
        onSelect: (text) => {
          ui.setAppState("idle");
          handleSubmit(text);
        },
        onCancel: () => ui.setAppState("idle")
      }
    ),
    ui.appState === "diffing" && /* @__PURE__ */ jsx16(DiffDialog, { diffs: ui.diffs, onClose: () => ui.setAppState("idle") }),
    /* @__PURE__ */ jsx16(
      StatusBar,
      {
        model: config.model,
        mode: ui.mode,
        isStreaming: chat.isStreaming,
        inputTokens: config.stats.inputTokens,
        outputTokens: config.stats.outputTokens,
        contextUsedPct: config.stats.maxTokens > 0 ? Math.round(config.stats.tokenCount / config.stats.maxTokens * 100) : 0,
        costUsd: config.stats.costUsd
      }
    ),
    /* @__PURE__ */ jsx16(
      InputArea,
      {
        onSubmit: handleSubmit,
        isStreaming: ui.appState !== "idle",
        onCancel: () => {
          chat.cancelStream();
          ui.setAppState("idle");
        }
      }
    ),
    /* @__PURE__ */ jsx16(Text13, { dimColor: true, children: "  ? for shortcuts  esc for vi mode" })
  ] });
};

// src/index.tsx
var args = process.argv.slice(2);
var serverAddr = "http://localhost:19996";
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--server" && args[i + 1]) {
    serverAddr = args[i + 1];
    i++;
  }
}
console.clear();
render(React13.createElement(App, { serverAddr }));
