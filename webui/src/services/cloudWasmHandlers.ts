/**
 * WASM-local endpoint handlers for CloudAdapter.
 *
 * In cloud mode, file operations AND agent queries are handled client-side
 * by the WASM shell rather than being proxied to a backend.
 */

import { describeAgentError, notifyCreditsBlocked } from './agentErrorMessage';
import { historyForChat, isChatRunning, recordTurn, setChatRunning } from './cloudChatSessions';
import { NATIVE_CHAT_ENABLED } from './nativeChatStubs/nativeChatFlag';
import { platformProviderConfig, reportedManagedContextWindow } from './platformProvider';
import {
  getVfsManifestSnapshot as snapshot,
  isRuntimePath,
  joinVfsPath,
  normalizeVfsPath as normalizePath,
  RUNTIME_DIRS,
  trackFileWrite as trackWrite,
  workspaceRootOf,
} from './vfsFiles';
import type { WasmDirEntry, WasmShell } from './wasmShell';
import { binaryMimeType } from './cloudWasmBinary';
import { workspaceCwdContextLine } from './workspaceCwd';

// Global event dispatcher — set by the webui's event system so WASM
// agent events flow into the same React state as WebSocket events.
let agentEventDispatcher: ((event: unknown) => void) | null = null;

export function setAgentEventDispatcher(fn: ((event: unknown) => void) | null): void {
  agentEventDispatcher = fn;
}

/**
 * The daemon announces file writes, creates and deletes as file_changed
 * events; the git panel refreshes on them. Delivered after the request
 * returns, as a server event would be.
 */
function announceFileChange(filePath: string, action: 'write' | 'created' | 'deleted'): void {
  const dispatchEvent = agentEventDispatcher;
  if (!dispatchEvent) return;
  // The user's own change (a save, a tree operation), not the agent's.
  const event = {
    type: 'file_changed',
    data: { file_path: filePath, action, ts: new Date().toISOString(), source: 'user' },
  };
  queueMicrotask(() => dispatchEvent(event));
}

/**
 * Shell-escape an argument for use in a command string.
 * Wraps in single quotes and escapes any embedded single quotes.
 */
function shellEscapeArg(arg: string): string {
  return `'${arg.replace(/'/g, "'\\''")}'`;
}

/**
 * Handle wasm-local endpoints by routing them to the WASM shell.
 * These endpoints would normally go to the Go backend, but in cloud mode
 * the WASM shell owns the virtual filesystem.
 */
export function handleWasmLocal(
  shell: WasmShell,
  urlPath: string,
  method: string,
  fullUrl: string,
  bodyStr?: string,
): Response {
  try {
    switch (urlPath) {
      // ── File listing ──────────────────────────────────────────
      case '/api/files':
        return handleWasmFileList(shell, fullUrl);
      case '/api/browse':
      case '/api/workspace/browse':
        return handleWasmBrowse(shell, fullUrl);
      case '/api/file-index':
        return handleWasmFileIndex(shell, fullUrl);

      // ── File read/write ──────────────────────────────────────
      case '/api/file':
        return handleWasmFile(shell, method, fullUrl, bodyStr);

      // ── File CRUD ────────────────────────────────────────────
      case '/api/create':
        return handleWasmCreate(shell, bodyStr);
      case '/api/delete':
        return handleWasmDelete(shell, bodyStr);
      case '/api/rename':
        return handleWasmRename(shell, bodyStr);

      // ── Search ──────────────────────────────────────────────
      case '/api/search':
        return handleWasmSearch(shell, fullUrl);
      case '/api/search/replace':
        return handleWasmSearchReplace(shell, bodyStr);

      // ── File metadata ──────────────────────────────────────
      case '/api/file/check-modified':
        return handleWasmCheckModified(shell, bodyStr);
      case '/api/file/consent':
        // No consent flow needed in cloud/WASM mode
        return jsonOk({ token: 'wasm-local', path: '/', operation: 'read', expires_at: '' });
      case '/api/files/prettier-config':
        return jsonOk({ prettier: null });

      // ── Design health (§6b) ──
      // The scanners are pure Go, so the WASM shell serves the same
      // /api/design/status payload the daemon does — the hosted editor's
      // health strip reads the same truth the agent tools read.
      case '/api/design/status': {
        const api = (globalThis as { SproutWasm?: { designStatus?: (root?: string) => string } }).SproutWasm;
        if (!api || typeof api.designStatus !== 'function') {
          return jsonOk({ exists: false });
        }
        const raw = api.designStatus(workspaceRootOf(shell));
        try {
          return jsonOk(JSON.parse(raw));
        } catch {
          return jsonOk({ exists: false });
        }
      }

      // ── Agent query (runs full agent loop in WASM) ──────────
      case '/api/query':
        // Compile-time short-circuit (R-4): in a --native-chat dist the shell
        // provides chat natively (the agent-turn transport is hard-excluded),
        // so the wasm-local query handler is never used. Dead branch in the
        // default build (flag off → today's exact behavior, byte-identical).
        if (NATIVE_CHAT_ENABLED) {
          return jsonError('Chat provided by the native shell', 501);
        }
        return handleWasmAgentQuery(shell, bodyStr);

      // ── Agent stop (interrupts in-browser agent loop) ───────
      case '/api/query/stop': {
        // Compile-time short-circuit (R-4): no in-browser agent loop to stop.
        if (NATIVE_CHAT_ENABLED) {
          return jsonError('Chat provided by the native shell', 501);
        }
        const stopChatId = new URL(fullUrl, 'http://local').searchParams.get('chat_id') || chatIdFromBody(bodyStr);
        stopRequested.add(stopChatId ?? '');
        shell.stopAgent(stopChatId);
        return jsonOk({ status: 'ok', stopped: true });
      }

      // ── Agent steer (injects into persistent agent) ─────────
      case '/api/query/steer':
        // Compile-time short-circuit (R-4): no in-browser agent to steer.
        if (NATIVE_CHAT_ENABLED) {
          return jsonError('Chat provided by the native shell', 501);
        }
        return handleWasmAgentSteer(shell, bodyStr);

      // ── Ask user response (delivers answer to WASM agent) ────
      case '/api/ask-user/response':
        return handleWasmAskUserResponse(shell, bodyStr);

      // ── Terminal stubs ──────────────────────────────────────
      case '/api/terminal/sessions':
        return jsonOk({ active_count: 0, count: 0 });
      case '/api/terminal/shells':
        return jsonOk({ shells: [{ name: 'wasm', path: '/bin/wasm', default: true }] });
      case '/api/terminal/history':
        if (method === 'POST') {
          // Accept but ignore — WASM terminal manages its own history
          return jsonOk({ success: true });
        }
        return jsonOk({ entries: [] });

      default:
        // Unrecognized wasm-local endpoint — return empty OK
        console.warn('[CloudAdapter] Unhandled wasm-local endpoint:', urlPath);
        return jsonOk({});
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error(`[CloudAdapter] WASM handler error for ${urlPath}:`, err);
    return jsonError(message, 500);
  }
}

// ── File manifest (fallback for broken listDir on old WASM binaries) ────────

/**
 * The deployed WASM binary (v0.15.4) has a broken os.ReadDir due to an
 * O_DIRECTORY syscall bug on js/wasm. writeFile/readFile work fine, but
 * listDir returns an error. This manifest tracks every file path written
 * to the VFS so handleWasmFileList/handleWasmBrowse can fall back to it.
 *
 * When the WASM binary is updated to include the O_DIRECTORY fix, listDir
 * will work and the manifest becomes a no-op supplement.
 *
 * The manifest itself and the pure enumeration helpers live in
 * vfsFiles.ts (React-free, shared with the standalone pages); the
 * re-exports below keep the existing import paths stable.
 */
export {
  getVfsManifestSnapshot,
  listAllVfsFiles,
  normalizeVfsPath as normalizePath,
  trackFileWrite,
  untrackFileWrite,
  workspaceRootOf,
} from './vfsFiles';

function writePlatformProviderConfig(shell: WasmShell, apiOrigin: string): void {
  try {
    shell.writeFile(
      '/home/user/.config/sprout/providers/platform.json',
      JSON.stringify(platformProviderConfig(apiOrigin, reportedManagedContextWindow())),
    );
  } catch {
    // Keep the previously written config.
  }
}

/** Chats (by id, '' for the default) whose run the user asked to stop. */
const stopRequested = new Set<string>();

/**
 * Hidden from the workspace's listings: runtime state, and a directory that
 * only holds it (/home).
 */
function hiddenFromWorkspace(shell: WasmShell, absPath: string, root: string): boolean {
  if (isRuntimePath(absPath, root)) return true;
  const prefix = absPath === '/' ? '/' : `${absPath}/`;
  if (!RUNTIME_DIRS.some((dir) => dir.startsWith(prefix) && !(root === dir || root.startsWith(`${dir}/`)))) {
    return false;
  }
  const listing = shell.listDir(absPath);
  return !listing.error && listing.entries.every((e) => hiddenFromWorkspace(shell, joinVfsPath(absPath, e.name), root));
}

// ── Individual wasm-local route handlers ─────────────────────────

/** Path relative to the shell's CWD (the browser workspace root). */
function vfsRelative(absPath: string, rootDir: string): string {
  if (rootDir === '/') return absPath.replace(/^\/+/, '');
  const prefix = `${rootDir}/`;
  if (absPath.startsWith(prefix)) return absPath.slice(prefix.length);
  if (absPath === rootDir) return '.';
  return absPath;
}

/**
 * Single-level listing in the daemon /api/files shape: every immediate
 * child is one entry, directories flagged is_dir (FileTree lazily
 * fetches a directory's children when it is expanded). The daemon
 * excludes .git from listings.
 */
function singleLevelFileEntries(
  shell: WasmShell,
  entries: WasmDirEntry[],
  dir: string,
  rootDir: string,
): Array<Record<string, unknown>> {
  return entries
    .filter((e) => !(e.type === 'dir' && e.name === '.git'))
    .filter((e) => !hiddenFromWorkspace(shell, joinVfsPath(dir, e.name), rootDir))
    .map((e) => {
      const absPath = joinVfsPath(dir, e.name);
      return {
        name: e.name,
        path: absPath,
        relative: vfsRelative(absPath, rootDir),
        is_dir: e.type === 'dir',
        size: e.size ?? 0,
        mod_time: 0,
      };
    });
}

/**
 * Group manifest paths into single-level children of dir: files directly
 * under dir plus implicit directory entries for any nested file. When
 * nothing lives under dir, the CWD may not match where importRepo wrote
 * the files — in that case group the whole manifest under '/' instead.
 */
function groupManifestChildren(dir: string, rootDir?: string): Array<{ name: string; path: string; isDir: boolean }> {
  const underDir = (p: string) => (dir === '/' ? p.startsWith('/') : p.startsWith(`${dir}/`) || p === dir);
  const workspacePaths = Array.from(snapshot()).filter((p) => rootDir === undefined || !isRuntimePath(p, rootDir));
  let base = dir;
  let paths = workspacePaths.filter(underDir);
  if (paths.length === 0 && workspacePaths.length > 0) {
    base = '/';
    paths = workspacePaths;
  }
  if (paths.length === 0) return [];

  const groups = new Map<string, { isDir: boolean; filePath: string }>();
  for (const p of paths) {
    const inner = base === '/' ? p.replace(/^\/+/, '') : p.slice(base.length + 1);
    const [top, ...rest] = inner.split('/');
    if (!top || top === '.git') continue;
    const g = groups.get(top) ?? { isDir: false, filePath: p };
    if (rest.length > 0) g.isDir = true;
    groups.set(top, g);
  }

  const out: Array<{ name: string; path: string; isDir: boolean }> = [];
  for (const [name, g] of groups) {
    const path = g.isDir ? joinVfsPath(base, name) : g.filePath;
    out.push({ name, path, isDir: g.isDir });
  }
  out.sort((a, b) => (a.isDir !== b.isDir ? (a.isDir ? -1 : 1) : a.name.localeCompare(b.name)));
  return out;
}

/**
 * GET /api/files — Returns the immediate children of a directory, in the
 * daemon's single-level shape ({name, path, relative, is_dir, size,
 * mod_time}). The webui FileTree renders folders from this shape and
 * fetches a directory's children lazily when expanded. A recursive flat
 * file list (the previous behavior) made the tree render every file at
 * the top level — folders vanished.
 */
function handleWasmFileList(shell: WasmShell, fullUrl?: string): Response {
  const root = workspaceRootOf(shell);
  const cwd = fullUrl ? getQueryParam(fullUrl, 'path') || root : root;
  const dir = normalizePath(cwd);
  const rootDir = normalizePath(root);

  // Try listDir first; fall back to the manifest.
  const dirResult = shell.listDir(dir);
  if (!dirResult.error && dirResult.entries && dirResult.entries.length > 0) {
    const files = singleLevelFileEntries(shell, dirResult.entries, dir, rootDir);
    return jsonOk({ message: 'success', files });
  }

  // listDir failed or the directory is empty in the VFS — derive a
  // single-level listing from the tracked-file manifest.
  const children = groupManifestChildren(dir, rootDir);
  const files = children.map((c) => ({
    name: c.name,
    path: c.path,
    relative: vfsRelative(c.path, rootDir),
    is_dir: c.isDir,
    size: 0,
    mod_time: 0,
  }));
  return jsonOk({ message: 'success', files });
}

/**
 * Recursively flatten WASM directory entries into a flat file list.
 * Each entry includes name (extracted from path) for the FileTree component.
 */
function flattenEntries(shell: WasmShell, dir: string): Array<{ path: string; modified: boolean; name: string }> {
  const result: Array<{ path: string; modified: boolean; name: string }> = [];
  const listResult = shell.listDir(dir);
  if (listResult.error) return result;

  for (const entry of listResult.entries) {
    const fullPath = dir === '/' ? `/${entry.name}` : `${dir}/${entry.name}`;
    if (entry.type === 'dir') {
      if (entry.name === '.git') continue;
      result.push(...flattenEntries(shell, fullPath));
    } else {
      result.push({ path: fullPath, modified: false, name: entry.name });
    }
  }
  return result;
}

// The fallback crawl's caps; mirrored from pkg/filediscovery's defaults.
const WASM_INDEX_MAX_FILES = 12000;
const WASM_INDEX_MAX_DEPTH = 8;
const WASM_SKIP_DIRS = new Set([
  '.git',
  'node_modules',
  '.next',
  '.nuxt',
  '.svelte-kit',
  'dist',
  'build',
  'out',
  '__pycache__',
  '.venv',
  'venv',
  'vendor',
  'target',
  '.turbo',
  '.cache',
  '.parcel-cache',
  'coverage',
  '.gradle',
  '.idea',
  '.vs',
  'Library',
  'Applications',
  'Documents',
  'Desktop',
  'Downloads',
  'Pictures',
  'Music',
  'Movies',
  'Public',
  'Videos',
  'Templates',
  'AppData',
  'OneDrive',
  'Contacts',
  'Favorites',
  'Links',
  'Saved Games',
  'Searches',
  'Dropbox',
  'Google Drive',
  'iCloud Drive',
]);

/**
 * GET /api/file-index — the workspace's whole quick-open index in ONE
 * response. Uses the WASM binary's bulk walkFiles export (one JS→WASM
 * crossing) instead of a per-directory listDir crawl; falls back to
 * serially walking via /api/browse semantics on older binaries that don't
 * expose it.
 * Expected: { files: [{name, path, type}], truncated, file_count, dir_count }
 */
function handleWasmFileIndex(shell: WasmShell, _fullUrl: string): Response {
  const root = workspaceRootOf(shell) || '/';
  if (typeof shell.walkFiles === 'function') {
    const json = shell.walkFiles('/');
    try {
      const parsed = JSON.parse(json) as {
        files?: Array<{ name: string; path: string; type?: string }>;
        truncated?: boolean;
        file_count?: number;
        dir_count?: number;
        error?: string;
      };
      if (parsed.error) {
        return jsonError(parsed.error, 500);
      }
      return jsonOk({
        message: 'success',
        workspace: root,
        files: (parsed.files ?? []).map((f) => ({
          name: f.name,
          path: f.path,
          type: f.type ?? 'file',
        })),
        truncated: parsed.truncated ?? false,
        file_count: parsed.file_count ?? parsed.files?.length ?? 0,
        dir_count: parsed.dir_count ?? 0,
      });
    } catch {
      // Fall through to the crawl below on malformed output.
    }
  }

  // Older binaries: crawl one listDir per directory (the pre-index shape).
  const files: Array<{ name: string; path: string; type: string }> = [];
  let truncated = false;
  const visit = (dir: string, depth: number): void => {
    if (files.length >= WASM_INDEX_MAX_FILES || depth > WASM_INDEX_MAX_DEPTH) {
      truncated = true;
      return;
    }
    let entries: Array<{ name: string; path: string; type: string }>;
    try {
      const result = shell.listDir(dir);
      if (result.error) return;
      entries = (result.entries ?? []).map((e) => ({
        name: e.name,
        path: dir === '/' ? `/${e.name}` : `${dir}/${e.name}`,
        type: e.type,
      }));
    } catch {
      return;
    }
    for (const entry of entries) {
      if (entry.type === 'dir') {
        if (WASM_SKIP_DIRS.has(entry.name)) continue;
        visit(entry.path, depth + 1);
        if (truncated) return;
        continue;
      }
      if (files.length >= WASM_INDEX_MAX_FILES) {
        truncated = true;
        return;
      }
      files.push({ name: entry.name, path: toWorkspaceRelative(entry.path, root), type: 'file' });
    }
  };
  visit('/', 0);

  return jsonOk({
    message: 'success',
    workspace: root,
    files,
    truncated,
    file_count: files.length,
    dir_count: 0,
  });
}

/** Convert an absolute VFS path to workspace-relative slash path (or '' root). */
function toWorkspaceRelative(path: string, root: string): string {
  const normRoot = root.endsWith('/') && root !== '/' ? root.slice(0, -1) : root;
  if (root === '/' || path === normRoot) return path.replace(/^\/+/, '');
  return path.startsWith(normRoot + '/') ? path.slice(normRoot.length + 1) : path;
}

/**
 * GET /api/browse?path=... — Browse a directory.
 * Expected: { files: [{name, path, type, size, modified}] }
 */
function handleWasmBrowse(shell: WasmShell, fullUrl: string): Response {
  const path = getQueryParam(fullUrl, 'path') || '/';
  const safePath = sanitizePath(path);
  const result = shell.listDir(safePath);
  if (!result.error && result.entries && result.entries.length > 0) {
    const files = result.entries.map((entry) => ({
      name: entry.name,
      path: safePath === '/' ? `/${entry.name}` : `${safePath}/${entry.name}`,
      type: entry.type === 'dir' ? 'directory' : 'file',
      size: entry.size,
      modified: 0,
    }));
    return jsonOk({ files });
  }

  // listDir failed — fall back to the manifest, deriving a single-level
  // listing (files + implicit directories) so folders aren't lost.
  const children = groupManifestChildren(safePath);
  const files = children.map((c) => ({
    name: c.name,
    path: c.path,
    type: c.isDir ? 'directory' : 'file',
    size: 0,
    modified: 0,
  }));
  return jsonOk({ files });
}

/**
 * GET /api/file?path=...  — Read file content.
 * POST /api/file?path=... — Write file content.
 */
function handleWasmFile(shell: WasmShell, method: string, fullUrl: string, bodyStr?: string): Response {
  const path = getQueryParam(fullUrl, 'path');
  if (!path) {
    return jsonError('Missing path parameter', 400);
  }
  const safePath = sanitizePath(path);

  if (method === 'GET') {
    const binaryMime = binaryMimeType(safePath);
    if (binaryMime) {
      const bytes = shell.readFileBytes(safePath);
      if (bytes.error || !bytes.bytes) {
        return jsonError(bytes.error ?? 'unreadable file', 404);
      }
      return new Response(bytes.bytes as BodyInit, { status: 200, headers: { 'Content-Type': binaryMime } });
    }
    const result = shell.readFile(safePath);
    if (result.error) {
      return jsonError(result.error, 404);
    }
    // Return raw content with text content-type. The ETag is the content
    // hash the conditional write (§7a) compares against: WASM files carry
    // no mtimes, so the "changed since you opened it" guard keys on bytes.
    return new Response(result.content, {
      status: 200,
      headers: {
        'Content-Type': 'text/plain; charset=utf-8',
        ETag: `"${wasmContentHash(result.content)}"`,
      },
    });
  }

  // POST — write
  if (!bodyStr) {
    return jsonError('Missing request body', 400);
  }
  let content: string;
  let baseHash: string | undefined;
  try {
    const parsed = JSON.parse(bodyStr);
    content = typeof parsed.content === 'string' ? parsed.content : bodyStr;
    if (typeof parsed.baseHash === 'string') baseHash = parsed.baseHash;
  } catch {
    // best-effort: non-JSON write body is stored as raw content.
    content = bodyStr;
  }
  // §7a conditional write: a baseHash that no longer matches the current
  // bytes means the file changed after the caller read it — refuse with the
  // daemon's 409 conflict shape instead of silently clobbering.
  if (baseHash) {
    const current = shell.readFile(safePath);
    if (!current.error && wasmContentHash(current.content) !== baseHash) {
      return new Response(
        JSON.stringify({ error: 'conflict', path: safePath, currentHash: wasmContentHash(current.content) }),
        { status: 409, headers: { 'Content-Type': 'application/json' } },
      );
    }
  }
  const err = shell.writeFile(safePath, content);
  if (err) {
    return jsonError(err, 500);
  }
  trackWrite(safePath);
  announceFileChange(safePath, 'write');
  // Same success contract as the daemon's write endpoint: the buffer manager
  // clears the unsaved flag only on this shape.
  return jsonOk({ message: 'File saved successfully', success: true });
}

/**
 * POST /api/create — Create a file or directory.
 * Body: { path, directory?: boolean } or { directory, path }
 */
function handleWasmCreate(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const path = parsed?.path as string | undefined;
  const isDir = !!(parsed?.directory || parsed?.is_directory);
  if (!path) return jsonError('Missing path', 400);

  const safePath = sanitizePath(path);

  if (isDir) {
    const result = shell.executeCommand(`mkdir -p ${shellEscapeArg(safePath)}`);
    if (result.exitCode !== 0) {
      return jsonError(result.stderr || 'mkdir failed', 500);
    }
  } else {
    // Create an empty file
    const err = shell.writeFile(safePath, '');
    if (err) {
      return jsonError(err, 500);
    }
    trackWrite(safePath);
  }
  announceFileChange(safePath, 'created');
  return jsonOk({ message: 'ok', path: safePath });
}

/**
 * DELETE /api/delete — Delete a file.
 * Body: { path }
 */
function handleWasmDelete(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const path = parsed?.path as string | undefined;
  if (!path) return jsonError('Missing path', 400);

  const safePath = sanitizePath(path);
  const err = shell.deleteFile(safePath);
  if (err) {
    // If file delete fails, try rm via command (handles directories)
    const result = shell.executeCommand(`rm -rf ${shellEscapeArg(safePath)}`);
    if (result.exitCode !== 0) {
      return jsonError(result.stderr || err, 500);
    }
  }
  announceFileChange(safePath, 'deleted');
  return jsonOk({ message: 'ok', path: safePath });
}

/**
 * POST /api/rename — Rename a file or directory.
 * Body: { old_path, new_path }
 */
function handleWasmRename(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const oldPath = parsed?.old_path as string | undefined;
  const newPath = parsed?.new_path as string | undefined;
  if (!oldPath || !newPath) return jsonError('Missing old_path or new_path', 400);

  const safeOld = sanitizePath(oldPath);
  const safeNew = sanitizePath(newPath);
  const result = shell.executeCommand(`mv ${shellEscapeArg(safeOld)} ${shellEscapeArg(safeNew)}`);
  if (result.exitCode !== 0) {
    return jsonError(result.stderr || 'rename failed', 500);
  }
  announceFileChange(safeOld, 'deleted');
  announceFileChange(safeNew, 'created');
  return jsonOk({ message: 'ok', old_path: safeOld, new_path: safeNew });
}

/**
 * GET /api/search?query=...&case_sensitive=...&regex=...&include=...
 * Uses WASM shell grep command.
 */
function handleWasmSearch(shell: WasmShell, fullUrl: string): Response {
  const query = getQueryParam(fullUrl, 'query') || '';
  const caseSensitive = getQueryParam(fullUrl, 'case_sensitive') === 'true';
  const regex = getQueryParam(fullUrl, 'regex') === 'true';
  const include = getQueryParam(fullUrl, 'include') || '';

  if (!query) return jsonOk({ results: [], total_matches: 0, total_files: 0, truncated: false, query: '' });

  const cwd = workspaceRootOf(shell);
  // Build grep command
  let grepCmd = 'grep';
  if (!caseSensitive) grepCmd += ' -i';
  if (regex) grepCmd += ' -E';
  grepCmd += ` -rn --include=${shellEscapeArg(include || '*')} ${shellEscapeArg(query)} ${shellEscapeArg(cwd)}`;

  const result = shell.executeCommand(grepCmd);
  // grep returns exit code 1 when no matches — that's not an error
  if (result.exitCode !== 0 && result.exitCode !== 1) {
    return jsonError(result.stderr || 'search failed', 500);
  }

  // Parse grep output into structured results, with workspace-relative
  // paths like the daemon's search returns.
  const prefix = cwd.endsWith('/') ? cwd : `${cwd}/`;
  const results = parseGrepOutput(result.stdout).map((r) => ({
    ...r,
    file: r.file.startsWith(prefix) ? r.file.slice(prefix.length) : r.file,
  }));
  const totalMatches = results.reduce((sum, r) => sum + r.match_count, 0);
  return jsonOk({
    results,
    total_matches: totalMatches,
    total_files: results.length,
    truncated: false,
    query,
  });
}

/**
 * POST /api/search/replace — Search and replace across files.
 * Body: { search, replace, files, case_sensitive?, whole_word?, regex?, preview }
 */
function handleWasmSearchReplace(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const search = parsed?.search as string | undefined;
  const replace = parsed?.replace as string | undefined;
  const files = parsed?.files as string[] | undefined;
  const preview = !!parsed?.preview;

  if (!search || !replace || !files?.length) {
    return jsonError('Missing search, replace, or files', 400);
  }

  const changes: Array<{
    file: string;
    matches: Array<{
      line_number: number;
      old_line: string;
      new_line: string;
      column_start: number;
      column_end: number;
    }>;
    changed_lines: number;
  }> = [];

  for (const filePath of files) {
    const safePath = sanitizePath(filePath);
    const readResult = shell.readFile(safePath);
    if (readResult.error) continue;

    const content = readResult.content;
    const lines = content.split('\n');
    const matches: Array<{
      line_number: number;
      old_line: string;
      new_line: string;
      column_start: number;
      column_end: number;
    }> = [];
    let changedLines = 0;

    for (let i = 0; i < lines.length; i++) {
      const idx = lines[i].indexOf(search);
      if (idx !== -1) {
        const oldLine = lines[i];
        const newLine = lines[i].replace(search, replace);
        if (oldLine !== newLine) {
          matches.push({
            line_number: i + 1,
            old_line: oldLine,
            new_line: newLine,
            column_start: idx,
            column_end: idx + search.length,
          });
          if (!preview) {
            lines[i] = newLine;
          }
          changedLines++;
        }
      }
    }

    if (matches.length > 0) {
      if (!preview) {
        const err = shell.writeFile(safePath, lines.join('\n'));
        if (err) {
          console.warn(`[CloudAdapter] searchReplace: failed to write ${safePath}:`, err);
        }
      }
      changes.push({ file: safePath, matches, changed_lines: changedLines });
    }
  }

  const totalChanges = changes.reduce((sum, c) => sum + c.changed_lines, 0);
  return jsonOk({ changes, total_changes: totalChanges, preview });
}

/**
 * The §7a revision guard's hash for WASM-held content: SHA-256 hex (the same
 * spelling the daemon's baseHash compares). Sync over the bytes so both the
 * read ETag and the write guard derive identically.
 */
function wasmContentHash(content: string): string {
  // A pure-JS SHA-256 (the shell has no async context here; 32-bit chunks
  // over UTF-8 bytes). Input sizes are file-scale, well under the
  // performance cliff.
  const bytes = new TextEncoder().encode(content);
  const k = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98,
    0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
    0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8,
    0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819,
    0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
    0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
    0xc67178f2,
  ];
  let h0 = 0x6a09e667,
    h1 = 0xbb67ae85,
    h2 = 0x3c6ef372,
    h3 = 0xa54ff53a;
  let h4 = 0x510e527f,
    h5 = 0x9b05688c,
    h6 = 0x1f83d9ab,
    h7 = 0x5be0cd19;
  const withOne = bytes.length + 1;
  const total = Math.ceil((withOne + 8) / 64) * 64;
  const words = new Uint32Array(total / 4);
  for (let i = 0; i < bytes.length; i++) {
    words[i >> 2] |= bytes[i] << ((3 - (i & 3)) * 8);
  }
  words[bytes.length >> 2] |= 0x80 << ((3 - (bytes.length & 3)) * 8);
  const bitLen = bytes.length * 8;
  words[words.length - 1] = bitLen >>> 0;
  words[words.length - 2] = Math.floor(bitLen / 0x100000000);
  const rr = (x: number, n: number) => (x >>> n) | (x << (32 - n));
  for (let block = 0; block < words.length; block += 16) {
    const w = new Uint32Array(64);
    for (let t = 0; t < 16; t++) w[t] = words[block + t];
    for (let t = 16; t < 64; t++) {
      const s0 = rr(w[t - 15], 7) ^ rr(w[t - 15], 18) ^ (w[t - 15] >>> 3);
      const s1 = rr(w[t - 2], 17) ^ rr(w[t - 2], 19) ^ (w[t - 2] >>> 10);
      w[t] = (w[t - 16] + s0 + w[t - 7] + s1) >>> 0;
    }
    let a = h0,
      b = h1,
      c = h2,
      d = h3,
      e = h4,
      f = h5,
      g = h6,
      h = h7;
    for (let t = 0; t < 64; t++) {
      const S1 = rr(e, 6) ^ rr(e, 11) ^ rr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const temp1 = (h + S1 + ch + k[t] + w[t]) >>> 0;
      const S0 = rr(a, 2) ^ rr(a, 13) ^ rr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (S0 + maj) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + temp1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) >>> 0;
    }
    h0 = (h0 + a) >>> 0;
    h1 = (h1 + b) >>> 0;
    h2 = (h2 + c) >>> 0;
    h3 = (h3 + d) >>> 0;
    h4 = (h4 + e) >>> 0;
    h5 = (h5 + f) >>> 0;
    h6 = (h6 + g) >>> 0;
    h7 = (h7 + h) >>> 0;
  }
  return [h0, h1, h2, h3, h4, h5, h6, h7].map((x) => x.toString(16).padStart(8, '0')).join('');
}

/**
 * POST /api/file/check-modified — Check if files have been modified.
 * In WASM mode, files are only modified through the WASM shell, so always
 * return empty (no external modifications detected).
 */
function handleWasmCheckModified(_shell: WasmShell, bodyStr?: string): Response {
  // Parse the request to acknowledge it, but always return no modifications
  // since WASM files can only change through the shell itself.
  void bodyStr;
  return jsonOk({ modified: [] });
}

// ── WASM helper utilities ────────────────────────────────────────

/**
 * Parse grep -rn output into structured search results.
 * Format: "filename:linenum:matched line text"
 */
function parseGrepOutput(output: string): Array<{
  file: string;
  matches: Array<{
    line_number: number;
    line: string;
    column_start: number;
    column_end: number;
    context_before: string[];
    context_after: string[];
  }>;
  match_count: number;
}> {
  const fileMap = new Map<
    string,
    Array<{
      line_number: number;
      line: string;
      column_start: number;
      column_end: number;
      context_before: string[];
      context_after: string[];
    }>
  >();

  for (const line of output.split('\n')) {
    if (!line) continue;
    // Parse "path:linenum:content" or "path:linenum:content"
    const firstColon = line.indexOf(':');
    if (firstColon === -1) continue;
    const secondColon = line.indexOf(':', firstColon + 1);
    if (secondColon === -1) continue;

    const file = line.substring(0, firstColon);
    const lineNum = parseInt(line.substring(firstColon + 1, secondColon), 10);
    const content = line.substring(secondColon + 1);

    if (isNaN(lineNum)) continue;

    let matches = fileMap.get(file);
    if (!matches) {
      matches = [];
      fileMap.set(file, matches);
    }
    matches.push({
      line_number: lineNum,
      line: content,
      column_start: 0,
      column_end: content.length,
      context_before: [],
      context_after: [],
    });
  }

  return Array.from(fileMap.entries()).map(([file, matches]) => ({
    file,
    matches,
    match_count: matches.length,
  }));
}

/**
 * Normalize and validate a file path for WASM operations.
 * Rejects path traversal attempts.
 */
export function sanitizePath(path: string): string {
  // Remove null bytes
  let clean = path.replace(/\0/g, '');
  // Normalize slashes
  clean = clean.replace(/\\/g, '/');
  // Collapse multiple slashes
  clean = clean.replace(/\/+/g, '/');
  // Remove trailing slash (unless root)
  if (clean.length > 1 && clean.endsWith('/')) {
    clean = clean.slice(0, -1);
  }
  // Reject path traversal
  const parts = clean.split('/');
  const resolved: string[] = [];
  for (const part of parts) {
    if (part === '..') {
      if (resolved.length > 0) resolved.pop();
      // Allow traversal at root — just clamp
    } else if (part !== '.' && part !== '') {
      resolved.push(part);
    }
  }
  return '/' + resolved.join('/');
}

/**
 * Extract a query parameter value from a URL string.
 */
export function getQueryParam(url: string, name: string): string | null {
  try {
    let search: string;
    if (url.startsWith('/')) {
      const qIdx = url.indexOf('?');
      search = qIdx === -1 ? '' : url.substring(qIdx);
    } else {
      search = new URL(url).search;
    }
    const params = new URLSearchParams(search);
    return params.get(name);
  } catch {
    // best-effort: absent/unparseable query param reads as null.
    return null;
  }
}

/**
 * Safely parse JSON, returning null on failure.
 */
export function safeParseJson(str: string): Record<string, unknown> | null {
  try {
    return JSON.parse(str);
  } catch {
    // best-effort: unparseable payload reads as null by contract.
    return null;
  }
}

/** Create a JSON success response. */
export function jsonOk(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** Create a JSON error response. */
export function jsonError(message: string, status: number): Response {
  return new Response(JSON.stringify({ error: message, message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Handle POST /api/query — runs the full agent loop in the WASM shell.
 *
 * Returns 200 OK immediately (fire-and-forget). The agent runs
 * asynchronously and dispatches events via agentEventDispatcher.
 * The webui's event system picks up these events and renders them
 * (chat chunks, tool calls, file edits, etc.).
 *
 * The WASM agent calls the LLM via the platform proxy (/proxy/chat)
 * which handles authentication and key management.
 *
 * Events are dispatched in the WsEvent shape: { type, data: {...} }
 * This matches what useEventHandler expects (it reads event.data).
 */
/** The chat a request targets, when its JSON body names one. */
function chatIdFromBody(bodyStr?: string): string | undefined {
  if (!bodyStr) return undefined;
  try {
    const parsed = JSON.parse(bodyStr) as { chat_id?: unknown };
    return typeof parsed.chat_id === 'string' && parsed.chat_id ? parsed.chat_id : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Handle POST /api/query/steer — injects a steering message into the
 * persistent WASM agent. If the agent is mid-turn, the message is
 * queued for the next turn. This replaces the platform-backend steer
 * path which had no control over the in-browser agent.
 */
function handleWasmAgentSteer(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  let parsed: { query?: string; chat_id?: string };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }
  const query = parsed.query || '';
  if (!query) return jsonError('Query is required', 400);

  // Call the WASM steerAgent function which injects into the chat's
  // agent steering channel.
  if (shell.steerAgent) {
    const result = parsed.chat_id ? shell.steerAgent(query, parsed.chat_id) : shell.steerAgent(query);
    return jsonOk(result);
  }
  return jsonOk({ steered: false, error: 'steerAgent not available' });
}

/**
 * POST /api/ask-user/response — delivers the user's answer to a pending
 * ask_user request in the WASM agent. The agent's AskUserManager blocks
 * on the request; this call unblocks it so the agent loop continues.
 */
function handleWasmAskUserResponse(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  let parsed: { request_id?: string; response?: string };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }
  const requestId = parsed.request_id || '';
  const response = parsed.response || '';
  if (!requestId) return jsonError('request_id is required', 400);

  const result = shell.respondToAskUser?.(requestId, response);
  if (!result) {
    return jsonError('respondToAskUser not available (WASM binary too old)', 501);
  }
  if (!result.delivered) {
    return jsonError(`Ask user request ${requestId} not found or already expired`, 404);
  }
  return jsonOk({ delivered: result.delivered });
}

/**
 * POST /api/edits/{id}/decision — delivers the user's edit approval
 * decision to a pending edit approval request in the WASM agent.
 *
 * This handler is called from cloudAdapter.ts via dynamic path matching
 * (the registry is static-only and cannot express the dynamic {id} segment).
 * See the comment in cloudEndpointRegistry/endpoints/wasm-local.ts for context.
 *
 * Body: { accepted_hunks: string[], rejected: boolean }
 * Response: { edit_id: string, decided: true, accepted: number, rejected: boolean }
 * (matches the local-mode response format from pkg/webui/api_edits.go)
 */
export function handleWasmEditDecision(shell: WasmShell, editId: string, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  let parsed: { accepted_hunks?: string[]; rejected?: boolean };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }

  const acceptedHunks = parsed.accepted_hunks ?? [];
  const rejected = parsed.rejected ?? false;

  const result = shell.respondToEditDecision?.(editId, !rejected, acceptedHunks);
  if (!result) {
    return jsonError('respondToEditDecision not available (WASM binary too old)', 501);
  }

  if (!result.delivered) {
    return jsonError(`Edit approval request ${editId} not found or already expired`, 404);
  }

  return jsonOk({
    edit_id: editId,
    decided: true,
    accepted: acceptedHunks.length,
    rejected,
  });
}

/**
 * POST /api/shell-approvals/{id}/decision — delivers the user's shell approval
 * decision to a pending shell approval request in the WASM agent.
 *
 * This handler is called from cloudAdapter.ts via dynamic path matching.
 *
 * Body: { request_id?: string, decisions: Record<string, boolean> }
 * Response: { ok: true, request_id: string, delivered: true }
 * (matches the local-mode response format from pkg/webui/shell_approval_api.go)
 */
export function handleWasmShellApprovalDecision(shell: WasmShell, requestId: string, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  let parsed: { request_id?: string; decisions?: unknown };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }

  const decisions = parsed.decisions;
  if (decisions == null || typeof decisions !== 'object' || Array.isArray(decisions)) {
    return jsonError('decisions map required', 400);
  }

  const result = shell.respondToShellApproval?.(requestId, decisions as Record<string, boolean>);
  if (!result) {
    return jsonError('respondToShellApproval not available (WASM binary too old)', 501);
  }

  if (!result.delivered) {
    return jsonError('decision not delivered (unknown or expired request)', 410);
  }

  return jsonOk({ ok: true, request_id: requestId, delivered: true });
}

function handleWasmAgentQuery(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) {
    return jsonError('Missing request body', 400);
  }

  let parsed: { query?: string; provider?: string; model?: string; chat_id?: string; mode?: string };
  try {
    parsed = JSON.parse(bodyStr);
  } catch {
    return jsonError('Invalid JSON body', 400);
  }

  const query = parsed.query || '';
  const chatId = parsed.chat_id || '';

  if (!query) {
    return jsonError('Query is required', 400);
  }

  // Working directory context (services/workspaceCwd.ts): when a repo is
  // selected in the Files panel, tell the agent up front so it stops
  // exploring from the workspace top. Appended to the string handed to the
  // model ONLY — `query` (and thus query_started / chat history / the UI)
  // keeps the user's original message untouched.
  const cwdNote = workspaceCwdContextLine();
  const agentQuery = cwdNote ? `${query}\n\n[${cwdNote}]` : query;

  // Dispatch helper: wraps events in the { type, data } envelope that
  // useEventHandler expects, and stamps chat_id into data for multi-chat filtering.
  const dispatch = (type: string, data: Record<string, unknown> = {}) => {
    if (!agentEventDispatcher) return;
    if (chatId) {
      data.chat_id = chatId;
    }
    agentEventDispatcher({ type, data });
  };

  // Intercept /clear to reset the persistent agent's conversation history.
  // In local mode the backend handles this; in cloud mode we reset the
  // WASM agent so the next query starts fresh. This runs BEFORE the
  // one-run-per-chat guard: /clear is a reset, not a query, and the webui's
  // clear path stops the running query then immediately clears (setChatRunning
  // flips false only in the stopped run's async .then), so guarding it would
  // reject a legitimate clear with "already running".
  if (query.trim().toLowerCase() === '/clear') {
    shell.clearConversation(chatId || undefined);
    dispatch('query_completed', { query: '/clear', response: '' });
    return jsonOk({ status: 'ok', message: 'Conversation cleared' });
  }

  // One run per chat: a submit while this chat's agent is already answering is
  // rejected with the same machine-readable code the local backend uses, so
  // the webui's send path recovers by steering instead of surfacing a raw
  // "already in process" error. Without this, the WASM /api/query returned 200
  // and the run rejected asynchronously, leaving the composer (which thought
  // it was idle) dead-ended on the agent's ErrQueryInProgress.
  if (isChatRunning(chatId)) {
    return new Response(
      JSON.stringify({
        error: 'A query is already running for this chat',
        message: 'A query is already running for this chat',
        code: 'query_in_progress',
      }),
      { status: 409, headers: { 'Content-Type': 'application/json' } },
    );
  }

  // Dispatch query_started immediately so the user's message appears in
  // the chat and isProcessing flips on. Without this, the first visible
  // UI update is the first stream_chunk (assistant text), and the user's
  // own message never renders.
  dispatch('query_started', { query });

  // The agent's provider routes to the platform proxy, on this origin in
  // both local dev and production.
  const apiOrigin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080';

  // Fire the agent loop asynchronously — events stream via the dispatcher.
  // The chat's transcript records the turn itself, so switching away mid-turn
  // loses neither the question nor the answer.
  stopRequested.delete(chatId ?? '');
  setChatRunning(chatId, true);
  recordTurn(chatId, query);
  // A chat is named after its first question: have the list pick it up, as
  // the daemon's session_changed does.
  if (chatId) dispatch('session_changed', { change: 'updated', summary: { id: chatId } });
  writePlatformProviderConfig(shell, apiOrigin);
  shell
    .runAgent(
      'platform',
      '',
      agentQuery,
      (eventJson: string) => {
        try {
          const event = JSON.parse(eventJson);
          // Events from Go's wireAgentEventForwarding are already in
          // { type, data } shape (UIEvent serializes to this format).
          // Skip query_started — it's already dispatched above (optimistic)
          // and the agent's own query_started from the streaming callback
          // would duplicate the user message + isProcessing flip.
          if (event.type === 'query_started') return;
          // query_completed is handled by the .then() below which carries
          // the final response from the resolved promise. Skipping the
          // streaming version avoids a double decrement of
          // activeRequestsRef and potential message duplication.
          if (event.type === 'query_completed') return;
          // Stamp chat_id if missing.
          if (event.data && chatId && !event.data.chat_id) {
            event.data.chat_id = chatId;
          }
          if (agentEventDispatcher) {
            agentEventDispatcher(event);
          }
        } catch {
          // best-effort: unparseable agent events are dropped; the loop continues.
        }
      },
      chatId || undefined,
      // Seeds the chat's agent if it has none yet (e.g. after a reload), so
      // the conversation on screen is also the one the agent remembers.
      JSON.stringify(historyForChat(chatId, query)),
      parsed.mode,
    )
    .then((result) => {
      stopRequested.delete(chatId ?? '');
      setChatRunning(chatId, false);
      recordTurn(chatId, query, result.response);
      dispatch('query_completed', {
        response: result.response,
        provider: result.provider,
        model: result.model,
      });
      if (chatId) dispatch('session_changed', { change: 'updated', summary: { id: chatId } });
    })
    .catch((err) => {
      setChatRunning(chatId, false);
      // Stopped, not failed: end the turn without an error, as the daemon does.
      if (stopRequested.delete(chatId ?? '')) {
        dispatch('query_completed', { query, response: '', status: 'interrupted' });
        if (chatId) dispatch('session_changed', { change: 'updated', summary: { id: chatId } });
        return;
      }
      const { message, creditsBlocked } = describeAgentError(err instanceof Error ? err.message : String(err));
      dispatch('error', { message });
      if (creditsBlocked) notifyCreditsBlocked(message);
    });

  // Return immediately — the webui picks up events via the dispatcher
  return jsonOk({ status: 'processing', message: 'Agent query started' });
}
