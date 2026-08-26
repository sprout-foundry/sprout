/**
 * EscalationListener — shown when the user hits a browser limitation in cloud
 * mode (git push, VFS quota, command timeout). Offers two ways out:
 *
 *   Mode B — "Start Full Workspace": provision a full cloud workspace and
 *            navigate to it (the original escalation path).
 *   Mode A — "Run as cloud task": submit the work to the platform task queue
 *            (POST /api/tasks) and poll it to completion inline, without the
 *            user leaving the browser IDE.
 *
 * Listens for sprout:escalation-trigger events with severity='blocking'.
 */

import { Cloud, Loader2, Rocket, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import { ESCALATION_TRIGGER_EVENT } from '../hooks/useEscalationTriggers';
import { pollCloudTask, submitCloudTask, type CloudTask } from '../services/cloudTasks';
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
        // A new blocking limit replaces any previous cloud-task view — the
        // old task stays reachable from the platform's /tasks page.
        setCloudTask(null);
        setSubmitting(false);
        setEscalation({ trigger: detail, visible: true });
      }
    };
    window.addEventListener(ESCALATION_TRIGGER_EVENT, handler);
    return () => window.removeEventListener(ESCALATION_TRIGGER_EVENT, handler);
  }, []);

  const handleDismiss = useCallback(() => {
    // The poll loop may still be in flight; it writes through the mounted
    // guard and the toast simply stops rendering.
    setEscalation(null);
    setCloudTask(null);
    setSubmitting(false);
  }, []);

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
  // Shown only when there is repo context (same gate the Mode B path uses for
  // a meaningful task) and no cloud task is already in flight for this toast.
  const showCloudTaskButton = Boolean(repoURL) && !cloudTask;

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
