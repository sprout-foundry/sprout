/**
 * FeedbackLibrary — the §8a Library group's third kind view: a global queue
 * over every `design/feedback/*.json` (SP-140-8's one §8a holdout).
 *
 * Where feedback lived before stays: per-screen in the workbench (§8b facet
 * 3) and workspace-wide on the health strip's pending list. This view is the
 * library-shaped answer — one row per feedback file with target, status,
 * open-annotation count, and click-through to the target screen's workbench
 * (the rail selects; the screens section renders the workbench whose facet 3
 * carries resolution). Rows with no open annotations sit under a resolved
 * divider, not hidden — the queue's history is part of the contract.
 */

import { useMemo } from 'react';
import type { DesignFeedbackEntry } from '../../services/api/types';

interface FeedbackLibraryProps {
  /** The design inventory's feedback rows (one per design/feedback/*.json). */
  feedback: DesignFeedbackEntry[];
  /** Select an asset (the feedback's target screen) in the shared selection. */
  onSelectAsset: (path: string) => void;
  /** Switch the section (to the screens workbench). */
  onSelectTab: (tab: 'flows' | 'screens' | 'tokens') => void;
}

const STATUS_LABELS: Record<string, string> = {
  'changes-requested': 'Changes requested',
  approved: 'Approved',
  acknowledged: 'Acknowledged',
};

export default function FeedbackLibrary({ feedback, onSelectAsset, onSelectTab }: FeedbackLibraryProps) {
  const { pending, resolved } = useMemo(() => {
    const sorted = [...feedback].sort((a, b) => a.name.localeCompare(b.name));
    return {
      pending: sorted.filter((f) => f.annotationCount > f.resolvedCount),
      resolved: sorted.filter((f) => f.annotationCount <= f.resolvedCount),
    };
  }, [feedback]);

  const openTarget = (name: string) => {
    // The feedback file's stem IS the target screen's stem (§4d contract);
    // selecting the screen and switching to the screens section lands the
    // workbench whose feedback facet owns resolution.
    onSelectAsset(`design/screens/${name}.html`);
    onSelectTab('screens');
  };

  const row = (entry: DesignFeedbackEntry) => {
    const open = entry.annotationCount - entry.resolvedCount;
    return (
      <li key={entry.path} className="feedback-library-row">
        <button
          type="button"
          className="feedback-library-target"
          data-testid={`feedback-library-row-${entry.name}`}
          onClick={() => openTarget(entry.name)}
          title={`Open ${entry.name}\u2019s workbench`}
        >
          <span className="feedback-library-name">{entry.name}</span>
          <span className={`feedback-library-status feedback-library-status--${entry.status || 'none'}`}>
            {STATUS_LABELS[entry.status] ?? entry.status ?? 'No status'}
          </span>
          <span className="feedback-library-count">
            {open > 0 ? `${open} open` : entry.annotationCount > 0 ? 'all resolved' : 'no annotations'}
          </span>
        </button>
      </li>
    );
  };

  if (feedback.length === 0) {
    return (
      <div className="feedback-library feedback-library--empty" data-testid="feedback-library-empty">
        <p className="feedback-library-empty-title">No feedback files yet</p>
        <p className="feedback-library-empty-sub">
          Human annotations land in design/feedback/&lt;stem&gt;.json — added from a screen&rsquo;s workbench (Add
          feedback) or the Design detail pane.
        </p>
      </div>
    );
  }

  return (
    <div className="feedback-library" data-testid="feedback-library" aria-label="Feedback queue">
      {pending.length > 0 ? (
        <>
          <h3 className="feedback-library-heading">
            Pending <span className="feedback-library-count-badge">{pending.length}</span>
          </h3>
          <ul className="feedback-library-list">{pending.map(row)}</ul>
        </>
      ) : (
        <p className="feedback-library-allclear" data-testid="feedback-library-allclear">
          All feedback resolved — nothing pending.
        </p>
      )}
      {resolved.length > 0 ? (
        <details className="feedback-library-resolved">
          <summary className="feedback-library-heading">
            Resolved <span className="feedback-library-count-badge">{resolved.length}</span>
          </summary>
          <ul className="feedback-library-list">{resolved.map(row)}</ul>
        </details>
      ) : null}
    </div>
  );
}
