/**
 * ToolGroupPanel — Collapsible "Progress Updates" panel
 *
 * Groups consecutive tool calls into a bordered panel with:
 * - Header: "Progress Updates" + Expand all / Collapse all toggle
 * - Numbered step rows with label + title (truncated)
 * - Per-step expand/collapse for detailed content
 * - Max 50vh height with internal scrolling
 */

import { useState, useMemo, memo } from 'react'
import type { FC } from 'react'
import type { ChatMessageData } from '../../chat/types'
import { getToolRenderer } from './toolRegistry.js'
import { shouldShowToolCall } from './index.js'
import type { ToolCallData } from './index.js'
import './ToolGroupPanel.css'

// ── Chevron icon ──
const ChevronIcon: FC<{ className?: string }> = ({ className }) => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className}
  >
    <polyline points="6 9 12 15 18 9" />
  </svg>
)

// ── Single step row ──
interface StepRowProps {
  index: number
  msg: ChatMessageData
  isExpanded: boolean
  onToggle: () => void
  sessionId?: string
}

/** Get a human-readable label from tool call kind */
function getStepLabel(kind: string): string {
  const k = kind.toLowerCase()
  if (k === 'bash' || k === 'execute') return 'Bash'
  if (k === 'read' || k === 'view_file') return 'Read'
  if (k === 'write' || k === 'create_file') return 'Write'
  if (k === 'edit' || k === 'replace') return 'Edit'
  if (k === 'search' || k === 'grep') return 'Search'
  if (k === 'web_fetch') return 'WebFetch'
  if (k === 'web_search') return 'WebSearch'
  if (k === 'spawn_agent') return 'SubAgent'
  if (k === 'think') return 'Think'
  if (k === 'save_memory') return 'Memory'
  if (k === 'updated_plan') return 'Plan'
  return kind.charAt(0).toUpperCase() + kind.slice(1)
}

/** Get a short title string from tool call */
function getStepTitle(tc: ToolCallData): string {
  if (typeof tc.title === 'string') return tc.title
  if (tc.title && typeof tc.title === 'object') {
    const t = tc.title as Record<string, unknown>
    return (t.description as string) || (t.command as string) || (t.path as string) || ''
  }
  return ''
}

/** Status bullet color */
function getStatusColor(tc: ToolCallData): string {
  if (tc.status === 'failed') return 'var(--tgp-status-error)'
  if (tc.status === 'in_progress' || tc.status === 'pending') return 'var(--tgp-status-loading)'
  return 'var(--tgp-status-success)'
}

const StepRow: FC<StepRowProps> = memo(({
  index,
  msg,
  isExpanded,
  onToggle,
  sessionId,
}) => {
  const tc = msg.toolCall!
  const label = getStepLabel(tc.kind)
  const title = getStepTitle(tc)
  const isLoading = tc.status === 'in_progress' || tc.status === 'pending'

  // Get the full renderer for expanded view
  const config = getToolRenderer(tc)
  const ToolCallComponent = config?.component

  return (
    <div className="tgp-step" data-expanded={isExpanded}>
      {/* Collapsed header row */}
      <div className="tgp-step-header group" onClick={onToggle}>
        <span className="tgp-step-index">{index + 1}</span>

        {/* Status bullet / hover chevron */}
        <span className="tgp-step-icon">
          <span
            className="tgp-step-bullet"
            style={{ backgroundColor: getStatusColor(tc) }}
          />
          <span className="tgp-step-chevron">
            <ChevronIcon
              className={`tgp-chevron-icon ${isExpanded ? 'tgp-chevron-open' : ''}`}
            />
          </span>
        </span>

        <span className="tgp-step-label">{label}</span>
        <span className="tgp-step-title">{title}</span>

        {isLoading && (
          <span className="tgp-step-spinner" />
        )}
      </div>

      {/* Expanded content */}
      {isExpanded && ToolCallComponent && (
        <div className="tgp-step-content">
          <ToolCallComponent
            toolCall={tc}
            isFirst={false}
            isLast={false}
            sessionId={sessionId}
          />
        </div>
      )}
    </div>
  )
})
StepRow.displayName = 'StepRow'

// ── Main panel ──
export interface ToolGroupPanelProps {
  items: ChatMessageData[]
  sessionId?: string
  /** Section task name from task_boundary (e.g. "Planning Authentication") */
  sectionTitle?: string
  /** Section mode from task_boundary (planning | execution | verification) */
  sectionMode?: string
}

/** Mode badge color */
function getModeBadgeClass(mode: string): string {
  switch (mode) {
    case 'planning': return 'tgp-badge-planning'
    case 'verification': return 'tgp-badge-verification'
    case 'execution': return 'tgp-badge-execution'
    default: return ''
  }
}

export const ToolGroupPanel: FC<ToolGroupPanelProps> = memo(({
  items,
  sessionId,
  sectionTitle,
  sectionMode,
}) => {
  const [allExpanded, setAllExpanded] = useState(false)
  const [expandedSet, setExpandedSet] = useState<Set<number>>(new Set())

  // Filter to visible tool calls only
  const visibleItems = useMemo(
    () => items.filter((m) => m.toolCall && shouldShowToolCall(m.toolCall.kind, m.toolCall)),
    [items],
  )

  if (visibleItems.length === 0) return null

  // Single tool call → don't wrap in panel, render directly
  if (visibleItems.length === 1 && !sectionTitle) {
    const tc = visibleItems[0].toolCall!
    const config = getToolRenderer(tc)
    if (!config) return null
    const ToolCallComponent = config.component
    return (
      <ToolCallComponent
        toolCall={tc}
        isFirst={true}
        isLast={true}
        sessionId={sessionId}
      />
    )
  }

  // Force Virtuoso to recalculate item heights after expand/collapse
  const notifyResize = () => {
    requestAnimationFrame(() => window.dispatchEvent(new Event('resize')))
  }

  const isStepExpanded = (idx: number) => allExpanded || expandedSet.has(idx)

  const toggleStep = (idx: number) => {
    setExpandedSet((prev) => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
    notifyResize()
  }

  const toggleAll = () => {
    if (allExpanded) {
      setAllExpanded(false)
      setExpandedSet(new Set())
    } else {
      setAllExpanded(true)
    }
    notifyResize()
  }

  const headerLabel = sectionTitle || 'Progress Updates'

  return (
    <div className="tgp-panel">
      {/* Header */}
      <div className="tgp-header">
        <span className="tgp-header-label">
          {headerLabel}
          {sectionMode && (
            <span className={`tgp-mode-badge ${getModeBadgeClass(sectionMode)}`}>
              {sectionMode.toUpperCase()}
            </span>
          )}
        </span>
        <button
          type="button"
          className="tgp-header-toggle"
          onClick={toggleAll}
        >
          {allExpanded ? 'Collapse all' : 'Expand all'}
          <ChevronIcon
            className={`tgp-header-chevron ${allExpanded ? 'tgp-chevron-open' : ''}`}
          />
        </button>
      </div>

      {/* Steps list */}
      <div className="tgp-steps-container">
        {visibleItems.map((msg, idx) => (
          <StepRow
            key={msg.uuid || `step-${idx}`}
            index={idx}
            msg={msg}
            isExpanded={isStepExpanded(idx)}
            onToggle={() => toggleStep(idx)}
            sessionId={sessionId}
          />
        ))}
      </div>
    </div>
  )
})
ToolGroupPanel.displayName = 'ToolGroupPanel'
