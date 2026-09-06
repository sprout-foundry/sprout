/**
 * gitFs — isomorphic-git filesystem adapter over the workspaceFs seam.
 *
 * isomorphic-git takes a POSIX-ish fs object (readFile, writeFile, unlink,
 * readdir, mkdir, rmdir, stat, lstat, symlink…). Implementing that over
 * the seam means clone/pull/push/commit run DIRECTLY against the active
 * workspace — whatever backend is live (native bridge on iOS/Android,
 * REST on desktop, memory in tests) — with no lightning-fs sidecar and no
 * export/import fan-out afterwards.
 *
 * Where repos live: `repos/<owner>/<name>/` — the same layout the agent's
 * git_clone/git_status/git_commit tools already use (services/agentGitTools.ts),
 * so the agent and the UI see the same clones.
 *
 * Batch optimization: isomorphic-git packfile writes arrive as many small
 * `writeFile` calls during clone/push. The adapter buffers sequential
 * writes and flushes them through `writeBatch` (single bridge round-trip),
 * keeping the ~3k-object clone from becoming ~3k bridge messages.
 */

import type { WorkspaceFs, WriteEntry } from './types';
import { normalizeFsPath } from './types';

/** Stats shape isomorphic-git expects (subset of node:fs Stats). */
export interface GitStats {
  isFile(): boolean;
  isDirectory(): boolean;
  isSymbolicLink(): boolean;
  size: number;
  /** ms or Date — git's index stores ctime/mtime; clones don't care about accuracy. */
  ctimeMs: number;
  mtimeMs: number;
  ctime: Date;
  mtime: Date;
}

type GitFsFile = Uint8Array | string;

export interface GitFs {
  readFile(path: string, opts?: { encoding?: string } | 'utf8'): Promise<GitFsFile>;
  writeFile(path: string, data: GitFsFile): Promise<void>;
  unlink(path: string): Promise<void>;
  readdir(path: string): Promise<string[]>;
  mkdir(path: string): Promise<void>;
  rmdir(path: string): Promise<void>;
  stat(path: string): Promise<GitStats>;
  lstat(path: string): Promise<GitStats>;
  readlink?(path: string): Promise<string>;
  symlink?(): never;
}

const SEAM_ENCODING = 'utf8';

export function createGitFs(fs: WorkspaceFs): GitFs {
  // ── Write coalescing ────────────────────────────────────────────────
  // isomorphic-git writes loose objects one at a time during clone
  // (objects/, packs, index…). Coalesce a bounded window of consecutive
  // writes into one writeBatch to cut bridge round-trips ~50x.
  const pending = new Map<string, WriteEntry>();
  const MAX_PENDING = 400;
  /**
   * Bridge messages must stay bounded: every batch is base64-encoded into
   * one postMessage/JSON payload. Packs regularly contain multi-MB blobs,
   * so cap each batch by serialized size as well as entry count — an
   * oversized single message risks blowing the bridge's transport budget
   * or stalling past the caller's timeout.
   */
  const MAX_BATCH_BYTES = 8 * 1024 * 1024;

  function entrySize(e: WriteEntry): number {
    let total = e.path.length + 12;
    if (e.content !== undefined) total += e.content.length;
    else if (e.contentBase64 !== undefined) total += Math.ceil((e.contentBase64.length * 3) / 4);
    return total;
  }

  async function flush(): Promise<void> {
    if (pending.size === 0) return;
    const all = Array.from(pending.values());
    pending.clear();
    // Split oversized batches into size-bounded chunks so no single
    // bridge message carries more than MAX_BATCH_BYTES of payload
    // (entries are base64-encoded into one postMessage/JSON body).
    let chunk: WriteEntry[] = [];
    let chunkSize = 0;
    for (const entry of all) {
      const size = entrySize(entry);
      if (chunk.length > 0 && chunkSize + size > MAX_BATCH_BYTES) {
        await writeBatchChecked(chunk);
        chunk = [];
        chunkSize = 0;
      }
      chunk.push(entry);
      chunkSize += size;
    }
    if (chunk.length > 0) await writeBatchChecked(chunk);
  }

  async function writeBatchChecked(batch: WriteEntry[]): Promise<void> {
    const r = await fs.writeBatch(batch);
    if (!r.ok) {
      // Swallowed batch failures here used to surface much later as
      // "commit <hash> is not available locally" during clone checkout —
      // isomorphic-git believed its objects were written when they were
      // not. Fail loudly with the transport-level error.
      throw new Error(`workspace writeBatch failed: ${r.error ?? 'unknown error'}`);
    }
    if (r.errors.length > 0) {
      const failed = r.errors.map((e) => e.path).slice(0, 5).join(', ');
      const detail = r.errors.length > 5 ? ` (+${r.errors.length - 5} more)` : '';
      throw new Error(`workspace writeBatch: ${r.errors.length} write(s) failed: ${failed}${detail}`);
    }
  }

  async function queueWrite(entry: WriteEntry): Promise<void> {
    pending.set(entry.path, entry);
    if (pending.size >= MAX_PENDING) await flush();
  }

  function decode(data: GitFsFile, encoding?: string | { encoding?: string }): Uint8Array | string {
    const enc = typeof encoding === 'string' ? encoding : encoding?.encoding;
    // NOTE: no `instanceof Uint8Array` — typed arrays can come from other
    // realms (jsdom/happy-dom polyfills), where instanceof lies. Duck-type.
    if (typeof data !== 'string') {
      if (enc === SEAM_ENCODING || enc === 'utf-8') return new TextDecoder().decode(data);
      // Copy through the constructor: guarantees a same-realm Uint8Array
      // even when the backend handed us a typed array from another realm
      // (vitest environments / webview polyfills).
      return new Uint8Array(data);
    }
    return data;
  }

  const adapter = {
    async readFile(path: string, opts?: { encoding?: string } | string) {
      await flush();
      const r = await fs.read(normalizeFsPath(path));
      if (!r.ok) {
        const err = new Error(r.error) as Error & { code?: string };
        if (r.error === 'notFound' || r.error === 'isDirectory') err.code = 'ENOENT';
        else if (r.error === 'notInWorkspace') err.code = 'EACCES';
        throw err;
      }
      const bytes = r.contentBase64 !== undefined ? base64ToBytes(r.contentBase64) : textToBytes(r.content ?? '');
      return decode(bytes, opts);
    },

    async writeFile(path: string, data: GitFsFile) {
      const p = normalizeFsPath(path);
      let content: string | undefined;
      let contentBase64: string | undefined;
      if (typeof data === 'string') content = data;
      else contentBase64 = bytesToBase64(data);
      await queueWrite({ path: p, content, contentBase64 });
    },

    async unlink(path: string) {
      await flush();
      const r = await fs.remove(normalizeFsPath(path));
      if (!r.ok && r.error !== 'notFound') {
        throw codeError(r.error === 'isDirectory' ? 'EISDIR' : 'EACCES', r.error);
      }
    },

    async readdir(path: string) {
      await flush();
      const p = normalizeFsPath(path);
      const r = await fs.list(p, 1);
      if (!r.ok) throw codeError(r.error === 'notFound' ? 'ENOENT' : 'EACCES', r.error);
      const prefix = p === '' ? '' : p + '/';
      return r.files
        .filter((f) => f.path.startsWith(prefix) || p === '')
        .map((f) => (p === '' ? f.path : f.path.slice(prefix.length)))
        .filter((name) => name !== '' && !name.includes('/'));
    },

    async mkdir(path: string) {
      await flush();
      const r = await fs.mkdir(normalizeFsPath(path));
      // mkdir -p semantics: existing dir (or race with writeBatch's parent
      // creation) is success; only a FILE at the path is an error.
      if (!r.ok && r.error !== 'exists' && r.error !== 'ioFailed') {
        throw codeError('EACCES', r.error);
      }
    },

    async rmdir(path: string) {
      await flush();
      const r = await fs.remove(normalizeFsPath(path));
      if (!r.ok && r.error !== 'notFound') throw codeError('EACCES', r.error);
    },

    async stat(path: string) {
      await flush();
      const p = normalizeFsPath(path);
      const r = await fs.stat(p);
      if (r.ok) return toStats(r.isDir, r.size);
      // isomorphic-git probes paths that may legitimately not exist.
      if (r.error === 'notFound') {
        // Distinguish via a listing probe (dir check) so ENOENT stays accurate.
        const parent = parentOf(p);
        const listing = await fs.list(parent, 1);
        if (listing.ok) {
          const leaf = leafOf(p);
          const hit = listing.files.find(
            (f) => (parent === '' ? f.path : f.path.slice(parent.length + 1)) === leaf,
          );
          if (hit) return toStats(hit.isDir, hit.size);
        }
        throw codeError('ENOENT', 'notFound');
      }
      throw codeError('EACCES', r.error);
    },
    lstat: undefined, // replaced just below with the stat implementation
    async readlink() {
      throw codeError('ENOSYS', 'unsupported');
    },
    // isomorphic-git's FileSystem wrapper binds every command in its list,
    // including symlink — it must EXIST even though the seam has no symlink
    // support (git worktrees/submodules would need it; clones don't).
    async symlink() {
      throw codeError('ENOSYS', 'unsupported');
    },
  } as unknown as GitFs & { __drain: () => Promise<void>; lstat: GitFs['stat'] };
  // isomorphic-git's FileSystem wrapper uses lstat for symlink checks and
  // falls back to stat only when lstat is absent — provide it explicitly.
  adapter.lstat = adapter.stat;
  // Expose the flush hook for drainGitWrites.
  adapter.__drain = flush;
  return adapter;
}

/** Await any buffered writes (call between git phases if ever needed). */
export async function drainGitWrites(gitFs: GitFs): Promise<void> {
  const internal = gitFs as unknown as { __drain?: () => Promise<void> };
  if (internal.__drain) await internal.__drain();
}

function toStats(isDir: boolean, size: number): GitStats {
  const now = new Date();
  return {
    isFile: () => !isDir,
    isDirectory: () => isDir,
    isSymbolicLink: () => false,
    size,
    // The git index records ctime/mtime; the seam doesn't carry them, so
    // stamp "now" (clones don't care; status staleness is acceptable).
    ctimeMs: now.getTime(),
    mtimeMs: now.getTime(),
    ctime: now,
    mtime: now,
  };
}

function codeError(code: string, detail: string): Error & { code?: string } {
  const err = new Error(`${code}: ${detail}`) as Error & { code?: string };
  err.code = code;
  return err;
}

function parentOf(p: string): string {
  const idx = p.lastIndexOf('/');
  return idx < 0 ? '' : p.slice(0, idx);
}

function leafOf(p: string): string {
  const idx = p.lastIndexOf('/');
  return idx < 0 ? p : p.slice(idx + 1);
}

function textToBytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i += 1) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

function bytesToBase64(bytes: Uint8Array): string {
  let bin = '';
  const CHUNK = 0x8000;
  for (let i = 0; i < bytes.length; i += CHUNK) {
    bin += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  return btoa(bin);
}
