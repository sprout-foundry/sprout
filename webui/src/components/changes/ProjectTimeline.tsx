/**
 * ProjectTimeline — the project timeline view for the changes surface.
 *
 * Renders one row per timeline entry: a change set with its summary, a
 * deploy, or a checkpoint. Checkpoints carry a restore action. Restore is
 * destructive, so it is a two-step confirm: the first click arms a row
 * (showing an inline confirm/cancel pair) and only the explicit confirm
 * calls `onRestore`. A single click never restores.
 *
 * The component is pure: it renders from its props and calls back for
 * restore and open. No fetch, no server, so it is testable in isolation.
 */

import { Bookmark, ChevronRight, Clock, ExternalLink, GitCommit, Inbox, Rocket, Undo2 } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { formatRelativeTime } from '../../utils/format';
import {
  TIMELINE_KIND_LABELS,
  isRestorableCheckpoint,
  orderTimeline,
  restoreConfirmMessage,
  type ProjectTimelineEntry,
  type ProjectTimelineOrder,
} from './timelineModel';
import './ProjectTimeline.css';

export interface ProjectTimelineProps {
  /** The timeline entries to render. */
  entries: readonly ProjectTimelineEntry[];
  /** Sort direction. Defaults to newest-first. */
  order?: ProjectTimelineOrder;
  /**
   * Restore a checkpoint by ID. Called only after the user explicitly
   * confirms; a single click on Restore never calls this.
   */
  onRestore?: (checkpointId: string) => void;
  /** Open/select an entry (e.g. view its diff or details). */
  onOpen?: (entry: ProjectTimelineEntry) => void;
  /** Optional message shown when the timeline is empty. */
  emptyMessage?: string;
}

function kindIcon(kind: ProjectTimelineEntry['kind']) {
  switch (kind) {
    case 'change_set':
      return <GitCommit size={14} className="ptl-icon ptl-icon--change_set" />;
    case 'deploy':
      return <Rocket size={14} className="ptl-icon ptl-icon--deploy" />;
    case 'checkpoint':
      return <Bookmark size={14} className="ptl-icon ptl-icon--checkpoint" />;
    default:
      return <GitCommit size={14} className="ptl-icon" />;
  }
}

function FileSummary({ entry }: { entry: ProjectTimelineEntry }) {
  if (entry.kind !== 'change_set' || !entry.files || entry.files.length === 0) return null;
  const named = entry.files.slice(0, 3);
  const extra = entry.files.length - named.length;
  return (
    <span className="ptl-files" title={entry.files.join('\n')}>
      {named.join(', ')}
      {extra > 0 ? ` +${extra} more` : ''}
    </span>
  );
}

function ProjectTimeline({
  entries,
  order = 'newest_first',
  onRestore,
  onOpen,
  emptyMessage = 'No timeline entries yet.',
}: ProjectTimelineProps): JSX.Element {
  const ordered = useMemo(() => orderTimeline(entries, order), [entries, order]);
  // The armed (first-click) checkpoint pending confirmation, if any. One
  // armed row at a time keeps the destructive step explicit and local.
  const [armedId, setArmedId] = useState<string | null>(null);

  const handleRestoreClick = useCallback((id: string) => {
    setArmedId(id);
  }, []);

  const handleConfirm = useCallback(
    (id: string) => {
      setArmedId(null);
      onRestore?.(id);
    },
    [onRestore],
  );

  if (ordered.length === 0) {
    return (
      <div className="project-timeline project-timeline--empty" data-testid="project-timeline">
        <div className="ptl-empty">
          <Inbox size={32} />
          <p>{emptyMessage}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="project-timeline" data-testid="project-timeline">
      <ol className="ptl-list">
        {ordered.map((entry) => {
          const restorable = isRestorableCheckpoint(entry);
          const armed = armedId === entry.id;
          return (
            <li key={`${entry.kind}-${entry.id}`} className={`ptl-row ptl-row--${entry.kind}`} data-testid="ptl-row">
              <span className="ptl-gutter">{kindIcon(entry.kind)}</span>
              <div className="ptl-body">
                <div className="ptl-head">
                  <span className={`ptl-kind ptl-kind--${entry.kind}`}>{TIMELINE_KIND_LABELS[entry.kind]}</span>
                  {entry.kind === 'deploy' && entry.deployKind && <span className="ptl-chip">{entry.deployKind}</span>}
                  {entry.kind === 'checkpoint' && entry.origin && <span className="ptl-chip">{entry.origin}</span>}
                  <span className="ptl-time" title={entry.timestamp}>
                    <Clock size={11} /> {formatRelativeTime(entry.timestamp)}
                  </span>
                </div>
                <div className="ptl-summary-line">
                  {onOpen ? (
                    <button
                      type="button"
                      className="ptl-summary ptl-summary--button"
                      onClick={() => onOpen(entry)}
                      title={entry.summary}
                    >
                      {entry.summary}
                    </button>
                  ) : (
                    <span className="ptl-summary" title={entry.summary}>
                      {entry.summary}
                    </span>
                  )}
                  <FileSummary entry={entry} />
                  {entry.kind === 'deploy' && entry.url && (
                    <a className="ptl-url" href={entry.url} target="_blank" rel="noreferrer" title={entry.url}>
                      <ExternalLink size={11} /> {entry.url}
                    </a>
                  )}
                </div>
                {restorable && !armed && onRestore && (
                  <div className="ptl-actions">
                    <button
                      type="button"
                      className="ptl-restore-btn"
                      data-testid="ptl-restore"
                      onClick={() => handleRestoreClick(entry.id)}
                      title="Restore the project to this checkpoint"
                    >
                      <Undo2 size={12} /> Restore
                    </button>
                  </div>
                )}
                {restorable && armed && (
                  <div className="ptl-actions ptl-actions--confirm" data-testid="ptl-restore-confirm">
                    <span className="ptl-confirm-msg">{restoreConfirmMessage(entry)}</span>
                    <button
                      type="button"
                      className="ptl-confirm-btn"
                      data-testid="ptl-restore-confirm-yes"
                      onClick={() => handleConfirm(entry.id)}
                    >
                      <Undo2 size={12} /> Yes, restore
                    </button>
                    <button
                      type="button"
                      className="ptl-cancel-btn"
                      data-testid="ptl-restore-cancel"
                      onClick={() => setArmedId(null)}
                    >
                      Cancel
                    </button>
                  </div>
                )}
              </div>
              {onOpen && <ChevronRight size={14} className="ptl-open-affordance" aria-hidden="true" />}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

export default ProjectTimeline;
