/**
 * HealthStrip — the design surface's loop-status bar (SP-140-6 §6c).
 *
 * One slim strip across the top of the design surface: validate tally chips,
 * the two §5c drift rows (only ahead rows render; a synced tree shows one
 * quiet mark), and the pending-feedback count. Data comes from
 * GET /api/design/status (§6b) — the same scanners the agent tools read, so
 * the strip can never disagree with what design_validate reports.
 *
 * Every chip is a click-through to the surface it names (§6c):
 *   validation chip  -> onOpenFinding(file)   (selects the asset)
 *   design-ahead     -> onOpenSection('tokens')
 *   code-ahead       -> onAskAgent(remedy)     (agent-panel prefill)
 *   pending feedback -> onOpenFinding(target)
 * plus the manual refresh control (§6a's demand refetch).
 *
 * The strip polls with the same rhythm as the live tree (focus/interval are
 * the provider's job); it refetches on mount, when the window regains focus,
 * and when the parent bumps `refreshKey`.
 */

import { RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { fetchDesignStatus, FINDING_SEVERITIES, type DesignStatus } from '../../services/api/designStatusApi';
import { DESIGN_REFRESH_INTERVAL_MS } from './DesignWorkspaceContext';

export interface HealthStripProps {
  /** Bump to force a refetch (wired to the live tree's refresh). */
  refreshKey?: number;
  /** A validation chip / pending-feedback click: open this asset. */
  onOpenFinding?: (path: string) => void;
  /** Design-ahead click: open the Tokens section (the remedy's surface). */
  onOpenSection?: (tab: 'flows' | 'screens' | 'tokens') => void;
  /** Code-ahead click: prefill the agent panel with the remedy. */
  onAskAgent?: (prompt: string) => void;
  /**
   * The refresh control's handler. When supplied it owns the refetch (the
   * shell also refreshes the inventory — one control, both views); when
   * omitted the strip refetches its own status only.
   */
  onRefresh?: () => void;
}

/** Severity chip classes for the tally. Kept token-only (§6h). */
function severityChipClass(kind: 'errors' | 'warnings' | 'infos'): string {
  return `design-health-chip design-health-chip-${kind}`;
}

export default function HealthStrip({
  refreshKey = 0,
  onOpenFinding,
  onOpenSection,
  onAskAgent,
  onRefresh,
}: HealthStripProps) {
  const fetchFn = useSproutFetch();
  const [status, setStatus] = useState<DesignStatus | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setBusy(true);
    (async () => {
      const next = await fetchDesignStatus(fetchFn);
      if (!cancelled) {
        setStatus(next);
        setBusy(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [fetchFn, refreshKey]);

  // Focus + slow interval, matching the live tree's rhythm (§6a): a user
  // who stays focused still sees tallies move after an agent turn.
  useEffect(() => {
    const refetch = () => {
      setBusy(true);
      void fetchDesignStatus(fetchFn).then((next) => {
        setStatus(next);
        setBusy(false);
      });
    };
    window.addEventListener('focus', refetch);
    const timer = window.setInterval(refetch, DESIGN_REFRESH_INTERVAL_MS);
    return () => {
      window.removeEventListener('focus', refetch);
      window.clearInterval(timer);
    };
  }, [fetchFn]);

  if (!status || !status.exists) {
    // No tree (or transport failure before the first response): render the
    // strip empty rather than gone, so the surface's layout is stable.
    return (
      <div className="design-health" data-testid="design-health-strip" data-state="idle">
        <span className="design-health-muted">No design status</span>
      </div>
    );
  }

  const { validation, drift, feedback } = status;
  const hasFindings = validation.errors > 0 || validation.warnings > 0;

  return (
    <div className="design-health" data-testid="design-health-strip" data-synced={drift.synced}>
      <div className="design-health-chips" role="status" aria-label="Design tree health">
        {hasFindings || validation.infos > 0 ? (
          <>
            {validation.errors > 0 && (
              <button
                type="button"
                className={severityChipClass('errors')}
                data-testid="design-health-errors"
                onClick={() => {
                  const first = validation.findings.find((f) => f.severity === 'error');
                  if (first) onOpenFinding?.(first.file);
                }}
              >
                {validation.errors} {validation.errors === 1 ? 'error' : 'errors'}
              </button>
            )}
            {validation.warnings > 0 && (
              <button
                type="button"
                className={severityChipClass('warnings')}
                data-testid="design-health-warnings"
                onClick={() => {
                  const first = validation.findings.find((f) =>
                    (FINDING_SEVERITIES.warnings as readonly string[]).includes(f.severity),
                  );
                  if (first) onOpenFinding?.(first.file);
                }}
              >
                {validation.warnings} {validation.warnings === 1 ? 'warning' : 'warnings'}
              </button>
            )}
            {validation.infos > 0 && (
              <span className={severityChipClass('infos')} data-testid="design-health-infos">
                {validation.infos} info
              </span>
            )}
          </>
        ) : (
          <span className="design-health-ok" data-testid="design-health-clean">
            Validated clean
          </span>
        )}

        {drift.designAhead.ahead && (
          <button
            type="button"
            className="design-health-chip design-health-chip-drift"
            data-testid="design-health-design-ahead"
            title={drift.designAhead.summary}
            onClick={() => onOpenSection?.('tokens')}
          >
            design-ahead: {drift.designAhead.count} — regenerate theme
          </button>
        )}
        {drift.codeAhead.ahead && (
          <button
            type="button"
            className="design-health-chip design-health-chip-drift"
            data-testid="design-health-code-ahead"
            title={drift.codeAhead.summary}
            onClick={() =>
              onAskAgent?.(
                drift.codeAhead.remedy ??
                  "Run design_sync to import the implementation's semantic deltas into design/.",
              )
            }
          >
            code-ahead: {drift.codeAhead.count} — import via design_sync
          </button>
        )}
        {drift.synced && (
          <span className="design-health-synced" data-testid="design-health-in-sync">
            in sync
          </span>
        )}

        {feedback.pendingCount > 0 && (
          <button
            type="button"
            className="design-health-chip design-health-chip-feedback"
            data-testid="design-health-feedback"
            onClick={() => feedback.pending[0] && onOpenFinding?.(feedback.pending[0].target)}
          >
            {feedback.pendingCount} pending feedback
          </button>
        )}
      </div>
      <button
        type="button"
        className={`design-health-refresh${busy ? ' busy' : ''}`}
        data-testid="design-health-refresh"
        aria-label="Refresh design status"
        onClick={() => {
          if (onRefresh) {
            setBusy(true);
            // The shell's handler refetches the inventory; the refreshKey
            // bump it triggers re-runs this strip's own fetch effect.
            onRefresh();
            return;
          }
          setBusy(true);
          void fetchDesignStatus(fetchFn).then((next) => {
            setStatus(next);
            setBusy(false);
          });
        }}
      >
        <RefreshCw size={12} />
      </button>
    </div>
  );
}
