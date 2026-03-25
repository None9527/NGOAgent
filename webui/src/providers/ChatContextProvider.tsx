/**
 * ChatContextProvider — unified context hook for all chat rendering needs.
 *
 * Analogous to Antigravity's Wn() hook:
 *   Wn() → { chatParams, cascadeContext, renderers, stepHandler }
 *   useChatContext() → { connection, renderers, actions, config }
 *
 * Aggregates services from all parent providers into a single hook,
 * so child renderers only need ONE import instead of 4.
 */

import { createContext, useContext, useMemo, useCallback, type ReactNode } from 'react'
import { useConnection, type ConnectionState } from './ConnectionProvider'
import { useConfig } from './ConfigProvider'
import { getApiBase } from '../chat/api'
import type { ToolCallData } from '../chat/types'

// ─── Registry types (populated by Phase 4) ──────────────────

export interface ToolRendererConfig {
  component: React.ComponentType<Record<string, unknown>>
  isVisible?: (data: ToolCallData) => boolean
  priority?: number
}

export interface MessageRendererConfig {
  component: React.ComponentType<Record<string, unknown>>
  isVisible?: (data: Record<string, unknown>) => boolean
}

// ─── Context shape ───────────────────────────────────────────

interface ChatContextValue {
  /** Connection services (from ConnectionProvider) */
  connection: {
    connectionState: ConnectionState
    stopStream: (sessionId: string) => Promise<void>
    scrollToBottom: (behavior?: ScrollBehavior) => void
  }

  /** Renderer registries (populated after Phase 4) */
  renderers: {
    getToolRenderer: (toolCall: ToolCallData) => ToolRendererConfig | null
    getMessageRenderer: (type: string) => MessageRendererConfig | null
  }

  /** Shared actions */
  actions: {
    copyToClipboard: (text: string) => Promise<void>
    openFile: (path: string) => void
  }

  /** Product configuration */
  config: {
    planMode: boolean
    availableModels: string[]
    apiBase: string
  }
}

const ChatContext = createContext<ChatContextValue | null>(null)

export function useChatContext(): ChatContextValue {
  const ctx = useContext(ChatContext)
  if (!ctx) throw new Error('useChatContext must be used within ChatContextProvider')
  return ctx
}

export function ChatContextProvider({ children }: { children: ReactNode }) {
  const connection = useConnection()
  const configCtx = useConfig()

  const copyToClipboard = useCallback(async (text: string) => {
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text)
      } else {
        const el = document.createElement('textarea')
        el.value = text
        el.style.position = 'fixed'
        el.style.opacity = '0'
        document.body.appendChild(el)
        el.select()
        document.execCommand('copy')
        document.body.removeChild(el)
      }
    } catch (e) {
      console.warn('[ChatContext] copyToClipboard failed:', e)
    }
  }, [])

  const openFile = useCallback((path: string) => {
    // TODO Phase 4+: integrate with file viewer panel
    console.log('[ChatContext] openFile:', path)
  }, [])

  // Renderer registries — will be populated in Phase 4
  // For now, return null to indicate "use fallback"
  const renderers = useMemo(() => ({
    getToolRenderer: (_toolCall: ToolCallData): ToolRendererConfig | null => null,
    getMessageRenderer: (_type: string): MessageRendererConfig | null => null,
  }), [])

  const value: ChatContextValue = useMemo(() => ({
    connection: {
      connectionState: connection.connectionState,
      stopStream: connection.stopStream,
      scrollToBottom: connection.scrollToBottom,
    },
    renderers,
    actions: {
      copyToClipboard,
      openFile,
    },
    config: {
      planMode: configCtx.planMode === 'plan',
      availableModels: configCtx.availableModels ?? [],
      apiBase: getApiBase(),
    },
  }), [connection, renderers, copyToClipboard, openFile, configCtx])

  return (
    <ChatContext.Provider value={value}>
      {children}
    </ChatContext.Provider>
  )
}
