/**
 * uiStore — Zustand store for transient UI state.
 *
 * Covers layout toggles, input text, and file attachments.
 * Replaces scattered useState in App.tsx.
 */

import { create } from 'zustand'
import type { FileItem } from '../renderers/InputForm'

interface UIState {
  sidebarOpen: boolean
  settingsOpen: boolean
  hubOpen: boolean
  inputText: string
  attachedFiles: FileItem[]
  planFeedbackInput: string
  showFeedbackInput: boolean
}

interface UIActions {
  setSidebarOpen: (v: boolean) => void
  toggleSidebar: () => void
  setSettingsOpen: (v: boolean) => void
  setHubOpen: (v: boolean) => void
  setInputText: (v: string) => void
  /** Supports both direct array and functional updater (like React setState) */
  setAttachedFiles: (filesOrUpdater: FileItem[] | ((prev: FileItem[]) => FileItem[])) => void
  addAttachedFile: (file: FileItem) => void
  removeAttachedFile: (name: string) => void
  clearAttachedFiles: () => void
  setPlanFeedbackInput: (v: string) => void
  setShowFeedbackInput: (v: boolean) => void
}

type UIStore = UIState & UIActions

export const useUIStore = create<UIStore>((set, get) => ({
  // ── Initial state ──
  sidebarOpen: true,
  settingsOpen: false,
  hubOpen: false,
  inputText: '',
  attachedFiles: [],
  planFeedbackInput: '',
  showFeedbackInput: false,

  // ── Actions ──
  setSidebarOpen: (v) => set({ sidebarOpen: v }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSettingsOpen: (v) => set({ settingsOpen: v }),
  setHubOpen: (v) => set({ hubOpen: v }),
  setInputText: (v) => set({ inputText: v }),
  // Supports both direct array and functional updater (avoids stale closure)
  setAttachedFiles: (filesOrUpdater) => {
    if (typeof filesOrUpdater === 'function') {
      set({ attachedFiles: filesOrUpdater(get().attachedFiles) })
    } else {
      set({ attachedFiles: filesOrUpdater })
    }
  },
  addAttachedFile: (file) =>
    set((s) => ({ attachedFiles: [...s.attachedFiles, file] })),
  removeAttachedFile: (name) =>
    set((s) => ({
      attachedFiles: s.attachedFiles.filter((f) => f.name !== name),
    })),
  clearAttachedFiles: () => set({ attachedFiles: [] }),
  setPlanFeedbackInput: (v) => set({ planFeedbackInput: v }),
  setShowFeedbackInput: (v) => set({ showFeedbackInput: v }),
}))
