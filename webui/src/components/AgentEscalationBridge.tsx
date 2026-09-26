/**
 * AgentEscalationBridge — cloud mode only. Installs the bridge the WASM agent
 * calls when one of its shell commands can't run in the browser, and asks the
 * user (per the escalation policy) before running it in the cloud workspace.
 */

import { Cloud } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { isCloud } from '../config/mode';
import { installEscalationBridge, type ConsentDecision } from '../services/agentEscalation';
import { txnPhaseLabel } from '../services/cloudTxnEscalate';
import './ThemedDialog.css';
import './AgentEscalationBridge.css';

interface PendingConsent {
  command: string;
  resolve: (decision: ConsentDecision) => void;
}

export function AgentEscalationBridge({ repoURL }: { repoURL?: string }) {
  const [pending, setPending] = useState<PendingConsent[]>([]);
  const [progress, setProgress] = useState<{ command: string; phase: string } | null>(null);
  const allowRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!isCloud) return undefined;
    return installEscalationBridge({
      repoURL,
      requestConsent: (command) =>
        new Promise<ConsentDecision>((resolve) => {
          setPending((q) => [...q, { command, resolve }]);
        }),
      onPhase: (command, phase) => setProgress(phase === 'done' ? null : { command, phase }),
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

  const answer = useCallback(
    (decision: ConsentDecision) => {
      if (!current) return;
      current.resolve(decision);
      setPending((q) => q.slice(1));
    },
    [current],
  );

  useEffect(() => {
    if (!current) return undefined;
    allowRef.current?.focus();
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

  return (
    <>
      {progress && !current && (
        <div className="agent-escalation-status" role="status">
          <Cloud size={14} /> {txnPhaseLabel(progress.phase)}: <code>{progress.command}</code>
        </div>
      )}
      {current && (
        <div className="security-approval-overlay" role="dialog" aria-modal="true" aria-labelledby="agent-escalation-title">
          <div className="security-approval-card">
            <div className="security-approval-header">
              <span className="security-approval-shield security-approval-shield--caution">
                <Cloud size={18} />
              </span>
              <div className="security-approval-header-row">
                <h2 id="agent-escalation-title" className="security-approval-title">
                  Run this in your cloud workspace?
                </h2>
              </div>
            </div>
            <div className="security-approval-body">
              <div className="security-approval-reasoning">
                The agent wants to run a command the in-browser shell can&apos;t. Your files are copied to your cloud
                workspace, the command runs there, and any changes come back here. This uses workspace time from your
                plan.
              </div>
              <div className="security-approval-command-wrapper">
                <div className="security-approval-command-label">Command</div>
                <pre className="security-approval-command-box">{current.command}</pre>
              </div>
            </div>
            <div className="security-approval-footer">
              <button type="button" className="security-approval-btn security-approval-btn--block" onClick={() => answer('deny')}>
                Don&apos;t run
              </button>
              <button type="button" className="security-approval-btn security-approval-btn--allow" onClick={() => answer('always')}>
                Always allow
              </button>
              <button
                ref={allowRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={() => answer('once')}
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
