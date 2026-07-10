import type { TerminalSession, AttachableSession } from '@sprout/ui';
import { useState, useEffect, useRef, useCallback } from 'react';
import type { CSSProperties, MouseEvent as ReactMouseEvent } from 'react';
import type { TerminalPaneHandle } from '../components/TerminalPane';
import { clientFetch } from '../services/clientSession';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

export type SplitDirection = 'none' | 'horizontal' | 'vertical';

export interface TerminalPaneData {
  id: string;
  sessions: TerminalSession[];
  activeSessionId: string;
}

export interface UseTerminalPanesOptions {
  /** Currently selected shell name (may be null). */
  selectedShell: string | null;
  /** Fetches the list of attachable agent sessions (side-effect). */
  fetchAttachableSessions: () => Promise<void>;
  /** Remove the session from the attachable list (side-effect). */
  setAttachableSessions: React.Dispatch<React.SetStateAction<AttachableSession[]>>;
  /** Whether the terminal panel is expanded. */
  isExpanded: boolean;
  /** Current terminal height in px (used by split divider drag). */
  terminalHeight: number;
  /** Whether the user is actively resizing the terminal vertically. */
  setIsResizingVertical: React.Dispatch<React.SetStateAction<boolean>>;
}

export interface UseTerminalPanesReturn {
  /* Pane data */
  panes: TerminalPaneData[];
  focusedPaneId: string;
  setFocusedPaneId: React.Dispatch<React.SetStateAction<string>>;
  getFocusedPane: () => TerminalPaneData | null;

  /* Split state */
  splitDirection: SplitDirection;
  splitSizes: number[];
  isSplitActive: boolean;
  splitStyleForPane: (index: number) => CSSProperties;
  handleSplitDividerDragStart: (e: ReactMouseEvent, dividerIndex: number) => void;

  /* Pane / session actions */
  addSessionToPane: (paneId: string, shell?: string | null) => void;
  closeSessionInPane: (paneId: string, sessionId: string) => void;
  removePane: (paneId: string) => void;
  renameSessionInPane: (paneId: string, sessionId: string, name: string) => void;
  togglePinInPane: (paneId: string, sessionId: string) => void;
  switchSessionInPane: (paneId: string, sessionId: string) => void;
  handleSessionTitleChange: (paneId: string, sessionId: string, title: string) => void;
  handleSessionActivity: (paneId: string, sessionId: string) => void;
  handlePaneExit: (paneId: string, sessionId: string) => void;
  handleAttachAgentSession: (sessionId: string, name: string) => Promise<void>;
  /** Always adds a pane in the given direction (never un-splits). */
  addSplitPane: (direction: 'horizontal' | 'vertical') => void;
  /** Whether a split button in the given direction can add a pane right now. */
  canAddPaneForDirection: (direction: 'horizontal' | 'vertical') => boolean;

  /* Activity tracking */
  activitySessionIds: Set<string>;
  clearActivityForSession: (sessionId: string) => void;

  /* Refs exposed for cleanup / external access */
  paneHandlesRef: React.MutableRefObject<Map<string, TerminalPaneHandle | null>>;
  sessionShellsRef: React.MutableRefObject<Map<string, string | null>>;
  sessionReattachIdsRef: React.MutableRefObject<Map<string, string | null>>;
}

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

const MIN_PANE_WIDTH_PX = 240;
const MIN_PANE_HEIGHT_PX = 120;
const MAX_PANES_HARD_CAP = 8;

const evenSplit = (n: number): number[] => Array.from({ length: n }, () => 100 / n);

const redistributeAfterRemove = (sizes: number[], removedIndex: number): number[] => {
  if (sizes.length <= 1) return [100];
  const next = sizes.filter((_, i) => i !== removedIndex);
  const total = next.reduce((acc, v) => acc + v, 0);
  if (total <= 0) return evenSplit(next.length);
  return next.map((v) => (v / total) * 100);
};

/* ------------------------------------------------------------------ */
/*  nextActiveAfterClose — shared utility (also exported by Terminal.tsx) */
/* ------------------------------------------------------------------ */

/**
 * Pick the session that should become active after `closedId` is removed
 * from `sessions`. Mirrors TerminalTabBar's display ordering — pinned
 * sessions sort to the front, ties broken by insertion order — and then
 * picks the next neighbour to the right (or the new last tab, if the
 * closing tab was the rightmost).
 *
 * Returns the closed session's own id when there's no other session left;
 * callers are expected to guard against that case before calling.
 */
export function nextActiveAfterClose(sessions: TerminalSession[], closedId: string): string {
  const displayOrder = sessions
    .map((session, index) => ({ session, index }))
    .sort((a, b) => {
      if (a.session.is_pinned !== b.session.is_pinned) {
        return a.session.is_pinned ? -1 : 1;
      }
      return a.index - b.index;
    })
    .map(({ session }) => session);
  const closedDisplayIdx = displayOrder.findIndex((s) => s.id === closedId);
  const remaining = displayOrder.filter((s) => s.id !== closedId);
  if (remaining.length === 0) return closedId;
  return remaining[Math.min(closedDisplayIdx, remaining.length - 1)].id;
}

/* ------------------------------------------------------------------ */
/*  Hook                                                               */
/* ------------------------------------------------------------------ */

export function useTerminalPanes(options: UseTerminalPanesOptions): UseTerminalPanesReturn {
  const {
    selectedShell,
    fetchAttachableSessions,
    setAttachableSessions,
    isExpanded,
    terminalHeight,
    setIsResizingVertical,
  } = options;
  const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
  const splitDirectionRef = useRef<SplitDirection>('none');
  const [splitSizes, setSplitSizes] = useState<number[]>([100]);

  /* ---- Pane-based session state ---- */
  const paneIdCounter = useRef(0);
  const sessionCounterRef = useRef(1);
  const panesRef = useRef<TerminalPaneData[]>([]);

  const [panes, setPanes] = useState<TerminalPaneData[]>(() => {
    paneIdCounter.current += 1;
    const paneId = `pane-${paneIdCounter.current}`;
    const sessionId = `session-${sessionCounterRef.current++}`;
    const initialPane: TerminalPaneData = {
      id: paneId,
      sessions: [{ id: sessionId, name: 'Session 1', is_pinned: false }],
      activeSessionId: sessionId,
    };
    panesRef.current = [initialPane];
    return [initialPane];
  });

  const [focusedPaneId, setFocusedPaneId] = useState<string>(panes[0].id);

  /* ---- Activity / rename tracking ---- */
  const [activitySessionIds, setActivitySessionIds] = useState<Set<string>>(() => new Set());
  const manuallyRenamedSessions = useRef<Set<string>>(new Set());

  /* ---- Refs ---- */
  const paneHandles = useRef<Map<string, TerminalPaneHandle | null>>(new Map());
  const sessionShellsRef = useRef<Map<string, string | null>>(new Map());
  const sessionReattachIdsRef = useRef<Map<string, string | null>>(new Map());

  /* ---- Drag refs (split divider) ---- */
  const isDraggingSplit = useRef(false);
  const splitDragStartPos = useRef(0);
  const splitDragStartSizes = useRef<number[]>([100]);
  const splitDragIndex = useRef(0);

  /* ---- Keep panesRef in sync ---- */
  useEffect(() => {
    panesRef.current = panes;
  }, [panes]);

  /* ---- Helpers ---- */
  const getFocusedPane = useCallback((): TerminalPaneData | null => {
    return panesRef.current.find((p) => p.id === focusedPaneId) ?? null;
  }, [focusedPaneId]);

  // Clear the active pane's terminal content
  const handleClearActivePane = useCallback(() => {
    const pane = getFocusedPane();
    if (pane) {
      const handle = paneHandles.current.get(pane.activeSessionId);
      handle?.clear();
    }
  }, [getFocusedPane]);

  // Update a pane immutably
  const updatePane = useCallback((paneId: string, updater: (pane: TerminalPaneData) => TerminalPaneData) => {
    setPanes((prev) => prev.map((p) => (p.id === paneId ? updater(p) : p)));
  }, []);

  // Drop a session id from the per-Terminal tracking sets
  const forgetSession = useCallback((sessionId: string) => {
    manuallyRenamedSessions.current.delete(sessionId);
    setActivitySessionIds((prev) => {
      if (!prev.has(sessionId)) return prev;
      const next = new Set(prev);
      next.delete(sessionId);
      return next;
    });
  }, []);

  /* ---- Session management scoped to panes ---- */
  const addSessionToPane = useCallback(
    (paneId: string, shell?: string | null) => {
      const sessionNum = sessionCounterRef.current++;
      const sessionId = `session-${sessionNum}`;
      const newSession: TerminalSession = {
        id: sessionId,
        name: `Session ${sessionNum}`,
        is_pinned: false,
      };
      sessionShellsRef.current.set(sessionId, shell ?? selectedShell ?? null);
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: [...pane.sessions, newSession],
        activeSessionId: sessionId,
      }));
    },
    [selectedShell, updatePane],
  );

  /* ---- Pane management ---- */
  const removePane = useCallback(
    (paneId: string) => {
      const all = panesRef.current;
      const idx = all.findIndex((p) => p.id === paneId);
      if (idx === -1) return;
      if (all.length <= 1) return;

      const pane = all[idx];
      pane.sessions.forEach((s) => {
        const handle = paneHandles.current.get(s.id);
        handle?.cleanup?.();
        paneHandles.current.delete(s.id);
        sessionShellsRef.current.delete(s.id);
        sessionReattachIdsRef.current.delete(s.id);
        forgetSession(s.id);
      });

      const remainingPanes = all.filter((p) => p.id !== paneId);
      setPanes(remainingPanes);
      setSplitSizes((prev) => redistributeAfterRemove(prev, idx));

      if (focusedPaneId === paneId) {
        const neighbor = remainingPanes[Math.max(0, idx - 1)];
        if (neighbor) setFocusedPaneId(neighbor.id);
      }

      if (remainingPanes.length === 1) {
        splitDirectionRef.current = 'none';
        setSplitDirection('none');
        setSplitSizes([100]);
      }
    },
    [focusedPaneId, forgetSession],
  );

  const closeSessionInPane = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;

      if (pane.sessions.length === 1) {
        if (panesRef.current.length === 1) return;
        removePane(paneId);
        return;
      }

      const handle = paneHandles.current.get(sessionId);
      handle?.cleanup?.();
      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);
      forgetSession(sessionId);

      const remaining = pane.sessions.filter((s) => s.id !== sessionId);
      const newActive =
        pane.activeSessionId === sessionId ? nextActiveAfterClose(pane.sessions, sessionId) : pane.activeSessionId;

      updatePane(paneId, (p) => ({
        ...p,
        sessions: remaining,
        activeSessionId: newActive,
      }));
    },
    [forgetSession, removePane, updatePane],
  );

  const renameSessionInPane = useCallback(
    (paneId: string, sessionId: string, name: string) => {
      manuallyRenamedSessions.current.add(sessionId);
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: pane.sessions.map((s) => (s.id === sessionId ? { ...s, name } : s)),
      }));
    },
    [updatePane],
  );

  const handleSessionTitleChange = useCallback(
    (paneId: string, sessionId: string, title: string) => {
      if (manuallyRenamedSessions.current.has(sessionId)) return;
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;
      const target = pane.sessions.find((s) => s.id === sessionId);
      if (!target || target.name === title) return;
      updatePane(paneId, (p) => ({
        ...p,
        sessions: p.sessions.map((s) => (s.id === sessionId ? { ...s, name: title } : s)),
      }));
    },
    [updatePane],
  );

  const handleSessionActivity = useCallback((paneId: string, sessionId: string) => {
    const pane = panesRef.current.find((p) => p.id === paneId);
    if (!pane) return;
    if (pane.activeSessionId === sessionId) return;
    setActivitySessionIds((prev) => {
      if (prev.has(sessionId)) return prev;
      const next = new Set(prev);
      next.add(sessionId);
      return next;
    });
  }, []);

  const clearActivityForSession = useCallback((sessionId: string) => {
    setActivitySessionIds((prev) => {
      if (!prev.has(sessionId)) return prev;
      const next = new Set(prev);
      next.delete(sessionId);
      return next;
    });
  }, []);

  const togglePinInPane = useCallback(
    (paneId: string, sessionId: string) => {
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: pane.sessions.map((s) => (s.id === sessionId ? { ...s, is_pinned: !s.is_pinned } : s)),
      }));
    },
    [updatePane],
  );

  const switchSessionInPane = useCallback(
    (paneId: string, sessionId: string) => {
      updatePane(paneId, (pane) => ({ ...pane, activeSessionId: sessionId }));
      clearActivityForSession(sessionId);
      requestAnimationFrame(() => {
        window.dispatchEvent(new Event('resize'));
      });
    },
    [updatePane, clearActivityForSession],
  );

  /* ---- Attach agent session ---- */
  const handleAttachAgentSession = useCallback(
    async (sessionId: string, name: string) => {
      setAttachableSessions((prev) => prev.filter((s) => s.id !== sessionId));
      try {
        const response = await clientFetch(`/api/terminal/agent-sessions/${sessionId}/attach`, {
          method: 'POST',
        });
        if (!response.ok) {
          if (response.status === 400 || response.status === 404 || response.status === 410) {
            notificationBus.notify('info', 'Terminal', `Session '${name}' is no longer available`);
            return;
          }
          throw new Error(`Failed to attach session: ${response.status}`);
        }

        const pane = getFocusedPane();
        if (pane) {
          const sessionName = name || `Agent: ${sessionId.substring(0, 20)}`;
          const newSession: TerminalSession = {
            id: sessionId,
            name: sessionName,
            is_pinned: false,
          };
          sessionReattachIdsRef.current.set(sessionId, sessionId);
          updatePane(pane.id, (p) => ({
            ...p,
            sessions: [...p.sessions, newSession],
            activeSessionId: sessionId,
          }));
        }
        await fetchAttachableSessions();
      } catch (err) {
        debugLog('[Terminal] Failed to attach agent session:', err);
        notificationBus.notify('warning', 'Terminal', 'Failed to attach session: ' + String(err));
        fetchAttachableSessions();
      }
    },
    [fetchAttachableSessions, getFocusedPane, updatePane, setAttachableSessions],
  );

  /* ---- Handle pane exit ---- */
  const handlePaneExit = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;

      if (!pane.sessions.some((s) => s.id === sessionId) && !paneHandles.current.has(sessionId)) {
        debugLog('[Terminal] pty_exit for already-closed session:', sessionId);
        return;
      }

      if (pane.sessions.length > 1) {
        closeSessionInPane(paneId, sessionId);
        notificationBus.notify('info', 'Terminal', 'Terminal process exited — session closed.');
        return;
      }

      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);

      if (panesRef.current.length > 1) {
        removePane(paneId);
        notificationBus.notify('info', 'Terminal', 'Split pane closed after process exited.');
        return;
      }

      forgetSession(sessionId);

      const sessionNum2 = sessionCounterRef.current++;
      const newSessionId = `session-${sessionNum2}`;
      const newSession: TerminalSession = {
        id: newSessionId,
        name: `Session ${sessionNum2}`,
        is_pinned: false,
      };
      sessionShellsRef.current.set(newSessionId, selectedShell ?? null);

      updatePane(paneId, (p) => ({
        ...p,
        sessions: [newSession],
        activeSessionId: newSessionId,
      }));

      notificationBus.notify('info', 'Terminal', 'Terminal process exited — restarted with fresh shell.');
    },
    [closeSessionInPane, forgetSession, removePane, selectedShell, updatePane],
  );

  /* ---- Split / pane creation ---- */
  const createPane = useCallback((): TerminalPaneData => {
    paneIdCounter.current += 1;
    const paneId = `pane-${paneIdCounter.current}`;
    const sessionNum = sessionCounterRef.current++;
    const sessionId = `session-${sessionNum}`;
    sessionShellsRef.current.set(sessionId, selectedShell ?? null);
    return {
      id: paneId,
      sessions: [{ id: sessionId, name: `Session ${sessionNum}`, is_pinned: false }],
      activeSessionId: sessionId,
    };
  }, [selectedShell]);

  const computeMaxPanes = useCallback((direction: SplitDirection): number => {
    if (direction === 'none') return 1;
    const el = document.querySelector('.terminal-panes-container') as HTMLElement | null;
    const rect = el?.getBoundingClientRect();
    const w = rect?.width ?? 0;
    const h = rect?.height ?? 0;
    if (w === 0 && h === 0) return MAX_PANES_HARD_CAP;
    const limit = direction === 'vertical' ? Math.floor(w / MIN_PANE_WIDTH_PX) : Math.floor(h / MIN_PANE_HEIGHT_PX);
    return Math.min(MAX_PANES_HARD_CAP, Math.max(2, limit));
  }, []);

  /**
   * Always-add split action (editor convention). Clicking a split button
   * adds a new pane — it never toggles/un-splits. Closing a pane is done
   * via the per-pane close button (which calls `removePane`).
   *
   *   - No split active → create a pane, set direction.
   *   - Split in the SAME direction → add another pane (respecting the max).
   *   - Split in a DIFFERENT direction → flip the layout direction, keep
   *     the existing panes (do NOT add a new one).
   */
  const addSplitPane = useCallback(
    (direction: 'horizontal' | 'vertical') => {
      const currentDir = splitDirectionRef.current;

      // Direction switch: keep existing panes, just change the axis.
      if (currentDir !== 'none' && currentDir !== direction) {
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
        setSplitSizes(evenSplit(panesRef.current.length));
        return;
      }

      // Same direction (or first split): respect the pane cap.
      const max = computeMaxPanes(direction);
      if (panesRef.current.length >= max) return;

      const newPane = createPane();
      const nextCount = panesRef.current.length + 1;
      setPanes((prev) => [...prev, newPane]);
      setFocusedPaneId(newPane.id);
      setSplitSizes(evenSplit(nextCount));
      splitDirectionRef.current = direction;
      setSplitDirection(direction);
    },
    [computeMaxPanes, createPane],
  );

  /* ---- Split divider drag ---- */
  const handleSplitDividerDragStart = useCallback(
    (e: ReactMouseEvent, dividerIndex: number) => {
      e.preventDefault();
      isDraggingSplit.current = true;
      splitDragIndex.current = dividerIndex;
      setIsResizingVertical(true);
      splitDragStartPos.current = splitDirection === 'vertical' ? e.clientX : e.clientY;
      splitDragStartSizes.current = [...splitSizes];

      const bodyEl = document.querySelector('.terminal-panes-container') as HTMLElement | null;
      const bodyRect = bodyEl?.getBoundingClientRect();
      const containerWidth = bodyRect?.width ?? window.innerWidth;
      const containerHeight = bodyRect?.height ?? terminalHeight;

      const onMove = (ev: MouseEvent) => {
        if (!isDraggingSplit.current) return;
        const currentPos = splitDirection === 'vertical' ? ev.clientX : ev.clientY;
        const containerSize = splitDirection === 'vertical' ? containerWidth : containerHeight;
        if (containerSize <= 0) return;

        const minPx = splitDirection === 'vertical' ? MIN_PANE_WIDTH_PX : MIN_PANE_HEIGHT_PX;
        const minPct = (minPx / containerSize) * 100;

        const deltaPx = currentPos - splitDragStartPos.current;
        const start = splitDragStartSizes.current;
        const i = splitDragIndex.current;
        const a = start[i];
        const b = start[i + 1];
        if (a === undefined || b === undefined) return;

        const pair = a + b;
        const minA = minPct;
        const maxA = pair - minPct;
        let nextA = a + (deltaPx / containerSize) * 100;
        if (maxA < minA) {
          nextA = pair / 2;
        } else {
          nextA = Math.max(minA, Math.min(maxA, nextA));
        }
        const nextB = pair - nextA;

        setSplitSizes((prev) => {
          if (i >= prev.length - 1) return prev;
          const next = [...prev];
          next[i] = nextA;
          next[i + 1] = nextB;
          return next;
        });
      };

      const onUp = () => {
        isDraggingSplit.current = false;
        setIsResizingVertical(false);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = splitDirection === 'vertical' ? 'col-resize' : 'row-resize';
    },
    [splitDirection, splitSizes, terminalHeight, setIsResizingVertical],
  );

  const splitStyleForPane = useCallback(
    (paneIndex: number): CSSProperties => {
      if (splitDirection === 'none') return {};
      const property = splitDirection === 'vertical' ? 'width' : 'height';
      const value = splitSizes[paneIndex] ?? 100 / Math.max(1, splitSizes.length);
      return { [property]: `${value}%`, minWidth: 0, minHeight: 0 };
    },
    [splitDirection, splitSizes],
  );

  /* ---- Computed ---- */
  const isSplitActive = splitDirection !== 'none';

  /**
   * Whether a split button in `direction` can add a pane right now.
   *
   * - If a split in a *different* direction is active, switching direction
   *   is always allowed (it just flips the axis without adding a pane), so
   *   return true.
   * - Otherwise the direction matches (or there's no split yet): allow
   *   adding as long as we haven't hit the pane cap for that direction.
   */
  const canAddPaneForDirection = useCallback(
    (direction: 'horizontal' | 'vertical'): boolean => {
      if (isSplitActive && splitDirection !== direction) return true;
      return panes.length < computeMaxPanes(direction);
    },
    [computeMaxPanes, isSplitActive, panes.length, splitDirection],
  );

  /* ---- Listen for sprout:terminal-attach-session ---- */
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ sessionId: string; name?: string }>).detail;
      if (!detail?.sessionId) return;
      const sessionId = detail.sessionId;
      const sessionName = detail.name || `Agent: ${sessionId.substring(0, 20)}`;

      const pane = getFocusedPane();
      if (!pane) return;

      if (pane.sessions.some((s) => s.id === sessionId)) {
        switchSessionInPane(pane.id, sessionId);
        return;
      }

      const newSession: TerminalSession = {
        id: sessionId,
        name: sessionName,
        is_pinned: false,
      };
      sessionReattachIdsRef.current.set(sessionId, sessionId);
      updatePane(pane.id, (p) => ({
        ...p,
        sessions: [...p.sessions, newSession],
        activeSessionId: sessionId,
      }));
    };
    window.addEventListener('sprout:terminal-attach-session', handler as EventListener);
    return () => window.removeEventListener('sprout:terminal-attach-session', handler as EventListener);
  }, [getFocusedPane, switchSessionInPane, updatePane]);

  /* ---- Listen for sprout:terminal-action ---- */
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ action: string }>).detail;
      if (!detail?.action) return;
      if (detail.action === 'split_horizontal') {
        addSplitPane('horizontal');
      } else if (detail.action === 'split_vertical') {
        addSplitPane('vertical');
      } else if (detail.action === 'clear') {
        handleClearActivePane();
      } else if (detail.action === 'kill') {
        const pane = getFocusedPane();
        if (pane) {
          closeSessionInPane(pane.id, pane.activeSessionId);
        }
      }
    };
    window.addEventListener('sprout:terminal-action', handler as EventListener);
    return () => window.removeEventListener('sprout:terminal-action', handler as EventListener);
  }, [addSplitPane, closeSessionInPane, getFocusedPane, handleClearActivePane]);

  /* ---- Cleanup on unmount ---- */
  useEffect(() => {
    return () => {
      paneHandles.current.forEach((handle) => {
        handle?.cleanup?.();
      });
      paneHandles.current.clear();
      sessionShellsRef.current.clear();
      sessionReattachIdsRef.current.clear();
    };
  }, []);

  /* ---- Handle split drag exit cleanup ---- */
  useEffect(() => {
    return () => {
      if (isDraggingSplit.current) {
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, []);

  return {
    /* Pane data */
    panes,
    focusedPaneId,
    setFocusedPaneId,
    getFocusedPane,

    /* Split state */
    splitDirection,
    splitSizes,
    isSplitActive,
    splitStyleForPane,
    handleSplitDividerDragStart,

    /* Pane / session actions */
    addSessionToPane,
    closeSessionInPane,
    removePane,
    renameSessionInPane,
    togglePinInPane,
    switchSessionInPane,
    handleSessionTitleChange,
    handleSessionActivity,
    handlePaneExit,
    handleAttachAgentSession,
    addSplitPane,
    canAddPaneForDirection,

    /* Activity tracking */
    activitySessionIds,
    clearActivityForSession,

    /* Refs */
    paneHandlesRef: paneHandles,
    sessionShellsRef,
    sessionReattachIdsRef,

    /* Clear active pane (for the component to call) */
  };
}
