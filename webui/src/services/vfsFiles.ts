/**
 * vfsFiles — pure VFS enumeration helpers, shared by the full webui's
 * cloud handlers and the standalone pages.
 *
 * Split out of cloudWasmHandlers.ts so the standalone embed pages can
 * track writes and enumerate files WITHOUT dragging the React-dependent
 * chain (workspaceCwd → react) into their bundles: the standalone pages
 * are advertised React-free, and this module keeps them honest.
 *
 * No React imports, no cloud/session state — only the shell.
 */

import type { WasmDirEntry, WasmShell } from './wasmShell';

/** HOME inside the WASM shell's virtual filesystem. */
export const AGENT_HOME = '/home/user';

/** Runtime directories: agent home (config/sessions) + scratch space. */
export const RUNTIME_DIRS = [AGENT_HOME, '/tmp'];

/**
 * The VFS write manifest: every path written through the bridge, for
 * binaries whose listDir is broken (O_DIRECTORY bug). Module-level so the
 * full app's handlers and any embed page share one view.
 */
const vfsManifest = new Set<string>();

/** The project directory. Binaries built before the bridge exposed it kept
 * everything at the (unmovable) cwd. */
export function workspaceRootOf(wasm: { getCwd(): string; getWorkspaceRoot?(): string } | undefined): string {
  if (!wasm) return '/workspace';
  return wasm.getWorkspaceRoot ? wasm.getWorkspaceRoot() : wasm.getCwd();
}

/** Normalize a path to absolute form; relative paths are relative to the
 *  workspace (never the terminal's cwd, which `cd` moves). */
export function normalizeVfsPath(p: string): string {
  if (!p.startsWith('/')) {
    const root = workspaceRootOf(typeof window !== 'undefined' ? window.SproutWasm : undefined);
    p = p === '.' ? root : `${root}/${p}`;
  }
  // Collapse ./ and resolve ../
  const parts = p.split('/');
  const resolved: string[] = [];
  for (const part of parts) {
    if (part === '' || part === '.') continue;
    if (part === '..') {
      resolved.pop();
      continue;
    }
    resolved.push(part);
  }
  return '/' + resolved.join('/');
}

/** Track a file write in the manifest. */
export function trackFileWrite(rawPath: string): void {
  vfsManifest.add(normalizeVfsPath(rawPath));
}

/** Drop a path from the manifest (deletes). */
export function untrackFileWrite(rawPath: string): void {
  vfsManifest.delete(normalizeVfsPath(rawPath));
}

/**
 * Read-only snapshot of the VFS write manifest. Used by browserGit's VFS
 * bridge to enumerate files when the deployed WASM binary's listDir is
 * broken (O_DIRECTORY bug). Returns a copy so callers can't mutate state.
 */
export function getVfsManifestSnapshot(): Set<string> {
  return new Set(vfsManifest);
}

const within = (p: string, dir: string) => p === dir || p.startsWith(`${dir}/`);

/** Whether `p` is runtime state rather than content of the workspace at `root`. */
export function isRuntimePath(p: string, root: string): boolean {
  return RUNTIME_DIRS.some((dir) => within(p, dir) && !within(root, dir));
}

/** Join a directory and a child name into an absolute path. */
export function joinVfsPath(dir: string, name: string): string {
  return dir === '/' ? `/${name}` : `${dir}/${name}`;
}

/**
 * Read all files from the WASM VFS, returning {path, content} pairs with
 * workspace-relative paths. Used by browserGit to sync the working tree
 * before git operations, and by the standalone escalation push.
 */
export async function listAllVfsFiles(shell: WasmShell): Promise<Array<{ path: string; content: string }>> {
  const cwd = workspaceRootOf(shell);
  const files: Array<{ path: string; content: string }> = [];

  // Every file in the tree: a one-level listing left out everything in
  // subfolders, and git then reported those files as deleted.
  let paths: string[] = [];
  try {
    paths = listAllFilesTracked(shell, cwd);
  } catch {
    // Fall back to manifest
    paths = Array.from(vfsManifest);
  }

  for (const absPath of paths) {
    if (isRuntimePath(absPath, cwd)) continue;
    try {
      const result = shell.readFile(absPath);
      if (!result.error) {
        // Only the workspace's own files, relative to it.
        const normalizedCwd = cwd.endsWith('/') ? cwd : cwd + '/';
        if (!absPath.startsWith(normalizedCwd)) continue;
        files.push({ path: absPath.slice(normalizedCwd.length), content: result.content });
      }
    } catch {
      // best-effort: skip unreadable entries.
    }
  }
  return files;
}

/**
 * Recursively list all files in a directory using listDir with manifest
 * fallback. Returns absolute paths.
 */
function listAllFilesTracked(shell: WasmShell, dir: string): string[] {
  // Try recursive listDir first.
  const result = flattenEntries(shell, dir);
  if (result.length > 0) return result.map((f) => f.path);

  // Fall back to manifest.
  return listFilesTracked(shell, dir);
}

/**
 * Get all known files from the manifest that are descendants of dir.
 * Tries listDir first; falls back to manifest on error.
 */
function listFilesTracked(shell: WasmShell, dir: string): string[] {
  // Try the WASM binary's listDir first — works on newer binaries.
  try {
    const result = shell.listDir(dir);
    if (!result.error && result.entries && result.entries.length > 0) {
      // listDir works — return entries as full paths.
      return result.entries
        .filter((e) => e.type === 'file')
        .map((e) => {
          const base = dir === '/' ? '' : dir;
          return `${base}/${e.name}`.replace(/\/+/g, '/');
        });
    }
  } catch {
    // listDir broken — fall through to manifest.
  }

  // Fall back to the manifest.
  const normalizedDir = normalizeVfsPath(dir);
  const files = Array.from(vfsManifest).filter((path) => {
    if (normalizedDir === '/') return path.startsWith('/'); // root: match everything
    return path.startsWith(normalizedDir + '/') || path === normalizedDir;
  });
  return files.sort();
}

/** Recursively flatten WASM directory entries into a flat file list. */
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

export type { WasmDirEntry, WasmShell };
