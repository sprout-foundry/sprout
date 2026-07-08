/**
 * useWasmTerminal — hook for WASM-based terminal command execution.
 *
 * Manages command history, current directory, and command execution
 * through the WASM shell. Used by WasmTerminal component in cloud mode.
 */

import { useCallback, useRef, useState } from 'react';
import type { WasmShell, WasmShellResult } from '../services/wasmShell';

export interface CommandEntry {
  input: string;
  output: string;
  exitCode: number;
  cwd: string;
}

export interface WasmTerminalState {
  entries: CommandEntry[];
  cwd: string;
  history: string[];
}

export function useWasmTerminal(shell: WasmShell | null) {
  const [entries, setEntries] = useState<CommandEntry[]>([]);
  const [cwd, setCwd] = useState<string>(() => {
    if (shell) {
      try { return shell.getCwd(); } catch { /* ignore */ }
    }
    return '/home/user';
  });
  const historyRef = useRef<string[]>([]);
  const historyIndexRef = useRef(-1);

  const executeCommand = useCallback((input: string): WasmShellResult | null => {
    if (!shell) return null;

    const trimmed = input.trim();
    if (!trimmed) {
      // Empty input — just show new prompt
      setEntries(prev => [...prev, { input: '', output: '', exitCode: 0, cwd }]);
      return { stdout: '', stderr: '', exitCode: 0 };
    }

    // Built-in handlers for commands that need JS-side state
    if (trimmed === 'clear' || trimmed === 'cls') {
      setEntries([]);
      return { stdout: '', stderr: '', exitCode: 0 };
    }

    // Add to history
    if (trimmed !== historyRef.current[historyRef.current.length - 1]) {
      historyRef.current.push(trimmed);
    }
    historyIndexRef.current = -1;

    let result: WasmShellResult;
    try {
      result = shell.executeCommand(trimmed);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      result = { stdout: '', stderr: `Error: ${msg}\n`, exitCode: 1 };
    }

    // Track directory changes for prompt display
    try {
      if (trimmed.startsWith('cd ') || trimmed === 'cd') {
        const newCwd = shell.getCwd();
        if (newCwd && newCwd !== cwd) {
          setCwd(newCwd);
        }
      }
    } catch { /* ignore */ }

    const output = [
      result.stdout && result.stdout,
      result.stderr && `\x1b[31m${result.stderr}\x1b[0m`,
    ].filter(Boolean).join('') || '';

    setEntries(prev => [...prev, { input: trimmed, output, exitCode: result.exitCode, cwd }]);

    return result;
  }, [shell, cwd]);

  const navigateHistory = useCallback((direction: 'up' | 'down', currentInput: string): string => {
    const history = historyRef.current;
    if (history.length === 0) return currentInput;

    if (direction === 'up') {
      if (historyIndexRef.current === -1) {
        // Save current input as temp
        historyIndexRef.current = history.length - 1;
      } else if (historyIndexRef.current > 0) {
        historyIndexRef.current--;
      }
      return history[historyIndexRef.current] || currentInput;
    } else {
      if (historyIndexRef.current < history.length - 1) {
        historyIndexRef.current++;
        return history[historyIndexRef.current];
      } else {
        historyIndexRef.current = -1;
        return '';
      }
    }
  }, []);

  const getPrompt = useCallback((): string => {
    // Format: user@wasm:~/path$
    let displayPath = cwd;
    const home = '/home/user';
    if (displayPath.startsWith(home)) {
      displayPath = '~' + displayPath.slice(home.length);
    }
    return `\x1b[1;32muser@wasm\x1b[0m:\x1b[1;34m${displayPath}\x1b[0m$ `;
  }, [cwd]);

  return {
    entries,
    cwd,
    executeCommand,
    navigateHistory,
    getPrompt,
    clear: () => setEntries([]),
  };
}
