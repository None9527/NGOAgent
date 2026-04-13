/**
 * StreamProvider — manages SSE streaming state, callbacks, send/stop actions.
 * Uses useHub() directly for Intelligence Hub integration.
 *
 * Phase 2 refactor: messages writes go through messageStore (Zustand).
 * Phase 3 refactor: scroll logic moved to ScrollProvider. StreamProvider
 * consumes scroll capabilities via useScrollContext().
 */

import { createContext, useContext, useState, useCallback, useRef, useMemo, useEffect, useLayoutEffect, type ReactNode } from 'react'
import { reconnectStream, getSharedWSClient, onWSStateChange } from '../chat/streamHandler'
import { api } from '../chat/api'
import { uid } from '../chat/messageMapper'
import { useScrollContext } from './ScrollProvider'
import { useSession } from './SessionProvider'
import { useHub } from './HubProvider'
import { useMessageStore } from '../stores/messageStore'
import type { ChatMessageData, StreamCallbacks, ApprovalRequest, SubagentProgressEntry, StreamPhase } from '../chat/types'

export type { SubagentProgressEntry, StreamPhase }

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
  scrollContainerRef: React.MutableRefObject<HTMLDivElement | null>

  scrollToBottom: (behavior?: ScrollBehavior) => void
  resetToBottom: () => void
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
  const { setSessions, refreshSessions } = useSession()
  const hub = useHub()
  const store = useMessageStore

  // Subscribe to messages for useLayoutEffect scroll trigger
  const messages = useMessageStore(s => s.messages)

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
    scrollContainerRef, scrollToEnd, resetToBottom,
    enterStreamingMode, exitStreamingMode,
    isStreamingRef, autoScrollRef, userScrolledUpRef,
  } = useScrollContext()

  // Pre-initialize WebSocket connection on mount so it's ready before first message.
  // Wire WS connection state → connectionState for accurate TopNavbar status.
  useEffect(() => {
    getSharedWSClient()
    const unsub = onWSStateChange((wsState: string) => {
      if (wsState === 'connected') setConnectionState('connected')
      else if (wsState === 'reconnecting' || wsState === 'connecting') setConnectionState('reconnecting')
      else if (wsState === 'closed') setConnectionState('disconnected')
    })
    return () => { unsub() } // Do NOT closeSharedWSClient() here — survives remounts
  }, [])

  // ── Streaming auto-scroll ──
  // useLayoutEffect fires AFTER React commits DOM changes, BEFORE browser paints.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useLayoutEffect(() => {
    if (!isStreamingRef.current || !autoScrollRef.current) return
    scrollToEnd()
  }, [messages]) // fires on every messages state change

  // ── SSE Callbacks (Phase 2: use messageStore) ──

  const onMessage = useCallback((msg: ChatMessageData) => {
    // Transition reconnecting → connected on first event
    if (reconnectingRef.current) {
      reconnectingRef.current = false
      setConnectionState('connected')
    }
    // store.add() returns false if uuid already exists → StrictMode safe dedup
    store.getState().add(msg)
  }, [store])

  // ── RAF Throttle for streaming text updates ──
  const pendingTextPatchRef = useRef<Map<string, Partial<ChatMessageData>>>(new Map())
  const rafIdRef = useRef(0)

  const flushTextUpdates = useCallback(() => {
    rafIdRef.current = 0
    const pending = pendingTextPatchRef.current
    if (pending.size === 0) return
    // batchUpdate auto-handles index lookups + fallbacks
    store.getState().batchUpdate(pending)
    pending.clear()
  }, [store])

  // Cleanup RAF on unmount
  useEffect(() => {
    return () => { if (rafIdRef.current) cancelAnimationFrame(rafIdRef.current) }
  }, [])

  const onUpdate = useCallback((uuid: string, patch: Partial<ChatMessageData>) => {
    // Fast path: streaming text updates → batch via RAF
    if (patch.isStreaming === true && patch.message && !patch.toolCall) {
      pendingTextPatchRef.current.set(uuid, patch)
      if (!rafIdRef.current) {
        rafIdRef.current = requestAnimationFrame(flushTextUpdates)
      }
      return
    }

    // Slow path: toolCall updates, isStreaming:false transitions → immediate
    // MERGE any pending RAF text patch so accumulated text isn't lost.
    const pendingPatch = pendingTextPatchRef.current.get(uuid)
    pendingTextPatchRef.current.delete(uuid)
    const mergedPatch = pendingPatch ? { ...pendingPatch, ...patch } : patch

    // store.update() handles toolCall deep merge + index fallback internally
    store.getState().update(uuid, mergedPatch)

    if (patch.toolCall?.status === 'completed' && patch.toolCall?.kind) {
      const kind = patch.toolCall.kind as string
      if (['write', 'edit', 'updated_plan'].includes(kind)) {
        const title = String(patch.toolCall.title || '').toLowerCase()
        if (/\b(task\.md|implementation_plan\.md|walkthrough\.md|plan\.md)\b/.test(title) || kind === 'updated_plan') {
          hub.setBrainRefreshTrigger(prev => prev + 1)
        }
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
  }, [store, hub, flushTextUpdates])

  const onToolCall = useCallback((msg: ChatMessageData) => {
    if (reconnectingRef.current) {
      reconnectingRef.current = false
      setConnectionState('connected')
    }
    // store.add() handles dedup internally
    store.getState().add(msg)
    if (msg.toolCall && ['write', 'edit', 'updated_plan'].includes(msg.toolCall.kind)) {
      const title = String(msg.toolCall.title || '').toLowerCase()
      const isBrainArtifact = /\b(task\.md|implementation_plan\.md|walkthrough\.md|plan\.md)\b/.test(title)
      if (isBrainArtifact || msg.toolCall.kind === 'updated_plan') {
        hub.setTab('brain')
        hub.setIsOpen(true)
      }
    }
  }, [store, hub])

  const subagentProgressRef = useRef(subagentProgress)
  subagentProgressRef.current = subagentProgress

  const streamPhaseRef = useRef(streamPhase)
  streamPhaseRef.current = streamPhase

  /** Drain pending RAF patches — used by onEnd/onError/stopStream (A5 fix) */
  const drainPendingPatches = useCallback(() => {
    if (rafIdRef.current) { cancelAnimationFrame(rafIdRef.current); rafIdRef.current = 0 }
    pendingTextPatchRef.current.clear()
  }, [])

  const onEnd = useCallback(() => {
    // Always transition to idle — any received 'done' event means agent finished.
    // Previous guard (!cancelRef.current && phase check) was too strict:
    // WS connections keep cancelRef alive after 'done', causing guard to skip idle reset.
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
    drainPendingPatches()
    // D9 fix: Don't force connectionState to 'connected' — WS onStateChange
    // manages the real connection state. Forcing here causes stale indicator
    // when WS disconnects at the same time as stream ends.
    cancelRef.current = null
    refreshSessions(true)
  }, [exitStreamingMode, refreshSessions, drainPendingPatches])

  const onError = useCallback((err: Error) => {
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
    drainPendingPatches()
    cancelRef.current = null
    console.error('Stream error:', err)
    const errText = err instanceof Error ? err.message : String(err)
    store.getState().appendError(errText)
  }, [exitStreamingMode, store, drainPendingPatches])

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
        const sectionId = uid()
        currentTaskSectionIdRef.current = sectionId
        onMessage({
          uuid: sectionId,
          timestamp: new Date().toISOString(),
          type: 'task_section',
          taskSection: { taskName, status, summary, mode },
        })
      } else {
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
      await api.stop(sessionId)
    } catch { /* ignore */ }
    cancelRef.current?.()
    cancelRef.current = null
    setStreamPhase('idle')
    exitStreamingMode()
    setTaskProgress(null)
    currentTaskSectionIdRef.current = null
    drainPendingPatches()
  }, [streamPhase, exitStreamingMode, drainPendingPatches])

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
      scrollContainerRef, scrollToBottom: scrollToEnd, resetToBottom,
      userScrolledUpRef, isStreamingRef,
      enterStreamingMode, exitStreamingMode,
      pendingApprovals, setPendingApprovals,
      planReview, setPlanReview,
      setIsStreaming, setStreamPhase, setSubagentProgress,
    }}>
      {children}
    </StreamContext.Provider>
  )
}
