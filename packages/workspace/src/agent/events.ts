/**
 * Translation of the agent's raw UI events (the `{type, data}` JSON the WASM
 * module streams from its event bus) into the headless API's typed events.
 */

import type { AgentEvent, MockupEvent } from './types';

export interface RawAgentEvent {
  type: string;
  data?: Record<string, unknown>;
}

/** Context the translation needs: how to read a file and where the workspace is. */
export interface EventMappingContext {
  workspaceRoot: string;
  readFile(path: string): string | null;
}

/**
 * Raw events the API does not surface: the turn's start and end are reported
 * by `send` itself (`turn_start`, `done`), and the remaining ones are UI
 * plumbing (dialog bookkeeping, footers) with no meaning for a host.
 */
const DROPPED = new Set(['query_started', 'query_completed', 'input_required']);

const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined);

/** Workspace-relative form of a path the agent reported. */
export function toWorkspacePath(path: string, root: string): string {
  const normalizedRoot = root.replace(/\/+$/, '') || '/';
  if (normalizedRoot !== '/' && path.startsWith(`${normalizedRoot}/`)) {
    return path.slice(normalizedRoot.length + 1);
  }
  if (normalizedRoot === '/' && path.startsWith('/')) return path.slice(1);
  return path.replace(/^\.\//, '');
}

/** Absolute path in the agent's filesystem for a workspace-relative one. */
export function toAbsolutePath(path: string, root: string): string {
  if (path.startsWith('/')) return path;
  const base = root.replace(/\/+$/, '');
  const rel = path.replace(/^\.\//, '');
  return rel === '' || rel === '.' ? base || '/' : `${base}/${rel}`;
}

/** Primary-argument keys, in the order a one-line summary prefers them. */
const SUMMARY_KEYS = ['path', 'file_path', 'command', 'query', 'pattern', 'url', 'question', 'title', 'name'];

/** A one-line summary of a tool call's arguments. */
export function summarizeArgs(args: Record<string, unknown> | undefined, raw: string): string {
  if (args) {
    for (const key of SUMMARY_KEYS) {
      const v = args[key];
      if (typeof v === 'string' && v.trim()) return oneLine(v);
    }
    const first = Object.values(args).find((v) => typeof v === 'string' && v.trim());
    if (typeof first === 'string') return oneLine(first);
    return '';
  }
  return oneLine(raw);
}

function oneLine(s: string, max = 120): string {
  const line = s.replace(/\s+/g, ' ').trim();
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}

function parseArgs(raw: unknown): Record<string, unknown> | undefined {
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) return raw as Record<string, unknown>;
  if (typeof raw !== 'string' || raw === '') return undefined;
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : undefined;
  } catch {
    // Arguments cut for size are not valid JSON; the summary falls back to the text.
    return undefined;
  }
}

function mapMockupEvent(
  phase: MockupEvent['phase'],
  data: Record<string, unknown>,
  ctx: EventMappingContext,
): MockupEvent {
  const event: MockupEvent = { type: 'mockup', phase, data };
  const path = str(data.path) ?? str(data.file_path);
  if (path) event.path = toWorkspacePath(toAbsolutePath(path, ctx.workspaceRoot), ctx.workspaceRoot);
  if (phase === 'chunk') event.chunk = str(data.chunk) ?? str(data.html) ?? str(data.content) ?? '';
  if (phase === 'done') {
    const html = str(data.html) ?? str(data.content);
    if (html !== undefined) event.html = html;
  }
  if (phase === 'error') event.error = str(data.error) ?? str(data.message) ?? 'mockup failed';
  return event;
}

/** Translate one raw event; null for events the API does not surface. */
export function mapAgentEvent(raw: RawAgentEvent, ctx: EventMappingContext): AgentEvent | null {
  const data = raw.data ?? {};
  if (DROPPED.has(raw.type)) return null;

  switch (raw.type) {
    case 'stream_chunk': {
      const text = str(data.chunk) ?? '';
      if (text === '') return null;
      return data.content_type === 'reasoning' ? { type: 'reasoning', text } : { type: 'text', text };
    }
    case 'tool_start': {
      const rawArgs = data.arguments;
      const args = data.arguments_truncated ? undefined : parseArgs(rawArgs);
      return {
        type: 'tool_start',
        toolCallId: str(data.tool_call_id) ?? '',
        tool: str(data.tool_name) ?? '',
        displayName: str(data.display_name) || undefined,
        args,
        argsSummary: summarizeArgs(args ?? parseArgs(rawArgs), typeof rawArgs === 'string' ? rawArgs : ''),
      };
    }
    case 'tool_end':
      return {
        type: 'tool_end',
        toolCallId: str(data.tool_call_id) ?? '',
        tool: str(data.tool_name) ?? '',
        status: data.status === 'failed' ? 'failed' : 'completed',
        result: str(data.result),
        error: str(data.error) || undefined,
        durationMs: typeof data.duration_ms === 'number' ? data.duration_ms : undefined,
      };
    case 'ask_user_request': {
      const id = str(data.request_id) ?? '';
      if (data.status === 'cancelled') return { type: 'ask_user_cancelled', id };
      const options = Array.isArray(data.options)
        ? (data.options as Array<Record<string, unknown>>).map((o) => {
            const label = str(o.label) ?? '';
            const description = str(o.description);
            return { label, value: str(o.value) || label, ...(description ? { description } : {}) };
          })
        : [];
      return {
        type: 'ask_user',
        id,
        question: str(data.question) ?? '',
        header: str(data.header),
        options,
        multiSelect: data.multi_select === true,
        default: str(data.default),
      };
    }
    case 'file_changed': {
      const absolute = str(data.file_path);
      if (!absolute) return null;
      const action = str(data.action) ?? 'write';
      const abs = toAbsolutePath(absolute, ctx.workspaceRoot);
      return {
        type: 'file_written',
        path: toWorkspacePath(abs, ctx.workspaceRoot),
        action,
        content: action === 'deleted' ? null : ctx.readFile(abs),
      };
    }
    case 'mockup_start':
    case 'mockup_chunk':
    case 'mockup_done':
    case 'mockup_error':
      return mapMockupEvent(raw.type.slice('mockup_'.length) as MockupEvent['phase'], data, ctx);
    case 'error':
      return { type: 'error', message: str(data.message) ?? str(data.error) ?? 'agent error', fatal: false };
    default:
      return { type: 'raw', name: raw.type, data };
  }
}

/**
 * Tools that write a workspace file, and where the path comes from: an
 * argument, or a fixed file (the plan tools write `.sprout/plan.json`, plus
 * its rendered `.sprout/plan.md`).
 */
const FILE_WRITING_TOOLS: Record<string, { action: string; paths: (args: Record<string, unknown>) => string[] }> = {
  write_file: { action: 'write', paths: (a) => argPath(a) },
  edit_file: { action: 'edit', paths: (a) => argPath(a) },
  write_structured_file: { action: 'write', paths: (a) => argPath(a) },
  patch_structured_file: { action: 'edit', paths: (a) => argPath(a) },
  write_plan: { action: 'write', paths: () => ['.sprout/plan.json', '.sprout/plan.md'] },
  plan_add_scope: { action: 'edit', paths: () => ['.sprout/plan.json', '.sprout/plan.md'] },
};

function argPath(args: Record<string, unknown>): string[] {
  const p = args.path ?? args.file_path;
  return typeof p === 'string' && p.trim() ? [p.trim()] : [];
}

/**
 * Turns completed file-writing tool calls into `file_written` events. The
 * in-browser agent's file tools report a write only through their tool
 * events, so the write is derived from the call: the path from its
 * arguments, the content read back once the call succeeded. A
 * `file_changed` event for the same file during the call (an agent build
 * that reports writes) supersedes the derived one.
 */
export class FileWriteTracker {
  private pending = new Map<string, { action: string; paths: string[] }>();
  private reported = new Set<string>();

  constructor(private readonly ctx: EventMappingContext) {}

  /** Feed every raw event; returns the file events to emit after the mapped one. */
  observe(raw: RawAgentEvent, mapped: AgentEvent | null): AgentEvent[] {
    if (mapped?.type === 'file_written') {
      this.reported.add(mapped.path);
      return [];
    }
    if (raw.type === 'tool_start') {
      const tool = String(raw.data?.tool_name ?? '');
      const spec = FILE_WRITING_TOOLS[tool];
      const id = String(raw.data?.tool_call_id ?? '');
      if (!spec) return [];
      const args = parseArgs(raw.data?.arguments) ?? partialPathArgs(raw.data?.arguments);
      const paths = spec.paths(args).map((p) => toWorkspacePath(toAbsolutePath(p, this.ctx.workspaceRoot), this.ctx.workspaceRoot));
      if (paths.length) this.pending.set(id, { action: spec.action, paths });
      return [];
    }
    if (raw.type === 'tool_end') {
      const id = String(raw.data?.tool_call_id ?? '');
      const call = this.pending.get(id);
      this.pending.delete(id);
      if (!call || raw.data?.status === 'failed') return [];
      const out: AgentEvent[] = [];
      for (const path of call.paths) {
        if (this.reported.delete(path)) continue;
        const content = this.ctx.readFile(toAbsolutePath(path, this.ctx.workspaceRoot));
        if (content === null) continue;
        out.push({ type: 'file_written', path, action: call.action, content });
      }
      return out;
    }
    return [];
  }
}

/** The path of a call whose arguments were cut for size (a large file write). */
function partialPathArgs(raw: unknown): Record<string, unknown> {
  if (typeof raw !== 'string') return {};
  const match = /"(?:path|file_path)"\s*:\s*"((?:[^"\\]|\\.)*)"/.exec(raw);
  if (!match) return {};
  try {
    return { path: JSON.parse(`"${match[1]}"`) as string };
  } catch {
    return {};
  }
}
