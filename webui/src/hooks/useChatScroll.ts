import { useRef, useCallback, useEffect } from 'react';

/**
 * ┌──────────────────────────────────────────────────────────────┐
 * │  useChatScroll — Production-grade sticky auto-scroll hook    │
 * └──────────────────────────────────────────────────────────────┘
 *
 * Scroll Strategy:
 * 1. Track whether user is "at bottom" via onScroll (150px threshold).
 * 2. MutationObserver watches entire subtree for DOM changes.
 * 3. On mutation: if user was at bottom → snap to bottom via scrollTop.
 * 4. RAF uses cancel-and-reschedule — every mutation cancels the previous
 *    pending RAF and queues a fresh one, guaranteeing we always read the
 *    LATEST scrollHeight (critical during rapid token streaming).
 * 5. scrollToBottom() for explicit triggers (user sends message).
 * 6. resetToBottom() forces scroll + resets isAtBottomRef — MUST be called
 *    on session switch / history load so sticky mode is re-engaged.
 */
export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Whether the user is currently "at the bottom" of the scroll area
  const isAtBottomRef = useRef(true);
  // RAF handle for cancel-and-reschedule pattern
  const rafIdRef = useRef<number | null>(null);

  // ── onScroll handler: update bottom-tracking ──
  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const { scrollTop, scrollHeight, clientHeight } = el;
    isAtBottomRef.current = scrollHeight - scrollTop - clientHeight < 150;
  }, []);

  // ── Imperative scroll-to-bottom ──
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollContainerRef.current;
    if (!el) return;
    if (behavior === 'smooth') {
      el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
    } else {
      el.scrollTop = el.scrollHeight;
    }
    isAtBottomRef.current = true;
  }, []);

  // ── Force reset to bottom ──
  // Cancels any pending RAF, immediately snaps to bottom, re-engages sticky mode.
  // MUST be called after loadHistory / session switch to prevent staying at top.
  const resetToBottom = useCallback(() => {
    if (rafIdRef.current !== null) {
      cancelAnimationFrame(rafIdRef.current);
      rafIdRef.current = null;
    }
    const el = scrollContainerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
  }, []);

  // ── Core observer: watch for ANY content change and auto-scroll ──
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    // Cancel-and-reschedule: each mutation cancels the previous pending RAF
    // and schedules a new one. This way the RAF always fires against the most
    // up-to-date scrollHeight, even when tokens arrive at 60+ fps.
    const scheduleScroll = () => {
      if (rafIdRef.current !== null) {
        cancelAnimationFrame(rafIdRef.current);
      }
      rafIdRef.current = requestAnimationFrame(() => {
        rafIdRef.current = null;
        if (isAtBottomRef.current && scrollContainerRef.current) {
          const el = scrollContainerRef.current;
          el.scrollTop = el.scrollHeight;
        }
      });
    };

    // MutationObserver: catches new nodes, text streaming, attribute changes
    const mutObs = new MutationObserver(scheduleScroll);
    mutObs.observe(container, {
      childList: true,
      subtree: true,
      characterData: true,
    });

    // ResizeObserver: catches image loads, card expand/collapse, etc.
    const resizeObs = new ResizeObserver(scheduleScroll);
    resizeObs.observe(container);

    return () => {
      mutObs.disconnect();
      resizeObs.disconnect();
      if (rafIdRef.current !== null) {
        cancelAnimationFrame(rafIdRef.current);
      }
    };
  }, []);

  return {
    scrollContainerRef,
    messagesEndRef,
    handleScroll,
    scrollToBottom,
    resetToBottom,
  };
}
