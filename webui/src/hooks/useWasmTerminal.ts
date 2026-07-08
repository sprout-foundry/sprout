/**
 * useWasmTerminal — hook for WASM-based terminal command execution.
 *
 * Manages command history, current directory, and command execution
 * through the WASM shell. Built-in commands execute locally; git commands
 * are proxied through the platform's /api/proxy/git/* endpoint.
 */

import { useCallback, useRef, useState } from 'react';
import type { WasmShell, WasmShellResult } from '../services/wasmShell';
import { clientFetch } from '../services/clientSession';

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

  const executeCommand = useCallback(async (input: string): Promise<WasmShellResult | null> => {
    if (!shell) return null;

    const trimmed = input.trim();
    if (!trimmed) {
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

    // ── Git commands: proxy to platform ────────────────────
    if (trimmed.toLowerCase().startsWith('git ')) {
      try {
        result = await proxyGitCommand(trimmed);
      } catch (err) {
        result = { stdout: '', stderr: `Error: ${err instanceof Error ? err.message : String(err)}\n`, exitCode: 1 };
      }
    } else {
      // ── Built-in commands: execute locally ───────────────
      try {
        result = shell.executeCommand(trimmed);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        result = { stdout: '', stderr: `Error: ${msg}\n`, exitCode: 1 };
      }
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

/**
 * Proxy a git command to the platform's /api/git/* endpoint.
 * The CloudAdapter rewrites /api/git/* → /api/proxy/git/*.
 */
async function proxyGitCommand(input: string): Promise<WasmShellResult> {
  const parts = input.trim().split(/\s+/);
  const subcommand = parts[1]?.toLowerCase() || '';

  // Map git subcommand to API endpoint
  let endpoint: string;
  let method = 'POST';
  let body: Record<string, unknown> = {};

  switch (subcommand) {
    case 'status':
      endpoint = '/api/git/status';
      method = 'GET';
      break;
    case 'diff':
      endpoint = '/api/git/diff';
      method = 'GET';
      // Parse --staged flag
      if (input.includes('--staged') || input.includes('--cached')) {
        endpoint += '?staged=true';
      }
      break;
    case 'log':
      endpoint = '/api/git/log';
      method = 'GET';
      break;
    case 'branch':
      endpoint = '/api/git/branch';
      method = 'GET';
      break;
    case 'add':
    case 'stage':
      endpoint = '/api/git/stage';
      body = { files: parts.slice(2) };
      break;
    case 'commit':
      endpoint = '/api/git/commit';
      // Extract message from -m flag
      const msgIdx = parts.indexOf('-m');
      body = { message: msgIdx >= 0 ? parts.slice(msgIdx + 1).join(' ') : '' };
      break;
    case 'push':
      endpoint = '/api/git/push';
      break;
    case 'pull':
      endpoint = '/api/git/pull';
      break;
    case 'clone':
      endpoint = '/api/git/clone';
      body = { url: parts[2] || '' };
      break;
    case 'checkout':
      endpoint = '/api/git/checkout';
      body = { branch: parts[2] || '' };
      break;
    default:
      return {
        stdout: '',
        stderr: `git: '${subcommand}' is not available in browser mode.\nSupported: status, diff, log, branch, add, commit, push, pull, clone, checkout\n`,
        exitCode: 1,
      };
  }

  const resp = await clientFetch(endpoint, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: method === 'POST' ? JSON.stringify(body) : undefined,
  });

  if (!resp.ok) {
    const errData = await resp.json().catch(() => ({ error: `HTTP ${resp.status}` }));
    return { stdout: '', stderr: `git ${subcommand}: ${errData.error || errData.message || `HTTP ${resp.status}`}\n`, exitCode: 1 };
  }

  // Responses vary by endpoint. Try to extract text output.
  const data = await resp.json().catch(() => null);
  if (data) {
    // Some git endpoints return structured data; render it as readable text
    if (data.output) return { stdout: data.output, stderr: '', exitCode: 0 };
    if (data.diff) return { stdout: data.diff, stderr: '', exitCode: 0 };
    if (data.status) return { stdout: JSON.stringify(data.status, null, 2), stderr: '', exitCode: 0 };
    if (data.commits) {
      const lines = data.commits.map((c: any) =>
        `${c.hash?.substring(0, 8) || ''} ${c.message || ''} (${c.author || ''})`
      ).join('\n');
      return { stdout: lines, stderr: '', exitCode: 0 };
    }
    if (data.branches) return { stdout: data.branches.join('\n'), stderr: '', exitCode: 0 };
    return { stdout: JSON.stringify(data, null, 2), stderr: '', exitCode: 0 };
  }

  return { stdout: '', stderr: '', exitCode: 0 };
}
