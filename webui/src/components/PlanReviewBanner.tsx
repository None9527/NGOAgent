/**
 * PlanReviewBanner — floating banner for plan review approval.
 * Extracted from App.tsx to keep the root component lean.
 */

import { memo, useState, useCallback } from 'react'
import { useHub } from '../providers/HubProvider'

interface PlanReview {
  message: string
  paths: string[]
}

interface Props {
  planReview: PlanReview
  setPlanReview: React.Dispatch<React.SetStateAction<PlanReview | null>>
  onSend: (e?: React.FormEvent, overrideText?: string) => void
}

export const PlanReviewBanner = memo(function PlanReviewBanner({ planReview, setPlanReview, onSend }: Props) {
  const hub = useHub()
  const [feedbackInput, setFeedbackInput] = useState('')
  const [showFeedback, setShowFeedback] = useState(false)

  const handleSendFeedback = useCallback(() => {
    if (!feedbackInput.trim()) return
    const text = feedbackInput.trim()
    setShowFeedback(false)
    setPlanReview(null)
    setFeedbackInput('')
    onSend(undefined, text)
  }, [feedbackInput, setPlanReview, onSend])

  return (
    <div className="px-1 mb-2">
      <div className="w-full rounded-2xl border border-blue-500/30 bg-black/60 backdrop-blur-[40px] px-5 py-4 flex flex-col gap-3 shadow-[0_20px_40px_-10px_rgba(0,0,0,0.8)]"
        style={{ animation: 'slideDown 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        <div className="flex items-start gap-4">
          <span className="text-blue-400 text-xl mt-0.5 opacity-90 leading-none">📋</span>
          <div className="flex-1 min-w-0">
            <div className="text-sm font-semibold tracking-wide text-blue-200">计划执行审批</div>
            <div className="text-[13px] text-blue-100/60 mt-1.5 leading-relaxed">{planReview.message}</div>
            {planReview.paths.length > 0 && (
              <div className="text-[11px] text-gray-500 mt-2 font-mono flex flex-wrap gap-1">
                {planReview.paths.map(p => (
                  <span key={p} className="bg-black/40 px-2 py-0.5 rounded-md border border-white/5">{p.split('/').pop()}</span>
                ))}
              </div>
            )}
          </div>
        </div>

        {showFeedback && (
          <div className="flex gap-2 mt-1">
            <input
              type="text"
              value={feedbackInput}
              onChange={(e) => setFeedbackInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleSendFeedback() }}
              placeholder="输入修改意见..."
              className="flex-1 bg-black/30 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-gray-200 placeholder:text-gray-600 focus:outline-none focus:border-blue-500/40"
              autoFocus
            />
            <button
              onClick={handleSendFeedback}
              className="px-3 py-1.5 rounded-lg text-sm font-medium bg-blue-900/60 hover:bg-blue-800/80 text-blue-200 border border-blue-700/40 transition-colors"
            >
              发送
            </button>
          </div>
        )}

        <div className="flex gap-2 justify-end flex-wrap mt-2">
          <button
            onClick={() => {
              setPlanReview(null)
              setShowFeedback(false)
              onSend(undefined, 'rejected')
            }}
            className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-red-500/10 hover:bg-red-500/20 text-red-400 border border-red-500/30 transition-all hover:scale-105"
          >
            拒绝并关闭
          </button>
          <button
            onClick={() => {
              hub.openTab('brain')
              if (planReview.paths.length > 0) {
                hub.focusFile(planReview.paths[0].split('/').pop() || 'plan.md')
              }
            }}
            className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-white/5 hover:bg-white/10 text-gray-300 border border-white/10 transition-all hover:scale-105"
          >
            检视大纲
          </button>
          <button
            onClick={() => setShowFeedback(true)}
            className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-amber-500/10 hover:bg-amber-500/20 text-amber-300 border border-amber-500/30 transition-all hover:scale-105"
          >
            修改计划
          </button>
          <button
            onClick={() => {
              setPlanReview(null)
              setShowFeedback(false)
              onSend(undefined, 'approved')
            }}
            className="px-4 py-1.5 rounded-full text-[11px] font-medium tracking-wide bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 transition-all hover:scale-105"
          >
            批准执行
          </button>
        </div>
      </div>
    </div>
  )
})
