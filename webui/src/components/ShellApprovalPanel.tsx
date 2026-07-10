import type { ShellApprovalPartData, ShellApprovalRequestData } from '@sprout/events';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Check, X } from 'lucide-react';
import './ShellApprovalPanel.css';

type PartDecision = 'pending' | 'approved' | 'rejected';

interface ShellApprovalPanelProps {
  request: {
    request_id: string;
    command: string;
    parts: ShellApprovalPartData[];
    unified_view: string;
    risk_level: string;
  };
  onSubmit: (decisions: Record<string, boolean>) => void | Promise<void>;
  onCancel?: () => void;
}

/**
 * Map a risk string to the default decision state.
 * Low / Medium → auto-approved.  High / Critical → pending (requires review).
 * Anything else → pending.
 */
function defaultDecisionForRisk(risk: string): PartDecision {
  const normalized = risk.trim().toLowerCase();
  if (normalized === 'low' || normalized === 'medium') return 'approved';
  return 'pending';
}

/** Cycle: pending → approved → rejected → pending */
const DECISION_CYCLE: PartDecision[] = ['pending', 'approved', 'rejected'];

function decisionIcon(status: PartDecision): JSX.Element {
  if (status === 'approved') return <Check size={14} />;
  if (status === 'rejected') return <X size={14} />;
  return <span style={{ color: 'var(--text-muted)' }}>?</span>;
}

function decisionLabel(status: PartDecision): string {
  if (status === 'approved') return 'Approved';
  if (status === 'rejected') return 'Rejected';
  return 'Pending';
}

/**
 * ShellApprovalPanel (SP-093-3) — renders a pending shell command's parts
 * with per-part Approve/Reject toggles and color-coded risk badges.
 *
 * Driven by the `shell_approval_request` WebSocket event. On decision,
 * the parent POSTs to /api/shell-approvals/{id}/decision.
 */
function ShellApprovalPanel({ request, onSubmit, onCancel }: ShellApprovalPanelProps): JSX.Element {
  const { request_id, command, parts, unified_view, risk_level } = request;

  // Per-part decisions keyed by part id.
  const [decisions, setDecisions] = useState<Record<string, PartDecision>>(() =>
    Object.fromEntries(parts.map((p) => [p.id, defaultDecisionForRisk(p.risk)])),
  );

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showUnified, setShowUnified] = useState(false);

  // Reset when a new request arrives.
  useEffect(() => {
    setDecisions(Object.fromEntries(parts.map((p) => [p.id, defaultDecisionForRisk(p.risk)])));
    setError(null);
    setShowUnified(false);
  }, [request_id, parts]);

  const togglePart = useCallback((partId: string) => {
    setDecisions((prev) => {
      const current = prev[partId] ?? 'pending';
      const idx = DECISION_CYCLE.indexOf(current);
      const next = DECISION_CYCLE[(idx + 1) % DECISION_CYCLE.length];
      return { ...prev, [partId]: next };
    });
  }, []);

  const acceptAll = useCallback(() => {
    setDecisions((prev) => Object.fromEntries(Object.keys(prev).map((k) => [k, 'approved'] as [string, PartDecision])));
  }, []);

  const rejectAll = useCallback(() => {
    setDecisions((prev) => Object.fromEntries(Object.keys(prev).map((k) => [k, 'rejected'] as [string, PartDecision])));
  }, []);

  const resetToDefaults = useCallback(() => {
    setDecisions(Object.fromEntries(parts.map((p) => [p.id, defaultDecisionForRisk(p.risk)])));
  }, [parts]);

  const handleSubmit = useCallback(async () => {
    setSubmitting(true);
    setError(null);
    try {
      // Convert PartDecision → boolean.
      // Pending parts are treated as denied for safety.
      const booleanDecisions: Record<string, boolean> = {};
      for (const [id, status] of Object.entries(decisions)) {
        booleanDecisions[id] = status === 'approved';
      }
      await Promise.resolve(onSubmit(booleanDecisions));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }, [decisions, onSubmit]);

  const approvedCount = useMemo(() => Object.values(decisions).filter((d) => d === 'approved').length, [decisions]);

  return (
    <div className="themed-dialog-overlay shell-approval-overlay" role="dialog" aria-modal="true">
      <div className="themed-dialog-card shell-approval-card">
        <div className="themed-dialog-accent-bar themed-dialog-accent-bar--warning" />
        <div className="shell-approval-header">
          <div className="shell-approval-header-left">
            <h2 className="shell-approval-title">Shell Command Approval</h2>
            <span
              className={`shell-approval-risk-badge shell-approval-risk-badge--${risk_level.toLowerCase()}`}
              data-testid="shell-approval-risk-badge"
            >
              {risk_level} Risk
            </span>
          </div>
          {onCancel && (
            <button
              type="button"
              className="shell-approval-close-btn"
              onClick={onCancel}
              aria-label="Close"
              disabled={submitting}
            >
              <X size={14} />
            </button>
          )}
        </div>

        {/* Quick actions */}
        <div className="shell-approval-actions-top">
          <button
            type="button"
            className="shell-approval-link-btn"
            onClick={acceptAll}
            disabled={submitting}
            data-testid="shell-approval-accept-all"
          >
            Accept all
          </button>
          <span className="shell-approval-sep">·</span>
          <button
            type="button"
            className="shell-approval-link-btn"
            onClick={rejectAll}
            disabled={submitting}
            data-testid="shell-approval-reject-all"
          >
            Reject all
          </button>
          <span className="shell-approval-sep">·</span>
          <button
            type="button"
            className="shell-approval-link-btn"
            onClick={resetToDefaults}
            disabled={submitting}
            data-testid="shell-approval-reset"
          >
            Reset to defaults
          </button>
        </div>

        {/* Full command */}
        <div className="shell-approval-command-block">
          <pre className="shell-approval-command-pre" data-testid="shell-approval-command">
            {command}
          </pre>
        </div>

        {/* Parts list */}
        <div className="shell-approval-parts-body">
          {parts.map((part) => {
            const status = decisions[part.id] ?? 'pending';
            return (
              <div
                key={part.id}
                className={`shell-approval-part shell-approval-part--${status}`}
                data-testid={`shell-approval-part-${part.id}`}
              >
                <div className="shell-approval-part-header">
                  <span className={`shell-approval-part-icon shell-approval-part-icon--${status}`}>
                    {decisionIcon(status)}
                  </span>
                  <code className="shell-approval-part-code">{part.text}</code>
                </div>
                <div className="shell-approval-part-meta">
                  <span className="shell-approval-part-caption">
                    {part.kind} — {part.semantic}
                  </span>
                  <span className={`shell-approval-part-risk shell-approval-part-risk--${part.risk.toLowerCase()}`}>
                    {part.risk}
                  </span>
                </div>
                <button
                  type="button"
                  className="shell-approval-part-toggle"
                  onClick={() => togglePart(part.id)}
                  disabled={submitting}
                  title={`Current: ${decisionLabel(status)}. Click to cycle.`}
                  data-testid={`shell-approval-part-toggle-${part.id}`}
                >
                  {decisionLabel(status)}
                </button>
              </div>
            );
          })}
        </div>

        {/* Unified view (collapsible) */}
        {unified_view && (
          <details
            className="shell-approval-unified"
            open={showUnified}
            onToggle={(e) => setShowUnified(e.currentTarget.open)}
          >
            <summary>Unified view</summary>
            <pre className="shell-approval-unified-pre">{unified_view}</pre>
          </details>
        )}

        {error && <div className="shell-approval-error">{error}</div>}

        {/* Footer */}
        <div className="shell-approval-footer">
          <span className="shell-approval-selected-count">
            {approvedCount}/{parts.length} parts approved
          </span>
          <div className="shell-approval-footer-actions">
            {onCancel && (
              <button
                type="button"
                className="shell-approval-btn shell-approval-btn--cancel"
                onClick={onCancel}
                disabled={submitting}
              >
                Cancel
              </button>
            )}
            <button
              type="button"
              className="shell-approval-btn shell-approval-btn--submit"
              onClick={handleSubmit}
              disabled={submitting}
              data-testid="shell-approval-submit"
            >
              {submitting ? 'Submitting…' : 'Submit'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default ShellApprovalPanel;
