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
 * - Falls back gracefully (caller checks state before sending)
 */

import { getApiBase, getAuthToken } from './api'

// ─── Types ───

export type WSState = 'connecting' | 'connected' | 'reconnecting' | 'closed'

export type WSUpstreamMsg =
  | { type: 'chat'; message: string; session_id: string; mode: string }
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
}

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
      setState('connected')
      // Start keepalive ping every 30s
      if (pingInterval) clearInterval(pingInterval)
      pingInterval = setInterval(() => {
        if (ws?.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }))
        }
      }, 30_000)
    }

    ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data) as Record<string, unknown>
        for (const handler of handlers) {
          handler(event)
        }
      } catch {
        // Not JSON — ignore
      }
    }

    ws.onclose = () => {
      if (pingInterval) { clearInterval(pingInterval); pingInterval = null }
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
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempt - 1), 30_000)
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

    send(msg: WSUpstreamMsg) {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg))
      } else {
        console.warn('[ws] Cannot send, state:', state)
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
      if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
      if (pingInterval) { clearInterval(pingInterval); pingInterval = null }
      ws?.close(1000, 'client close')
      ws = null
      setState('closed')
    },
  }
}
