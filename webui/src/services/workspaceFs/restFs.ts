/**
 * REST backend for the workspaceFs seam (daemon / cloud mode).
 *
 * Maps the seam onto the existing daemon endpoints (`/api/files`,
 * `/api/create`, `/api/write`, `/api/read`, `/api/delete`, `/api/rename`).
 * Binary payloads ride as base64 in JSON, same as the native channel.
 *
 * The daemon routes are served by the local `sprout` web server; this
 * backend is what runs on desktop/web where the shell bridge is absent.
 */

import type {
  BatchResult,
  FsOk,
  FsEntry,
  ListResult,
  ReadResult,
  StatResult,
  WorkspaceFs,
  WriteEntry,
} from './types';
import { normalizeFsPath, WRITE_BATCH_CAP } from './types';

export type FetchFn = typeof fetch;

/** Map HTTP failure to the seam's error-code vocabulary. */
async function toResult(resp: Response): Promise<{ ok: true; data: Record<string, unknown> } | { ok: false; error: string }> {
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

function json(init: BodyInit): RequestInit {
  return { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: init };
}

export function createRestFs(fetchFn: FetchFn = fetch): WorkspaceFs {
  return {
    async read(path) {
      const resp = await fetchFn('/api/read', json(JSON.stringify({ path: normalizeFsPath(path) })));
      const r = await toResult(resp);
      if (!r.ok) return { ok: false, error: r.error } as ReadResult;
      const d = r.data;
      return {
        ok: true,
        path: String(d.path ?? path),
        content: d.content as string | undefined,
        contentBase64: d.contentBase64 as string | undefined,
      };
    },

    async write(path, payload) {
      const entry: WriteEntry = typeof payload === 'string' ? { path, content: payload } : { ...payload, path };
      const body: Record<string, unknown> = { path: normalizeFsPath(entry.path) };
      if (entry.contentBase64 !== undefined) body.contentBase64 = entry.contentBase64;
      else body.content = entry.content ?? '';
      const resp = await fetchFn('/api/write', json(JSON.stringify(body)));
      const r = await toResult(resp);
      return r.ok ? { ok: true } : { ok: false, error: r.error };
    },

    async list(path = '', maxDepth = 3) {
      const resp = await fetchFn('/api/files', { method: 'POST', body: JSON.stringify({ maxDepth }) });
      const r = await toResult(resp);
      if (!r.ok) return { ok: false, error: r.error } as ListResult;
      const files = Array.isArray((r.data as { files?: unknown }).files)
        ? ((r.data as { files: Array<Record<string, unknown>> }).files)
        : [];
      const prefix = normalizeFsPath(path);
      const mapped: FsEntry[] = files
        .map((f) => ({ path: String(f.path ?? ''), size: Number(f.size ?? 0), isDir: Boolean(f.isDir) }))
        .filter((f) => f.path !== '' && (prefix === '' || f.path === prefix || f.path.startsWith(prefix + '/')));
      return { ok: true, files: mapped };
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
      const resp = await fetchFn('/api/rename', json(JSON.stringify({ oldPath: normalizeFsPath(from), newPath: normalizeFsPath(to) })));
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
