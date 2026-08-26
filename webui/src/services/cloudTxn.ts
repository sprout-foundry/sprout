/**
 * cloudTxn.ts — typed client for the ETH-2 transactional escalation surface.
 *
 * When the browser WASM shell cannot run a command (exit 127 — no compilers
 * there), the work can be executed inside the user's cloud workspace
 * container as a three-phase transaction (see docs/txn-protocol.md):
 *
 *   POST /workspace/fly/{ws}/txn              → open (201) / 409 busy
 *   POST /workspace/fly/{ws}/txn/{id}/push    → apply browser file deltas
 *   POST /workspace/fly/{ws}/txn/{id}/run     → execute the command
 *   POST /workspace/fly/{ws}/txn/{id}/pull    → container deltas back
 *   POST /workspace/fly/{ws}/txn/{id}/finish  → close + stop the machine
 *
 * All calls use RELATIVE paths so the CloudAdapter intercepts them in cloud
 * mode and proxies to the Foundry backend with session credentials (same
 * convention as cloudTasks.ts).
 *
 * Also hosts the delta-manifest builders so the browser and the container
 * agree on the pinned shape: caps (5 MiB/file, 2000 files, 100 MiB total)
 * are honored client-side and over-cap entries are reported in `skipped`
 * instead of failing the whole transfer.
 *
 * Side-effect free and framework agnostic (no React imports).
 */

// ── Caps (mirrored from the daemon contract) ───────────────────────────────

export const TXN_MAX_FILE_BYTES = 5 * 1024 * 1024;
export const TXN_MAX_FILES = 2000;
export const TXN_MAX_TOTAL_BYTES = 100 * 1024 * 1024;

/** Default run timeout for an escalation-spawned command (seconds). */
export const TXN_RUN_TIMEOUT_SECONDS = 600;

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

export interface TxnRunResult {
  stdout: string;
  stderr: string;
  exit_code: number;
  duration_ms: number;
  timed_out: boolean;
  truncated: boolean;
}

export interface TxnPushResult {
  applied: number;
  deleted: number;
  skipped: TxnSkipped[];
  status: string;
}

export interface TxnStatus {
  txn_id: string;
  status: string;
  created_at?: string;
  expires_at?: string;
  run_result?: TxnRunResult | null;
}

export interface TxnFinishResult {
  status: string;
  txn_duration_seconds?: number;
  stop_initiated?: boolean;
}

export interface TxnWorkspace {
  workspace_id?: string;
  repo_url?: string;
  status?: string;
  [key: string]: unknown;
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

// ── Errors ──────────────────────────────────────────────────────────────────

/** Non-2xx platform response; `status` drives the friendly toast messages. */
export class CloudTxnError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'CloudTxnError';
    this.status = status;
  }
}

async function toTxnError(res: Response, action: string): Promise<Error> {
  const fallback = `${action} failed (HTTP ${res.status})`;
  try {
    const json = (await res.json()) as { error?: unknown } | null;
    const detail = json && typeof json.error === 'string' ? json.error.trim() : '';
    return new CloudTxnError(detail || fallback, res.status);
  } catch {
    return new CloudTxnError(fallback, res.status);
  }
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
 */
export async function buildPushManifest(
  readFiles: () => Promise<TxnPushInput[]> | TxnPushInput[],
  opts: { deletes?: string[]; gitSha?: string } = {},
): Promise<TxnManifest> {
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
    if (files.length >= TXN_MAX_FILES) {
      skipped.push({ path: input.path, reason: 'exceeds_file_count_cap' });
      continue;
    }
    const bytes = toBytes(input.content);
    if (bytes.byteLength > TXN_MAX_FILE_BYTES) {
      skipped.push({ path: input.path, reason: 'exceeds_per_file_cap' });
      continue;
    }
    if (total + bytes.byteLength > TXN_MAX_TOTAL_BYTES) {
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

// ── Workspace resolution ────────────────────────────────────────────────────

function normalizeRepoURL(url: string): string {
  return url
    .trim()
    .replace(/\/+$/, '')
    .replace(/\.git$/, '')
    .toLowerCase();
}

/**
 * Find the caller's fly workspace for `repoURL`, creating one when none
 * exists. The list response carries `repo_url` per workspace
 * (FlyWorkspaceView), so the match is client-side; a `.git` suffix or
 * trailing slash never breaks it.
 */
export async function resolveTxnWorkspace(repoURL: string): Promise<{ workspaceId: string; created: boolean }> {
  if (typeof repoURL !== 'string' || repoURL.trim() === '') {
    throw new TypeError('repoURL is required');
  }
  const wanted = normalizeRepoURL(repoURL);

  const listRes = await fetch('/workspace/fly', { method: 'GET', credentials: 'include' });
  if (listRes.ok) {
    const body = (await listRes.json()) as { workspaces?: TxnWorkspace[] } | null;
    const workspaces = Array.isArray(body?.workspaces) ? body.workspaces : [];
    const match =
      workspaces.find((ws) => normalizeRepoURL(String(ws.repo_url ?? '')) === wanted && ws.status === 'running') ??
      workspaces.find((ws) => normalizeRepoURL(String(ws.repo_url ?? '')) === wanted);
    if (match?.workspace_id) return { workspaceId: match.workspace_id, created: false };
  }
  // A failed list is not fatal — fall through to create, which reports its
  // own (more specific) error.

  const createRes = await fetch('/workspace/fly', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ repo_url: repoURL, mode: 'build' }),
  });
  if (!createRes.ok) {
    throw await toTxnError(createRes, 'Cloud workspace resolve');
  }
  const created = (await createRes.json()) as TxnWorkspace;
  if (typeof created?.workspace_id !== 'string' || created.workspace_id === '') {
    throw new TypeError('Cloud workspace resolve response is missing workspace_id');
  }
  return { workspaceId: created.workspace_id, created: true };
}

// ── Txn lifecycle ───────────────────────────────────────────────────────────

function txnURL(workspaceId: string, txnId: string, suffix = ''): string {
  return `/workspace/fly/${encodeURIComponent(workspaceId)}/txn/${encodeURIComponent(txnId)}${suffix}`;
}

async function postJSON<T>(url: string, action: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) throw await toTxnError(res, action);
  return (await res.json()) as T;
}

/** Open a transaction. 409 when one is already open for the workspace. */
export async function createTxn(workspaceId: string): Promise<{ txn_id: string; status: string; expires_at?: string }> {
  if (typeof workspaceId !== 'string' || workspaceId === '') throw new TypeError('workspaceId is required');
  const res = await postJSON<{ txn_id?: string; status?: string; expires_at?: string }>(
    `/workspace/fly/${encodeURIComponent(workspaceId)}/txn`,
    'Cloud txn open',
    {},
  );
  if (typeof res?.txn_id !== 'string' || res.txn_id === '') {
    throw new TypeError('Cloud txn open response is missing txn_id');
  }
  return { txn_id: res.txn_id, status: res.status ?? 'push', expires_at: res.expires_at };
}

export function txnPush(workspaceId: string, txnId: string, manifest: TxnManifest): Promise<TxnPushResult> {
  return postJSON<TxnPushResult>(txnURL(workspaceId, txnId, '/push'), 'Cloud txn push', manifest);
}

export function txnRun(
  workspaceId: string,
  txnId: string,
  command: string,
  timeoutSeconds = TXN_RUN_TIMEOUT_SECONDS,
): Promise<TxnRunResult> {
  if (typeof command !== 'string' || command.trim() === '') throw new TypeError('command is required');
  return postJSON<TxnRunResult>(txnURL(workspaceId, txnId, '/run'), 'Cloud txn run', {
    command,
    timeout_seconds: timeoutSeconds,
  });
}

export async function txnPull(workspaceId: string, txnId: string): Promise<TxnManifest> {
  const res = await fetch(txnURL(workspaceId, txnId, '/pull'), { method: 'POST', credentials: 'include' });
  if (!res.ok) throw await toTxnError(res, 'Cloud txn pull');
  const manifest = (await res.json()) as TxnManifest;
  return {
    base: manifest?.base ?? { git_sha: '', client: 'container' },
    files: Array.isArray(manifest?.files) ? manifest.files : [],
    deletes: Array.isArray(manifest?.deletes) ? manifest.deletes : [],
    truncated: Boolean(manifest?.truncated),
    skipped: Array.isArray(manifest?.skipped) ? manifest.skipped : [],
  };
}

export async function txnStatus(workspaceId: string, txnId: string): Promise<TxnStatus> {
  const res = await fetch(txnURL(workspaceId, txnId), { method: 'GET', credentials: 'include' });
  if (!res.ok) throw await toTxnError(res, 'Cloud txn status');
  const status = (await res.json()) as TxnStatus;
  if (typeof status?.txn_id !== 'string' || status.txn_id === '') {
    throw new TypeError('Cloud txn status response is missing txn_id');
  }
  return status;
}

export function txnFinish(workspaceId: string, txnId: string): Promise<TxnFinishResult> {
  return postJSON<TxnFinishResult>(txnURL(workspaceId, txnId, '/finish'), 'Cloud txn finish', {});
}
