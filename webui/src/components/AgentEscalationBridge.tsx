/**
 * AgentEscalationBridge — cloud mode only. Installs the bridge the WASM agent
 * calls when one of its shell commands can't run in the browser, and asks the
 * user (per the escalation policy) before running it on one of their runners
 * or in the cloud workspace.
 */

import { Cloud, Server } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { isCloud } from '../config/mode';
import { useRunHostChoice } from '../hooks/useRunHostChoice';
import {
  installEscalationBridge,
  type ConsentAnswer,
  type ConsentContext,
  type ConsentDecision,
} from '../services/agentEscalation';
import { txnPhaseLabel } from '../services/cloudTxnEscalate';
import { AUTO_HOST, getRememberedHost, hostDisplayName, type EscalationHost } from '../services/escalationHost';
import { isRunnerSelectable, listRunners } from '../services/runners';
import { RunHostPicker } from './RunHostPicker';
import './ThemedDialog.css';
import './AgentEscalationBridge.css';

interface PendingConsent {
  command: string;
  context?: ConsentContext;
  resolve: (answer: ConsentAnswer) => void;
}

/** Whether a runner host is still on the user's account and able to take work. */
async function runnerAvailable(host: EscalationHost): Promise<boolean> {
  if (host.kind !== 'runner') return true;
  const runner = (await listRunners()).find((r) => r.runner_id === host.runnerId);
  return Boolean(runner && isRunnerSelectable(runner));
}

export function AgentEscalationBridge({ repoURL }: { repoURL?: string }) {
  const [pending, setPending] = useState<PendingConsent[]>([]);
  const [progress, setProgress] = useState<{ command: string; phase: string; host: EscalationHost } | null>(null);
  const allowRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!isCloud) return undefined;
    return installEscalationBridge({
      repoURL,
      requestConsent: (command, context) =>
        new Promise<ConsentAnswer>((resolve) => {
          setPending((q) => [...q, { command, context, resolve }]);
        }),
      alwaysHost: () => getRememberedHost(repoURL) ?? AUTO_HOST,
      isHostAvailable: runnerAvailable,
      onPhase: (command, phase, host) => setProgress(phase === 'done' ? null : { command, phase, host }),
    });
  }, [repoURL]);

  // The run reports opening → pushing → running → pulling; clear the status
  // shortly after the last phase since runTxnCommand has no "done" callback.
  useEffect(() => {
    if (progress?.phase !== 'pulling') return undefined;
    const t = setTimeout(() => setProgress(null), 4000);
    return () => clearTimeout(t);
  }, [progress]);

  const current = pending[0];
  const choice = useRunHostChoice(repoURL, current ?? null, current?.context?.unavailableRunnerId);
  const { host } = choice;

  const answer = useCallback(
    (decision: ConsentDecision) => {
      if (!current) return;
      current.resolve(decision === 'deny' ? { decision } : { decision, host });
      setPending((q) => q.slice(1));
    },
    [current, host],
  );

  // Focus the primary action once it is enabled (runners loaded).
  const ready = choice.loaded;
  useEffect(() => {
    if (current && ready) allowRef.current?.focus();
  }, [current, ready]);

  useEffect(() => {
    if (!current) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        answer('deny');
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [current, answer]);

  if (!isCloud) return null;

  const onRunner = host.kind === 'runner';
  const HostIcon = onRunner ? Server : Cloud;

  return (
    <>
      {progress && !current && (
        <div className="agent-escalation-status" role="status">
          {progress.host.kind === 'runner' ? <Server size={14} /> : <Cloud size={14} />}{' '}
          {txnPhaseLabel(progress.phase, progress.host)}: <code>{progress.command}</code>
        </div>
      )}
      {current && (
        <div
          className="security-approval-overlay"
          role="dialog"
          aria-modal="true"
          aria-labelledby="agent-escalation-title"
        >
          <div className="security-approval-card">
            <div className="security-approval-header">
              <span className="security-approval-shield security-approval-shield--caution">
                <HostIcon size={18} />
              </span>
              <div className="security-approval-header-row">
                <h2 id="agent-escalation-title" className="security-approval-title">
                  {onRunner ? `Run this on ${hostDisplayName(host)}?` : 'Run this in your cloud workspace?'}
                </h2>
              </div>
            </div>
            <div className="security-approval-body">
              {current.context?.notice ? (
                <p className="run-host-notice" role="alert" data-testid="run-host-notice">
                  {current.context.notice} You can run it in the cloud instead.
                </p>
              ) : null}
              <div className="security-approval-reasoning">
                {onRunner
                  ? `The agent wants to run a command the in-browser shell can't. Your files are copied to a workspace on ${hostDisplayName(host)}, the command runs there, and any changes come back here. Runs on your own runner don't use workspace time from your plan.`
                  : "The agent wants to run a command the in-browser shell can't. Your files are copied to your cloud workspace, the command runs there, and any changes come back here. This uses workspace time from your plan."}
              </div>
              {choice.runners.length > 0 ? (
                <RunHostPicker runners={choice.runners} value={host} onChange={choice.setHost} />
              ) : null}
              <div className="security-approval-command-wrapper">
                <div className="security-approval-command-label">Command</div>
                <pre className="security-approval-command-box">{current.command}</pre>
              </div>
            </div>
            <div className="security-approval-footer">
              <button
                type="button"
                className="security-approval-btn security-approval-btn--block"
                onClick={() => answer('deny')}
              >
                Don&apos;t run
              </button>
              <button
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={() => answer('always')}
                disabled={!ready}
              >
                Always allow
              </button>
              <button
                ref={allowRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={() => answer('once')}
                disabled={!ready}
              >
                Run once
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
