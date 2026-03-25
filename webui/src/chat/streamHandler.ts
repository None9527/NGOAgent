/**
 * streamHandler.ts — Thin orchestration facade (backward-compatible API).
 *
 * Phase 7 refactor: event processing and transport are now separate modules:
 * - eventProcessor.ts — pure functions, fully unit-testable
 * - transport.ts      — SSE/WS network I/O
 *
 * This file:
 * 1. Re-exports the full public API so existing callers (StreamProvider,
 *    ConnectionProvider, etc.) continue to work with zero changes.
 * 2. chatStream() — orchestration: WS if available, SSE fallback.
 * 3. checkActiveRun() — stays here as it is a simple HTTP helper.
 *
 * Future (Phase 8): Remove StreamProvider and inline chatStream() into
 * ConnectionProvider. streamHandler.ts can then be deleted.
 */

import type { StreamCallbacks } from './types'
import { getApiBase, getAuthToken } from './api'
import {
  chatStreamSSE,
  chatStreamWS,
  getSharedWSClient,
  onWSStateChange as _onWSStateChange,
} from './transport'

// ─── Re-exports (keep callers from changing) ─────────────────

export { getSharedWSClient, closeSharedWSClient } from './transport'
export { onWSStateChange } from './transport'
export { reconnectStream } from './transport'
export { createStreamState, processEvent } from './eventProcessor'

// ─── Orchestration ───────────────────────────────────────────

/**
 * Start a chat stream.
 * Auto-routes to WS if the shared client is connected, falls back to SSE.
 */
export function chatStream(
  message: string,
  sessionId: string,
  mode: string,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const ws = getSharedWSClient()
  if (ws && ws.state === 'connected') {
    return chatStreamWS(ws, message, sessionId, mode, cb)
  }
  return chatStreamSSE(message, sessionId, mode, cb)
}

// ─── Status check ─────────────────────────────────────────────

/**
 * Check if a session has an active (in-progress) run on the backend.
 */
export async function checkActiveRun(
  sessionId: string,
): Promise<{ active: boolean; done: boolean; lastSeq: number }> {
  try {
    const headers: Record<string, string> = {}
    const token = getAuthToken()
    if (token) headers['Authorization'] = `Bearer ${token}`

    const res = await fetch(
      `${getApiBase()}/v1/chat/status?session_id=${encodeURIComponent(sessionId)}`,
      { headers },
    )
    if (!res.ok) return { active: false, done: false, lastSeq: 0 }
    const data = await res.json()
    return { active: !!data.active, done: !!data.done, lastSeq: data.last_seq || 0 }
  } catch {
    return { active: false, done: false, lastSeq: 0 }
  }
}
