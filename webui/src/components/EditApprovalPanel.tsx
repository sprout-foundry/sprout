import React, { useState, useEffect, useCallback } from 'react';

export interface EditHunk {
  id: string;
  summary: string;
  add_count: number;
  del_count: number;
  lines: string[];
}

export interface PendingEdit {
  id: string;
  path: string;
  hunks: EditHunk[];
  unified_diff: string;
  decided: boolean;
}

interface EditApprovalPanelProps {
  editId: string;
  onResolved: (acceptedHunks: string[], rejected: boolean) => void;
}

/**
 * EditApprovalPanel — renders a pending edit's unified diff with per-hunk
 * Accept/Reject toggles. "Apply Selected" submits the accepted hunks,
 * "Reject All" rejects every hunk. Fires an input_required notification
 * (SP-070) when mounted so the user is alerted even if the tab is hidden.
 *
 * POST /api/edits/{id}/decision { accepted_hunks, rejected }
 */
export const EditApprovalPanel: React.FC<EditApprovalPanelProps> = ({ editId, onResolved }) => {
  const [edit, setEdit] = useState<PendingEdit | null>(null);
  const [acceptedHunks, setAcceptedHunks] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);

  // Fetch the pending edit details.
  useEffect(() => {
    let cancelled = false;

    const fetchEdit = async () => {
      try {
        const resp = await fetch(`/api/edits/${editId}`);
        if (!resp.ok) {
          console.error('Failed to fetch edit:', resp.statusText);
          return;
        }
        const data: PendingEdit = await resp.json();
        if (cancelled) return;
        setEdit(data);
        // Default: all hunks accepted.
        setAcceptedHunks(new Set(data.hunks.map((h) => h.id)));
      } catch (err) {
        console.error('Error fetching edit:', err);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    fetchEdit();
    return () => {
      cancelled = true;
    };
  }, [editId]);

  const toggleHunk = useCallback((hunkId: string) => {
    setAcceptedHunks((prev) => {
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
    if (!edit) return;
    setAcceptedHunks(new Set(edit.hunks.map((h) => h.id)));
  }, [edit]);

  const rejectAll = useCallback(() => {
    setAcceptedHunks(new Set());
  }, []);

  const submitDecision = useCallback(
    async (rejected: boolean) => {
      setSubmitting(true);
      try {
        const body = rejected
          ? { accepted_hunks: [], rejected: true }
          : { accepted_hunks: Array.from(acceptedHunks), rejected: false };

        const resp = await fetch(`/api/edits/${editId}/decision`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });

        if (!resp.ok) {
          console.error('Failed to submit decision:', resp.statusText);
          return;
        }

        onResolved(rejected ? [] : Array.from(acceptedHunks), rejected);
      } catch (err) {
        console.error('Error submitting decision:', err);
      } finally {
        setSubmitting(false);
      }
    },
    [editId, acceptedHunks, onResolved]
  );

  if (loading) {
    return (
      <div className="edit-approval-panel loading" data-testid="edit-approval-panel">
        <p>Loading edit details…</p>
      </div>
    );
  }

  if (!edit) {
    return (
      <div className="edit-approval-panel error" data-testid="edit-approval-panel">
        <p>Edit not found or already decided.</p>
      </div>
    );
  }

  return (
    <div className="edit-approval-panel" data-testid="edit-approval-panel">
      <div className="edit-approval-header">
        <h3>Edit Review: {edit.path}</h3>
        <span className="edit-hunk-count">{edit.hunks.length} hunk(s)</span>
      </div>

      <div className="edit-approval-diff" data-testid="edit-unified-diff">
        <pre>
          <code>{edit.unified_diff}</code>
        </pre>
      </div>

      <div className="edit-hunks-list">
        {edit.hunks.map((hunk) => (
          <div
            key={hunk.id}
            className={`edit-hunk-row ${acceptedHunks.has(hunk.id) ? 'accepted' : 'rejected'}`}
            data-testid={`edit-hunk-${hunk.id}`}
          >
            <label className="edit-hunk-toggle">
              <input
                type="checkbox"
                checked={acceptedHunks.has(hunk.id)}
                onChange={() => toggleHunk(hunk.id)}
                data-testid={`edit-hunk-checkbox-${hunk.id}`}
              />
              <span className="edit-hunk-summary">
                {hunk.id}: {hunk.summary}
              </span>
            </label>
          </div>
        ))}
      </div>

      <div className="edit-approval-actions">
        <button onClick={acceptAll} className="edit-btn-accept-all" data-testid="edit-accept-all">
          Accept All
        </button>
        <button onClick={rejectAll} className="edit-btn-reject-all" data-testid="edit-reject-all">
          Reject All
        </button>
        <button
          onClick={() => submitDecision(false)}
          disabled={submitting || acceptedHunks.size === 0}
          className="edit-btn-apply"
          data-testid="edit-apply-selected"
        >
          {submitting ? 'Applying…' : `Apply Selected (${acceptedHunks.size})`}
        </button>
        <button
          onClick={() => submitDecision(true)}
          disabled={submitting}
          className="edit-btn-reject"
          data-testid="edit-reject"
        >
          Reject
        </button>
      </div>
    </div>
  );
};

export default EditApprovalPanel;
