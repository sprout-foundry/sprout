/**
 * In-memory backend for the workspaceFs seam.
 *
 * Used by unit tests and any future sandboxed-browser mode. Implements the
 * full contract against a plain nested map — including writeBatch's
 * continue-past-failure semantics and the 5000-entry cap — so tests assert
 * behavior, not backend quirks.
 */

import type { BatchResult, FsOk, FsEntry, ListResult, ReadResult, StatResult, WorkspaceFs, WriteEntry } from './types';
import { normalizeFsPath, WRITE_BATCH_CAP } from './types';

/** Serialized entry stored under its normalized path. */
interface MemEntry {
  isDir: boolean;
  content?: Uint8Array;
}

function encodeBytes(entry: WriteEntry): Uint8Array | null {
  if (entry.contentBase64 !== undefined) {
    const bin = atob(entry.contentBase64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i += 1) bytes[i] = bin.charCodeAt(i);
    return bytes;
  }
  return new TextEncoder().encode(entry.content ?? '');
}

function decode(bytes: Uint8Array): { content?: string; contentBase64?: string } {
  try {
    return { content: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
  } catch {
    // best-effort: non-UTF-8 bytes fall back to base64 storage.
    let bin = '';
    for (let i = 0; i < bytes.length; i += 1) bin += String.fromCharCode(bytes[i]);
    return { contentBase64: btoa(bin) };
  }
}

/** Split a normalized path into [parentDir, leaf]. */
function splitParent(path: string): [string, string] {
  const idx = path.lastIndexOf('/');
  return idx < 0 ? ['', path] : [path.slice(0, idx), path.slice(idx + 1)];
}

export function createMemoryFs(seed: Record<string, string | Uint8Array> = {}): WorkspaceFs {
  const entries = new Map<string, MemEntry>();
  entries.set('', { isDir: true });

  const ensureDir = (dir: string): boolean => {
    if (dir === '') return true;
    const existing = entries.get(dir);
    if (existing) return existing.isDir;
    const [parent, leaf] = splitParent(dir);
    if (!ensureDir(parent)) return false;
    entries.set(dir, { isDir: true });
    void leaf;
    return true;
  };

  const exists = (path: string): boolean => entries.has(path);

  const childrenOf = (dir: string): string[] => {
    const prefix = dir === '' ? '' : dir + '/';
    const out: string[] = [];
    for (const key of entries.keys()) {
      if (key !== '' && key.startsWith(prefix) && !key.slice(prefix.length).includes('/')) out.push(key);
    }
    return out;
  };

  const unsafe = (p: string): boolean => p.split('/').some((seg) => seg === '' || seg === '.' || seg === '..');

  // Seed files.
  for (const [path, data] of Object.entries(seed)) {
    const p = normalizeFsPath(path);
    if (p === '' || unsafe(p)) continue;
    const bytes = typeof data === 'string' ? new TextEncoder().encode(data) : data;
    const [parent] = splitParent(p);
    if (!ensureDir(parent)) continue;
    entries.set(p, { isDir: false, content: bytes });
  }

  return {
    async read(path) {
      const p = normalizeFsPath(path);
      const entry = entries.get(p);
      if (!entry) return { ok: false, error: 'notFound' } as ReadResult;
      if (entry.isDir) return { ok: false, error: 'isDirectory' } as ReadResult;
      return { ok: true, path: p, ...decode(entry.content ?? new Uint8Array()) };
    },

    async write(path, payload) {
      const entry: WriteEntry = typeof payload === 'string' ? { path, content: payload } : { ...payload };
      const p = normalizeFsPath(entry.path || path);
      if (p === '' || unsafe(p)) return { ok: false, error: 'notInWorkspace' };
      const bytes = encodeBytes(entry);
      if (bytes === null) return { ok: false, error: 'invalidParams' };
      const existing = entries.get(p);
      if (existing?.isDir) return { ok: false, error: 'isDirectory' };
      const [parent] = splitParent(p);
      if (!ensureDir(parent)) return { ok: false, error: 'ioFailed' };
      entries.set(p, { isDir: false, content: bytes });
      return { ok: true };
    },

    async list(path = '', _maxDepth) {
      void _maxDepth;
      const dir = normalizeFsPath(path);
      if (dir !== '' && !exists(dir)) return { ok: false, error: 'notFound' } as ListResult;
      const dirEntry = entries.get(dir);
      if (dirEntry && !dirEntry.isDir) return { ok: false, error: 'isDirectory' } as ListResult;
      const prefix = dir === '' ? '' : dir + '/';
      const files: FsEntry[] = [];
      const seen = new Set<string>();
      for (const [key, value] of entries.entries()) {
        if (key === '' || !key.startsWith(prefix)) continue;
        const rest = key.slice(prefix.length);
        if (rest === '') continue;
        if (rest.includes('/')) {
          // Derive the immediate child dir (registered once, even when both
          // an explicit dir entry and deeper files exist).
          const childDir = prefix + rest.split('/')[0];
          if (!seen.has(childDir)) {
            seen.add(childDir);
            files.push({ path: childDir, size: 0, isDir: true });
          }
        } else {
          if (!seen.has(key)) {
            seen.add(key);
            files.push({ path: key, size: value.content?.length ?? 0, isDir: value.isDir });
          }
        }
      }
      files.sort((a, b) => a.path.localeCompare(b.path));
      return { ok: true, files };
    },

    async stat(path) {
      const p = normalizeFsPath(path);
      const entry = entries.get(p);
      if (!entry) return { ok: false, error: 'notFound' } as StatResult;
      return { ok: true, path: p, size: entry.content?.length ?? 0, isDir: entry.isDir };
    },

    async mkdir(path) {
      const p = normalizeFsPath(path);
      if (p === '' || unsafe(p)) return { ok: false, error: 'notInWorkspace' };
      const existing = entries.get(p);
      if (existing && !existing.isDir) return { ok: false, error: 'exists' };
      if (!ensureDir(p)) return { ok: false, error: 'ioFailed' };
      return { ok: true };
    },

    async remove(path) {
      const p = normalizeFsPath(path);
      if (p === '') return { ok: false, error: 'notInWorkspace' };
      if (!exists(p)) return { ok: false, error: 'notFound' };
      // Delete the subtree: every entry under p.
      const prefix = p + '/';
      for (const key of Array.from(entries.keys())) {
        if (key === p || key.startsWith(prefix)) entries.delete(key);
      }
      return { ok: true };
    },

    async rename(from, to) {
      const src = normalizeFsPath(from);
      const dst = normalizeFsPath(to);
      if (src === '' || unsafe(dst) || dst === '') return { ok: false, error: 'notInWorkspace' };
      if (!exists(src)) return { ok: false, error: 'notFound' };
      if (exists(dst)) return { ok: false, error: 'exists' };
      if (dst === src || dst.startsWith(src + '/')) return { ok: false, error: 'ioFailed' };
      const [dstParent] = splitParent(dst);
      if (!ensureDir(dstParent)) return { ok: false, error: 'ioFailed' };
      const moved: Array<[string, MemEntry]> = [];
      const prefix = src + '/';
      for (const [key, value] of entries.entries()) {
        if (key === src) moved.push([dst, value]);
        else if (key.startsWith(prefix)) moved.push([dst + '/' + key.slice(prefix.length), value]);
      }
      for (const [key, value] of moved) entries.set(key, value);
      // Remove the original source subtree (children were copied above;
      // dst-inside-src is rejected, so the new keys never match `prefix`).
      entries.delete(src);
      for (const key of Array.from(entries.keys())) {
        if (key.startsWith(prefix)) entries.delete(key);
      }
      return { ok: true } as FsOk;
    },

    async writeBatch(files) {
      if (!Array.isArray(files) || files.length === 0 || files.length > WRITE_BATCH_CAP) {
        return { ok: false, errors: [], error: 'invalidParams' } as unknown as BatchResult;
      }
      let written = 0;
      const errors: Array<{ path: string; error: string }> = [];
      for (const f of files) {
        const r = await this.write(f.path, f);
        if (r.ok) written += 1;
        else errors.push({ path: f.path, error: r.error });
      }
      return { ok: true, written, errors };
    },
  };
}
