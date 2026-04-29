import { useState, useEffect, useRef, useCallback } from 'react';
import type { CSSProperties, MouseEvent as ReactMouseEvent } from 'react';
import { Trash2, Columns2, Rows2, Plus, Check, ZoomIn, ZoomOut, Type } from 'lucide-react';
import './Terminal.css';
import TerminalPane, { type TerminalPaneHandle } from './TerminalPane';
import TerminalTabBar, { type TerminalSession } from './TerminalTabBar';
import { FONT_SIZE_DEFAULT } from './terminalConstants';

type SplitDirection = 'none' | 'horizontal' | 'vertical';

const TERMINAL_HEIGHT_MIN = 120;
const TERMINAL_HEIGHT_DEFAULT = 400;
const TERMINAL_HEIGHT_MAX_FACTOR = 100;
const TERMINAL_HEIGHT_STORAGE_KEY = 'sprout-terminal-height';

const FONT_SIZE_MIN = 8;
const FONT_SIZE_MAX = 32;
const FONT_SIZE_STORAGE_KEY = 'sprout-terminal-font-size';

export interface ShellInfo {
  name: string;
  path: string;
  is_default?: boolean;
}

const clampTerminalHeight = (value: number): number => {
  if (!Number.isFinite(value)) return TERMINAL_HEIGHT_DEFAULT;
  return Math.max(TERMINAL_HEIGHT_MIN, Math.min(window.innerHeight - TERMINAL_HEIGHT_MAX_FACTOR, value));
};

const clampFontSize = (value: number): number => {
  if (!Number.isFinite(value)) return FONT_SIZE_DEFAULT;
  return Math.max(FONT_SIZE_MIN, Math.min(FONT_SIZE_MAX, value));
};

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
  createWasmShell?: TerminalPaneProps['createWasmShell'];
}

type TerminalPaneProps = React.ComponentProps<typeof TerminalPane>;

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

  // ── Sessions ────────────────────────────────────────────────────────
  const [sessions, setSessions] = useState<TerminalSession[]>([
    { id: 'default', name: 'Terminal 1', is_pinned: false },
  ]);
  const [activeSessionId, setActiveSessionId] = useState('default');
  const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
  const [secondarySessionId, setSecondarySessionId] = useState<string | null>(null);

  const isResizing = useRef(false);

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
    isResizing.current = true;
    const startY = e.clientY;
    const startHeight = terminalHeight;

    const onMove = (ev: globalThis.MouseEvent) => {
      if (!isResizing.current) return;
      const delta = startY - ev.clientY;
      setTerminalHeight(clampTerminalHeight(startHeight + delta));
    };
    const onUp = () => {
      isResizing.current = false;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };

    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, [terminalHeight]);

  // ── Session management ───────────────────────────────────────────────
  const addSession = useCallback(() => {
    const id = `session-${Date.now()}`;
    const name = `Terminal ${sessions.length + 1}`;
    setSessions((prev) => [...prev, { id, name, is_pinned: false }]);
    setActiveSessionId(id);
  }, [sessions.length]);

  const removeSession = useCallback((id: string) => {
    setSessions((prev) => {
      const next = prev.filter((s) => s.id !== id);
      if (next.length === 0) {
        const newId = `session-${Date.now()}`;
        next.push({ id: newId, name: 'Terminal 1', is_pinned: false });
        setActiveSessionId(newId);
      } else if (activeSessionId === id) {
        setActiveSessionId(next[0].id);
      }
      return next;
    });
    if (secondarySessionId === id) setSecondarySessionId(null);
  }, [activeSessionId, secondarySessionId]);

  // ── Split management ─────────────────────────────────────────────────
  const toggleSplit = useCallback((direction: SplitDirection) => {
    setSplitDirection((prev) => prev === direction ? 'none' : direction);
    if (splitDirection === 'none' && !secondarySessionId) {
      const id = `split-${Date.now()}`;
      setSessions((prev) => [...prev, { id, name: `Terminal ${prev.length + 1}`, is_pinned: false }]);
      setSecondarySessionId(id);
    }
  }, [splitDirection, secondarySessionId]);

  // ── Font size controls ───────────────────────────────────────────────
  const zoomIn = useCallback(() => setFontSize((prev) => clampFontSize(prev + 1)), []);
  const zoomOut = useCallback(() => setFontSize((prev) => clampFontSize(prev - 1)), []);
  const resetFontSize = useCallback(() => setFontSize(FONT_SIZE_DEFAULT), []);

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

  return (
    <div className="terminal-container" style={{ height: terminalHeight }}>
      {/* Resize handle */}
      <div
        className="terminal-resize-handle"
        onMouseDown={handleResizeStart}
      />

      {/* Header bar */}
      <div className="terminal-header">
        <div className="terminal-header-left">
          <TerminalTabBar
            sessions={sessions}
            activeSessionId={activeSessionId}
            onSwitch={setActiveSessionId}
            onClose={removeSession}
            onCreate={addSession}
            onRename={(id, name) => {
              setSessions((prev) => prev.map((s) => s.id === id ? { ...s, name } : s));
            }}
            onTogglePin={(id) => {
              setSessions((prev) => prev.map((s) => s.id === id ? { ...s, is_pinned: !s.is_pinned } : s));
            }}
          />
        </div>
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
            <Trash2 size={14} />
          </button>
        </div>
      </div>

      {/* Terminal panes */}
      <div
        className={`terminal-panes ${splitDirection !== 'none' ? `split-${splitDirection}` : ''}`}
        style={{ height: terminalHeight - collapsedHeight }}
      >
        <TerminalPane
          
          isActive={true}
          sessionId={activeSessionId}
          fontSize={fontSize}
          themePack={themePack}
          createConnection={createConnection}
          createWasmShell={createWasmShell}
        />
        {splitDirection !== 'none' && secondarySessionId && (
          <TerminalPane
            
            isActive={true}
            sessionId={secondarySessionId}
            fontSize={fontSize}
            isSplit={true}
            themePack={themePack}
            createConnection={createConnection}
            createWasmShell={createWasmShell}
          />
        )}
      </div>
    </div>
  );
}

export default Terminal;
