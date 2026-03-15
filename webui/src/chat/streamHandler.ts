/**
 * SSE stream handler — processes /v1/chat SSE events into ChatMessageData.
 * Extracted from adapter.ts. Uses messageMapper for shared tool logic.
 */

import type { ToolCallData, StreamCallbacks } from './types'
import { API_BASE } from './api'
import { mapToolKind, normalizeToolArgs, getDisplayTitle, uid } from './messageMapper'
import { buildToolCallContent } from '../renderers/toolcalls/shared/utils'

/**
 * Start an SSE chat stream.
 * Parses backend delta events and emits ChatMessageData objects.
 */
export function chatStream(
  message: string,
  sessionId: string,
  cb: StreamCallbacks,
): { cancel: () => void } {
  const controller = new AbortController()
  let currentAssistantId: string | null = null
  let currentAssistantText = ''
  let currentThinkingId: string | null = null
  let currentThinkingText = ''
  // Track tool_start → tool_result correlation using name→id stack
  const pendingToolIds: Map<string, string[]> = new Map()

  ;(async () => {
    try {
      const res = await fetch(`${API_BASE}/v1/chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, session_id: sessionId }),
        signal: controller.signal,
      })

      if (!res.ok) {
        cb.onError(new Error(`HTTP ${res.status}`))
        return
      }

      const reader = res.body?.getReader()
      if (!reader) {
        cb.onError(new Error('No stream'))
        return
      }

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
          if (raw === '[DONE]') {
            cb.onEnd()
            return
          }

          let event: Record<string, unknown>
          try {
            event = JSON.parse(raw)
          } catch {
            continue
          }

          const type = event.type as string

          switch (type) {
            case 'text_delta': {
              const delta = (event.content as string) || ''
              if (!currentAssistantId) {
                currentAssistantId = uid()
                currentAssistantText = delta
                cb.onMessage({
                  uuid: currentAssistantId,
                  timestamp: new Date().toISOString(),
                  type: 'assistant',
                  message: { role: 'model', parts: [{ text: delta }] },
                })
              } else {
                currentAssistantText += delta
                cb.onUpdate(currentAssistantId, {
                  message: { role: 'model', parts: [{ text: currentAssistantText }] },
                })
              }
              break
            }

            case 'thinking': {
              const delta = (event.content as string) || ''
              if (!currentThinkingId) {
                currentThinkingId = uid()
                currentThinkingText = delta
                cb.onMessage({
                  uuid: currentThinkingId,
                  timestamp: new Date().toISOString(),
                  type: 'assistant',
                  message: { role: 'thinking', parts: [{ text: delta }] },
                })
              } else {
                currentThinkingText += delta
                cb.onUpdate(currentThinkingId, {
                  message: { role: 'thinking', parts: [{ text: currentThinkingText }] },
                })
              }
              break
            }

            case 'tool_start': {
              // Suppress task_boundary from rendering as tool card
              const toolName = (event.name as string) || 'unknown'
              if (toolName === 'task_boundary') break

              // Finalize current assistant/thinking
              currentAssistantId = null
              currentAssistantText = ''
              currentThinkingId = null
              currentThinkingText = ''

              const rawArgs = (event.args || {}) as Record<string, unknown>
              const normalizedInput = normalizeToolArgs(rawArgs, toolName)
              const displayTitle = getDisplayTitle(normalizedInput, toolName)

              const cardId = (event.call_id as string) || uid()
              const stack = pendingToolIds.get(toolName) || []
              stack.push(cardId)
              pendingToolIds.set(toolName, stack)

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

              console.log("[DEBUG SSE tool_result]", { 
                name: resultToolName, 
                hasOutput: !!event.output,
                outputLength: output?.length,
                rawEvent: event 
              })

              let targetId = (event.call_id as string) || ''
              if (!targetId) {
                const stack = pendingToolIds.get(resultToolName)
                if (stack && stack.length > 0) {
                  targetId = stack.shift()!
                  if (stack.length === 0) pendingToolIds.delete(resultToolName)
                }
              }

              if (targetId) {
                cb.onUpdate(targetId, {
                  toolCall: {
                    toolCallId: targetId,
                    kind: mapToolKind(resultToolName || 'unknown'),
                    title: '',
                    status: hasError ? 'failed' : 'completed',
                    content: buildToolCallContent(output, hasError ? (event.error as string) : null)
                  },
                })
              }
              currentAssistantText = ''
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
              cb.onError(new Error((event.error as string) || 'Unknown error'))
              break
            }

            case 'step_done': {
              currentAssistantId = null
              currentAssistantText = ''
              currentThinkingId = null
              currentThinkingText = ''
              cb.onStepDone?.()
              break
            }

            case 'plan_review': {
              const message = (event.message as string) || ''
              const paths = (event.paths as string[]) || []
              cb.onPlanReview?.(message, paths)
              break
            }

            case 'progress': {
              const taskName = (event.task_name as string) || ''
              const status = (event.status as string) || ''
              const summary = (event.summary as string) || ''
              const mode = (event.mode as string) || ''
              cb.onProgress?.(taskName, status, summary, mode)
              break
            }
          }
        }
      }
      cb.onEnd()
    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'AbortError') {
        cb.onEnd()
        return
      }
      cb.onError(err instanceof Error ? err : new Error(String(err)))
    }
  })()

  return { cancel: () => controller.abort() }
}
