/**
 * Unified message mapping — single source of truth for:
 * - Tool name → renderer kind mapping
 * - Tool argument normalization
 * - Display title generation
 * - History → ChatMessageData conversion
 *
 * Used by BOTH chatStream (SSE) and loadHistory (REST) paths
 * to ensure consistent rendering.
 */

import type { ChatMessageData, HistoryMessage, ToolCallData } from './types'
import { buildToolCallContent } from '../renderers/toolcalls/shared/utils'

// ═══════════════════════════════════════════
// Tool Kind Mapping
// ═══════════════════════════════════════════

/**
 * Map NGOAgent tool names → renderer kind values.
 * Covers all 20+ registered tools.
 *
 * Renderer kinds (from ChatViewer.getToolCallComponent):
 *   read, write, edit, execute, search, think, save_memory,
 *   web_fetch, updated_plan, (default → GenericToolCall)
 */
export function mapToolKind(name: string): string {
  const lower = name.toLowerCase()

  // File operations
  if (lower === 'read_file' || lower === 'view_file' || lower === 'list_dir') return 'read'
  if (lower === 'write_file' || lower === 'create_file' || lower === 'write_to_file' || lower === 'create') return 'write'
  if (lower === 'edit_file' || lower === 'str_replace' || lower === 'replace_file_content' || lower === 'multi_replace_file_content') return 'edit'
  if (lower === 'brain_artifact') return 'write'

  // Shell & execution
  if (lower === 'run_command' || lower === 'bash' || lower === 'shell') return 'execute'
  if (lower === 'command_status') return 'execute'
  if (lower === 'spawn_agent') return 'spawn_agent'
  if (lower === 'forge') return 'execute'

  // Search
  if (lower === 'grep_search' || lower === 'find_by_name' || lower === 'glob') return 'search'
  if (lower.includes('search') && !lower.includes('web')) return 'search'

  // Web
  if (lower === 'web_search') return 'web_fetch'
  if (lower === 'web_fetch' || lower === 'fetch_url') return 'web_fetch'

  // Planning & tasks
  if (lower === 'task_plan' || lower === 'task_list' || lower === 'task_boundary') return 'updated_plan'

  // Knowledge & memory
  if (lower === 'save_knowledge' || lower === 'save_memory') return 'save_memory'

  // Thinking
  if (lower === 'think' || lower === 'thinking') return 'think'

  // Communication
  if (lower === 'notify_user' || lower === 'send_message') return 'notify'

  // Configuration
  if (lower === 'update_project_context') return 'edit'
  if (lower === 'manage_cron') return 'cron'

  // Fallback — generic tool card
  return 'tool'
}

// ═══════════════════════════════════════════
// Argument Normalization
// ═══════════════════════════════════════════

/**
 * Normalize NGOAgent tool argument names → standard field names.
 * Ensures renderers can find standard fields regardless of backend naming.
 */
export function normalizeToolArgs(
  rawArgs: Record<string, unknown>,
  toolName?: string,
): Record<string, unknown> {
  const out: Record<string, unknown> = { ...rawArgs }

  // Path fields
  if (rawArgs.AbsolutePath) out.path = rawArgs.AbsolutePath
  if (rawArgs.TargetFile) out.path = rawArgs.TargetFile
  if (rawArgs.SearchPath) out.searchPath = rawArgs.SearchPath
  if (rawArgs.file) out.path = rawArgs.file
  if (rawArgs.File) out.path = rawArgs.File
  if (rawArgs.file_path) out.path = rawArgs.file_path
  if (rawArgs.filename) out.path = rawArgs.filename

  // Command fields
  if (rawArgs.CommandLine) out.command = rawArgs.CommandLine
  if (rawArgs.CommandId) out.commandId = rawArgs.CommandId

  // Query/pattern fields
  if (rawArgs.Query) out.query = rawArgs.Query
  if (rawArgs.query) out.query = rawArgs.query
  if (rawArgs.pattern) out.query = rawArgs.pattern
  if (rawArgs.Pattern) out.query = rawArgs.Pattern

  // URL fields
  if (rawArgs.Url) out.url = rawArgs.Url
  if (rawArgs.url) out.url = rawArgs.url

  // Generic Name fields (for resource creation)
  if (rawArgs.Name) out.name = rawArgs.Name
  if (rawArgs.name) out.name = rawArgs.name

  // Knowledge / Context / Planning fields
  if (rawArgs.key) out.key = rawArgs.key
  if (rawArgs.topic) out.topic = rawArgs.topic
  if (rawArgs.section) out.section = rawArgs.section
  if (rawArgs.type) out.type = rawArgs.type
  if (rawArgs.action) out.action = rawArgs.action
  if (rawArgs.summary) out.summary = rawArgs.summary

  // Preserve original tool name for specialized renderers
  if (toolName) out._toolName = toolName

  return out
}

// ═══════════════════════════════════════════
// Display Title
// ═══════════════════════════════════════════

/**
 * Pick the most meaningful field as display title.
 * Unified logic — replaces both adapter.ts getDisplayTitle and App.tsx inline code.
 */
export function getDisplayTitle(
  args: Record<string, unknown>,
  toolName: string,
): string {
  // Shell: show command
  if (args.command) return args.command as string
  // Search/Grep: show query
  if (args.query) return args.query as string
  // File ops: show path
  if (args.path) return args.path as string
  // Web: show url
  if (args.url) return args.url as string
  // Command status: show ID
  if (args.commandId) return `command ${args.commandId}`
  // Notification: show snippet
  if (args.message) return (args.message as string).slice(0, 60)
  // Knowledge / Project context / Planning entities — compound checks FIRST
  if (args.key) return `${args.action || toolName} knowledge: ${args.key}`
  if (args.topic) return `${args.action || toolName} topic: ${args.topic}`
  if (args.section) return `${args.action || toolName} section: ${args.section}`
  // Actions with typed subject (e.g. task_plan action=create type=plan summary=xxx)
  if (args.action && args.type) return `${args.action} ${args.type}${args.summary ? ': ' + (args.summary as string).slice(0, 60) : ''}`
  // Named resource (e.g. manage_cron action=create name=backup)
  if (args.name) return `${args.action || toolName} ${args.name}`
  // Bare action only (last resort before generic fallback)
  if (args.action) return `${args.action}`
  // Fallback to just the tool name
  return toolName
}

// ═══════════════════════════════════════════
// History → ChatMessageData Converter
// ═══════════════════════════════════════════

// C4: crypto.randomUUID for true uniqueness (no counter reset on HMR)
const uid = () =>
  typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? `msg-${crypto.randomUUID()}`
    : `msg-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`

/**
 * Convert backend history messages to ChatMessageData array.
 * Replaces the inline mapping logic in App.tsx loadHistory().
 *
 * Uses the same mapToolKind/normalizeToolArgs/getDisplayTitle as SSE path,
 * ensuring consistent rendering between history replay and live streaming.
 */
export function historyToMessages(
  msgs: HistoryMessage[],
  sessionId: string,
): ChatMessageData[] {
  const result: ChatMessageData[] = []
  let seq = 0

  for (let i = 0; i < msgs.length; i++) {
    const m = msgs[i]
    const ts = new Date().toISOString()
    const content = m.content || ''

    // User message
    if (m.role === 'user') {
      result.push({
        uuid: `hist-${sessionId}-${seq++}`,
        timestamp: ts,
        type: 'user' as const,
        message: { role: 'user', parts: [{ text: content }] },
      })
      continue
    }

    // Tool message → tool card
    if (m.role === 'tool') {
      const toolName = m.tool_name || 'unknown'

      // task_boundary → inject as section delimiter for Progress Updates grouping
      if (toolName === 'task_boundary') {
        let parsedArgs: Record<string, unknown> = {}
        if (m.tool_args) {
          try { parsedArgs = JSON.parse(m.tool_args) } catch { /* ignore */ }
        }
        result.push({
          uuid: uid(),
          timestamp: new Date().toISOString(),
          type: 'task_section',
          taskSection: {
            taskName: (parsedArgs.task_name as string) || '',
            status: (parsedArgs.status as string) || '',
            summary: (parsedArgs.summary as string) || '',
            mode: (parsedArgs.mode as string) || '',
          },
        })
        continue
      }

      const kind = mapToolKind(toolName)

      // Parse args
      let parsedArgs: Record<string, unknown> = {}
      if (m.tool_args) {
        try { parsedArgs = JSON.parse(m.tool_args) } catch { /* ignore */ }
      }

      const normalizedInput = normalizeToolArgs(parsedArgs, toolName)
      const displayTitle = getDisplayTitle(normalizedInput, toolName)

      const toolCall: ToolCallData = {
        toolCallId: `hist-${sessionId}-${seq}`,
        kind,
        title: displayTitle,
        status: 'completed',
        rawInput: Object.keys(normalizedInput).length > 0 ? normalizedInput : undefined,
        content: buildToolCallContent(content)
      }

      result.push({
        uuid: `hist-${sessionId}-${seq++}`,
        timestamp: ts,
        type: 'tool_call' as const,
        toolCall,
      })
      continue
    }

    // Assistant message
    if (m.role === 'assistant') {
      // Emit thinking block first (if reasoning was persisted)
      if (m.reasoning) {
        result.push({
          uuid: `hist-${sessionId}-${seq++}`,
          timestamp: ts,
          type: 'assistant' as const,
          message: { role: 'thinking', parts: [{ text: m.reasoning }] },
        })
      }
      // Emit text content (skip empty tool_call carrier messages)
      if (content.trim()) {
        result.push({
          uuid: `hist-${sessionId}-${seq++}`,
          timestamp: ts,
          type: 'assistant' as const,
          message: { role: 'model', parts: [{ text: content }] },
        })
      }
      continue
    }
  }

  return result
}

// Re-export uid for streamHandler
export { uid }
