import React, { useState, useRef, useEffect } from 'react';

export interface TopNavbarProps {
  onToggleSidebar: () => void
  modelName: string
  onToggleHub: () => void
  isHubOpen: boolean
  connectionState?: 'connected' | 'reconnecting' | 'disconnected'
  isStreaming?: boolean
  subagentStats?: { running: number; total: number } | null
  availableModels?: string[]
  currentModel?: string
  onModelSelect?: (modelName: string) => void
  onOpenSettings?: () => void
}

export const TopNavbar: React.FC<TopNavbarProps> = ({ 
  onToggleSidebar, 
  modelName, 
  onToggleHub, 
  isHubOpen,
  connectionState = 'connected',
  isStreaming = false,
  subagentStats = null,
  availableModels = [],
  currentModel = '',
  onModelSelect,
  onOpenSettings,
}) => {
  const [showDropdown, setShowDropdown] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

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
    <header className="absolute top-0 left-0 right-0 h-14 md:h-16 z-10 flex items-center px-3 md:px-4 bg-black/30 backdrop-blur-xl justify-between border-b border-white/5">
      <div className="flex items-center gap-1 md:gap-2">
        <button 
          onClick={onToggleSidebar}
          className="p-2 md:-ml-2 rounded-lg hover:bg-[#1a1a1a] dark:hover:bg-[#242424] text-gray-600 dark:text-gray-400 transition-all duration-200 hover:scale-105"
        >
          <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className="w-5 h-5" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><line x1="9" y1="3" x2="9" y2="21"></line></svg>
        </button>
        <div 
          ref={dropdownRef}
          onClick={handleModelClick}
          className="flex items-center gap-1 group cursor-pointer hover:bg-white/5 px-2 py-1.5 rounded-lg transition-all duration-200 relative"
        >
          <span className="text-lg md:text-xl font-medium text-gray-200">{modelName || 'NGOAgent'}</span>
          <svg stroke="currentColor" fill="none" strokeWidth="2" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className={`w-4 h-4 text-gray-500 group-hover:text-gray-400 transition-colors transition-transform duration-200 ${showDropdown ? 'rotate-180' : ''}`} xmlns="http://www.w3.org/2000/svg"><polyline points="6 9 12 15 18 9"></polyline></svg>
          
          {showDropdown && (
            <div className="absolute top-full left-0 mt-1 bg-[#1a1a1a] border border-white/10 rounded-lg shadow-xl min-w-[200px] z-50">
              <div className="py-1 max-h-64 overflow-y-auto">
                <div className="px-3 py-2 text-xs text-gray-500 border-b border-white/5">选择模型</div>
                {availableModels.length > 0 ? (
                  availableModels.map((model) => (
                    <button
                      key={model}
                      onClick={() => handleSelectModel(model)}
                      className={`w-full text-left px-3 py-2 text-sm hover:bg-white/10 transition-colors flex items-center justify-between ${
                        model === currentModel ? 'text-blue-400 bg-blue-500/10' : 'text-gray-300'
                      }`}
                    >
                      <span>{model}</span>
                      {model === currentModel && (
                        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
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
        {/* Unified status indicator — always visible */}
        {(() => {
          if (connectionState === 'disconnected') {
            return (
              <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-red-500/10 border border-red-500/20">
                <span className="w-1.5 h-1.5 rounded-full bg-red-400" />
                <span className="text-[11px] font-medium text-red-400/90 tracking-wide">Offline</span>
              </div>
            )
          }
          if (connectionState === 'reconnecting') {
            return (
              <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-amber-500/10 border border-amber-500/20 animate-in fade-in">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
                <span className="text-[11px] font-medium text-amber-400/90 tracking-wide">Reconnecting</span>
              </div>
            )
          }
          // Subagent progress takes priority over Thinking
          if (subagentStats && subagentStats.running > 0) {
            return (
              <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-blue-500/8 border border-blue-500/15">
                <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                <span className="text-[11px] font-medium text-blue-400/80 tracking-wide">
                  子任务 {subagentStats.total - subagentStats.running}/{subagentStats.total}
                </span>
              </div>
            )
          }
          if (isStreaming) {
            return (
              <div className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-blue-500/8 border border-blue-500/15">
                <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                <span className="text-[11px] font-medium text-blue-400/80 tracking-wide">Thinking</span>
              </div>
            )
          }
          return (
            <div className="flex items-center gap-1.5 px-1.5 py-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400/80" />
            </div>
          )
        })()}
      </div>
      
      <div className="flex items-center gap-2">
        {onToggleHub && (
          <button 
          onClick={onToggleHub}
          className={`flex items-center justify-center w-8 h-8 rounded-full border transition-all ${
            isHubOpen 
              ? 'bg-blue-600/30 border-blue-500/50 text-blue-400 shadow-[0_0_15px_rgba(37,99,235,0.3)]' 
              : 'bg-white/5 border-white/10 text-gray-400 hover:text-white hover:bg-white/10'
          }`}
          title="Intelligence Hub"
        >
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><path d="M4 6h16M4 12h16m-7 6h7"/></svg>
        </button>
        )}
        <button
          onClick={onOpenSettings}
          title="Settings"
          className="w-9 h-9 rounded-full border border-white/10 flex items-center justify-center text-gray-400 hover:bg-white/10 hover:text-white cursor-pointer transition-all duration-200 hover:scale-105"
        >
          <svg stroke="currentColor" fill="none" strokeWidth="1.5" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
        </button>
      </div>
    </header>
  );
};
