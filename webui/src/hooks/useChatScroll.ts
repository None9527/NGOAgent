import { useRef, useCallback, useEffect } from 'react';

/**
 * useChatScroll — bulletproof sticky auto-scroll (v3)
 *
 * Root causes of v2 failures:
 *   1. isStreaming was React state → threshold switch has async delay
 *      → first streaming mutations use non-streaming threshold (2px)
 *      → auto-scroll disengages immediately
 *   2. flush() was async (await scrollend) but called from RAF without await
 *      → concurrent flush calls, race conditions
 *   3. MutationObserver recreated on isStreaming change → brief blindspot
 *
 * v3 fix:
 *   - ALL state is refs (synchronous, zero delay)
 *   - flush() is SYNC: scrollTop assignment during streaming, no animation
 *   - MutationObserver created once, never torn down during streaming
 *   - ~100 lines, zero complexity
 */

export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const isAtBottomRef = useRef(true);
  const rafIdRef = useRef<number | null>(null);
  const isStreamingRef = useRef(false);
  const lastFlushTimeRef = useRef(0);

  const FLUSH_GRACE_MS = 80;

  // ── Bottom detection ──
  const checkIsAtBottom = (el: HTMLElement): boolean => {
    // Streaming: 80px tolerance (content grows fast)
    // Idle: 2px tolerance (pixel-perfect)
    const threshold = isStreamingRef.current ? 80 : 2;
    return Math.ceil(el.scrollHeight) - el.scrollTop - el.clientHeight <= threshold;
  };

  // ── onScroll: only genuine user scrolls update at-bottom state ──
  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;

    // Ignore programmatic scrolls (within grace period of flush)
    if (Date.now() - lastFlushTimeRef.current < FLUSH_GRACE_MS) return;

    isAtBottomRef.current = checkIsAtBottom(el);
  }, []);

  // ── flush: SYNC scroll, no await, no animation during streaming ──
  const flush = useCallback(() => {
    rafIdRef.current = null;
    const el = scrollContainerRef.current;
    if (!el) return;

    // During streaming: if user hasn't explicitly scrolled up, always follow bottom
    if (isStreamingRef.current) {
      // Only disengage if user has scrolled significantly up (> 300px from bottom)
      const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      if (distFromBottom > 300) return; // User intentionally scrolled up
      // Otherwise force follow
      lastFlushTimeRef.current = Date.now();
      el.scrollTop = el.scrollHeight;
      isAtBottomRef.current = true;
      return;
    }

    // Non-streaming: respect isAtBottom
    if (!isAtBottomRef.current) return;
    lastFlushTimeRef.current = Date.now();
    el.scrollTop = el.scrollHeight;
  }, []);

  // ── schedule: RAF-gated, max one pending ──
  const scheduleFlush = useCallback(() => {
    if (rafIdRef.current === null) {
      rafIdRef.current = requestAnimationFrame(flush);
    }
  }, [flush]);

  // ── Streaming mode: synchronous ref toggle ──
  const enterStreamingMode = useCallback(() => {
    isStreamingRef.current = true;
    isAtBottomRef.current = true;

    // Immediately scroll to bottom + force next 3 frames to keep following
    const el = scrollContainerRef.current;
    if (el) {
      lastFlushTimeRef.current = Date.now();
      el.scrollTop = el.scrollHeight;
    }
    // Multi-frame chase: DOM may update across several frames
    let frames = 3;
    const chase = () => {
      if (frames-- > 0 && scrollContainerRef.current) {
        lastFlushTimeRef.current = Date.now();
        scrollContainerRef.current.scrollTop = scrollContainerRef.current.scrollHeight;
        requestAnimationFrame(chase);
      }
    };
    requestAnimationFrame(chase);
  }, []);

  const exitStreamingMode = useCallback(() => {
    // Delay exit so final content renders still auto-scroll
    setTimeout(() => {
      isStreamingRef.current = false;
    }, 500);
  }, []);

  // ── Imperative: user sends message (smooth scroll) ──
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollContainerRef.current;
    if (!el) return;
    lastFlushTimeRef.current = Date.now();
    el.scrollTo({ top: el.scrollHeight, behavior });
    isAtBottomRef.current = true;
  }, []);

  // ── Force reset: session switch / history load ──
  const resetToBottom = useCallback(() => {
    if (rafIdRef.current !== null) {
      cancelAnimationFrame(rafIdRef.current);
      rafIdRef.current = null;
    }
    isStreamingRef.current = false;

    const el = scrollContainerRef.current;
    if (!el) return;

    lastFlushTimeRef.current = Date.now();
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
  }, []);

  // ── Core observer: created ONCE, never torn down during streaming ──
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    const mutObs = new MutationObserver(scheduleFlush);
    mutObs.observe(container, {
      childList: true,
      subtree: true,
      characterData: true,
    });

    const resizeObs = new ResizeObserver(scheduleFlush);
    resizeObs.observe(container);

    return () => {
      mutObs.disconnect();
      resizeObs.disconnect();
      if (rafIdRef.current !== null) {
        cancelAnimationFrame(rafIdRef.current);
      }
    };
  }, [scheduleFlush]);

  return {
    scrollContainerRef,
    handleScroll,
    scrollToBottom,
    resetToBottom,
    enterStreamingMode,
    exitStreamingMode,
    isStreaming: isStreamingRef.current,
  };
}
