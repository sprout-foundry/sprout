/**
 * useTerminalResize - manages resize observer and expand event handling.
 *
 * Extracted from TerminalPane.tsx. Handles window resize events, ResizeObserver
 * on paneWrapperRef and xtermContainerRef, and the 'sprout-terminal-expand'
 * custom event. All resize actions are debounced to avoid excessive SIGWINCH
 * signals, and immediate resize is skipped if within 5 seconds of the last
 * session_restored event to avoid duplicate SIGWINCH signals that cause
 * prompt line duplication.
 */

import { useRef, useEffect } from 'react';

const EXPAND_RESIZE_DELAY_MS = 100;

export interface UseTerminalResizeOptions {
  isActive: boolean;
  paneConnected: boolean;
  sendResize: () => void;
  paneWrapperRef: React.RefObject<HTMLDivElement>;
  xtermContainerRef: React.RefObject<HTMLDivElement>;
  lastRestoreTimeRef: React.MutableRefObject<number>;
}

export interface UseTerminalResizeReturn {
  // No return values - this is a side-effect-only hook
}

export function useTerminalResize(options: UseTerminalResizeOptions): UseTerminalResizeReturn {
  const { isActive, paneConnected, sendResize, paneWrapperRef, xtermContainerRef, lastRestoreTimeRef } = options;

  const resizeTimerRef = useRef<number | null>(null);
  const expandTimeoutRef = useRef<number | null>(null);
  const isMountedRef = useRef(true);

  // ── Resize observer and window resize listener ───────────────────────
  useEffect(() => {
    if (!isActive || !paneConnected) return;

    const schedule = () => {
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
      }
      resizeTimerRef.current = window.setTimeout(sendResize, 80);
    };

    // Skip the immediate resize if we just restored from a reattach — the
    // session_restored handler already sent a resize to avoid duplicate
    // SIGWINCH events that cause prompt line duplication.
    if (Date.now() - lastRestoreTimeRef.current > 5000) {
      schedule();
    }

    window.addEventListener('resize', schedule);

    let observer: ResizeObserver | null = null;
    if ('ResizeObserver' in window) {
      observer = new ResizeObserver(schedule);
      if (paneWrapperRef.current) {
        observer.observe(paneWrapperRef.current);
      }
      if (xtermContainerRef.current) {
        observer.observe(xtermContainerRef.current);
      }
    }

    return () => {
      window.removeEventListener('resize', schedule);
      observer?.disconnect();
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = null;
      }
    };
  }, [isActive, paneConnected, sendResize, paneWrapperRef, xtermContainerRef]);

  // ── Terminal expand event listener ─────────────────────────────────────
  useEffect(() => {
    if (!isActive || !paneConnected) return;

    const handleExpand = () => {
      expandTimeoutRef.current = window.setTimeout(() => {
        if (isMountedRef.current) {
          sendResize();
        }
      }, EXPAND_RESIZE_DELAY_MS);
    };

    window.addEventListener('sprout-terminal-expand', handleExpand);

    return () => {
      window.removeEventListener('sprout-terminal-expand', handleExpand);
      if (expandTimeoutRef.current !== null) {
        window.clearTimeout(expandTimeoutRef.current);
        expandTimeoutRef.current = null;
      }
    };
  }, [isActive, paneConnected, sendResize]);

  // ── Cleanup isMountedRef on unmount ───────────────────────────────────
  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  // No return values
  return {};
}

export default useTerminalResize;
