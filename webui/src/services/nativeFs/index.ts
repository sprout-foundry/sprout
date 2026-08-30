/**
 * Track R (R-2w): manifest-driven native-FS deferral.
 *
 * This leaf module is the runtime half of the Track R FS seam. It ships in
 * BOTH the default build and the `--native-fs` build. It is deliberately NOT
 * under `nativeFsStubs/` and is NOT aliased by `nativeFsStubAliases` (see
 * webui/vite.config.ts — those regexes only rewrite imports of exactly
 * `services/{fileAccess|repoVfsBridge|opfsReplica|wasmShell}`), so it is
 * importable from both the `--native-fs` stub and the default-build
 * Sidebar, and lands in both bundles.
 *
 * In the default build the compile-time constant `NATIVE_FS_ENABLED` is
 * `false`, so every gate here short-circuits into a dead branch and the
 * module is inert — default behavior stays byte-identical.
 *
 * Exports:
 *   - a narrow structural type + detector for the `window.SproutStudio`
 *     bridge subset the webui uses (getCapabilities / readWorkspaceFile /
 *     writeWorkspaceFile / listWorkspace),
 *   - a PURE gate-decision function (`resolveNativeFsGate`) plus a cached
 *     async resolver (`nativeFsGate`),
 *   - PURE path normalization (webui path → workspace-relative),
 *   - a PURE bridge-result → Response mapping (read/write) and a
 *     PURE error-code → HTTP-status table.
 *
 * Leaf: no new dependencies. The only import is the compile-time flag
 * constant from the `nativeFsStubs/` stand-in (a real module present in
 * every build; `nativeFsFlag` is itself NOT one of the aliased stub names).
 */

import { NATIVE_FS_ENABLED } from '../nativeFsStubs/nativeFsFlag';

// ── Narrow structural type for the SproutStudio FS bridge ───────────────────
//
// The shell-injected `window.SproutStudio` exposes a large surface; the webui
// only needs these four for the FS deferral. We model them narrowly and detect
// the bridge STRUCTURALLY — we never add a global `window.SproutStudio`
// declaration (which could clash with the shell-injected script's own types).

/** The `bridge.capabilities` op response (carries both gate signals). */
export interface SproutStudioCapabilities {
  schemaVersion: number;
  capabilities: Record<string, boolean>;
  excluded: Array<{
    portion: string;
    flag: string;
    replacedBy: string;
    hardExclusion: boolean;
    status: string;
    notes: string;
  }>;
  manifestPresent: boolean;
  servable: boolean;
}

/** One entry in a `listWorkspace` result. */
export interface WorkspaceFileEntry {
  path: string;
  size: number;
  isDir: boolean;
}

/**
 * The narrow `window.SproutStudio` subset the webui uses for FS deferral.
 * The workspace helpers never reject — validation failures and timeouts
 * resolve `{ ok: false, error: "<code>" }` (see bridge-protocol.md §10.1).
 */
export type SproutStudioFsBridge = {
  getCapabilities(): Promise<SproutStudioCapabilities>;
  readWorkspaceFile(
    path: string,
  ): Promise<
    | { ok: true; path: string; content?: string; contentBase64?: string }
    | { ok: false; error: string }
  >;
  writeWorkspaceFile(
    path: string,
    payload: string | { content?: string; contentBase64?: string },
  ): Promise<{ ok: true; path: string } | { ok: false; error: string }>;
  listWorkspace(maxDepth?: number): Promise<
    | { ok: true; files: WorkspaceFileEntry[] }
    | { ok: false; error: string }
  >;
};

// ── Structural detector ───────────────────────────────────────────────────────

/**
 * Type guard: is `obj` a usable SproutStudio FS bridge? Checks that the four
 * methods are functions. Pure and synchronous; safe to call with `null`,
 * `undefined`, or a plain object.
 */
export function hasSproutStudioFsBridge(obj: unknown): obj is SproutStudioFsBridge {
  if (!obj || typeof obj !== 'object') return false;
  const c = obj as Record<string, unknown>;
  return (
    typeof c.getCapabilities === 'function' &&
    typeof c.readWorkspaceFile === 'function' &&
    typeof c.writeWorkspaceFile === 'function' &&
    typeof c.listWorkspace === 'function'
  );
}

/**
 * Detect the shell-injected bridge on `window.SproutStudio`. Returns `null`
 * when there is no usable bridge (no `window`, no `SproutStudio`, or missing
 * methods). Never throws.
 */
export function detectSproutStudio(): SproutStudioFsBridge | null {
  if (typeof window === 'undefined') return null;
  const candidate = (window as unknown as { SproutStudio?: unknown }).SproutStudio;
  return hasSproutStudioFsBridge(candidate) ? (candidate as SproutStudioFsBridge) : null;
}

// ── Gate decision (pure) + cached resolver ────────────────────────────────────

export interface NativeFsGateDecision {
  /** True only when the webui defers workspace FS ops to the shell. */
  active: boolean;
  /** Human-readable reason for the decision (for logging/tests). */
  reason: string;
}

/**
 * PURE gate decision. Deferral is active IFF, in precedence order:
 *   1. `nativeFsEnabled` is true (the compile-time `NATIVE_FS_ENABLED`), AND
 *   2. `bridge` is a usable SproutStudio FS bridge, AND
 *   3. `capabilitiesResponse.capabilities.fs === true`, AND
 *   4. `capabilitiesResponse.excluded[]` contains `{ portion: 'fs',
 *      status: 'ratified' }`.
 *
 * Any failing step returns `{ active: false, reason: <step> }`. Malformed
 * capabilities (non-object / missing) are a gate-fail, never a throw.
 */
export function resolveNativeFsGate(
  nativeFsEnabled: boolean,
  bridge: unknown,
  capabilitiesResponse: unknown,
): NativeFsGateDecision {
  if (!nativeFsEnabled) {
    return { active: false, reason: 'native-fs-disabled' };
  }
  if (!hasSproutStudioFsBridge(bridge)) {
    return { active: false, reason: 'no-bridge' };
  }
  const caps = capabilitiesResponse as
    | SproutStudioCapabilities
    | null
    | undefined;
  if (!caps || typeof caps !== 'object') {
    return { active: false, reason: 'malformed-capabilities' };
  }
  if (caps.capabilities?.fs !== true) {
    return { active: false, reason: 'fs-not-declared' };
  }
  const ratified =
    Array.isArray(caps.excluded) &&
    caps.excluded.some(
      (e) => e && e.portion === 'fs' && e.status === 'ratified',
    );
  if (!ratified) {
    return { active: false, reason: 'fs-not-ratified' };
  }
  return { active: true, reason: 'active' };
}

let gatePromise: Promise<NativeFsGateDecision> | null = null;

/**
 * Cached async resolver: calls `bridge.getCapabilities()` ONCE for the app's
 * lifetime and returns the decision. Never re-fetches per operation and never
 * throws (all failures resolve to a `{ active: false, reason }` decision).
 *
 * In the default build (`NATIVE_FS_ENABLED === false`) this short-circuits
 * BEFORE ever touching `window.SproutStudio` — a dead branch.
 */
export function nativeFsGate(): Promise<NativeFsGateDecision> {
  if (gatePromise) return gatePromise;
  gatePromise = (async () => {
    // Compile-time short-circuit: the default build never reaches here.
    if (!NATIVE_FS_ENABLED) {
      return { active: false, reason: 'native-fs-disabled' } as NativeFsGateDecision;
    }
    try {
      const bridge = detectSproutStudio();
      if (!bridge) {
        return { active: false, reason: 'no-bridge' };
      }
      let caps: unknown;
      try {
        caps = await bridge.getCapabilities();
      } catch {
        // getCapabilities rejected → gate-fail (throw as today), never crash.
        return { active: false, reason: 'getCapabilities-rejected' };
      }
      return resolveNativeFsGate(NATIVE_FS_ENABLED, bridge, caps);
    } catch {
      return { active: false, reason: 'unexpected-error' };
    }
  })();
  return gatePromise;
}

// For tests / diagnostics: reset the cached promise (not part of the app path).
export function __resetNativeFsGateForTests(): void {
  gatePromise = null;
}

// ── Path normalization (pure) ─────────────────────────────────────────────────

/**
 * Normalize a webui path to a workspace-RELATIVE path for the bridge.
 *
 *   - strips a leading `./` (repeatedly) and a leading `/`,
 *   - converts backslashes to `/` (mirrors the bridge's own normalization),
 *   - REJECTS any `..` segment with a clear error (before ever hitting the
 *     bridge — the shell also rejects these, but we fail fast client-side),
 *   - REJECTS the empty path.
 *
 * Pure and synchronous. Throws an `Error` on an invalid path; returns the
 * normalized string otherwise.
 */
export function normalizeWorkspacePath(input: string): string {
  if (typeof input !== 'string' || input.length === 0) {
    throw new Error('normalizeWorkspacePath: path must be a non-empty string');
  }
  let p = input.replace(/\\/g, '/');
  while (p.startsWith('./')) p = p.slice(2);
  if (p.startsWith('/')) p = p.slice(1);
  if (p.length === 0) {
    throw new Error(`normalizeWorkspacePath: '${input}' normalizes to an empty path`);
  }
  for (const segment of p.split('/')) {
    if (segment === '..') {
      throw new Error(
        `normalizeWorkspacePath: '${input}' contains a '..' segment ` +
          '(paths must be workspace-relative, no parent traversal)',
      );
    }
  }
  return p;
}

// ── Bridge-result → Response mapping ──────────────────────────────────────────

/**
 * Error-code → HTTP-status table for the files-channel workspace ops
 * (bridge-protocol.md §10.1). Exported as a pure constant + a pure lookup
 * so it is unit-testable without jsdom. `ioFailed` and any unknown code map
 * to 500.
 *
 *   - notFound        → 404  (target or a parent does not exist)
 *   - invalidParams   → 400  (missing/empty path, bad payload)
 *   - notInWorkspace  → 400  (path fails the workspace safety rules)
 *   - isDirectory     → 400  (the op targeted a directory)
 *   - userCancelled   → 409  (the pending native op was cancelled by the user;
 *                              not a hard error, but we must still return a
 *                              non-2xx so callers see it failed)
 *   - workspaceNotSet → 503  (no workspace picked/restored)
 *   - ioFailed / unknown → 500
 */
export const WORKSPACE_ERROR_STATUS: Readonly<Record<string, number>> = {
  notFound: 404,
  invalidParams: 400,
  notInWorkspace: 400,
  isDirectory: 400,
  userCancelled: 409,
  workspaceNotSet: 503,
  ioFailed: 500,
};

/** Pure lookup: error code → HTTP status. Unknown/`ioFailed` → 500. */
export function workspaceErrorStatus(error: string | undefined): number {
  if (error !== undefined && error !== '') {
    const mapped = WORKSPACE_ERROR_STATUS[error];
    if (mapped !== undefined) return mapped;
  }
  return 500;
}

function base64ToBytes(b64: string): ArrayBuffer {
  if (typeof atob === 'function') {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i += 1) bytes[i] = bin.charCodeAt(i);
    return bytes.buffer;
  }
  // Node fallback (vitest / node env) — `atob` is a browser global. Copy into
  // a plain ArrayBuffer so the result is a valid BlobPart; `Buffer.from`
  // returns a view over a larger pool.
  const g = globalThis as {
    Buffer?: { from(s: string, enc: 'base64'): Uint8Array };
  };
  if (g.Buffer) {
    const buf = g.Buffer.from(b64, 'base64');
    const out = new ArrayBuffer(buf.length);
    new Uint8Array(out).set(buf);
    return out;
  }
  return new ArrayBuffer(0);
}

function errorJsonResponse(error: string, status: number): Response {
  return new Response(JSON.stringify({ ok: false, error }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Map a `readWorkspaceFile` result to a synthesized `Response`.
 *   - `{ ok: false, error }`            → mapped status + JSON error body
 *   - `{ ok, contentBase64 }`          → 200, application/octet-stream bytes
 *   - `{ ok, content }` (utf-8 text)   → 200, text/plain; charset=utf-8
 * Consumers rely on the standard `Response` interface (`.ok`, `.text()`,
 * `.blob()`, `.status`, `.statusText`).
 */
export function readWorkspaceResponse(result: unknown): Response {
  const r = result as
    | { ok: true; path: string; content?: string; contentBase64?: string }
    | { ok: false; error: string };
  if (r && (r as { ok?: boolean }).ok === false) {
    const err = (r as { ok: false; error: string }).error;
    return errorJsonResponse(err, workspaceErrorStatus(err));
  }
  const success = r as { ok: true; path: string; content?: string; contentBase64?: string };
  if (typeof success.contentBase64 === 'string' && success.contentBase64.length > 0) {
    let bytes: ArrayBuffer;
    try {
      // `atob` throws a DOMException on malformed input (the Node
      // `Buffer.from` fallback tolerates it), so guard the decode
      // explicitly: a bad `contentBase64` in an ok:true result degrades to
      // the documented ioFailed (500) Response instead of throwing out to
      // consumers.
      bytes = base64ToBytes(success.contentBase64);
    } catch {
      return errorJsonResponse('ioFailed', 500);
    }
    const body = new Blob([bytes], { type: 'application/octet-stream' });
    return new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'application/octet-stream' },
    });
  }
  const text = typeof success.content === 'string' ? success.content : '';
  return new Response(text, {
    status: 200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
}

// ── listWorkspace → file-tree mapping (pure) ─────────────────────────────────
//
// The `files` channel's `listWorkspace(maxDepth)` lists the workspace ROOT
// only (no sub-directory arg) and returns `{path, size, isDir}` per entry
// (no `modified`, no `gitStatus`). To list the direct children of an
// arbitrary requested directory, we (a) request a depth deep enough to reach
// one level below it and (b) filter the flat result down to that level.

/** Number of path segments (e.g. `src/main.go` → 2, `.`/`''` → 0). */
function segmentCount(p: string): number {
  return p.split('/').filter((s) => s.length > 0).length;
}

/**
 * The `maxDepth` to pass to `listWorkspace` so that the direct children of
 * `requestedPath` are visible. "maxDepth = 1 relative to the requested path":
 * root (`.`/`''`) → 1; a depth-N directory → N + 1. Pure.
 */
export function workspaceListDepth(requestedPath: string): number {
  // Root forms ('.', '', '/') list the direct children of the workspace root.
  // (normalizeWorkspacePath rejects '' and '/', so handle them explicitly.)
  if (requestedPath === '.' || requestedPath === '' || requestedPath === '/') {
    return 1;
  }
  return segmentCount(normalizeWorkspacePath(requestedPath)) + 1;
}

export interface NativeFileInfo {
  name: string;
  path: string;
  size: number;
  modified: number;
  isDir: boolean;
  ext: string;
  gitStatus?: 'modified' | 'untracked' | 'ignored';
}

/** Derive a file's `ext` (`.` + suffix) from its bare name; '' for dirs/no dot. */
function deriveExt(name: string, isDir: boolean): string {
  if (isDir) return '';
  return name.includes('.') ? `.${name.split('.').pop() || ''}` : '';
}

/**
 * The SAME sort the Sidebar's `clientFetch` path uses: directories first,
 * then non-ignored before ignored, then `name.localeCompare`. Pure.
 */
export function sortNativeFileInfo(items: NativeFileInfo[]): NativeFileInfo[] {
  return [...items].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    if ((a.gitStatus === 'ignored') !== (b.gitStatus === 'ignored')) {
      return a.gitStatus === 'ignored' ? 1 : -1;
    }
    return a.name.localeCompare(b.name);
  });
}

/**
 * Pure: turn a `listWorkspace` result into the sorted file-tree listing for
 * the direct children of `requestedPath`.
 *
 *   - validates the result (must be `{ ok: true, files }`); an error /
 *     malformed result yields `[]` (the tree simply shows nothing, matching
 *     how a failed fetch degrades),
 *   - filters the flat root listing down to the direct children of
 *     `requestedPath` (depth + prefix),
 *   - maps each entry to the file-tree shape. `listWorkspace` carries no
 *     `modified`/`gitStatus`, so `modified` is `0` and `gitStatus` is
 *     `undefined`; `name` is the last path segment and `ext` is derived from
 *     it exactly as the `clientFetch` path does.
 *
 * The result is returned already sorted (dirs first, then by name).
 */
export function mapWorkspaceListing(
  result: unknown,
  requestedPath: string,
): NativeFileInfo[] {
  const r = result as { ok?: boolean; files?: WorkspaceFileEntry[]; error?: string };
  if (!r || r.ok !== true || !Array.isArray(r.files)) {
    return [];
  }
  const normalized =
    requestedPath === '.' || requestedPath === '' || normalizeWorkspacePath(requestedPath) === '.'
      ? ''
      : normalizeWorkspacePath(requestedPath);
  const baseDepth = normalized === '' ? 0 : segmentCount(normalized);
  const childDepth = baseDepth + 1;

  const children = r.files.filter((entry) => {
    if (!entry || typeof entry.path !== 'string' || entry.path === '') return false;
    const normalizedEntry = entry.path.replace(/\\/g, '/').replace(/^\.\//, '');
    if (segmentCount(normalizedEntry) !== childDepth) return false;
    if (normalized !== '') {
      return normalizedEntry.startsWith(`${normalized}/`);
    }
    return true;
  });

  const items = children.map((entry) => {
    const normalizedEntry = entry.path.replace(/\\/g, '/').replace(/^\.\//, '');
    const isDir = Boolean(entry.isDir);
    const name = normalizedEntry.split('/').filter(Boolean).pop() || normalizedEntry;
    return {
      name,
      path: normalizedEntry,
      size: typeof entry.size === 'number' ? entry.size : 0,
      modified: 0,
      isDir,
      ext: deriveExt(name, isDir),
      gitStatus: undefined,
    };
  });

  return sortNativeFileInfo(items);
}

/** Map a `writeWorkspaceFile` result to a synthesized `Response`. */
export function writeWorkspaceResponse(result: unknown): Response {
  const r = result as
    | { ok: true; path: string }
    | { ok: false; error: string };
  if (r && (r as { ok?: boolean }).ok === false) {
    const err = (r as { ok: false; error: string }).error;
    return errorJsonResponse(err, workspaceErrorStatus(err));
  }
  const success = r as { ok: true; path: string };
  return new Response(JSON.stringify({ ok: true, path: success.path }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

// ── High-level accessors (used by the nativeFsStubs/fileAccess stub) ─────────

/**
 * Read a workspace file through the bridge and synthesize a `Response`.
 * `path` must already be normalized (call `normalizeWorkspacePath` first).
 * The bridge never rejects; a transport failure is surfaced as a 500.
 */
export async function nativeReadWorkspaceFile(path: string): Promise<Response> {
  const bridge = detectSproutStudio();
  if (!bridge) {
    throw new Error(
      'nativeReadWorkspaceFile: no SproutStudio bridge available (Track R --native-fs)',
    );
  }
  let result: unknown;
  try {
    result = await bridge.readWorkspaceFile(path);
  } catch {
    // The bridge helpers are "never-reject"; a hard throw means the transport
    // itself is broken. Surface as ioFailed (500) rather than crashing.
    return errorJsonResponse('ioFailed', 500);
  }
  return readWorkspaceResponse(result);
}

/**
 * Write a workspace file through the bridge and synthesize a `Response`.
 * `path` must already be normalized; `content` is a utf-8 text string.
 */
export async function nativeWriteWorkspaceFile(
  path: string,
  content: string,
): Promise<Response> {
  const bridge = detectSproutStudio();
  if (!bridge) {
    throw new Error(
      'nativeWriteWorkspaceFile: no SproutStudio bridge available (Track R --native-fs)',
    );
  }
  let result: unknown;
  try {
    result = await bridge.writeWorkspaceFile(path, content);
  } catch {
    return errorJsonResponse('ioFailed', 500);
  }
  return writeWorkspaceResponse(result);
}