import { useEffect, useRef } from 'react';
import { WebSocketService } from '../services/websocket';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { debugLog } from '../utils/log';

/** Interval (ms) to debounce rapid visibility toggles and avoid freeze/resume thrashing. */
const VISIBILITY_DEBOUNCE_MS = 500;

/**
 * Stateless utility — returns `true` when the document is currently visible.
 * Safe to call outside of React (e.g. from service workers or event handlers).
 */
export function isPageVisible(): boolean {
  if (typeof document === 'undefined') return true;
  return document.visibilityState === 'visible';
}

/**
 * React hook that listens for `visibilitychange` DOM events and calls
 * `freeze()` / `resume()` on the WebSocket services when the page is
 * hidden / restored.
 *
 * IMPORTANT: This hook must be called at the top level of a React component
 * (it uses `useEffect` internally which is subject to the Rules of Hooks).
 *
 * Features:
 * - Calls `freeze()` on `WebSocketService` singleton + all active
 *   `TerminalWebSocketService` instances when the page becomes hidden.
 * - Calls `resume()` on all of them when the page becomes visible again.
 * - Debounces rapid toggles within `VISIBILITY_DEBOUNCE_MS` to avoid
 *   freeze/resume thrashing (e.g. Cmd+Tab passes through hidden→visible in <1 frame).
 * - Uses a ref guard to prevent double-firing in React StrictMode.
 * - Cleans up event listener on unmount.
 */
export function usePageVisibility(): void {
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    let debounceTimer: ReturnType<typeof setTimeout> | null = null;
    let pendingAction: 'freeze' | 'resume' | null = null;

    const handleVisibilityChange = () => {
      const visible = document.visibilityState === 'visible';
      const action = visible ? 'resume' : 'freeze';

      // If a debounce timer is already pending and the action hasn't changed,
      // just let it ride — no need to reschedule the same action.
      if (debounceTimer !== null && pendingAction === action) {
        return;
      }

      pendingAction = action;

      if (debounceTimer !== null) {
        clearTimeout(debounceTimer);
      }

      debugLog(
        `[visibility] page ${visible ? 'visible' : 'hidden'} — ${action} scheduled (debounce ${VISIBILITY_DEBOUNCE_MS}ms)`,
      );

      debounceTimer = setTimeout(() => {
        debounceTimer = null;
        if (!mountedRef.current) return;

        // Only execute if the intended action still matches the *current*
        // visibility state (the user may have toggled back while we waited).
        const currentlyVisible = document.visibilityState === 'visible';
        const effectiveAction = currentlyVisible ? 'resume' : 'freeze';
        if (pendingAction !== effectiveAction) {
          // User toggled back during the debounce window — skip stale action.
          // A new timer will have been scheduled by a re-entrant call to
          // handleVisibilityChange if needed.
          pendingAction = null;
          return;
        }

        pendingAction = null;

        if (effectiveAction === 'freeze') {
          debugLog('[visibility] executing freeze');
          try {
            WebSocketService.getInstance().freeze();
          } catch (err) {
            debugLog('[visibility] WebSocketService.freeze() failed:', err);
          }
          try {
            TerminalWebSocketService.freezeAll();
          } catch (err) {
            debugLog('[visibility] TerminalWebSocketService.freezeAll() failed:', err);
          }
        } else {
          debugLog('[visibility] executing resume');
          try {
            WebSocketService.getInstance().resume();
          } catch (err) {
            debugLog('[visibility] WebSocketService.resume() failed:', err);
          }
          try {
            TerminalWebSocketService.resumeAll();
          } catch (err) {
            debugLog('[visibility] TerminalWebSocketService.resumeAll() failed:', err);
          }
        }
      }, VISIBILITY_DEBOUNCE_MS);
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      // Set guard first so any in-flight debounce callback bails out.
      mountedRef.current = false;
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      if (debounceTimer !== null) {
        clearTimeout(debounceTimer);
        debounceTimer = null;
      }
    };
  }, []);
}
