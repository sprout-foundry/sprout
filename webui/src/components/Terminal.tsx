import { Trash2, Columns2, Rows2, Plus, Check, ZoomIn, ZoomOut, Type, Copy, ChevronUp, ChevronDown, SquarePlus } from 'lucide-react';
import React, { useState, useEffect, useRef, useCallback } from 'react';
import type { CSSProperties, MouseEvent as ReactMouseEvent } from 'react';
import './Terminal.css';
import { TerminalTabBar, type TerminalSession, type AttachableSession } from '@sprout/ui';
import { ApiService, type ShellInfo } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';
import BackgroundTasks from './BackgroundTasks';
import { FONT_SIZE_DEFAULT, COPY_ON_SELECT_DEFAULT, COPY_ON_SELECT_STORAGE_KEY } from './terminalConstants';
import TerminalPane, { type TerminalPaneHandle } from './TerminalPane';

type SplitDirection = 'none' | 'horizontal' | 'vertical';

const TERMINAL_HEIGHT_MIN = 120;
const TERMINAL_HEIGHT_DEFAULT = 400;
const TERMINAL_HEIGHT_MAX_FACTOR = 100;
const TERMINAL_HEIGHT_STORAGE_KEY = 'sprout-terminal-height';

const FONT_SIZE_MIN = 8;
const FONT_SIZE_MAX = 32;
const FONT_SIZE_STORAGE_KEY = 'sprout-terminal-font-size';

// Minimum width per pane on a vertical split (panes sit side-by-side),
// and minimum height per pane on a horizontal split (panes stack).
// Used both to clamp divider drags and to compute how many panes the
// current container can fit before the +pane button disables.
const MIN_PANE_WIDTH_PX = 240;
const MIN_PANE_HEIGHT_PX = 120;

// Even with very large containers, cap the toolbar +pane button at a
// number that feels usable. 8 vertical panes is already a lot; horizontal
// is rarer.
const MAX_PANES_HARD_CAP = 8;

const evenSplit = (n: number): number[] => Array.from({ length: n }, () => 100 / n);

const redistributeAfterRemove = (sizes: number[], removedIndex: number): number[] => {
  if (sizes.length <= 1) return [100];
  const next = sizes.filter((_, i) => i !== removedIndex);
  const total = next.reduce((acc, v) => acc + v, 0);
  if (total <= 0) return evenSplit(next.length);
  return next.map((v) => (v / total) * 100);
};

const clampTerminalHeight = (value: number): number => {
  if (!Number.isFinite(value)) return TERMINAL_HEIGHT_DEFAULT;
  return Math.max(TERMINAL_HEIGHT_MIN, Math.min(window.innerHeight - TERMINAL_HEIGHT_MAX_FACTOR, value));
};

const clampFontSize = (value: number): number => {
  if (!Number.isFinite(value)) return FONT_SIZE_DEFAULT;
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, value));
};

interface TerminalPaneData {
  id: string;
  sessions: TerminalSession[];
  activeSessionId: string;
}

interface TerminalProps {
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
}

function Terminal({
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand,
}: TerminalProps): JSX.Element {
  const getCollapsedHeight = useCallback(() => {
    if (typeof window === 'undefined') return 42;
    return window.innerWidth <= 768 ? 34 : 42;
  }, []);

  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [hasActivated, setHasActivated] = useState(externalIsExpanded);
  const [terminalHeight, setTerminalHeight] = useState<number>(() => {
    if (typeof window === 'undefined') return TERMINAL_HEIGHT_DEFAULT;
    try {
      const stored = localStorage.getItem(TERMINAL_HEIGHT_STORAGE_KEY);
      return stored ? clampTerminalHeight(Number(stored)) : TERMINAL_HEIGHT_DEFAULT;
    } catch (err) {
      debugLog('[Terminal] failed to read terminal height from localStorage:', err);
      return TERMINAL_HEIGHT_DEFAULT;
    }
  });
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  const [collapsedHeight, setCollapsedHeight] = useState(getCollapsedHeight);

  // Shell selection state
  const [availableShells, setAvailableShells] = useState<ShellInfo[]>([]);
  const [shellsLoaded, setShellsLoaded] = useState(false);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);
  const [showShellMenu, setShowShellMenu] = useState(false);
  const shellPickerRef = useRef<HTMLDivElement>(null);
  const sessionShellsRef = useRef<Map<string, string | null>>(new Map());
  const sessionReattachIdsRef = useRef<Map<string, string | null>>(new Map());

  // Attachable sessions
  const [attachableSessions, setAttachableSessions] = useState<AttachableSession[]>([]);
  const isFetchingSessionsRef = useRef(false);

  // Font size
  const [fontSize, setFontSize] = useState<number>(() => {
    if (typeof window === 'undefined') return FONT_SIZE_DEFAULT;
    try {
      const stored = localStorage.getItem(FONT_SIZE_STORAGE_KEY);
      const parsed = stored ? Number(stored) : FONT_SIZE_DEFAULT;
      return clampFontSize(parsed);
    } catch (err) {
      debugLog('[Terminal] failed to read font size from localStorage:', err);
      return FONT_SIZE_DEFAULT;
    }
  });

  // Copy-on-select
  const [copyOnSelect, setCopyOnSelect] = useState<boolean>(() => {
    if (typeof window === 'undefined') return COPY_ON_SELECT_DEFAULT;
    try {
      const stored = localStorage.getItem(COPY_ON_SELECT_STORAGE_KEY);
      return stored !== null ? stored === 'true' : COPY_ON_SELECT_DEFAULT;
    } catch (err) {
      debugLog('[Terminal] failed to read copy-on-select from localStorage:', err);
      return COPY_ON_SELECT_DEFAULT;
    }
  });

  // Split state
  const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
  const splitDirectionRef = useRef<SplitDirection>('none');
  const [splitSizes, setSplitSizes] = useState<number[]>([100]);

  // Pane-based session state
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

  // Tracks which sessions have shown new output since the user last
  // looked at them (their tab wasn't the active one in its pane).
  const [activitySessionIds, setActivitySessionIds] = useState<Set<string>>(() => new Set());
  // Tracks sessions whose name the user has explicitly renamed. Once
  // a session is in this set, OSC title changes won't overwrite the
  // user's chosen name.
  const manuallyRenamedSessions = useRef<Set<string>>(new Set());

  const hasMountedRef = useRef(false);
  const isDraggingVertical = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);

  const isDraggingSplit = useRef(false);
  const splitDragStartPos = useRef(0);
  const splitDragStartSizes = useRef<number[]>([100]);
  const splitDragIndex = useRef(0);

  const paneHandles = useRef<Map<string, TerminalPaneHandle | null>>(new Map());

  // Keep panesRef in sync
  useEffect(() => {
    panesRef.current = panes;
  }, [panes]);

  useEffect(() => {
    setIsExpanded(externalIsExpanded);
    if (externalIsExpanded) {
      setHasActivated(true);
      window.dispatchEvent(new CustomEvent('sprout-terminal-expand'));
    }
  }, [externalIsExpanded]);

  useEffect(() => {
    const reservedHeight = isExpanded ? terminalHeight : collapsedHeight;
    document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${reservedHeight}px`);
    return () => {
      document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${collapsedHeight}px`);
    };
  }, [collapsedHeight, isExpanded, terminalHeight]);

  useEffect(() => {
    if (typeof window === 'undefined') return undefined;

    const updateCollapsedHeight = () => {
      setCollapsedHeight(getCollapsedHeight());
    };

    updateCollapsedHeight();
    window.addEventListener('resize', updateCollapsedHeight);
    return () => window.removeEventListener('resize', updateCollapsedHeight);
  }, [getCollapsedHeight]);

  useEffect(() => {
    let cancelled = false;
    ApiService.getInstance()
      .getAvailableShells()
      .then((res) => {
        if (cancelled) return;
        setAvailableShells(res.shells || []);
        const defaultShell = res.shells.find((s) => s.default) || res.shells[0];
        if (defaultShell) {
          setSelectedShell(defaultShell.name);
        }
        setShellsLoaded(true);
      })
      .catch((err) => {
        debugLog('[Terminal] Failed to load available shells:', err);
        notificationBus.notify('warning', 'Terminal', 'Failed to load available shells: ' + String(err));
        setShellsLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const fetchAttachableSessions = useCallback(async () => {
    if (isFetchingSessionsRef.current) return;
    isFetchingSessionsRef.current = true;
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      }
      const data = await response.json();
      interface RawSession {
        id: string;
        name?: string;
        status?: string;
      }
      const rawSessions: RawSession[] = data?.sessions || [];
      const sessions: AttachableSession[] = rawSessions.map((s) => ({
        id: s.id,
        name: s.name || s.id,
        status: s.status === 'active' ? 'active' : 'inactive',
      }));
      setAttachableSessions(sessions);
    } catch (err) {
      debugLog('[Terminal] Failed to fetch attachable sessions:', err);
      setAttachableSessions([]);
    } finally {
      isFetchingSessionsRef.current = false;
    }
  }, []);

  useEffect(() => {
    fetchAttachableSessions();
    const intervalId = setInterval(() => {
      if (isExpanded) {
        fetchAttachableSessions();
      }
    }, 5000);
    return () => {
      clearInterval(intervalId);
    };
  }, [isExpanded, fetchAttachableSessions]);

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (
        detail?.type === 'terminal_output' ||
        detail?.type === 'pty_exit' ||
        detail?.type === 'agent_session_update'
      ) {
        fetchAttachableSessions();
      }
    };
    window.addEventListener('sprout:wsevent', handler as EventListener);
    return () => window.removeEventListener('sprout:wsevent', handler as EventListener);
  }, [fetchAttachableSessions]);

  useEffect(() => {
    if (!showShellMenu) return;
    const handleClick = (e: MouseEvent) => {
      if (shellPickerRef.current && !shellPickerRef.current.contains(e.target as Node)) {
        setShowShellMenu(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowShellMenu(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [showShellMenu]);

  const toggleExpanded = useCallback(() => {
    setIsExpanded((prev) => {
      const next = !prev;
      if (next) {
        setHasActivated(true);
      }
      onToggleExpand?.(next);
      return next;
    });
  }, [onToggleExpand]);

  const zoomIn = useCallback(() => {
    setFontSize((prev) => {
      const next = Math.min(FONT_SIZE_MAX, prev + 1);
      try {
        localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(next));
      } catch (err) {
        debugLog('[Terminal] failed to persist font size:', err);
      }
      return next;
    });
  }, []);

  const zoomOut = useCallback(() => {
    setFontSize((prev) => {
      const next = Math.max(FONT_SIZE_MIN, prev - 1);
      try {
        localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(next));
      } catch (err) {
        debugLog('[Terminal] failed to persist font size:', err);
      }
      return next;
    });
  }, []);

  const resetFontSize = useCallback(() => {
    setFontSize(FONT_SIZE_DEFAULT);
    try {
      localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(FONT_SIZE_DEFAULT));
    } catch (err) {
      debugLog('[Terminal] failed to persist font size:', err);
    }
  }, []);

  const toggleCopyOnSelect = useCallback(() => {
    setCopyOnSelect((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(COPY_ON_SELECT_STORAGE_KEY, String(next));
      } catch (err) {
        debugLog('[Terminal] failed to persist copy-on-select:', err);
      }
      return next;
    });
  }, []);

  const getFocusedPane = useCallback((): TerminalPaneData | null => {
    return panesRef.current.find((p) => p.id === focusedPaneId) ?? null;
  }, [focusedPaneId]);

  const clearActivePane = useCallback(() => {
    const pane = getFocusedPane();
    if (pane) {
      const handle = paneHandles.current.get(pane.activeSessionId);
      handle?.clear();
    }
  }, [getFocusedPane]);

  // Helper: Update a pane immutably
  const updatePane = useCallback((paneId: string, updater: (pane: TerminalPaneData) => TerminalPaneData) => {
    setPanes((prev) => prev.map((p) => (p.id === paneId ? updater(p) : p)));
  }, []);

  // Session management scoped to panes
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
    [fetchAttachableSessions, getFocusedPane, updatePane],
  );

  // Drop a session id from the per-Terminal tracking sets so closed/
  // restarted sessions don't leak into them.
  const forgetSession = useCallback((sessionId: string) => {
    manuallyRenamedSessions.current.delete(sessionId);
    setActivitySessionIds((prev) => {
      if (!prev.has(sessionId)) return prev;
      const next = new Set(prev);
      next.delete(sessionId);
      return next;
    });
  }, []);

  const removePane = useCallback(
    (paneId: string) => {
      const all = panesRef.current;
      const idx = all.findIndex((p) => p.id === paneId);
      if (idx === -1) return;
      if (all.length <= 1) return; // Last pane never removes — handled by restart elsewhere.

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

      // Focus the neighbor closest to the removed pane.
      if (focusedPaneId === paneId) {
        const neighbor = remainingPanes[Math.max(0, idx - 1)];
        if (neighbor) setFocusedPaneId(neighbor.id);
      }

      // Down to one pane → exit split mode entirely.
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

      // Closing the only session in this pane. With more than one pane
      // open, this removes the whole pane; with only one pane, it would
      // leave the terminal empty so we refuse — the user can run `exit`
      // to trigger the restart path via pty_exit.
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

      const closedIdx = pane.sessions.findIndex((s) => s.id === sessionId);
      const remaining = pane.sessions.filter((s) => s.id !== sessionId);
      // Browser-tab convention: when the active tab is closed, focus moves
      // to the tab on its right; if it was the last, to the new last tab.
      const newActive =
        pane.activeSessionId === sessionId
          ? remaining[Math.min(closedIdx, remaining.length - 1)].id
          : pane.activeSessionId;

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

  // Apply an OSC 0/2 title sequence as the tab name — but only when
  // the user hasn't manually renamed this session. A manual rename
  // pins the name and we refuse to clobber it from shell title changes.
  const handleSessionTitleChange = useCallback(
    (paneId: string, sessionId: string, title: string) => {
      if (manuallyRenamedSessions.current.has(sessionId)) return;
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: pane.sessions.map((s) =>
          s.id === sessionId && s.name !== title ? { ...s, name: title } : s,
        ),
      }));
    },
    [updatePane],
  );

  // Mark a session as having background activity. Skip when the session
  // is currently the active tab in its pane — the user is already looking
  // at it, no indicator needed.
  const handleSessionActivity = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;
      if (pane.activeSessionId === sessionId) return;
      setActivitySessionIds((prev) => {
        if (prev.has(sessionId)) return prev;
        const next = new Set(prev);
        next.add(sessionId);
        return next;
      });
    },
    [],
  );

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
      // Activating a tab clears its background-activity indicator —
      // the user is now looking at it.
      setActivitySessionIds((prev) => {
        if (!prev.has(sessionId)) return prev;
        const next = new Set(prev);
        next.delete(sessionId);
        return next;
      });
      // After switching tabs, the newly visible xterm needs a resize since it
      // was display:none (0×0) and now needs to fit the pane dimensions.
      requestAnimationFrame(() => {
        window.dispatchEvent(new Event('resize'));
      });
    },
    [updatePane],
  );

  const handlePaneExit = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;

      if (!pane.sessions.some((s) => s.id === sessionId) && !paneHandles.current.has(sessionId)) {
        debugLog('[Terminal] pty_exit for already-closed session:', sessionId);
        return;
      }

      // Multi-session pane — close just the tab and keep the pane.
      if (pane.sessions.length > 1) {
        closeSessionInPane(paneId, sessionId);
        notificationBus.notify('info', 'Terminal', 'Terminal process exited — session closed.');
        return;
      }

      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);
      forgetSession(sessionId);

      // Last session in a pane that's part of a split — drop the pane.
      if (panesRef.current.length > 1) {
        removePane(paneId);
        notificationBus.notify('info', 'Terminal', 'Split pane closed after process exited.');
        return;
      }

      // Last session in the only pane — restart with a fresh shell so
      // the terminal panel never becomes empty.
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

  // Build a fresh pane (with a single shell session) ready to be added
  // to the panes array. Centralized so toggleSplit and addPaneInDirection
  // produce structurally identical panes.
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

  // Measure the panes container and convert it into "how many panes of
  // the current direction can fit before they cross MIN_PANE_*_PX." In
  // jsdom (or before first layout) the rect reads as 0×0; we fall back
  // to the hard cap so tests can exercise the +pane path.
  const computeMaxPanes = useCallback((direction: SplitDirection): number => {
    if (direction === 'none') return 1;
    const el = document.querySelector('.terminal-panes-container') as HTMLElement | null;
    const rect = el?.getBoundingClientRect();
    const w = rect?.width ?? 0;
    const h = rect?.height ?? 0;
    if (w === 0 && h === 0) return MAX_PANES_HARD_CAP;
    const limit = direction === 'vertical'
      ? Math.floor(w / MIN_PANE_WIDTH_PX)
      : Math.floor(h / MIN_PANE_HEIGHT_PX);
    return Math.min(MAX_PANES_HARD_CAP, Math.max(2, limit));
  }, []);

  const toggleSplit = useCallback(
    (direction: 'horizontal' | 'vertical') => {
      const currentDir = splitDirectionRef.current;
      const paneCount = panesRef.current.length;

      if (currentDir === direction) {
        // Matching direction click. Preserve the legacy 1↔2 toggle: at
        // exactly 2 panes, collapse back to 1. At 3+ panes the toggle
        // would silently destroy multiple terminals, so it's a no-op —
        // users reduce via tab close on each pane.
        if (paneCount === 2) {
          const dropped = panesRef.current[1];
          dropped.sessions.forEach((s) => {
            const handle = paneHandles.current.get(s.id);
            handle?.cleanup?.();
            paneHandles.current.delete(s.id);
            sessionShellsRef.current.delete(s.id);
            sessionReattachIdsRef.current.delete(s.id);
          });
          setPanes((prev) => prev.slice(0, 1));
          setFocusedPaneId(panesRef.current[0].id);
          setSplitSizes([100]);
          splitDirectionRef.current = 'none';
          setSplitDirection('none');
        }
        return;
      }

      if (currentDir !== 'none') {
        // Switch axis without changing pane count; redistribute evenly.
        setSplitSizes(evenSplit(paneCount));
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
        return;
      }

      // Going from unsplit → 2 panes in the requested direction.
      const newPane = createPane();
      setPanes((prev) => [...prev, newPane]);
      setFocusedPaneId(newPane.id);
      setSplitSizes(evenSplit(2));
      splitDirectionRef.current = direction;
      setSplitDirection(direction);
    },
    [createPane],
  );

  const addPaneInDirection = useCallback(() => {
    const dir = splitDirectionRef.current;
    if (dir === 'none') return;
    const max = computeMaxPanes(dir);
    if (panesRef.current.length >= max) return;
    const newPane = createPane();
    const nextCount = panesRef.current.length + 1;
    setPanes((prev) => [...prev, newPane]);
    setFocusedPaneId(newPane.id);
    setSplitSizes(evenSplit(nextCount));
  }, [computeMaxPanes, createPane]);

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ action: string }>).detail;
      if (!detail?.action) return;
      if (detail.action === 'split_horizontal') {
        toggleSplit('horizontal');
      } else if (detail.action === 'split_vertical') {
        toggleSplit('vertical');
      } else if (detail.action === 'clear') {
        clearActivePane();
      } else if (detail.action === 'kill') {
        const pane = getFocusedPane();
        if (pane) {
          closeSessionInPane(pane.id, pane.activeSessionId);
        }
      }
    };
    window.addEventListener('sprout:terminal-action', handler as EventListener);
    return () => window.removeEventListener('sprout:terminal-action', handler as EventListener);
  }, [clearActivePane, closeSessionInPane, getFocusedPane, toggleSplit]);

  const handleSplitDividerDragStart = useCallback(
    (e: ReactMouseEvent, dividerIndex: number) => {
      e.preventDefault();
      isDraggingSplit.current = true;
      splitDragIndex.current = dividerIndex;
      setIsResizingVertical(true);
      splitDragStartPos.current = splitDirection === 'vertical' ? e.clientX : e.clientY;
      splitDragStartSizes.current = [...splitSizes];

      const bodyEl = document.querySelector('.terminal-panes-container');
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

        // Move only the size on either side of this divider; everything
        // else stays put. Clamp both panes against the per-pane minimum
        // so a drag can't squeeze a neighbour below MIN_PANE_*_PX.
        const pair = a + b;
        const minA = minPct;
        const maxA = pair - minPct;
        let nextA = a + (deltaPx / containerSize) * 100;
        if (maxA < minA) {
          // Container too small to honor both minimums — split the
          // available room evenly instead of producing a NaN-clamped
          // value.
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
    [splitDirection, splitSizes, terminalHeight],
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

  const handleVerticalResizeStart = useCallback(
    (e: ReactMouseEvent) => {
      e.preventDefault();
      isDraggingVertical.current = true;
      setIsResizingVertical(true);
      dragStartY.current = e.clientY;
      dragStartHeight.current = terminalHeight;

      const onMove = (ev: MouseEvent) => {
        if (!isDraggingVertical.current) return;
        const delta = dragStartY.current - ev.clientY;
        const next = clampTerminalHeight(dragStartHeight.current + delta);
        setTerminalHeight(next);
      };

      const onUp = () => {
        isDraggingVertical.current = false;
        setIsResizingVertical(false);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        setTerminalHeight((prev) => {
          try {
            localStorage.setItem(TERMINAL_HEIGHT_STORAGE_KEY, String(Math.round(prev)));
          } catch (err) {
            debugLog('[Terminal] failed to persist terminal height:', err);
          }
          return prev;
        });
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
    },
    [terminalHeight],
  );

  useEffect(() => {
    if (!hasMountedRef.current) {
      hasMountedRef.current = true;
      const timer = setTimeout(() => {
        hasMountedRef.current = false;
      }, 300);
      return () => clearTimeout(timer);
    }
  }, []);

  useEffect(() => {
    return () => {
      if (isDraggingSplit.current || isDraggingVertical.current) {
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, []);

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

  const isSplitActive = splitDirection !== 'none';

  const focusedPane = panes.find((p) => p.id === focusedPaneId) ?? panes[0];
  const focusedSession = focusedPane?.sessions.find((s) => s.id === focusedPane.activeSessionId);
  const totalSessions = panes.reduce((acc, p) => acc + p.sessions.length, 0);

  // Recomputed each render so the +pane button disables the instant the
  // container becomes too small for another MIN_PANE_*_PX-sized pane.
  const maxPanesForCurrentSplit = isSplitActive ? computeMaxPanes(splitDirection) : 1;
  const canAddPane = isSplitActive && panes.length < maxPanesForCurrentSplit;

  return (
    <div
      className={[
        'terminal-container',
        isExpanded ? 'expanded' : 'collapsed',
        hasMountedRef.current ? 'initial-mount' : '',
        isResizingVertical ? 'resizing' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      style={{ height: `${isExpanded ? terminalHeight : collapsedHeight}px` }}
    >
      {isExpanded && (
        <div
          className="terminal-resize-handle"
          onMouseDown={handleVerticalResizeStart}
          title="Drag to resize terminal"
        />
      )}

      {/* Header */}
      <div className="terminal-header">
        <div className="terminal-title-row" onClick={toggleExpanded}>
          <div className="terminal-title">
            <span className="terminal-icon">$</span>
            <span>Terminal</span>
            {focusedSession && (
              <span className="terminal-title-session" title={focusedSession.name}>
                <span className="terminal-title-separator">—</span>
                <span className="terminal-title-session-name">{focusedSession.name}</span>
                {totalSessions > 1 && <span className="terminal-title-count">{totalSessions}</span>}
              </span>
            )}
          </div>
          <div className="terminal-controls" onClick={(e) => e.stopPropagation()}>
            <button
              className="terminal-btn font-btn"
              onClick={zoomOut}
              title="Zoom out (decrease font size)"
              aria-label="Zoom out"
            >
              <ZoomOut size={16} />
            </button>
            <button
              className="terminal-btn font-btn"
              onClick={zoomIn}
              title="Zoom in (increase font size)"
              aria-label="Zoom in"
            >
              <ZoomIn size={16} />
            </button>
            <button
              className="terminal-btn font-btn"
              onClick={resetFontSize}
              title={`Reset font size (${fontSize}px)`}
              aria-label="Reset font size"
            >
              <Type size={14} />
            </button>
            <button
              className="terminal-btn clear-btn"
              onClick={clearActivePane}
              title="Clear terminal"
              aria-label="Clear terminal"
            >
              <Trash2 size={16} />
            </button>
            <button
              className={`terminal-btn split-btn ${splitDirection === 'vertical' ? 'split-btn-active' : ''}`}
              onClick={() => toggleSplit('vertical')}
              title={splitDirection === 'vertical' ? 'Unsplit terminal' : 'Split terminal vertically'}
              aria-label={splitDirection === 'vertical' ? 'Unsplit terminal' : 'Split terminal vertically'}
              aria-pressed={splitDirection === 'vertical'}
            >
              <Columns2 size={16} />
            </button>
            <button
              className={`terminal-btn split-btn ${splitDirection === 'horizontal' ? 'split-btn-active' : ''}`}
              onClick={() => toggleSplit('horizontal')}
              title={splitDirection === 'horizontal' ? 'Unsplit terminal' : 'Split terminal horizontally'}
              aria-label={splitDirection === 'horizontal' ? 'Unsplit terminal' : 'Split terminal horizontally'}
              aria-pressed={splitDirection === 'horizontal'}
            >
              <Rows2 size={16} />
            </button>
            {isSplitActive && (
              <button
                className="terminal-btn add-pane-btn"
                onClick={addPaneInDirection}
                disabled={!canAddPane}
                title={
                  canAddPane
                    ? `Add ${splitDirection === 'vertical' ? 'vertical' : 'horizontal'} pane`
                    : 'No room for another pane — resize terminal or close one first'
                }
                aria-label="Add terminal pane"
              >
                <SquarePlus size={16} />
              </button>
            )}
            <button
              className={`terminal-btn copy-on-select-btn ${copyOnSelect ? 'copy-on-select-btn-active' : ''}`}
              onClick={toggleCopyOnSelect}
              title={`Copy on select: ${copyOnSelect ? 'enabled' : 'disabled'}`}
              aria-label={`Toggle copy on select: currently ${copyOnSelect ? 'enabled' : 'disabled'}`}
              aria-pressed={copyOnSelect}
            >
              <Copy size={16} />
            </button>
            <button
              className="terminal-btn toggle-btn"
              onClick={toggleExpanded}
              title={isExpanded ? 'Collapse terminal (Ctrl+`)' : 'Expand terminal (Ctrl+`)'}
              aria-label={isExpanded ? 'Collapse terminal' : 'Expand terminal'}
              aria-expanded={isExpanded}
            >
              {isExpanded ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
            </button>
          </div>
        </div>
      </div>

      {/* Body */}
      <div className="terminal-body">
        <div className={`terminal-panes-container ${isSplitActive ? `terminal-split-${splitDirection}` : ''}`}>
          {panes.map((pane, index) => (
            <React.Fragment key={pane.id}>
              <div
                className={`terminal-pane-wrapper${isSplitActive && pane.id === focusedPaneId ? ' terminal-pane-wrapper--focused' : ''}`}
                style={splitStyleForPane(index)}
                onMouseDown={() => setFocusedPaneId(pane.id)}
              >
                <div className="terminal-pane-tab-bar">
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <TerminalTabBar
                      sessions={pane.sessions}
                      activeSessionId={pane.activeSessionId}
                      onSwitch={(id) => switchSessionInPane(pane.id, id)}
                      onClose={(id) => closeSessionInPane(pane.id, id)}
                      onRename={(id, name) => renameSessionInPane(pane.id, id, name)}
                      onTogglePin={(id) => togglePinInPane(pane.id, id)}
                      attachableSessions={attachableSessions}
                      onAttachSession={handleAttachAgentSession}
                      allowCloseLastTab={panes.length > 1}
                      activitySessionIds={activitySessionIds}
                    />
                  </div>
                  {index === 0 && <BackgroundTasks />}
                  <div className="shell-picker-dropdown" ref={focusedPaneId === pane.id ? shellPickerRef : null}>
                    <button
                      className="terminal-tab-new shell-picker-btn"
                      onClick={() => {
                        if (availableShells.length <= 1) {
                          addSessionToPane(pane.id);
                        } else {
                          setShowShellMenu((prev) => !prev);
                        }
                      }}
                      title="New terminal session"
                      type="button"
                      aria-label="New terminal session"
                      aria-haspopup={availableShells.length > 1}
                      aria-expanded={showShellMenu && focusedPaneId === pane.id}
                    >
                      <Plus size={14} />
                      {shellsLoaded && selectedShell && (
                        <span className="shell-picker-current">{selectedShell}</span>
                      )}
                    </button>
                    {showShellMenu && shellsLoaded && availableShells.length > 1 && focusedPaneId === pane.id && (
                      <div className="shell-picker-menu" role="menu">
                        <div className="shell-picker-header">New Terminal</div>
                        {availableShells.map((shell) => (
                          <button
                            key={shell.name}
                            className="shell-picker-item"
                            onClick={() => {
                              setSelectedShell(shell.name);
                              setShowShellMenu(false);
                              addSessionToPane(pane.id, shell.name);
                            }}
                            type="button"
                            role="menuitem"
                            title={shell.path}
                          >
                            {shell.default && <Check size={12} className="shell-default-indicator" />}
                            {!shell.default && <span className="shell-default-spacer" />}
                            <span className="shell-name">{shell.name}</span>
                            <span className="shell-path">{shell.path}</span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
                {pane.sessions.map((session) => {
                  const isActiveSession = session.id === pane.activeSessionId;
                  return (
                    <div
                      key={session.id}
                      style={{
                        display: isActiveSession ? 'flex' : 'none',
                        flex: '1 1 0%',
                        minWidth: 0,
                        minHeight: 0,
                        flexDirection: 'column',
                      }}
                    >
                      <TerminalPane
                        ref={(handle) => {
                          if (handle) {
                            paneHandles.current.set(session.id, handle);
                          } else {
                            paneHandles.current.delete(session.id);
                          }
                        }}
                        isActive={hasActivated || isExpanded}
                        shouldFocus={pane.id === focusedPaneId && isActiveSession}
                        isConnected={isConnected}
                        showCloseButton={false}
                        preferredShell={sessionShellsRef.current.get(session.id) ?? null}
                        reattachSessionId={sessionReattachIdsRef.current.get(session.id) ?? null}
                        fontSize={fontSize}
                        copyOnSelect={copyOnSelect}
                        onProcessExit={() => handlePaneExit(pane.id, session.id)}
                        onTitleChange={(title) => handleSessionTitleChange(pane.id, session.id, title)}
                        onActivity={() => handleSessionActivity(pane.id, session.id)}
                      />
                    </div>
                  );
                })}
              </div>
              {isSplitActive && index < panes.length - 1 && (
                <div
                  className={`terminal-split-divider terminal-split-divider-${splitDirection}`}
                  onMouseDown={(e) => handleSplitDividerDragStart(e, index)}
                />
              )}
            </React.Fragment>
          ))}
        </div>
      </div>
    </div>
  );
}

export default Terminal;
