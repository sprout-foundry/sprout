/**
 * useTerminalSession - manages WebSocket session lifecycle for the TerminalPane.
 *
 * Extracted from TerminalPane.tsx. Handles the WebSocket connection lifecycle,
 * event handling (connection_status, session_ready, output, session_restored,
 * pty_exit, error), and scrollback loading on session restore.
 */

import type { WsEvent } from '@sprout/events';
import type { FitAddon } from '@xterm/addon-fit';
import type { Terminal as XTerm } from '@xterm/xterm';
import { useRef, useState, useCallback, useEffect } from 'react';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

export interface UseTerminalSessionOptions {
  isActive: boolean;
  isConnected: boolean;
  xtermRef: React.RefObject<XTerm | null>;
  fitAddonRef: React.RefObject<FitAddon | null>;
  preferredShell: string | null;
  reattachSessionId: string | null;
  onConnectionChange?: (connected: boolean) => void;
  onProcessExit?: () => void;
  /** Reset search bar state (called during session_restored and pty_exit). */
  onResetSearch: () => void;
  /** Reset reverse-i-search overlay state. */
  onResetReverseSearch: () => void;
  /** Save scrollback for a given session. */
  onSaveScrollback: (sessionId: string) => void;
  /** Load scrollback for a given session. */
  onLoadScrollback: (sessionId: string) => void;
  /** Called whenever the PTY emits output (used to flag background-tab
      activity in the parent). Filtered to skip pure cursor noise. */
  onActivity?: () => void;
}

export interface UseTerminalSessionReturn {
  paneConnected: boolean;
  terminalWSRef: React.MutableRefObject<TerminalWebSocketService | null>;
  eventHandlerRef: React.MutableRefObject<((event: WsEvent) => void) | null>;
  sendResize: () => void;
  /** Timestamp of the last session_restored event, for guarding against duplicate resizes. */
  lastRestoreTimeRef: React.MutableRefObject<number>;
}

export function useTerminalSession(options: UseTerminalSessionOptions): UseTerminalSessionReturn {
  const {
    isActive,
    isConnected,
    xtermRef,
    fitAddonRef,
    preferredShell,
    reattachSessionId,
    onConnectionChange,
    onProcessExit,
    onResetSearch,
    onResetReverseSearch,
    onSaveScrollback,
    onLoadScrollback,
    onActivity,
  } = options;

  const terminalWSRef = useRef<TerminalWebSocketService | null>(null);
  const eventHandlerRef = useRef<((event: WsEvent) => void) | null>(null);
  const hasAutoFocusedReadyRef = useRef(false);
  const lastRestoreTimeRef = useRef(0);
  const [paneConnected, setPaneConnected] = useState(false);
  const paneConnectedRef = useRef(paneConnected);
  paneConnectedRef.current = paneConnected;

  // Stabilize callbacks in refs.
  //
  // The four search/scrollback callbacks were previously read directly
  // inside the WS-lifecycle effect AND listed in its dependency array.
  // Every parent re-render produces fresh closure identities for them
  // (unless the caller wraps each in useCallback — which TerminalPane
  // does not), so the effect tore down and re-spun the WebSocket on
  // every parent render. The visible symptom: opening the terminal
  // logged three back-to-back "session_created"/"closed (1001 going
  // away)" cycles before settling, and the user saw the panel flicker
  // through several reconnects. Stashing the latest function in a ref
  // lets the effect read the current closure without re-running.
  const onConnectionChangeRef = useRef(onConnectionChange);
  onConnectionChangeRef.current = onConnectionChange;
  const onProcessExitRef = useRef(onProcessExit);
  onProcessExitRef.current = onProcessExit;
  const preferredShellRef = useRef(preferredShell);
  preferredShellRef.current = preferredShell;
  const reattachSessionIdRef = useRef(reattachSessionId);
  reattachSessionIdRef.current = reattachSessionId;
  const onResetSearchRef = useRef(onResetSearch);
  onResetSearchRef.current = onResetSearch;
  const onResetReverseSearchRef = useRef(onResetReverseSearch);
  onResetReverseSearchRef.current = onResetReverseSearch;
  const onSaveScrollbackRef = useRef(onSaveScrollback);
  onSaveScrollbackRef.current = onSaveScrollback;
  const onLoadScrollbackRef = useRef(onLoadScrollback);
  onLoadScrollbackRef.current = onLoadScrollback;
  const onActivityRef = useRef(onActivity);
  onActivityRef.current = onActivity;

  // Track whether the pane is currently mounted/active
  const isActiveRef = useRef(isActive);
  isActiveRef.current = isActive;

  const sendResize = useCallback(() => {
    if (!paneConnectedRef.current || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current) return;
    fitAddonRef.current.fit();
    const cols = xtermRef.current.cols;
    const rows = xtermRef.current.rows;
    if (!cols || !rows || cols < 1 || rows < 1) return;
    terminalWSRef.current.sendResize(cols, rows);
  }, [xtermRef, fitAddonRef]);

  // ── WebSocket lifecycle ─────────────────────────────────────────

  useEffect(() => {
    if (!isActive) {
      if (eventHandlerRef.current && terminalWSRef.current) {
        terminalWSRef.current.removeEvent(eventHandlerRef.current);
        terminalWSRef.current.disconnect();
      }
      eventHandlerRef.current = null;
      terminalWSRef.current = null;
      hasAutoFocusedReadyRef.current = false;
      setPaneConnected(false);
      onConnectionChangeRef.current?.(false);
      return;
    }

    // Don't tear down during freeze or reconnect
    if (
      isConnected === false &&
      terminalWSRef.current &&
      (terminalWSRef.current.isCurrentlyFrozen() || terminalWSRef.current.isReconnecting())
    ) {
      return;
    }

    if (!isConnected) {
      if (eventHandlerRef.current && terminalWSRef.current) {
        terminalWSRef.current.removeEvent(eventHandlerRef.current);
        terminalWSRef.current.disconnect();
      }
      eventHandlerRef.current = null;
      terminalWSRef.current = null;
      setPaneConnected(false);
      onConnectionChangeRef.current?.(false);
      return;
    }

    const service = terminalWSRef.current ?? TerminalWebSocketService.createInstance();
    if (!terminalWSRef.current) {
      terminalWSRef.current = service;
    }

    const handler = (event: WsEvent) => {
      const data = event.data as Record<string, unknown> | undefined;
      if (event.type === 'connection_status') {
        if (!data?.connected) {
          onResetReverseSearchRef.current();
          setPaneConnected(false);
          onConnectionChangeRef.current?.(false);
          xtermRef.current?.writeln('\r\nTerminal disconnected');
        }
      } else if (event.type === 'session_ready') {
        const sessionId = service.getSessionId();
        setPaneConnected(true);
        onConnectionChangeRef.current?.(true);
        if (Date.now() - lastRestoreTimeRef.current < 5000) {
          return;
        }
        const shouldAutoFocus = !hasAutoFocusedReadyRef.current;
        if (shouldAutoFocus) {
          hasAutoFocusedReadyRef.current = true;
        }
        if (sessionId && Date.now() - lastRestoreTimeRef.current >= 5000) {
          onLoadScrollbackRef.current(sessionId);
        }
        requestAnimationFrame(() => {
          sendResize();
          if (shouldAutoFocus) {
            xtermRef.current?.focus();
          }
        });
      } else if (event.type === 'output' || event.type === 'error_output') {
        const chunk = (data?.output as string) || '';
        xtermRef.current?.write(chunk);
        // Bubble activity to the parent only when there's actual content.
        // Cursor-position pokes and other zero-length frames shouldn't
        // light up the tab indicator.
        if (chunk.length > 0) {
          onActivityRef.current?.();
        }
      } else if (event.type === 'session_restored') {
        onResetSearchRef.current();
        onResetReverseSearchRef.current();

        const term = xtermRef.current;
        const sessionId = service.getSessionId();

        if (term) {
          term.reset();
          const scrollback = (data?.scrollback as string) || '';
          if (scrollback) {
            term.write(scrollback);
          } else if (sessionId) {
            onLoadScrollbackRef.current(sessionId);
          }
        }
        lastRestoreTimeRef.current = Date.now();
        setPaneConnected(true);
        onConnectionChangeRef.current?.(true);
        requestAnimationFrame(() => {
          sendResize();
          // Do NOT auto-focus on session restore — this steals focus from
          // the editor/tabs that the user is actively typing in.
        });
      } else if (event.type === 'pty_exit') {
        xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
        setPaneConnected(false);
        onConnectionChangeRef.current?.(false);
        onResetReverseSearchRef.current();

        const svc = terminalWSRef.current;
        if (svc && eventHandlerRef.current) {
          svc.removeEvent(eventHandlerRef.current);
          eventHandlerRef.current = null;
        }
        if (svc) {
          svc.closeSession();
          svc.disconnect();
          terminalWSRef.current = null;
        }
        onProcessExitRef.current?.();
      } else if (event.type === 'error') {
        xtermRef.current?.write(`\r\n${data?.message as string}\r\n`);
      }
    };

    eventHandlerRef.current = handler;
    service.onEvent(handler);

    if (preferredShellRef.current && !service.getSessionId()) {
      service.setPreferredShell(preferredShellRef.current);
    }
    if (reattachSessionIdRef.current && !service.getSessionId()) {
      service.restoreSessionId(reattachSessionIdRef.current);
    }
    if (!service.isConnectedToServer() && !service.isReconnecting()) {
      service.connect();
    }

    return () => {
      if (terminalWSRef.current && (service.isCurrentlyFrozen() || service.isReconnecting()) && isActiveRef.current) {
        service.removeEvent(handler);
        return;
      }

      service.removeEvent(handler);
      if (typeof service.closeSession === 'function') {
        service.closeSession();
      }
      service.disconnect();
      terminalWSRef.current = null;
      eventHandlerRef.current = null;
    };
    // Deps narrowed to the values whose change must actually re-spin
    // the WS connection. The four scrollback/search callbacks moved to
    // refs above; including them here let unmemoized callers (the
    // common case in TerminalPane) re-create the WebSocket every time
    // a sibling state changed in the parent. xtermRef is a stable ref
    // object and is read via `.current` inside the effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- callbacks read via refs above
  }, [isActive, isConnected, sendResize, xtermRef]);

  return {
    paneConnected,
    terminalWSRef,
    eventHandlerRef,
    sendResize,
    lastRestoreTimeRef,
  };
}

export default useTerminalSession;
