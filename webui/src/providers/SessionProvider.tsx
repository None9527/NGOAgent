/**
 * SessionProvider — manages sessionId, sessions list, messages, and history.
 * Eliminates ~6 useState + 4 handlers from App.tsx.
 */

import { createContext, useContext, useState, useCallback, useRef, type ReactNode } from 'react'
import { api } from '../chat/api'
import { historyToMessages } from '../chat/messageMapper'
import type { ChatMessageData, SessionListItem } from '../chat/types'

interface SessionState {
  sessionId: string
  sessions: SessionListItem[]
  messages: ChatMessageData[]
}

interface SessionActions {
  setSessionId: (id: string) => void
  setSessions: React.Dispatch<React.SetStateAction<SessionListItem[]>>
  setMessages: React.Dispatch<React.SetStateAction<ChatMessageData[]>>
  /** O(1) message index lookup used during streaming */
  msgIndexRef: React.MutableRefObject<Map<string, number>>
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
  const [messages, setMessages] = useState<ChatMessageData[]>([])
  const msgIndexRef = useRef<Map<string, number>>(new Map())
  const pendingScrollToEnd = useRef(false)
  const lastRefreshRef = useRef(0)

  const loadHistory = useCallback(async (sid: string) => {
    try {
      const data = await api.getHistory(sid)
      setMessages(() => {
        const msgs = historyToMessages(data.messages, sid)
        const idx = new Map<string, number>()
        msgs.forEach((m, i) => idx.set(m.uuid, i))
        msgIndexRef.current = idx
        return msgs
      })
      pendingScrollToEnd.current = true
    } catch (err) {
      console.error('Failed to load history', err)
      setMessages([])
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
    setMessages([])
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
        setMessages([])
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
      sessionId, sessions, messages,
      setSessionId, setSessions, setMessages,
      msgIndexRef, pendingScrollToEnd,
      loadHistory, refreshSessions,
      newSession, deleteSession, renameSession,
      initialize,
    }}>
      {children}
    </SessionContext.Provider>
  )
}
