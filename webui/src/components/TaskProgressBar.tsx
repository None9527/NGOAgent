/**
 * TaskProgressBar — unified agent work status indicator above the composer.
 *
 * Replaces the inline TaskProgress JSX in App.tsx with a proper state-aware
 * component that covers all streamPhase × taskProgress combinations.
 *
 * Responsibility: show WHAT the agent is doing right now.
 * TopNavbar only shows network connection; this shows agent work state.
 */

import { memo } from 'react'

interface TaskProgress {
  taskName: string
  status: string
  summary: string
  mode: string // planning | execution | verification
}

interface TaskProgressBarProps {
  isStreaming: boolean
  taskProgress: TaskProgress | null
  isWaitingPlan: boolean
  isWaitingApproval: boolean
}

// Mode → visual config
const MODE_CONFIG: Record<string, { icon: string; label: string; dotColor: string; badgeBg: string; badgeText: string }> = {
  planning: {
    icon: '📐',
    label: 'PLANNING',
    dotColor: 'bg-blue-400',
    badgeBg: 'bg-blue-900/50',
    badgeText: 'text-blue-300',
  },
  execution: {
    icon: '⚡',
    label: 'EXECUTION',
    dotColor: 'bg-amber-400',
    badgeBg: 'bg-amber-900/50',
    badgeText: 'text-amber-300',
  },
  verification: {
    icon: '✅',
    label: 'VERIFICATION',
    dotColor: 'bg-emerald-400',
    badgeBg: 'bg-emerald-900/50',
    badgeText: 'text-emerald-300',
  },
}

export const TaskProgressBar = memo(function TaskProgressBar({
  isStreaming,
  taskProgress,
  isWaitingPlan,
  isWaitingApproval,
}: TaskProgressBarProps) {
  // ── Waiting states (highest priority) ──
  if (isWaitingPlan && taskProgress) {
    return (
      <Bar borderClass="border-blue-500/30 animate-border-breathe">
        <span className="text-base leading-none">📋</span>
        <span className="text-sm font-medium text-blue-200 truncate flex-1">
          等待审批 · {taskProgress.taskName}
        </span>
        <PulseDot color="bg-blue-400" breathing />
      </Bar>
    )
  }

  if (isWaitingApproval) {
    return (
      <Bar borderClass="border-amber-500/30 animate-border-breathe">
        <span className="text-base leading-none">⚠️</span>
        <span className="text-sm font-medium text-amber-200 truncate flex-1">
          等待授权
        </span>
        <PulseDot color="bg-amber-400" breathing />
      </Bar>
    )
  }

  // ── Active task with mode ──
  if (taskProgress) {
    const config = MODE_CONFIG[taskProgress.mode] || MODE_CONFIG.execution
    return (
      <Bar>
        <PulseDot color={config.dotColor} />
        <span className="text-base leading-none">{config.icon}</span>
        <span className="text-sm font-medium text-gray-200 truncate flex-1">
          {taskProgress.taskName}
        </span>
        <span className={`shrink-0 text-[10px] uppercase tracking-wider font-semibold px-1.5 py-0.5 rounded ${config.badgeBg} ${config.badgeText}`}>
          {config.label}
        </span>
        {taskProgress.status && (
          <span className="shrink-0 text-[11px] text-gray-500 hidden sm:block truncate max-w-[200px]">
            {taskProgress.status}
          </span>
        )}
      </Bar>
    )
  }

  // ── Streaming without task progress (thinking) ──
  if (isStreaming) {
    return (
      <Bar>
        <PulseDot color="bg-blue-400" />
        <span className="text-sm text-blue-300/80">Agent 正在思考...</span>
      </Bar>
    )
  }

  // ── Idle: don't render ──
  return null
})

// ── Subcomponents ──

function Bar({ children, borderClass = 'border-white/[0.08]' }: {
  children: React.ReactNode
  borderClass?: string
}) {
  return (
    <div className="px-1 mb-2">
      <div className={`w-full rounded-xl border ${borderClass} bg-[#1c1c1c] px-4 py-2.5 flex items-center gap-3 transition-all duration-200`}>
        {children}
      </div>
    </div>
  )
}

function PulseDot({ color, breathing = false }: { color: string; breathing?: boolean }) {
  return (
    <span className={`shrink-0 inline-block w-2 h-2 rounded-full ${color} ${breathing ? 'animate-breathe' : 'animate-pulse'}`} />
  )
}
