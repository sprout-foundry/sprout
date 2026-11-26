/**
 * Native bridge backend for the projectFolderFs seam.
 *
 * Routes every operation to the shell's `files` bridge channel
 * (bridge-protocol.md §10.1): iOS via WKWebView message handlers, Android
 * via the AndroidBridge JS interface — the same transport as every other
 * channel, exposed to the page as `window.SproutStudioBridge.call`.
 *
 * Conventions mirror §10.1 exactly: operations never reject; expected
 * failures resolve `{ ok: false, error: <code> }`. Transport-level
 * failures (no bridge, timeout) map to `ioFailed` — features can treat
 * every result uniformly.
 */

import type { BatchResult, FsOk, ListResult, ReadResult, StatResult, ProjectFolderFs, WriteEntry } from './types';
import { normalizeFsPath, WRITE_BATCH_CAP } from './types';
import { withCurrentErrorCode } from '../nativeFs';

/** Op names a shell built before the project folder rename knows instead. */
const EARLIER_OPS: Readonly<Record<string, string>> = {
  readProjectFolderFile: 'readWorkspaceFile',
  writeProjectFolderFile: 'writeWorkspaceFile',
  listProjectFolder: 'listWorkspace',
};

/** Minimal shape of the bridge the shell injects (studio-bridge.js). */
export type BridgeCall = (
  channel: string,
  payload: Record<string, unknown>,
  timeoutMs?: number,
) => Promise<Record<string, unknown>>;

/** Locate the bridge the shell injected; null when not running in a shell. */
export function detectBridgeCall(): BridgeCall | null {
  if (typeof window === 'undefined') return null;
  const bridge = (window as unknown as { SproutStudioBridge?: { call?: BridgeCall } }).SproutStudioBridge;
  if (bridge && typeof bridge.call === 'function') {
    return (channel, payload, timeoutMs) => bridge.call!(channel, payload, timeoutMs);
  }
  return null;
}

/** Internal: run one files-channel op, normalizing transport failure. */
async function sendFilesOp(call: BridgeCall, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  try {
    // 60s: batched git-clone writes (base64 packfiles) can take a while
    // to land natively; the old 30s default made big batches time out and
    // look like silent data loss.
    const result = await call('files', payload, 60000);
    if (result && typeof result === 'object') return result;
    return { ok: false, error: 'ioFailed' };
  } catch {
    // Bridge missing / timed out / platform without the channel.
    return { ok: false, error: 'ioFailed' };
  }
}

/**
 * Run one files-channel op. A shell that predates an op's current name
 * answers "unknown files operation"; the op is then sent once more under its
 * earlier name. Results carry the current error codes.
 */
async function filesOp(call: BridgeCall, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  let r = await sendFilesOp(call, payload);
  const earlier = EARLIER_OPS[String(payload.op)];
  if (earlier && !r.ok && String(r.error ?? '').startsWith('unknown files operation')) {
    r = await sendFilesOp(call, { ...payload, op: earlier });
  }
  return withCurrentErrorCode(r);
}

export function createNativeBridgeFs(call: BridgeCall = requiredCall()): ProjectFolderFs {
  return {
    async read(path) {
      const r = await filesOp(call, { op: 'readProjectFolderFile', path: normalizeFsPath(path) });
      return (
        r.ok
          ? {
              ok: true,
              path: String(r.path ?? path),
              content: r.content as string | undefined,
              contentBase64: r.contentBase64 as string | undefined,
            }
          : { ok: false, error: String(r.error ?? 'ioFailed') }
      ) as ReadResult;
    },

    async write(path, payload) {
      const entry: WriteEntry = typeof payload === 'string' ? { path, content: payload } : { ...payload, path };
      const body: Record<string, unknown> = { op: 'writeProjectFolderFile', path: normalizeFsPath(entry.path) };
      if (entry.contentBase64 !== undefined) body.contentBase64 = entry.contentBase64;
      else body.content = entry.content ?? '';
      const r = await filesOp(call, body);
      return r.ok ? { ok: true } : { ok: false, error: String(r.error ?? 'ioFailed') };
    },

    async list(path = '', maxDepth = 3) {
      const r = await filesOp(call, {
        op: 'listProjectFolder',
        path: normalizeFsPath(path),
        maxDepth,
      });
      if (!r.ok) return { ok: false, error: String(r.error ?? 'ioFailed') } as ListResult;
      const files = Array.isArray(r.files) ? (r.files as Array<Record<string, unknown>>) : [];
      const prefix = normalizeFsPath(path);
      const mapped = files
        .map((f) => ({ path: String(f.path ?? ''), size: Number(f.size ?? 0), isDir: Boolean(f.isDir) }))
        .filter((f) => f.path !== '' && (prefix === '' || f.path === prefix || f.path.startsWith(prefix + '/')));
      return { ok: true, files: mapped };
    },

    async stat(path) {
      const r = await filesOp(call, { op: 'statPath', path: normalizeFsPath(path) });
      if (!r.ok) return { ok: false, error: String(r.error ?? 'notFound') } as StatResult;
      return { ok: true, path: String(r.path ?? path), size: Number(r.size ?? 0), isDir: Boolean(r.isDir) };
    },

    async mkdir(path) {
      const r = await filesOp(call, { op: 'mkdir', path: normalizeFsPath(path) });
      return r.ok ? { ok: true } : { ok: false, error: String(r.error ?? 'ioFailed') };
    },

    async remove(path) {
      const r = await filesOp(call, { op: 'deletePath', path: normalizeFsPath(path) });
      return r.ok ? { ok: true } : { ok: false, error: String(r.error ?? 'ioFailed') };
    },

    async rename(from, to) {
      const r = await filesOp(call, { op: 'renamePath', from: normalizeFsPath(from), to: normalizeFsPath(to) });
      return r.ok ? { ok: true } : { ok: false, error: String(r.error ?? 'ioFailed') };
    },

    async writeBatch(files) {
      if (!Array.isArray(files) || files.length === 0) {
        return { ok: false, errors: [], error: 'invalidParams' } as unknown as BatchResult;
      }
      if (files.length > WRITE_BATCH_CAP) {
        return { ok: false, errors: [], error: 'invalidParams' } as unknown as BatchResult;
      }
      const entries = files.map((f) => {
        const e: Record<string, unknown> = { path: normalizeFsPath(f.path) };
        if (f.contentBase64 !== undefined) e.contentBase64 = f.contentBase64;
        else e.content = f.content ?? '';
        return e;
      });
      const r = await filesOp(call, { op: 'writeBatch', files: entries });
      if (!r.ok) return { ok: false, errors: [], error: String(r.error ?? 'ioFailed') } as unknown as BatchResult;
      return {
        ok: true,
        written: Number(r.written ?? 0),
        errors: Array.isArray(r.errors) ? (r.errors as Array<{ path: string; error: string }>) : [],
      };
    },
  };
}

function requiredCall(): BridgeCall {
  const call = detectBridgeCall();
  if (!call) throw new Error('nativeBridgeFs requires a studio shell bridge (window.SproutStudioBridge)');
  return call;
}
