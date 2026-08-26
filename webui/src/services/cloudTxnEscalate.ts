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
import { CloudTxnError } from './cloudTxn';

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
}

const TXN_PHASE_LABELS: Record<string, string> = {
  opening: 'Starting cloud container',
  pushing: 'Pushing your files',
  running: 'Running command',
  pulling: 'Pulling results back',
};

/** Human label for a txn phase, used in both the status line and errors. */
export function txnPhaseLabel(phase: string): string {
  return TXN_PHASE_LABELS[phase] ?? phase;
}

/**
 * Turn a txn failure into a friendly, phase-aware message. The platform's own
 * error body wins for 402 (credits); 409/502/503 get fixed wording so the
 * user knows what to do next instead of reading a raw gateway error.
 */
export function describeTxnError(err: unknown, phase: string): string {
  const detail = err instanceof Error && err.message ? err.message : err ? String(err) : 'unknown error';
  const action = phase === 'error' ? 'Cloud container run' : `${txnPhaseLabel(phase)} failed`;
  if (err instanceof CloudTxnError) {
    if (err.status === 409) return 'another transaction is running, try again shortly';
    if (err.status === 402) return `Not enough credits: ${detail}`;
    if (err.status === 502 || err.status === 503) {
      return 'Cloud workspace is unavailable right now — try again shortly.';
    }
  }
  return `${action}: ${detail}`;
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
