/**
 * ApprovalBanner — floating banner for pending tool approval requests.
 * Extracted from App.tsx to keep the root component lean.
 */

import { memo } from 'react'
import { api } from '../chat/api'
import type { ApprovalRequest } from '../chat/types'

interface Props {
  pendingApprovals: ApprovalRequest[]
  setPendingApprovals: React.Dispatch<React.SetStateAction<ApprovalRequest[]>>
}

export const ApprovalBanner = memo(function ApprovalBanner({ pendingApprovals, setPendingApprovals }: Props) {
  if (pendingApprovals.length === 0) return null

  return (
    <div className="absolute top-14 sm:top-20 left-1/2 -translate-x-1/2 z-50 flex flex-col gap-2 w-full max-w-4xl px-2 sm:px-4 pointer-events-none">
      <div className="flex flex-col gap-2 w-full pointer-events-auto" style={{ animation: 'slideDown 0.3s cubic-bezier(0.4, 0, 0.2, 1)' }}>
        {pendingApprovals.map((req, idx) => (
          <div key={req.approvalId}
            className={`w-full rounded-2xl border border-amber-500/30 bg-black/60 backdrop-blur-[40px] px-5 py-4 flex flex-col gap-3 shadow-[0_20px_40px_-10px_rgba(0,0,0,0.8)] ${pendingApprovals.length > 1 && idx > 0 ? 'mt-2' : ''}`}
            style={{ animation: 'fadeInScale 0.25s cubic-bezier(0.4, 0, 0.2, 1)', animationDelay: `${idx * 0.1}s`, animationFillMode: 'backwards' }}>
          <div className="flex items-start gap-4">
            <span className="text-amber-400 text-xl mt-0.5 opacity-90 leading-none">⚠️</span>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-semibold tracking-wide text-amber-200">
                待审批操作 <span className="text-gray-500 mx-2">|</span> <code className="font-mono text-[11px] bg-amber-500/10 text-amber-300/90 px-2 py-0.5 rounded-md border border-amber-500/20">{req.toolName}</code>
              </div>
              {req.reason && (
                <div className="text-[13px] text-amber-100/60 mt-1.5 leading-relaxed">{req.reason}</div>
              )}
              {Object.keys(req.args).length > 0 && (
                <pre className="text-[11px] mt-2 text-gray-400 bg-black/40 rounded-lg px-3 py-2.5 overflow-auto max-h-32 font-mono ring-1 ring-white/5">
                  {JSON.stringify(req.args, null, 2)}
                </pre>
              )}
            </div>
          </div>
          <div className="flex gap-2 justify-end flex-wrap">
            <button
              onClick={async () => {
                await api.approve(req.approvalId, false)
                setPendingApprovals(prev => prev.filter(r => r.approvalId !== req.approvalId))
              }}
              className="px-4 py-1.5 rounded-lg text-sm font-medium bg-red-900/60 hover:bg-red-800/80 text-red-200 border border-red-700/40 transition-all hover:scale-105">
              拒绝
            </button>
            <button
              onClick={async () => {
                await api.approve(req.approvalId, true)
                setPendingApprovals(prev => prev.filter(r => r.approvalId !== req.approvalId))
              }}
              className="px-4 py-1.5 rounded-lg text-sm font-medium bg-emerald-900/60 hover:bg-emerald-800/80 text-emerald-200 border border-emerald-700/40 transition-all hover:scale-105">
              允许执行
            </button>
          </div>
        </div>
        ))}
        {pendingApprovals.length > 1 && (
          <div className="text-xs text-center text-amber-300/60 mt-1">
            {pendingApprovals.length} 个待审批操作
          </div>
        )}
      </div>
    </div>
  )
})
