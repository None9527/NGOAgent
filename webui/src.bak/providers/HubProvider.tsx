/**
 * HubProvider — manages Intelligence Hub state (isOpen, activeTab, brain triggers).
 * Replaces Hub-related useState from App.tsx and the hubDispatchers bridge in StreamProvider.
 */

import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'

interface HubState {
  isOpen: boolean
  tab: 'brain' | 'knowledge' | 'cron' | 'skills'
  brainRefreshTrigger: number
  brainFocusTrigger: { file: string; ts: number } | null
}

interface HubActions {
  openTab: (tab: HubState['tab']) => void
  close: () => void
  toggle: () => void
  triggerRefresh: () => void
  focusFile: (file: string) => void
  /** Raw setters for SSE callback integration */
  setTab: React.Dispatch<React.SetStateAction<HubState['tab']>>
  setIsOpen: React.Dispatch<React.SetStateAction<boolean>>
  setBrainRefreshTrigger: React.Dispatch<React.SetStateAction<number>>
  setBrainFocusTrigger: React.Dispatch<React.SetStateAction<{ file: string; ts: number } | null>>
}

type HubContextValue = HubState & HubActions

const HubContext = createContext<HubContextValue | null>(null)

export function useHub(): HubContextValue {
  const ctx = useContext(HubContext)
  if (!ctx) throw new Error('useHub must be used within HubProvider')
  return ctx
}

export function HubProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false)
  const [tab, setTab] = useState<HubState['tab']>('brain')
  const [brainRefreshTrigger, setBrainRefreshTrigger] = useState(0)
  const [brainFocusTrigger, setBrainFocusTrigger] = useState<{ file: string; ts: number } | null>(null)

  const openTab = useCallback((t: HubState['tab']) => {
    setTab(t)
    setIsOpen(true)
  }, [])

  const close = useCallback(() => setIsOpen(false), [])
  const toggle = useCallback(() => setIsOpen(prev => !prev), [])
  const triggerRefresh = useCallback(() => setBrainRefreshTrigger(prev => prev + 1), [])
  const focusFile = useCallback((file: string) => setBrainFocusTrigger({ file, ts: Date.now() }), [])

  return (
    <HubContext.Provider value={{
      isOpen, tab, brainRefreshTrigger, brainFocusTrigger,
      openTab, close, toggle, triggerRefresh, focusFile,
      setTab, setIsOpen, setBrainRefreshTrigger, setBrainFocusTrigger,
    }}>
      {children}
    </HubContext.Provider>
  )
}
