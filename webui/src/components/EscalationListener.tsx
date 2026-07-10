/**
 * EscalationModal — shown when the user hits a browser limitation in cloud
 * mode (git push, VFS quota, command timeout). Offers a path to upgrade
 * from the browser IDE (Mode C) to a full workspace (Mode B).
 *
 * Listens for sprout:escalation-trigger events with severity='blocking'.
 */

import { useCallback, useEffect, useState } from 'react';
import { Rocket, X } from 'lucide-react';
import type { EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import { ESCALATION_TRIGGER_EVENT } from '../hooks/useEscalationTriggers';
import './EscalationToast.css';

interface EscalationState {
  trigger: EscalationTriggerEvent;
  visible: boolean;
}

export function EscalationListener() {
  const [escalation, setEscalation] = useState<EscalationState | null>(null);

  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<EscalationTriggerEvent>).detail;
      if (detail?.severity === 'blocking') {
        setEscalation({ trigger: detail, visible: true });
      }
    };
    window.addEventListener(ESCALATION_TRIGGER_EVENT, handler);
    return () => window.removeEventListener(ESCALATION_TRIGGER_EVENT, handler);
  }, []);

  const handleDismiss = useCallback(() => {
    setEscalation(null);
  }, []);

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
            <button className="escalation-toast-action" onClick={handleStartWorkspace}>
              <Rocket size={14} />
              Start Full Workspace
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
