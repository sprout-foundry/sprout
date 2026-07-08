/**
 * useWasmTerminalInput - manages the WASM browser shell input handling
 * for the TerminalPane component.
 *
 * Extracted from TerminalPane.tsx. Handles all WASM shell state,
 * input processing (including reverse-i-search within WASM mode),
 * and WASM shell lifecycle (activation when backend disconnects).
 */

import type { Terminal as XTerm } from '@xterm/xterm';
import { useRef, useState, useCallback, useEffect } from 'react';
import { initWasmShell, type WasmShell, type WasmShellResult } from '../services/wasmShell';
import { debugLog } from '../utils/log';

export interface UseWasmTerminalInputOptions {
  xtermRef: React.RefObject<XTerm | null>;
  isActive: boolean;
  isConnected: boolean;
}

export interface UseWasmTerminalInputReturn {
  wasmActive: boolean;
  wasmActiveRef: React.MutableRefObject<boolean>;
  wasmLoading: boolean;
  wasmError: string | null;
  handleWasmInput: (data: string) => void;
}

export function useWasmTerminalInput(options: UseWasmTerminalInputOptions): UseWasmTerminalInputReturn {
  const { xtermRef, isActive, isConnected } = options;

  // ── WASM shell state ──────────────────────────────────────────────
  const wasmShellRef = useRef<WasmShell | null>(null);
  const [wasmActive, setWasmActive] = useState(false);
  const wasmActiveRef = useRef(false);
  const [wasmLoading, setWasmLoading] = useState(false);
  const [wasmError, setWasmError] = useState<string | null>(null);
  const [retryCount, setRetryCount] = useState(0);
  const wasmLineRef = useRef('');
  const wasmCursorRef = useRef(0);
  const wasmHistoryRef = useRef<string[]>([]);
  const wasmHistoryIdxRef = useRef(-1);
  const wasmPromptRef = useRef('\x1b[1;36muser@sprout-wasm\x1b[0m:\x1b[1;34m~\x1b[0m$ ');
  const wasmInitializedRef = useRef(false);

  // ── WASM reverse-i-search state ───────────────────────────────────
  const wasmReverseSearchActiveRef = useRef(false);
  const wasmReverseSearchQueryRef = useRef('');
  const wasmReverseSearchResultRef = useRef('');
  const wasmReverseSearchIdxRef = useRef(-1);
  const wasmSavedLineRef = useRef('');
  const wasmSavedCursorRef = useRef(0);

  // ── WASM shell helpers ────────────────────────────────────────────

  /** Build the shell prompt string using the current WASM cwd. */
  const buildWasmPrompt = useCallback((cwd: string): string => {
    const display = cwd.startsWith('/home/user') ? '~' + cwd.slice(10) : cwd;
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
  }, [xtermRef, buildWasmPrompt]);

  /** Clear the current input line (prompt + typed text) and rewrite it. */
  const rewriteWasmLine = useCallback(() => {
    const term = xtermRef.current;
    if (!term) return;
    term.write('\r\x1b[2K');
    const prompt = wasmPromptRef.current;
    const line = wasmLineRef.current;
    term.write(prompt + line);
    const cursorPos = wasmCursorRef.current;
    if (cursorPos < line.length) {
      term.write(`\x1b[${line.length - cursorPos}D`);
    }
  }, [xtermRef]);

  /** Handle a single character/data event from xterm when in WASM mode. */
  const handleWasmInput = useCallback(
    (data: string) => {
      const term = xtermRef.current;
      const shell = wasmShellRef.current;
      if (!term || !shell) return;

      // ── Reverse-i-search mode handling helpers ─────────────────────

      const updateReverseSearchDisplay = () => {
        term.write('\r\x1b[2K');
        const query = wasmReverseSearchQueryRef.current;
        const result = wasmReverseSearchResultRef.current;
        const display = result || '\x1b[90m(no match)\x1b[0m';
        term.write(`\x1b[1;32m(reverse-i-search)\x1b[0m'${query}': ${display}`);
      };

      const searchHistoryFrom = (startIndex: number) => {
        const query = wasmReverseSearchQueryRef.current.toLowerCase();
        const hist = wasmHistoryRef.current;
        if (!query) {
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
        wasmReverseSearchResultRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
      };

      const searchHistoryNext = () => {
        const hist = wasmHistoryRef.current;
        const currentIdx = wasmReverseSearchIdxRef.current;
        const startIndex = currentIdx >= 0 ? currentIdx - 1 : hist.length - 1;
        searchHistoryFrom(startIndex);
      };

      // ── If in reverse-i-search mode, handle search-specific input ──

      if (wasmReverseSearchActiveRef.current) {
        if (data.length > 1) {
          if (data.startsWith('\x1b[D') || data.startsWith('\x1b[C')) {
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          if (data.startsWith('\x1b[H') || data.startsWith('\x1b[F')) {
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = data.startsWith('\x1b[H') ? 0 : wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          if (data.startsWith('\x1b[A') || data.startsWith('\x1b[B')) {
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          if (data.startsWith('\x1b')) {
            wasmReverseSearchActiveRef.current = false;
            const result = wasmReverseSearchResultRef.current;
            wasmReverseSearchQueryRef.current = '';
            wasmReverseSearchIdxRef.current = -1;
            wasmLineRef.current = result || '';
            wasmCursorRef.current = wasmLineRef.current.length;
            rewriteWasmLine();
            return;
          }
          const printable = data.replace(/[\x00-\x1f\x7f-\x9f]/g, '');
          if (printable) {
            wasmReverseSearchQueryRef.current += printable;
            searchHistoryFrom(wasmHistoryRef.current.length - 1);
            updateReverseSearchDisplay();
          }
          return;
        }

        const ch = data;

        if (ch === '\r' || ch === '\n') {
          term.write('\r\n');
          wasmReverseSearchActiveRef.current = false;
          const result = wasmReverseSearchResultRef.current;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          if (result) {
            wasmLineRef.current = result;
            wasmCursorRef.current = result.length;
            wasmReverseSearchResultRef.current = '';
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

        if (ch === '\x1b') {
          wasmReverseSearchActiveRef.current = false;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchResultRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          wasmLineRef.current = wasmSavedLineRef.current;
          wasmCursorRef.current = wasmSavedCursorRef.current;
          rewriteWasmLine();
          return;
        }

        if (ch === '\x03') {
          term.write('^C\r\n');
          wasmReverseSearchActiveRef.current = false;
          wasmReverseSearchQueryRef.current = '';
          wasmReverseSearchResultRef.current = '';
          wasmReverseSearchIdxRef.current = -1;
          wasmLineRef.current = wasmSavedLineRef.current;
          wasmCursorRef.current = wasmSavedCursorRef.current;
          rewriteWasmLine();
          return;
        }

        if (ch === '\x12') {
          searchHistoryNext();
          updateReverseSearchDisplay();
          return;
        }

        if (ch === '\x7f' || ch === '\b') {
          const query = wasmReverseSearchQueryRef.current;
          if (query.length > 0) {
            wasmReverseSearchQueryRef.current = query.slice(0, -1);
            searchHistoryFrom(wasmHistoryRef.current.length - 1);
            updateReverseSearchDisplay();
          }
          return;
        }

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

        if (ch >= ' ' || ch === '\t') {
          wasmReverseSearchQueryRef.current += ch;
          searchHistoryFrom(wasmHistoryRef.current.length - 1);
          updateReverseSearchDisplay();
          return;
        }

        wasmReverseSearchActiveRef.current = false;
        wasmReverseSearchQueryRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
        const result = wasmReverseSearchResultRef.current;
        wasmLineRef.current = result || '';
        wasmCursorRef.current = wasmLineRef.current.length;
        rewriteWasmLine();
        // Fall through to normal handling for the control character
      }

      // ── Normal WASM mode handling ──────────────────────────────────

      if (data.length > 1) {
        if (data === '\r' || data === '\n') {
          // Recursively handle enter
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
        wasmSavedLineRef.current = wasmLineRef.current;
        wasmSavedCursorRef.current = wasmCursorRef.current;
        wasmReverseSearchActiveRef.current = true;
        wasmReverseSearchQueryRef.current = '';
        wasmReverseSearchResultRef.current = '';
        wasmReverseSearchIdxRef.current = -1;
        term.write('\r\x1b[2K');
        term.write("\x1b[1;32m(reverse-i-search)\x1b[0m'': ");
        return;
      }

      if (ch === '\r' || ch === '\n') {
        term.write('\r\n');
        const cmd = wasmLineRef.current.trim();
        if (cmd) {
          wasmHistoryRef.current.push(cmd);
          wasmHistoryIdxRef.current = wasmHistoryRef.current.length;
          try {
            const res: WasmShellResult = shell.executeCommand(cmd);
            if (res.stdout) {
              term.write(res.stdout.replace(/\r?\n/g, '\r\n'));
            }
            if (res.stderr) {
              term.write('\x1b[31m' + res.stderr.replace(/\r?\n/g, '\r\n') + '\x1b[0m');
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
        const line = wasmLineRef.current;
        try {
          const compResult = shell.autoComplete(line);
          if (compResult.completions.length === 1) {
            const completion = compResult.completions[0];
            wasmLineRef.current = completion;
            wasmCursorRef.current = completion.length;
            rewriteWasmLine();
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
        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        rewriteWasmLine();
        return;
      }

      if (ch === '\x1b[A') {
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
        if (wasmCursorRef.current > 0) {
          wasmCursorRef.current -= 1;
          term.write('\x1b[D');
        }
        return;
      }

      if (ch === '\x1b[C') {
        if (wasmCursorRef.current < wasmLineRef.current.length) {
          wasmCursorRef.current += 1;
          term.write('\x1b[C');
        }
        return;
      }

      if (ch === '\x1b[H' || ch === '\x01') {
        if (wasmCursorRef.current > 0) {
          term.write(`\x1b[${wasmCursorRef.current}D`);
          wasmCursorRef.current = 0;
        }
        return;
      }

      if (ch === '\x1b[F' || ch === '\x05') {
        const diff = wasmLineRef.current.length - wasmCursorRef.current;
        if (diff > 0) {
          term.write(`\x1b[${diff}C`);
          wasmCursorRef.current = wasmLineRef.current.length;
        }
        return;
      }

      if (ch === '\x03') {
        term.write('^C\r\n');
        wasmLineRef.current = '';
        wasmCursorRef.current = 0;
        writeWasmPrompt();
        return;
      }

      if (ch === '\x0c') {
        term.clear();
        term.write('\x1b[H');
        rewriteWasmLine();
        return;
      }

      if (ch === '\x15') {
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

      if (ch >= ' ' || ch === '\t') {
        const before = wasmLineRef.current.slice(0, wasmCursorRef.current);
        const after = wasmLineRef.current.slice(wasmCursorRef.current);
        wasmLineRef.current = before + ch + after;
        wasmCursorRef.current += 1;
        term.write(ch);
        if (after.length > 0) {
          rewriteWasmLine();
        }
      }
    },
    [xtermRef, rewriteWasmLine, writeWasmPrompt],
  );

  // ── WASM shell lifecycle ──────────────────────────────────────────

  useEffect(() => {
    console.log('[TerminalPane] WASM effect fired, isActive=' + isActive + ' isConnected=' + isConnected);
    if (!isActive) {
      console.log('[TerminalPane] WASM effect: not active, skipping');
      return;
    }

    if (isConnected) {
      console.log('[TerminalPane] WASM effect: isConnected=true, using PTY not WASM');
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
      // Clear WASM reverse-search state to prevent stale mode on reactivation
      wasmReverseSearchActiveRef.current = false;
      wasmReverseSearchQueryRef.current = '';
      wasmReverseSearchResultRef.current = '';
      wasmReverseSearchIdxRef.current = -1;
      return;
    }

    if (wasmActiveRef.current || wasmLoading || wasmInitializedRef.current) {
      return;
    }

    let cancelled = false;

    const activateWasm = async () => {
      const term = xtermRef.current;
      if (!term) {
        // xterm hasn't been mounted yet — increment retry counter to
        // trigger a re-render/retry. Stops after 30 retries (~15s).
        if (!cancelled && retryCount < 30) {
          setTimeout(() => setRetryCount(c => c + 1), 300);
        }
        return;
      }

      if (!wasmShellRef.current && !wasmInitializedRef.current) {
        setWasmLoading(true);
        setWasmError(null);

        try {
          const s = await initWasmShell();
          wasmShellRef.current = s;
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

      const s = wasmShellRef.current;
      if (!s || !term) return;

      term.writeln('');
      term.writeln('\x1b[33m╔══════════════════════════════════════════╗\x1b[0m');
      term.writeln('\x1b[33m║  \x1b[1mSprout WASM Browser Shell\x1b[0m\x1b[33m              ║\x1b[0m');
      term.writeln('\x1b[33m║  \x1b[2mGo compiled to WebAssembly\x1b[0m\x1b[33m             ║\x1b[0m');
      term.writeln('\x1b[33m║  \x1b[2mFiles persist in IndexedDB\x1b[0m\x1b[33m            ║\x1b[0m');
      term.writeln('\x1b[33m╚══════════════════════════════════════════╝\x1b[0m');
      term.writeln('');
      term.writeln('Type \x1b[1mhelp\x1b[0m for available commands.');
      term.writeln('');

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
  }, [isActive, isConnected, wasmLoading, xtermRef, writeWasmPrompt, retryCount]);

  return {
    wasmActive,
    wasmActiveRef,
    wasmLoading,
    wasmError,
    handleWasmInput,
  };
}

export default useWasmTerminalInput;
