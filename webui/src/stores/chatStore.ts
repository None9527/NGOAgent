/**
 * chatStore — Zustand store, single source of truth for all chat state.
 *
 * Replaces ~30 scattered useState calls across App.tsx and Providers.
 * Uses msgIndex Map for O(1) message lookups instead of Array.findIndex.
 */

import { create } from 'zustand'
import type {
  ChatMessageData,
  StreamPhase,
  SubagentProgressEntry,
} from '../chat/types'
import type { ApprovalRequest } from '../chat/types'

// ─── Task Progress ──────────────────────────────────────────

export interface TaskProgress {
  taskName: string
  status: string
  summary: string
  mode: string
}

export interface PlanReview {
  message: string
  paths: string[]
}

// ─── Store Shape ────────────────────────────────────────────

interface ChatState {
  // ── Messages ──
  messages: ChatMessageData[]
  msgIndex: Map<string, number>     // uuid → array index (O(1) lookup)

  // ── Session ──
  sessionId: string | null

  // ── Stream state ──
  streamPhase: StreamPhase
  taskProgress: TaskProgress | null
  subagentProgress: SubagentProgressEntry[]

  // ── Approvals ──
  pendingApprovals: ApprovalRequest[]
  planReview: PlanReview | null

  // ── RAF batching (internal) ──
  _pendingTextUpdate: { uuid: string; text: string } | null
  _rafHandle: number | null
}

interface ChatActions {
  // ── Messages ──
  addMessage: (msg: ChatMessageData) => void
  updateMessage: (uuid: string, patch: Partial<ChatMessageData>) => void
  /** Accumulate streaming text — RAF-throttled, max 1 React update per frame */
  batchUpdateText: (uuid: string, text: string) => void
  /** Remove all messages after (and including) the given uuid — used for retry */
  removeMessagesFrom: (uuid: string) => void
  setMessages: (msgs: ChatMessageData[]) => void
  clearMessages: () => void

  // ── Session ──
  setSessionId: (id: string | null) => void

  // ── Stream ──
  setStreamPhase: (phase: StreamPhase) => void
  setTaskProgress: (p: TaskProgress | null) => void
  updateSubagent: (entry: SubagentProgressEntry) => void
  clearSubagents: () => void

  // ── Approvals ──
  addApproval: (req: ApprovalRequest) => void
  removeApproval: (id: string) => void
  setPlanReview: (r: PlanReview | null) => void
}

type ChatStore = ChatState & ChatActions

// ─── Store Implementation ────────────────────────────────────

export const useChatStore = create<ChatStore>((set, get) => ({
  // ── Initial state ──
  messages: [],
  msgIndex: new Map(),
  sessionId: null,
  streamPhase: 'idle',
  taskProgress: null,
  subagentProgress: [],
  pendingApprovals: [],
  planReview: null,
  _pendingTextUpdate: null,
  _rafHandle: null,

  // ── Message actions ──

  addMessage: (msg) =>
    set((state) => {
      const messages = [...state.messages, msg]
      const msgIndex = new Map(state.msgIndex)
      msgIndex.set(msg.uuid, messages.length - 1)
      return { messages, msgIndex }
    }),

  updateMessage: (uuid, patch) =>
    set((state) => {
      const idx = state.msgIndex.get(uuid)
      if (idx === undefined) return state
      const messages = [...state.messages]
      messages[idx] = { ...messages[idx], ...patch }
      return { messages }
    }),

  batchUpdateText: (uuid, text) => {
    // Accumulate in state, flush via requestAnimationFrame
    const state = get()

    // Cancel existing RAF if pending
    if (state._rafHandle !== null) {
      cancelAnimationFrame(state._rafHandle)
    }

    // Stage the latest update
    const rafHandle = requestAnimationFrame(() => {
      const s = get()
      const pending = s._pendingTextUpdate
      if (!pending) return

      const idx = s.msgIndex.get(pending.uuid)
      if (idx === undefined) return

      const messages = [...s.messages]
      messages[idx] = {
        ...messages[idx],
        message: {
          ...messages[idx].message,
          parts: [{ text: pending.text }],
        },
      }
      set({ messages, _pendingTextUpdate: null, _rafHandle: null })
    })

    set({ _pendingTextUpdate: { uuid, text }, _rafHandle: rafHandle })
  },

  removeMessagesFrom: (uuid) =>
    set((state) => {
      const idx = state.msgIndex.get(uuid)
      if (idx === undefined) return state
      const messages = state.messages.slice(0, idx)
      const msgIndex = new Map<string, number>()
      messages.forEach((m, i) => msgIndex.set(m.uuid, i))
      return { messages, msgIndex }
    }),

  setMessages: (msgs) => {
    const msgIndex = new Map<string, number>()
    msgs.forEach((m, i) => msgIndex.set(m.uuid, i))
    set({ messages: msgs, msgIndex })
  },

  clearMessages: () => set({ messages: [], msgIndex: new Map() }),

  // ── Session ──
  setSessionId: (id) => set({ sessionId: id }),

  // ── Stream ──
  setStreamPhase: (phase) => set({ streamPhase: phase }),
  setTaskProgress: (p) => set({ taskProgress: p }),

  updateSubagent: (entry) =>
    set((state) => {
      const existing = state.subagentProgress.findIndex(e => e.runID === entry.runID)
      if (existing >= 0) {
        const updated = [...state.subagentProgress]
        updated[existing] = { ...updated[existing], ...entry }
        return { subagentProgress: updated }
      }
      return { subagentProgress: [...state.subagentProgress, entry] }
    }),

  clearSubagents: () => set({ subagentProgress: [] }),

  // ── Approvals ──
  addApproval: (req) =>
    set((state) => ({ pendingApprovals: [...state.pendingApprovals, req] })),

  removeApproval: (id) =>
    set((state) => ({
      pendingApprovals: state.pendingApprovals.filter(r => r.approvalId !== id),
    })),

  setPlanReview: (r) => set({ planReview: r }),
}))

// ─── Convenience selectors ──────────────────────────────────

/** Get a single message by uuid (O(1)) */
export function getMessageByUuid(uuid: string): ChatMessageData | undefined {
  const { messages, msgIndex } = useChatStore.getState()
  const idx = msgIndex.get(uuid)
  return idx !== undefined ? messages[idx] : undefined
}

/** Get the last assistant message uuid */
export function getLastAssistantUuid(): string | undefined {
  const { messages } = useChatStore.getState()
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].type === 'assistant') return messages[i].uuid
  }
  return undefined
}

/** Shorthand: is streaming? */
export function isStreaming(): boolean {
  return useChatStore.getState().streamPhase !== 'idle'
}
