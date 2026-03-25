/**
 * Stream handler — processes chat events into ChatMessageData.
 * Supports both SSE (POST /v1/chat) and WebSocket (/v1/ws) transports.
 * Both paths share processEvent() for event dispatch.
 */

import type { ToolCallData, StreamCallbacks } from './types'
import { getApiBase, getAuthToken } from './api'
import { mapToolKind, normalizeToolArgs, getDisplayTitle, uid } from './messageMapper'
import { buildToolCallContent } from '../renderers/toolcalls/shared/utils'
import { createWSClient, type WSClient, type WSMessageHandler } from './wsClient'

// ═══════════════════════════════════════════
// Shared State + Event Processor
// ═══════════════════════════════════════════

interface StreamState {
  currentAssistantId: string | null
  currentAssistantText: string
  currentThinkingId: string | null
  currentThinkingText: string
  pendingToolIds: Map<string, string[]>
}

function createStreamState(): StreamState {
  return {
    currentAssistantId: null,
    currentAssistantText: '',
    currentThinkingId: null,
    currentThinkingText: '',
    pendingToolIds: new Map(),
  }
}

/**
 * Process a parsed event object. Returns true if the stream should end.
 * Shared between SSE and WebSocket paths — single source of truth.
 */
function processEvent(
  event: Record<string, unknown>,
  state: StreamState,
  cb: StreamCallbacks,
  aborted: boolean,
): boolean {
  const type = event.type as string

  switch (type) {
    case 'text_delta': {
      const delta = (event.content as string) || ''
      if (!state.currentAssistantId) {
        state.currentAssistantId = uid()
        state.currentAssistantText = delta
        cb.onMessage({
          uuid: state.currentAssistantId,
          timestamp: new Date().toISOString(),
          type: 'assistant',
          message: { role: 'model', parts: [{ text: delta }] },
        })
      } else {
        state.currentAssistantText += delta
        cb.onUpdate(state.currentAssistantId, {
          message: { role: 'model', parts: [{ text: state.currentAssistantText }] },
        })
      }
      break
    }

    case 'thinking': {
      const delta = (event.content as string) || ''
      if (!state.currentThinkingId) {
        state.currentThinkingId = uid()
        state.currentThinkingText = delta
        cb.onMessage({
          uuid: state.currentThinkingId,
          timestamp: new Date().toISOString(),
          type: 'assistant',
          message: { role: 'thinking', parts: [{ text: delta }] },
        })
      } else {
        state.currentThinkingText += delta
        cb.onUpdate(state.currentThinkingId, {
          message: { role: 'thinking', parts: [{ text: state.currentThinkingText }] },
        })
      }
      break
    }

    case 'tool_start': {
      const toolName = (event.name as string) || 'unknown'
      if (toolName === 'task_boundary') break

      state.currentAssistantId = null
      state.currentAssistantText = ''
      state.currentThinkingId = null
      state.currentThinkingText = ''

      const rawArgs = (event.args || {}) as Record<string, unknown>
      const normalizedInput = normalizeToolArgs(rawArgs, toolName)
      const displayTitle = getDisplayTitle(normalizedInput, toolName)

      const cardId = (event.call_id as string) || uid()
      const stack = state.pendingToolIds.get(toolName) || []
      stack.push(cardId)
      state.pendingToolIds.set(toolName, stack)

      const toolData: ToolCallData = {
        toolCallId: cardId,
        kind: mapToolKind(toolName),
        title: displayTitle,
        status: 'in_progress',
        rawInput: normalizedInput,
        content: [],
      }
      cb.onToolCall({
        uuid: cardId,
        timestamp: new Date().toISOString(),
        type: 'tool_call',
        toolCall: toolData,
      })
      break
    }

    case 'tool_result': {
      const resultToolName = (event.name as string) || ''
      if (resultToolName === 'task_boundary') break

      const output = (event.output as string) || ''
      const hasError = !!(event.error as string)

      let targetId = (event.call_id as string) || ''
      if (!targetId) {
        const stack = state.pendingToolIds.get(resultToolName)
        if (stack && stack.length > 0) {
          targetId = stack.shift()!
          if (stack.length === 0) state.pendingToolIds.delete(resultToolName)
        }
      }

      if (targetId) {
        cb.onUpdate(targetId, {
          toolCall: {
            toolCallId: targetId,
            kind: mapToolKind(resultToolName || 'unknown'),
            title: '',
            status: hasError ? 'failed' : 'completed',
            content: buildToolCallContent(output, hasError ? (event.error as string) : null),
          },
        })
      }
      state.currentAssistantText = ''
      break
    }

    case 'approval_request': {
      cb.onApproval({
        approvalId: (event.approval_id as string) || '',
        toolName: (event.tool_name as string) || '',
        args: (event.args as Record<string, unknown>) || {},
        reason: (event.reason as string) || '',
      })
      break
    }

    case 'error': {
      if (aborted) { cb.onEnd(); return true }
      cb.onError(new Error((event.message as string) || (event.error as string) || 'Unknown error'))
      return true
    }

    case 'title_updated': {
      const sessionId = (event.session_id as string) || ''
      const title = (event.title as string) || ''
      if (sessionId && title) cb.onTitleUpdate?.(sessionId, title)
      break
    }

    case 'step_done': {
      state.currentAssistantId = null
      state.currentAssistantText = ''
      state.currentThinkingId = null
      state.currentThinkingText = ''
      cb.onStepDone?.()
      break
    }

    case 'plan_review': {
      cb.onPlanReview?.((event.message as string) || '', (event.paths as string[]) || [])
      break
    }

    case 'progress': {
      cb.onProgress?.(
        (event.task_name as string) || '',
        (event.status as string) || '',
        (event.summary as string) || '',
        (event.mode as string) || '',
      )
      break
    }

    case 'subagent_progress': {
      cb.onSubagentProgress?.(
        (event.run_id as string) || '',
        (event.task_name as string) || '',
        (event.status as string) || '',
        (event.done as number) || 0,
        (event.total as number) || 0,
        (event.error as string) || '',
        (event.output as string) || '',
        (event.current_step as string) || '',
      )
      break
    }

    case 'slash_result': {
      const result = (event.result as string) || ''
      cb.onMessage({
        uuid: uid(),
        timestamp: new Date().toISOString(),
        type: 'assistant',
        message: { role: 'model', parts: [{ text: result }] },
      })
      cb.onEnd()
      return true
    }

    case 'done': {
      // WS equivalent of SSE [DONE] — chat turn completed.
      // Returns false so the handler stays alive for auto-wake events.
      cb.onEnd()
      return false
    }

    case 'auto_wake_start': {
      // Subagent results arrived, parent auto-continuing
      cb.onAutoWakeStart?.()
      break
    }

    case 'auto_wake_done': {
      // Auto-wake turn completed — same as done for frontend
      cb.onEnd()
      return false
    }

    case 'pong':
      break
  }

  return false
}

/**
 * SSE wrapper: parses raw SSE line, delegates to processEvent.
 */
function processSSELine(
  raw: string,
  state: StreamState,
  cb: StreamCallbacks,
  aborted: boolean,
): boolean {
  if (raw === '[DONE]') {
    cb.onEnd()
    return true
  }

  let event: Record<string, unknown>
  try {
    event = JSON.parse(raw)
  } catch {
    return false
  }

  return processEvent(event, state, cb, aborted)
}

// ═══════════════════════════════════════════
// Shared SSE Read Loop
// ═══════════════════════════════════════════

async function readSSEStream(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  state: StreamState,
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
      if (processSSELine(raw, state, cb, abortedRef.value)) return
    }
  }
  cb.onEnd()
}

// ═══════════════════════════════════════════
// Shared WebSocket Client (singleton per app)
// ═══════════════════════════════════════════

let _sharedWS: WSClient | null = null
let _wsStateListeners: ((state: string) => void)[] = []

/** Get or create the shared WS client */
export function getSharedWSClient(): WSClient | null {
  if (_sharedWS && _sharedWS.state !== 'closed') return _sharedWS

  const token = getAuthToken()
  if (!token) return null

  _sharedWS = createWSClient((state) => {
    _wsStateListeners.forEach(fn => fn(state))
  })
  return _sharedWS
}

/** Close the shared WS client */
export function closeSharedWSClient() {
  _sharedWS?.close()
  _sharedWS = null
}

/** Subscribe to WS state changes */
export function onWSStateChange(fn: (state: string) => void): () => void {
  _wsStateListeners.push(fn)
  return () => { _wsStateListeners = _wsStateListeners.filter(f => f !== fn) }
}

// ═══════════════════════════════════════════
// Public API
// ═══════════════════════════════════════════

/**
 * Start a chat stream — auto-routes to WS if available, falls back to SSE.
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

/**
 * Chat via WebSocket — sends upstream, receives events through handler.
 */
function chatStreamWS(
  ws: WSClient,
  message: string,
  sessionId: string,
  mode: string,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const state = createStreamState()
  let cancelled = false

  // Handler stays alive after 'done' to receive auto-wake events.
  // Only removed on explicit cancel or when next chatStreamWS replaces it.
  const handler: WSMessageHandler = (event) => {
    if (cancelled) return
    processEvent(event, state, cb, false)
    // Never remove handler based on processEvent return.
    // 'done' resets UI but handler stays for auto-wake.
  }

  // Remove any previous handler from last chat turn
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

// Track the active WS handler so it can be replaced on next chat
let _activeWSHandler: WSMessageHandler | null = null

/**
 * Chat via SSE — original POST /v1/chat path (fallback).
 */
function chatStreamSSE(
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

/**
 * Check if a session has an active (in-progress) run on the backend.
 */
export async function checkActiveRun(sessionId: string): Promise<{ active: boolean; done: boolean; lastSeq: number }> {
  try {
    const headers: Record<string, string> = {}
    const token = getAuthToken()
    if (token) headers['Authorization'] = `Bearer ${token}`

    const res = await fetch(`${getApiBase()}/v1/chat/status?session_id=${encodeURIComponent(sessionId)}`, { headers })
    if (!res.ok) return { active: false, done: false, lastSeq: 0 }
    const data = await res.json()
    return { active: !!data.active, done: !!data.done, lastSeq: data.last_seq || 0 }
  } catch {
    return { active: false, done: false, lastSeq: 0 }
  }
}

/**
 * Reconnect to an active SSE stream (GET /v1/chat/reconnect).
 * In WS mode, reconnection is handled automatically by wsClient.
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
