import React, {
  useState,
  useEffect,
  useRef,
  useCallback,
  useImperativeHandle,
  forwardRef,
} from 'react';
import { X, TriangleAlert } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { useTheme } from '../contexts/ThemeContext';
import { debugLog } from '../utils/log';

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

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  ({ isActive, isConnected = true, showCloseButton, onClose, onConnectionChange }, ref) => {
    const { themePack } = useTheme();
    const [paneConnected, setPaneConnected] = useState(false);

    const paneWrapperRef = useRef<HTMLDivElement>(null);
    const xtermContainerRef = useRef<HTMLDivElement>(null);
    const xtermRef = useRef<XTerm | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const terminalWSRef = useRef<TerminalWebSocketService | null>(null);
    const eventHandlerRef = useRef<((event: any) => void) | null>(null);
    const resizeTimerRef = useRef<number | null>(null);
    const paneConnectedRef = useRef(false);
    const onConnectionChangeRef = useRef(onConnectionChange);
    const lastHiddenTimeRef = useRef<number>(Date.now());

    // Keep refs in sync so event handlers always have the current value
    useEffect(() => {
      paneConnectedRef.current = paneConnected;
      onConnectionChangeRef.current = onConnectionChange;
    }, [paneConnected, onConnectionChange]);

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
      if (!paneConnectedRef.current || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current)
        return;
      fitAddonRef.current.fit();
      terminalWSRef.current.sendResize(xtermRef.current.cols, xtermRef.current.rows);
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
      if (!isActive) {
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

      // Connect regardless of main WS state - terminal has its own independent connection
      const service = TerminalWebSocketService.createInstance();
      // Restore persisted terminal session ID for reattach after tab discard
      service.restorePersistedSessionId();
      terminalWSRef.current = service;

      const handler = (event: any) => {
        if (event.type === 'connection_status') {
          if (!event.data.connected) {
            setPaneConnected(false);
            onConnectionChangeRef.current?.(false);
            if (event.data.reattach) {
              // Will auto-reattach - show softer message
              xtermRef.current?.writeln('\r\x1b[33m⏳ Reconnecting to terminal session...\x1b[0m');
            } else {
              xtermRef.current?.writeln('\r\nTerminal disconnected');
            }
          }
        } else if (event.type === 'session_ready') {
          setPaneConnected(true);
          onConnectionChangeRef.current?.(true);
          requestAnimationFrame(() => {
            sendResize();
            xtermRef.current?.focus();
          });
        } else if (event.type === 'session_restored') {
          // Reattached to existing tmux session - display scrollback
          const scrollback = event.data.scrollback || '';
          if (xtermRef.current) {
            xtermRef.current.clear();
            if (scrollback) {
              xtermRef.current.write(scrollback);
            }
          }
          setPaneConnected(true);
          onConnectionChangeRef.current?.(true);
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

      // Reconnect terminal WS when tab becomes visible after being backgrounded.
      // Browsers throttle timers in background tabs, killing WebSocket connections.
      const handleTerminalVisibility = () => {
        if (document.hidden) {
          lastHiddenTimeRef.current = Date.now();
          return;
        }
        // Tab became visible
        if (!isActive || !terminalWSRef.current) return;

        const hiddenDuration = Date.now() - lastHiddenTimeRef.current;
        // Reconnect if: (a) not connected at all, or (b) was hidden for more
        // than 30s. Chrome throttles timers in background tabs, so after 30s
        // the ping interval may have been skipped and the connection could be
        // half-open even though readyState still says OPEN.
        if (hiddenDuration > 30000 || !paneConnectedRef.current) {
          debugLog('[terminal:visibility] Tab visible, reconnecting terminal WS',
            hiddenDuration > 30000 ? `(hidden for ${Math.round(hiddenDuration / 1000)}s, forcing reconnect)` : '');
          terminalWSRef.current.resetAndReconnect();
        }
      };
      document.addEventListener('visibilitychange', handleTerminalVisibility);

      // Page lifecycle: freeze/resume for terminal WebSocket.
      // When Chrome freezes a background tab, WebSocket timers are completely
      // suspended and the connection may die silently. Proactively closing the
      // socket gives the server a clean close frame so it can detach from the
      // tmux session. On resume, we reconnect and reattach using the persisted
      // session ID (preserved by freeze()).
      const handleTerminalFreeze = () => {
        if (terminalWSRef.current) {
          debugLog('[terminal:lifecycle] Page freezing, proactively disconnecting terminal WS');
          terminalWSRef.current.freeze();
        }
      };
      const handleTerminalResume = () => {
        // terminalWSRef.current is only set while the pane is active
        if (terminalWSRef.current) {
          debugLog('[terminal:lifecycle] Page resumed from freeze, reconnecting terminal WS');
          terminalWSRef.current.resume();
        }
      };
      document.addEventListener('freeze', handleTerminalFreeze);
      document.addEventListener('resume', handleTerminalResume);

      return () => {
        document.removeEventListener('visibilitychange', handleTerminalVisibility);
        document.removeEventListener('freeze', handleTerminalFreeze);
        document.removeEventListener('resume', handleTerminalResume);
        service.removeEvent(handler);
        service.disconnect();
        terminalWSRef.current = null;
        eventHandlerRef.current = null;
      };
    }, [isActive]);

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
        >
          <div ref={xtermContainerRef} className="terminal-xterm" />
        </div>
        {!paneConnected && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Backend not connected. Start with: <code>./ledit agent</code>
          </div>
        )}
      </div>
    );
  }
);

TerminalPane.displayName = 'TerminalPane';

export default TerminalPane;
