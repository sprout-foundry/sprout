import React, { useState, useEffect, useRef, useCallback } from 'react';
import { Trash2, SplitSquareHorizontal } from 'lucide-react';
import './Terminal.css';
import TerminalPane, { TerminalPaneHandle } from './TerminalPane';

interface TerminalProps {
  onCommand?: (command: string) => void;
  onOutput?: (output: string) => void;
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
}

let paneCounter = 0;
const newPaneId = () => `pane-${++paneCounter}`;

const Terminal: React.FC<TerminalProps> = ({
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand,
}) => {
  const getCollapsedHeight = useCallback(() => {
    if (typeof window === 'undefined') {
      return 42;
    }
    return window.innerWidth <= 768 ? 34 : 42;
  }, []);
  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [hasActivated, setHasActivated] = useState(externalIsExpanded);
  const [terminalHeight, setTerminalHeight] = useState(400);
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  const [collapsedHeight, setCollapsedHeight] = useState(getCollapsedHeight);

  // Split pane state
  const [panes, setPanes] = useState<{ id: string }[]>(() => [{ id: newPaneId() }]);
  const [splitPosition, setSplitPosition] = useState(50); // left pane width %
  const [isResizingSplit, setIsResizingSplit] = useState(false);

  const hasMountedRef = useRef(false);
  const isDraggingVertical = useRef(false);
  const isDraggingSplit = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);
  const dragStartX = useRef(0);
  const dragStartSplit = useRef(50);
  const splitContainerRef = useRef<HTMLDivElement>(null);

  // Keyed refs to each pane's imperative handle
  const paneHandles = useRef<Map<string, TerminalPaneHandle | null>>(new Map());

  const isSplit = panes.length > 1;

  useEffect(() => {
    setIsExpanded(externalIsExpanded);
    if (externalIsExpanded) {
      setHasActivated(true);
    }
  }, [externalIsExpanded]);

  useEffect(() => {
    const reservedHeight = isExpanded ? terminalHeight : collapsedHeight;
    document.documentElement.style.setProperty('--ledit-terminal-reserved-height', `${reservedHeight}px`);
    return () => {
      document.documentElement.style.setProperty('--ledit-terminal-reserved-height', `${collapsedHeight}px`);
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

  const toggleExpanded = useCallback(() => {
    setIsExpanded(prev => {
      const next = !prev;
      if (next) {
        setHasActivated(true);
      }
      onToggleExpand?.(next);
      return next;
    });
  }, [onToggleExpand]);

  const clearAllPanes = useCallback(() => {
    paneHandles.current.forEach(handle => handle?.clear());
  }, []);

  const addSplitPane = useCallback(() => {
    setPanes(prev => (prev.length >= 2 ? prev : [...prev, { id: newPaneId() }]));
  }, []);

  const removePane = useCallback((id: string) => {
    setPanes(prev => {
      if (prev.length <= 1) return prev;
      paneHandles.current.delete(id);
      return prev.filter(p => p.id !== id);
    });
    setSplitPosition(50);
  }, []);

  // ── Vertical resize (terminal height) ──────────────────────────────────────

  const handleVerticalResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      isDraggingVertical.current = true;
      setIsResizingVertical(true);
      dragStartY.current = e.clientY;
      dragStartHeight.current = terminalHeight;

      const onMove = (ev: MouseEvent) => {
        if (!isDraggingVertical.current) return;
        const delta = dragStartY.current - ev.clientY;
        const next = Math.max(120, Math.min(window.innerHeight - 100, dragStartHeight.current + delta));
        setTerminalHeight(next);
      };

      const onUp = () => {
        isDraggingVertical.current = false;
        setIsResizingVertical(false);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
    },
    [terminalHeight]
  );

  // ── Horizontal split resize ─────────────────────────────────────────────────

  const handleSplitResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      isDraggingSplit.current = true;
      setIsResizingSplit(true);
      dragStartX.current = e.clientX;
      dragStartSplit.current = splitPosition;
      const containerWidth = splitContainerRef.current?.offsetWidth ?? window.innerWidth;

      const onMove = (ev: MouseEvent) => {
        if (!isDraggingSplit.current) return;
        const delta = ev.clientX - dragStartX.current;
        const deltaPercent = (delta / containerWidth) * 100;
        const next = Math.max(20, Math.min(80, dragStartSplit.current + deltaPercent));
        setSplitPosition(next);
      };

      const onUp = () => {
        isDraggingSplit.current = false;
        setIsResizingSplit(false);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      };

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'col-resize';
    },
    [splitPosition]
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

  return (
    <div
      className={[
        'terminal-container',
        isExpanded ? 'expanded' : 'collapsed',
        hasMountedRef.current ? 'initial-mount' : '',
        isResizingVertical || isResizingSplit ? 'resizing' : '',
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
      <div className="terminal-header" onClick={toggleExpanded}>
        <div className="terminal-title">
          <span className="terminal-icon">$</span>
          <span>Terminal</span>
        </div>
        <div className="terminal-controls" onClick={e => e.stopPropagation()}>
          {isExpanded && !isSplit && (
            <button
              className="terminal-btn split-btn"
              onClick={addSplitPane}
              title="Split terminal"
            >
              <SplitSquareHorizontal size={15} />
            </button>
          )}
          <button
            className="terminal-btn clear-btn"
            onClick={clearAllPanes}
            title="Clear terminal"
          >
            <Trash2 size={16} />
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

      {/* ── Body ── */}
      <div className="terminal-body">
        <div
          className={`terminal-panes-container${isSplit ? ' split' : ''}`}
          ref={splitContainerRef}
        >
          {panes.map((pane, index) => (
            <React.Fragment key={pane.id}>
              <div
                className="terminal-pane-wrapper"
                style={
                  isSplit
                    ? { width: index === 0 ? `${splitPosition}%` : `${100 - splitPosition}%` }
                    : undefined
                }
              >
                <TerminalPane
                  ref={handle => {
                    if (handle) {
                      paneHandles.current.set(pane.id, handle);
                    } else {
                      paneHandles.current.delete(pane.id);
                    }
                  }}
                  isActive={hasActivated || isExpanded}
                  isConnected={isConnected}
                  showCloseButton={isSplit}
                  onClose={() => removePane(pane.id)}
                />
              </div>

              {/* Draggable divider between panes */}
              {isSplit && index === 0 && (
                <div
                  className="terminal-split-divider"
                  onMouseDown={handleSplitResizeStart}
                  title="Drag to resize panes"
                />
              )}
            </React.Fragment>
          ))}
        </div>
      </div>
    </div>
  );
};

export default Terminal;
