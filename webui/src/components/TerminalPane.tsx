import { useState, useEffect, useRef, useCallback, useImperativeHandle, forwardRef } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import ContextMenu from './ContextMenu';
import { X, TriangleAlert, Copy, ClipboardPaste, Trash2, TextSelect, Link2 } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import type { WsEvent } from '../services/websocket';
import { useTheme } from '../contexts/ThemeContext';
import { debugLog } from '../utils/log';
import { copyToClipboard } from '../utils/clipboard';

// Font size constants (must match Terminal.tsx)
const FONT_SIZE_DEFAULT = 13;

export interface TerminalPaneHandle {
  clear: () => void;
  focus: () => void;
}

interface TerminalPaneProps {
  /** Whether the parent terminal is expanded (mounted). */
  isActive: boolean;
  /** Whether the app-level WebSocket connection is available. */
  isConnected?: boolean;
  /** When true, renders a close button in the pane header. */
  showCloseButton: boolean;
  /** Called when the user clicks the pane close button. */
  onClose?: () => void;
  /** Notifies the parent of connection state changes. */
  onConnectionChange?: (connected: boolean) => void;
  /** Preferred shell name (e.g. "bash", "zsh", "fish") for the initial PTY session. */
  preferredShell?: string | null;
  /** Font size in pixels (overrides default). */
  fontSize?: number;
}

interface TerminalContextMenuState {
  x: number;
  y: number;
  hasSelection: boolean;
  hasLink: boolean;
  linkUrl: string;
}

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  ({ isActive, isConnected = true, showCloseButton, onClose, onConnectionChange, preferredShell, fontSize }, ref) => {
    const { themePack } = useTheme();
    const [paneConnected, setPaneConnected] = useState(false);
    const [contextMenu, setContextMenu] = useState<TerminalContextMenuState | null>(null);

    // Stabilize callback props in refs so the WebSocket lifecycle effect doesn't
    // tear down / reconnect when a parent passes an inline callback.
    const onConnectionChangeRef = useRef(onConnectionChange);
    onConnectionChangeRef.current = onConnectionChange;

    // Stabilize preferredShell so the WebSocket lifecycle effect doesn't
    // tear down / reconnect when a parent changes the value.
    const preferredShellRef = useRef(preferredShell);
    preferredShellRef.current = preferredShell;

    const paneWrapperRef = useRef<HTMLDivElement>(null);
    const xtermContainerRef = useRef<HTMLDivElement>(null);
    const xtermRef = useRef<XTerm | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const terminalWSRef = useRef<TerminalWebSocketService | null>(null);
    const eventHandlerRef = useRef<((event: WsEvent) => void) | null>(null);
    const resizeTimerRef = useRef<number | null>(null);

    const getTerminalTheme = useCallback(() => {
      return {
        // Keep terminal palette independent from app light/dark theme
        // to preserve readability and ANSI color contrast.
        background: '#05070d',
        foreground: '#d7dee9',
        cursor: '#5ea1ff',
        cursorAccent: '#05070d',
        selectionBackground: 'rgba(94, 161, 255, 0.25)',
        black: '#111827',
        red: '#ef6b73',
        green: '#7ddf97',
        yellow: '#f4d56f',
        blue: '#5ea1ff',
        magenta: '#c792ea',
        cyan: '#4fd3d9',
        white: '#d7dee9',
        brightBlack: '#5f6b7a',
        brightRed: '#ff8a92',
        brightGreen: '#96f0ad',
        brightYellow: '#ffe08a',
        brightBlue: '#86b8ff',
        brightMagenta: '#f0abfc',
        brightCyan: '#75e7eb',
        brightWhite: '#ffffff',
      };
    }, []);

    const getTerminalFontFamily = useCallback(() => {
      const css = getComputedStyle(document.documentElement);
      const raw = (css.getPropertyValue('--font-mono') || '').trim();
      if (!raw || raw.includes('var(')) {
        return "'JetBrains Mono', 'SF Mono', 'Fira Code', 'Consolas', monospace";
      }
      return raw;
    }, []);

    const sendResize = useCallback(() => {
      if (!paneConnected || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current) return;
      fitAddonRef.current.fit();
      terminalWSRef.current.sendResize(xtermRef.current.cols, xtermRef.current.rows);
    }, [paneConnected]);

    // ── Context menu handlers ──
    const closeContextMenu = useCallback(() => {
      setContextMenu(null);
    }, []);

    const handleCopy = useCallback(() => {
      const term = xtermRef.current;
      if (term?.hasSelection()) {
        copyToClipboard(term.getSelection()).catch((err) => {
          debugLog('Clipboard access denied:', err);
        });
      }
      closeContextMenu();
    }, [closeContextMenu]);

    const handlePaste = useCallback(async () => {
      try {
        const text = await navigator.clipboard.readText();
        terminalWSRef.current?.sendRawInput(text);
      } catch (err) {
        debugLog('[TerminalPane] clipboard readText failed:', err);
        // Clipboard access denied
      }
      closeContextMenu();
    }, [closeContextMenu]);

    const handleClear = useCallback(() => {
      xtermRef.current?.clear();
      closeContextMenu();
    }, [closeContextMenu]);

    const handleSelectAll = useCallback(() => {
      xtermRef.current?.selectAll();
      closeContextMenu();
    }, [closeContextMenu]);

    const handleCopyLink = useCallback(() => {
      if (contextMenu?.linkUrl) {
        copyToClipboard(contextMenu.linkUrl);
      }
      closeContextMenu();
    }, [contextMenu?.linkUrl, closeContextMenu]);

    const handleContextMenu = useCallback((e: ReactMouseEvent) => {
      e.preventDefault();
      const term = xtermRef.current;
      const hasSelection = term?.hasSelection() ?? false;

      // Detect link under cursor
      let hasLink = false;
      let linkUrl = '';
      const el = xtermContainerRef.current;
      if (term && el) {
        const rect = el.getBoundingClientRect();
        if (rect.width > 0 && rect.height > 0) {
          const cellWidth = rect.width / term.cols;
          const cellHeight = rect.height / term.rows;
          const cellX = Math.floor((e.clientX - rect.left) / cellWidth);
          const cellY = Math.floor((e.clientY - rect.top) / cellHeight);
          const buf = term.buffer.active;
          const lineIdx = buf.baseY + cellY;
          const line = buf.getLine(lineIdx);
          if (line) {
            let text = '';
            for (let i = 0; i < line.length; i++) {
              text += line.getCell(i)?.getChars() || '';
            }
            // eslint-disable-next-line no-useless-escape
            const urlRegex = /https?:\/\/[\w\-._~:/?#\[\]@!$&'()*+,;=%]+/g;
            let match;
            while ((match = urlRegex.exec(text)) !== null) {
              const start = match.index;
              const end = start + match[0].length;
              if (cellX >= start && cellX < end) {
                hasLink = true;
                linkUrl = match[0];
                break;
              }
            }
          }
        }
      }

      setContextMenu({
        x: e.clientX,
        y: e.clientY,
        hasSelection,
        hasLink,
        linkUrl,
      });
    }, []);

    // Expose clear / focus to parent via ref
    useImperativeHandle(ref, () => ({
      clear: () => xtermRef.current?.clear(),
      focus: () => xtermRef.current?.focus(),
    }));

    // Initialize xterm when pane becomes active
    useEffect(() => {
      if (!isActive || !xtermContainerRef.current || xtermRef.current) return;

      const term = new XTerm({
        convertEol: false,
        cursorBlink: true,
        allowProposedApi: false,
        fontFamily: getTerminalFontFamily(),
        fontSize: fontSize ?? FONT_SIZE_DEFAULT,
        lineHeight: 1.2,
        letterSpacing: 0,
        scrollback: 5000,
        theme: getTerminalTheme(),
      });

      const fitAddon = new FitAddon();
      term.loadAddon(fitAddon);
      term.open(xtermContainerRef.current);

      xtermRef.current = term;
      fitAddonRef.current = fitAddon;

      term.onData((data) => {
        terminalWSRef.current?.sendRawInput(data);
      });

      requestAnimationFrame(() => {
        fitAddon.fit();
        term.focus();
      });

      return () => {
        try {
          term.dispose();
        } catch (err) {
          debugLog('[TerminalPane] failed to dispose xterm instance:', err);
        }
        xtermRef.current = null;
        fitAddonRef.current = null;
      };
    }, [isActive, getTerminalTheme, getTerminalFontFamily, fontSize]);

    // Keep theme and font size in sync
    useEffect(() => {
      if (!xtermRef.current) return;
      xtermRef.current.options.theme = getTerminalTheme();
      xtermRef.current.options.fontFamily = getTerminalFontFamily();
      xtermRef.current.options.fontSize = fontSize ?? FONT_SIZE_DEFAULT;
      requestAnimationFrame(() => fitAddonRef.current?.fit());
    }, [themePack.id, getTerminalTheme, getTerminalFontFamily, fontSize]);

    // Manage WebSocket connection lifecycle
    useEffect(() => {
      if (!isActive || !isConnected) {
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

      // Each pane gets its own independent WebSocket connection / PTY session
      const service = TerminalWebSocketService.createInstance();
      terminalWSRef.current = service;

      const handler = (event: WsEvent) => {
        const data = event.data as Record<string, unknown> | undefined;
        if (event.type === 'connection_status') {
          if (!data?.connected) {
            setPaneConnected(false);
            onConnectionChangeRef.current?.(false);
            xtermRef.current?.writeln('\r\nTerminal disconnected');
          }
        } else if (event.type === 'session_ready') {
          setPaneConnected(true);
          onConnectionChangeRef.current?.(true);
          requestAnimationFrame(() => {
            sendResize();
            xtermRef.current?.focus();
          });
        } else if (event.type === 'output' || event.type === 'error_output') {
          xtermRef.current?.write((data?.output as string) || '');
        } else if (event.type === 'error') {
          xtermRef.current?.write(`\r\n${data?.message as string}\r\n`);
        }
      };

      eventHandlerRef.current = handler;
      service.onEvent(handler);

      // Set the preferred shell before the initial connection so the backend
      // creates a PTY with the requested shell.
      if (preferredShellRef.current) {
        service.setPreferredShell(preferredShellRef.current);
      }

      service.connect();

      return () => {
        service.removeEvent(handler);
        if (typeof service.closeSession === 'function') {
          service.closeSession();
        }
        service.disconnect();
        terminalWSRef.current = null;
        eventHandlerRef.current = null;
      };
    }, [isActive, isConnected, sendResize]);

    // Resize observer
    useEffect(() => {
      if (!isActive || !paneConnected) return;

      const schedule = () => {
        if (resizeTimerRef.current !== null) window.clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = window.setTimeout(sendResize, 80);
      };

      schedule();
      window.addEventListener('resize', schedule);

      let observer: ResizeObserver | null = null;
      if (paneWrapperRef.current && 'ResizeObserver' in window) {
        observer = new ResizeObserver(schedule);
        observer.observe(paneWrapperRef.current);
      }

      return () => {
        window.removeEventListener('resize', schedule);
        observer?.disconnect();
        if (resizeTimerRef.current !== null) {
          window.clearTimeout(resizeTimerRef.current);
          resizeTimerRef.current = null;
        }
      };
    }, [isActive, paneConnected, sendResize]);

    // Reset context menu when pane becomes inactive or unmounts
    useEffect(() => {
      if (!isActive) {
        setContextMenu(null);
      }
    }, [isActive]);

    return (
      <div className="terminal-pane" ref={paneWrapperRef}>
        {showCloseButton && (
          <div className="terminal-pane-header">
            <span className={`terminal-pane-dot ${paneConnected ? 'connected' : 'disconnected'}`} />
            <button className="terminal-pane-close" onClick={onClose} title="Close pane">
              <X size={12} />
            </button>
          </div>
        )}
        <div
          className="terminal-pane-content"
          onClick={() => xtermRef.current?.focus()}
          onContextMenu={handleContextMenu}
        >
          <div ref={xtermContainerRef} className="terminal-xterm" />
        </div>
        {!paneConnected && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Backend not connected. Start with: <code>./ledit agent --web-port 54421</code>
          </div>
        )}
        <ContextMenu
          isOpen={contextMenu !== null}
          x={contextMenu?.x ?? 0}
          y={contextMenu?.y ?? 0}
          onClose={closeContextMenu}
        >
          <button
            className={`context-menu-item ${!contextMenu?.hasSelection ? 'disabled' : ''}`}
            onClick={handleCopy}
            disabled={!contextMenu?.hasSelection}
            type="button"
          >
            <Copy size={13} />
            <span className="menu-item-label">Copy</span>
          </button>
          <button className="context-menu-item" onClick={handlePaste} type="button">
            <ClipboardPaste size={13} />
            <span className="menu-item-label">Paste</span>
          </button>
          <div className="context-menu-divider" />
          <button className="context-menu-item" onClick={handleClear} type="button">
            <Trash2 size={13} />
            <span className="menu-item-label">Clear Terminal</span>
          </button>
          <button className="context-menu-item" onClick={handleSelectAll} type="button">
            <TextSelect size={13} />
            <span className="menu-item-label">Select All</span>
          </button>
          {contextMenu?.hasLink && (
            <>
              <div className="context-menu-divider" />
              <button className="context-menu-item" onClick={handleCopyLink} type="button">
                <Link2 size={13} />
                <span className="menu-item-label">Copy Link</span>
              </button>
            </>
          )}
        </ContextMenu>
      </div>
    );
  },
);

TerminalPane.displayName = 'TerminalPane';

export default TerminalPane;
