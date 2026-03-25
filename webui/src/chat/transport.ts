/**
 * transport.ts — SSE & WebSocket transport layer.
 *
 * Handles network I/O only. All event processing is delegated to eventProcessor.ts.
 *
 * Exports:
 * - chatStreamSSE()        — POST /v1/chat → ReadableStream → processEvent
 * - chatStreamWS()         — WebSocket send → handler → processEvent
 * - getSharedWSClient()    — WS singleton management
 * - closeSharedWSClient()  — WS cleanup
 * - onWSStateChange()      — WS state subscription
 * - reconnectStream()      — GET /v1/chat/reconnect
 */

import type { StreamCallbacks } from './types'
import { getApiBase, getAuthToken } from './api'
import { createWSClient, type WSClient, type WSMessageHandler } from './wsClient'
import {
  createStreamState,
  processEvent,
  parseSSELine,
} from './eventProcessor'

// ─── SSE Read Loop ────────────────────────────────────────────

async function readSSEStream(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  state: ReturnType<typeof createStreamState>,
  cb: StreamCallbacks,
  abortedRef: { value: boolean },
): Promise<void> {
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''

    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      const raw = line.slice(6).trim()

      // [DONE] sentinel
      if (raw === '[DONE]') {
        cb.onEnd()
        return
      }

      const event = parseSSELine(raw)
      if (event && processEvent(event, state, cb, abortedRef.value)) return
    }
  }
  cb.onEnd()
}

// ─── SSE Streaming ───────────────────────────────────────────

/**
 * Chat via SSE — POST /v1/chat and stream response.
 * Fallback when WS is unavailable or not connected.
 */
export function chatStreamSSE(
  message: string,
  sessionId: string,
  mode: string,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const controller = new AbortController()
  const abortedRef = { value: false }
  const state = createStreamState()

  ;(async () => {
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      const token = getAuthToken()
      if (token) headers['Authorization'] = `Bearer ${token}`

      const res = await fetch(`${getApiBase()}/v1/chat`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ message, session_id: sessionId, mode }),
        signal: controller.signal,
      })

      if (!res.ok) {
        if (abortedRef.value) { cb.onEnd(); return }
        cb.onError(new Error(`HTTP ${res.status}`))
        return
      }

      const reader = res.body?.getReader()
      if (!reader) {
        if (abortedRef.value) { cb.onEnd(); return }
        cb.onError(new Error('No stream body'))
        return
      }

      await readSSEStream(reader, state, cb, abortedRef)
    } catch (err: unknown) {
      if (abortedRef.value || (err instanceof Error && err.name === 'AbortError')) {
        cb.onEnd()
        return
      }
      cb.onError(err instanceof Error ? err : new Error(String(err)))
    }
  })()

  return {
    cancel: () => {
      abortedRef.value = true
      controller.abort()
    },
  }
}

// ─── WebSocket Singleton ──────────────────────────────────────

let _sharedWS: WSClient | null = null
let _wsStateListeners: ((state: string) => void)[] = []
let _activeWSHandler: WSMessageHandler | null = null

/** Get or create the shared WS client. Returns null if no auth token. */
export function getSharedWSClient(): WSClient | null {
  if (_sharedWS && _sharedWS.state !== 'closed') return _sharedWS

  const token = getAuthToken()
  if (!token) return null

  _sharedWS = createWSClient((state) => {
    _wsStateListeners.forEach(fn => fn(state))
  })
  return _sharedWS
}

/** Explicitly close the shared WS client */
export function closeSharedWSClient(): void {
  _sharedWS?.close()
  _sharedWS = null
}

/** Subscribe to WS state changes. Returns unsubscribe function. */
export function onWSStateChange(fn: (state: string) => void): () => void {
  _wsStateListeners.push(fn)
  return () => { _wsStateListeners = _wsStateListeners.filter(f => f !== fn) }
}

// ─── WebSocket Streaming ──────────────────────────────────────

/**
 * Chat via WebSocket. Returns cancel function.
 * The handler stays alive after 'done' to receive auto-wake events.
 */
export function chatStreamWS(
  ws: WSClient,
  message: string,
  sessionId: string,
  mode: string,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const state = createStreamState()
  let cancelled = false

  const handler: WSMessageHandler = (event) => {
    if (cancelled) return
    processEvent(event, state, cb, false)
    // Never remove based on return: 'done' resets UI but handler stays for auto-wake
  }

  if (_activeWSHandler) {
    ws.removeHandler(_activeWSHandler)
  }
  _activeWSHandler = handler

  ws.addHandler(handler)
  ws.send({ type: 'chat', message, session_id: sessionId, mode })

  return {
    cancel: () => {
      cancelled = true
      ws.removeHandler(handler)
      _activeWSHandler = null
      ws.send({ type: 'stop', session_id: sessionId })
      cb.onEnd()
    },
  }
}

// ─── Reconnect (SSE) ─────────────────────────────────────────

/**
 * Reconnect to an active SSE stream via GET /v1/chat/reconnect.
 * Used after a page reload or network hiccup while a run was in progress.
 */
export function reconnectStream(
  sessionId: string,
  lastSeqId: number,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const controller = new AbortController()
  const abortedRef = { value: false }
  const state = createStreamState()

  ;(async () => {
    try {
      const headers: Record<string, string> = {}
      const token = getAuthToken()
      if (token) headers['Authorization'] = `Bearer ${token}`

      const url = `${getApiBase()}/v1/chat/reconnect?session_id=${encodeURIComponent(sessionId)}&last_seq=${lastSeqId}`
      const res = await fetch(url, { headers, signal: controller.signal })

      if (!res.ok) {
        if (res.status === 404 || abortedRef.value) { cb.onEnd(); return }
        cb.onError(new Error(`HTTP ${res.status}`))
        return
      }

      const reader = res.body?.getReader()
      if (!reader) {
        if (abortedRef.value) { cb.onEnd(); return }
        cb.onError(new Error('No stream'))
        return
      }

      await readSSEStream(reader, state, cb, abortedRef)
    } catch (err: unknown) {
      if (abortedRef.value || (err instanceof Error && err.name === 'AbortError')) {
        cb.onEnd()
        return
      }
      cb.onError(err instanceof Error ? err : new Error(String(err)))
    }
  })()

  return { cancel: () => { abortedRef.value = true; controller.abort() } }
}
