/**
 * Ship surface — the deploy lens on the shared project conversation.
 *
 * Shows what is live (URL, version, last deploy), offers the deploy action,
 * lists the deploy history with each entry linkable, and offers roll back.
 * Ship does not host its own chat: it is a view of the same conversation that
 * Code and Design share, so it advertises the deploy tools and their state
 * rather than opening a second transcript.
 *
 * The component is pure: every value it renders arrives as a prop, and every
 * action is a callback. That keeps it testable without a server and keeps the
 * seam to the real data source (the deploy API and its adapters) in the app,
 * not here.
 */

import { AlertTriangle, ArrowUpRight, ChevronRight, Loader2, Rocket, Undo2 } from 'lucide-react';
import React from 'react';
import type { ShipDeployKind, ShipDeployState, ShipHistoryEntry, ShipPayload } from '../../workspaces/ship';
import './ShipSurface.css';

export interface ShipSurfaceProps {
  payload: ShipPayload;
}

/** Human label for a deployment's lifecycle state. */
const STATE_LABELS: Record<ShipDeployState, string> = {
  queued: 'Queued',
  deploying: 'Deploying',
  ready: 'Live',
  failed: 'Failed',
  rolled_back: 'Rolled back',
};

/** Human label for the deploy kind. */
const KIND_LABELS: Record<ShipDeployKind, string> = {
  preview: 'Preview',
  production: 'Production',
};

/** Format an ISO timestamp for display; falls back to the raw value. */
function formatTimestamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

/** One history row: version, plan revision, summary, state, and its link/action. */
const HistoryRow: React.FC<{
  entry: ShipHistoryEntry;
  disabled: boolean;
  onOpenEntry?: (id: string) => void;
  onRollback?: (id: string) => void;
}> = ({ entry, disabled, onOpenEntry, onRollback }) => {
  // Rollback targets the deploy before an entry, so only a settled production
  // deployment can be rolled back: rolling back an in-flight (queued or
  // deploying) one, or one already rolled back, has nothing to revert to.
  const rollbackable = entry.kind === 'production' && entry.state === 'ready' && !!onRollback;
  return (
    <li className="ship-history-row" data-testid={`ship-history-row-${entry.id}`}>
      <div className="ship-history-main">
        <span className={`ship-history-state ship-history-state-${entry.state}`} data-testid="ship-history-state">
          {STATE_LABELS[entry.state]}
        </span>
        <div className="ship-history-text">
          <div className="ship-history-headline">
            <span className="ship-history-kind">{KIND_LABELS[entry.kind]}</span>
            <span className="ship-history-version" data-testid="ship-history-version">
              {entry.version}
            </span>
            {entry.planRevision ? (
              <span className="ship-history-revision" data-testid="ship-history-revision">
                {entry.planRevision}
              </span>
            ) : null}
          </div>
          {/* The change-summary slot: whatever the payload carries renders here;
              absent means the row shows the version and revision alone. */}
          {entry.summary ? (
            <p className="ship-history-summary" data-testid="ship-history-summary">
              {entry.summary}
            </p>
          ) : null}
          <span className="ship-history-time" data-testid="ship-history-time">
            {formatTimestamp(entry.createdAt)}
          </span>
        </div>
      </div>
      <div className="ship-history-actions">
        {onOpenEntry ? (
          <button
            type="button"
            className="ship-history-link"
            onClick={() => onOpenEntry(entry.id)}
            data-testid={`ship-history-open-${entry.id}`}
            title="Open this deploy"
          >
            {entry.url ? <ArrowUpRight size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
            <span>Open</span>
          </button>
        ) : null}
        {rollbackable ? (
          <button
            type="button"
            className="ship-history-rollback"
            onClick={() => onRollback?.(entry.id)}
            disabled={disabled}
            data-testid={`ship-history-rollback-${entry.id}`}
            title="Roll back to the deploy before this one"
          >
            <Undo2 size={14} aria-hidden="true" />
            <span>Roll back</span>
          </button>
        ) : null}
      </div>
    </li>
  );
};

const ShipSurface: React.FC<ShipSurfaceProps> = ({ payload }) => {
  const { live, availability, history, isDeploying, isRollingBack, error, onDeploy, onRollback, onOpenEntry } = payload;
  const busy = !!isDeploying || !!isRollingBack;

  return (
    <div className="ship-surface" data-testid="ship-surface">
      <header className="ship-surface-header">
        <h1 className="ship-surface-title">Ship</h1>
        <p className="ship-surface-subtitle">
          Deploy status for this project. Ship is a lens on the same conversation.
        </p>
      </header>

      <section className="ship-card ship-status-card" data-testid="ship-status" aria-label="Deploy status">
        <div className="ship-status-live">
          <span className="ship-status-label">Live URL</span>
          {live.url ? (
            <a
              className="ship-status-url"
              href={live.url}
              target="_blank"
              rel="noreferrer noopener"
              data-testid="ship-live-url"
            >
              {live.url}
            </a>
          ) : (
            <span className="ship-status-empty" data-testid="ship-live-url-empty">
              Not deployed yet
            </span>
          )}
        </div>
        <div className="ship-status-version">
          <span className="ship-status-label">Live version</span>
          <span className="ship-status-value" data-testid="ship-live-version">
            {live.version || '—'}
          </span>
        </div>
        <div className="ship-status-last">
          <span className="ship-status-label">Last deploy</span>
          <span className="ship-status-value" data-testid="ship-live-deployed-at">
            {formatTimestamp(live.deployedAt)}
          </span>
        </div>
        {live.state ? (
          <span className={`ship-status-state ship-history-state-${live.state}`} data-testid="ship-live-state">
            {STATE_LABELS[live.state]}
          </span>
        ) : null}
      </section>

      <section className="ship-card ship-action-card">
        <div className="ship-action-row">
          <button
            type="button"
            className="ship-deploy-button"
            onClick={onDeploy}
            disabled={busy || !availability.canDeploy || !onDeploy}
            data-testid="ship-deploy"
          >
            {isDeploying ? (
              <Loader2 size={15} className="ship-spinner" aria-hidden="true" />
            ) : (
              <Rocket size={15} aria-hidden="true" />
            )}
            <span>{isDeploying ? 'Deploying…' : 'Deploy'}</span>
          </button>
          {!availability.canDeploy && availability.blockedReason ? (
            <span className="ship-action-blocked" data-testid="ship-deploy-blocked">
              {availability.blockedReason}
            </span>
          ) : null}
        </div>
        {error ? (
          <p className="ship-action-error" role="alert" data-testid="ship-error">
            <AlertTriangle size={14} aria-hidden="true" />
            <span>{error}</span>
          </p>
        ) : null}
      </section>

      <section className="ship-card ship-history-card" aria-label="Deploy history">
        <h2 className="ship-history-title">History</h2>
        {history.length === 0 ? (
          <p className="ship-history-empty" data-testid="ship-history-empty">
            No deploys yet.
          </p>
        ) : (
          <ul className="ship-history-list" data-testid="ship-history-list">
            {history.map((entry) => (
              <HistoryRow
                key={entry.id}
                entry={entry}
                disabled={busy}
                onOpenEntry={onOpenEntry}
                onRollback={onRollback}
              />
            ))}
          </ul>
        )}
      </section>
    </div>
  );
};

export default ShipSurface;
