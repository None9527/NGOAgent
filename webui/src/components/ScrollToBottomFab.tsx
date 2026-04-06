/**
 * ScrollToBottomFab — floating action button to scroll back to conversation bottom.
 *
 * Shows when user scrolls away from bottom. Hides when at bottom.
 * Uses ScrollProvider refs to avoid unnecessary re-renders.
 */

import { useState, useEffect, useCallback } from 'react'
import { useScrollContext } from '../providers/ScrollProvider'

export function ScrollToBottomFab() {
  const { scrollContainerRef, scrollToEnd, autoScrollRef } = useScrollContext()
  const [visible, setVisible] = useState(false)

  // Poll scroll position via scroll event to toggle visibility
  useEffect(() => {
    const dom = scrollContainerRef.current
    if (!dom) return

    let ticking = false
    const check = () => {
      ticking = false
      const el = scrollContainerRef.current
      if (!el) return
      const { scrollTop, scrollHeight, clientHeight } = el
      const distFromBottom = scrollHeight - scrollTop - clientHeight
      // Show FAB when user is more than 300px from bottom
      setVisible(distFromBottom > 300)
    }

    const onScroll = () => {
      if (!ticking) {
        ticking = true
        requestAnimationFrame(check)
      }
    }

    dom.addEventListener('scroll', onScroll, { passive: true })
    return () => dom.removeEventListener('scroll', onScroll)
  }, [scrollContainerRef])

  const handleClick = useCallback(() => {
    autoScrollRef.current = true
    scrollToEnd()
    setVisible(false)
  }, [scrollToEnd, autoScrollRef])

  // Don't render when not visible — but use CSS transition for exit
  // We keep in DOM briefly for exit animation
  const [mounted, setMounted] = useState(false)
  useEffect(() => {
    if (visible) {
      setMounted(true)
    } else {
      const t = setTimeout(() => setMounted(false), 200)
      return () => clearTimeout(t)
    }
  }, [visible])

  if (!mounted) return null

  return (
    <button
      type="button"
      onClick={handleClick}
      aria-label="滚动到底部"
      className="scroll-to-bottom-fab"
      style={{
        opacity: visible ? 1 : 0,
        transform: visible ? 'translateY(0)' : 'translateY(12px)',
      }}
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </button>
  )
}
