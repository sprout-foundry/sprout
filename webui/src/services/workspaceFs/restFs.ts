/**
 * REST backend for the workspaceFs seam (daemon / cloud mode).
 *
 * Maps the seam onto the daemon endpoints the hosted editor also serves:
 * `/api/files` (list), `/api/file` (read/write), `/api/create`,
 * `/api/delete`, `/api/rename`. Binary files read back as base64; binary
 * writes are unsupported (the file endpoint stores text).
 *
 * The daemon routes are served by the local `sprout` web server; this
 * backend is what runs on desktop/web where the shell bridge is absent.
 */

import type { BatchResult, FsOk, FsEntry, ListResult, ReadResult, StatResult, WorkspaceFs, WriteEntry } from './types';
import { normalizeFsPath, WRITE_BATCH_CAP } from './types';

export type FetchFn = typeof fetch;

/** Map HTTP failure to the seam's error-code vocabulary. */
async function toResult(
  resp: Response,
): Promise<{ ok: true; data: Record<string, unknown> } | { ok: false; error: string }> {
  if (resp.ok) {
    try {
      const data = (await resp.json()) as Record<string, unknown>;
      return { ok: true, data };
    } catch {
      // best-effort: empty/non-JSON success body reads as empty data.
      return { ok: true, data: {} };
    }
  }
  let code = 'ioFailed';
  if (resp.status === 404) code = 'notFound';
  else if (resp.status === 409) code = 'exists';
  else if (resp.status === 400) code = 'invalidParams';
  try {
    const body = (await resp.json()) as { error?: string; message?: string };
    if (body && typeof body.error === 'string') code = body.error;
  } catch {
    // best-effort: non-JSON error body keeps the status-mapped code.
  }
  return { ok: false, error: code };
}

function isTextType(contentType: string): boolean {
  const type = contentType.split(';')[0].trim().toLowerCase();
  return type === '' || type.startsWith('text/') || type === 'application/json' || type === 'application/javascript';
}

function toBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

function json(init: BodyInit): RequestInit {
  return { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: init };
}

export function createRestFs(fetchFn: FetchFn = fetch): WorkspaceFs {
  return {
    // The file endpoint answers a read with the raw bytes: text types come
    // back as content, anything else as base64.
    async read(path) {
      const p = normalizeFsPath(path);
      const resp = await fetchFn(`/api/file?path=${encodeURIComponent(p)}`);
      if (!resp.ok) {
        const r = await toResult(resp);
        return { ok: false, error: r.ok ? 'ioFailed' : r.error } as ReadResult;
      }
      const type = resp.headers.get('Content-Type') ?? '';
      if (isTextType(type)) return { ok: true, path: p, content: await resp.text() };
      return { ok: true, path: p, contentBase64: toBase64(new Uint8Array(await resp.arrayBuffer())) };
    },

    async write(path, payload) {
      const entry: WriteEntry = typeof payload === 'string' ? { path, content: payload } : { ...payload, path };
      // The endpoint stores text; there is no binary write over REST.
      if (entry.contentBase64 !== undefined) return { ok: false, error: 'unsupported' };
      const p = normalizeFsPath(entry.path);
      const resp = await fetchFn(
        `/api/file?path=${encodeURIComponent(p)}`,
        json(JSON.stringify({ content: entry.content ?? '' })),
      );
      const r = await toResult(resp);
      return r.ok ? { ok: true } : { ok: false, error: r.error };
    },

    // The files endpoint lists one directory per call (GET, workspace-relative
    // `relative` paths), so deeper levels are walked here. Only the requested
    // directory's own failure fails the listing.
    async list(path = '', maxDepth = 3) {
      const prefix = normalizeFsPath(path);
      const inside = (p: string) => p !== '' && p !== '.' && (prefix === '' || p.startsWith(prefix + '/'));
      const files: FsEntry[] = [];
      const walk = async (dir: string, depth: number): Promise<string | null> => {
        const resp = await fetchFn(dir === '' ? '/api/files' : `/api/files?path=${encodeURIComponent(dir)}`);
        const r = await toResult(resp);
        if (!r.ok) return r.error;
        const raw = (r.data as { files?: unknown }).files;
        for (const e of Array.isArray(raw) ? (raw as Array<Record<string, unknown>>) : []) {
          const entryPath = normalizeFsPath(String(e.relative ?? ''));
          if (!inside(entryPath)) continue;
          const isDir = Boolean(e.is_dir);
          files.push({ path: entryPath, size: Number(e.size ?? 0), isDir });
          if (isDir && depth > 1) await walk(entryPath, depth - 1);
        }
        return null;
      };
      const error = await walk(prefix, maxDepth);
      return error ? ({ ok: false, error } as ListResult) : { ok: true, files };
    },

    async stat(path) {
      // No dedicated daemon endpoint; derive from a listing (cached cheaply
      // by the daemon for small workspaces; acceptable for stat's rarity).
      const p = normalizeFsPath(path);
      if (p === '') return { ok: true, path: '', size: 0, isDir: true };
      const listing = await this.list('', 6);
      if (!listing.ok) return { ok: false, error: listing.error } as StatResult;
      const hit = listing.files.find((f) => f.path === p);
      if (!hit) return { ok: false, error: 'notFound' } as StatResult;
      return { ok: true, path: p, size: hit.size, isDir: hit.isDir };
    },

    async mkdir(path) {
      const resp = await fetchFn('/api/create', json(JSON.stringify({ directory: normalizeFsPath(path), path })));
      const r = await toResult(resp);
      return r.ok ? { ok: true } : { ok: false, error: r.error };
    },

    async remove(path) {
      const resp = await fetchFn('/api/delete', json(JSON.stringify({ path: normalizeFsPath(path) })));
      const r = await toResult(resp);
      return r.ok ? { ok: true } : { ok: false, error: r.error };
    },

    async rename(from, to) {
      const resp = await fetchFn(
        '/api/rename',
        json(JSON.stringify({ oldPath: normalizeFsPath(from), newPath: normalizeFsPath(to) })),
      );
      const r = await toResult(resp);
      return r.ok ? { ok: true } : { ok: false, error: r.error };
    },

    async writeBatch(files) {
      if (!Array.isArray(files) || files.length === 0 || files.length > WRITE_BATCH_CAP) {
        return { ok: false, errors: [], error: 'invalidParams' } as unknown as BatchResult;
      }
      let written = 0;
      const errors: Array<{ path: string; error: string }> = [];
      // The daemon has no batch endpoint; sequential writes preserve the
      // continue-past-failure semantics of the native writeBatch.
      for (const f of files) {
        const r = await this.write(f.path, f);
        if (r.ok) written += 1;
        else errors.push({ path: f.path, error: r.error });
      }
      return { ok: true, written, errors };
    },
  };
}
