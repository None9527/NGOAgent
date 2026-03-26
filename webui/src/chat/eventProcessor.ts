/**
 * eventProcessor.ts — Pure functions for processing chat stream events.
 *
 * ZERO side effects — all state mutation happens through return values / callbacks.
 * This makes the event logic fully unit-testable without any transport setup.
 *
 * Extracted from streamHandler.ts (was processEvent() + StreamState, lines 17-250).
 */

import type { ToolCallData, StreamCallbacks } from './types'
import { mapToolKind, normalizeToolArgs, getDisplayTitle, uid } from './messageMapper'
import { buildToolCallContent } from '../renderers/toolcalls/shared/utils'

// ─── State ────────────────────────────────────────────────────

export interface StreamState {
  currentAssistantId: string | null
  currentAssistantText: string
  currentThinkingId: string | null
  currentThinkingText: string
  pendingToolIds: Map<string, string[]>
}

export function createStreamState(): StreamState {
  return {
    currentAssistantId: null,
    currentAssistantText: '',
    currentThinkingId: null,
    currentThinkingText: '',
    pendingToolIds: new Map(),
  }
}

// ─── Event Processor ─────────────────────────────────────────

/**
 * Process a single parsed backend event.
 *
 * @param event   - Parsed event object from SSE/WS
 * @param state   - Mutable stream state (mutated in place)
 * @param cb      - Stream callbacks to notify consumers
 * @param aborted - Whether the stream was aborted (suppresses error cb)
 * @returns true if the stream should end after this event
 */
export function processEvent(
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

      // task_boundary → reset assistant state only; section injection happens via progress event
      if (toolName === 'task_boundary') {
        state.currentAssistantId = null
        state.currentAssistantText = ''
        state.currentThinkingId = null
        state.currentThinkingText = ''
        break
      }

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
      cb.onEnd()
      return false  // WS handler stays alive for auto-wake events
    }

    case 'auto_wake_start': {
      cb.onAutoWakeStart?.()
      break
    }

    case 'auto_wake_done': {
      cb.onEnd()
      return false
    }

    case 'pong':
      break

    // Unknown events are silently ignored — forward compatibility
  }

  return false
}

// ─── SSE line parser ─────────────────────────────────────────

/**
 * Parse a single SSE `data:` line into an event object.
 * Returns null for keep-alive, [DONE], or parse errors.
 */
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line || line === '[DONE]') return null

  try {
    return JSON.parse(line) as Record<string, unknown>
  } catch {
    return null
  }
}
