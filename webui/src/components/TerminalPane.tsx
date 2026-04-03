import React, {
  useState,
  useEffect,
  useRef,
  useCallback,
  useImperativeHandle,
  forwardRef,
} from 'react';
import { createPortal } from 'react-dom';
import { X, TriangleAlert, Copy, ClipboardPaste, Trash2, TextSelect, Link2 } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { useTheme } from '../contexts/ThemeContext';
import { copyToClipboard } from '../utils/clipboard';

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
}

interface TerminalContextMenuState {
  visible: boolean;
  x: number;
  y: number;
  hasSelection: boolean;
  hasLink: boolean;
  linkUrl: string;
}

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  ({ isActive, isConnected = true, showCloseButton, onClose, onConnectionChange }, ref) => {
    const { themePack } = useTheme();
    const [paneConnected, setPaneConnected] = useState(false);
    const [contextMenu, setContextMenu] = useState<TerminalContextMenuState>({
      visible: false,
      x: 0,
      y: 0,
      hasSelection: false,
      hasLink: false,
      linkUrl: '',
    });

    const paneWrapperRef = useRef<HTMLDivElement>(null);
    const xtermContainerRef = useRef<HTMLDivElement>(null);
    const xtermRef = useRef<XTerm | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const terminalWSRef = useRef<TerminalWebSocketService | null>(null);
    const eventHandlerRef = useRef<((event: any) => void) | null>(null);
    const resizeTimerRef = useRef<number | null>(null);
    const contextMenuRef = useRef<HTMLDivElement>(null);

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
      if (!paneConnected || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current)
        return;
      fitAddonRef.current.fit();
      terminalWSRef.current.sendResize(xtermRef.current.cols, xtermRef.current.rows);
    }, [paneConnected]);

    // ── Context menu: close on outside click, scroll, Escape, or another contextmenu ──
    useEffect(() => {
      if (!contextMenu.visible) return;
      const handleClickOutside = (e: MouseEvent) => {
        if (contextMenuRef.current && !contextMenuRef.current.contains(e.target as Node)) {
          setContextMenu((prev) => ({ ...prev, visible: false }));
        }
      };
      const handleScroll = () => {
        setContextMenu((prev) => ({ ...prev, visible: false }));
      };
      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
          setContextMenu((prev) => ({ ...prev, visible: false }));
        }
      };
      const handleContextMenuOutside = () => {
        setContextMenu((prev) => ({ ...prev, visible: false }));
      };
      document.addEventListener('mousedown', handleClickOutside);
      window.addEventListener('scroll', handleScroll, true);
      document.addEventListener('keydown', handleKeyDown);
      window.addEventListener('contextmenu', handleContextMenuOutside);
      return () => {
        document.removeEventListener('mousedown', handleClickOutside);
        window.removeEventListener('scroll', handleScroll, true);
        document.removeEventListener('keydown', handleKeyDown);
        window.removeEventListener('contextmenu', handleContextMenuOutside);
      };
    }, [contextMenu.visible]);

    // ── Context menu handlers ──
    const closeContextMenu = useCallback(() => {
      setContextMenu((prev) => ({ ...prev, visible: false }));
    }, []);

    const handleCopy = useCallback(() => {
      const term = xtermRef.current;
      if (term?.hasSelection()) {
        copyToClipboard(term.getSelection());
      }
      closeContextMenu();
    }, [closeContextMenu]);

    const handlePaste = useCallback(async () => {
      try {
        const text = await navigator.clipboard.readText();
        terminalWSRef.current?.sendRawInput(text);
      } catch {
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
      if (contextMenu.linkUrl) {
        copyToClipboard(contextMenu.linkUrl);
      }
      closeContextMenu();
    }, [contextMenu.linkUrl, closeContextMenu]);

    const handleContextMenu = useCallback((e: React.MouseEvent) => {
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
            const urlRegex = /https?:\/\/[\w\-._~:/?#\[\]@!$&'()*+,;=%]+/g;
            let match;
            while ((match = urlRegex.exec(text)) !== null) {
              const start = match.index;
              const end = start + match[0].length;
              if (cellX >= start && cellX <= end) {
                hasLink = true;
                linkUrl = match[0];
                break;
              }
            }
          }
        }
      }

      // Clamp position so menu stays within viewport
      const x = Math.max(0, Math.min(e.clientX, window.innerWidth - 240));
      const y = Math.max(0, Math.min(e.clientY, window.innerHeight - 250));

      setContextMenu({
        visible: true,
        x,
        y,
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
        fontSize: 13,
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
        } catch {
          // ignore
        }
        xtermRef.current = null;
        fitAddonRef.current = null;
      };
    }, [isActive, getTerminalTheme, getTerminalFontFamily]);

    // Keep theme in sync
    useEffect(() => {
      if (!xtermRef.current) return;
      xtermRef.current.options.theme = getTerminalTheme();
      xtermRef.current.options.fontFamily = getTerminalFontFamily();
      requestAnimationFrame(() => fitAddonRef.current?.fit());
    }, [themePack.id, getTerminalTheme, getTerminalFontFamily]);

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
        onConnectionChange?.(false);
        return;
      }

      // Each pane gets its own independent WebSocket connection / PTY session
      const service = TerminalWebSocketService.createInstance();
      terminalWSRef.current = service;

      const handler = (event: any) => {
        if (event.type === 'connection_status') {
          if (!event.data.connected) {
            setPaneConnected(false);
            onConnectionChange?.(false);
            xtermRef.current?.writeln('\r\nTerminal disconnected');
          }
        } else if (event.type === 'session_ready') {
          setPaneConnected(true);
          onConnectionChange?.(true);
          requestAnimationFrame(() => {
            sendResize();
            xtermRef.current?.focus();
          });
        } else if (event.type === 'output' || event.type === 'error_output') {
          xtermRef.current?.write(event.data.output || '');
        } else if (event.type === 'error') {
          xtermRef.current?.write(`\r\n${event.data.message}\r\n`);
        }
      };

      eventHandlerRef.current = handler;
      service.onEvent(handler);
      service.connect();

      return () => {
        service.removeEvent(handler);
        service.disconnect();
        terminalWSRef.current = null;
        eventHandlerRef.current = null;
      };
    }, [isActive, isConnected, sendResize, onConnectionChange]);

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
        setContextMenu({
          visible: false,
          x: 0,
          y: 0,
          hasSelection: false,
          hasLink: false,
          linkUrl: '',
        });
      }
    }, [isActive]);

    return (
      <div className="terminal-pane" ref={paneWrapperRef}>
        {showCloseButton && (
          <div className="terminal-pane-header">
            <span
              className={`terminal-pane-dot ${paneConnected ? 'connected' : 'disconnected'}`}
            />
            <button
              className="terminal-pane-close"
              onClick={onClose}
              title="Close pane"
            >
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
        {/* Terminal context menu */}
        {contextMenu.visible &&
          createPortal(
            <div
              ref={contextMenuRef}
              className="terminal-context-menu"
              style={{ left: contextMenu.x, top: contextMenu.y }}
              onClick={(e) => e.stopPropagation()}
            >
              <button
                className={`terminal-context-item ${!contextMenu.hasSelection ? 'disabled' : ''}`}
                onClick={handleCopy}
                disabled={!contextMenu.hasSelection}
                type="button"
              >
                <Copy size={13} />
                <span className="terminal-context-item-label">Copy</span>
              </button>
              <button
                className="terminal-context-item"
                onClick={handlePaste}
                type="button"
              >
                <ClipboardPaste size={13} />
                <span className="terminal-context-item-label">Paste</span>
              </button>
              <div className="terminal-context-separator" />
              <button
                className="terminal-context-item"
                onClick={handleClear}
                type="button"
              >
                <Trash2 size={13} />
                <span className="terminal-context-item-label">Clear Terminal</span>
              </button>
              <button
                className="terminal-context-item"
                onClick={handleSelectAll}
                type="button"
              >
                <TextSelect size={13} />
                <span className="terminal-context-item-label">Select All</span>
              </button>
              {contextMenu.hasLink && (
                <>
                  <div className="terminal-context-separator" />
                  <button
                    className="terminal-context-item"
                    onClick={handleCopyLink}
                    type="button"
                  >
                    <Link2 size={13} />
                    <span className="terminal-context-item-label">Copy Link</span>
                  </button>
                </>
              )}
            </div>,
            document.body
          )}
      </div>
    );
  }
);

TerminalPane.displayName = 'TerminalPane';

export default TerminalPane;
