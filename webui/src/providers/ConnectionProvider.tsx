/**
 * ConnectionProvider — observes WS connection state + exposes stop action.
 *
 * SINGLE RESPONSIBILITY: connection state observation + scroll ref.
 * — Phase 3: thin wrapper around existing streamHandler exports
 * — Phase 7: this will absorb all transport logic from StreamProvider
 *
 * Does NOT call reconnectStream directly (requires StreamCallbacks,
 * will be wired in Phase 7 when StreamProvider is removed).
 */

import {
  createContext,
  useContext,
  useCallback,
  useState,
  useEffect,
  type ReactNode,
} from 'react'
import { onWSStateChange } from '../chat/streamHandler'
import { useChatScroll } from '../hooks/useChatScroll'
import { api } from '../chat/api'

export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'

interface ConnectionContextValue {
  connectionState: ConnectionState
  stopStream: (sessionId: string) => Promise<void>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
  handleScroll: () => void
  scrollToBottom: (behavior?: ScrollBehavior) => void
  resetToBottom: () => void
}

const ConnectionContext = createContext<ConnectionContextValue | null>(null)

export function useConnection(): ConnectionContextValue {
  const ctx = useContext(ConnectionContext)
  if (!ctx) throw new Error('useConnection must be used within ConnectionProvider')
  return ctx
}

export function ConnectionProvider({ children }: { children: ReactNode }) {
  const { scrollContainerRef, handleScroll, scrollToBottom, resetToBottom } =
    useChatScroll()

  const [connectionState, setConnectionState] = useState<ConnectionState>('connected')

  useEffect(() => {
    return onWSStateChange((state) => {
      setConnectionState(
        state === 'open' || state === 'connected'
          ? 'connected'
          : state === 'connecting' || state === 'reconnecting'
          ? 'reconnecting'
          : 'disconnected',
      )
    })
  }, [])

  const stopStream = useCallback(async (sessionId: string) => {
    try {
      await api.stop(sessionId)
    } catch (e) {
      console.error('[ConnectionProvider] stopStream error:', e)
    }
  }, [])

  const value: ConnectionContextValue = {
    connectionState,
    stopStream,
    scrollContainerRef,
    handleScroll,
    scrollToBottom,
    resetToBottom,
  }

  return (
    <ConnectionContext.Provider value={value}>
      {children}
    </ConnectionContext.Provider>
  )
}
