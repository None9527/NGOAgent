import React, { useState, useRef } from 'react'
import { BrainPanel } from '../BrainPanel'
import { KIManager } from '../KIManager'
import { CronManager } from '../CronManager'
import { SkillMCPManager } from '../SkillMCPManager'

export interface IntelligenceHubProps {
  sessionId: string;
  activeTab: 'brain' | 'knowledge' | 'cron' | 'skills';
  onTabChange: (tab: 'brain' | 'knowledge' | 'cron' | 'skills') => void;
  onClose: () => void;
  refreshTrigger?: number;
  brainFocusTrigger?: { file: string; ts: number } | null;
}

const TABS = [
  { id: 'brain' as const,     icon: '🧠', label: 'Brain' },
  { id: 'knowledge' as const, icon: '💡', label: 'KI' },
  { id: 'cron' as const,      icon: '⏰', label: 'Cron' },
  { id: 'skills' as const,    icon: '🧩', label: 'Skills' },
]

export const IntelligenceHub: React.FC<IntelligenceHubProps> = ({
  sessionId,
  activeTab,
  onTabChange,
  onClose,
  refreshTrigger,
  brainFocusTrigger
}) => {
  const [detailViews, setDetailViews] = useState<Record<string, string | null>>({})
  const startYRef = useRef<number>(0)
  const sheetRef = useRef<HTMLDivElement>(null)

  const handleNavigateDetail = (tab: string, id: string | null) => {
    setDetailViews(prev => ({ ...prev, [tab]: id }))
  }

  const isDrilledDown = detailViews[activeTab] !== null && detailViews[activeTab] !== undefined

  // ─── Touch-to-dismiss (swipe down) ───
  const handleTouchStart = (e: React.TouchEvent) => {
    startYRef.current = e.touches[0].clientY
  }
  const handleTouchEnd = (e: React.TouchEvent) => {
    const dy = e.changedTouches[0].clientY - startYRef.current
    if (dy > 80) onClose()
  }

  // ─── Backdrop click closes on mobile ───
  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }

  // ─── Shared content pane ───
  const contentPane = (
    <div className="flex-1 overflow-hidden relative min-h-0">
      <div className={`absolute inset-0 transition-opacity duration-300 ${activeTab === 'brain' ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'}`}>
        <BrainPanel onNavigateDetail={(id) => handleNavigateDetail('brain', id)} sessionId={sessionId} refreshTrigger={refreshTrigger} focusTrigger={brainFocusTrigger} />
      </div>
      <div className={`absolute inset-0 transition-opacity duration-300 ${activeTab === 'knowledge' ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'}`}>
        <KIManager onNavigateDetail={(id) => handleNavigateDetail('knowledge', id)} />
      </div>
      <div className={`absolute inset-0 transition-opacity duration-300 ${activeTab === 'cron' ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'}`}>
        <CronManager onNavigateDetail={(id) => handleNavigateDetail('cron', id)} />
      </div>
      <div className={`absolute inset-0 transition-opacity duration-300 ${activeTab === 'skills' ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'}`}>
        <SkillMCPManager onNavigateDetail={(id) => handleNavigateDetail('skills', id)} />
      </div>
    </div>
  )

  // ─── Desktop: right sidebar (unchanged) ───
  const desktopPanel = (
    <div className="hidden md:flex md:relative md:w-[420px] shrink-0 h-full flex-col bg-black/40 backdrop-blur-[60px] border-l border-white/[0.06] shadow-[-30px_0_60px_-15px_rgba(0,0,0,0.6)] z-40"
         style={{ animation: 'slideInRight 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>

      {/* Header */}
      <div className={`transition-all duration-[400ms] ease-[cubic-bezier(0.4,0,0.2,1)] overflow-hidden shrink-0 ${isDrilledDown ? 'h-0 opacity-0' : 'h-[114px] opacity-100'}`}>
        <header className="px-5 pt-5 pb-4 border-b border-white/[0.04]">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold tracking-wide text-gray-200">Intelligence Hub</h2>
            <button onClick={onClose} className="p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-300 transition-colors group">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:rotate-90 transition-transform duration-300"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>
          <div className="flex bg-black/40 p-1 rounded-xl ring-1 ring-white/[0.05]">
            {TABS.map((t) => (
              <button key={t.id} onClick={() => onTabChange(t.id)}
                className={`flex-1 py-1.5 text-[11px] font-medium rounded-lg transition-all duration-300 active:scale-[0.98] ${
                  activeTab === t.id
                    ? 'bg-white/15 text-white shadow-[0_2px_10px_rgba(0,0,0,0.2)] ring-1 ring-white/10'
                    : 'text-gray-500 hover:text-gray-300 hover:bg-white/[0.04]'
                }`}>
                {t.label}
              </button>
            ))}
          </div>
        </header>
      </div>

      {contentPane}
    </div>
  )

  // ─── Mobile: full-screen overlay with bottom sheet feel ───
  const mobileSheet = (
    <div
      className="md:hidden fixed inset-0 z-50"
      style={{ animation: 'fadeIn 0.2s ease-out' }}
      onClick={handleBackdropClick}
    >
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" />

      {/* Sheet */}
      <div
        ref={sheetRef}
        className="absolute inset-x-0 bottom-0 flex flex-col bg-[#0d0d0f] border-t border-white/[0.08] rounded-t-2xl shadow-[0_-20px_60px_-10px_rgba(0,0,0,0.8)]"
        style={{ height: '88vh', animation: 'slideUp 0.32s cubic-bezier(0.4, 0, 0.2, 1)' }}
        onTouchStart={handleTouchStart}
        onTouchEnd={handleTouchEnd}
      >
        {/* Drag handle */}
        <div className="flex justify-center pt-3 pb-1 shrink-0">
          <div className="w-10 h-1 rounded-full bg-white/20" />
        </div>

        {/* Tab bar */}
        <div className="flex items-center shrink-0 px-4 pb-3 pt-1 border-b border-white/[0.06]">
          <div className="flex flex-1 bg-black/50 p-1 rounded-xl ring-1 ring-white/[0.06] gap-1">
            {TABS.map((t) => (
              <button key={t.id} onClick={() => onTabChange(t.id)}
                className={`flex-1 flex flex-col items-center py-1.5 rounded-lg transition-all duration-200 active:scale-[0.96] gap-0.5 ${
                  activeTab === t.id
                    ? 'bg-white/15 text-white ring-1 ring-white/10 shadow-sm'
                    : 'text-gray-500'
                }`}>
                <span className="text-base leading-none">{t.icon}</span>
                <span className="text-[10px] font-medium tracking-wide">{t.label}</span>
              </button>
            ))}
          </div>
          <button onClick={onClose} className="ml-3 p-2 rounded-full bg-white/[0.06] text-gray-400 active:bg-white/[0.12] transition-colors shrink-0">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M18 6L6 18M6 6l12 12"/></svg>
          </button>
        </div>

        {contentPane}
      </div>
    </div>
  )

  return (
    <>
      {desktopPanel}
      {mobileSheet}
      <style>{`
        @keyframes slideUp {
          from { transform: translateY(100%); }
          to   { transform: translateY(0); }
        }
        @keyframes fadeIn {
          from { opacity: 0; }
          to   { opacity: 1; }
        }
      `}</style>
    </>
  )
}
