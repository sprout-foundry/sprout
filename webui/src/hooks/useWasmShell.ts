import { useCallback, useEffect, useRef, useState } from 'react';
import {
  initWasmShell,
  resetWasmShell,
  type WasmShell,
  type WasmShellResult,
} from '../services/wasmShell';

interface UseWasmShellOptions {
  /** Override the virtual home directory (default: /home/user) */
  home?: string;
  /** Auto-initialize on mount (default: true) */
  autoInit?: boolean;
}

interface UseWasmShellReturn {
  /** Whether the WASM module is currently loading. */
  isLoading: boolean;
  /** Any error that occurred during initialization or execution. */
  error: string | null;
  /** The current working directory (updates after cd commands). */
  cwd: string;
  /** The loaded shell instance (null while loading). */
  shell: WasmShell | null;
  /** Execute a shell command and return the result. */
  executeCommand: (input: string) => WasmShellResult;
  /** Change directory (updates the cwd state). */
  changeDir: (dir: string) => string | null;
  /** Get tab completions for a partial command. */
  autoComplete: (input: string) => string[];
  /** Command history (updated reactively). */
  history: string[];
  /** Re-initialize the WASM module (useful after errors). */
  reinitialize: () => Promise<void>;
}

/**
 * React hook for the ledit WASM shell.
 *
 * Manages the WASM lifecycle and provides a convenient interface for
 * executing commands, changing directories, and getting tab completions.
 *
 * @example
 * ```tsx
 * const { executeCommand, cwd, isLoading, error } = useWasmShell();
 * if (isLoading) return <div>Loading shell...</div>;
 * if (error) return <div>Error: {error}</div>;
 *
 * const result = executeCommand('ls -la');
 * console.log(result.stdout);
 * ```
 */
export function useWasmShell(options: UseWasmShellOptions = {}): UseWasmShellReturn {
  const { home, autoInit = true } = options;

  const [isLoading, setIsLoading] = useState(autoInit);
  const [error, setError] = useState<string | null>(null);
  const [cwd, setCwd] = useState('/home/user');
  const [history, setHistory] = useState<string[]>([]);

  const shellRef = useRef<WasmShell | null>(null);

  const initialize = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const shell = await initWasmShell(home ? { home } : undefined);
      shellRef.current = shell;
      setCwd(shell.getCwd());
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      console.error('[useWasmShell] Init failed:', message);
    } finally {
      setIsLoading(false);
    }
  }, [home]);

  const reinitialize = useCallback(async () => {
    resetWasmShell();
    shellRef.current = null;
    await initialize();
  }, [initialize]);

  useEffect(() => {
    if (autoInit) {
      initialize();
    }
  }, [autoInit, initialize]);

  const executeCommand = useCallback(
    (input: string): WasmShellResult => {
      if (!shellRef.current) {
        return {
          stdout: '',
          stderr: 'WASM shell not initialized yet.\n',
          exitCode: 1,
        };
      }

      try {
        const result = shellRef.current.executeCommand(input);

        // Update cwd if it might have changed (cd command).
        const trimmed = input.trim();
        if (
          trimmed.startsWith('cd ') ||
          trimmed === 'cd' ||
          trimmed.startsWith('cd\t')
        ) {
          setCwd(shellRef.current.getCwd());
        }

        // Update history.
        if (trimmed && !trimmed.startsWith('#')) {
          setHistory((prev) => {
            // Avoid duplicate of last entry.
            if (prev.length > 0 && prev[prev.length - 1] === trimmed) {
              return prev;
            }
            const next = [...prev, trimmed];
            // Cap history at 1000 entries.
            if (next.length > 1000) {
              return next.slice(next.length - 1000);
            }
            return next;
          });
        }

        return result;
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return {
          stdout: '',
          stderr: `Error: ${message}\n`,
          exitCode: 1,
        };
      }
    },
    [],
  );

  const changeDir = useCallback(
    (dir: string): string | null => {
      if (!shellRef.current) {
        return 'WASM shell not initialized yet.';
      }

      try {
        const result = shellRef.current.changeDir(dir);
        if (result.error) {
          return result.error;
        }
        setCwd(result.cwd);
        return null;
      } catch (err) {
        return err instanceof Error ? err.message : String(err);
      }
    },
    [],
  );

  const autoComplete = useCallback((input: string): string[] => {
    if (!shellRef.current) {
      return [];
    }

    try {
      const result = shellRef.current.autoComplete(input);
      return result.completions;
    } catch {
      return [];
    }
  }, []);

  return {
    isLoading,
    error,
    cwd,
    shell: shellRef.current,
    executeCommand,
    changeDir,
    autoComplete,
    history,
    reinitialize,
  };
}

export default useWasmShell;
