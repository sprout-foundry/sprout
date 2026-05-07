import { useState, useEffect, useRef, useCallback, useImperativeHandle, forwardRef } from 'react';
import { X, TriangleAlert, Terminal } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import type { IDisposable } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import '@xterm/xterm/css/xterm.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import type { WsEvent } from '../services/websocket';
import { useTheme } from '../contexts/ThemeContext';
import { debugLog } from '../utils/log';
import { registerTerminalFilePathLinks } from '../extensions/terminalFilePaths';
import { copyToClipboard } from '../utils/clipboard';
import { FONT_SIZE_DEFAULT } from './terminalConstants';
import TerminalSearchBar, { type TerminalSearchOptions, type TerminalSearchBarHandle } from './TerminalSearchBar';
import TerminalContextMenu from './TerminalContextMenu';
import ReverseSearchOverlay from './ReverseSearchOverlay';
import {
  initWasmShell,
  type WasmShell,
  type WasmShellResult,
} from '../services/wasmShell';

export interface TerminalPaneHandle {
  clear: () => void;
  focus: () => void;
  /** Cleanup the pane's WebSocket connection when the session is being closed */
  cleanup: () => void;
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
  /** Session ID to reattach to (for promoting background agent sessions to visible tabs). */
  reattachSessionId?: string | null;
  /** Called when the PTY process exits (pty_exit event from backend). */
  onProcessExit?: () => void;
  /** When true, automatically copies selected text to clipboard. */
  copyOnSelect?: boolean;
}

const EXPAND_RESIZE_DELAY_MS = 100; // Delay to allow terminal expand animation to progress before triggering resize

const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  ({ isActive, isConnected = true, showCloseButton, onClose, onConnectionChange, preferredShell, fontSize, reattachSessionId, onProcessExit, copyOnSelect = false }, ref) => {
    const { themePack } = useTheme();
    const [paneConnected, setPaneConnected] = useState(false);

    // Stabilize callback props in refs so the WebSocket lifecycle effect doesn't
    // tear down / reconnect when a parent passes an inline callback.
    const onConnectionChangeRef = useRef(onConnectionChange);
    onConnectionChangeRef.current = onConnectionChange;

    // Stabilize onProcessExit callback
    const onProcessExitRef = useRef(onProcessExit);
    onProcessExitRef.current = onProcessExit;

    // Stabilize preferredShell so the WebSocket lifecycle effect doesn't
    // tear down / reconnect when a parent changes the value.
    const preferredShellRef = useRef(preferredShell);
    preferredShellRef.current = preferredShell;

    // Stabilize reattachSessionId so the WebSocket lifecycle effect doesn't
    // tear down / reconnect when a parent changes the value.
    const reattachSessionIdRef = useRef(reattachSessionId);
    reattachSessionIdRef.current = reattachSessionId;

    const paneWrapperRef = useRef<HTMLDivElement>(null);
    const xtermContainerRef = useRef<HTMLDivElement>(null);
    const xtermRef = useRef<XTerm | null>(null);
    const fitAddonRef = useRef<FitAddon | null>(null);
    const terminalWSRef = useRef<TerminalWebSocketService | null>(null);
    const eventHandlerRef = useRef<((event: WsEvent) => void) | null>(null);
    const hasAutoFocusedReadyRef = useRef(false);
    const lastRestoreTimeRef = useRef(0);
    const resizeTimerRef = useRef<number | null>(null);
    const expandTimeoutRef = useRef<number | null>(null);
    const linkProviderRef = useRef<IDisposable | null>(null);

    // ── Search functionality ────────────────────────────────────────────────
    const searchAddonRef = useRef<SearchAddon | null>(null);
    const searchBarRef = useRef<TerminalSearchBarHandle | null>(null);
    const searchInitialQueryRef = useRef<string | null>(null);
    const [searchVisible, setSearchVisible] = useState(false);
    const [matchIndex, setMatchIndex] = useState<number | undefined>(undefined);
    const [matchCount, setMatchCount] = useState<number | undefined>(undefined);
    const [searchError, setSearchError] = useState<string | null>(null);

    // ── Reverse-i-Search overlay for PTY mode ────────────────────────────────
    const reverseSearchActiveRef = useRef(false);
    const reverseSearchQueryRef = useRef('');
    const [reverseSearchVisible, setReverseSearchVisible] = useState(false);
    const [reverseSearchQuery, setReverseSearchQuery] = useState('');
    const reverseSearchTimerRef = useRef<number | null>(null);

    // Track whether the pane is currently mounted/active so the cleanup function
    // can distinguish between a temporary freeze and a permanent unmount.
    const isActiveRef = useRef(isActive);
    isActiveRef.current = isActive;

    // Track whether component is mounted to prevent callbacks after unmount
    const isMountedRef = useRef(true);

    // Stabilize fontSize so the xterm init effect doesn't recreate the XTerm
    // instance when the user clicks zoom (+) or (-). The separate "keep font
    // size in sync" effect handles updates to the existing instance.
    const fontSizeRef = useRef(fontSize);
    fontSizeRef.current = fontSize;

    // ── WASM shell state ──────────────────────────────────────────────────
    const wasmShellRef = useRef<WasmShell | null>(null);
    const [wasmActive, setWasmActive] = useState(false);
    const wasmActiveRef = useRef(false);
    const [wasmLoading, setWasmLoading] = useState(false);
    const [wasmError, setWasmError] = useState<string | null>(null);
    const wasmLineRef = useRef('');
    const wasmCursorRef = useRef(0);
    const wasmHistoryRef = useRef<string[]>([]);
    const wasmHistoryIdxRef = useRef(-1);
    const wasmPromptRef = useRef('\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m~\x1b[0m$ ');
    const wasmInitializedRef = useRef(false);

    // ── WASM reverse-i-search state ───────────────────────────────────────────
    const wasmReverseSearchActiveRef = useRef(false);
    const wasmReverseSearchQueryRef = useRef('');
    const wasmReverseSearchResultRef = useRef('');
    const wasmReverseSearchIdxRef = useRef(-1);
    const wasmSavedLineRef = useRef('');
    const wasmSavedCursorRef = useRef(0);

    // Ref for copy-on-select debounce timer
    const copyOnSelectTimerRef = useRef<number | null>(null);
    // Stabilize copyOnSelect so the xterm init effect doesn't recreate xterm on toggle
    const copyOnSelectRef = useRef(copyOnSelect);

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
      return `\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m${display}\x1b[0m$ `;
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

      // ── Reverse-i-search mode handling ────────────────────────────────────

      // Helper to update the reverse-i-search display
      const updateReverseSearchDisplay = () => {
        term.write('\r\x1b[2K');
        const query = wasmReverseSearchQueryRef.current;
        const result = wasmReverseSearchResultRef.current;
        const display = result || '\x1b[90m(no match)\x1b[0m';
        term.write(`\x1b[1;32m(reverse-i-search)\x1b[0m'${query}': ${display}`);
      };

      // Helper to search history for a query starting from a given index
      const searchHistoryFrom = (startIndex: number) => {
        const query = wasmReverseSearchQueryRef.current.toLowerCase();
        const hist = wasmHistoryRef.current;
        if (!query) {
          // Empty query — no search
          wasmReverseSearchResultRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          return;
        }
        for (let i = startIndex; i >= 0; i--) {
          if (hist[i].toLowerCase().includes(query)) {
            wasmReverseSearchIdxRef.current = i;
            wasmReverseSearchResultRef.current = hist[i];
            return;
        }
        }
        // No match found
        wasmReverseSearchResultRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
      };

      // Helper to search for the next (earlier) match
      const searchHistoryNext = () => {
        const hist = wasmHistoryRef.current;
        const currentIdx = wasmReverseSearchIdxRef.current;
        // Search starting from one position before current match
        const startIndex = currentIdx > 0 ? currentIdx - 1 : hist.length - 1;
        searchHistoryFrom(startIndex);
      };

      // If in reverse-i-search mode, handle search-specific input
      if (wasmReverseSearchActiveRef.current) {
        // Handle multi-character paste
        if (data.length > 1) {
          // Check for known escape sequences BEFORE treating as paste
          if (data === '\x1b[D' || data === '\x1b[C') {
            // Left/Right arrow — exit search mode and put the found command on the line for editing
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          if (data === '\x1b[H' || data === '\x1b[F') {
            // Home/End — exit search mode and put command on line
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = (data === '\x1b[H') ? 0 : wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          if (data === '\x1b[A' || data === '\x1b[B') {
            // Up/Down — exit search, put command on line for editing
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          // Genuine paste — append to search query
          wasmReverseSearchQueryRef.current += data;
          searchHistoryFrom(wasmHistoryRef.current.length - 1);
          updateReverseSearchDisplay();
          return;
        }

        const ch = data;

        // Enter — accept the match and execute it
        if (ch === '\r' || ch === '\n') {
          term.write('\r\n');
          wasmReverseSearchActiveRef.current = false;
          const result = wasmReverseSearchResultRef.current;
          // Clear search state
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchIdxRef.current = -1;

          if (result) {
            // Put the matched command into the line buffer so it gets added to history
            wasmLineRef.current = result;
            wasmCursorRef.current = result.length;
            wasmReverseSearchResultRef.current = '';

            // Execute (without re-adding to history since result came from history)
            wasmHistoryIdxRef.current = wasmHistoryRef.current.length;
            try {
              const shellResult: WasmShellResult = shell.executeCommand(result);
              if (shellResult.stdout) {
                term.write(shellResult.stdout.replace(/\r?\n/g, '\r\n'));
              }
              if (shellResult.stderr) {
                term.write('\x1b[31m' + shellResult.stderr.replace(/\r?\n/g, '\r\n') + '\x1b[0m');
              }
            } catch (err) {
              term.write(`\x1b[31mError: ${err instanceof Error ? err.message : String(err)}\x1b[0m\r\n`);
            }
            wasmLineRef.current = '';
            wasmCursorRef.current = 0;
          } else {
            wasmLineRef.current = '';
            wasmCursorRef.current = 0;
            wasmReverseSearchResultRef.current = '';
          }
          writeWasmPrompt();
          return;
        }

        // Escape — cancel reverse-i-search and return to normal prompt
        if (ch === '\x1b') {
          wasmReverseSearchActiveRef.current = false;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchResultRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          // Restore saved line instead of clearing
          wasmLineRef.current = wasmSavedLineRef.current;
          wasmCursorRef.current = wasmSavedCursorRef.current;
          rewriteWasmLine();
          return;
        }

        // Ctrl+C — cancel reverse-i-search
        if (ch === '\x03') {
          term.write('^C\r\n');
          wasmReverseSearchActiveRef.current = false;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchResultRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          wasmLineRef.current = '';
          wasmCursorRef.current = 0;
          writeWasmPrompt();
          return;
        }

        // Ctrl+R — search for next earlier match
        if (ch === '\x12') {
          searchHistoryNext();
          updateReverseSearchDisplay();
          return;
        }

        // Backspace — remove last character from query
        if (ch === '\x7f' || ch === '\b') {
          const query = wasmReverseSearchQueryRef.current;
          if (query.length > 0) {
            wasmReverseSearchQueryRef.current = query.slice(0, -1);
            searchHistoryFrom(wasmHistoryRef.current.length - 1);
            updateReverseSearchDisplay();
          }
          return;
        }

        // Ctrl+A/Ctrl+E — exit search mode and put command on line
        if (ch === '\x01' || ch === '\x05') {
          wasmReverseSearchActiveRef.current = false;
          const result = wasmReverseSearchResultRef.current;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          wasmLineRef.current = result || '';
          wasmCursorRef.current = result?.length || 0;
          if (ch === '\x01') {
            wasmCursorRef.current = 0;
          }
          rewriteWasmLine();
          return;
        }

        // Regular printable character — append to search query and search
        if (ch >= ' ' || ch === '\t') {
          wasmReverseSearchQueryRef.current += ch;
          searchHistoryFrom(wasmHistoryRef.current.length - 1);
          updateReverseSearchDisplay();
          return;
        }

        // Any other control character — exit search mode and handle normally
        // (will be processed in the normal mode section below)
        wasmReverseSearchActiveRef.current = false;
        wasmReverseSearchQueryRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
        const result = wasmReverseSearchResultRef.current;
        wasmLineRef.current = result || '';
        wasmCursorRef.current = wasmLineRef.current.length;
        rewriteWasmLine();
        // Fall through to normal handling for the control character
      }

      // ── Normal WASM mode handling (not in reverse-i-search) ───────────────

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

      if (ch === '\x12') {
        // Ctrl+R — enter reverse-i-search mode
        // Save current line buffer before entering search
        wasmSavedLineRef.current = wasmLineRef.current;
        wasmSavedCursorRef.current = wasmCursorRef.current;
        wasmReverseSearchActiveRef.current = true;
        wasmReverseSearchQueryRef.current = '';
        wasmReverseSearchResultRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
        // Clear current line and show search prompt
        term.write('\r\x1b[2K');
        term.write('\x1b[1;32m(reverse-i-search)\x1b[0m\'\': ');
        return;
      }

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
              // Convert \n to \r\n for xterm (convertEol is false).
              term.write(result.stdout.replace(/\r?\n/g, '\r\n'));
            }
            if (result.stderr) {
              term.write('\x1b[31m' + result.stderr.replace(/\r?\n/g, '\r\n') + '\x1b[0m');
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
        // Escape — clear current line (exit to empty prompt)
        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        rewriteWasmLine();
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
        if (wasmActiveRef.current && term) {
          debugLog('[TerminalPane] Backend connected — switching to remote PTY');
          term.writeln('\r\n\x1b[32m→ Connected to workspace\x1b[0m');
          term.writeln('  Switching to remote terminal.\r\n');
          wasmLineRef.current = '';
          wasmCursorRef.current = 0;
        }
        wasmActiveRef.current = false;
        setWasmActive(false);
        // Clear reverse-search state when switching to PTY mode
        reverseSearchActiveRef.current = false;
        reverseSearchQueryRef.current = '';
        setReverseSearchVisible(false);
        setReverseSearchQuery('');
        return;
      }

      // No backend connection — WASM shell is the default terminal
      if (wasmActiveRef.current || wasmLoading || wasmInitializedRef.current) {
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
        wasmActiveRef.current = true;
        setWasmActive(true);

        const shell = wasmShellRef.current;
        if (!shell || !term) return;

        term.writeln('');
        term.writeln('\x1b[33m╔══════════════════════════════════════════╗\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[1mSprout WASM Browser Shell\x1b[0m\x1b[33m              ║\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[2mGo compiled to WebAssembly\x1b[0m\x1b[33m             ║\x1b[0m');
        term.writeln('\x1b[33m║  \x1b[2mFiles persist in IndexedDB\x1b[0m\x1b[33m            ║\x1b[0m');
        term.writeln('\x1b[33m╚══════════════════════════════════════════╝\x1b[0m');
        term.writeln('');
        term.writeln('Type \x1b[1mhelp\x1b[0m for available commands.');
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

    const paneConnectedRef = useRef(paneConnected);
    paneConnectedRef.current = paneConnected;

    const sendResize = useCallback(() => {
      if (!paneConnectedRef.current || !terminalWSRef.current || !xtermRef.current || !fitAddonRef.current) return;
      fitAddonRef.current.fit();
      const cols = xtermRef.current.cols;
      const rows = xtermRef.current.rows;
      // Guard against zero/NaN dimensions — these cause process.stdout.columns
      // to be 0 in Node.js child processes (e.g. tools using sharp image resize).
      if (!cols || !rows || cols < 1 || rows < 1) return;
      terminalWSRef.current.sendResize(cols, rows);
    }, []);

    // ── Wheel event handler ──
    // xterm.js v6 handles scroll natively via its .xterm-viewport element.
    // We only need to prevent scroll-chaining to the outer page when the
    // user scrolls inside the terminal.
    const handleWheel = useCallback((e: WheelEvent) => {
      const term = xtermRef.current;
      if (!term) return;

      // Determine whether xterm has scrollback content above or below the
      // current viewport. If the user is scrolling in a direction that has
      // no further content, let the event propagate so the page can scroll.
      const buffer = term.buffer.active;
      const atTop = buffer.viewportY === 0;
      const atBottom = buffer.viewportY === buffer.baseY;

      const scrollingUp = e.deltaY < 0;
      const scrollingDown = e.deltaY > 0;

      // If there is room to scroll in the requested direction, contain the
      // event so it doesn't bubble to the page.
      if ((scrollingUp && !atTop) || (scrollingDown && !atBottom)) {
        e.preventDefault();
      }
      // Do NOT call e.stopPropagation() — let xterm's own viewport handler
      // process the wheel event for smooth, native scrolling.
    }, []);

    // ── Search functionality handlers ────────────────────────────────────────

    // Handle search errors from the search bar
    const handleSearchError = useCallback((message: string | null) => {
      setSearchError(message);
    }, []);

    // Handle search requests from the search bar
    const handleSearch = useCallback(
      (options: TerminalSearchOptions, direction: 'next' | 'previous') => {
        const term = xtermRef.current;
        const searchAddon = searchAddonRef.current;
        if (!term || !searchAddon) return;

        const { query, caseSensitive, regex } = options;
        const searchOptions = {
          caseSensitive,
          regex,
          wholeWord: false,
        };

        try {
          setSearchError(null);
          if (direction === 'next') {
            searchAddon.findNext(query, searchOptions);
          } else {
            searchAddon.findPrevious(query, searchOptions);
          }
        } catch (err) {
          const message = err instanceof Error ? err.message : String(err);
          setSearchError(message);
        }
      },
      [],
    );

    // Close search bar and clear decorations
    const handleCloseSearch = useCallback(() => {
      setSearchVisible(false);
      searchAddonRef.current?.clearDecorations();
      setMatchIndex(undefined);
      setMatchCount(undefined);
      setSearchError(null);
      xtermRef.current?.focus();
    }, []);

    // ── PTY input handler with reverse-i-search tracking ─────────────────────

    /** Batch reverse-search query updates to avoid excessive re-renders */
    const scheduleReverseSearchUpdate = useCallback(() => {
      if (reverseSearchTimerRef.current !== null) {
        clearTimeout(reverseSearchTimerRef.current);
      }
      reverseSearchTimerRef.current = window.setTimeout(() => {
        if (isMountedRef.current) {
          setReverseSearchQuery(reverseSearchQueryRef.current);
        }
        reverseSearchTimerRef.current = null;
      }, 20);
    }, []);

    /**
     * Handle input for remote PTY sessions with reverse-i-search overlay support.
     *
     * When not in WASM mode, this monitors input for Ctrl+R (reverse-i-search) and
     * maintains overlay state showing the search query. The overlay is purely visual
     * and doesn't interfere with the actual PTY data flow.
     */
    const handlePtyInput = useCallback((data: string) => {
      // Send data to PTY first (don't intercept)
      terminalWSRef.current?.sendRawInput(data);

      // Track reverse-i-search mode for the overlay
      // Note: This is purely for visualization - the PTY handles Ctrl+R natively

      // Handle multi-character data (escape sequences or paste)
      if (data.length > 1) {
        // Escape sequences (arrow keys, Home/End, etc.) — exit reverse-search mode
        if (data.startsWith('\x1b')) {
          if (reverseSearchActiveRef.current) {
            reverseSearchActiveRef.current = false;
            reverseSearchQueryRef.current = '';
            setReverseSearchVisible(false);
            setReverseSearchQuery('');
          }
          return;
        }
        // Multi-character paste in reverse-i-search mode
        if (reverseSearchActiveRef.current) {
          reverseSearchQueryRef.current += data;
          scheduleReverseSearchUpdate();
        }
        return;
      }

      const ch = data;

      // Ctrl+R — enter reverse-i-search mode OR find next match
      if (ch === '\x12') {
        if (!reverseSearchActiveRef.current) {
          // First Ctrl+R: enter reverse-i-search mode
          reverseSearchActiveRef.current = true;
          reverseSearchQueryRef.current = '';
          setReverseSearchVisible(true);
          setReverseSearchQuery('');
        }
        // If already in reverse-search mode, the PTY handles finding the next match
        // We just stay in the mode (don't clear the query)
        return;
      }

      // Exit reverse-i-search on these keys
      if (ch === '\r' || ch === '\n' || ch === '\x03' || ch === '\x1b') {
        // Enter, Ctrl+C, or Escape
        reverseSearchActiveRef.current = false;
        reverseSearchQueryRef.current = '';
        setReverseSearchVisible(false);
        setReverseSearchQuery('');
        return;
      }

      // Backspace in reverse-i-search mode
      if (ch === '\x7f' || ch === '\b') {
        if (reverseSearchActiveRef.current && reverseSearchQueryRef.current.length > 0) {
          reverseSearchQueryRef.current = reverseSearchQueryRef.current.slice(0, -1);
          scheduleReverseSearchUpdate();
        }
        return;
      }

      // Regular printable character while in reverse-i-search mode
      if (reverseSearchActiveRef.current && (ch >= ' ' || ch === '\t')) {
        reverseSearchQueryRef.current += ch;
        scheduleReverseSearchUpdate();
        return;
      }
    }, [scheduleReverseSearchUpdate]);

    // ── Context menu handlers for TerminalContextMenu ───────────────────────

    const getXTerminal = useCallback(() => xtermRef.current, []);

    const hasXTermSelection = useCallback(() => xtermRef.current?.hasSelection() ?? false, []);

    const handleContextCopy = useCallback((text: string) => {
      copyToClipboard(text).catch((err) => {
        debugLog('[TerminalPane] clipboard copy failed:', err);
      });
    }, []);

    const handleContextPaste = useCallback((text: string) => {
      if (wasmActiveRef.current) {
        handleWasmInput(text);
      } else {
        handlePtyInput(text);
      }
    }, [handleWasmInput, handlePtyInput]);

    const handleContextSearch = useCallback(() => {
      const sel = xtermRef.current?.getSelection();
      searchInitialQueryRef.current = (sel && sel.trim()) ? sel.trim() : null;
      setSearchVisible(true);
    }, []);

    const handleContextClear = useCallback(() => {
      xtermRef.current?.clear();
    }, []);

    const handleContextSelectAll = useCallback(() => {
      xtermRef.current?.selectAll();
    }, []);

    const handleContextSplitPane = useCallback((direction: 'horizontal' | 'vertical') => {
      const action = direction === 'horizontal' ? 'split_horizontal' : 'split_vertical';
      window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action } }));
    }, []);

    // ── Expose methods to parent ─────────────────────────────────────────────

    // Expose clear / focus / cleanup to parent via ref
    useImperativeHandle(ref, () => ({
      clear: () => xtermRef.current?.clear(),
      focus: () => xtermRef.current?.focus(),
      cleanup: () => {
        const service = terminalWSRef.current;
        if (!service) return;

        // Remove the event handler
        if (eventHandlerRef.current) {
          service.removeEvent(eventHandlerRef.current);
          eventHandlerRef.current = null;
        }

        // Close the session and disconnect
        // This works even if the service is frozen or reconnecting
        service.closeSession();
        service.disconnect();

        // Clear refs
        terminalWSRef.current = null;
        setPaneConnected(false);
        onConnectionChangeRef.current?.(false);
      },
    }));

    // Initialize xterm when pane becomes active
    useEffect(() => {
      if (!isActive || !xtermContainerRef.current || xtermRef.current) return;

      const term = new XTerm({
        convertEol: false,
        cursorBlink: true,
        allowProposedApi: true,
        fontFamily: getTerminalFontFamily(),
        fontSize: fontSizeRef.current ?? FONT_SIZE_DEFAULT,
        lineHeight: 1.2,
        letterSpacing: 0,
        scrollback: 5000,
        theme: getTerminalTheme(),
      });

      const fitAddon = new FitAddon();
      const searchAddon = new SearchAddon();
      term.loadAddon(fitAddon);
      term.loadAddon(searchAddon);
      term.open(xtermContainerRef.current);

      linkProviderRef.current = registerTerminalFilePathLinks(term);

      // Add wheel event handler to prevent page scroll-chaining when the
      // terminal has scrollback content. xterm handles actual scrolling natively.
      const container = xtermContainerRef.current;
      if (container) {
        container.addEventListener('wheel', handleWheel, { passive: false });
      }

      xtermRef.current = term;

      // Intercept Ctrl+Shift+C (copy) and Ctrl+Shift+V (paste) before xterm
      // processes them, so the user can copy selections and paste from the
      // clipboard — a standard expectation in terminal emulators.
      term.attachCustomKeyEventHandler((event: KeyboardEvent) => {
        if (event.ctrlKey && event.shiftKey && !event.altKey && !event.metaKey) {
          if (event.key.toLowerCase() === 'c') {
            event.preventDefault();
            if (term.hasSelection()) {
              copyToClipboard(term.getSelection()).catch((err) => {
                debugLog('[TerminalPane] clipboard copy failed:', err);
              });
            }
            return false;
          }
          if (event.key.toLowerCase() === 'v') {
            event.preventDefault();
            navigator.clipboard.readText().then((text) => {
              if (wasmActiveRef.current) {
                handleWasmInput(text);
              } else {
                handlePtyInput(text);
              }
            }).catch((err) => {
              debugLog('[TerminalPane] clipboard paste failed:', err);
            });
            return false;
          }
          if (event.key.toLowerCase() === 'f') {
            event.preventDefault();
            if (searchVisible) {
              // Already visible — close it
              searchAddonRef.current?.clearDecorations();
              setSearchVisible(false);
            } else {
              // Capture selection before toggling visibility (ref is null during updater)
              const sel = xtermRef.current?.getSelection();
              searchInitialQueryRef.current = (sel && sel.trim()) ? sel.trim() : null;
              setSearchVisible(true);
            }
            return false;
          }
        }
        return true;
      });

      fitAddonRef.current = fitAddon;
      searchAddonRef.current = searchAddon;

      // Set up search results listener (store disposable for cleanup)
      const resultsDisposable = searchAddon.onDidChangeResults((results: { resultIndex?: number; resultCount?: number }) => {
        setMatchIndex(results.resultIndex);
        setMatchCount(results.resultCount);
      });

      term.onData((data) => {
        if (wasmActiveRef.current) {
          handleWasmInput(data);
        } else {
          handlePtyInput(data);
        }
      });

      // Set up copy-on-select handler with debounce
      const selectionChangeDisposable = term.onSelectionChange(() => {
        if (copyOnSelectRef.current && term.hasSelection()) {
          // Clear any pending copy timer
          if (copyOnSelectTimerRef.current !== null) {
            clearTimeout(copyOnSelectTimerRef.current);
          }
          // Set a new timer with 150ms debounce
          copyOnSelectTimerRef.current = window.setTimeout(() => {
            // Re-check in case feature was toggled off during debounce
            if (!copyOnSelectRef.current) {
              copyOnSelectTimerRef.current = null;
              return;
            }
            try {
              const selection = term.getSelection();
              if (selection) {
                copyToClipboard(selection).catch((err) => {
                  debugLog('[TerminalPane] copy-on-select failed:', err);
                });
              }
            } catch (err) {
              debugLog('[TerminalPane] copy-on-select failed:', err);
            }
            copyOnSelectTimerRef.current = null;
          }, 150);
        }
      });

      requestAnimationFrame(() => {
        fitAddon.fit();
        term.focus();
      });

      return () => {
        linkProviderRef.current?.dispose();
        linkProviderRef.current = null;
        // Dispose search results listener
        resultsDisposable.dispose();
        // Dispose selection change listener
        selectionChangeDisposable.dispose();
        // Clear copy-on-select timer
        if (copyOnSelectTimerRef.current !== null) {
          clearTimeout(copyOnSelectTimerRef.current);
          copyOnSelectTimerRef.current = null;
        }
        // Clear reverse-search timer
        if (reverseSearchTimerRef.current !== null) {
          clearTimeout(reverseSearchTimerRef.current);
          reverseSearchTimerRef.current = null;
        }
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
        searchAddonRef.current = null;
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps -- fontSize intentionally excluded to avoid recreating xterm on zoom; updates handled by separate "keep font size in sync" effect
    }, [isActive, getTerminalTheme, getTerminalFontFamily, handleWheel]);

    // Keep theme and font size in sync
    useEffect(() => {
      if (!xtermRef.current) return;
      xtermRef.current.options.theme = getTerminalTheme();
      xtermRef.current.options.fontFamily = getTerminalFontFamily();
      xtermRef.current.options.fontSize = fontSize ?? FONT_SIZE_DEFAULT;
      requestAnimationFrame(() => fitAddonRef.current?.fit());
    }, [themePack.id, getTerminalTheme, getTerminalFontFamily, fontSize]);

    // Keep copyOnSelect ref in sync
    useEffect(() => {
      copyOnSelectRef.current = copyOnSelect;
      // Cancel any pending copy timer when feature is toggled off
      if (!copyOnSelect && copyOnSelectTimerRef.current !== null) {
        clearTimeout(copyOnSelectTimerRef.current);
        copyOnSelectTimerRef.current = null;
      }
    }, [copyOnSelect]);

    // Manage WebSocket connection lifecycle
    useEffect(() => {
      if (!isActive) {
        if (eventHandlerRef.current && terminalWSRef.current) {
          terminalWSRef.current.removeEvent(eventHandlerRef.current);
          terminalWSRef.current.disconnect();
        }
        eventHandlerRef.current = null;
        terminalWSRef.current = null;
        hasAutoFocusedReadyRef.current = false;
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
          // Skip resize if we just restored — session_restored already sent it
          // to avoid duplicate SIGWINCH that causes prompt line duplication.
          if (Date.now() - lastRestoreTimeRef.current < 5000) {
            return;
          }
          const shouldAutoFocus = !hasAutoFocusedReadyRef.current;
          if (shouldAutoFocus) {
            hasAutoFocusedReadyRef.current = true;
          }
          requestAnimationFrame(() => {
            sendResize();
            if (shouldAutoFocus) {
              xtermRef.current?.focus();
            }
          });
        } else if (event.type === 'output' || event.type === 'error_output') {
          xtermRef.current?.write((data?.output as string) || '');
        } else if (event.type === 'session_restored') {
          // Reattach: reset the terminal to prevent duplicating content
          // that was already displayed. The server sends its ring buffer
          // scrollback which we write into the fresh terminal.
          // Close search bar and clear search state before reset
          setSearchVisible(false);
          searchAddonRef.current?.clearDecorations();
          setMatchIndex(undefined);
          setMatchCount(undefined);
          setSearchError(null);

          const term = xtermRef.current;
          if (term) {
            term.reset();
            const scrollback = (data?.scrollback as string) || '';
            if (scrollback) {
              term.write(scrollback);
            }
          }
          // Record restore time so session_ready and resize observer can
          // skip their own resize — we send it here to avoid multiple
          // SIGWINCH events that cause prompt line duplication.
          lastRestoreTimeRef.current = Date.now();
          setPaneConnected(true);
          onConnectionChangeRef.current?.(true);
          requestAnimationFrame(() => {
            sendResize();
            xtermRef.current?.focus();
          });
        } else if (event.type === 'pty_exit') {
          xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
          setPaneConnected(false);
          onConnectionChangeRef.current?.(false);

          // Clean up the WebSocket connection for this dead session
          const service = terminalWSRef.current;
          if (service && eventHandlerRef.current) {
            service.removeEvent(eventHandlerRef.current);
            eventHandlerRef.current = null;
          }
          if (service) {
            service.closeSession();
            service.disconnect();
            terminalWSRef.current = null;
          }

          onProcessExitRef.current?.();
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

      // If a reattach session ID is provided, restore it so the WebSocket connects
      // to the existing PTY session instead of creating a new one.
      if (reattachSessionIdRef.current && !service.getSessionId()) {
        service.restoreSessionId(reattachSessionIdRef.current);
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

      // Skip the immediate resize if we just restored from a reattach — the
      // session_restored handler already sent a resize to avoid duplicate
      // SIGWINCH events that cause prompt line duplication.
      if (Date.now() - lastRestoreTimeRef.current > 5000) {
        schedule();
      }
      window.addEventListener('resize', schedule);

      let observer: ResizeObserver | null = null;
      if ('ResizeObserver' in window) {
        observer = new ResizeObserver(schedule);
        if (paneWrapperRef.current) observer.observe(paneWrapperRef.current);
        // Also observe the xterm container: its size changes when the pane
        // header is added (e.g. in the secondary split pane), even though the
        // outer wrapper stays the same size.
        if (xtermContainerRef.current) observer.observe(xtermContainerRef.current);
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

    // Listen for terminal expand event to force a resize
    // This fixes the issue where terminal doesn't fill space after reopening
    useEffect(() => {
      if (!isActive || !paneConnected) return;

      const handleExpand = () => {
        // 100ms delay allows the terminal expand animation to progress before triggering resize.
        // This prevents the terminal from being sized incorrectly during the early phase of expansion.
        // Note: CSS transition is 280ms, but xterm.fit() works reliably after this shorter delay.
        expandTimeoutRef.current = window.setTimeout(() => {
          if (isMountedRef.current) {
            sendResize();
          }
        }, EXPAND_RESIZE_DELAY_MS);
      };

      window.addEventListener('sprout-terminal-expand', handleExpand);
      return () => {
        window.removeEventListener('sprout-terminal-expand', handleExpand);
        if (expandTimeoutRef.current !== null) {
          window.clearTimeout(expandTimeoutRef.current);
          expandTimeoutRef.current = null;
        }
      };
    }, [isActive, paneConnected, sendResize]);

    // Global cleanup: set isMountedRef to false on unmount
    useEffect(() => {
      return () => {
        isMountedRef.current = false;
      };
    }, []);

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
        <TerminalSearchBar
          ref={searchBarRef}
          visible={searchVisible}
          onSearch={handleSearch}
          onClose={handleCloseSearch}
          matchIndex={matchIndex}
          matchCount={matchCount}
          searchError={searchError}
          onSearchError={handleSearchError}
          initialQuery={searchInitialQueryRef.current}
        />
        <div
          className="terminal-pane-content"
          onClick={() => xtermRef.current?.focus()}
        >
          <div ref={xtermContainerRef} className="terminal-xterm" />
          {!wasmActive && (
            <ReverseSearchOverlay
              query={reverseSearchQuery}
              visible={reverseSearchVisible}
            />
          )}
        </div>
        {!paneConnected && !wasmActive && !wasmLoading && (
          <div className="terminal-status-inline">
            <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
            Loading terminal...
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
            Browser shell · Files persist in IndexedDB
          </div>
        )}
        <TerminalContextMenu
          containerRef={xtermContainerRef}
          getTerminal={getXTerminal}
          hasSelection={hasXTermSelection}
          onCopy={handleContextCopy}
          onPaste={handleContextPaste}
          onSearch={handleContextSearch}
          onClear={handleContextClear}
          onSelectAll={handleContextSelectAll}
          onSplitPane={handleContextSplitPane}
        />
      </div>
    );
  },
);

TerminalPane.displayName = 'TerminalPane';

export default TerminalPane;
