/**
 * ScrollProvider — Unified scroll state + behavior center.
 *
 * Merges the former useChatScroll hook logic into the provider.
 * Single source of truth for:
 * - scrollContainerRef (DOM element)
 * - virtualizer-powered scrollToEnd (registered by ChatVirtualList)
 * - streaming auto-scroll state (isStreaming, autoScroll, userScrolledUp)
 * - imperative scroll actions (scrollToEnd, resetToBottom, enter/exitStreamingMode)
 *
 * This eliminates the architectural split where scroll behavior logic
 * (useChatScroll) was separated from the virtualizer (ChatVirtualList),
 * forcing broken raw scrollTop workarounds.
 */

import {
  createContext,
  useContext,
  useRef,
  useCallback,
  useEffect,
  type ReactNode,
  type MutableRefObject,
} from 'react'

type ScrollToEndFn = () => void

interface ScrollContextValue {
  /** Shared DOM ref for the scroll container */
  scrollContainerRef: MutableRefObject<HTMLDivElement | null>

  // ── Virtualizer registration ──
  registerScrollToEnd: (fn: ScrollToEndFn) => void
  unregisterScrollToEnd: () => void

  // ── Scroll actions ──
  /** Scroll to bottom using virtualizer (preferred) or raw fallback */
  scrollToEnd: () => void
  /** Reset all state and scroll to bottom (post history-load) */
  resetToBottom: () => void
  /** Check if currently near bottom (50px tolerance) */
  isAtBottom: () => boolean

  // ── Streaming lifecycle ──
  enterStreamingMode: () => void
  exitStreamingMode: () => void
  isStreamingRef: MutableRefObject<boolean>
  autoScrollRef: MutableRefObject<boolean>
  userScrolledUpRef: MutableRefObject<boolean>
}

const ScrollContext = createContext<ScrollContextValue | null>(null)

/** Legacy compat: access just the scroll container ref */
export function useScrollRef(): MutableRefObject<HTMLDivElement | null> {
  const ctx = useContext(ScrollContext)
  if (!ctx) throw new Error('useScrollRef must be used within ScrollProvider')
  return ctx.scrollContainerRef
}

/** Access the full scroll context */
export function useScrollContext(): ScrollContextValue {
  const ctx = useContext(ScrollContext)
  if (!ctx) throw new Error('useScrollContext must be used within ScrollProvider')
  return ctx
}

export function ScrollProvider({ children }: { children: ReactNode }) {
  const scrollContainerRef = useRef<HTMLDivElement | null>(null)
  const scrollToEndFnRef = useRef<ScrollToEndFn | null>(null)

  // ── Streaming state refs (no re-renders, just coordination flags) ──
  const isStreamingRef = useRef(false)
  const autoScrollRef = useRef(true)
  const userScrolledUpRef = useRef(false)

  // Track which DOM element has our scroll listener attached
  const attachedElRef = useRef<HTMLDivElement | null>(null)
  const listenerCleanupRef = useRef<(() => void) | null>(null)

  // ── Virtualizer registration ──
  const registerScrollToEnd = useCallback((fn: ScrollToEndFn) => {
    scrollToEndFnRef.current = fn
  }, [])

  const unregisterScrollToEnd = useCallback(() => {
    scrollToEndFnRef.current = null
  }, [])

  // ── Core scroll action ──
  const scrollToEnd = useCallback(() => {
    if (scrollToEndFnRef.current) {
      scrollToEndFnRef.current()
      return
    }
    // Fallback: raw scrollTop (only when virtualizer not mounted)
    const dom = scrollContainerRef.current
    if (dom) dom.scrollTop = dom.scrollHeight
  }, [])

  const isAtBottom = useCallback((): boolean => {
    const dom = scrollContainerRef.current
    if (!dom) return true
    const { scrollTop, scrollHeight, clientHeight } = dom
    return (scrollHeight - scrollTop - clientHeight) < 50
  }, [])

  // ── Reset: clear all flags + scroll to end with retry ──
  const resetToBottom = useCallback(() => {
    isStreamingRef.current = false
    autoScrollRef.current = true
    userScrolledUpRef.current = false

    // Retry loop: virtualizer needs multiple frames to estimate→measure→settle.
    // Try up to 5 times, 60ms apart, until we're actually at bottom.
    let attempts = 0
    const maxAttempts = 5
    const tryScroll = () => {
      scrollToEnd()
      attempts++
      if (attempts < maxAttempts) {
        requestAnimationFrame(() => {
          if (!isAtBottom()) {
            setTimeout(tryScroll, 60)
          }
        })
      }
    }
    // First attempt after one frame (let React commit DOM)
    requestAnimationFrame(tryScroll)
  }, [scrollToEnd, isAtBottom])

  // ── Streaming lifecycle ──
  const enterStreamingMode = useCallback(() => {
    isStreamingRef.current = true
    autoScrollRef.current = true
    userScrolledUpRef.current = false
  }, [])

  const exitStreamingMode = useCallback(() => {
    isStreamingRef.current = false
  }, [])

  // ── Scroll listener: detect user scroll-up during streaming ──
  // Re-attaches when the DOM element changes (e.g. session switch remounts ChatVirtualList).
  // Uses polling at reduced frequency instead of per-frame RAF.
  useEffect(() => {
    let cancelled = false
    let pollTimer: ReturnType<typeof setTimeout> | null = null

    const attachListener = (dom: HTMLDivElement) => {
      // Skip if already attached to this element
      if (attachedElRef.current === dom && listenerCleanupRef.current) return
      // Detach from previous
      listenerCleanupRef.current?.()
      attachedElRef.current = dom

      let ticking = false
      const onScroll = () => {
        if (ticking) return
        ticking = true
        requestAnimationFrame(() => {
          ticking = false
          const el = scrollContainerRef.current
          if (!el) return
          const { scrollTop, scrollHeight, clientHeight } = el
          const distanceFromBottom = scrollHeight - scrollTop - clientHeight
          const atBottom = distanceFromBottom < 50

          if (atBottom) {
            autoScrollRef.current = true
            userScrolledUpRef.current = false
          } else {
            // User is not at bottom — disable auto-scroll unconditionally.
            // This prevents ResizeObserver from force-scrolling back to bottom.
            autoScrollRef.current = false
            userScrolledUpRef.current = true
          }
        })
      }

      dom.addEventListener('scroll', onScroll, { passive: true })
      listenerCleanupRef.current = () => dom.removeEventListener('scroll', onScroll)
    }

    // Poll to detect element changes (e.g. session switch remounts)
    // Immediate first attempt, then 200ms polling for efficiency
    const poll = () => {
      if (cancelled) return
      const dom = scrollContainerRef.current
      if (dom) attachListener(dom)
      pollTimer = setTimeout(poll, 200)
    }
    // Immediate first check — no 200ms delay on mount
    const dom = scrollContainerRef.current
    if (dom) attachListener(dom)
    pollTimer = setTimeout(poll, 200)

    return () => {
      cancelled = true
      if (pollTimer) clearTimeout(pollTimer)
      listenerCleanupRef.current?.()
      attachedElRef.current = null
    }
  }, [])

  return (
    <ScrollContext.Provider value={{
      scrollContainerRef,
      registerScrollToEnd,
      unregisterScrollToEnd,
      scrollToEnd,
      resetToBottom,
      isAtBottom,
      enterStreamingMode,
      exitStreamingMode,
      isStreamingRef,
      autoScrollRef,
      userScrolledUpRef,
    }}>
      {children}
    </ScrollContext.Provider>
  )
}
