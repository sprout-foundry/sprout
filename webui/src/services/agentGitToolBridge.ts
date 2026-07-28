/**
 * Agent Git Tool Bridge — intercepts `gittool:` shell commands from the WASM
 * agent and dispatches them to the async executor functions in agentGitTools.ts.
 *
 * ### Architecture
 *
 * The WASM agent (Go → WASM) has a synchronous tool-execution hook
 * (`SproutWasm.setToolExecutionHook`) that fires before every shell command.
 * The hook MUST return synchronously — Go's js.Value cannot await Promises.
 * Since the git tools perform async I/O (IndexedDB), the sync hook cannot
 * directly return their results.
 *
 * This bridge provides two integration paths:
 *
 * 1. **Sync hook** (`installGitToolBridge`) — intercepts `gittool:` commands
 *    in the Go-side tool execution hook. Returns a synchronous message
 *    guiding the caller to the async global API. Non-`gittool:` commands
 *    pass through to the normal wasmshell executor.
 *
 * 2. **Async global** (`registerGitToolGlobal`) — registers
 *    `globalThis.__sproutGitTools` with an `execute(toolName, args)` method
 *    that returns `Promise<string>`. Future Go-side async hook support or
 *    direct JS invocation can use this path for actual tool execution.
 *
 * ### Command format
 *
 *     gittool:<toolName> <jsonArgs>
 *
 * Examples:
 *     gittool:git_status {"repo":"owner/name"}
 *     gittool:git_read_file {"repo":"owner/name","filepath":"src/main.ts"}
 */

import { AGENT_GIT_TOOLS, AGENT_GIT_TOOL_NAMES } from './agentGitTools';

// ── Parsing ──────────────────────────────────────────────────────────

export const GITTOOL_COMMAND_PREFIX = 'gittool:';

/**
 * Parse a `gittool:` command into its tool name and JSON arguments.
 *
 * Returns null if the command doesn't start with the `gittool:` prefix.
 * The tool name is extracted between the prefix and the first whitespace.
 * Remaining text after the tool name is parsed as JSON; on failure the
 * args default to an empty object.
 *
 * @example
 *   parseGitToolCommand('gittool:git_status {"repo":"a/b"}')
 *   → { toolName: 'git_status', args: { repo: 'a/b' } }
 *
 * @example
 *   parseGitToolCommand('ls -la')
 *   → null
 */
export function parseGitToolCommand(command: string): { toolName: string; args: Record<string, unknown> } | null {
  if (!command.startsWith(GITTOOL_COMMAND_PREFIX)) {
    return null;
  }

  const afterPrefix = command.slice(GITTOOL_COMMAND_PREFIX.length);
  const spaceIdx = afterPrefix.search(/\s/);

  const toolName = spaceIdx === -1 ? afterPrefix.trim() : afterPrefix.slice(0, spaceIdx).trim();
  if (!toolName) return null;

  const argsStr = spaceIdx === -1 ? '' : afterPrefix.slice(spaceIdx + 1).trim();

  let args: Record<string, unknown> = {};
  if (argsStr) {
    try {
      const parsed = JSON.parse(argsStr);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        args = parsed;
      }
    } catch {
      // Invalid JSON — leave args as empty object; the tool's execute()
      // will validate required parameters and return an error message.
    }
  }

  return { toolName, args };
}

// ── Async dispatch ───────────────────────────────────────────────────

/**
 * Dispatch a git tool call by name, returning its string result.
 *
 * Looks up the tool in the AGENT_GIT_TOOLS registry and calls its
 * `execute(args)` method. Throws on unknown tool names; otherwise
 * returns the tool's string result.
 *
 * @throws Error if the tool name is not registered.
 */
export async function dispatchGitTool(toolName: string, args: Record<string, unknown>): Promise<string> {
  const tool = AGENT_GIT_TOOLS.find((t) => t.name === toolName);
  if (!tool) {
    throw new Error(`Unknown git tool: ${toolName}. Available: ${Array.from(AGENT_GIT_TOOL_NAMES).join(', ')}`);
  }
  return tool.execute(args);
}

// ── Global registration ──────────────────────────────────────────────

/**
 * The shape of `globalThis.__sproutGitTools` once `registerGitToolGlobal()` is called.
 */
export interface SproutGitToolsGlobal {
  /** Execute a git tool by name. Throws on unknown tool names. */
  execute(toolName: string, args: Record<string, unknown>): Promise<string>;
  /** Set of all registered tool names. */
  readonly names: ReadonlySet<string>;
  /** Return all registered tool names as a sorted array. */
  list(): string[];
}

declare global {
  interface Window {
    __sproutGitTools: SproutGitToolsGlobal;
  }
  /* eslint-disable no-var -- `var` is required in `declare global` blocks: only `var`
     augments `typeof globalThis`. `let`/`const` declare block-scoped globals that do not
     appear on `globalThis`, which would break `globalThis.__sproutGitTools`. */
  var __sproutGitTools: SproutGitToolsGlobal;
  /* eslint-enable no-var */
}

/**
 * Register `globalThis.__sproutGitTools` so any JS context (including
 * the WASM agent via a future async hook or JS-eval command) can
 * invoke git tools asynchronously.
 *
 * Safe to call multiple times — overwrites the previous registration
 * with the same singleton object.
 */
export function registerGitToolGlobal(): void {
  const gitTools: SproutGitToolsGlobal = {
    execute: (toolName: string, args: Record<string, unknown>): Promise<string> => {
      return dispatchGitTool(toolName, args);
    },
    names: AGENT_GIT_TOOL_NAMES,
    list(): string[] {
      return Array.from(AGENT_GIT_TOOL_NAMES).sort();
    },
  };

  globalThis.__sproutGitTools = gitTools;

  // Also set on window for environments where SproutWasm looks there.
  if (typeof window !== 'undefined') {
    window.__sproutGitTools = gitTools;
  }
}

// ── Sync hook bridge ─────────────────────────────────────────────────

/**
 * Minimum shape of the WASM API needed to install the tool execution hook.
 * Matches the signature exposed by `cmd/wasm/tool_exec_funcs.go`.
 */
export interface ToolHookWasmApi {
  setToolExecutionHook?: (fn: (cmd: string) => unknown) => void;
}

/**
 * Install the synchronous tool execution hook on the WASM API.
 *
 * The Go-side executor intercepts `gittool:` commands before this hook
 * fires (via callGitToolJS in shell_executor.go), so in production this
 * hook primarily passes through non-gittool commands. It still handles
 * gittool: commands for direct JS invocation or testing, returning a
 * synchronous error message for known tools and rejecting unknown ones.
 *
 * Call `registerGitToolGlobal()` before or after this function to ensure
 * the async API is available. The typical initialization sequence is:
 *
 * ```ts
 * registerGitToolGlobal();
 * installGitToolBridge(wasmApi);
 * ```
 */
export function installGitToolBridge(wasmApi: ToolHookWasmApi): void {
  if (!wasmApi.setToolExecutionHook) {
    console.warn('[agentGitToolBridge] setToolExecutionHook not available on WASM API');
    return;
  }

  const syncHook = (command: string): unknown => {
    const parsed = parseGitToolCommand(command);
    if (!parsed) {
      // Not a gittool command — allow normal wasmshell execution.
      return null;
    }

    // Validate that the tool name is registered. Unknown tool names
    // get a simple rejection; known tools get the async guidance message.
    if (!AGENT_GIT_TOOL_NAMES.has(parsed.toolName)) {
      return {
        stdout: '',
        stderr: `Unknown git tool: ${parsed.toolName}`,
        exitCode: 1,
      };
    }

    // Known git tool — in production the Go side handles this before
    // the hook fires, but for direct JS invocation or testing we return
    // a guidance message since this hook cannot await the async result.
    const argsJson = JSON.stringify(parsed.args);
    return {
      stdout: '',
      stderr:
        `Git tool '${parsed.toolName}' requires async execution. ` +
        `Use: globalThis.__sproutGitTools.execute('${parsed.toolName}', ${argsJson})`,
      exitCode: 1,
    };
  };

  wasmApi.setToolExecutionHook(syncHook);
}
