/**
 * wsClient.ts — WebSocket client for NGOAgent session-level connections.
 *
 * Unlike SSE (per-request), a WS connection is bound to a session and survives
 * multiple chat turns. Supports async push events (subagent_progress, etc.)
 * that arrive after a chat run completes.
 *
 * Features:
 * - Auto-reconnect with exponential backoff (1s→2s→4s→...→30s)
 * - Send typed upstream messages (chat, stop, approve, ping)
 * - Receive downstream events via registered handler
 * - Ping-pong health check: dead connection detection within 10s
 * - Falls back gracefully (caller checks state before sending)
 */

import { getApiBase, getAuthToken } from './api'
import type { WSContentPart } from './types'

// ─── Types ───

export type WSState = 'connecting' | 'connected' | 'reconnecting' | 'closed'

export type WSUpstreamMsg =
  | { type: 'chat'; message: string; session_id: string; mode: string; content_parts?: WSContentPart[] }
  | { type: 'stop'; session_id: string }
  | { type: 'approve'; approval_id: string; approved: boolean }
  | { type: 'ping' }

export type WSMessageHandler = (event: Record<string, unknown>) => void

export interface WSClient {
  send(msg: WSUpstreamMsg): void
  addHandler(handler: WSMessageHandler): void
  removeHandler(handler: WSMessageHandler): void
  close(): void
  readonly state: WSState
  /** True if pong was received within the last 40s (healthy connection) */
  readonly isHealthy: boolean
}

// ─── Constants ───

const PING_INTERVAL_MS = 30_000      // Send ping every 30s
const PONG_TIMEOUT_MS = 10_000       // Expect pong within 10s
const HEALTH_WINDOW_MS = 40_000      // Connection is "healthy" if pong within 40s
const MAX_RECONNECT_DELAY_MS = 30_000

// ─── Implementation ───

export function createWSClient(
  onStateChange?: (state: WSState) => void,
): WSClient {
  let ws: WebSocket | null = null
  let state: WSState = 'connecting'
  let handlers: WSMessageHandler[] = []
  let reconnectAttempt = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let closed = false // explicit close requested
  let pingInterval: ReturnType<typeof setInterval> | null = null
  let pongTimeoutTimer: ReturnType<typeof setTimeout> | null = null
  let lastPongAt = Date.now() // Assume healthy on creation

  function setState(s: WSState) {
    state = s
    onStateChange?.(s)
  }

  function buildWSUrl(): string {
    const token = getAuthToken()
    const apiBase = getApiBase()

    // Determine WS URL from API base
    let wsBase: string
    if (apiBase && (apiBase.startsWith('http://') || apiBase.startsWith('https://'))) {
      // Explicit server URL (production/APK mode)
      wsBase = apiBase.replace(/^http/, 'ws')
    } else {
      // Relative URL (Vite dev proxy) — construct from window.location
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      wsBase = `${proto}//${window.location.host}`
    }

    return `${wsBase}/v1/ws?token=${encodeURIComponent(token)}`
  }

  function clearTimers() {
    if (pingInterval) { clearInterval(pingInterval); pingInterval = null }
    if (pongTimeoutTimer) { clearTimeout(pongTimeoutTimer); pongTimeoutTimer = null }
  }

  /** Force-close current WS and reconnect (dead connection detected) */
  function forceReconnect(reason: string) {
    console.warn(`[ws] Force reconnect: ${reason}`)
    clearTimers()
    if (ws) {
      try { ws.close(1000, reason) } catch { /* ignore */ }
      ws = null
    }
    if (!closed) {
      scheduleReconnect()
    }
  }

  function startPingPong() {
    clearTimers()
    // Send ping every 30s
    pingInterval = setInterval(() => {
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'ping' }))

      // Start pong timeout — if no pong within 10s, connection is dead
      if (pongTimeoutTimer) clearTimeout(pongTimeoutTimer)
      pongTimeoutTimer = setTimeout(() => {
        // Pong not received — connection is dead
        forceReconnect('pong timeout')
      }, PONG_TIMEOUT_MS)
    }, PING_INTERVAL_MS)
  }

  /** Called when a pong message is received */
  function onPong() {
    lastPongAt = Date.now()
    // Cancel the pong timeout — connection is alive
    if (pongTimeoutTimer) {
      clearTimeout(pongTimeoutTimer)
      pongTimeoutTimer = null
    }
  }

  function connect() {
    if (closed) return

    const url = buildWSUrl()
    setState(reconnectAttempt > 0 ? 'reconnecting' : 'connecting')

    try {
      ws = new WebSocket(url)
    } catch (err) {
      console.error('[ws] Failed to create WebSocket:', err)
      scheduleReconnect()
      return
    }

    ws.onopen = () => {
      reconnectAttempt = 0
      lastPongAt = Date.now() // Reset health on new connection
      setState('connected')
      startPingPong()
    }

    ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data) as Record<string, unknown>
        // Handle pong internally — don't forward to handlers
        if (event.type === 'pong') {
          onPong()
          return
        }
        for (const handler of handlers) {
          handler(event)
        }
      } catch {
        // Not JSON — ignore
      }
    }

    ws.onclose = () => {
      clearTimers()
      ws = null
      if (!closed) {
        scheduleReconnect()
      } else {
        setState('closed')
      }
    }

    ws.onerror = (err) => {
      console.error('[ws] WebSocket error:', err)
      // onclose will fire after onerror, which triggers reconnect
    }
  }

  function scheduleReconnect() {
    if (closed) return
    reconnectAttempt++
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempt - 1), MAX_RECONNECT_DELAY_MS)
    setState('reconnecting')
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      connect()
    }, delay)
  }

  // Start initial connection
  connect()

  return {
    get state() { return state },

    get isHealthy() {
      return state === 'connected' && (Date.now() - lastPongAt) < HEALTH_WINDOW_MS
    },

    send(msg: WSUpstreamMsg) {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg))
      } else {
        console.warn('[ws] Cannot send, state:', state, 'readyState:', ws?.readyState)
      }
    },

    addHandler(handler: WSMessageHandler) {
      handlers.push(handler)
    },

    removeHandler(handler: WSMessageHandler) {
      handlers = handlers.filter(h => h !== handler)
    },

    close() {
      closed = true
      clearTimers()
      if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
      ws?.close(1000, 'client close')
      ws = null
      setState('closed')
    },
  }
}
