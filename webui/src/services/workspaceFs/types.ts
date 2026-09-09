/**
 * workspaceFs — THE filesystem seam (Track R).
 *
 * One typed contract for every workspace filesystem operation the webui
 * performs, with three interchangeable backends:
 *
 *   - `nativeBridgeFs`  — studio shells (iOS/Android) via the `files` bridge
 *                         channel (bridge-protocol.md §10.1).
 *   - `restFs`          — cloud/desktop daemon via the REST API.
 *   - `memoryFs`        — in-memory backend for tests (and any future
 *                         sandboxed-browser mode).
 *
 * Why one seam: features (file tree, editor saves, git clone via the
 * isomorphic-git adapter, zip import/export, agent file tools) must not
 * each invent their own fs-shaped plumbing. They program against
 * `WorkspaceFs`, pick a backend once at startup, and the same code runs
 * against the native workspace, the daemon API, or a test double.
 *
 * Conventions (mirroring bridge-protocol.md §10.1):
 *   - Paths are workspace-relative POSIX (`src/lib/a.ts`, `repos/…`),
 *     never absolute, never containing `..`. Backends reject unsafe paths.
 *   - Operations never throw for expected failures — they resolve
 *     `{ ok: false, error: <code> }`. Error codes are the §10.1 set:
 *     `invalidParams`, `workspaceNotSet`, `notInWorkspace`, `notFound`,
 *     `exists`, `isDirectory`, `ioFailed`, `unsupported`.
 *   - `write` creates missing parent directories (mkdir -p semantics).
 *   - Text content is UTF-8; binary goes through `contentBase64`.
 */

/** One entry in a directory listing. */
export interface FsEntry {
  /** Workspace-relative path (`src/lib/a.ts`). */
  path: string;
  /** Size in bytes (0 for directories). */
  size: number;
  isDir: boolean;
}

/** Payload for a single write. Exactly one of content/contentBase64. */
export interface WriteEntry {
  path: string;
  content?: string;
  contentBase64?: string;
}

/** Discriminated result. `ok: false` carries an error CODE, not prose. */
export type FsResult<T = undefined> = ({ ok: true } & T) | { ok: false; error: string };

export type FsOk = FsResult<{ path?: string }>;
export type ReadResult = FsResult<{ path: string; content?: string; contentBase64?: string }>;
export type ListResult = FsResult<{ files: FsEntry[] }>;
export type StatResult = FsResult<{ path: string; size: number; isDir: boolean }>;
export type BatchResult = FsResult<{ written: number; errors: Array<{ path: string; error: string }> }>;

/** writeBatch hard cap (mirrors native FilesHandler + protocol §10.1). */
export const WRITE_BATCH_CAP = 5000;

/**
 * The seam. Every method resolves; nothing throws for expected failures.
 */
export interface WorkspaceFs {
  /** Read one file. UTF-8 → `content`, binary → `contentBase64`. */
  read(path: string): Promise<ReadResult>;
  /** Write one file, creating parent dirs. */
  write(path: string, payload: string | WriteEntry): Promise<FsOk>;
  /**
   * List workspace files under `path` (default: workspace root).
   * `maxDepth` is a hint; backends may return MORE depth than asked but
   * never less for the native backend (it lists root deep and filters).
   */
  list(path?: string, maxDepth?: number): Promise<ListResult>;
  /** Metadata for one path. */
  stat(path: string): Promise<StatResult>;
  /** Create a directory plus missing components. Idempotent for dirs. */
  mkdir(path: string): Promise<FsOk>;
  /** Delete a file or directory (recursive). */
  remove(path: string): Promise<FsOk>;
  /** Rename/move within the workspace. Creates `to`'s missing parents. */
  rename(from: string, to: string): Promise<FsOk>;
  /** Write many files, continuing past per-entry failures. */
  writeBatch(files: WriteEntry[]): Promise<BatchResult>;
}

/** Structural check so features can assert they got a usable backend. */
export function isWorkspaceFs(obj: unknown): obj is WorkspaceFs {
  if (!obj || typeof obj !== 'object') return false;
  const c = obj as Record<string, unknown>;
  return (
    typeof c.read === 'function' &&
    typeof c.write === 'function' &&
    typeof c.list === 'function' &&
    typeof c.stat === 'function' &&
    typeof c.mkdir === 'function' &&
    typeof c.remove === 'function' &&
    typeof c.rename === 'function' &&
    typeof c.writeBatch === 'function'
  );
}

/** Normalize a workspace-relative path; empty string means root. */
export function normalizeFsPath(input: string): string {
  return (input || '').replace(/^\/+/, '').replace(/\/+$/, '');
}
