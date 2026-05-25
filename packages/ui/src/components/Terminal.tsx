/**
 * @deprecated This component is superseded by the per-pane model used in
 * `webui/src/components/Terminal.tsx`, which orchestrates `TerminalPane` and
 * `TerminalTabBar` directly. The webui app no longer imports this wrapper.
 * Retain only if a standalone, framework-agnostic terminal widget is needed
 * outside the main webui. If no such consumer emerges, this file (and its
 * story/test) can be removed in a future cleanup pass.
 *
 * See: SP-011-P2.3-cleanupPackagesUI
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import type { CSSProperties, MouseEvent as ReactMouseEvent } from 'react';
import React from 'react';
import { Columns2, Rows2, ZoomIn, ZoomOut, Type } from 'lucide-react';
import './Terminal.css';
import TerminalPane from './TerminalPane';
import TerminalTabBar, { type TerminalSession } from './TerminalTabBar';
import { FONT_SIZE_DEFAULT } from './terminalConstants';
import type { ShellInfo } from '../types/terminal';

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

/** Data structure for each pane in the terminal */
interface TerminalPaneData {
  id: string;
  sessions: TerminalSession[];
  activeSessionId: string;
}

export interface TerminalProps {
  /** Whether the backend is connected */
  isConnected?: boolean;
  /** Whether the terminal is expanded */
  isExpanded?: boolean;
  /** Callback when expand state changes */
  onToggleExpand?: (expanded: boolean) => void;
  /** Callback to fetch available shells */
  onFetchShells?: () => Promise<ShellInfo[]>;
  /** Callback for notifications */
  onNotify?: (type: 'info' | 'success' | 'warning' | 'error', title: string, message: string) => void;
  /** Factory to create terminal connections */
  createConnection?: import('./TerminalPane').CreateTerminalConnection;
  /** Theme pack for terminal colors */
  themePack?: import('./TerminalPane').TerminalThemePack;
  /** Optional WASM shell factory */
  createWasmShell?: () => Promise<{
    write: (data: string) => void;
    onData: (callback: (data: string) => void) => void;
    close: () => void;
  } | null>;
}

function Terminal({
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand,
  onFetchShells,
  onNotify,
  createConnection,
  themePack,
  createWasmShell,
}: TerminalProps): JSX.Element {
  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [hasActivated, setHasActivated] = useState(externalIsExpanded);
  const [terminalHeight, setTerminalHeight] = useState<number>(() => {
    if (typeof window === 'undefined') return TERMINAL_HEIGHT_DEFAULT;
    try {
      const stored = localStorage.getItem(TERMINAL_HEIGHT_STORAGE_KEY);
      return stored ? clampTerminalHeight(Number(stored)) : TERMINAL_HEIGHT_DEFAULT;
    } catch {
      return TERMINAL_HEIGHT_DEFAULT;
    }
  });
  const [fontSize, setFontSize] = useState<number>(() => {
    if (typeof window === 'undefined') return FONT_SIZE_DEFAULT;
    try {
      const stored = localStorage.getItem(FONT_SIZE_STORAGE_KEY);
      return stored ? clampFontSize(Number(stored)) : FONT_SIZE_DEFAULT;
    } catch {
      return FONT_SIZE_DEFAULT;
    }
  });

  // ── Split state ─────────────────────────────────────────────────────
  const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
  const splitDirectionRef = useRef<SplitDirection>('none');
  const [splitSizes, setSplitSizes] = useState<[number, number]>([50, 50]);

  // ── Pane-based session state ───────────────────────────────────────
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

  const isDraggingSplit = useRef(false);
  const splitDragStartPos = useRef(0);
  const splitDragStartSizes = useRef<[number, number]>([50, 50]);
  const isDraggingVertical = useRef(false);

  // Keep panesRef in sync
  useEffect(() => {
    panesRef.current = panes;
  }, [panes]);

  // ── Expand/collapse ──────────────────────────────────────────────────
  const toggleExpand = useCallback(() => {
    const next = !isExpanded;
    setIsExpanded(next);
    if (next) setHasActivated(true);
    onToggleExpand?.(next);
  }, [isExpanded, onToggleExpand]);

  // Persist height
  useEffect(() => {
    try {
      localStorage.setItem(TERMINAL_HEIGHT_STORAGE_KEY, String(terminalHeight));
    } catch { /* ignore */ }
  }, [terminalHeight]);

  // Persist font size
  useEffect(() => {
    try {
      localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(fontSize));
    } catch { /* ignore */ }
  }, [fontSize]);

  // ── Resize dragging ──────────────────────────────────────────────────
  const handleResizeStart = useCallback((e: ReactMouseEvent) => {
    e.preventDefault();
    isDraggingVertical.current = true;
    const startY = e.clientY;
    const startHeight = terminalHeight;

    const onMove = (ev: globalThis.MouseEvent) => {
      const delta = startY - ev.clientY;
      setTerminalHeight(clampTerminalHeight(startHeight + delta));
    };
    const onUp = () => {
      isDraggingVertical.current = false;
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };

    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'row-resize';
  }, [terminalHeight]);

  // ── Split divider dragging ───────────────────────────────────────────
  const handleSplitDividerDragStart = useCallback(
    (e: ReactMouseEvent) => {
      e.preventDefault();
      isDraggingSplit.current = true;
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

  // ── Helper: Update a pane immutably ───────────────────────────────────
  const updatePane = useCallback((paneId: string, updater: (pane: TerminalPaneData) => TerminalPaneData) => {
    setPanes((prev) => prev.map((p) => (p.id === paneId ? updater(p) : p)));
  }, []);

  // ── Session management scoped to panes ───────────────────────────────
  const getFocusedPane = useCallback((): TerminalPaneData | null => {
    return panesRef.current.find((p) => p.id === focusedPaneId) ?? null;
  }, [focusedPaneId]);

  const addSessionToPane = useCallback(
    (paneId: string) => {
      const sessionNum = sessionCounterRef.current++;
      const sessionId = `session-${sessionNum}`;
      const newSession: TerminalSession = {
        id: sessionId,
        name: `Session ${sessionNum}`,
        is_pinned: false,
      };
      updatePane(paneId, (pane) => ({
        ...pane,
        sessions: [...pane.sessions, newSession],
        activeSessionId: sessionId,
      }));
    },
    [updatePane],
  );

  const closeSessionInPane = useCallback(
    (paneId: string, sessionId: string) => {
      const pane = panesRef.current.find((p) => p.id === paneId);
      if (!pane) return;
      if (pane.sessions.length <= 1 && panesRef.current.length <= 1) return;

      const remaining = pane.sessions.filter((s) => s.id !== sessionId);

      // Auto-close secondary pane if it has no sessions left
      if (panesRef.current.length > 1 && remaining.length === 0) {
        setPanes((prev) => prev.filter((p) => p.id !== paneId));
        splitDirectionRef.current = 'none';
        setSplitDirection('none');
        return;
      }

      const newActive = pane.activeSessionId === sessionId ? remaining[0].id : pane.activeSessionId;

      updatePane(paneId, (p) => ({
        ...p,
        sessions: remaining,
        activeSessionId: newActive,
      }));
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
    },
    [updatePane],
  );

  // ── Split management ─────────────────────────────────────────────────
  const toggleSplit = useCallback(
    (direction: SplitDirection) => {
      const currentDir = splitDirectionRef.current;

      if (currentDir === direction) {
        // Same direction → toggle off (unsplit)
        if (panesRef.current.length > 1) {
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
        setPanes((prev) => [...prev, newPane]);
        setFocusedPaneId(paneId);
        setSplitSizes([50, 50]);
        splitDirectionRef.current = direction;
        setSplitDirection(direction);
      }
    },
    [],
  );

  // ── Font size controls ───────────────────────────────────────────────
  const zoomIn = useCallback(() => setFontSize((prev) => clampFontSize(prev + 1)), []);
  const zoomOut = useCallback(() => setFontSize((prev) => clampFontSize(prev - 1)), []);
  const resetFontSize = useCallback(() => setFontSize(FONT_SIZE_DEFAULT), []);

  // ── Cleanup effect for drag listeners ────────────────────────────────
  useEffect(() => {
    return () => {
      if (isDraggingSplit.current || isDraggingVertical.current) {
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, []);

  // ── Collapsed bar ────────────────────────────────────────────────────
  if (!isExpanded || !hasActivated) {
    return (
      <div className="terminal-collapsed-bar" onClick={toggleExpand}>
        <span className="terminal-collapsed-label">Terminal</span>
        <span className="terminal-collapsed-hint">Click to expand</span>
      </div>
    );
  }

  const collapsedHeight = 42;
  const isSplitActive = splitDirection !== 'none';

  return (
    <div className="terminal-container" style={{ height: terminalHeight }}>
      {/* Resize handle */}
      <div
        className="terminal-resize-handle"
        onMouseDown={handleResizeStart}
      />

      {/* Header bar */}
      <div className="terminal-header">
        <div className="terminal-header-right">
          <button className="terminal-header-btn" onClick={zoomOut} title="Zoom out">
            <ZoomOut size={14} />
          </button>
          <button className="terminal-header-btn" onClick={zoomIn} title="Zoom in">
            <ZoomIn size={14} />
          </button>
          <button className="terminal-header-btn" onClick={resetFontSize} title="Reset font size">
            <Type size={14} />
          </button>
          <button
            className={`terminal-header-btn ${splitDirection === 'horizontal' ? 'active' : ''}`}
            onClick={() => toggleSplit('horizontal')}
            title="Split horizontal"
          >
            <Rows2 size={14} />
          </button>
          <button
            className={`terminal-header-btn ${splitDirection === 'vertical' ? 'active' : ''}`}
            onClick={() => toggleSplit('vertical')}
            title="Split vertical"
          >
            <Columns2 size={14} />
          </button>
          <button className="terminal-header-btn" onClick={toggleExpand} title="Collapse terminal">
            ▼
          </button>
        </div>
      </div>

      {/* Terminal panes */}
      <div
        className={`terminal-panes-container ${isSplitActive ? `terminal-split-${splitDirection}` : ''}`}
        style={{ height: terminalHeight - collapsedHeight }}
      >
        {panes.map((pane, index) => (
          <React.Fragment key={pane.id}>
            <div
              className="terminal-pane-wrapper"
              style={splitStyleForPane(index)}
              onMouseDown={() => setFocusedPaneId(pane.id)}
            >
              <div className="terminal-pane-tab-bar">
                <TerminalTabBar
                  sessions={pane.sessions}
                  activeSessionId={pane.activeSessionId}
                  onSwitch={(id) => switchSessionInPane(pane.id, id)}
                  onClose={(id) => closeSessionInPane(pane.id, id)}
                  onRename={(id, name) => renameSessionInPane(pane.id, id, name)}
                  onTogglePin={(id) => togglePinInPane(pane.id, id)}
                  onCreate={() => addSessionToPane(pane.id)}
                />
              </div>
              <TerminalPane
                key={pane.activeSessionId}
                isActive={true}
                sessionId={pane.activeSessionId}
                fontSize={fontSize}
                themePack={themePack}
                createConnection={createConnection}
                createWasmShell={createWasmShell}
              />
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
  );
}

export default Terminal;
