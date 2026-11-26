/**
 * cloudTxnEscalate.ts — the browser-side glue for the ETH-2 escalation
 * action ("Run in cloud container").
 *
 * Sits between the toast (EscalationListener) and the txn client
 * (cloudTxn.ts): enumerates what the push phase should carry (browser git's
 * dirty/untracked files, or the whole VFS when git has nothing to compare
 * against), adapts the pulled manifest back onto the string-backed WASM VFS,
 * and turns platform failures into friendly, phase-aware messages.
 *
 * browserGit is imported dynamically so local-mode bundles never pull
 * isomorphic-git in for a cloud-only code path (same reason
 * useAppInitialization imports it lazily).
 */

import type { TxnPullIO, TxnPushInput, TxnRunResult } from './cloudTxn';
import {
  applyPullManifest,
  buildPushManifest,
  CloudTxnError,
  createTxn,
  resolveTxnDevSpace,
  RunnerUnavailableError,
  txnFinish,
  txnPull,
  txnPush,
  txnRun,
  TXN_RUN_TIMEOUT_SECONDS,
} from './cloudTxn';
import { AUTO_HOST, hostDisplayName, toTxnHostChoice, type EscalationHost } from './escalationHost';

/** Inline view state while an ETH-2 transaction runs. */
export interface TxnProgress {
  /** 'opening' | 'pushing' | 'running' | 'pulling' | 'done' | 'error' */
  phase: string;
  error?: string;
  /** Non-fatal follow-up problem (e.g. the finish call failed). */
  warning?: string;
  result?: TxnRunResult;
  pulledFiles?: number;
  skippedFiles?: number;
  /** Files the push manifest carried (0 = the container ran without the browser's edits). */
  pushedFiles?: number;
}

const TXN_PHASE_LABELS: Record<string, string> = {
  opening: 'Starting cloud container',
  pushing: 'Pushing your files',
  running: 'Running command',
  pulling: 'Pulling results back',
};

/**
 * Human label for a txn phase, used in both the status line and errors. On a
 * runner the "opening" phase names the runner instead of the cloud container.
 */
export function txnPhaseLabel(phase: string, host: EscalationHost = AUTO_HOST): string {
  if (phase === 'opening' && host.kind === 'runner') return `Starting dev space on ${hostDisplayName(host)}`;
  return TXN_PHASE_LABELS[phase] ?? phase;
}

/**
 * The chosen runner could not take the dev space (offline, busy or no
 * longer the user's). Kept distinct from other failures so the prompt can
 * offer the cloud instead.
 */
export class HostUnavailableError extends Error {
  readonly runnerId: string;

  constructor(host: Extract<EscalationHost, { kind: 'runner' }>) {
    super(`${hostDisplayName(host)} is offline or busy.`);
    this.name = 'HostUnavailableError';
    this.runnerId = host.runnerId;
  }
}

/**
 * Turn a txn failure into a friendly, phase-aware message. The platform's own
 * error body wins for 402 (credits); 409/502/503 get fixed wording so the
 * user knows what to do next instead of reading a raw gateway error.
 */
export function describeTxnError(err: unknown, phase: string, host: EscalationHost = AUTO_HOST): string {
  const detail = err instanceof Error && err.message ? err.message : err ? String(err) : 'unknown error';
  const onRunner = host.kind === 'runner';
  const where = onRunner ? `the dev space on ${hostDisplayName(host)}` : 'the cloud dev space';
  const action =
    phase === 'error' ? (onRunner ? 'Runner run' : 'Cloud container run') : `${txnPhaseLabel(phase, host)} failed`;
  if (err instanceof CloudTxnError) {
    // The platform says so when the dev space runs an outdated sprout
    // (409 dev_space_outdated) or the deployment has no dev space compute
    // (503) — both need action, not a retry, so pass its wording through.
    if (err.status === 409 && /older version/i.test(detail)) return capitalize(detail);
    if (err.status === 409) return `Another command is already running in ${where} — try again shortly.`;
    if (err.status === 402) return `Not enough credits: ${detail}`;
    if (err.status === 503 && /not available on this deployment/i.test(detail)) {
      return "Dev spaces aren't enabled on this deployment.";
    }
    if (err.status === 502 || err.status === 503) {
      const subject = onRunner ? `The dev space on ${hostDisplayName(host)}` : 'The cloud dev space';
      return `${subject} is unavailable right now — try again shortly.`;
    }
  }
  return `${action}: ${detail}`;
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

const SYNCED_COMMIT_KEY = 'sprout.txnSyncedCommit:';

/** The browser commit a dev space last received from this browser, if any. */
function readSyncedCommit(devSpaceId: string): string | null {
  try {
    return window.localStorage.getItem(SYNCED_COMMIT_KEY + devSpaceId);
  } catch {
    return null;
  }
}

function writeSyncedCommit(devSpaceId: string, oid: string): void {
  try {
    window.localStorage.setItem(SYNCED_COMMIT_KEY + devSpaceId, oid);
  } catch {
    // Storage unavailable: the next push measures from the remote's copy.
  }
}

/**
 * Enumerate what the push phase should carry: the browser's uncommitted
 * changes, plus every file its commits changed since the dev space last
 * received them. That base is the commit this browser last pushed to the
 * dev space, else the branch's origin copy (what a fresh dev space clones).
 * With no base at all (never pushed, no origin copy) everything is sent, and
 * a broken git state also falls back to everything rather than nothing.
 */
export async function collectTxnPushFiles(
  devSpaceId?: string,
): Promise<{ inputs: TxnPushInput[]; deletes: string[]; headOid: string | null }> {
  const { getBrowserGitVfsBridge, gitChangedFilesBetween, gitHeadOid, gitRemoteTrackingOid, gitStatus } =
    await import('./browserGit');
  const bridge = getBrowserGitVfsBridge();
  if (!bridge) return { inputs: [], deletes: [], headOid: null };

  const all = await bridge.readVfsFiles();
  try {
    const headOid = await gitHeadOid();
    const send = new Set<string>();
    const removed = new Set<string>();
    if (headOid) {
      const base = (devSpaceId ? readSyncedCommit(devSpaceId) : null) ?? (await gitRemoteTrackingOid());
      if (!base) return { inputs: all, deletes: [], headOid };
      if (base !== headOid) {
        const committed = await gitChangedFilesBetween(base, headOid);
        committed.changed.forEach((p) => send.add(p));
        committed.removed.forEach((p) => removed.add(p));
      }
    } else {
      // No commits: nothing else can tell the dev space what the browser holds.
      return { inputs: all, deletes: [], headOid };
    }
    const status = await gitStatus();
    for (const file of [...status.staged, ...status.unstaged]) {
      if (file.status === 'deleted') removed.add(file.path);
      else send.add(file.path);
    }
    const present = new Set(all.map((f) => f.path));
    return {
      inputs: all.filter((f) => send.has(f.path)),
      deletes: [...removed].filter((p) => !present.has(p)),
      headOid,
    };
  } catch {
    // best-effort: when git can't be read, send everything.
    return { inputs: all, deletes: [], headOid: null };
  }
}

/**
 * The VFS side of the pull phase: decode each file (binary as UTF-8 — the
 * WASM VFS is string-backed) and write through the browser-git bridge,
 * applying deletes when the bridge supports them. With no bridge configured
 * the writes become a no-op so the run result still renders.
 */
export async function txnPullIO(): Promise<TxnPullIO> {
  const { getBrowserGitVfsBridge } = await import('./browserGit');
  const bridge = getBrowserGitVfsBridge();
  if (!bridge) {
    return { writeFiles: async () => undefined };
  }
  const decoder = new TextDecoder();
  return {
    writeFiles: async (files) => {
      await bridge.writeVfsFiles(
        files.map((f) => ({
          path: f.path,
          content: typeof f.content === 'string' ? f.content : decoder.decode(f.content),
        })),
      );
    },
    deleteFiles: bridge.deleteVfsFiles,
  };
}

/** Outcome of one command run transactionally in a dev space. */
export interface TxnCommandOutcome {
  result: TxnRunResult;
  pulledFiles: number;
  skippedFiles: number;
  /** Files the push manifest carried (0 = the container ran without the browser's edits). */
  pushedFiles: number;
  /** Non-fatal follow-up problem (e.g. the machine-stop call failed). */
  warning?: string;
}

/**
 * Run one command in the user's dev space for repoURL on the chosen host
 * (a runner, the cloud, or "auto"): open → push browser deltas → run → pull
 * deltas back into the VFS → finish. `finish` always runs once a txn is open
 * (success, failure or timeout) so the pay-per-run machine is never left
 * running. Throws HostUnavailableError when a chosen runner can't take the
 * dev space, else an Error with a phase-aware message (describeTxnError).
 */
export async function runTxnCommand(
  repoURL: string,
  command: string,
  onPhase?: (phase: string) => void,
  host: EscalationHost = AUTO_HOST,
): Promise<TxnCommandOutcome> {
  let devSpaceId = '';
  let txnId = '';
  let finished = false;
  let phase = 'opening';
  onPhase?.(phase);
  try {
    const resolved = await resolveTxnDevSpace(repoURL, toTxnHostChoice(host));
    devSpaceId = resolved.devSpaceId;
    const opened = await createTxn(devSpaceId);
    txnId = opened.txn_id;

    phase = 'pushing';
    onPhase?.(phase);
    const { inputs, deletes, headOid } = await collectTxnPushFiles(devSpaceId);
    const manifest = await buildPushManifest(() => inputs, { deletes });
    const pushedFiles = manifest.files.length;
    await txnPush(devSpaceId, txnId, manifest);

    phase = 'running';
    onPhase?.(phase);
    const result = await txnRun(devSpaceId, txnId, command, TXN_RUN_TIMEOUT_SECONDS);

    phase = 'pulling';
    onPhase?.(phase);
    const pulled = await txnPull(devSpaceId, txnId);
    const applied = await applyPullManifest(pulled, await txnPullIO());
    if (headOid) writeSyncedCommit(devSpaceId, headOid);

    let warning: string | undefined;
    try {
      await txnFinish(devSpaceId, txnId);
      finished = true;
    } catch (err) {
      const what = host.kind === 'runner' ? 'Closing the runner transaction' : 'Cloud container stop';
      warning = `${what} failed — it will idle out on its own. ${err instanceof Error ? err.message : String(err)}`;
    }
    // Terminal phase so a status UI always learns the run is over — including
    // runs that never reach "pulling" (an error at open/push/run).
    onPhase?.('done');
    return {
      result,
      pulledFiles: applied.applied,
      skippedFiles: applied.skipped.length + pulled.skipped.length,
      pushedFiles,
      warning,
    };
  } catch (err) {
    // Report the failure phase first, then the terminal "error" phase, so the
    // status UI clears instead of sticking on the last progress phase.
    onPhase?.('error');
    if (err instanceof RunnerUnavailableError && host.kind === 'runner') throw new HostUnavailableError(host);
    throw new Error(describeTxnError(err, phase, host));
  } finally {
    if (txnId !== '' && !finished) {
      try {
        await txnFinish(devSpaceId, txnId);
      } catch {
        // The thrown error already names the side that failed.
      }
    }
  }
}
