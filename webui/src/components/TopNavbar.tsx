import React, { useState, useRef, useEffect } from 'react';

type TaskProgress = {
  taskName: string
  status: string
  summary: string
  mode: string
}

export interface TopNavbarProps {
  onToggleSidebar: () => void
  modelName: string
  onToggleHub: () => void
  isHubOpen: boolean
  connectionState?: 'connected' | 'reconnecting' | 'disconnected'
  availableModels?: string[]
  currentModel?: string
  onModelSelect?: (modelName: string) => void
  onOpenSettings?: () => void
  planMode?: string
  streamPhase?: string
  taskProgress?: TaskProgress | null
}

// ── Status Indicator ─────────────────────────────────────────
// Only shows network connection state — agent work status is in TaskProgressBar
function StatusIndicator({ connectionState }: { connectionState: string }) {
  const status = connectionState === 'disconnected'
    ? { label: 'OFFLINE', dot: 'bg-red-400', shell: 'border-red-400/20 text-red-200 bg-red-500/10' }
    : connectionState === 'reconnecting'
    ? { label: 'RECONNECT', dot: 'bg-amber-300 animate-pulse', shell: 'border-amber-300/20 text-amber-100 bg-amber-500/10' }
    : { label: 'LIVE', dot: 'bg-emerald-300', shell: 'border-emerald-300/15 text-emerald-100 bg-emerald-500/10' }

  return (
    <div className={`inline-flex h-7 items-center gap-2 rounded-lg border px-2.5 text-[11px] font-semibold ${status.shell}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${status.dot}`} />
      <span className="hidden sm:inline">{status.label}</span>
    </div>
  )
}

function getKernelStage(streamPhase?: string, taskProgress?: TaskProgress | null) {
  if (taskProgress?.mode) return taskProgress.mode.toUpperCase()
  if (streamPhase === 'auto_waking') return 'AUTO WAKE'
  if (streamPhase === 'streaming') return 'RUNNING'
  if (streamPhase && streamPhase !== 'idle') return streamPhase.toUpperCase()
  return 'READY'
}

function shortModelName(modelName: string) {
  return modelName.replace(/^models\//, '') || 'NGOAgent'
}

export const TopNavbar: React.FC<TopNavbarProps> = ({ 
  onToggleSidebar, 
  modelName, 
  onToggleHub, 
  isHubOpen,
  connectionState = 'connected',
  availableModels = [],
  currentModel = '',
  onModelSelect,
  onOpenSettings,
  planMode = 'auto',
  streamPhase = 'idle',
  taskProgress = null,
}) => {
  const [showDropdown, setShowDropdown] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const kernelStage = getKernelStage(streamPhase, taskProgress)
  const displayModel = shortModelName(modelName || currentModel)

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const handleModelClick = () => {
    setShowDropdown(!showDropdown)
  }

  const handleSelectModel = (model: string) => {
    onModelSelect?.(model)
    setShowDropdown(false)
  }

  return (
    <header className="top-navbar absolute top-0 left-0 right-0 h-14 md:h-16 z-10 flex items-center px-3 md:px-4 justify-between">
      <div className="flex min-w-0 items-center gap-2">
        <button 
          onClick={onToggleSidebar}
          className="control-button p-2 md:-ml-2"
          aria-label="Toggle sidebar"
        >
          <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className="w-5 h-5" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><line x1="9" y1="3" x2="9" y2="21"></line></svg>
        </button>
        <div 
          ref={dropdownRef}
          onClick={handleModelClick}
          className="kernel-model-switch group relative flex min-w-0 cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 transition-all duration-200"
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              handleModelClick()
            }
          }}
        >
          <span className="hidden h-7 w-[3px] rounded-sm bg-cyan-300/80 md:block" />
          <span className="min-w-0">
            <span className="block truncate text-sm font-semibold text-white md:text-[15px]">{displayModel}</span>
            <span className="hidden text-[10px] font-semibold uppercase text-white/40 md:block">model runtime</span>
          </span>
          <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className={`h-4 w-4 shrink-0 text-white/45 transition-transform duration-200 ${showDropdown ? 'rotate-180' : ''}`} xmlns="http://www.w3.org/2000/svg"><polyline points="6 9 12 15 18 9"></polyline></svg>
          
          {showDropdown && (
            <div className="model-menu absolute top-full left-0 z-50 mt-2 min-w-[260px] rounded-lg border border-white/10">
              <div className="py-1 max-h-64 overflow-y-auto">
                <div className="px-3 py-2 text-[11px] font-semibold uppercase text-white/45 border-b border-white/5">选择模型</div>
                {availableModels.length > 0 ? (
                  availableModels.map((model) => (
                    <button
                      key={model}
                      onClick={(e) => {
                        e.stopPropagation()
                        handleSelectModel(model)
                      }}
                      className={`w-full text-left px-3 py-2.5 text-sm transition-colors flex items-center justify-between gap-4 ${
                        model === currentModel ? 'text-cyan-100 bg-cyan-400/10' : 'text-gray-300 hover:bg-white/[0.06]'
                      }`}
                    >
                      <span className="truncate">{shortModelName(model)}</span>
                      {model === currentModel && (
                        <svg className="w-4 h-4 shrink-0 text-cyan-200" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
                        </svg>
                      )}
                    </button>
                  ))
                ) : (
                  <div className="text-xs text-gray-400 px-3 py-2">加载中...</div>
                )}
              </div>
            </div>
          )}
        </div>
        <StatusIndicator connectionState={connectionState} />
        <div className="hidden items-center gap-2 rounded-lg border border-white/[0.07] bg-white/[0.035] px-2.5 py-1.5 text-[11px] font-semibold uppercase text-white/55 lg:flex">
          <span className="text-white/35">mode</span>
          <span className="text-emerald-200">{planMode}</span>
          <span className="h-3 w-px bg-white/10" />
          <span className="text-white/35">stage</span>
          <span className="text-amber-100">{kernelStage}</span>
        </div>
      </div>
      
      <div className="flex items-center gap-2">
        {onToggleHub && (
          <button 
          onClick={onToggleHub}
          className={`control-button flex h-9 items-center justify-center rounded-lg border px-3 text-xs font-semibold transition-all ${
            isHubOpen 
              ? 'bg-cyan-400/15 border-cyan-300/35 text-cyan-100' 
              : 'bg-white/5 border-white/10 text-gray-400 hover:text-white hover:bg-white/10'
          }`}
          title="Intelligence Hub"
        >
          <span className="hidden sm:inline">Hub</span>
          <svg className="sm:hidden" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M4 6h16M4 12h16m-7 6h7"/></svg>
        </button>
        )}
        <button
          onClick={onOpenSettings}
          title="Settings"
          className="control-button w-9 h-9 rounded-lg border border-white/10 flex items-center justify-center text-gray-400 hover:bg-white/10 hover:text-white cursor-pointer transition-all duration-200"
        >
          <svg stroke="currentColor" fill="none" strokeWidth="1.5" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
        </button>
      </div>
    </header>
  );
};
