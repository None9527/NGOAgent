/**
 * SubagentDock — sub-agent progress indicator.
 * Clean design: animated dots, expandable detail, subtle styling.
 */

import { useState, useEffect, useRef } from 'react'
import { useStream } from '../providers/StreamProvider'
import type { SubagentProgressEntry } from '../providers/StreamProvider'

export function SubagentDock() {
  const { subagentProgress } = useStream()
  const [isExpanded, setIsExpanded] = useState(false)
  const [visible, setVisible] = useState(false)
  const dismissTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const hasEntries = subagentProgress.length > 0
  const runningCount = subagentProgress.filter(e => e.status === 'running').length
  const completedCount = subagentProgress.filter(e => e.status === 'completed').length
  const failedCount = subagentProgress.filter(e => e.status === 'failed').length
  const allDone = hasEntries && runningCount === 0

  useEffect(() => {
    if (hasEntries && !allDone) {
      setVisible(true)
      if (dismissTimer.current) { clearTimeout(dismissTimer.current); dismissTimer.current = null }
    } else if (hasEntries && allDone) {
      setVisible(true)
      if (!dismissTimer.current) {
        dismissTimer.current = setTimeout(() => { setVisible(false); dismissTimer.current = null }, 4000)
      }
    } else {
      setVisible(false)
    }
    return () => { if (dismissTimer.current) { clearTimeout(dismissTimer.current); dismissTimer.current = null } }
  }, [hasEntries, allDone])

  if (!visible || !hasEntries) return null

  const active = subagentProgress.find(e => e.status === 'running')

  return (
    <div className="w-full max-w-xl mx-auto mb-2"
      style={{ animation: 'dockFadeIn 0.25s ease-out' }}>
      <style>{`
        @keyframes dockFadeIn {
          from { opacity: 0; transform: translateY(6px); }
          to { opacity: 1; transform: translateY(0); }
        }
        @keyframes dotWave {
          0%, 100% { transform: scale(1); opacity: 0.6; }
          50% { transform: scale(1.3); opacity: 1; }
        }
      `}</style>

      <div className="rounded-xl border border-white/[0.08] bg-white/[0.02] backdrop-blur-sm overflow-hidden">
        {/* Compact bar */}
        <button type="button" onClick={() => setIsExpanded(!isExpanded)}
          className="w-full flex items-center gap-2.5 px-3.5 py-2 text-left cursor-pointer hover:bg-white/[0.02] transition-colors">
          {/* Animated dots */}
          <span className="flex items-center gap-1">
            {subagentProgress.map((entry, i) => (
              <span key={entry.runID}
                className={`w-2 h-2 rounded-full transition-all duration-500 ${
                  entry.status === 'completed' ? 'bg-gray-500' :
                  entry.status === 'failed' ? 'bg-red-400' :
                  'bg-blue-400'
                }`}
                style={entry.status === 'running' ? {
                  animation: `dotWave 1.4s ease-in-out ${i * 0.15}s infinite`,
                } : {}}
              />
            ))}
          </span>

          {/* Count */}
          <span className="text-[11px] text-gray-400 tabular-nums">
            {completedCount}/{subagentProgress.length}
            {failedCount > 0 && <span className="text-red-400/70 ml-1">({failedCount} err)</span>}
          </span>

          {/* Current step (compact) */}
          {!isExpanded && active?.currentStep && (
            <span className="text-[10px] text-gray-500 truncate max-w-[180px]">
              → {active.currentStep}
            </span>
          )}

          {/* Expand arrow */}
          <span className="ml-auto text-[9px] text-gray-600 transition-transform duration-200" style={{
            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
          }}>▼</span>
        </button>

        {/* Expanded detail */}
        {isExpanded && (
          <div className="px-3.5 pb-2.5 flex flex-col gap-1 border-t border-white/[0.05] pt-2">
            {subagentProgress.map((entry) => (
              <div key={entry.runID} className="flex items-center gap-2 py-0.5 text-[11px]">
                <span className="w-3.5 text-center flex-shrink-0">
                  {entry.status === 'completed' ? '✓' :
                   entry.status === 'failed' ? '✕' : '◦'}
                </span>
                <span className={`flex-1 truncate ${
                  entry.status === 'running' ? 'text-gray-300' : 'text-gray-500'
                }`}>{entry.taskName}</span>
                {entry.status === 'running' && entry.currentStep && (
                  <span className="text-[10px] text-gray-600 truncate max-w-[120px]">
                    {entry.currentStep}
                  </span>
                )}
                {entry.status === 'failed' && entry.error && (
                  <span className="text-[10px] text-red-400/60 truncate max-w-[120px]">
                    {entry.error}
                  </span>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
