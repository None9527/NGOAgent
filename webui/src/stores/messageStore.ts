/**
 * messageStore — Single source of truth for chat messages.
 *
 * Encapsulates messages[] + internal uuid→index Map.
 * All mutations go through semantic methods → index always in sync.
 * Operates outside React lifecycle → immune to StrictMode double-invoke.
 *
 * Fixes: A1 (updater purity), A2 (scattered setMessages), A3 (off-by-one),
 *        A4 (msgIndexRef consistency), A5 partial (onError index sync).
 */

import { create } from 'zustand'
import type { ChatMessageData } from '../chat/types'

interface MessageStore {
  // State
  messages: ChatMessageData[]

  // Mutations — every method auto-syncs internal index
  /** Add a message. Returns false if uuid already exists (dedup). */
  add: (msg: ChatMessageData) => boolean
  /** Update a single message by uuid. Deep-merges toolCall if both exist. */
  update: (uuid: string, patch: Partial<ChatMessageData>) => void
  /** Batch-update multiple messages (used by RAF text flush). */
  batchUpdate: (patches: Map<string, Partial<ChatMessageData>>) => void
  /** Replace all messages (loadHistory). */
  replace: (msgs: ChatMessageData[]) => void
  /** Clear all messages (newSession/deleteSession). */
  clear: () => void
  /** Strip from last user message onward (inclusive). Returns the stripped user text, or undefined. */
  stripFromLastUser: () => string | undefined
  /** Append an error message with auto-indexing. */
  appendError: (text: string) => void

  // Read — O(1) index lookup
  getIndex: (uuid: string) => number | undefined
}

// Internal uuid→index map. Separated from Zustand state to avoid
// unnecessary re-renders — only messages[] changes trigger re-renders.
const _index = new Map<string, number>()

function rebuildIndex(msgs: ChatMessageData[]) {
  _index.clear()
  msgs.forEach((m, i) => _index.set(m.uuid, i))
}

export const useMessageStore = create<MessageStore>((set, get) => ({
  messages: [],

  add: (msg) => {
    if (_index.has(msg.uuid)) return false // dedup — StrictMode safe
    const msgs = get().messages
    _index.set(msg.uuid, msgs.length)
    set({ messages: [...msgs, msg] })
    return true
  },

  update: (uuid, patch) => {
    let idx = _index.get(uuid)
    const msgs = get().messages
    // Guard: stale index → linear fallback + re-index
    if (idx === undefined || idx >= msgs.length || msgs[idx]?.uuid !== uuid) {
      idx = msgs.findIndex(m => m.uuid === uuid)
      if (idx >= 0) _index.set(uuid, idx)
    }
    if (idx === undefined || idx < 0 || !msgs[idx]) return

    const m = msgs[idx]
    const next = [...msgs]

    // Deep merge toolCall if both exist
    if (patch.toolCall && m.toolCall) {
      next[idx] = {
        ...m, ...patch,
        toolCall: {
          ...m.toolCall, ...patch.toolCall,
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
    set({ messages: next })
  },

  batchUpdate: (patches) => {
    if (patches.size === 0) return
    const next = [...get().messages]
    for (const [uuid, patch] of patches) {
      let idx = _index.get(uuid)
      if (idx === undefined || idx >= next.length || next[idx]?.uuid !== uuid) {
        idx = next.findIndex(m => m.uuid === uuid)
        if (idx >= 0) _index.set(uuid, idx)
      }
      if (idx !== undefined && idx >= 0 && next[idx]) {
        next[idx] = { ...next[idx], ...patch }
      }
    }
    set({ messages: next })
  },

  replace: (msgs) => {
    rebuildIndex(msgs)
    set({ messages: msgs })
  },

  clear: () => {
    _index.clear()
    set({ messages: [] })
  },

  stripFromLastUser: () => {
    const msgs = get().messages
    let lastUserIdx = -1
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].type === 'user') { lastUserIdx = i; break }
    }
    if (lastUserIdx === -1) return undefined
    const userText = msgs[lastUserIdx].message?.parts?.[0]?.text || ''
    const trimmed = msgs.slice(0, lastUserIdx)
    rebuildIndex(trimmed)
    set({ messages: trimmed })
    return userText
  },

  appendError: (text) => {
    const errMsg: ChatMessageData = {
      uuid: `err-${Date.now()}`,
      timestamp: new Date().toISOString(),
      type: 'assistant',
      message: { role: 'model', parts: [{ text: `⚠️ **Error:** ${text}` }] },
    }
    const msgs = get().messages
    _index.set(errMsg.uuid, msgs.length)
    set({ messages: [...msgs, errMsg] })
  },

  getIndex: (uuid) => _index.get(uuid),
}))
