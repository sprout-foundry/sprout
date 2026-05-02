import { useState, useEffect, useRef, useCallback } from 'react';
import type { CSSProperties, MouseEvent as ReactMouseEvent } from 'react';
import { Trash2, Columns2, Rows2, Plus, Check, ZoomIn, ZoomOut, Type } from 'lucide-react';
import './Terminal.css';
import TerminalPane, { type TerminalPaneHandle } from './TerminalPane';
import { TerminalTabBar, type TerminalSession, type AttachableSession } from '@sprout/ui';
import { ApiService, type ShellInfo } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';
import { FONT_SIZE_DEFAULT } from './terminalConstants';
import BackgroundTasks from './BackgroundTasks';

type SplitDirection = 'none' | 'horizontal' | 'vertical';

const TERMINAL_HEIGHT_MIN = 120;
const TERMINAL_HEIGHT_DEFAULT = 400;
const TERMINAL_HEIGHT_MAX_FACTOR = 100; // max = innerHeight - this
const TERMINAL_HEIGHT_STORAGE_KEY = 'sprout-terminal-height';

// Font size constants and storage
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
    if (typeof window === 'undefined') {
      return 42;
    }
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

  // ── Shell selection state ─────────────────────────────────────────────────
  const [availableShells, setAvailableShells] = useState<ShellInfo[]>([]);
  const [shellsLoaded, setShellsLoaded] = useState(false);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);
  const [showShellMenu, setShowShellMenu] = useState(false);
  const shellPickerRef = useRef<HTMLDivElement>(null);
  // Track which shell each session should use (map: sessionId → shell name | null)
  const sessionShellsRef = useRef<Map<string, string | null>>(new Map());
  // Track which sessions need reattach to existing PTY (map: sessionId → sessionId)
  const sessionReattachIdsRef = useRef<Map<string, string | null>>(new Map());

  // ── Attachable sessions state ───────────────────────────────────────────────
  const [attachableSessions, setAttachableSessions] = useState<AttachableSession[]>([]);
  const isFetchingSessionsRef = useRef(false);

  // ── Font size state ───────────────────────────────────────────────────────
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

  // ── Split state ──────────────────────────────────────────────────────────
  const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
  const splitDirectionRef = useRef<SplitDirection>('none');
  const [splitSizes, setSplitSizes] = useState<[number, number]>([50, 50]);
  const [secondarySessionId, setSecondarySessionId] = useState<string | null>(null);
  const secondarySessionIdRef = useRef<string | null>(null);

  // Tabbed session state — derive initial IDs together
  const paneIdCounter = useRef(0);
  const [sessionState] = useState(() => {
    paneIdCounter.current += 1;
    const id = `pane-${paneIdCounter.current}`;
    return { initialId: id, initialSessions: [{ id, name: 'Session 1', is_pinned: false }] };
  });
  const [sessions, setSessions] = useState<TerminalSession[]>(sessionState.initialSessions);
  const [activeSessionId, setActiveSessionId] = useState(sessionState.initialId);
  const sessionCounterRef = useRef(1);
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;

  const hasMountedRef = useRef(false);
  const isDraggingVertical = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);

  // Split divider drag refs
  const isDraggingSplit = useRef(false);
  const splitDragStartPos = useRef(0);
  const splitDragStartSizes = useRef<[number, number]>([50, 50]);

  // Keyed refs to each pane's imperative handle
  const paneHandles = useRef<Map<string, TerminalPaneHandle | null>>(new Map());
  const activeSessionIdRef = useRef(activeSessionId);
  activeSessionIdRef.current = activeSessionId;

  useEffect(() => {
    setIsExpanded(externalIsExpanded);
    if (externalIsExpanded) {
      setHasActivated(true);
      // Dispatch a custom event to trigger terminal resize in all panes
      // This fixes the issue where terminal doesn't fill space after reopening
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
    if (typeof window === 'undefined') {
      return undefined;
    }

    const updateCollapsedHeight = () => {
      setCollapsedHeight(getCollapsedHeight());
    };

    updateCollapsedHeight();
    window.addEventListener('resize', updateCollapsedHeight);
    return () => window.removeEventListener('resize', updateCollapsedHeight);
  }, [getCollapsedHeight]);

  // ── Fetch available shells on mount ────────────────────────────────────────
  useEffect(() => {
    let cancelled = false;
    ApiService.getInstance()
      .getAvailableShells()
      .then((res) => {
        if (cancelled) return;
        setAvailableShells(res.shells || []);
        // Default to the server-specified default shell
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

  // Fetch attachable agent sessions
  const fetchAttachableSessions = useCallback(async () => {
    if (isFetchingSessionsRef.current) return;
    isFetchingSessionsRef.current = true;
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      }
      const data = await response.json();
      interface RawSession { id: string; name?: string; status?: string }
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

  // ── Poll for attachable agent sessions ─────────────────────────────────────
  useEffect(() => {
    // Initial fetch
    fetchAttachableSessions();

    // Poll every 5 seconds when terminal is expanded
    const intervalId = setInterval(() => {
      if (isExpanded) {
        fetchAttachableSessions();
      }
    }, 5000);

    return () => {
      clearInterval(intervalId);
    };
  }, [isExpanded, fetchAttachableSessions]);

  // Listen for WebSocket events to refresh attachable sessions
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      // Refresh if it's a terminal-related event
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

  // Close shell picker when clicking outside or pressing Escape
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

  const clearActivePane = useCallback(() => {
    const handle = paneHandles.current.get(activeSessionIdRef.current);
    handle?.clear();
  }, []);

  // ── Session management ────────────────────────────────────────────────────

  const newPaneId = useCallback(() => {
    paneIdCounter.current += 1;
    return `pane-${paneIdCounter.current}`;
  }, []);

  const addSession = useCallback(
    (shell?: string | null) => {
      sessionCounterRef.current += 1;
      const id = newPaneId();
      const newSession: TerminalSession = {
        id,
        name: `Session ${sessionCounterRef.current}`,
        is_pinned: false,
      };
      // Track which shell this session should use
      sessionShellsRef.current.set(id, shell ?? selectedShell ?? null);
      setSessions((prev) => [...prev, newSession]);
      setActiveSessionId(id);
    },
    [newPaneId, selectedShell],
  );

  const handleShellPickerSelect = useCallback(
    (shellName: string) => {
      setSelectedShell(shellName);
      setShowShellMenu(false);
      addSession(shellName);
    },
    [addSession],
  );

  // Handle attaching an agent session
  const handleAttachAgentSession = useCallback(
    async (sessionId: string, name: string) => {
      // Optimistically remove from the list to prevent double-clicks
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

        // Fire a custom event to notify the terminal to create a new tab
        window.dispatchEvent(
          new CustomEvent('sprout:terminal-attach-session', {
            detail: { sessionId, name },
          }),
        );

        // Refresh the attachable sessions list
        await fetchAttachableSessions();
      } catch (err) {
        debugLog('[Terminal] Failed to attach agent session:', err);
        notificationBus.notify('warning', 'Terminal', 'Failed to attach session: ' + String(err));
        // Rollback: re-fetch to restore the session in the list
        fetchAttachableSessions();
      }
    },
    [fetchAttachableSessions],
  );

  // Clear all split state (used by closeSecondaryPane and closeSession)
  const clearSplitState = useCallback(() => {
    secondarySessionIdRef.current = null;
    setSecondarySessionId(null);
    splitDirectionRef.current = 'none';
    setSplitDirection('none');
  }, []);

  // Close secondary pane (called directly or when secondary session is closed)
  const closeSecondaryPane = useCallback(() => {
    // Clean up the secondary pane's WebSocket connection before clearing state
    if (secondarySessionIdRef.current) {
      const handle = paneHandles.current.get(secondarySessionIdRef.current);
      handle?.cleanup?.();
      paneHandles.current.delete(secondarySessionIdRef.current);
      sessionShellsRef.current.delete(secondarySessionIdRef.current);
      sessionReattachIdsRef.current.delete(secondarySessionIdRef.current);
    }
    clearSplitState();
  }, [clearSplitState]);

  const closeSession = useCallback(
    (id: string) => {
      // Synchronously compute what remains using the ref (always up-to-date)
      const remaining = sessionsRef.current.filter((s) => s.id !== id);
      if (remaining.length === 0) return; // guard: never remove the last session

      // Clean up the pane's WebSocket connection
      const handle = paneHandles.current.get(id);
      handle?.cleanup?.();
      paneHandles.current.delete(id);
      sessionShellsRef.current.delete(id);
      sessionReattachIdsRef.current.delete(id);

      // If closing the session that's in the secondary pane, unsplit
      if (secondarySessionIdRef.current === id) {
        clearSplitState();
      }

      // Batch state updates
      setSessions(remaining);
      setActiveSessionId((current) => (current !== id ? current : remaining[0].id));

      // Auto-unsplit if only one session would remain and split is still active
      // (could happen if the closed session was NOT the secondary one but was one of >2)
      if (remaining.length === 1 && splitDirectionRef.current !== 'none') {
        clearSplitState();
      }
    },
    [clearSplitState],
  );

  const renameSession = useCallback((id: string, name: string) => {
    setSessions((prev) => prev.map((s) => (s.id === id ? { ...s, name } : s)));
  }, []);

  const togglePinSession = useCallback((id: string) => {
    setSessions((prev) => prev.map((s) => (s.id === id ? { ...s, is_pinned: !s.is_pinned } : s)));
  }, []);

  const switchSession = useCallback((id: string) => {
    setActiveSessionId(id);
  }, []);

  const handleProcessExit = useCallback(
    (sessionId: string) => {
      // Guard: if session no longer exists in tracking, it was already closed
      if (
        !sessionsRef.current.some((s) => s.id === sessionId) &&
        secondarySessionIdRef.current !== sessionId
      ) {
        debugLog('[Terminal] pty_exit for already-closed session:', sessionId);
        return;
      }

      // Case a: The exited session is the secondary split pane
      if (secondarySessionIdRef.current === sessionId) {
        closeSecondaryPane();
        notificationBus.notify('info', 'Terminal', 'Terminal process exited in split pane — pane closed.');
        return;
      }

      // Case b: There's more than one session → close normally
      if (sessionsRef.current.length > 1) {
        closeSession(sessionId);
        notificationBus.notify('info', 'Terminal', 'Terminal process exited — session closed.');
        return;
      }

      // Case c: Last/only session → restart with fresh shell.
      // Don't call cleanup() directly here — the React key change
      // (setSessions + setActiveSessionId below) triggers effect teardown
      // which handles WebSocket disconnection. Calling cleanup() mid-event-handler
      // risks disconnecting the service whose event callback is still on the stack.
      paneHandles.current.delete(sessionId);
      sessionShellsRef.current.delete(sessionId);
      sessionReattachIdsRef.current.delete(sessionId);

      // Create a brand new session
      paneIdCounter.current += 1;
      sessionCounterRef.current += 1;
      const newId = `pane-${paneIdCounter.current}`;
      const newSession: TerminalSession = {
        id: newId,
        name: `Session ${sessionCounterRef.current}`,
        is_pinned: false,
      };

      // Track the shell for the new session (use the currently selected shell)
      sessionShellsRef.current.set(newId, selectedShell ?? null);

      // Replace sessions array with just the new session
      setSessions([newSession]);
      setActiveSessionId(newId);

      // Clear any split state
      clearSplitState();

      notificationBus.notify('info', 'Terminal', 'Terminal process exited — restarted with fresh shell.');
    },
    [closeSecondaryPane, closeSession, clearSplitState, selectedShell],
  );

  // ── Listen for attach-session events from BackgroundTasks ─────────────────────
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ sessionId: string; name?: string }>).detail;
      if (!detail?.sessionId) return;
      const sessionId = detail.sessionId;
      const sessionName = detail.name || `Agent: ${sessionId.substring(0, 20)}`;

      // Check if session already exists in tabs
      if (sessionsRef.current.some((s) => s.id === sessionId)) {
        setActiveSessionId(sessionId);
        return;
      }

      // Add as new session tab
      const newSession: TerminalSession = {
        id: sessionId,
        name: sessionName,
        is_pinned: false,
      };
      sessionReattachIdsRef.current.set(sessionId, sessionId);
      setSessions((prev) => [...prev, newSession]);
      setActiveSessionId(sessionId);
    };
    window.addEventListener('sprout:terminal-attach-session', handler as EventListener);
    return () => window.removeEventListener('sprout:terminal-attach-session', handler as EventListener);
  }, []);

  // ── Split management ──────────────────────────────────────────────────────

  const toggleSplit = useCallback(
    (direction: 'horizontal' | 'vertical') => {
      const currentDir = splitDirectionRef.current;

      if (currentDir === direction) {
        // Unsplit: clear secondary session reference
        clearSplitState();
      } else {
        // Entering or switching split mode — create a secondary session if needed
        if (!secondarySessionIdRef.current) {
          paneIdCounter.current += 1;
          const newId = `pane-${paneIdCounter.current}`;
          sessionCounterRef.current += 1;
          const newSession: TerminalSession = {
            id: newId,
            name: `Session ${sessionCounterRef.current}`,
            is_pinned: false,
          };
          // Track the shell for the new split pane session
          sessionShellsRef.current.set(newId, selectedShell ?? null);
          setSessions((s) => [...s, newSession]);
          secondarySessionIdRef.current = newId;
          setSecondarySessionId(newId);
        }
        setSplitSizes([50, 50]);
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
      }
    },
    [clearSplitState, selectedShell],
  );

  // Listen for external terminal action events (from command palette / hotkeys)
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<{ action: string }>).detail;
      if (!detail?.action) return;
      if (detail.action === 'split_horizontal') {
        toggleSplit('horizontal');
      } else if (detail.action === 'split_vertical') {
        toggleSplit('vertical');
      } else if (detail.action === 'clear') {
        // Clear the active terminal pane
        clearActivePane();
      } else if (detail.action === 'kill') {
        // Kill the active terminal session (close it)
        const activeId = activeSessionIdRef.current;
        if (activeId) {
          closeSession(activeId);
        }
      }
    };
    window.addEventListener('sprout:terminal-action', handler as EventListener);
    return () => window.removeEventListener('sprout:terminal-action', handler as EventListener);
  }, [toggleSplit, clearActivePane, closeSession]);

  // ── Split divider drag ────────────────────────────────────────────────────

  const handleSplitDividerDragStart = useCallback(
    (e: ReactMouseEvent) => {
      e.preventDefault();
      isDraggingSplit.current = true;
      setIsResizingVertical(true); // disable transitions
      splitDragStartPos.current = splitDirection === 'vertical' ? e.clientX : e.clientY;
      splitDragStartSizes.current = [...splitSizes];

      // Measure the panes container for accurate percentage calculation
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
        // Convert pixel delta to relative change
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

  // Compute inline style for a given pane index (0 = primary, 1 = secondary)
  const splitStyleForPane = useCallback(
    (paneIndex: number): CSSProperties => {
      if (splitDirection === 'none') return {};
      const property = splitDirection === 'vertical' ? 'width' : 'height';
      const value = paneIndex === 0 ? `${splitSizes[0]}%` : `${splitSizes[1]}%`;
      return { [property]: value, minWidth: 0, minHeight: 0 };
    },
    [splitDirection, splitSizes],
  );

  // ── Vertical resize (terminal height) ──────────────────────────────────────

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
        // Persist the final height
        setTerminalHeight((prev) => {
          try {
            localStorage.setItem(TERMINAL_HEIGHT_STORAGE_KEY, String(Math.round(prev)));
          } catch (err) {
            debugLog('[Terminal] failed to persist terminal height:', err);
            // Storage write failed (quota, security policy, etc.) — non-critical
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

  // Clean up body styles and flags if component unmounts mid-drag
  useEffect(() => {
    return () => {
      if (isDraggingSplit.current || isDraggingVertical.current) {
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, []);

  // Clean up all WebSocket connections when the Terminal component unmounts
  useEffect(() => {
    return () => {
      // Clean up all pane handles and their WebSocket connections
      paneHandles.current.forEach((handle) => {
        handle?.cleanup?.();
      });
      // Ensure all refs are cleared
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
      style={isExpanded ? { height: `${terminalHeight}px` } : undefined}
    >
      {isExpanded && (
        <div
          className="terminal-resize-handle"
          onMouseDown={handleVerticalResizeStart}
          title="Drag to resize terminal"
        />
      )}

      {/* ── Header ── */}
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
            <button className="terminal-btn clear-btn" onClick={clearActivePane} title="Clear terminal">
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
              className="terminal-btn toggle-btn"
              onClick={toggleExpanded}
              title={isExpanded ? 'Collapse terminal' : 'Expand terminal'}
            >
              {isExpanded ? '▼' : '▲'}
            </button>
          </div>
        </div>
        {isExpanded && sessions.length > 0 && (
          <div
            className="terminal-tab-bar-row"
            style={{ display: 'flex', alignItems: 'stretch', position: 'relative' }}
          >
            <div style={{ flex: 1, minWidth: 0 }}>
              <TerminalTabBar
                sessions={sessions}
                activeSessionId={activeSessionId}
                onSwitch={switchSession}
                onClose={closeSession}
                onRename={renameSession}
                onTogglePin={togglePinSession}
                attachableSessions={attachableSessions}
                onAttachSession={handleAttachAgentSession}
              />
            </div>
            <div className="shell-picker-dropdown" ref={shellPickerRef}>
              <button
                className="terminal-tab-new shell-picker-btn"
                onClick={() => {
                  if (availableShells.length <= 1) {
                    addSession(selectedShell);
                  } else {
                    setShowShellMenu((prev) => !prev);
                  }
                }}
                title="New terminal session"
                type="button"
                aria-label="New terminal session"
                aria-haspopup={availableShells.length > 1}
                aria-expanded={showShellMenu}
              >
                <Plus size={14} />
                {shellsLoaded && selectedShell && (
                  <span style={{ fontSize: 10, marginLeft: 3, opacity: 0.7 }}>{selectedShell}</span>
                )}
              </button>
              {showShellMenu && shellsLoaded && availableShells.length > 1 && (
                <div className="shell-picker-menu" role="menu">
                  <div className="shell-picker-header">New Terminal</div>
                  {availableShells.map((shell) => (
                    <button
                      key={shell.name}
                      className="shell-picker-item"
                      onClick={() => handleShellPickerSelect(shell.name)}
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
        )}
      </div>

      {/* ── Background Tasks ── */}
      {isExpanded && <BackgroundTasks />}

      {/* ── Body ── */}
      <div className="terminal-body">
        <div className={`terminal-panes-container ${isSplitActive ? `terminal-split-${splitDirection}` : ''}`}>
          {/* Primary pane */}
          {activeSessionId && (
            <div className="terminal-pane-wrapper" style={splitStyleForPane(0)}>
              <TerminalPane
                key={activeSessionId}
                ref={(handle) => {
                  // Capture the session ID from the key to avoid stale ref issues
                  const id = activeSessionId;
                  if (handle) {
                    paneHandles.current.set(id, handle);
                  } else {
                    paneHandles.current.delete(id);
                  }
                }}
                isActive={hasActivated || isExpanded}
                isConnected={isConnected}
                showCloseButton={false}
                preferredShell={sessionShellsRef.current.get(activeSessionId) ?? null}
                reattachSessionId={sessionReattachIdsRef.current.get(activeSessionId) ?? null}
                fontSize={fontSize}
                onProcessExit={() => handleProcessExit(activeSessionId)}
              />
            </div>
          )}

          {/* Split divider */}
          {isSplitActive && (
            <div
              className={`terminal-split-divider terminal-split-divider-${splitDirection}`}
              onMouseDown={handleSplitDividerDragStart}
            />
          )}

          {/* Secondary pane */}
          {isSplitActive && secondarySessionId && (
            <div className="terminal-pane-wrapper" style={splitStyleForPane(1)}>
              <TerminalPane
                key={secondarySessionId}
                ref={(handle) => {
                  const id = secondarySessionId;
                  if (handle) {
                    paneHandles.current.set(id, handle);
                  } else {
                    paneHandles.current.delete(id);
                  }
                }}
                isActive={hasActivated || isExpanded}
                isConnected={isConnected}
                showCloseButton={true}
                onClose={closeSecondaryPane}
                preferredShell={sessionShellsRef.current.get(secondarySessionId) ?? null}
                reattachSessionId={sessionReattachIdsRef.current.get(secondarySessionId) ?? null}
                fontSize={fontSize}
                onProcessExit={() => handleProcessExit(secondarySessionId!)}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default Terminal;
