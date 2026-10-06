/**
 * cloudTxnManifest.ts — the delta manifests the ETH-2 txn protocol moves
 * between the browser and the workspace daemon (see docs/txn-protocol.md).
 *
 * The browser and the daemon agree on one pinned shape: caps (5 MiB/file,
 * 2000 files, 100 MiB total) are honored client-side and over-cap entries
 * are reported in `skipped` instead of failing the whole transfer.
 *
 * Side-effect free and framework agnostic (no React imports).
 */

// ── Caps (mirrored from the daemon contract) ───────────────────────────────

export const TXN_MAX_FILE_BYTES = 5 * 1024 * 1024;
export const TXN_MAX_FILES = 2000;
export const TXN_MAX_TOTAL_BYTES = 100 * 1024 * 1024;

// ── Contract shapes ─────────────────────────────────────────────────────────

export interface TxnSkipped {
  path: string;
  reason: string;
}

export interface TxnFile {
  path: string;
  content_base64: string;
  size: number;
  mode?: string;
}

export interface TxnManifest {
  base: { git_sha: string; client: string };
  files: TxnFile[];
  deletes: string[];
  truncated: boolean;
  skipped: TxnSkipped[];
}

/** A file the browser side can hand to buildPushManifest. */
export interface TxnPushInput {
  path: string;
  content: string | Uint8Array;
}

/** Result of applying a pulled manifest to the browser VFS. */
export interface TxnPullApplyResult {
  applied: number;
  deleted: number;
  skipped: TxnSkipped[];
}

/** VFS bridge the pull applier needs. Deletes are optional (the browser VFS
 *  bridge only knows how to write today). */
export interface TxnPullIO {
  writeFiles: (files: Array<{ path: string; content: string | Uint8Array }>) => Promise<void>;
  deleteFiles?: (paths: string[]) => Promise<void>;
}

// ── base64 helpers ──────────────────────────────────────────────────────────

const B64_CHUNK = 0x8000;

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += B64_CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + B64_CHUNK));
  }
  return btoa(binary);
}

export function base64ToBytes(b64: string): Uint8Array | null {
  try {
    const binary = atob(b64);
    const out = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) out[i] = binary.charCodeAt(i);
    return out;
  } catch {
    // best-effort: invalid base64 reads as null (caller treats as absent).
    return null;
  }
}

/** Path rules shared by both manifest directions (see txn-protocol.md). */
export function txnPathSkipReason(path: unknown): string | null {
  if (typeof path !== 'string' || path.trim() === '') return 'empty_path';
  if (path.includes('\0')) return 'nul_in_path';
  if (path.startsWith('/') || path.startsWith('\\') || /^[a-zA-Z]:/.test(path)) return 'absolute_path';
  for (const segment of path.split('/')) {
    if (segment === '..') return 'path_traversal';
    if (segment === '.git') return 'git_path';
    if (segment === '' || segment === '.') return 'invalid_path';
  }
  return null;
}

function toBytes(content: string | Uint8Array): Uint8Array {
  if (typeof content === 'string') return new TextEncoder().encode(content);
  return content;
}

// ── Manifest builders ───────────────────────────────────────────────────────

/**
 * Build a push manifest from the browser's files, honoring the daemon caps
 * client-side: per-file 5 MiB, 2000 files, 100 MiB total. Over-cap entries
 * land in `skipped` (with the contract's reason strings) instead of failing
 * the transfer, and `truncated` marks any skip. `opts.deletes` carries files
 * the browser removed relative to HEAD so the container converges.
 * `opts.maxFileBytes`/`opts.maxFiles`/`opts.maxTotalBytes` exist for tests;
 * production callers use the contract defaults.
 */
export async function buildPushManifest(
  readFiles: () => Promise<TxnPushInput[]> | TxnPushInput[],
  opts: {
    deletes?: string[];
    gitSha?: string;
    maxFileBytes?: number;
    maxFiles?: number;
    maxTotalBytes?: number;
  } = {},
): Promise<TxnManifest> {
  const maxFileBytes = opts.maxFileBytes ?? TXN_MAX_FILE_BYTES;
  const maxFiles = opts.maxFiles ?? TXN_MAX_FILES;
  const maxTotalBytes = opts.maxTotalBytes ?? TXN_MAX_TOTAL_BYTES;
  const inputs = await readFiles();
  const files: TxnFile[] = [];
  const skipped: TxnSkipped[] = [];
  let total = 0;

  for (const input of Array.isArray(inputs) ? inputs : []) {
    const pathReason = txnPathSkipReason(input?.path);
    if (pathReason) {
      skipped.push({ path: String(input?.path ?? ''), reason: pathReason });
      continue;
    }
    if (files.length >= maxFiles) {
      skipped.push({ path: input.path, reason: 'exceeds_file_count_cap' });
      continue;
    }
    const bytes = toBytes(input.content);
    if (bytes.byteLength > maxFileBytes) {
      skipped.push({ path: input.path, reason: 'exceeds_per_file_cap' });
      continue;
    }
    if (total + bytes.byteLength > maxTotalBytes) {
      skipped.push({ path: input.path, reason: 'exceeds_total_cap' });
      continue;
    }
    total += bytes.byteLength;
    files.push({ path: input.path, content_base64: bytesToBase64(bytes), size: bytes.byteLength, mode: '0644' });
  }

  const deletes: string[] = [];
  for (const path of opts.deletes ?? []) {
    const pathReason = txnPathSkipReason(path);
    if (pathReason) {
      skipped.push({ path: String(path), reason: pathReason });
      continue;
    }
    deletes.push(path);
  }

  return {
    base: { git_sha: opts.gitSha ?? '', client: 'wasm' },
    files,
    deletes,
    truncated: skipped.length > 0,
    skipped,
  };
}

/**
 * Apply a pulled manifest to the browser side: decode each file (an entry
 * whose base64 does not decode is skipped, never fatal), validate paths, hand
 * the batch to `io.writeFiles`, then process deletes via `io.deleteFiles`
 * when the bridge supports it.
 */
export async function applyPullManifest(manifest: TxnManifest, io: TxnPullIO): Promise<TxnPullApplyResult> {
  const skipped: TxnSkipped[] = [];
  const files: Array<{ path: string; content: string | Uint8Array }> = [];
  const deletes: string[] = [];

  for (const file of manifest?.files ?? []) {
    const pathReason = txnPathSkipReason(file?.path);
    if (pathReason) {
      skipped.push({ path: String(file?.path ?? ''), reason: pathReason });
      continue;
    }
    const bytes = base64ToBytes(file.content_base64 ?? '');
    if (bytes === null) {
      skipped.push({ path: file.path, reason: 'invalid_base64' });
      continue;
    }
    files.push({ path: file.path, content: bytes });
  }

  for (const path of manifest?.deletes ?? []) {
    const pathReason = txnPathSkipReason(path);
    if (pathReason) {
      skipped.push({ path: String(path), reason: pathReason });
      continue;
    }
    deletes.push(path);
  }

  if (files.length > 0) await io.writeFiles(files);

  let deleted = 0;
  if (deletes.length > 0) {
    if (typeof io.deleteFiles === 'function') {
      await io.deleteFiles(deletes);
      deleted = deletes.length;
    } else {
      for (const path of deletes) skipped.push({ path, reason: 'delete_unsupported' });
    }
  }

  return { applied: files.length, deleted, skipped };
}
