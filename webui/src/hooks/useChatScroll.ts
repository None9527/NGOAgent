import { useRef, useCallback } from 'react';

/**
 * useChatScroll — Virtuoso-compatible scroll state manager (v6)
 *
 * v5 had MutationObserver + ResizeObserver + manual `el.scrollTop = el.scrollHeight`
 * which COMPETED with Virtuoso's internal scroll management → scroll jank, missed follows.
 *
 * v6: Delegates ALL scroll control to Virtuoso via `followOutput` and `atBottomStateChange`.
 * This hook only manages boolean state refs that Virtuoso callbacks read.
 *
 * - No MutationObserver, no ResizeObserver, no RAF scheduling
 * - `isStreamingRef` + `userScrolledUpRef` → consumed by ChatViewer's `followOutput` prop
 * - `atBottomStateChange` → sets `isAtBottomRef` so we know when user scrolls back
 */

export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const isAtBottomRef = useRef(true);
  const isStreamingRef = useRef(false);
  const userScrolledUpRef = useRef(false);

  // Called by Virtuoso's atBottomStateChange
  const handleAtBottomChange = useCallback((atBottom: boolean) => {
    isAtBottomRef.current = atBottom;
    // If user scrolls back to bottom during streaming, re-engage auto-follow
    if (isStreamingRef.current && atBottom) {
      userScrolledUpRef.current = false;
    }
  }, []);

  // Called by Virtuoso's isScrolling combined with scroll direction
  // We detect user-initiated scroll-up via the onScroll handler
  const handleScroll = useCallback(() => {
    // No-op: atBottomStateChange handles everything for Virtuoso
  }, []);

  // Streaming mode enter/exit
  const enterStreamingMode = useCallback(() => {
    isStreamingRef.current = true;
    isAtBottomRef.current = true;
    userScrolledUpRef.current = false;
  }, []);

  const exitStreamingMode = useCallback(() => {
    requestAnimationFrame(() => {
      isStreamingRef.current = false;
      userScrolledUpRef.current = false;
    });
  }, []);

  // Imperative: user sends message → snap to bottom
  const scrollToBottom = useCallback((_behavior: ScrollBehavior = 'smooth') => {
    userScrolledUpRef.current = false;
    isAtBottomRef.current = true;
    // DOM-level snap for immediate effect; Virtuoso followOutput handles subsequent follows
    const el = scrollContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  // Force reset: session switch / history load
  const resetToBottom = useCallback(() => {
    isStreamingRef.current = false;
    userScrolledUpRef.current = false;
    isAtBottomRef.current = true;
    // DOM-level snap for session switch
    const el = scrollContainerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  // followOutput callback — Virtuoso calls this whenever data changes
  // Return 'smooth' to auto-scroll, false to stay put
  const followOutput = useCallback((isAtBottom: boolean): false | 'smooth' | 'auto' => {
    // During streaming: follow unless user explicitly scrolled up
    if (isStreamingRef.current) {
      return userScrolledUpRef.current ? false : 'auto';
    }
    // Not streaming: follow only if already at bottom
    return isAtBottom ? 'auto' : false;
  }, []);

  return {
    scrollContainerRef,
    handleScroll,
    handleAtBottomChange,
    scrollToBottom,
    resetToBottom,
    enterStreamingMode,
    exitStreamingMode,
    followOutput,
    isStreamingRef,
    userScrolledUpRef,
  };
}
