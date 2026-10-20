/**
 * browserWorkspaceFiles — the host contract's read-only in-browser workspace
 * file API (the move-to-container seam).
 *
 * A host that moves a project off the in-browser WASM agent (e.g. into a
 * container) must carry along the files the user edited in the browser but
 * never pushed. This module offers two read-only functions over that
 * in-browser workspace: list the project's files (path, size, mode, and
 * whether each differs from the repository's checkout) and read a file's
 * bytes. It is built on the pure VFS enumeration in `vfsFiles.ts` (the
 * runtime dirs are excluded) and the browser repository's checkout
 * comparison in `browserGit.ts`.
 *
 * The API is capability-gated: it is offered only when the active host
 * advertises `capabilities.browserFiles` and the agent runs in-browser
 * (`transport.agent.kind === 'wasm'`), and only while the browser's WASM
 * shell is reachable. Otherwise the calls reject with a stable error code
 * (the `browser_files_unavailable` convention, mirroring the host's other
 * stable error codes), so a host can branch on one source of truth.
 *
 * The shell is read from the installed adapter's cached instance, never
 * booted here: a host that has not started the in-browser agent gets the
 * honest gate error, not a lazily-spawned shell. The adapter is read through
 * the light `apiAdapter` singleton (no cloud adapter code is pulled in), and
 * the host is read through the active-host accessor.
 */

import { getActiveHost } from '../host/accessor';
import { getAdapter } from './apiAdapter';
import { gitCheckoutDiffers } from './browserGit';
import type { CloudAdapter } from './cloudAdapter';
import { isRuntimePath, listVfsFileEntries, resolveWorkspacePath, workspaceRootOf } from './vfsFiles';
import type { VfsFileEntry } from './vfsFiles';
import type { WasmShell } from './wasmShell';

/**
 * One workspace file as the host reads it: the VFS listing (path, size,
 * mode) plus whether the file differs from the repository's checkout. A
 * `false` means the container's clone already carries the same bytes (skip
 * it); a `true` (or an absent checkout) means carry it along.
 */
export interface WorkspaceFileEntry {
  /** Workspace-relative slash path, e.g. 'src/a.ts'. */
  path: string;
  /** The file's size in bytes, or 0 when the listing carried no size. */
  size: number;
  /** POSIX mode bits as the VFS records them, or 0 when the listing carried no mode. */
  mode: number;
  /** Whether the file is new or modified relative to the browser repository's checkout. */
  differsFromCheckout: boolean;
}

/**
 * The stable gate error: the in-browser file API is not available in this
 * host (the capability is off, the agent is not the in-browser agent, or the
 * browser's shell is not reachable). A host branches on `code`, not the
 * message, the way it branches on the host's other stable error codes.
 */
export class BrowserFilesUnavailableError extends Error {
  constructor(
    message: string,
    readonly code: 'browser_files_unavailable' = 'browser_files_unavailable',
  ) {
    super(message);
    this.name = 'BrowserFilesUnavailableError';
  }
}

/**
 * The in-browser workspace's WASM shell, gated on the host's capability and
 * agent backend. Throws the stable gate error when the API is unavailable,
 * so the two public functions share one availability check.
 */
function resolveActiveShell(): WasmShell {
  const host = getActiveHost();
  if (host?.capabilities.browserFiles !== true) {
    throw new BrowserFilesUnavailableError(
      'the in-browser file API is not offered by this host (capabilities.browserFiles is not set)',
    );
  }
  if (host?.transport.agent?.kind !== 'wasm') {
    throw new BrowserFilesUnavailableError(
      'the in-browser file API is offered only when the agent runs in-browser (transport.agent.kind "wasm")',
    );
  }
  const shell = (getAdapter() as CloudAdapter | null)?.getWasmShell?.() ?? null;
  if (!shell) {
    throw new BrowserFilesUnavailableError('the in-browser workspace has no reachable shell yet');
  }
  return shell;
}

/**
 * List the in-browser workspace's own files: path, size, mode, and whether
 * each differs from the repository's checkout. The runtime dirs (the agent
 * home, the scratch space) and .git are excluded; the listing reads no file
 * content. A file that is identical to the checkout (or to no checkout when
 * the browser repository was never wired) is reported by its `differs`
 * flag, so a host knows which to carry into a container.
 *
 * Rejects with the stable gate error when the API is unavailable (see
 * `BrowserFilesUnavailableError`).
 */
export async function listBrowserWorkspaceFiles(): Promise<WorkspaceFileEntry[]> {
  const shell = resolveActiveShell();
  const entries = listVfsFileEntries(shell);
  const paths = entries.map((e) => e.path);
  const differs = await gitCheckoutDiffers(paths);
  return entries.map((e: VfsFileEntry) => ({ ...e, differsFromCheckout: differs.get(e.path) ?? true }));
}

/**
 * Read one in-browser workspace file's bytes. The path is resolved against
 * the workspace root; a path that escapes the root or lands in a runtime
 * dir is refused. Prefers the byte-exact read and falls back to the
 * text read (re-encoded) when the binary has no byte reader.
 *
 * Rejects with the stable gate error when the API is unavailable, and with
 * an error naming the path when the file is unknown or unreadable.
 */
export async function readBrowserWorkspaceFile(path: string): Promise<Uint8Array> {
  const shell = resolveActiveShell();
  const root = workspaceRootOf(shell);
  const abs = resolveWorkspacePath(path, root, (p) => !shell.listDir(p).error);

  if (isRuntimePath(abs, root)) {
    throw new Error(`the in-browser file API will not read a runtime path: ${path}`);
  }
  const normalizedRoot = root.endsWith('/') ? root : root + '/';
  if (abs !== root && !abs.startsWith(normalizedRoot)) {
    throw new Error(`the in-browser file API will not read outside the workspace: ${path}`);
  }

  const bytesResult = typeof shell.readFileBytes === 'function' ? shell.readFileBytes(abs) : undefined;
  if (bytesResult?.bytes) return bytesResult.bytes;

  const textResult = shell.readFile(abs);
  if (textResult.error) {
    throw new Error(`could not read ${path}: ${textResult.error}`);
  }
  return new TextEncoder().encode(textResult.content);
}
