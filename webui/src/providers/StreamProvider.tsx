/**
 * StreamProvider — manages SSE streaming state, callbacks, send/stop actions.
 * Uses useHub() directly for Intelligence Hub integration.
 */

import { createContext, useContext, useState, useCallback, useRef, useMemo, useEffect, type ReactNode } from 'react'
import { reconnectStream, getSharedWSClient, onWSStateChange } from '../chat/streamHandler'
import { uid } from '../chat/messageMapper'
import { useChatScroll } from '../hooks/useChatScroll'
import { useSession } from './SessionProvider'
import { useHub } from './HubProvider'
import type { ChatMessageData, StreamCallbacks, ApprovalRequest } from '../chat/types'

export interface SubagentProgressEntry {
  runID: string
  taskName: string
  status: 'running' | 'completed' | 'failed'
  done: number
  total: number
  error?: string
  output?: string
  currentStep?: string // current tool being executed
}

export type StreamPhase = 'idle' | 'streaming' | 'waiting_approval' | 'awaiting_subagents' | 'auto_waking' | 'reconnecting'

interface StreamState {
  isStreaming: boolean // computed: phase !== 'idle'
  streamPhase: StreamPhase
  connectionState: 'connected' | 'reconnecting' | 'disconnected'
  taskProgress: { taskName: string; status: string; summary: string; mode: string } | null
  subagentProgress: SubagentProgressEntry[]
}

interface StreamActions {
  stopStream: (sessionId: string) => Promise<void>
  reconnect: (sessionId: string, lastSeq: number) => void
  streamCallbacks: StreamCallbacks
  cancelRef: React.MutableRefObject<(() => void) | null>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
  handleScroll: () => void
  scrollToBottom: (behavior?: ScrollBehavior) => void
  resetToBottom: () => void
  followOutput: (isAtBottom: boolean) => false | 'smooth' | 'auto'
  handleAtBottomChange: (atBottom: boolean) => void
  userScrolledUpRef: React.MutableRefObject<boolean>
  isStreamingRef: React.MutableRefObject<boolean>
  enterStreamingMode: () => void
  exitStreamingMode: () => void
  pendingApprovals: ApprovalRequest[]
  setPendingApprovals: React.Dispatch<React.SetStateAction<ApprovalRequest[]>>
  planReview: { message: string; paths: string[] } | null
  setPlanReview: React.Dispatch<React.SetStateAction<{ message: string; paths: string[] } | null>>
  setIsStreaming: React.Dispatch<React.SetStateAction<boolean>>
  setStreamPhase: React.Dispatch<React.SetStateAction<StreamPhase>>
  setSubagentProgress: React.Dispatch<React.SetStateAction<SubagentProgressEntry[]>>
}

type StreamContextValue = StreamState & StreamActions

export const StreamContext = createContext<StreamContextValue | null>(null)

export function useStream(): StreamContextValue {
  const ctx = useContext(StreamContext)
  if (!ctx) throw new Error('useStream must be used within StreamProvider')
  return ctx
}

export function StreamProvider({ children }: { children: ReactNode }) {
  const { setMessages, setSessions, msgIndexRef, refreshSessions } = useSession()
  const hub = useHub()

  const [streamPhase, setStreamPhase] = useState<StreamPhase>('idle')
  const isStreaming = streamPhase !== 'idle'
  // Shim for backward compat: setIsStreaming(true) → 'streaming', false → 'idle'
  const setIsStreaming = useCallback((v: boolean | ((prev: boolean) => boolean)) => {
    if (typeof v === 'function') {
      setStreamPhase(prev => {
        const current = prev !== 'idle'
        const next = v(current)
        return next ? 'streaming' : 'idle'
      })
    } else {
      setStreamPhase(v ? 'streaming' : 'idle')
    }
  }, [])
  const [connectionState, setConnectionState] = useState<'connected' | 'reconnecting' | 'disconnected'>('connected')
  const [taskProgress, setTaskProgress] = useState<{ taskName: string; status: string; summary: string; mode: string } | null>(null)
  const [subagentProgress, setSubagentProgress] = useState<SubagentProgressEntry[]>([])
  const [pendingApprovals, setPendingApprovals] = useState<ApprovalRequest[]>([])
  const [planReview, setPlanReview] = useState<{ message: string; paths: string[] } | null>(null)
  const cancelRef = useRef<(() => void) | null>(null)
  const reconnectingRef = useRef(false) // Track reconnecting state for instant transition
  // Track the task_section msg id — only inject once per task, update in-place thereafter
  const currentTaskSectionIdRef = useRef<string | null>(null)

  const {
    scrollContainerRef, handleScroll, scrollToBottom, resetToBottom,
    enterStreamingMode, exitStreamingMode,
    followOutput, handleAtBottomChange, userScrolledUpRef,
    isStreamingRef,
  } = useChatScroll()

  // Pre-initialize WebSocket connection on mount so it's ready before first message.
  // Wire WS connection state → connectionState for accurate TopNavbar status.
  // NOTE: Do NOT close the WS on cleanup — it's a session-level singleton that must
  // survive React StrictMode's mount→unmount→remount cycle in dev.
  useEffect(() => {
    getSharedWSClient()
    const unsub = onWSStateChange((wsState: string) => {
      if (wsState === 'connected') setConnectionState('connected')
      else if (wsState === 'reconnecting' || wsState === 'connecting') setConnectionState('reconnecting')
      else if (wsState === 'closed') setConnectionState('disconnected')
    })
    return () => { unsub() } // Do NOT closeSharedWSClient() here — survives remounts
  }, [])

  // ── SSE Callbacks ──
  const onMessage = useCallback((msg: ChatMessageData) => {
    // Transition reconnecting → connected on first event
    if (reconnectingRef.current) {
      reconnectingRef.current = false
      setConnectionState('connected')
    }
    setMessages(prev => {
      if (prev.some(m => m.uuid === msg.uuid)) return prev
      msgIndexRef.current.set(msg.uuid, prev.length)
      return [...prev, msg]
    })
  }, [setMessages, msgIndexRef])

  const onUpdate = useCallback((uuid: string, patch: Partial<ChatMessageData>) => {
    setMessages(prev => {
      const idx = msgIndexRef.current.get(uuid)
      if (idx === undefined) return prev
      const m = prev[idx]
      if (!m) return prev
      const next = [...prev]
      if (patch.toolCall && m.toolCall) {
        next[idx] = {
          ...m, ...patch,
          toolCall: {
            ...m.toolCall,
            ...patch.toolCall,
            title: patch.toolCall.title || m.toolCall.title,
            rawInput: patch.toolCall.rawInput || m.toolCall.rawInput,
            content: patch.toolCall.content && patch.toolCall.content.length > 0
              ? patch.toolCall.content
              : m.toolCall.content,
          },
        }
      } else {
        next[idx] = { ...m, ...patch }
      }
      return next
    })
    if (patch.toolCall?.status === 'completed' && patch.toolCall?.kind) {
      const kind = patch.toolCall.kind as string
      if (['write', 'edit', 'updated_plan'].includes(kind)) {
        hub.setBrainRefreshTrigger(prev => prev + 1)
      }
      if (kind === 'updated_plan' && patch.toolCall.rawInput) {
        const planType = (patch.toolCall.rawInput as Record<string, unknown>).type as string
        const fileMap: Record<string, string> = { plan: 'plan.md', task: 'task.md', walkthrough: 'walkthrough.md' }
        const targetFile = fileMap[planType]
        if (targetFile) {
          hub.setBrainFocusTrigger({ file: targetFile, ts: Date.now() })
        }
      }
    }
  }, [setMessages, msgIndexRef, hub])

  const onToolCall = useCallback((msg: ChatMessageData) => {
    if (reconnectingRef.current) {
      reconnectingRef.current = false
      setConnectionState('connected')
    }
    setMessages(prev => {
      if (prev.some(m => m.uuid === msg.uuid)) return prev
      msgIndexRef.current.set(msg.uuid, prev.length)
      return [...prev, msg]
    })
    if (msg.toolCall && ['write', 'edit', 'updated_plan'].includes(msg.toolCall.kind)) {
      hub.setTab('brain')
      hub.setIsOpen(true)
    }
  }, [setMessages, msgIndexRef, hub])

  const subagentProgressRef = useRef(subagentProgress)
  subagentProgressRef.current = subagentProgress

  const streamPhaseRef = useRef(streamPhase)
  streamPhaseRef.current = streamPhase

  const onEnd = useCallback(() => {
    const currentPhase = streamPhaseRef.current
    // Guard: if cancelRef was already cleared (e.g. session-switch pre-cleanup),
    // this is a stale onEnd from an abandoned stream — don't reset state.
    // EXCEPTION: if we're in auto_waking/awaiting_subagents, force cleanup to prevent stuck UI.
    if (!cancelRef.current && currentPhase !== 'auto_waking' && currentPhase !== 'awaiting_subagents' && currentPhase !== 'streaming') {
      refreshSessions(true)
      return
    }
    // Always transition to idle on done event.
    // SubagentDock handles sub-agent progress display independently.
    // Auto-wake will set phase to 'auto_waking' via auto_wake_start event if needed.
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
    setConnectionState('connected')
    cancelRef.current = null
    refreshSessions(true)
    setTimeout(() => refreshSessions(true), 5000)
    setTimeout(() => refreshSessions(true), 10000)
  }, [exitStreamingMode, refreshSessions])

  const onError = useCallback((err: Error) => {
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
    // Do NOT set connectionState to 'disconnected' here —
    // stream errors are transient; WS connection may still be alive.
    // connectionState should only reflect actual WS state.
    cancelRef.current = null
    console.error('Stream error:', err)
    const errText = err instanceof Error ? err.message : String(err)
    setMessages(prev => [...prev, {
      uuid: `err-${Date.now()}`,
      timestamp: new Date().toISOString(),
      type: 'assistant' as const,
      message: { role: 'model', parts: [{ text: `⚠️ **Error:** ${errText}` }] },
    }])
  }, [exitStreamingMode, setMessages])

  const onAutoWakeStart = useCallback(() => {
    setStreamPhase('auto_waking')
    enterStreamingMode() // Re-enable scroll tracking
  }, [enterStreamingMode])

  const streamCallbacks = useMemo<StreamCallbacks>(() => ({
    onMessage,
    onUpdate,
    onToolCall,
    onApproval: (req: ApprovalRequest) => setPendingApprovals(prev => [...prev, req]),
    onPlanReview: (message: string, paths: string[]) => setPlanReview({ message, paths }),
    onStepDone: () => refreshSessions(true),
    onTitleUpdate: (sid: string, title: string) => {
      setSessions(prev => prev.map(s => s.id === sid ? { ...s, title } : s))
    },
    onProgress: (taskName: string, status: string, summary: string, mode: string) => {
      setTaskProgress({ taskName, status, summary, mode })
      // task_section dedup: inject once, then update in-place
      if (!currentTaskSectionIdRef.current) {
        // First progress event → create new task_section message
        const sectionId = uid()
        currentTaskSectionIdRef.current = sectionId
        onMessage({
          uuid: sectionId,
          timestamp: new Date().toISOString(),
          type: 'task_section',
          taskSection: { taskName, status, summary, mode },
        })
      } else {
        // Subsequent updates → patch the existing task_section in-place
        onUpdate(currentTaskSectionIdRef.current, {
          taskSection: { taskName, status, summary, mode },
        })
      }
    },
    onSubagentProgress: (runID: string, taskName: string, status: string, done: number, total: number, error?: string, output?: string, currentStep?: string) => {
      setSubagentProgress(prev => {
        const existing = prev.find(e => e.runID === runID)
        const entry: SubagentProgressEntry = {
          runID, taskName,
          status: status === 'failed' ? 'failed' : status === 'completed' ? 'completed' : 'running',
          done, total,
          error: error || undefined,
          output: output || undefined,
          currentStep: currentStep || existing?.currentStep || undefined,
        }
        if (existing) return prev.map(e => e.runID === runID ? entry : e)
        return [...prev, entry]
      })
    },
    onAutoWakeStart,
    onEnd,
    onError,
  }), [onMessage, onUpdate, onToolCall, onEnd, onError, onAutoWakeStart, setSessions, refreshSessions])

  const stopStream = useCallback(async (sessionId: string) => {
    if (streamPhase === 'idle') return
    try {
      const { api } = await import('../chat/api')
      await api.stop(sessionId)
    } catch { /* ignore */ }
    cancelRef.current?.()
    cancelRef.current = null
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
  }, [streamPhase, exitStreamingMode])

  const reconnect = useCallback((sessionId: string, lastSeq: number) => {
    setStreamPhase('reconnecting')
    setConnectionState('reconnecting')
    reconnectingRef.current = true
    enterStreamingMode()
    const handle = reconnectStream(sessionId, lastSeq, streamCallbacks)
    cancelRef.current = handle.cancel
  }, [enterStreamingMode, streamCallbacks])

  return (
    <StreamContext.Provider value={{
      isStreaming, streamPhase, connectionState, taskProgress, subagentProgress,
      stopStream, reconnect, streamCallbacks, cancelRef,
      scrollContainerRef, handleScroll, scrollToBottom, resetToBottom,
      followOutput, handleAtBottomChange, userScrolledUpRef, isStreamingRef,
      enterStreamingMode, exitStreamingMode,
      pendingApprovals, setPendingApprovals,
      planReview, setPlanReview,
      setIsStreaming, setStreamPhase, setSubagentProgress,
    }}>
      {children}
    </StreamContext.Provider>
  )
}
