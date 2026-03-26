import { useRef, useCallback, useEffect } from 'react';

/**
 * useChatScroll — intent-aware sticky auto-scroll (v5)
 *
 * v4 problem:
 *   - Used 50ms grace period to distinguish programmatic vs user scroll events
 *   - During streaming, flush() runs every frame → 50ms window eats user swipes
 *   - Slow upward swipe within 50ms of a flush → treated as programmatic → ignored
 *   - Result: user gets bounced back to bottom even when trying to scroll up
 *
 * v5 fix — event-source intent detection:
 *   - wheel + touchmove = guaranteed user intent (not fired by scrollTop assignment)
 *   - handleScroll only tracks isAtBottom; NO grace period, NO intent detection
 *   - userScrolledUpRef set ONLY by wheel/touchmove events → zero false positives
 *   - Zero time-based heuristics, zero race conditions
 */

export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const isAtBottomRef = useRef(true);
  const rafIdRef = useRef<number | null>(null);
  const isStreamingRef = useRef(false);
  // True intent flag: set by wheel/touch events only (never by scroll event timing)
  const userScrolledUpRef = useRef(false);
  // Touch tracking for swipe direction detection
  const touchStartYRef = useRef(0);

  // ── Bottom detection ──
  const checkIsAtBottom = (el: HTMLElement): boolean => {
    const threshold = 4; // subpixel-safe threshold
    return Math.ceil(el.scrollHeight) - el.scrollTop - el.clientHeight <= threshold;
  };

  // ── onScroll: ONLY tracks at-bottom state, no intent detection ──
  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const atBottom = checkIsAtBottom(el);
    isAtBottomRef.current = atBottom;
    // If user scrolls back to bottom during streaming, re-engage auto-scroll
    if (isStreamingRef.current && atBottom) {
      userScrolledUpRef.current = false;
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── flush: SYNC scroll to bottom ──
  // Tracks scrollHeight to compute delta — avoids snap-jumping on image loads
  const prevScrollHeightRef = useRef(0);
  const flush = useCallback(() => {
    rafIdRef.current = null;
    const el = scrollContainerRef.current;
    if (!el) return;

    const newHeight = el.scrollHeight;
    const delta = newHeight - prevScrollHeightRef.current;
    prevScrollHeightRef.current = newHeight;

    if (isStreamingRef.current) {
      if (userScrolledUpRef.current) return;
      el.scrollTop = el.scrollHeight;
      isAtBottomRef.current = true;
      return;
    }

    // Non-streaming: if at bottom and content grew (image load, etc.),
    // adjust by the exact delta — no snap, no jump.
    if (delta > 0 && isAtBottomRef.current) {
      el.scrollTop += delta;
    }
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
    userScrolledUpRef.current = false; // new message = re-engage

    const el = scrollContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
    // MutationObserver will handle subsequent scroll-to-bottom via flush()
  }, []);

  const exitStreamingMode = useCallback(() => {
    // Use single RAF to allow final content to settle, then release immediately
    requestAnimationFrame(() => {
      isStreamingRef.current = false;
      userScrolledUpRef.current = false;
    });
  }, []);

  // ── Imperative: user sends message ──
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const el = scrollContainerRef.current;
    if (!el) return;
    userScrolledUpRef.current = false;
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
    userScrolledUpRef.current = false;
    const el = scrollContainerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
  }, []);

  // ── Core observer + wheel/touch intent detection (created ONCE) ──
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    // Content change observers
    const mutObs = new MutationObserver(scheduleFlush);
    mutObs.observe(container, { childList: true, subtree: true, characterData: true });
    const resizeObs = new ResizeObserver(scheduleFlush);
    resizeObs.observe(container);

    // ── User intent: wheel (desktop) ──
    // wheel events are NEVER fired by programmatic scrollTop changes → zero false positives
    const onWheel = (e: WheelEvent) => {
      if (isStreamingRef.current && e.deltaY < 0) {
        // Scrolling up (wheel toward user)
        userScrolledUpRef.current = true;
      }
    };

    // ── User intent: touch (mobile) ──
    const onTouchStart = (e: TouchEvent) => {
      touchStartYRef.current = e.touches[0].clientY;
    };
    const onTouchMove = (e: TouchEvent) => {
      if (!isStreamingRef.current) return;
      const dy = e.touches[0].clientY - touchStartYRef.current;
      if (dy > 8) {
        // Finger moving down = content scrolling up
        userScrolledUpRef.current = true;
      }
    };

    container.addEventListener('wheel', onWheel, { passive: true });
    container.addEventListener('touchstart', onTouchStart, { passive: true });
    container.addEventListener('touchmove', onTouchMove, { passive: true });

    return () => {
      mutObs.disconnect();
      resizeObs.disconnect();
      container.removeEventListener('wheel', onWheel);
      container.removeEventListener('touchstart', onTouchStart);
      container.removeEventListener('touchmove', onTouchMove);
      if (rafIdRef.current !== null) cancelAnimationFrame(rafIdRef.current);
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
