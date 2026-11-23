/**
 * EscalationListener — shown when the user hits a browser limitation in cloud
 * mode (git push, VFS quota, command timeout, exit 127). Offers three ways
 * out, in priority order:
 *
 *   Txn  — "Run in cloud container" / "Run on <runner>" (ETH-2, primary):
 *          open a transaction on the user's dev space on the chosen host (one
 *          of their runners, or the cloud), push the browser's dirty files,
 *          run the exact command that returned 127 there, pull the resulting
 *          deltas back into the browser VFS, finish. The user never leaves
 *          the IDE; cloud runs pay only for the machine time used.
 *   Mode A — "Run as cloud task": submit the work to the platform task queue
 *          (POST /api/tasks) and poll it to completion inline.
 *   Mode B — "Start a Dev Space": provision a cloud dev space and
 *          navigate to it (the original escalation path).
 *
 * Listens for sprout:escalation-trigger events with severity='blocking'.
 */

import { Cloud, Container, Loader2, Rocket, Server, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useHost } from '../host/useHost';
import type { EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import { ESCALATION_TRIGGER_EVENT } from '../hooks/useEscalationTriggers';
import { useRunHostChoice } from '../hooks/useRunHostChoice';
import { isTerminalCloudTaskStatus, pollCloudTask, submitCloudTask, type CloudTask } from '../services/cloudTasks';
import {
  collectTxnPushFiles,
  HostUnavailableError,
  runTxnCommand,
  txnPhaseLabel,
  type TxnProgress,
} from '../services/cloudTxnEscalate';
import { CLOUD_HOST, hostDisplayName, rememberHost, type EscalationHost } from '../services/escalationHost';
import { startDevSpace, useDevSpacesAvailable } from '../services/cloudDevSpace';
import { onPlatformLinkClick, openPlatformPage } from '../services/homeView';
import { RunHostPicker } from './RunHostPicker';
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
  const command = typeof trigger.command === 'string' ? trigger.command.trim() : '';
  const context = ESCALATION_REASON_CONTEXT[reason];
  const parts = [ESCALATION_CLOUD_TASK_PROMPT];
  if (context) parts.push(context);
  if (command) parts.push(`Run \`${command}\` and fix any problems it reports.`);
  return parts.join(' ');
}

/** Plain-language context for each escalation trigger, for the platform agent. */
const ESCALATION_REASON_CONTEXT: Record<string, string> = {
  git_push_failed: 'Pushing from the browser failed; commit and push the current changes.',
  vfs_quota_exceeded: 'The browser ran out of storage for this project.',
  command_timeout: 'A command timed out in the browser shell.',
  command_unavailable_in_browser: "The browser shell couldn't run a command this work needs.",
  build_command_detected: 'The next step needs a full build environment.',
};

/** Inline progress view state while a cloud task runs. */
interface CloudTaskProgress {
  taskId: string;
  status: string;
  /** Set once the poll loop reports an error or times out. */
  error?: string;
}

export function EscalationListener() {
  const { navigation } = useHost();
  const [escalation, setEscalation] = useState<EscalationState | null>(null);
  const [cloudTask, setCloudTask] = useState<CloudTaskProgress | null>(null);
  const [txn, setTxn] = useState<(TxnProgress & { host: EscalationHost; offerCloud?: boolean }) | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [startingDevSpace, setStartingDevSpace] = useState(false);
  const [devSpaceError, setDevSpaceError] = useState<string | null>(null);
  // The confirmation step: opened by the txn CTA, closed by confirming or
  // cancelling. The command reaches a remote machine only from there.
  const [confirmTxn, setConfirmTxn] = useState(false);
  // Mode A's own confirmation step, opened by the cloud-task CTA. Until it is
  // confirmed nothing is submitted to the platform.
  const [confirmCloudTask, setConfirmCloudTask] = useState(false);
  // The prompt the user will submit, seeded from the trigger when the step
  // opens and then owned by the textarea.
  const [cloudTaskPrompt, setCloudTaskPrompt] = useState('');
  // What the push phase would carry, counted when the step opens (null =
  // still counting / not countable).
  const [pendingPushCount, setPendingPushCount] = useState<number | null>(null);
  const devSpacesAvailable = useDevSpacesAvailable();
  const mountedRef = useRef(true);
  // Bumped whenever the toast is dismissed or a new trigger replaces the
  // current one. Async txn/cloud-task callbacks capture the value at start
  // and bail when it has moved on — otherwise dismissing the toast mid-run
  // would be undone by the next phase update, resurrecting it.
  const epochRef = useRef(0);
  const triggerCommand = typeof escalation?.trigger.command === 'string' ? escalation.trigger.command.trim() : '';
  const canRunTxn = triggerCommand !== '' && Boolean(escalation?.trigger.repoURL);
  const hostChoice = useRunHostChoice(escalation?.trigger.repoURL, canRunTxn && escalation ? escalation : null);

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
        // the old task stays reachable from the platform's /tasks page. Its
        // in-flight callbacks are invalidated by the epoch bump.
        epochRef.current += 1;
        setCloudTask(null);
        setTxn(null);
        setSubmitting(false);
        setDevSpaceError(null);
        setConfirmTxn(false);
        setConfirmCloudTask(false);
        setCloudTaskPrompt('');
        setPendingPushCount(null);
        setEscalation({ trigger: detail, visible: true });
      }
    };
    window.addEventListener(ESCALATION_TRIGGER_EVENT, handler);
    return () => window.removeEventListener(ESCALATION_TRIGGER_EVENT, handler);
  }, []);

  const handleDismiss = useCallback(() => {
    // Invalidate any in-flight txn/cloud-task callbacks so a late phase
    // update can't bring the toast back; the run itself continues on the
    // platform and its result stays available there.
    epochRef.current += 1;
    setEscalation(null);
    setCloudTask(null);
    setTxn(null);
    setSubmitting(false);
    setDevSpaceError(null);
    setConfirmTxn(false);
    setConfirmCloudTask(false);
    setCloudTaskPrompt('');
    setPendingPushCount(null);
  }, []);

  /**
   * ETH-2: run the triggering command transactionally in the user's
   * dev space on `host` — open → push browser deltas → run → pull deltas
   * back into the VFS → finish. `finish` is ALWAYS called (success, failure
   * or timeout) so the pay-per-run machine is never left running: the
   * finally block is the guarantee, the in-flow call just avoids an extra
   * round trip on the happy path. A runner that can't take the run (offline
   * or busy) leaves the error up with a "run in the cloud" way out.
   */
  const startTxn = useCallback(
    (host: EscalationHost) => {
      const trigger = escalation?.trigger;
      const command = typeof trigger?.command === 'string' ? trigger.command.trim() : '';
      const repoURL = trigger?.repoURL;
      if (!command || !repoURL || submitting) return;

      rememberHost(repoURL, host);
      setSubmitting(true);
      setTxn({ phase: 'opening', host });

      const epoch = epochRef.current;
      const live = () => mountedRef.current && epochRef.current === epoch;
      const run = async (): Promise<void> => {
        try {
          const outcome = await runTxnCommand(
            repoURL,
            command,
            (phase) => {
              // Ignore the terminal phases: runTxnCommand emits "done"/"error"
              // for status UIs, but this listener sets its own richer terminal
              // state (with the result/error) once the promise settles.
              if (phase !== 'done' && phase !== 'error' && live()) setTxn({ phase, host });
            },
            host,
          );
          if (live()) {
            setTxn({
              phase: 'done',
              host,
              result: outcome.result,
              pulledFiles: outcome.pulledFiles,
              skippedFiles: outcome.skippedFiles,
              pushedFiles: outcome.pushedFiles,
              warning: outcome.warning,
            });
          }
        } catch (err) {
          if (live()) {
            setTxn({
              phase: 'error',
              host,
              error: err instanceof Error ? err.message : String(err),
              offerCloud: err instanceof HostUnavailableError,
            });
          }
        } finally {
          if (live()) setSubmitting(false);
        }
      };

      run();
    },
    [escalation, submitting],
  );

  /**
   * The txn CTA opens the confirmation step instead of starting the run: the
   * command executes on a machine the browser does not control, so it needs
   * an explicit go-ahead naming what will run, where, and what it costs.
   */
  const handleRunTxn = useCallback(() => {
    if (txn || !hostChoice.loaded) return;
    setPendingPushCount(null);
    setConfirmTxn(true);
  }, [txn, hostChoice.loaded]);

  /** The confirmed run: starts the transaction on the currently chosen host. */
  const handleConfirmTxn = useCallback(() => {
    setConfirmTxn(false);
    startTxn(hostChoice.host);
  }, [hostChoice.host, startTxn]);

  const handleCancelTxnConfirm = useCallback(() => setConfirmTxn(false), []);

  // What the push phase would carry, counted with the same enumerator the run
  // itself uses so the number is what will really be sent. Zero means the
  // machine would run without the browser's edits (the step warns); a failed
  // count only omits the line, it never blocks the run.
  useEffect(() => {
    if (!confirmTxn) return;
    let cancelled = false;
    collectTxnPushFiles()
      .then(({ inputs, deletes }) => {
        if (!cancelled) setPendingPushCount(inputs.length + deletes.length);
      })
      .catch(() => {
        if (!cancelled) setPendingPushCount(null);
      });
    return () => {
      cancelled = true;
    };
  }, [confirmTxn]);

  const handleRunInCloudInstead = useCallback(() => startTxn(CLOUD_HOST), [startTxn]);

  /**
   * Mode A, step 1: the cloud-task CTA opens a confirmation step holding the
   * derived prompt, rather than submitting. The task runs on the repository as
   * last pushed — without the browser's edits — and spends platform credits,
   * so the user gets to read and edit the prompt before anything is sent.
   */
  const handleRunCloudTask = useCallback(() => {
    const trigger = escalation?.trigger;
    // confirmTxn too: its step owns the toast while open, so the CTA (hidden
    // by showCloudTaskButton then) must not arm Mode A behind it either.
    if (!trigger?.repoURL || submitting || cloudTask || confirmTxn) return;
    setCloudTaskPrompt(deriveEscalationPrompt(trigger));
    setConfirmCloudTask(true);
  }, [escalation, submitting, cloudTask, confirmTxn]);

  /** Mode A, step 2: submit exactly what the prompt box holds, then poll. */
  const handleConfirmCloudTask = useCallback(() => {
    const trigger = escalation?.trigger;
    const repoURL = trigger?.repoURL;
    const prompt = cloudTaskPrompt.trim();
    if (!repoURL || !prompt || submitting || cloudTask) return;

    setConfirmCloudTask(false);
    setSubmitting(true);
    const epoch = epochRef.current;
    const live = () => mountedRef.current && epochRef.current === epoch;
    submitCloudTask({
      repo_url: repoURL,
      prompt,
    })
      .then(({ task }: { task: CloudTask }) => {
        if (!live()) return;
        setCloudTask({ taskId: task.task_id, status: task.status || 'pending' });
        return pollCloudTask(task.task_id, {
          intervalMs: CLOUD_TASK_POLL_INTERVAL_MS,
          onTick: (t: CloudTask) => {
            if (!live()) return;
            setCloudTask((prev) =>
              prev && prev.taskId === task.task_id ? { ...prev, status: t.status, error: prev.error } : prev,
            );
          },
        }).catch((err: unknown) => {
          // Rejected only on timeout or a failed status request; either way
          // the task itself still exists and stays linkable.
          if (!live()) return;
          const message = err instanceof Error && err.message ? err.message : 'Cloud task status polling failed';
          setCloudTask((prev) => (prev && prev.taskId === task.task_id ? { ...prev, error: message } : prev));
        });
      })
      .catch((err: unknown) => {
        if (!live()) return;
        const message = err instanceof Error && err.message ? err.message : String(err);
        // Submit failed — no task id yet, so only the error renders.
        setCloudTask({ taskId: '', status: '', error: message });
      })
      .finally(() => {
        if (live()) setSubmitting(false);
      });
  }, [escalation, cloudTaskPrompt, submitting, cloudTask]);

  /** Mode A: leave the confirmation step without submitting anything. */
  const handleBackFromCloudTask = useCallback(() => setConfirmCloudTask(false), []);

  const handleStartDevSpace = useCallback(() => {
    const repoURL = escalation?.trigger?.repoURL;
    if (!repoURL) {
      // No repo context: the host's account/dashboard page is where a
      // dev space gets picked. The host owns the path (and its outward URL).
      const dashboard = navigation.intentPath?.({ type: 'account' }) ?? null;
      if (dashboard && !openPlatformPage(dashboard)) {
        window.location.href = navigation.platformPagePath?.(dashboard) ?? dashboard;
      }
      return;
    }
    setDevSpaceError(null);
    setStartingDevSpace(true);
    startDevSpace(repoURL)
      .then((result) => {
        if (!mountedRef.current) return;
        if (result.kind === 'unavailable') {
          setDevSpaceError("This deployment doesn't offer dev spaces.");
        } else if (result.kind === 'error') {
          setDevSpaceError(`Couldn't start the dev space: ${result.message}`);
        } else if (result.kind === 'status') {
          setDevSpaceError(`The dev space is ${result.status}; try again shortly.`);
        }
      })
      .catch((err: unknown) => {
        if (!mountedRef.current) return;
        setDevSpaceError(`Couldn't start the dev space: ${err instanceof Error ? err.message : String(err)}`);
      })
      .finally(() => {
        if (mountedRef.current) setStartingDevSpace(false);
      });
  }, [escalation, navigation]);

  if (!escalation?.visible) return null;

  const repoURL = escalation.trigger.repoURL;
  // The txn CTA needs both the command (what to run) and repo context (where
  // to run it); without either the toast falls back to Mode A / Mode B.
  // A runnable command with nothing in flight: the user acts on the CTA, or
  // on the confirmation step once the CTA has been clicked.
  const canStartTxn = canRunTxn && !txn && !cloudTask;
  const showTxnButton = canStartTxn && !confirmTxn;
  const showTxnConfirm = canStartTxn && confirmTxn;
  const txnOnRunner = hostChoice.host.kind === 'runner';
  // Shown only when there is repo context (same gate the Mode B path uses for
  // a meaningful task) and no txn/cloud task is already in flight.
  const showCloudTaskButton = Boolean(repoURL) && !cloudTask && !txn && !confirmCloudTask;
  // The Mode A confirmation step: repo context, nothing in flight, and the
  // txn confirmation not already holding the toast.
  const showCloudTaskConfirm = Boolean(repoURL) && confirmCloudTask && !cloudTask && !txn && !showTxnConfirm;
  // The task page's route for the deep link, resolved host-side: a cloud
  // host maps it to its platform route; the local host maps it to its own
  // hash route. When the host has no page for it the link is not rendered —
  // which is why localHost resolves the tasks intent itself (returning null
  // dropped the "View task" link and broke the CLOUD-2 surface).
  const taskLinkPath = cloudTask?.taskId
    ? (navigation.intentPath?.({ type: 'nav', id: 'tasks', detail: cloudTask.taskId }) ?? null)
    : null;

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
                  <>
                    <p className="escalation-toast-task-error" data-testid="escalation-toast-txn-error">
                      {txn.error}
                    </p>
                    {txn.offerCloud ? (
                      <button
                        className="escalation-toast-action escalation-toast-action-secondary"
                        onClick={handleRunInCloudInstead}
                        disabled={submitting}
                        data-testid="escalation-toast-txn-cloud-fallback"
                      >
                        <Container size={14} />
                        Run in cloud container instead
                      </button>
                    ) : null}
                  </>
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
                    {txn.pushedFiles === 0 ? (
                      <p className="escalation-toast-task-error" data-testid="escalation-toast-txn-empty-push">
                        0 files were pushed — the container ran WITHOUT your browser edits (no git state in this page).
                      </p>
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
                    {txnPhaseLabel(txn.phase, txn.host)}…
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
                    {isTerminalCloudTaskStatus(cloudTask.status) ? null : (
                      <Loader2 size={13} className="spinner" aria-hidden="true" />
                    )}
                    Cloud task {cloudTask.status || 'pending'}
                  </p>
                )}
                {cloudTask.taskId && taskLinkPath ? (
                  <a
                    className="escalation-toast-task-link"
                    href={navigation.platformPagePath?.(taskLinkPath) ?? taskLinkPath}
                    onClick={onPlatformLinkClick(taskLinkPath)}
                    data-testid="escalation-toast-cloud-task-link"
                  >
                    View task on platform
                  </a>
                ) : null}
              </div>
            ) : null}

            {/* Mode A's confirm step: the CTA opens this instead of
                submitting, so the prompt — editable — and the fact that the
                task runs from the pushed repository are read before the task
                is created. */}
            {showCloudTaskConfirm ? (
              <div
                className="escalation-toast-confirm"
                data-testid="escalation-cloud-task-confirm"
                role="group"
                aria-label="Confirm running a cloud task"
              >
                <p className="escalation-toast-confirm-lead">
                  Run an agent on <strong>{repoURL}</strong> as your cloud task?
                </p>
                <label className="escalation-toast-confirm-label" htmlFor="escalation-cloud-task-prompt">
                  Prompt
                </label>
                <textarea
                  id="escalation-cloud-task-prompt"
                  className="escalation-toast-confirm-prompt"
                  data-testid="escalation-cloud-task-prompt"
                  value={cloudTaskPrompt}
                  onChange={(e) => setCloudTaskPrompt(e.target.value)}
                  rows={3}
                  disabled={submitting}
                />
                <p className="escalation-toast-confirm-detail">
                  Runs on the repository as last pushed — browser edits aren&apos;t included. Uses platform credits.
                </p>
                <div className="escalation-toast-actions">
                  <button
                    className="escalation-toast-action"
                    onClick={handleConfirmCloudTask}
                    disabled={submitting || cloudTaskPrompt.trim() === ''}
                    data-testid="escalation-cloud-task-start"
                  >
                    {submitting ? <Loader2 size={14} className="spinner" aria-hidden="true" /> : <Cloud size={14} />}
                    Start task
                  </button>
                  <button
                    className="escalation-toast-action escalation-toast-action-secondary"
                    onClick={handleBackFromCloudTask}
                    disabled={submitting}
                    data-testid="escalation-cloud-task-back"
                  >
                    Back
                  </button>
                </div>
              </div>
            ) : null}

            {devSpaceError ? (
              <p className="escalation-toast-task-error" data-testid="escalation-toast-workspace-error">
                {devSpaceError}
              </p>
            ) : null}

            {/* The host stays switchable while confirming: the choice is read
                when the run is confirmed, not when the step opens. */}
            {canStartTxn && hostChoice.runners.length > 0 ? (
              <RunHostPicker
                runners={hostChoice.runners}
                value={hostChoice.host}
                onChange={hostChoice.setHost}
                disabled={submitting}
              />
            ) : null}

            {showTxnConfirm ? (
              <div
                className="escalation-toast-confirm"
                data-testid="escalation-toast-txn-confirm"
                role="group"
                aria-label="Confirm running this command on another machine"
              >
                <p className="escalation-toast-confirm-lead">
                  Run <code>{escalation.trigger.command}</code> on{' '}
                  <strong>{txnOnRunner ? hostDisplayName(hostChoice.host) : 'a cloud container'}</strong>?
                </p>
                <p className="escalation-toast-confirm-detail" data-testid="escalation-toast-txn-confirm-changes">
                  {pendingPushCount === null
                    ? 'Counting your browser changes…'
                    : pendingPushCount === 0
                      ? 'No browser changes detected.'
                      : `${pendingPushCount} changed ${pendingPushCount === 1 ? 'file' : 'files'} will be sent.`}
                </p>
                {pendingPushCount === 0 ? (
                  <p className="escalation-toast-task-error" data-testid="escalation-toast-txn-confirm-empty-push">
                    The command would run without your browser edits.
                  </p>
                ) : null}
                <p className="escalation-toast-confirm-detail" data-testid="escalation-toast-txn-confirm-cost">
                  Cloud and runner machines bill per minute while they run. A run that fails or is cancelled is billed
                  for the time it used.
                </p>
                <div className="escalation-toast-actions">
                  <button
                    className="escalation-toast-action"
                    onClick={handleConfirmTxn}
                    disabled={submitting}
                    data-testid="escalation-toast-txn-confirm-run"
                  >
                    {submitting ? <Loader2 size={14} className="spinner" aria-hidden="true" /> : <Rocket size={14} />}
                    Run command
                  </button>
                  <button
                    className="escalation-toast-action escalation-toast-action-secondary"
                    onClick={handleCancelTxnConfirm}
                    disabled={submitting}
                    data-testid="escalation-toast-txn-confirm-cancel"
                  >
                    Cancel
                  </button>
                </div>
              </div>
            ) : null}

            {showTxnConfirm || showCloudTaskConfirm ? null : (
              <div className="escalation-toast-actions">
                {showTxnButton ? (
                  <button
                    className="escalation-toast-action"
                    onClick={handleRunTxn}
                    disabled={submitting || !hostChoice.loaded}
                    data-testid="escalation-toast-txn"
                  >
                    {submitting ? (
                      <Loader2 size={14} className="spinner" aria-hidden="true" />
                    ) : txnOnRunner ? (
                      <Server size={14} />
                    ) : (
                      <Container size={14} />
                    )}
                    {txnOnRunner ? `Run on ${hostDisplayName(hostChoice.host)}` : 'Run in cloud container'}
                  </button>
                ) : null}
                {showCloudTaskButton ? (
                  <button
                    className="escalation-toast-action escalation-toast-action-secondary"
                    onClick={handleRunCloudTask}
                    disabled={submitting}
                    data-testid="escalation-toast-cloud-task"
                    title="Runs an agent on the repository as last pushed; edits made in the browser aren't included. Uses platform credits."
                  >
                    {submitting ? <Loader2 size={14} className="spinner" aria-hidden="true" /> : <Cloud size={14} />}
                    Run as cloud task
                  </button>
                ) : null}
                {devSpacesAvailable ? (
                  <button
                    className="escalation-toast-action"
                    onClick={handleStartDevSpace}
                    disabled={startingDevSpace}
                    data-testid="escalation-toast-start-workspace"
                  >
                    {startingDevSpace ? (
                      <Loader2 size={14} className="spinner" aria-hidden="true" />
                    ) : (
                      <Rocket size={14} />
                    )}
                    Start a Dev Space
                  </button>
                ) : null}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
