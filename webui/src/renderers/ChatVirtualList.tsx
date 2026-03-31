/**
 * ChatVirtualList — Headless virtual scroll for chat messages
 *
 * Uses @tanstack/react-virtual with FULL scroll ownership.
 *
 * KEY DESIGN:
 * - We OWN the scroll container (a plain div with overflow-y: scroll)
 * - We OWN height estimation (Pretext + measureElement)
 * - We OWN scroll behavior via virtualizer.scrollToIndex
 * - ScrollProvider is the capability registry: we register our
 *   virtualizer-powered scrollToEnd function so StreamProvider
 *   can call it without knowing about the virtualizer directly.
 *
 * SCROLL-TO-BOTTOM:
 * Uses virtualizer.scrollToIndex(lastIndex, { align: 'end' }) which
 * internally handles the estimate→measure→re-scroll feedback loop.
 *
 * MEDIA RESILIENCE:
 * A ResizeObserver on each virtual item detects async height changes
 * (image load, video load, code block expand, etc.) and re-measures
 * via virtualizer.measureElement. When autoScroll is active, height
 * increases automatically trigger scrollToEnd to maintain bottom lock.
 */

import {
  forwardRef,
  useImperativeHandle,
  useCallback,
  useEffect,
  useRef,
  memo,
} from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useScrollRef, useScrollContext } from '../providers/ScrollProvider'
import type { RenderItem } from '../chat/groupMessages'

export interface ChatVirtualListHandle {
  scrollToBottom: (behavior?: ScrollBehavior) => void
  scrollToTop: (behavior?: ScrollBehavior) => void
}

interface ChatVirtualListProps {
  items: RenderItem[]
  renderItem: (index: number, item: RenderItem) => React.ReactNode
  estimateSize: (index: number) => number
  composerHeight: number
  showDots: boolean
  overscan?: number
  className?: string
}

function getItemKey(item: RenderItem, index: number): string {
  if (item.type === 'tool_group') return item.id
  return item.data.uuid || `msg-${index}`
}

const ThinkingDots = memo(() => (
  <div className="flex items-center gap-[6px] pl-[10px] py-4">
    {[0, 1, 2].map(i => (
      <span
        key={i}
        className="w-[5px] h-[5px] rounded-full bg-white/30"
        style={{
          animation: 'thinkingBounce 1.6s ease-in-out infinite',
          animationDelay: `${i * 0.16}s`,
        }}
      />
    ))}
  </div>
))
ThinkingDots.displayName = 'ThinkingDots'

export const ChatVirtualList = forwardRef<ChatVirtualListHandle, ChatVirtualListProps>(
  ({ items, renderItem, estimateSize, composerHeight, showDots, overscan = 10, className }, ref) => {
    const scrollContainerRef = useScrollRef()
    const {
      registerScrollToEnd, unregisterScrollToEnd,
      scrollToEnd, autoScrollRef,
    } = useScrollContext()

    // Track current item count via ref for stable closure in registered function
    const countRef = useRef(items.length)
    countRef.current = items.length

    const virtualizer = useVirtualizer({
      count: items.length,
      getScrollElement: () => scrollContainerRef.current,
      estimateSize,
      overscan,
      getItemKey: (index) => getItemKey(items[index], index),
    })

    // Register virtualizer-powered scrollToEnd with ScrollProvider.
    useEffect(() => {
      registerScrollToEnd(() => {
        if (countRef.current > 0) {
          virtualizer.scrollToIndex(countRef.current - 1, { align: 'end' })
        }
      })
      return () => unregisterScrollToEnd()
    }, [virtualizer, registerScrollToEnd, unregisterScrollToEnd])

    useImperativeHandle(ref, () => ({
      scrollToBottom: () => {
        if (items.length > 0) {
          virtualizer.scrollToIndex(items.length - 1, { align: 'end' })
        }
      },
      scrollToTop: (behavior: ScrollBehavior = 'smooth') => {
        scrollContainerRef.current?.scrollTo({ top: 0, behavior })
      },
    }), [virtualizer, items.length, scrollContainerRef])

    const virtualItems = virtualizer.getVirtualItems()
    const totalSize = virtualizer.getTotalSize()

    // ── Media-resilient measurement ──
    // ResizeObserver on each virtual item detects async size changes
    // (image loads, code block expansion, lazy content, etc.)
    // and re-measures + maintains bottom lock if autoScroll is active.
    //
    // KEY: We track each element's last known height in a WeakMap.
    // ResizeObserver fires immediately on observe() — we record the initial
    // height but do NOT trigger scrollToEnd. Only SUBSEQUENT changes (e.g.
    // image load completes) trigger scroll compensation.
    const resizeObserverRef = useRef<ResizeObserver | null>(null)
    const observedElementsRef = useRef<Set<HTMLElement>>(new Set())
    const knownHeightsRef = useRef<WeakMap<Element, number>>(new WeakMap())
    const compensateRafRef = useRef(0)

    useEffect(() => {
      const knownHeights = knownHeightsRef.current
      const ro = new ResizeObserver((entries) => {
        let heightChanged = false
        for (const entry of entries) {
          const el = entry.target as HTMLElement
          const newHeight = entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height
          const prevHeight = knownHeights.get(el)

          // Record current height
          knownHeights.set(el, newHeight)

          if (prevHeight === undefined) {
            // First observation (mount) — record but don't trigger scroll
            continue
          }

          if (Math.abs(newHeight - prevHeight) > 2) {
            // Actual height change (image load, content expand, etc.)
            virtualizer.measureElement(el)
            heightChanged = true
          }
        }

        // Only compensate scroll for real height changes, not mount events
        if (heightChanged && autoScrollRef.current) {
          if (compensateRafRef.current) cancelAnimationFrame(compensateRafRef.current)
          compensateRafRef.current = requestAnimationFrame(() => {
            compensateRafRef.current = 0
            scrollToEnd()
          })
        }
      })
      resizeObserverRef.current = ro

      return () => {
        ro.disconnect()
        resizeObserverRef.current = null
        observedElementsRef.current.clear()
        if (compensateRafRef.current) cancelAnimationFrame(compensateRafRef.current)
      }
    }, [virtualizer, autoScrollRef, scrollToEnd])

    // Measure + observe callback: initial measurement AND ongoing resize tracking
    const measureRef = useCallback(
      (node: HTMLElement | null) => {
        if (!node) return
        // Initial measurement for virtualizer
        virtualizer.measureElement(node)
        // Start observing for async size changes (image load, etc.)
        const ro = resizeObserverRef.current
        if (ro && !observedElementsRef.current.has(node)) {
          observedElementsRef.current.add(node)
          ro.observe(node)
        }
      },
      [virtualizer],
    )

    // Cleanup observed elements that are no longer in the DOM (virtual item recycling)
    useEffect(() => {
      const ro = resizeObserverRef.current
      if (!ro) return
      const observed = observedElementsRef.current
      const toRemove: HTMLElement[] = []
      for (const el of observed) {
        if (!el.isConnected) {
          ro.unobserve(el)
          toRemove.push(el)
        }
      }
      for (const el of toRemove) observed.delete(el)
    }) // Run after every render — virtual items may have been recycled

    return (
      <div
        ref={scrollContainerRef}
        className={className}
        style={{
          height: '100%',
          overflowY: 'auto',
          scrollbarGutter: 'stable',
        }}
      >
        {/* Reading column container */}
        <div className="w-full max-w-4xl mx-auto pt-10 md:pt-20 px-1 md:px-4">
          {/* Virtual content wrapper with total height */}
          <div style={{ height: totalSize, width: '100%', position: 'relative' }}>
            {virtualItems.map((vRow) => (
              <div
                key={getItemKey(items[vRow.index], vRow.index)}
                data-index={vRow.index}
                ref={measureRef}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${vRow.start}px)`,
                  willChange: 'transform',
                  contain: 'layout style paint',
                }}
              >
                {renderItem(vRow.index, items[vRow.index])}
              </div>
            ))}
          </div>

          {/* Thinking dots — outside virtual list */}
          {showDots && <ThinkingDots />}

          {/* Floating composer spacer */}
          <div
            style={{ height: composerHeight }}
            className="pointer-events-none"
            aria-hidden="true"
          />
        </div>
      </div>
    )
  },
)

ChatVirtualList.displayName = 'ChatVirtualList'
