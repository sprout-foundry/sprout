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
  resolveTxnWorkspace,
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
  if (phase === 'opening' && host.kind === 'runner') return `Starting workspace on ${hostDisplayName(host)}`;
  return TXN_PHASE_LABELS[phase] ?? phase;
}

/**
 * The chosen runner could not take the workspace (offline, busy or no
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
  const where = onRunner ? `the workspace on ${hostDisplayName(host)}` : 'the cloud workspace';
  const action =
    phase === 'error' ? (onRunner ? 'Runner run' : 'Cloud container run') : `${txnPhaseLabel(phase, host)} failed`;
  if (err instanceof CloudTxnError) {
    // The platform says so when the workspace runs an outdated sprout
    // (409 workspace_outdated) or the deployment has no workspace compute
    // (503) — both need action, not a retry, so pass its wording through.
    if (err.status === 409 && /older version/i.test(detail)) return capitalize(detail);
    if (err.status === 409) return `Another command is already running in ${where} — try again shortly.`;
    if (err.status === 402) return `Not enough credits: ${detail}`;
    if (err.status === 503 && /not available on this deployment/i.test(detail)) {
      return "Cloud workspaces aren't enabled on this deployment.";
    }
    if (err.status === 502 || err.status === 503) {
      const subject = onRunner ? `The workspace on ${hostDisplayName(host)}` : 'Cloud workspace';
      return `${subject} is unavailable right now — try again shortly.`;
    }
  }
  return `${action}: ${detail}`;
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * Enumerate what the push phase should carry:
 *
 *   - dirty/untracked files (plus deleted paths) when browser git sees
 *     changes — the container's clone already holds everything else;
 *   - ALL VFS files when git has no commits (never cloned/pushed — nothing
 *     else can tell the container what the browser holds);
 *   - nothing when the repo is clean (the clone matches HEAD).
 *
 * A broken git state falls back to "push everything" rather than silently
 * pushing nothing.
 */
export async function collectTxnPushFiles(): Promise<{ inputs: TxnPushInput[]; deletes: string[] }> {
  const { getBrowserGitVfsBridge, gitLog, gitStatus } = await import('./browserGit');
  const bridge = getBrowserGitVfsBridge();
  if (!bridge) return { inputs: [], deletes: [] };

  const all = await bridge.readVfsFiles();
  try {
    const status = await gitStatus();
    const dirty = new Set<string>();
    const deletes: string[] = [];
    for (const file of [...status.staged, ...status.unstaged]) {
      if (file.status === 'deleted') deletes.push(file.path);
      else dirty.add(file.path);
    }
    if (dirty.size > 0 || deletes.length > 0) {
      return { inputs: all.filter((f) => dirty.has(f.path)), deletes };
    }
    const commits = await gitLog(1);
    return { inputs: commits.length > 0 ? [] : all, deletes: [] };
  } catch {
    // best-effort: when status can't be read, send everything.
    return { inputs: all, deletes: [] };
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

/** Outcome of one command run transactionally in a workspace. */
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
 * Run one command in the user's workspace for repoURL on the chosen host
 * (a runner, the cloud, or "auto"): open → push browser deltas → run → pull
 * deltas back into the VFS → finish. `finish` always runs once a txn is open
 * (success, failure or timeout) so the pay-per-run machine is never left
 * running. Throws HostUnavailableError when a chosen runner can't take the
 * workspace, else an Error with a phase-aware message (describeTxnError).
 */
export async function runTxnCommand(
  repoURL: string,
  command: string,
  onPhase?: (phase: string) => void,
  host: EscalationHost = AUTO_HOST,
): Promise<TxnCommandOutcome> {
  let workspaceId = '';
  let txnId = '';
  let finished = false;
  let phase = 'opening';
  onPhase?.(phase);
  try {
    const resolved = await resolveTxnWorkspace(repoURL, toTxnHostChoice(host));
    workspaceId = resolved.workspaceId;
    const opened = await createTxn(workspaceId);
    txnId = opened.txn_id;

    phase = 'pushing';
    onPhase?.(phase);
    const { inputs, deletes } = await collectTxnPushFiles();
    const manifest = await buildPushManifest(() => inputs, { deletes });
    const pushedFiles = manifest.files.length;
    await txnPush(workspaceId, txnId, manifest);

    phase = 'running';
    onPhase?.(phase);
    const result = await txnRun(workspaceId, txnId, command, TXN_RUN_TIMEOUT_SECONDS);

    phase = 'pulling';
    onPhase?.(phase);
    const pulled = await txnPull(workspaceId, txnId);
    const applied = await applyPullManifest(pulled, await txnPullIO());

    let warning: string | undefined;
    try {
      await txnFinish(workspaceId, txnId);
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
        await txnFinish(workspaceId, txnId);
      } catch {
        // The thrown error already names the side that failed.
      }
    }
  }
}
