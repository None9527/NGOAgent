import { useRef, useCallback, useEffect, useState } from 'react';

/**
 * useChatScroll — single-observer, zero-waste sticky auto-scroll
 *
 * Architecture:
 *   MutationObserver → dirty flag → single RAF → scroll if at-bottom
 *
 * Root cause of previous failures:
 *   flush() sets scrollTop = scrollHeight, which synchronously fires onScroll.
 *   Between the assignment and the onScroll read, React may have committed
 *   new DOM content, making scrollHeight larger. checkIsAtBottom then reads:
 *     newScrollHeight - oldScrollTop - clientHeight > 2px → false
 *   This permanently disables auto-scroll. The longer the content, the bigger
 *   the per-frame pixel growth, the wider the race window.
 *
 * Fix v1:
 *   `isFlushingRef` gates the onScroll handler. During programmatic scrollTop
 *   writes, onScroll is suppressed. Only genuine user scroll gestures can
 *   disengage auto-follow.
 *
 * Fix v2 (current):
 *   - Added streaming mode detection with enter/exit callbacks
 *   - Throttled MutationObserver during streaming (100ms vs 16ms)
 *   - RAF timestamp grace period to distinguish user vs programmatic scrolls
 *   - scrollend event support for smooth scrolling completion detection
 *   - Streaming mode uses 'instant' behavior to prevent animation queue buildup
 */

// 节流函数：限制函数执行频率
function throttle<T extends (...args: unknown[]) => void>(
  fn: T,
  limitMs: number
): (...args: Parameters<T>) => void {
  let lastRun = 0;
  return (...args: Parameters<T>) => {
    const now = Date.now();
    if (now - lastRun >= limitMs) {
      lastRun = now;
      fn(...args);
    }
  };
}

export function useChatScroll() {
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const isAtBottomRef = useRef(true);
  const dirtyRef = useRef(false);
  const rafIdRef = useRef<number | null>(null);
  const isFlushingRef = useRef(false);

  // 流式模式状态
  const [isStreaming, setIsStreaming] = useState(false);
  const streamingTimeoutRef = useRef<number | null>(null);

  // RAF 时间戳替代布尔标志来避免竞争条件
  const lastFlushTimeRef = useRef(0);
  const FLUSH_GRACE_PERIOD = 100; // ms

  // ── 流式模式控制 ──
  const enterStreamingMode = useCallback(() => {
    setIsStreaming(true);
    if (streamingTimeoutRef.current) {
      window.clearTimeout(streamingTimeoutRef.current);
    }
  }, []);

  const exitStreamingMode = useCallback(() => {
    if (streamingTimeoutRef.current) {
      window.clearTimeout(streamingTimeoutRef.current);
    }
    streamingTimeoutRef.current = window.setTimeout(() => {
      setIsStreaming(false);
    }, 500);
  }, []);

  // ── 底部检测：流式模式下使用更大容差 ──
  const checkIsAtBottom = useCallback((el: HTMLElement): boolean => {
    const threshold = isStreaming ? 50 : 2; // 流式模式下使用更大容差
    return Math.ceil(el.scrollHeight) - el.scrollTop - el.clientHeight <= threshold;
  }, [isStreaming]);

  // ── 等待 scrollend 事件 ──
  const waitForScrollEnd = useCallback((el: HTMLElement): Promise<void> => {
    return new Promise((resolve) => {
      // 优先使用原生 scrollend 事件
      if ('onscrollend' in window) {
        const handler = () => {
          el.removeEventListener('scrollend', handler);
          resolve();
        };
        el.addEventListener('scrollend', handler, { once: true });

        // 超时兜底
        setTimeout(() => {
          el.removeEventListener('scrollend', handler);
          resolve();
        }, 300);
      } else {
        // 降级方案：等待一帧 + 检查 scrollTop 稳定
        let lastScrollTop = el.scrollTop;
        const checkStable = () => {
          if (el.scrollTop === lastScrollTop) {
            resolve();
          } else {
            lastScrollTop = el.scrollTop;
            requestAnimationFrame(checkStable);
          }
        };
        requestAnimationFrame(checkStable);
      }
    });
  }, []);

  // ── onScroll：使用 RAF 时间戳判断是否为程序触发 ──
  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;

    // 如果在 flush 的宽限期内，忽略此滚动事件
    const timeSinceFlush = Date.now() - lastFlushTimeRef.current;
    if (timeSinceFlush < FLUSH_GRACE_PERIOD) {
      return;
    }

    isAtBottomRef.current = checkIsAtBottom(el);
  }, [checkIsAtBottom]);

  // ── Flush：使用 RAF 时间戳和 scrollend ──
  const flush = useCallback(async () => {
    rafIdRef.current = null;
    dirtyRef.current = false;
    const el = scrollContainerRef.current;
    if (!el || !isAtBottomRef.current) return;

    isFlushingRef.current = true;
    lastFlushTimeRef.current = Date.now();

    // 流式模式下使用 instant 避免动画堆积
    const behavior = isStreaming ? 'instant' : 'auto';
    el.scrollTo({ top: el.scrollHeight, behavior });

    // 等待滚动完成
    await waitForScrollEnd(el);

    isFlushingRef.current = false;
    lastFlushTimeRef.current = Date.now();
  }, [isStreaming, waitForScrollEnd]);

  // ── Schedule：流式模式下节流 ──
  const scheduleScroll = useCallback(() => {
    dirtyRef.current = true;

    // 流式模式下限制为每 100ms 最多一次
    const throttleMs = isStreaming ? 100 : 0;

    if (rafIdRef.current === null) {
      const schedule = () => {
        rafIdRef.current = requestAnimationFrame(() => {
          flush();
        });
      };

      if (throttleMs > 0) {
        setTimeout(schedule, throttleMs);
      } else {
        schedule();
      }
    }
  }, [isStreaming, flush]);

  // ── Imperative：用户发送消息 ──
  const scrollToBottom = useCallback(async (behavior: ScrollBehavior = 'smooth') => {
    const el = scrollContainerRef.current;
    if (!el) return;

    isFlushingRef.current = true;
    lastFlushTimeRef.current = Date.now();

    el.scrollTo({ top: el.scrollHeight, behavior });

    // 只有 smooth 行为需要等待
    if (behavior === 'smooth') {
      await waitForScrollEnd(el);
    }

    isFlushingRef.current = false;
    lastFlushTimeRef.current = Date.now();
    isAtBottomRef.current = true;
  }, [waitForScrollEnd]);

  // ── Force reset：会话切换 / 历史加载 ──
  const resetToBottom = useCallback(async () => {
    // 清理所有 pending 状态
    if (rafIdRef.current !== null) {
      cancelAnimationFrame(rafIdRef.current);
      rafIdRef.current = null;
    }
    if (streamingTimeoutRef.current) {
      window.clearTimeout(streamingTimeoutRef.current);
    }
    dirtyRef.current = false;
    setIsStreaming(false);

    const el = scrollContainerRef.current;
    if (!el) return;

    isFlushingRef.current = true;
    lastFlushTimeRef.current = Date.now();

    el.scrollTop = el.scrollHeight;

    isFlushingRef.current = false;
    lastFlushTimeRef.current = Date.now();
    isAtBottomRef.current = true;
  }, []);

  // ── Core observer：使用节流的 mutation handler ──
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    // 节流的 mutation handler
    const throttledSchedule = throttle(scheduleScroll, isStreaming ? 100 : 16);

    const mutObs = new MutationObserver(() => {
      throttledSchedule();
    });
    mutObs.observe(container, {
      childList: true,
      subtree: true,
      characterData: true,
    });

    const resizeObs = new ResizeObserver(throttledSchedule);
    resizeObs.observe(container);

    return () => {
      mutObs.disconnect();
      resizeObs.disconnect();
      if (rafIdRef.current !== null) {
        cancelAnimationFrame(rafIdRef.current);
      }
      if (streamingTimeoutRef.current) {
        window.clearTimeout(streamingTimeoutRef.current);
      }
    };
  }, [scheduleScroll, isStreaming]);

  return {
    scrollContainerRef,
    handleScroll,
    scrollToBottom,
    resetToBottom,
    enterStreamingMode,
    exitStreamingMode,
    isStreaming,
  };
}
