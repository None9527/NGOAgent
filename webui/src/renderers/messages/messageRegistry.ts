/**
 * messageRegistry — Registry-based message type renderer dispatch.
 *
 * Replaces the message-type if/else chain in ChatViewer.tsx with
 * a pluggable registration system.
 *
 * Usage:
 *   // Register (in renderers/messages/index.ts):
 *   registerMessage('user', { component: UserMessage })
 *   registerMessage('assistant', { component: AssistantMessage })
 *
 *   // Resolve (in ChatViewer):
 *   const config = getMessageRenderer(msg.type)
 */

import type { ComponentType } from 'react'
import type { ChatMessageData } from '../../chat/types'

// ─── Registry Types ──────────────────────────────────────────

export interface MessageProps {
  msg: ChatMessageData
  [key: string]: unknown
}

export interface MessageRendererConfig {
  /** The React component to render this message */
  component: ComponentType<MessageProps>
  /** Optional: whether to render this message at all */
  isVisible?: (msg: ChatMessageData) => boolean
  /** Priority for conflict resolution (higher wins). Default: 0 */
  priority?: number
}

// ─── Registry State ──────────────────────────────────────────

const _registry = new Map<string, MessageRendererConfig>()
let _fallbackConfig: MessageRendererConfig | null = null

// ─── Registration API ────────────────────────────────────────

/**
 * Register a renderer for a specific message type.
 * @param type - The message type (e.g. 'user', 'assistant', 'tool_call')
 */
export function registerMessage(type: string, config: MessageRendererConfig): void {
  if (_registry.has(type)) {
    const existing = _registry.get(type)!
    if ((config.priority || 0) <= (existing.priority || 0)) return
  }
  _registry.set(type, config)
}

/** Register the fallback renderer */
export function registerFallbackMessage(config: MessageRendererConfig): void {
  _fallbackConfig = config
}

// ─── Resolution API ──────────────────────────────────────────

/**
 * Resolve the renderer for a message.
 * Also applies sub-type logic (e.g. thinking role → thinking renderer).
 */
export function getMessageRenderer(msg: ChatMessageData): MessageRendererConfig | null {
  // Sub-type: thinking role → use 'thinking' renderer
  const role = msg.message?.role
  if (role === 'thinking' && _registry.has('thinking')) {
    const config = _registry.get('thinking')!
    if (!config.isVisible || config.isVisible(msg)) return config
  }

  const config = _registry.get(msg.type)
  if (config) {
    if (!config.isVisible || config.isVisible(msg)) return config
  }

  return _fallbackConfig
}

/** Check if a message has a registered renderer */
export function hasMessageRenderer(msg: ChatMessageData): boolean {
  return getMessageRenderer(msg) !== null
}

/** List all registered message types (for debugging) */
export function listRegisteredMessages(): string[] {
  return Array.from(_registry.keys())
}
