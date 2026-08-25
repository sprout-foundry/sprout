import { Collapsible } from '@sprout/ui';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { clientFetch } from '../services/clientSession';
import './EditApprovalPanel.css';

export interface EditApprovalHunkLine {
  type: 'context' | 'add' | 'remove';
  content: string;
}

export interface EditApprovalHunk {
  id: string;
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  lines: EditApprovalHunkLine[];
  addCount: number;
  delCount: number;
}

export interface EditApprovalPanelProps {
  requestId: string;
  filePath: string;
  unifiedDiff?: string;
  hunks: EditApprovalHunk[];
  onRespond: (requestId: string) => void;
}

const LINE_PREFIX: Record<EditApprovalHunkLine['type'], string> = {
  add: '+',
  remove: '-',
  context: ' ',
};

/**
 * EditApprovalPanel (SP-072-3) — renders a pending edit's diff with
 * per-hunk Accept/Reject toggles and color-coded add/remove/context lines.
 *
 * Driven by the `edit_approval_request` WebSocket event, which populates
 * the hunks array with per-line change types. On decision, POSTs to
 * /api/edits/{id}/decision with the accepted hunk IDs.
 */
function EditApprovalPanel({
  requestId,
  filePath,
  unifiedDiff,
  hunks,
  onRespond,
}: EditApprovalPanelProps): JSX.Element {
  // Track accepted hunk IDs. Default: all accepted so the user can
  // just hit "Apply Selected" to approve everything. Unchecking a
  // hunk's checkbox removes it from the accepted set.
  const [acceptedIds, setAcceptedIds] = useState<Set<string>>(() => new Set(hunks.map((h) => h.id)));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If the hunks change (new request arrives via state), reset selections.
  useEffect(() => {
    setAcceptedIds(new Set(hunks.map((h) => h.id)));
    setError(null);
  }, [requestId, hunks]);

  const toggleHunk = useCallback((hunkId: string) => {
    setAcceptedIds((prev) => {
      const next = new Set(prev);
      if (next.has(hunkId)) {
        next.delete(hunkId);
      } else {
        next.add(hunkId);
      }
      return next;
    });
  }, []);

  const acceptAll = useCallback(() => {
    setAcceptedIds(new Set(hunks.map((h) => h.id)));
  }, [hunks]);

  const rejectAll = useCallback(() => {
    setAcceptedIds(new Set());
  }, []);

  const postDecision = useCallback(
    async (acceptedHunks: string[], rejected: boolean) => {
      setSubmitting(true);
      setError(null);
      try {
        const resp = await clientFetch(`/api/edits/${encodeURIComponent(requestId)}/decision`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ accepted_hunks: acceptedHunks, rejected }),
        });
        if (!resp.ok) {
          const body = await resp.text();
          throw new Error(body || `HTTP ${resp.status}`);
        }
        onRespond(requestId);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setSubmitting(false);
      }
    },
    [requestId, onRespond],
  );

  const handleApplySelected = useCallback(() => {
    void postDecision([...acceptedIds], false);
  }, [acceptedIds, postDecision]);

  const handleRejectAll = useCallback(() => {
    void postDecision([], true);
  }, [postDecision]);

  const totalAdds = useMemo(() => hunks.reduce((sum, h) => sum + h.addCount, 0), [hunks]);
  const totalDels = useMemo(() => hunks.reduce((sum, h) => sum + h.delCount, 0), [hunks]);

  return (
    <div className="themed-dialog-overlay edit-approval-overlay" role="dialog" aria-modal="true">
      <div className="themed-dialog-card edit-approval-card">
        <div className="themed-dialog-accent-bar themed-dialog-accent-bar--warning" />
        <div className="edit-approval-header">
          <h2 className="edit-approval-title">Edit Approval Required</h2>
          <div className="edit-approval-file-info">
            <span className="edit-approval-file-path" title={filePath}>
              {filePath}
            </span>
            <span className="edit-approval-stats">
              {hunks.length} {hunks.length === 1 ? 'hunk' : 'hunks'} ·{' '}
              <span className="edit-approval-add">+{totalAdds}</span>{' '}
              <span className="edit-approval-del">-{totalDels}</span>
            </span>
          </div>
        </div>

        <div className="edit-approval-actions-top">
          <button type="button" className="edit-approval-link-btn" onClick={acceptAll} disabled={submitting}>
            Accept all
          </button>
          <span className="edit-approval-sep">·</span>
          <button type="button" className="edit-approval-link-btn" onClick={rejectAll} disabled={submitting}>
            Reject all
          </button>
        </div>

        <div className="edit-approval-diff-body">
          {unifiedDiff && (
            // SP-101-Phase 3 (AUDIT-GAP-1): the unified-diff toggle is
            // now a <Collapsible variant="flush">. The legacy
            // `edit-approval-raw-diff` class is preserved on the
            // container so the existing typography (smaller font,
            // tertiary color) stays consistent with the surrounding
            // diff body.
            <Collapsible
              title="Unified diff"
              variant="flush"
              className="edit-approval-raw-diff"
              ariaLabel="Unified diff"
            >
              <pre className="edit-approval-raw-diff-pre">{unifiedDiff}</pre>
            </Collapsible>
          )}
          {hunks.map((hunk) => {
            const accepted = acceptedIds.has(hunk.id);
            return (
              <div key={hunk.id} className={`edit-approval-hunk ${accepted ? 'is-accepted' : 'is-rejected'}`}>
                <div className="edit-approval-hunk-header">
                  <label className="edit-approval-hunk-label">
                    <input
                      type="checkbox"
                      checked={accepted}
                      onChange={() => toggleHunk(hunk.id)}
                      disabled={submitting}
                    />
                    <span className="edit-approval-hunk-id">{hunk.id}</span>
                    <span className="edit-approval-hunk-lines">
                      lines {hunk.oldStart}–{hunk.oldStart + Math.max(hunk.oldLines - 1, 0)}
                    </span>
                    <span className="edit-approval-hunk-counts">
                      <span className="edit-approval-add">+{hunk.addCount}</span>{' '}
                      <span className="edit-approval-del">-{hunk.delCount}</span>
                    </span>
                  </label>
                </div>
                <div className="edit-approval-hunk-code">
                  {hunk.lines.map((line, i) => (
                    <div key={i} className={`edit-approval-line edit-approval-line--${line.type}`}>
                      <span className="edit-approval-line-prefix">{LINE_PREFIX[line.type]}</span>
                      <span className="edit-approval-line-content">{line.content}</span>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>

        {error && <div className="edit-approval-error">{error}</div>}

        <div className="edit-approval-footer">
          <span className="edit-approval-selected-count">
            {acceptedIds.size}/{hunks.length} hunks selected
          </span>
          <div className="edit-approval-footer-actions">
            <button
              type="button"
              className="edit-approval-btn edit-approval-btn--reject"
              onClick={handleRejectAll}
              disabled={submitting}
            >
              Reject All
            </button>
            <button
              type="button"
              className="edit-approval-btn edit-approval-btn--apply"
              onClick={handleApplySelected}
              disabled={submitting || acceptedIds.size === 0}
            >
              {submitting ? 'Applying…' : 'Apply Selected'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default EditApprovalPanel;
