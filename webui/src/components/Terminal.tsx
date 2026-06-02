import { Trash2, Columns2, Rows2, Plus, Check, ZoomIn, ZoomOut, Type, Copy } from 'lucide-react';
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
  const [splitSizes, setSplitSizes] = useState<[number, number]>([50, 50]);

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

  const hasMountedRef = useRef(false);
  const isDraggingVertical = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);

  const isDraggingSplit = useRef(false);
  const splitDragStartPos = useRef(0);
  const splitDragStartSizes = useRef<[number, number]>([50, 50]);

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

  const closeSessionInPane = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane || pane.sessions.length <= 1) return;

      const handle = paneHandles.current.get(sessionId);
      handle?.cleanup?.();
      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);

      const remaining = pane.sessions.filter((s) => s.id !== sessionId);
      const newActive = pane.activeSessionId === sessionId ? remaining[0].id : pane.activeSessionId;

      updatePane(paneId, (p) => ({
        ...p,
        sessions: remaining,
        activeSessionId: newActive,
      }));

      // Auto-close secondary pane if it has no sessions
      if (panesRef.current.length > 1 && remaining.length === 0) {
        setPanes((prev) => prev.filter((p) => p.id !== paneId));
        splitDirectionRef.current = 'none';
        setSplitDirection('none');
      }
    },
    [updatePane],
  );

  const renameSessionInPane = useCallback(
    (paneId: string, sessionId: string, name: string) => {
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: pane.sessions.map((s) => (s.id === sessionId ? { ...s, name } : s)),
      }));
    },
    [updatePane],
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

      const isSecondaryPane = panesRef.current.length > 1 && panesRef.current[1]?.id === paneId;
      const isOnlyTwoPanes = panesRef.current.length === 2;

      // Case 1: Multi-session pane — close the specific tab
      if (pane.sessions.length > 1) {
        closeSessionInPane(paneId, sessionId);
        notificationBus.notify('info', 'Terminal', 'Terminal process exited — session closed.');
        return;
      }

      // Clean up the exited session's resources
      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);

      // Case 2: Single-session secondary pane with only 2 panes — auto-close the split
      if (isSecondaryPane && isOnlyTwoPanes) {
        // Remove the secondary pane entirely
        setPanes((prev) => prev.filter((p) => p.id !== paneId));
        splitDirectionRef.current = 'none';
        setSplitDirection('none');
        setFocusedPaneId(panesRef.current[0].id);
        notificationBus.notify('info', 'Terminal', 'Split pane closed after process exited.');
        return;
      }

      // Case 3: Single-session pane — auto-restart with a fresh session
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

      if (isSecondaryPane) {
        notificationBus.notify('info', 'Terminal', 'Terminal process exited in split pane — restarted.');
      } else {
        notificationBus.notify('info', 'Terminal', 'Terminal process exited — restarted with fresh shell.');
      }
    },
    [closeSessionInPane, selectedShell, updatePane],
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

  const toggleSplit = useCallback(
    (direction: 'horizontal' | 'vertical') => {
      const currentDir = splitDirectionRef.current;

      if (currentDir === direction) {
        // Same direction → toggle off (unsplit)
        if (panesRef.current.length > 1) {
          const secondaryPane = panesRef.current[1];
          secondaryPane.sessions.forEach((s) => {
            const handle = paneHandles.current.get(s.id);
            handle?.cleanup?.();
            paneHandles.current.delete(s.id);
            sessionShellsRef.current.delete(s.id);
            sessionReattachIdsRef.current.delete(s.id);
          });
          setPanes((prev) => prev.slice(0, 1));
          setFocusedPaneId(panesRef.current[0].id);
        }
        splitDirectionRef.current = 'none';
        setSplitDirection('none');
      } else if (currentDir !== 'none') {
        // Different direction → switch direction, keep existing panes
        setSplitSizes([50, 50]);
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
      } else {
        // Not split → create new pane with session
        paneIdCounter.current += 1;
        const paneId = `pane-${paneIdCounter.current}`;
        const splitSessionNum = sessionCounterRef.current++;
        const sessionId = `session-${splitSessionNum}`;
        const newPane: TerminalPaneData = {
          id: paneId,
          sessions: [{ id: sessionId, name: `Session ${splitSessionNum}`, is_pinned: false }],
          activeSessionId: sessionId,
        };
        sessionShellsRef.current.set(sessionId, selectedShell ?? null);
        setPanes((prev) => [...prev, newPane]);
        setFocusedPaneId(paneId);
        setSplitSizes([50, 50]);
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
      }
    },
    [selectedShell],
  );

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
    (e: ReactMouseEvent) => {
      e.preventDefault();
      isDraggingSplit.current = true;
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

        const deltaPx = currentPos - splitDragStartPos.current;
        const startSizes = splitDragStartSizes.current;
        const newFirst = startSizes[0] + (deltaPx / containerSize) * 100;
        const clamped = Math.max(20, Math.min(80, newFirst));
        setSplitSizes([clamped, 100 - clamped]);
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
      const value = paneIndex === 0 ? `${splitSizes[0]}%` : `${splitSizes[1]}%`;
      return { [property]: value, minWidth: 0, minHeight: 0 };
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
              title={isExpanded ? 'Collapse terminal' : 'Expand terminal'}
              aria-label={isExpanded ? 'Collapse terminal' : 'Expand terminal'}
              aria-expanded={isExpanded}
            >
              {isExpanded ? '▼' : '▲'}
            </button>
          </div>
        </div>
      </div>

      {/* Background Tasks */}
      {isExpanded && <BackgroundTasks />}

      {/* Body */}
      <div className="terminal-body">
        <div className={`terminal-panes-container ${isSplitActive ? `terminal-split-${splitDirection}` : ''}`}>
          {panes.map((pane, index) => (
            <React.Fragment key={pane.id}>
              <div
                className="terminal-pane-wrapper"
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
                      onCreate={() => addSessionToPane(pane.id)}
                    />
                  </div>
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
                        <span style={{ fontSize: 10, marginLeft: 3, opacity: 0.7 }}>{selectedShell}</span>
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
                            {!shell.default && <span style={{ width: 12, flexShrink: 0 }} />}
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
                      />
                    </div>
                  );
                })}
              </div>
              {index === 0 && isSplitActive && (
                <div
                  className={`terminal-split-divider terminal-split-divider-${splitDirection}`}
                  onMouseDown={handleSplitDividerDragStart}
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
