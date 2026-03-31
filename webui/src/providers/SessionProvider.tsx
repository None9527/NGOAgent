/**
 * SessionProvider — manages sessionId, sessions list, and history.
 *
 * Phase 2 refactor: messages state moved to messageStore (Zustand).
 * SessionProvider now orchestrates session lifecycle, history loading,
 * and sidebar refresh — but no longer owns message state.
 */

import { createContext, useContext, useState, useCallback, useRef, type ReactNode } from 'react'
import { api } from '../chat/api'
import { historyToMessages } from '../chat/messageMapper'
import { useMessageStore } from '../stores/messageStore'
import type { SessionListItem } from '../chat/types'

interface SessionState {
  sessionId: string
  sessions: SessionListItem[]
}

interface SessionActions {
  setSessionId: (id: string) => void
  setSessions: React.Dispatch<React.SetStateAction<SessionListItem[]>>
  /** Load history for a session and reset scroll flag */
  loadHistory: (sid: string) => Promise<void>
  /** Refresh sessions list from backend (throttled) */
  refreshSessions: (force?: boolean) => Promise<void>
  /** Create new session + update sidebar */
  newSession: () => Promise<string>
  /** Delete session, create new one if current was deleted */
  deleteSession: (id: string) => Promise<void>
  /** Rename session */
  renameSession: (id: string, newTitle: string) => Promise<void>
  /** Initialize: load session list, return active session id */
  initialize: () => Promise<{ activeSessionId: string }>
  /** Flag: signals the scroll hook to snap to bottom after history load */
  pendingScrollToEnd: React.MutableRefObject<boolean>
}

type SessionContextValue = SessionState & SessionActions

const SessionContext = createContext<SessionContextValue | null>(null)

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession must be used within SessionProvider')
  return ctx
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [sessionId, setSessionId] = useState('')
  const [sessions, setSessions] = useState<SessionListItem[]>([])
  const pendingScrollToEnd = useRef(false)
  const lastRefreshRef = useRef(0)

  const loadHistory = useCallback(async (sid: string) => {
    try {
      const data = await api.getHistory(sid)
      const msgs = historyToMessages(data.messages, sid)
      useMessageStore.getState().replace(msgs)
      pendingScrollToEnd.current = true
    } catch (err) {
      console.error('Failed to load history', err)
      useMessageStore.getState().clear()
    }
  }, [])

  const refreshSessions = useCallback(async (force = false) => {
    if (!force) {
      const now = Date.now()
      if (now - lastRefreshRef.current < 3000) return
      lastRefreshRef.current = now
    } else {
      lastRefreshRef.current = Date.now()
    }
    try {
      const data = await api.listSessions()
      setSessions(data.sessions)
    } catch (err) {
      console.error('Failed to fetch sessions', err)
    }
  }, [])

  const newSession = useCallback(async (): Promise<string> => {
    const sess = await api.newSession()
    setSessionId(sess.session_id)
    useMessageStore.getState().clear()
    await refreshSessions(true)
    return sess.session_id
  }, [refreshSessions])

  const deleteSession = useCallback(async (id: string) => {
    try {
      await api.deleteSession(id)
      await refreshSessions(true)
      // If deleted current session, create a new one
      if (id === sessionId) {
        const sess = await api.newSession()
        setSessionId(sess.session_id)
        useMessageStore.getState().clear()
      }
    } catch (err) {
      console.error('Failed to delete session', err)
    }
  }, [sessionId, refreshSessions])

  const renameSession = useCallback(async (id: string, newTitle: string) => {
    try {
      await api.setSessionTitle(id, newTitle)
      await refreshSessions(true)
    } catch (err) {
      console.error('Failed to rename session', err)
    }
  }, [refreshSessions])

  const initialize = useCallback(async () => {
    const data = await api.listSessions()
    setSessions(data.sessions)
    if (data.active) {
      setSessionId(data.active)
    }
    return { activeSessionId: data.active || '' }
  }, [])

  return (
    <SessionContext.Provider value={{
      sessionId, sessions,
      setSessionId, setSessions,
      pendingScrollToEnd,
      loadHistory, refreshSessions,
      newSession, deleteSession, renameSession,
      initialize,
    }}>
      {children}
    </SessionContext.Provider>
  )
}
