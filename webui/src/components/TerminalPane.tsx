import { useState, useEffect, useRef, useCallback, useImperativeHandle, forwardRef } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import ContextMenu from './ContextMenu';
import { X, TriangleAlert, Copy, ClipboardPaste, Trash2, TextSelect, Link2, Terminal } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import type { WsEvent } from '../services/websocket';
import { useTheme } from '../contexts/ThemeContext';
import { debugLog } from '../utils/log';
import { copyToClipboard } from '../utils/clipboard';
import {
  initWasmShell,
  type WasmShell,
  type WasmShellResult,
} from '../services/wasmShell';

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

    // Track whether the pane is currently mounted/active so the cleanup function
    // can distinguish between a temporary freeze and a permanent unmount.
    const isActiveRef = useRef(isActive);
    isActiveRef.current = isActive;

    // ── WASM shell state ──────────────────────────────────────────────────
    const wasmShellRef = useRef<WasmShell | null>(null);
    const [wasmActive, setWasmActive] = useState(false);
    const [wasmLoading, setWasmLoading] = useState(false);
    const [wasmError, setWasmError] = useState<string | null>(null);
    const wasmLineRef = useRef('');
    const wasmCursorRef = useRef(0);
    const wasmHistoryRef = useRef<string[]>([]);
    const wasmHistoryIdxRef = useRef(-1);
    const wasmPromptRef = useRef('\x1b[1;36muser@ledit-wasm\x1b[0m:\x1b[1;34m~\x1b[0m$ ');
    const wasmInitializedRef = useRef(false);

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

    // ── WASM shell helpers ──────────────────────────────────────────────

    /** Build the shell prompt string using the current WASM cwd. */
    const buildWasmPrompt = useCallback((cwd: string): string => {
      // Shorten /home/user to ~
      const display = cwd.startsWith('/home/user') ? ('~' + cwd.slice(10)) : cwd;
      return `\x1b[1;36muser@ledit-wasm\x1b[0m:\x1b[1;34m${display}\x1b[0m$ `;
    }, []);

    /** Write the prompt to xterm without adding a newline. */
    const writeWasmPrompt = useCallback(() => {
      const term = xtermRef.current;
      if (!term || !wasmShellRef.current) return;
      const cwd = wasmShellRef.current.getCwd();
      const prompt = buildWasmPrompt(cwd);
      wasmPromptRef.current = prompt;
      term.write(prompt);
    }, [buildWasmPrompt]);

    /** Clear the current input line (prompt + typed text) and rewrite it. */
    const rewriteWasmLine = useCallback(() => {
      const term = xtermRef.current;
      if (!term) return;
      // Move to beginning of line, clear to end of screen
      term.write('\r\x1b[2K');
      // Rewrite prompt + current buffer
      const prompt = wasmPromptRef.current;
      const line = wasmLineRef.current;
      term.write(prompt + line);
      // Position cursor correctly
      const cursorPos = wasmCursorRef.current;
      if (cursorPos < line.length) {
        term.write(`\x1b[${line.length - cursorPos}D`);
      }
    }, []);

    /** Handle a single character/data event from xterm when in WASM mode. */
    const handleWasmInput = useCallback((data: string) => {
      const term = xtermRef.current;
      const shell = wasmShellRef.current;
      if (!term || !shell) return;

      // Handle multi-character paste (length > 1) — just insert directly
      if (data.length > 1) {
        if (data === '\r' || data === '\n') {
          // Enter from paste — treat as enter
          handleWasmInput('\r');
          return;
        }
        const before = wasmLineRef.current.slice(0, wasmCursorRef.current);
        const after = wasmLineRef.current.slice(wasmCursorRef.current);
        wasmLineRef.current = before + data + after;
        wasmCursorRef.current += data.length;
        rewriteWasmLine();
        return;
      }

      const ch = data;

      if (ch === '\r' || ch === '\n') {
        // Enter — execute command
        term.write('\r\n');
        const cmd = wasmLineRef.current.trim();

        if (cmd) {
          wasmHistoryRef.current.push(cmd);
          wasmHistoryIdxRef.current = wasmHistoryRef.current.length;

          try {
            const result: WasmShellResult = shell.executeCommand(cmd);
            if (result.stdout) {
              term.write(result.stdout);
            }
            if (result.stderr) {
              term.write('\x1b[31m' + result.stderr + '\x1b[0m');
            }
          } catch (err) {
            term.write(`\x1b[31mError: ${err instanceof Error ? err.message : String(err)}\x1b[0m\r\n`);
          }
        }

        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        writeWasmPrompt();
        return;
      }

      if (ch === '\x7f' || ch === '\b') {
        // Backspace — delete character before cursor
        if (wasmCursorRef.current > 0) {
          const before = wasmLineRef.current.slice(0, wasmCursorRef.current - 1);
          const after = wasmLineRef.current.slice(wasmCursorRef.current);
          wasmLineRef.current = before + after;
          wasmCursorRef.current -= 1;
          rewriteWasmLine();
        }
        return;
      }

      if (ch === '\t') {
        // Tab completion
        const line = wasmLineRef.current;
        try {
          const compResult = shell.autoComplete(line);
          if (compResult.completions.length === 1) {
            // Single completion — apply it
            const completion = compResult.completions[0];
            wasmLineRef.current = completion;
            wasmCursorRef.current = completion.length;
            rewriteWasmLine();
            // If the completed path is a directory, append /
            if (compResult.completions.length === 1) {
              try {
                const listResult = shell.listDir(completion);
                if (listResult.entries && listResult.entries.length > 0) {
                  wasmLineRef.current += '/';
                  wasmCursorRef.current += 1;
                  rewriteWasmLine();
                }
              } catch {
                // Not a directory — fine
              }
            }
          } else if (compResult.completions.length > 1) {
            // Multiple completions — show them
            term.write('\r\n');
            for (const c of compResult.completions) {
              term.write('  ' + c + '\r\n');
            }
            rewriteWasmLine();
          }
        } catch {
          // Completion failed — ignore
        }
        return;
      }

      if (ch === '\x1b') {
        // Escape sequence start — the next onData calls will deliver [A, [B, etc.
        // xterm.js bundles these, so we handle arrow codes here directly.
        // Arrow keys arrive as \x1b[A, \x1b[B, \x1b[C, \x1b[D, \x1b[H, \x1b[F
        // But onData already delivers the full sequence, so we handle below.
        return;
      }

      if (ch === '\x1b[A') {
        // Up arrow — history previous
        const hist = wasmHistoryRef.current;
        if (hist.length === 0) return;
        if (wasmHistoryIdxRef.current > 0) {
          wasmHistoryIdxRef.current -= 1;
          wasmLineRef.current = hist[wasmHistoryIdxRef.current];
          wasmCursorRef.current = wasmLineRef.current.length;
          rewriteWasmLine();
        }
        return;
      }

      if (ch === '\x1b[B') {
        // Down arrow — history next
        const hist = wasmHistoryRef.current;
        wasmHistoryIdxRef.current += 1;
        if (wasmHistoryIdxRef.current >= hist.length) {
          wasmHistoryIdxRef.current = hist.length;
          wasmLineRef.current = '';
          wasmCursorRef.current = 0;
        } else {
          wasmLineRef.current = hist[wasmHistoryIdxRef.current];
          wasmCursorRef.current = wasmLineRef.current.length;
        }
        rewriteWasmLine();
        return;
      }

      if (ch === '\x1b[D') {
        // Left arrow — move cursor left
        if (wasmCursorRef.current > 0) {
          wasmCursorRef.current -= 1;
          term.write('\x1b[D');
        }
        return;
      }

      if (ch === '\x1b[C') {
        // Right arrow — move cursor right
        if (wasmCursorRef.current < wasmLineRef.current.length) {
          wasmCursorRef.current += 1;
          term.write('\x1b[C');
        }
        return;
      }

      if (ch === '\x1b[H' || ch === '\x01') {
        // Home or Ctrl+A — move cursor to start of line
        if (wasmCursorRef.current > 0) {
          term.write(`\x1b[${wasmCursorRef.current}D`);
          wasmCursorRef.current = 0;
        }
        return;
      }

      if (ch === '\x1b[F' || ch === '\x05') {
        // End or Ctrl+E — move cursor to end of line
        const diff = wasmLineRef.current.length - wasmCursorRef.current;
        if (diff > 0) {
          term.write(`\x1b[${diff}C`);
          wasmCursorRef.current = wasmLineRef.current.length;
        }
        return;
      }

      if (ch === '\x03') {
        // Ctrl+C — cancel current line
        term.write('^C\r\n');
        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        writeWasmPrompt();
        return;
      }

      if (ch === '\x0c') {
        // Ctrl+L — clear screen
        term.clear();
        term.write('\x1b[H'); // cursor home
        rewriteWasmLine();
        return;
      }

      if (ch === '\x15') {
        // Ctrl+U — kill line from cursor back
        const after = wasmLineRef.current.slice(wasmCursorRef.current);
        const killed = wasmCursorRef.current;
        wasmLineRef.current = after;
        wasmCursorRef.current = 0;
        if (killed > 0) {
          rewriteWasmLine();
        }
        return;
      }

      if (ch === '\x17') {
        // Ctrl+W — kill word before cursor
        const before = wasmLineRef.current.slice(0, wasmCursorRef.current);
        const trimmed = before.replace(/\S+\s*$/, '');
        const killed = before.length - trimmed.length;
        if (killed > 0) {
          wasmLineRef.current = trimmed + wasmLineRef.current.slice(wasmCursorRef.current);
          wasmCursorRef.current -= killed;
          rewriteWasmLine();
        }
        return;
      }

      // Regular printable character (check if control char)
      if (ch >= ' ' || ch === '\t') {
        const before = wasmLineRef.current.slice(0, wasmCursorRef.current);
        const after = wasmLineRef.current.slice(wasmCursorRef.current);
        wasmLineRef.current = before + ch + after;
        wasmCursorRef.current += 1;
        // Echo the character
        term.write(ch);
        // If there are characters after cursor, rewrite to maintain display
        if (after.length > 0) {
          rewriteWasmLine();
        }
      }
    }, [rewriteWasmLine, writeWasmPrompt]);

    // ── WASM shell lifecycle ─────────────────────────────────────────────

    // When backend disconnects, activate WASM shell; when it reconnects, deactivate.
    useEffect(() => {
      if (!isActive) {
        return;
      }

      if (isConnected) {
        // Backend connected — tear down WASM shell if active
        const term = xtermRef.current;
        if (wasmActive && term) {
          debugLog('[TerminalPane] Backend reconnected — deactivating WASM shell');
          term.writeln('\r\n\x1b[32m→ Connected to backend\x1b[0m');
          term.writeln('  WASM browser shell deactivated. Using remote PTY.\r\n');
          wasmLineRef.current = '';
          wasmCursorRef.current = 0;
        }
        setWasmActive(false);
        return;
      }

      // Backend not connected — activate WASM shell
      if (wasmActive || wasmLoading || wasmInitializedRef.current) {
        return; // already active or loading
      }

      let cancelled = false;

      const activateWasm = async () => {
        const term = xtermRef.current;
        if (!term) return;

        if (!wasmShellRef.current && !wasmInitializedRef.current) {
          setWasmLoading(true);
          setWasmError(null);

          try {
            const shell = await initWasmShell();
            wasmShellRef.current = shell;
            wasmInitializedRef.current = true;
            debugLog('[TerminalPane] WASM shell initialized');
          } catch (err) {
            if (cancelled) return;
            const msg = err instanceof Error ? err.message : String(err);
            setWasmError(msg);
            debugLog('[TerminalPane] WASM shell init failed:', msg);
            setWasmLoading(false);
            return;
          }
        }

        if (cancelled) return;

        setWasmLoading(false);
        setWasmActive(true);

        const shell = wasmShellRef.current;
        if (!shell || !term) return;

        term.writeln('');
        term.writeln('\x1b[33m╔══════════════════════════════════════════╗\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[1mLedit WASM Browser Shell\x1b[0m\x1b[33m               ║\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[2mGo compiled to WebAssembly\x1b[0m\x1b[33m             ║\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[2mFiles persist in IndexedDB\x1b[0m\x1b[33m            ║\x1b[0m');
        term.writeln('\x1b[33m╚══════════════════════════════════════════╝\x1b[0m');
        term.writeln('');
        term.writeln('Type \x1b[1mhelp\x1b[0m for available commands.');
        term.writeln('This shell runs \x1b[1mentirely in your browser\x1b[0m — no backend needed.');
        term.writeln('');

        // Reset state
        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        wasmHistoryRef.current = [];
        wasmHistoryIdxRef.current = -1;

        writeWasmPrompt();
      };

      activateWasm();

      return () => {
        cancelled = true;
      };
    }, [isActive, isConnected, wasmActive, wasmLoading, writeWasmPrompt]);

    const sendResize = useCallback(() => {
      if (!paneConnected || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current) return;
      fitAddonRef.current.fit();
      terminalWSRef.current.sendResize(xtermRef.current.cols, xtermRef.current.rows);
    }, [paneConnected]);

    // ── Wheel event handler for native scrolling ──
    const handleWheel = useCallback((e: WheelEvent) => {
      const term = xtermRef.current;
      if (!term) return;

      // Prevent default browser behavior
      e.preventDefault();

      // Use xterm's scroll API to scroll by the wheel delta
      // deltaY is positive for scrolling down, negative for scrolling up
      // xterm.scrollLines(negative) scrolls up, positive scrolls down
      term.scrollLines(-e.deltaY);
    }, []);

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
        allowProposedApi: true,
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

      // Add wheel event handler for native scrolling
      const container = xtermContainerRef.current;
      if (container) {
        container.addEventListener('wheel', handleWheel, { passive: false });
      }

      xtermRef.current = term;
      fitAddonRef.current = fitAddon;

      term.onData((data) => {
        if (wasmActive) {
          handleWasmInput(data);
        } else {
          terminalWSRef.current?.sendRawInput(data);
        }
      });

      requestAnimationFrame(() => {
        fitAddon.fit();
        term.focus();
      });

      return () => {
        // Remove wheel event listener
        if (container) {
          container.removeEventListener('wheel', handleWheel);
        }
        try {
          term.dispose();
        } catch (err) {
          debugLog('[TerminalPane] failed to dispose xterm instance:', err);
        }
        xtermRef.current = null;
        fitAddonRef.current = null;
      };
    }, [isActive, getTerminalTheme, getTerminalFontFamily, fontSize, handleWheel]);

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

      // Don't tear down during freeze or reconnect - wait for resume to reconnect
      // Check if the current WebSocket is frozen or actively reconnecting
      if (isConnected === false && terminalWSRef.current && (terminalWSRef.current.isCurrentlyFrozen() || terminalWSRef.current.isReconnecting())) {
        // Still frozen or reconnecting, keep the existing connection around
        return;
      }

      if (!isConnected) {
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
      // Check if we already have a terminalWS instance to avoid recreating during freeze/resume cycles
      const service = terminalWSRef.current ?? TerminalWebSocketService.createInstance();
      if (!terminalWSRef.current) {
        terminalWSRef.current = service;
      }

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
      if (preferredShellRef.current && !service.getSessionId()) {
        service.setPreferredShell(preferredShellRef.current);
      }

      // Only call connect() if we don't already have a connection.
      // During freeze/resume, the service will call connect() itself via resume(),
      // so we must not call connect() here while it is still reconnecting.
      if (!service.isConnectedToServer() && !service.isReconnecting()) {
        service.connect();
      }

      return () => {
        // If the service is frozen or actively reconnecting AND the pane is still
        // mounted, preserve the service without calling disconnect(). Remove the
        // handler so the next effect run can register a fresh one without dupes.
        if (terminalWSRef.current &&
            (service.isCurrentlyFrozen() || service.isReconnecting()) &&
            isActiveRef.current) {
          // Remove the old handler so it doesn't duplicate when the next
          // effect run registers a fresh one. Keep the service + refs intact.
          service.removeEvent(handler);
          return;
        }

        // Normal teardown path
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
            <span className={`terminal-pane-dot ${paneConnected || wasmActive ? 'connected' : 'disconnected'}`} />
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
        {!paneConnected && !wasmActive && !wasmLoading && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Backend not connected. Start with: <code>./ledit agent --web-port 54421</code>
          </div>
        )}
        {wasmLoading && (
          <div className="terminal-status-inline">
            <Terminal size={14} className="inline-block mr-1 align-text-bottom" style={{ animation: 'spin 1s linear infinite' }} />
            Initializing browser shell (loading WebAssembly)...
          </div>
        )}
        {wasmError && !wasmActive && (
          <div className="terminal-status-inline" style={{ color: '#ef6b73' }}>
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            WASM shell failed: {wasmError}
          </div>
        )}
        {wasmActive && (
          <div className="terminal-status-inline" style={{ color: '#7ddf97' }}>
            <Terminal size={14} className="inline-block mr-1 align-text-bottom" />
            Browser shell active · Go→WASM · IndexedDB persistence
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
