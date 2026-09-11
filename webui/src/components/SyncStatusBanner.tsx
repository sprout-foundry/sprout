import { useCallback, useEffect, useState } from 'react';
import { RefreshCw, Download, GitCompareArrows, X } from 'lucide-react';
import { clientFetch } from '../services/clientSession';
import { getBootstrapSync } from '../bootstrapAdapter';
import type { GitSyncReport } from '../types/runtimeConfig';
import './SyncStatusBanner.css';

const DISMISSAL_KEY = 'sprout:sync-banner-dismissed';

/**
 * SyncStatusBanner — ETH-1 sync-on-resume surface.
 *
 * Shows the workspace git snapshot taken at boot (`sync` field of
 * /api/bootstrap, surfaced through getBootstrapSync()): dirty files,
 * ahead/behind, last commit. "Refresh" re-reads the live state via
 * GET /api/sync (status only — never pulls); "Pull" runs POST /api/sync,
 * the non-destructive `git pull --ff-only` reconcile. Hidden entirely when
 * the workspace is clean and in sync, or once dismissed for the session.
 */
const SyncStatusBanner: React.FC = () => {
  const [report, setReport] = useState<GitSyncReport | null>(() => getBootstrapSync());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState(() => {
    try {
      return sessionStorage.getItem(DISMISSAL_KEY) === '1';
    } catch {
      return false;
    }
  });

  const fetchReport = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await clientFetch('/api/sync');
      if (!res.ok) {
        setError(`Sync status failed (HTTP ${res.status})`);
        return;
      }
      setReport((await res.json()) as GitSyncReport);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  const handlePull = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await clientFetch('/api/sync', { method: 'POST' });
      if (!res.ok) {
        setError(`Pull failed (HTTP ${res.status})`);
        return;
      }
      setReport((await res.json()) as GitSyncReport);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  // Re-read live state once when the backend connection is available — the
  // boot snapshot can be stale by the time the user looks at it.
  useEffect(() => {
    void fetchReport();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleDismiss = useCallback(() => {
    setDismissed(true);
    try {
      sessionStorage.setItem(DISMISSAL_KEY, '1');
    } catch {
      // sessionStorage unavailable — dismissal is session-scoped anyway
    }
  }, []);

  if (dismissed) return null;

  const dirtyCount = report?.dirty_files.length ?? 0;
  const ahead = report?.ahead ?? 0;
  const behind = report?.behind ?? 0;
  // Hide entirely when there is nothing to reconcile: clean repo, or the
  // workspace is not a git repository at all.
  const clean = report !== null && (!report.in_git_repo || (dirtyCount === 0 && ahead === 0 && behind === 0));
  if (clean && !error) return null;

  const parts: string[] = [];
  if (dirtyCount > 0) parts.push(`${dirtyCount} uncommitted file${dirtyCount === 1 ? '' : 's'}`);
  if (ahead > 0) parts.push(`${ahead} ahead`);
  if (behind > 0) parts.push(`${behind} behind`);

  return (
    <div className="sync-status-banner" role="status">
      <GitCompareArrows size={14} className="sync-status-banner-icon" />
      <div className="sync-status-banner-text">
        {error ? (
          <span className="sync-status-banner-error">Sync check failed: {error}</span>
        ) : (
          <>
            <span className="sync-status-banner-branch">{report?.branch || 'git'}</span>
            <span className="sync-status-banner-detail">
              Workspace not in sync — {parts.join(', ')}
              {report?.last_commit.sha ? ` · last commit ${report.last_commit.sha.slice(0, 7)}` : ''}
            </span>
          </>
        )}
      </div>
      <button
        className="sync-status-banner-btn"
        onClick={() => void fetchReport()}
        disabled={busy}
        title="Re-read git status"
      >
        <RefreshCw size={12} className={busy ? 'spin' : ''} /> Refresh
      </button>
      <button
        className="sync-status-banner-btn primary"
        onClick={() => void handlePull()}
        disabled={busy}
        title="Non-destructive git pull --ff-only (skipped while the tree is dirty)"
      >
        <Download size={12} /> Pull
      </button>
      <button className="sync-status-banner-close" onClick={handleDismiss} aria-label="Dismiss sync status">
        <X size={12} />
      </button>
    </div>
  );
};

export default SyncStatusBanner;
