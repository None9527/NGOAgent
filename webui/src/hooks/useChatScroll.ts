import { useRef, useCallback, useEffect } from 'react';

/**
 * ┌──────────────────────────────────────────────────────────────┐
 * │  useChatScroll — Production-grade sticky auto-scroll hook    │
 * │                                                              │
 * │  Architecture:                                               │
 * │  ┌─────────────────────────────────────┐                     │
 * │  │ scrollContainerRef (overflow-y:auto) │ ← handleScroll     │
 * │  │  ┌─────────────────────────────────┐ │                    │
 * │  │  │        chat content             │ │ ← MutationObserver │
 * │  │  │        ...messages...           │ │                    │
 * │  │  │        [spacer 250px]           │ │                    │
 * │  │  │        [anchor: messagesEnd]    │ │                    │
 * │  │  └─────────────────────────────────┘ │                    │
 * │  └─────────────────────────────────────┘                     │
 * │  ┌─────────────────────────────────────┐                     │
 * │  │  floating composer (absolute)       │                     │
 * │  └─────────────────────────────────────┘                     │
 * └──────────────────────────────────────────────────────────────┘
 *
 * Scroll Strategy:
 * 1. Track whether user is "at bottom" via onScroll (150px threshold).
 * 2. MutationObserver watches entire subtree for DOM changes.
 * 3. On mutation: if user was at bottom → snap to bottom via scrollTop.
 * 4. RAF debounce prevents scroll-storm during rapid streaming.
 * 5. scrollToBottom() for explicit triggers (user sends message).
 */
export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Whether the user is currently "at the bottom" of the scroll area
  const isAtBottomRef = useRef(true);
  // RAF debounce handle to prevent scroll-storm
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

  // ── Core observer: watch for ANY content change and auto-scroll ──
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    // Throttled scroll action via requestAnimationFrame
    const scheduleScroll = () => {
      if (rafIdRef.current !== null) return; // already scheduled
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
  };
}
