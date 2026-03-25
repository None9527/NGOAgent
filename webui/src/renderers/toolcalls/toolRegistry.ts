/**
 * toolRegistry — Registry-based tool call renderer dispatch.
 *
 * Replaces the ~50-line switch/case in ChatViewer.tsx with a
 * pluggable registration system (Open-Closed Principle).
 *
 * Usage:
 *   // Register (in toolcalls/index.ts):
 *   registerTool('read', { component: ReadToolCall })
 *
 *   // Resolve (in ChatViewer):
 *   const config = getToolRenderer(toolCall)
 *   if (config) return <config.component {...props} />
 */

import type { ComponentType } from 'react'
import type { ToolCallData } from '../../chat/types'

// ─── Registry Types ──────────────────────────────────────────

export interface ToolCallProps {
  toolCall: ToolCallData
  [key: string]: unknown
}

export interface ToolRendererConfig {
  /** The React component to render this tool call */
  component: ComponentType<ToolCallProps>
  /** Optional: whether to render this tool call at all */
  isVisible?: (data: ToolCallData) => boolean
  /** Priority for conflict resolution (higher wins). Default: 0 */
  priority?: number
}

// ─── Registry State ──────────────────────────────────────────

const _registry = new Map<string, ToolRendererConfig>()
const _wildcardHandlers: Array<{
  match: (data: ToolCallData) => boolean
  config: ToolRendererConfig
}> = []

let _fallbackConfig: ToolRendererConfig | null = null

// ─── Registration API ────────────────────────────────────────

/**
 * Register a renderer for a specific tool kind.
 * @param kind - The tool kind string (e.g. 'read', 'edit', 'execute')
 */
export function registerTool(kind: string, config: ToolRendererConfig): void {
  if (_registry.has(kind)) {
    const existing = _registry.get(kind)!
    if ((config.priority || 0) <= (existing.priority || 0)) return
  }
  _registry.set(kind, config)
}

/**
 * Register a renderer for tool calls matching a custom predicate.
 * Useful for compound conditions (e.g. artifact detection).
 */
export function registerToolMatcher(
  match: (data: ToolCallData) => boolean,
  config: ToolRendererConfig,
): void {
  _wildcardHandlers.push({ match, config })
}

/** Register the fallback renderer (used when no match is found) */
export function registerFallbackTool(config: ToolRendererConfig): void {
  _fallbackConfig = config
}

// ─── Resolution API ──────────────────────────────────────────

/**
 * Resolve the best renderer for a tool call.
 * Resolution order:
 *   1. Exact kind match in registry
 *   2. Wildcard matcher (in registration order)
 *   3. Fallback renderer
 *   4. null (caller must handle)
 */
export function getToolRenderer(toolCall: ToolCallData): ToolRendererConfig | null {
  // 1. Exact kind match
  const kindKey = toolCall.kind ?? ''
  if (_registry.has(kindKey)) {
    const config = _registry.get(kindKey)!
    if (!config.isVisible || config.isVisible(toolCall)) {
      return config
    }
  }

  // 2. Wildcard matchers
  for (const { match, config } of _wildcardHandlers) {
    if (match(toolCall)) {
      if (!config.isVisible || config.isVisible(toolCall)) {
        return config
      }
    }
  }

  // 3. Fallback
  return _fallbackConfig
}

/**
 * Check if a tool call has a registered renderer.
 * Useful for deciding whether to render or skip.
 */
export function hasToolRenderer(toolCall: ToolCallData): boolean {
  return getToolRenderer(toolCall) !== null
}

/** List all registered tool kinds (for debugging) */
export function listRegisteredTools(): string[] {
  return Array.from(_registry.keys())
}
