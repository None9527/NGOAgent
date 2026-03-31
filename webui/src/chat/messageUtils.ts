/**
 * messageUtils — Shared message content extraction.
 *
 * Phase 4/C3: Deduplicated from ChatViewer.extractContent and useHeightEstimator.extractText.
 * Single source of truth for extracting text from ChatMessageData.message.
 */

import type { ChatMessageData } from './types'

/**
 * Extract text content from a message's parts/content structure.
 * Handles all known formats: parts array, string content, Claude content array.
 */
export function extractContent(message: ChatMessageData['message']): string {
  if (!message) return ''

  if (message.parts && Array.isArray(message.parts)) {
    return message.parts.map((part) => part.text || '').join('')
  }

  if (typeof message.content === 'string') {
    return message.content
  }

  if (Array.isArray(message.content)) {
    return message.content
      .filter((item) => item.type === 'text' && item.text)
      .map((item) => item.text || '')
      .join('')
  }

  return ''
}
