/**
 * Message grouping utility — merges consecutive tool_call messages
 * into collapsible ToolGroup items, split by task_section boundaries.
 *
 * Input:  [user, assistant, task_section(A), assistant, tool_call, tool_call, task_section(B), tool_call, assistant]
 * Output: [message(user), message(assistant), message(assistant), tool_group("A", ×2), tool_group("B", ×1), message(assistant)]
 *
 * task_section messages are consumed by the grouper (not rendered independently).
 * Each tool_group inherits the task name + mode from the most recent preceding task_section.
 */

import type { ChatMessageData, TaskSectionMeta } from './types'

/**
 * Union type for virtual list render items.
 * - `message`: a single chat message (user/assistant/thinking/system)
 * - `tool_group`: a collapsed group of tool calls within a task section
 */
export type RenderItem =
  | { type: 'message'; data: ChatMessageData }
  | { type: 'tool_group'; id: string; items: ChatMessageData[]; section?: TaskSectionMeta }

/**
 * Groups consecutive tool_call messages into ToolGroup render items,
 * splitting at task_section boundaries so each group has a named header.
 */
export function groupMessages(messages: ChatMessageData[]): RenderItem[] {
  const result: RenderItem[] = []
  let currentGroup: ChatMessageData[] = []
  let currentSection: TaskSectionMeta | undefined

  const flushGroup = () => {
    if (currentGroup.length === 0) return
    result.push({
      type: 'tool_group',
      id: `tg-${currentGroup[0].uuid}`,
      items: [...currentGroup],
      section: currentSection,
    })
    currentGroup = []
    // Reset section AFTER flushing — section only applies to the immediately following tool group
    currentSection = undefined
  }

  for (const msg of messages) {
    if (msg.type === 'task_section' && msg.taskSection) {
      // Flush any pending group under the previous section
      flushGroup()
      // Start a new section context — persists across text messages
      // until the next flushGroup (i.e., until tool_group is emitted)
      currentSection = msg.taskSection
      // task_section is consumed — not rendered as a standalone message
      continue
    }

    if (msg.type === 'tool_call' && msg.toolCall) {
      currentGroup.push(msg)
    } else {
      flushGroup()
      // NOTE: do NOT reset currentSection here — text messages between
      // task_section and tool calls should not break the section link.
      // Section is only reset by flushGroup() above.
      result.push({ type: 'message', data: msg })
    }
  }
  flushGroup()

  return result
}
