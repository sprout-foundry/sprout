import React, { useState, useEffect, useRef, useCallback } from 'react';
import { Trash2, TriangleAlert } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import './Terminal.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { useTheme } from '../contexts/ThemeContext';

interface TerminalProps {
  onCommand?: (command: string) => void;
  onOutput?: (output: string) => void;
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
}

const Terminal: React.FC<TerminalProps> = ({
  onCommand,
  onOutput,
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand
}) => {
  const { themePack } = useTheme();
  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [terminalConnected, setTerminalConnected] = useState(false);
  const [terminalHeight, setTerminalHeight] = useState(400);
  const [isResizing, setIsResizing] = useState(false);

  const isDragging = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);
  const hasMountedRef = useRef(false);

  const terminalWrapperRef = useRef<HTMLDivElement>(null);
  const xtermContainerRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);

  const terminalWS = useRef<TerminalWebSocketService | null>(null);
  const terminalEventHandlerRef = useRef<((event: any) => void) | null>(null);
  const resizeTimerRef = useRef<number | null>(null);

  const getTerminalThemeFromCssVars = useCallback(() => {
    const css = getComputedStyle(document.documentElement);
    const v = (name: string, fallback: string) => (css.getPropertyValue(name) || fallback).trim();
    return {
      background: 'transparent',
      foreground: v('--text-primary', '#e2e8f0'),
      cursor: v('--accent-primary', '#22d3ee'),
      cursorAccent: v('--bg-primary', '#0f172a'),
      selectionBackground: v('--accent-primary-alpha', 'rgba(34, 211, 238, 0.2)'),
      black: '#000000',
      red: v('--accent-error', '#ef4444'),
      green: v('--accent-success', '#4ade80'),
      yellow: v('--accent-warning', '#facc15'),
      blue: v('--accent-primary', '#60a5fa'),
      magenta: '#d946ef',
      cyan: v('--accent-cyan', '#22d3ee'),
      white: v('--text-primary', '#e5e7eb'),
      brightBlack: v('--text-muted', '#64748b'),
      brightRed: v('--accent-error', '#f87171'),
      brightGreen: v('--accent-success', '#86efac'),
      brightYellow: v('--accent-warning', '#fde047'),
      brightBlue: v('--accent-primary', '#93c5fd'),
      brightMagenta: '#f0abfc',
      brightCyan: v('--accent-cyan', '#67e8f9'),
      brightWhite: '#ffffff',
    };
  }, []);

  const sendTerminalResize = useCallback(() => {
    if (!isExpanded || !terminalConnected || !terminalWS.current || !xtermRef.current || !fitAddonRef.current) {
      return;
    }

    fitAddonRef.current.fit();
    const cols = xtermRef.current.cols;
    const rows = xtermRef.current.rows;
    terminalWS.current.sendResize(cols, rows);
  }, [isExpanded, terminalConnected]);

  useEffect(() => {
    setIsExpanded(externalIsExpanded);
  }, [externalIsExpanded]);

  useEffect(() => {
    if (!isExpanded || !xtermContainerRef.current || xtermRef.current) {
      return;
    }

    const term = new XTerm({
      convertEol: false,
      cursorBlink: true,
      allowProposedApi: false,
      fontFamily: 'var(--font-mono)',
      fontSize: 13,
      lineHeight: 1.45,
      scrollback: 5000,
      theme: getTerminalThemeFromCssVars(),
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(xtermContainerRef.current);

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    term.onData((data) => {
      if (terminalWS.current) {
        terminalWS.current.sendRawInput(data);
      }
      if (onCommand) {
        onCommand(data);
      }
    });

    requestAnimationFrame(() => {
      fitAddon.fit();
      term.focus();
    });

    return () => {
      try {
        term.dispose();
      } catch {
        // ignore
      }
      xtermRef.current = null;
      fitAddonRef.current = null;
    };
  }, [isExpanded, onCommand, getTerminalThemeFromCssVars]);

  useEffect(() => {
    if (!xtermRef.current) return;
    xtermRef.current.options.theme = getTerminalThemeFromCssVars();
    requestAnimationFrame(() => {
      fitAddonRef.current?.fit();
    });
  }, [themePack.id, getTerminalThemeFromCssVars]);

  useEffect(() => {
    const terminalService = TerminalWebSocketService.getInstance();

    if (isExpanded && isConnected) {
      if (terminalService.isReady()) {
        setTerminalConnected(true);
      }

      const eventHandler = (event: any) => {
        if (event.type === 'connection_status') {
          if (!event.data.connected) {
            setTerminalConnected(false);
            xtermRef.current?.writeln('\r\nTerminal disconnected');
          }
        } else if (event.type === 'session_ready') {
          setTerminalConnected(true);
          requestAnimationFrame(() => {
            sendTerminalResize();
            xtermRef.current?.focus();
          });
        } else if (event.type === 'output') {
          const out = event.data.output || '';
          xtermRef.current?.write(out);
          onOutput?.(out);
        } else if (event.type === 'error_output') {
          const out = event.data.output || '';
          xtermRef.current?.write(out);
          onOutput?.(out);
        } else if (event.type === 'error') {
          const msg = `\r\n${event.data.message}\r\n`;
          xtermRef.current?.write(msg);
          onOutput?.(msg);
        }
      };

      terminalEventHandlerRef.current = eventHandler;
      terminalWS.current = terminalService;
      terminalService.onEvent(eventHandler);

      if (!terminalService.isReady()) {
        terminalService.connect();
      }
    } else {
      if (terminalEventHandlerRef.current && terminalWS.current) {
        terminalWS.current.removeEvent(terminalEventHandlerRef.current);
      }
      terminalEventHandlerRef.current = null;
      terminalWS.current = null;
      setTerminalConnected(false);
    }

    return () => {
      if (terminalEventHandlerRef.current && terminalWS.current) {
        terminalWS.current.removeEvent(terminalEventHandlerRef.current);
        terminalEventHandlerRef.current = null;
      }
    };
  }, [isExpanded, isConnected, onOutput, sendTerminalResize]);

  useEffect(() => {
    if (!isExpanded || !terminalConnected) {
      return;
    }

    const scheduleResize = () => {
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
      }
      resizeTimerRef.current = window.setTimeout(() => {
        sendTerminalResize();
      }, 80);
    };

    scheduleResize();
    window.addEventListener('resize', scheduleResize);

    let observer: ResizeObserver | null = null;
    if (terminalWrapperRef.current && 'ResizeObserver' in window) {
      observer = new ResizeObserver(() => scheduleResize());
      observer.observe(terminalWrapperRef.current);
    }

    return () => {
      window.removeEventListener('resize', scheduleResize);
      if (observer) observer.disconnect();
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = null;
      }
    };
  }, [isExpanded, terminalConnected, sendTerminalResize, terminalHeight]);

  const toggleExpanded = useCallback(() => {
    setIsExpanded(prev => {
      const next = !prev;
      onToggleExpand?.(next);
      return next;
    });
  }, [onToggleExpand]);

  const clearTerminal = useCallback(() => {
    xtermRef.current?.clear();
  }, []);

  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDragging.current = true;
    setIsResizing(true);
    dragStartY.current = e.clientY;
    dragStartHeight.current = terminalHeight;

    const handleResizeMove = (moveEvent: MouseEvent) => {
      if (!isDragging.current) return;
      const delta = dragStartY.current - moveEvent.clientY;
      const newHeight = Math.max(120, Math.min(window.innerHeight - 100, dragStartHeight.current + delta));
      setTerminalHeight(newHeight);
    };

    const handleResizeEnd = () => {
      isDragging.current = false;
      setIsResizing(false);
      document.removeEventListener('mousemove', handleResizeMove);
      document.removeEventListener('mouseup', handleResizeEnd);
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };

    document.addEventListener('mousemove', handleResizeMove);
    document.addEventListener('mouseup', handleResizeEnd);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'row-resize';
  }, [terminalHeight]);

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
      className={`terminal-container ${isExpanded ? 'expanded' : 'collapsed'} ${hasMountedRef.current ? 'initial-mount' : ''} ${isResizing ? 'resizing' : ''}`}
      style={isExpanded ? { height: `${terminalHeight}px` } : undefined}
    >
      {isExpanded && (
        <div
          className="terminal-resize-handle"
          onMouseDown={handleResizeStart}
          title="Drag to resize terminal"
        />
      )}
      <div className="terminal-header">
        <div className="terminal-title">
          <span className="terminal-icon">$</span>
          <span>Terminal</span>
          {!terminalConnected && <span className="connection-status disconnected">●</span>}
          {terminalConnected && <span className="connection-status connected">●</span>}
        </div>
        <div className="terminal-controls">
          <button
            className="terminal-btn clear-btn"
            onClick={clearTerminal}
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

      {isExpanded && (
        <div className="terminal-body">
          <div
            ref={terminalWrapperRef}
            className="terminal-output xterm-host"
            onClick={() => xtermRef.current?.focus()}
          >
            <div ref={xtermContainerRef} className="terminal-xterm" />
          </div>

          {!terminalConnected && (
            <div className="terminal-status-inline">
              <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
              Backend not connected. Start with: <code>./ledit agent --web-port 54421</code>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default Terminal;
