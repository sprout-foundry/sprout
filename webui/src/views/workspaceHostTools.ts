/**
 * Host tools for a mounted workspace's in-browser agent: tools the host
 * implements in JavaScript (a board, an account) and offers to the agent
 * while the workspace is mounted. Same shape as the agent package's
 * `HostTool`, so one value serves both.
 *
 * Only the in-browser agent takes them: a daemon backend runs its agent in
 * another process, out of the page's reach.
 */

import { getAdapter } from '../services/apiAdapter';
import type { WasmShell } from '../services/wasmShell';

/** A tool the host implements and offers to the workspace's agent. */
export interface SpacesHostTool {
  /** Function name the model calls: letters, digits, `_`, `-`; at most 64. */
  name: string;
  /** What the tool does, for the model. */
  description: string;
  /** JSON schema of the arguments (an object schema). Defaults to no arguments. */
  parameters?: Record<string, unknown>;
  /** Ceiling for one call; the call fails after it. Default 5 minutes. */
  timeoutMs?: number;
  /** Runs the call; a non-string result is sent as JSON, a throw fails it. */
  handler: (args: Record<string, unknown>, context: { chatId: string; toolName: string }) => unknown | Promise<unknown>;
}

type ShellSource = { agentShell(): Promise<WasmShell> };

function parseArgs(argsJSON: string): Record<string, unknown> {
  if (!argsJSON) return {};
  const parsed = JSON.parse(argsJSON) as unknown;
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
}

function resultText(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value === undefined || value === null) return '';
  return JSON.stringify(value);
}

/**
 * Register `tools` with the installed in-browser agent's runtime. Resolves to
 * the function that withdraws them. A runtime without an in-browser adapter,
 * or one that predates host tools, takes none (with a warning).
 */
export async function registerSpacesHostTools(tools: SpacesHostTool[]): Promise<() => void> {
  const adapter = getAdapter() as Partial<ShellSource> | null;
  if (!tools.length || typeof adapter?.agentShell !== 'function') return () => {};
  const shell = await adapter.agentShell();
  const registered: string[] = [];
  for (const tool of tools) {
    const spec = JSON.stringify({
      name: tool.name,
      description: tool.description,
      parameters: tool.parameters ?? { type: 'object', properties: {} },
      timeoutMs: tool.timeoutMs ?? 0,
    });
    const res = shell.registerHostTool(spec, async (argsJSON, context) =>
      resultText(await tool.handler(parseArgs(argsJSON), { chatId: context?.chatId ?? '', toolName: tool.name })),
    );
    if (res.ok) registered.push(tool.name);
    else console.warn(`[SproutWorkspace] host tool ${tool.name} not offered: ${res.error ?? 'refused'}`);
  }
  return () => registered.forEach((name) => shell.unregisterHostTool(name));
}
