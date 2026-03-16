import React, { useState } from 'react'
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

export const IntelligenceHub: React.FC<IntelligenceHubProps> = ({
  sessionId,
  activeTab,
  onTabChange,
  onClose,
  refreshTrigger,
  brainFocusTrigger
}) => {
  // Track drill-down state for each tab independently
  const [detailViews, setDetailViews] = useState<Record<string, string | null>>({})

  const handleNavigateDetail = (tab: string, id: string | null) => {
    setDetailViews(prev => ({ ...prev, [tab]: id }))
  }

  const isDrilledDown = detailViews[activeTab] !== null && detailViews[activeTab] !== undefined

  // Map tabs to human-friendly names
  const labels: Record<string, string> = {
    brain: 'Brain', knowledge: 'Knowledge', cron: 'Cron', skills: 'Skills & MCP'
  }

  return (
    <div className="absolute inset-y-0 right-0 w-[85vw] max-w-[300px] sm:max-w-none sm:w-[340px] md:relative md:w-[420px] shrink-0 h-full bg-black/40 backdrop-blur-[60px] md:border-l border-white/[0.06] shadow-[-30px_0_60px_-15px_rgba(0,0,0,0.6)] z-50 md:z-40 flex flex-col"
         style={{ animation: 'slideInRight 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>         
      {/* Segmented Control Header - Hides when drilled down */}
      <div className={`transition-all duration-[400ms] ease-[cubic-bezier(0.4,0,0.2,1)] overflow-hidden shrink-0 ${isDrilledDown ? 'h-0 opacity-0 mb-0' : 'h-[92px] md:h-[114px] opacity-100 mb-0'}`}>
        <header className="px-3 md:px-5 pt-3 md:pt-5 pb-2 md:pb-4 border-b border-white/[0.04]">
          <div className="flex items-center justify-between mb-2 md:mb-4">
            <h2 className="text-base md:text-lg font-semibold tracking-wide text-gray-200">Intelligence Hub</h2>
            <button onClick={onClose} className="p-1 md:p-1.5 rounded-full hover:bg-white/10 text-gray-500 hover:text-gray-300 transition-colors group">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" className="group-hover:rotate-90 transition-transform duration-300"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>
          
          {/* iOS-style Segmented Control */}
          <div className="flex bg-black/40 p-1 rounded-xl ring-1 ring-white/[0.05]">
            {(['brain', 'knowledge', 'cron', 'skills'] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => onTabChange(tab)}
                className={`flex-1 py-1 md:py-1.5 text-[10px] md:text-[11px] font-medium rounded-lg transition-all duration-300 active:scale-[0.98] ${
                  activeTab === tab 
                    ? 'bg-white/15 text-white shadow-[0_2px_10px_rgba(0,0,0,0.2)] ring-1 ring-white/10' 
                    : 'text-gray-500 hover:text-gray-300 hover:bg-white/[0.04]'
                }`}
              >
                {labels[tab]}
              </button>
            ))}
          </div>
        </header>
      </div>

      {/* Embedded Content Area (Mounted simultaneously to persist UI states) */}
      <div className="flex-1 overflow-hidden relative">
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
    </div>
  )
}
