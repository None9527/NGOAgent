/**
 * useHeightEstimator — Pretext-powered per-item height prediction
 *
 * Three-tier estimation strategy:
 * 1. Pretext prepare()+layout() — pure text messages (User, Thinking, plain Assistant)
 * 2. Empirical constants — ToolGroup panels (collapsed/expanded)
 * 3. Fallback 80px — unpredictable rich content
 *
 * After DOM renders, @tanstack/virtual's measureElement() overrides estimates
 * with precise values cached indefinitely.
 */

import { prepare, layout } from '@chenglou/pretext'
import { useCallback, useRef, useEffect } from 'react'
import type { RenderItem } from '../chat/groupMessages'
import type { ChatMessageData } from '../chat/types'
import { extractContent } from '../chat/messageUtils'

// CSS-derived constants for height calculation
const CHAT_FONT = '13px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
const LINE_HEIGHT = 22
const USER_MSG_PADDING = 48           // vertical padding + margins for user bubble
const ASSISTANT_MARKDOWN_OVERHEAD = 64 // markdown container padding + margins
const TOOL_GROUP_BASE = 56            // collapsed tool group header
const TOOL_GROUP_PER_ITEM = 28        // height increment per tool
const THINKING_COLLAPSED = 48         // thinking block collapsed state
const FALLBACK_HEIGHT = 80

// Track container width for responsive layout
const DEFAULT_COLUMN_WIDTH = 768      // max-w-4xl minus padding

// C3: delegate to shared extractContent utility
function extractText(msg: ChatMessageData): string {
  return extractContent(msg.message)
}

export function useHeightEstimator(containerWidth?: number) {
  const cacheRef = useRef<Map<string, number>>(new Map())
  const widthRef = useRef(containerWidth ?? DEFAULT_COLUMN_WIDTH)

  useEffect(() => {
    if (containerWidth && containerWidth > 0) {
      widthRef.current = containerWidth
    }
  }, [containerWidth])

  const estimateSize = useCallback((index: number, items: RenderItem[]): number => {
    const item = items[index]
    if (!item) return FALLBACK_HEIGHT

    // Tool group: static calculation
    if (item.type === 'tool_group') {
      return TOOL_GROUP_BASE + item.items.length * TOOL_GROUP_PER_ITEM
    }

    const msg = item.data
    // Cache key: uuid + content length (invalidates when streaming text grows)
    const contentLen = typeof msg.message?.content === 'string'
      ? msg.message.content.length
      : Array.isArray(msg.message?.parts) ? msg.message!.parts.length : 0
    const cacheKey = `${msg.uuid}:${contentLen}`
    const cached = cacheRef.current.get(cacheKey)
    if (cached) return cached

    // Thinking: collapsed state
    if (msg.message?.role === 'thinking') {
      cacheRef.current.set(cacheKey, THINKING_COLLAPSED)
      return THINKING_COLLAPSED
    }

    const text = extractText(msg)
    if (!text) return FALLBACK_HEIGHT

    try {
      const prepared = prepare(text, CHAT_FONT)
      const { height } = layout(prepared, widthRef.current, LINE_HEIGHT)

      let total: number
      if (msg.type === 'user') {
        total = height + USER_MSG_PADDING
      } else {
        // Assistant: Pretext estimate + markdown overhead
        // Code blocks/tables/images will be corrected by measureElement
        total = height + ASSISTANT_MARKDOWN_OVERHEAD
      }

      cacheRef.current.set(cacheKey, total)
      return total
    } catch {
      // Pretext failure fallback
      return FALLBACK_HEIGHT
    }
  }, [])

  // Clear cache on session switch
  const clearCache = useCallback(() => {
    cacheRef.current.clear()
  }, [])

  return { estimateSize, clearCache }
}
