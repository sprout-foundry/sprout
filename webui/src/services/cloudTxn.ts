/**
 * cloudTxn.ts — typed client for the ETH-2 transactional escalation surface.
 *
 * When the browser WASM shell cannot run a command (exit 127 — no compilers
 * there), the work can be executed inside the user's cloud workspace
 * container as a three-phase transaction (see docs/txn-protocol.md):
 *
 *   POST /workspace/txn                      → resolve/create the workspace
 *   POST /workspace/txn/{ws}/txn             → open (201) / 409 busy
 *   POST /workspace/txn/{ws}/txn/{id}/push   → apply browser file deltas
 *   POST /workspace/txn/{ws}/txn/{id}/run    → execute the command
 *   POST /workspace/txn/{ws}/txn/{id}/pull   → container deltas back
 *   POST /workspace/txn/{ws}/txn/{id}/finish → close
 *
 * The workspace may be Fly-hosted or hosted on the user's own runner
 * (SP-BUILDER-12); the platform routes per workspace backend, so the client
 * never needs to know which.
 *
 * All calls use RELATIVE paths so the CloudAdapter intercepts them in cloud
 * mode and proxies to the Foundry backend with session credentials (same
 * convention as cloudTasks.ts).
 *
 * The delta-manifest builders live in cloudTxnManifest.ts and are
 * re-exported here so callers keep one import surface.
 *
 * Side-effect free and framework agnostic (no React imports).
 */

import type { TxnManifest, TxnSkipped } from './cloudTxnManifest';

export {
  applyPullManifest,
  base64ToBytes,
  buildPushManifest,
  bytesToBase64,
  txnPathSkipReason,
  TXN_MAX_FILE_BYTES,
  TXN_MAX_FILES,
  TXN_MAX_TOTAL_BYTES,
} from './cloudTxnManifest';
export type { TxnFile, TxnManifest, TxnPullApplyResult, TxnPullIO, TxnPushInput, TxnSkipped } from './cloudTxnManifest';

/** Default run timeout for an escalation-spawned command (seconds). */
export const TXN_RUN_TIMEOUT_SECONDS = 600;

/**
 * Budget for the lifecycle calls that open a workspace/transaction (resolve,
 * create, txn open). These are quick platform round trips; without a bound, a
 * hung request leaves the escalation toast spinning on "Starting cloud
 * container" forever with no way out but a reload.
 */
export const TXN_OPEN_TIMEOUT_MS = 120_000;

/**
 * fetch() with a hard timeout. Aborts (and rejects) after `timeoutMs` so a
 * stalled platform request surfaces as an error the toast can show instead of
 * an eternal progress spinner.
 */
async function fetchWithTimeout(url: string, init: RequestInit, timeoutMs: number): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } catch (err) {
    if (controller.signal.aborted) {
      throw new Error(`request timed out after ${Math.round(timeoutMs / 1000)}s`);
    }
    throw err;
  } finally {
    clearTimeout(timer);
  }
}

// ── Contract shapes ─────────────────────────────────────────────────────────

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
  /** "fly" | "runner" | "docker" (legacy). */
  backend?: string;
  /** Set on runner-hosted rows; only GET /workspace/{id} returns it. */
  runner_id?: string;
  [key: string]: unknown;
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

// ── Workspace resolution ────────────────────────────────────────────────────

/**
 * Where the workspace should live (SP-159 §159d): "auto" lets the platform
 * pick (the user's runner when one is available, else Fly), "fly" is the
 * cloud, "runner" pins one of the user's runners by id.
 */
export type TxnHostChoice = { host: 'auto' } | { host: 'fly' } | { host: 'runner'; runnerId: string };

const AUTO_CHOICE: TxnHostChoice = { host: 'auto' };

/**
 * 409 from a create pinned to one runner: the runner is not the caller's,
 * offline, or at capacity. Escalation offers the cloud instead.
 */
export class RunnerUnavailableError extends CloudTxnError {
  readonly runnerId: string;

  constructor(runnerId: string) {
    super('runner_unavailable', 409);
    this.name = 'RunnerUnavailableError';
    this.runnerId = runnerId;
  }
}

async function getJSON<T>(url: string): Promise<T | null> {
  try {
    const res = await fetchWithTimeout(url, { method: 'GET', credentials: 'include' }, TXN_OPEN_TIMEOUT_MS);
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    // best-effort lookup: the caller falls through to create.
    return null;
  }
}

/**
 * GET /workspace/txn/resolve — the caller's live workspace for the repo on
 * the chosen host (the platform filters by host and runner, newest first).
 */
function resolveWorkspace(repoURL: string, choice: TxnHostChoice): Promise<TxnWorkspace | null> {
  const params = new URLSearchParams({ repo_url: repoURL });
  if (choice.host !== 'auto') params.set('host', choice.host);
  if (choice.host === 'runner') params.set('runner_id', choice.runnerId);
  return getJSON<TxnWorkspace>('/workspace/txn/resolve?' + params.toString());
}

async function createWorkspace(repoURL: string, choice: TxnHostChoice): Promise<TxnWorkspace> {
  const body: Record<string, string> = { repo_url: repoURL, host: choice.host };
  if (choice.host === 'runner') body.runner_id = choice.runnerId;
  const createRes = await fetchWithTimeout(
    '/workspace/txn',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    },
    TXN_OPEN_TIMEOUT_MS,
  );
  if (!createRes.ok) {
    const err = await toTxnError(createRes, 'Cloud workspace resolve');
    if (choice.host === 'runner' && createRes.status === 409 && err.message === 'runner_unavailable') {
      throw new RunnerUnavailableError(choice.runnerId);
    }
    throw err;
  }
  const created = (await createRes.json()) as TxnWorkspace;
  if (typeof created?.workspace_id !== 'string' || created.workspace_id === '') {
    throw new TypeError('Cloud workspace resolve response is missing workspace_id');
  }
  return created;
}

/**
 * Find the caller's workspace for `repoURL` on the chosen host, creating one
 * when none exists. With "auto" the platform picks the host (resolve any
 * backend, else create where it decides). With "fly" or a pinned runner the
 * chosen host wins even when the user also has a workspace for the repo
 * elsewhere. A pinned runner that cannot take the workspace rejects with
 * RunnerUnavailableError.
 */
export async function resolveTxnWorkspace(
  repoURL: string,
  choice: TxnHostChoice = AUTO_CHOICE,
): Promise<{ workspaceId: string; created: boolean }> {
  if (typeof repoURL !== 'string' || repoURL.trim() === '') {
    throw new TypeError('repoURL is required');
  }

  // A failed resolve is not fatal — fall through to create, which reports
  // its own (more specific) error. Create is idempotent for a runner, and
  // resumes the existing Fly workspace for the cloud.
  const existing = await resolveWorkspace(repoURL, choice);
  if (typeof existing?.workspace_id === 'string' && existing.workspace_id !== '') {
    noteWorkspaceBackend(existing.workspace_id, typeof existing.backend === 'string' ? existing.backend : 'fly');
    return { workspaceId: existing.workspace_id, created: false };
  }

  const created = await createWorkspace(repoURL, choice);
  const workspaceId = created.workspace_id as string;
  noteWorkspaceBackend(workspaceId, typeof created.backend === 'string' ? created.backend : 'fly');
  return { workspaceId, created: true };
}

// ── Backend-aware txn addressing (SP-BUILDER-12) ────────────────────────────
//
// The txn lifecycle lives at /workspace/txn/{ws}/txn/... for every backend —
// the platform dispatches per workspace (runner-hosted workspaces get the
// bearer-proxied path, Fly keeps its billing semantics). These helpers only
// exist so in-flight escalations that resolved a workspace under the old
// /workspace/fly contract keep working: anything without a recorded backend
// still addresses /workspace/fly.

const txnBackends = new Map<string, string>();

function noteWorkspaceBackend(workspaceId: string, backend: string): void {
  txnBackends.set(workspaceId, backend === 'runner' ? 'txn' : 'fly');
}

function txnBase(workspaceId: string): string {
  return txnBackends.get(workspaceId) ?? 'fly';
}

// ── Txn lifecycle ───────────────────────────────────────────────────────────

function txnURL(workspaceId: string, txnId: string, suffix = ''): string {
  return `/workspace/${txnBase(workspaceId)}/${encodeURIComponent(workspaceId)}/txn/${encodeURIComponent(txnId)}${suffix}`;
}

async function postJSON<T>(url: string, action: string, body?: unknown, timeoutMs?: number): Promise<T> {
  const init: RequestInit = {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(body ?? {}),
  };
  const res = timeoutMs ? await fetchWithTimeout(url, init, timeoutMs) : await fetch(url, init);
  if (!res.ok) throw await toTxnError(res, action);
  return (await res.json()) as T;
}

/** Open a transaction. 409 when one is already open for the workspace. */
export async function createTxn(workspaceId: string): Promise<{ txn_id: string; status: string; expires_at?: string }> {
  if (typeof workspaceId !== 'string' || workspaceId === '') throw new TypeError('workspaceId is required');
  const res = await postJSON<{ txn_id?: string; status?: string; expires_at?: string }>(
    `/workspace/${txnBase(workspaceId)}/${encodeURIComponent(workspaceId)}/txn`,
    'Cloud txn open',
    {},
    TXN_OPEN_TIMEOUT_MS,
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
