import type { Dispatch, SetStateAction } from 'react';
import { useCallback, useEffect, useRef } from 'react';
import { useAttachableSessions } from './useAttachableSessions';
import { useAvailableShells } from './useAvailableShells';
import { useTerminalPanes } from './useTerminalPanes';
import type { UseTerminalPanesReturn } from './useTerminalPanes';

export interface UseTerminalWorkbenchOptions {
  /** Whether the terminal is showing (drives session polling and pane activation). */
  isExpanded: boolean;
  /** The terminal's height in px, used by the split-divider drag. */
  terminalHeight: number;
  setIsResizingVertical: Dispatch<SetStateAction<boolean>>;
}

/**
 * The terminal's session state — shells, panes, sessions and their exit
 * handling — independent of where the terminal is placed. The app's docked
 * terminal and the workspace's arranged terminal view both run on it, so the
 * two surfaces behave the same.
 */
export interface TerminalWorkbench extends UseTerminalPanesReturn, ReturnType<typeof useAvailableShells> {
  attachableSessions: ReturnType<typeof useAttachableSessions>['attachableSessions'];
  /** Exit handler for a pane's session (restarts the last session after a short delay). */
  handleProcessExit: (paneId: string, sessionId: string) => void;
  /** The active session of the focused pane, for compact summaries. */
  focusedSession: UseTerminalPanesReturn['panes'][number]['sessions'][number] | undefined;
  totalSessions: number;
}

export function useTerminalWorkbench({
  isExpanded,
  terminalHeight,
  setIsResizingVertical,
}: UseTerminalWorkbenchOptions): TerminalWorkbench {
  const shells = useAvailableShells();
  const { attachableSessions, setAttachableSessions, fetchAttachableSessions } = useAttachableSessions(isExpanded);

  const paneState = useTerminalPanes({
    selectedShell: shells.selectedShell,
    fetchAttachableSessions,
    setAttachableSessions,
    isExpanded,
    terminalHeight,
    setIsResizingVertical,
  });
  const { panes, focusedPaneId, handlePaneExit } = paneState;

  const exitRestartTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    return () => {
      if (exitRestartTimerRef.current !== null) {
        clearTimeout(exitRestartTimerRef.current);
        exitRestartTimerRef.current = null;
      }
    };
  }, []);

  /**
   * The last session in the only pane restarts with a fresh shell after a
   * 1.5 s delay, so the user sees the "[Process exited]" message first.
   * Closing a split pane or a tab with siblings happens immediately.
   */
  const handleProcessExit = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panes.find((p) => p.id === paneId);
      if (!pane) return;

      if (panes.length === 1 && pane.sessions.length === 1) {
        if (exitRestartTimerRef.current !== null) {
          clearTimeout(exitRestartTimerRef.current);
        }
        exitRestartTimerRef.current = setTimeout(() => {
          exitRestartTimerRef.current = null;
          handlePaneExit(paneId, sessionId);
        }, 1500);
        return;
      }

      handlePaneExit(paneId, sessionId);
    },
    [panes, handlePaneExit],
  );

  const focusedPane = panes.find((p) => p.id === focusedPaneId) ?? panes[0];
  const focusedSession = focusedPane?.sessions.find((s) => s.id === focusedPane.activeSessionId);
  const totalSessions = panes.reduce((acc, p) => acc + p.sessions.length, 0);

  return {
    ...paneState,
    ...shells,
    attachableSessions,
    handleProcessExit,
    focusedSession,
    totalSessions,
  };
}
