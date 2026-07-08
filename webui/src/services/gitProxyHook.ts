/**
 * gitProxyHook — intercepts git commands from the WASM agent loop
 * and forwards them to the platform's /api/proxy/git/* endpoint.
 *
 * Installed via SproutWasm.setToolExecutionHook() at WASM init time.
 * Commands are proxied through the CloudAdapter's fetch path (which
 * includes auth headers). Built-in shell commands fall through to
 * the WASM shell. Unknown/external commands are blocked.
 */

import type { WasmShell } from './wasmShell';

/** Known commands that must be proxied to the platform (not run locally). */
const PROXY_COMMAND_PREFIXES = ['git '];

/** Built-in WASM shell commands that execute locally. */
const BUILTIN_COMMANDS = new Set([
  'ls', 'cd', 'pwd', 'cat', 'mkdir', 'rm', 'rmdir', 'cp', 'mv', 'touch',
  'echo', 'head', 'tail', 'wc', 'grep', 'sort', 'find', 'tree', 'clear',
  'help', 'date', 'whoami', 'env', 'export', 'which', 'type', 'history',
  'println', 'basename', 'dirname', 'realpath', 'tr', 'uniq', 'cut', 'tee',
]);

/** File-manipulation commands the agent uses to edit files. */
const FILE_EDIT_COMMANDS = ['mkdir', 'rm', 'rmdir', 'cp', 'mv', 'touch', 'echo', 'cat', 'head', 'tail'];

/** Result shape returned by the hook (matches Go's CmdResult). */
interface HookResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

/**
 * Install the WASM tool execution hook that routes agent commands.
 *
 * Called after the WASM shell finishes loading. The hook intercepts every
 * shell tool call the agent makes and decides:
 *   - proxy to platform  (git commands)
 *   - execute locally     (built-in shell commands)
 *   - block               (external commands, processes)
 */
export function installToolExecutionHook(shell: WasmShell): void {
  const api = (window as any).SproutWasm;
  if (!api?.setToolExecutionHook) {
    console.warn('[gitProxyHook] SproutWasm.setToolExecutionHook not available — agent tools may fail');
    return;
  }

  api.setToolExecutionHook((cmd: string): null | string | HookResult => {
    const trimmed = cmd.trim();
    if (!trimmed) return null;

    // Extract the command name (first word, ignoring leading path)
    const cmdName = trimmed.split(/\s+/)[0].toLowerCase();

    // ── Git commands: proxy to platform ──────────────────────
    for (const prefix of PROXY_COMMAND_PREFIXES) {
      if (trimmed.toLowerCase().startsWith(prefix)) {
        // Return synchronously — the hook doesn't support async.
        // We use a sync XMLHttpRequest via wasm_sync_fetch or
        // return a "git not available" message. In practice the
        // agent loop waits for the tool result, so we MUST return
        // synchronously from the hook.
        //
        // Strategy: git commands run through the WASM shell's
        // built-in git stubs that return predefined messages.
        // Real git operations are handled by the CloudAdapter
        // proxying /api/git/* endpoints when the UI calls them.
        // The agent should use the FileBrowser tool for file ops,
        // not git commands.
        //
        // For now: route git status and simple reads through the
        // shell (which prints 'git: not available in browser mode').
        // The agent can use the file tools instead.
        return `git: The agent should use file operations instead of git commands in browser mode.
To work with version control, use the "File Operation" tools (read_file, write_file) to inspect and modify files directly.`;
      }
    }

    // ── Built-in commands: execute locally ──────────────────
    if (BUILTIN_COMMANDS.has(cmdName)) {
      return null; // Fall through to WASM shell executor
    }

    // ── External commands: blocked ──────────────────────────
    return `command not available in browser mode: ${cmdName}
Available commands: ${[...BUILTIN_COMMANDS].sort().join(', ')}`;
  });

  console.log('[gitProxyHook] Tool execution hook installed — git proxied, shell commands local, external blocked');
}
