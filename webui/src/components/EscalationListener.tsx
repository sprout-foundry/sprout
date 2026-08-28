/**
 * EscalationListener — shown when the user hits a browser limitation in cloud
 * mode (git push, VFS quota, command timeout, exit 127). Offers three ways
 * out, in priority order:
 *
 *   Txn  — "Run in cloud container" (ETH-2, primary): open a transaction on
 *          the user's fly workspace, push the browser's dirty files, run the
 *          exact command that returned 127 in the container, pull the
 *          resulting deltas back into the browser VFS, finish. The user
 *          never leaves the IDE and pays only for the machine time used.
 *   Mode A — "Run as cloud task": submit the work to the platform task queue
 *          (POST /api/tasks) and poll it to completion inline.
 *   Mode B — "Start Full Workspace": provision a full cloud workspace and
 *          navigate to it (the original escalation path).
 *
 * Listens for sprout:escalation-trigger events with severity='blocking'.
 */

import { Cloud, Container, Loader2, Rocket, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import { ESCALATION_TRIGGER_EVENT } from '../hooks/useEscalationTriggers';
import { pollCloudTask, submitCloudTask, type CloudTask } from '../services/cloudTasks';
import {
  applyPullManifest,
  buildPushManifest,
  createTxn,
  resolveTxnWorkspace,
  txnFinish,
  txnPull,
  txnPush,
  txnRun,
  TXN_RUN_TIMEOUT_SECONDS,
} from '../services/cloudTxn';
import {
  collectTxnPushFiles,
  describeTxnError,
  txnPhaseLabel,
  txnPullIO,
  type TxnProgress,
} from '../services/cloudTxnEscalate';
import './EscalationToast.css';

interface EscalationState {
  trigger: EscalationTriggerEvent;
  visible: boolean;
}

/** Poll cadence for the inline cloud-task progress view. */
const CLOUD_TASK_POLL_INTERVAL_MS = 3000;

/**
 * Sensible default prompt for a task spawned from an escalation toast: the
 * user just hit a browser limit mid-work, so the platform agent should pick up
 * where the browser left off rather than start from a blank slate.
 */
export const ESCALATION_CLOUD_TASK_PROMPT = 'Continue building this repository.';

/**
 * Build the prompt submitted with an escalation-spawned cloud task. Including
 * the trigger's `reason` gives the platform agent the context of *why* the
 * work was handed off (git push failed, VFS quota, command timeout, ...).
 */
export function deriveEscalationPrompt(trigger: EscalationTriggerEvent): string {
  const reason = typeof trigger.reason === 'string' ? trigger.reason.trim() : '';
  if (!reason) return ESCALATION_CLOUD_TASK_PROMPT;
  return `${ESCALATION_CLOUD_TASK_PROMPT} Escalation reason: ${reason}.`;
}

/** Inline progress view state while a cloud task runs. */
interface CloudTaskProgress {
  taskId: string;
  status: string;
  /** Set once the poll loop reports an error or times out. */
  error?: string;
}

export function EscalationListener() {
  const [escalation, setEscalation] = useState<EscalationState | null>(null);
  const [cloudTask, setCloudTask] = useState<CloudTaskProgress | null>(null);
  const [txn, setTxn] = useState<TxnProgress | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<EscalationTriggerEvent>).detail;
      if (detail?.severity === 'blocking') {
        // A new blocking limit replaces any previous cloud-task/txn view —
        // the old task stays reachable from the platform's /tasks page.
        setCloudTask(null);
        setTxn(null);
        setSubmitting(false);
        setEscalation({ trigger: detail, visible: true });
      }
    };
    window.addEventListener(ESCALATION_TRIGGER_EVENT, handler);
    return () => window.removeEventListener(ESCALATION_TRIGGER_EVENT, handler);
  }, []);

  const handleDismiss = useCallback(() => {
    // The poll/txn loops may still be in flight; they write through the
    // mounted guard and the toast simply stops rendering.
    setEscalation(null);
    setCloudTask(null);
    setTxn(null);
    setSubmitting(false);
  }, []);

  /**
   * ETH-2: run the triggering command transactionally in the user's cloud
   * workspace container — open → push browser deltas → run → pull container
   * deltas back into the VFS → finish. `finish` is ALWAYS called (success,
   * failure or timeout) so the pay-per-run machine is never left running:
   * the finally block is the guarantee, the in-flow call just avoids an
   * extra round trip on the happy path.
   */
  const handleRunTxn = useCallback(() => {
    const trigger = escalation?.trigger;
    const command = typeof trigger?.command === 'string' ? trigger.command.trim() : '';
    const repoURL = trigger?.repoURL;
    if (!command || !repoURL || submitting || txn) return;

    setSubmitting(true);
    setTxn({ phase: 'opening' });

    const run = async (): Promise<void> => {
      let workspaceId = '';
      let txnId = '';
      let finished = false;
      let phase = 'opening';
      try {
        const resolved = await resolveTxnWorkspace(repoURL);
        workspaceId = resolved.workspaceId;
        const opened = await createTxn(workspaceId);
        txnId = opened.txn_id;

        phase = 'pushing';
        if (mountedRef.current) setTxn({ phase });
        const { inputs, deletes } = await collectTxnPushFiles();
        const manifest = await buildPushManifest(() => inputs, { deletes });
        await txnPush(workspaceId, txnId, manifest);

        phase = 'running';
        if (mountedRef.current) setTxn({ phase });
        const result = await txnRun(workspaceId, txnId, command, TXN_RUN_TIMEOUT_SECONDS);

        phase = 'pulling';
        if (mountedRef.current) setTxn({ phase });
        const pulled = await txnPull(workspaceId, txnId);
        const applied = await applyPullManifest(pulled, await txnPullIO());
        const skippedFiles = applied.skipped.length + pulled.skipped.length;

        let warning: string | undefined;
        try {
          await txnFinish(workspaceId, txnId);
          finished = true;
        } catch (err) {
          warning = `Cloud container stop failed — it will idle out on its own. ${
            err instanceof Error ? err.message : String(err)
          }`;
        }

        phase = 'done';
        if (mountedRef.current) {
          setTxn({ phase, result, pulledFiles: applied.applied, skippedFiles, warning });
        }
      } catch (err) {
        if (mountedRef.current) setTxn({ phase: 'error', error: describeTxnError(err, phase) });
      } finally {
        // The machine-stop guarantee: finish even when a phase failed, unless
        // the in-flow finish already ran (or no txn was ever opened).
        if (txnId !== '' && !finished) {
          try {
            await txnFinish(workspaceId, txnId);
          } catch {
            // A failure here can only add noise — the error/warning above
            // already names the side that failed.
          }
        }
        if (mountedRef.current) setSubmitting(false);
      }
    };

    run();
  }, [escalation, submitting, txn]);

  /**
   * Mode A: submit the escalation as a platform task, then poll it inline.
   * Status is rendered from the submit response first (usually 'pending') so
   * the user sees progress before the first poll tick.
   */
  const handleRunCloudTask = useCallback(() => {
    const trigger = escalation?.trigger;
    const repoURL = trigger?.repoURL;
    if (!repoURL || submitting || cloudTask) return;

    setSubmitting(true);
    submitCloudTask({
      repo_url: repoURL,
      prompt: deriveEscalationPrompt(trigger),
    })
      .then(({ task }: { task: CloudTask }) => {
        if (!mountedRef.current) return;
        setCloudTask({ taskId: task.task_id, status: task.status || 'pending' });
        return pollCloudTask(task.task_id, {
          intervalMs: CLOUD_TASK_POLL_INTERVAL_MS,
          onTick: (t: CloudTask) => {
            if (!mountedRef.current) return;
            setCloudTask((prev) =>
              prev && prev.taskId === task.task_id ? { ...prev, status: t.status, error: prev.error } : prev,
            );
          },
        }).catch((err: unknown) => {
          // Rejected only on timeout or a failed status request; either way
          // the task itself still exists and stays linkable.
          if (!mountedRef.current) return;
          const message = err instanceof Error && err.message ? err.message : 'Cloud task status polling failed';
          setCloudTask((prev) => (prev && prev.taskId === task.task_id ? { ...prev, error: message } : prev));
        });
      })
      .catch((err: unknown) => {
        if (!mountedRef.current) return;
        const message = err instanceof Error && err.message ? err.message : String(err);
        // Submit failed — no task id yet, so only the error renders.
        setCloudTask({ taskId: '', status: '', error: message });
      })
      .finally(() => {
        if (mountedRef.current) setSubmitting(false);
      });
  }, [escalation, submitting, cloudTask]);

  const handleStartWorkspace = useCallback(() => {
    const trigger = escalation?.trigger;
    setEscalation(null);
    if (trigger?.repoURL) {
      // Route through the platform workspace creation API
      fetch('/workspace/fly', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          repo_url: trigger.repoURL,
          mode: 'build',
        }),
      })
        .then((res) => {
          if (res.ok) {
            return res.json().then((data) => {
              if (data?.workspace_url) {
                window.location.href = data.workspace_url;
              }
            });
          }
        })
        .catch(() => {
          // If workspace creation fails, redirect to dashboard
          window.location.href = '/';
        });
    } else {
      // No repo context — go to dashboard
      window.location.href = '/';
    }
  }, [escalation]);

  if (!escalation?.visible) return null;

  const repoURL = escalation.trigger.repoURL;
  const command = typeof escalation.trigger.command === 'string' ? escalation.trigger.command.trim() : '';
  // The txn CTA needs both the command (what to run) and repo context (where
  // to run it); without either the toast falls back to Mode A / Mode B.
  const showTxnButton = command !== '' && Boolean(repoURL) && !txn && !cloudTask;
  // Shown only when there is repo context (same gate the Mode B path uses for
  // a meaningful task) and no txn/cloud task is already in flight.
  const showCloudTaskButton = Boolean(repoURL) && !cloudTask && !txn;

  return (
    <div className="escalation-toast-overlay">
      <div className="escalation-toast">
        <button className="escalation-toast-close" onClick={handleDismiss} aria-label="Dismiss">
          <X size={16} />
        </button>
        <div className="escalation-toast-body">
          <div className="escalation-toast-icon">
            <Rocket size={24} />
          </div>
          <div className="escalation-toast-content">
            <h3 className="escalation-toast-title">Browser limitation reached</h3>
            <p className="escalation-toast-message">{escalation.trigger.message}</p>

            {txn ? (
              <div className="escalation-toast-progress" data-testid="escalation-toast-txn-progress">
                {txn.error ? (
                  <p className="escalation-toast-task-error" data-testid="escalation-toast-txn-error">
                    {txn.error}
                  </p>
                ) : txn.result ? (
                  <div data-testid="escalation-toast-txn-result">
                    <p className="escalation-toast-txn-summary">
                      <span
                        className={
                          txn.result.exit_code === 0 ? 'escalation-toast-txn-exit is-ok' : 'escalation-toast-txn-exit'
                        }
                      >
                        exit {txn.result.exit_code}
                      </span>
                      <span className="escalation-toast-txn-duration">
                        {(txn.result.duration_ms / 1000).toFixed(1)}s
                      </span>
                      {txn.result.timed_out ? <span className="escalation-toast-txn-flag">timed out</span> : null}
                    </p>
                    {txn.result.stdout ? <pre className="escalation-toast-txn-pre">{txn.result.stdout}</pre> : null}
                    {txn.result.stderr ? (
                      <pre className="escalation-toast-txn-pre is-stderr">{txn.result.stderr}</pre>
                    ) : null}
                    <p className="escalation-toast-task-status" data-testid="escalation-toast-txn-pulled">
                      {txn.pulledFiles ?? 0} file{txn.pulledFiles === 1 ? '' : 's'} pulled back
                    </p>
                    {txn.skippedFiles ? (
                      <p className="escalation-toast-task-status" data-testid="escalation-toast-txn-skipped">
                        {txn.skippedFiles} file{txn.skippedFiles === 1 ? '' : 's'} skipped (over transfer caps)
                      </p>
                    ) : null}
                    {txn.warning ? (
                      <p className="escalation-toast-task-error" data-testid="escalation-toast-txn-warning">
                        {txn.warning}
                      </p>
                    ) : null}
                  </div>
                ) : (
                  <p className="escalation-toast-task-status" data-testid="escalation-toast-txn-status">
                    <Loader2 size={13} className="spinner" aria-hidden="true" />
                    {txnPhaseLabel(txn.phase)}…
                  </p>
                )}
              </div>
            ) : null}

            {cloudTask ? (
              <div className="escalation-toast-progress">
                {cloudTask.error ? (
                  <p className="escalation-toast-task-error" data-testid="escalation-toast-cloud-task-error">
                    {cloudTask.error}
                  </p>
                ) : (
                  <p className="escalation-toast-task-status" data-testid="escalation-toast-cloud-task-status">
                    <Loader2 size={13} className="spinner" aria-hidden="true" />
                    Cloud task {cloudTask.status || 'pending'}
                  </p>
                )}
                {cloudTask.taskId ? (
                  <a
                    className="escalation-toast-task-link"
                    href={'/tasks/' + cloudTask.taskId}
                    data-testid="escalation-toast-cloud-task-link"
                  >
                    View task on platform
                  </a>
                ) : null}
              </div>
            ) : null}

            <div className="escalation-toast-actions">
              {showTxnButton ? (
                <button
                  className="escalation-toast-action"
                  onClick={handleRunTxn}
                  disabled={submitting}
                  data-testid="escalation-toast-txn"
                >
                  {submitting ? <Loader2 size={14} className="spinner" aria-hidden="true" /> : <Container size={14} />}
                  Run in cloud container
                </button>
              ) : null}
              {showCloudTaskButton ? (
                <button
                  className="escalation-toast-action escalation-toast-action-secondary"
                  onClick={handleRunCloudTask}
                  disabled={submitting}
                  data-testid="escalation-toast-cloud-task"
                >
                  {submitting ? <Loader2 size={14} className="spinner" aria-hidden="true" /> : <Cloud size={14} />}
                  Run as cloud task
                </button>
              ) : null}
              <button className="escalation-toast-action" onClick={handleStartWorkspace}>
                <Rocket size={14} />
                Start Full Workspace
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
