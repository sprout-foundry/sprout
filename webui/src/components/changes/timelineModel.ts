/**
 * timelineModel — the typed shape and pure helpers for the project
 * timeline rendered in the changes surface.
 *
 * The timeline is a join of three recorded domains (change sets,
 * deploys, checkpoints). It is supplied to the UI as a typed prop rather
 * than fetched here: the component layer stays pure and testable without
 * a server, and the app owns whatever transport eventually feeds it.
 *
 * The shape mirrors the backend timeline entry: a kind tag
 * (`change_set` | `deploy` | `checkpoint`), a timestamp, a short summary,
 * and the fields specific to the kind.
 */

export type ProjectTimelineKind = 'change_set' | 'deploy' | 'checkpoint';

/** Fields a change-set entry carries: the files it touched and the plan
 *  scope IDs it pertains to (both optional). */
export interface ProjectChangeSetEntry {
  kind: 'change_set';
  /** Stable identity (the revision's hash). */
  id: string;
  /** ISO-8601 timestamp. */
  timestamp: string;
  /** Short one-line summary of the change set's diff. */
  summary: string;
  /** Files the change set touched, sorted. */
  files?: string[];
  /** Plan scope IDs the change set pertains to. */
  scopeIds?: string[];
}

/** Fields a deploy entry carries. */
export interface ProjectDeployEntry {
  kind: 'deploy';
  /** Stable identity. */
  id: string;
  /** ISO-8601 timestamp. */
  timestamp: string;
  /** Short one-line summary of the deploy. */
  summary: string;
  /** Deploy target kind, e.g. `production` or `preview`. */
  deployKind?: string;
  /** The deployed version, when the target reports one. */
  version?: string;
  /** A URL the deploy is reachable at, when the target reports one. */
  url?: string;
}

/** Fields a checkpoint entry carries. A restore is itself a checkpoint
 *  (origin `restore`), so it appears on the timeline like any other. */
export interface ProjectCheckpointEntry {
  kind: 'checkpoint';
  /** The checkpoint's stable identity — what a restore acts on. */
  id: string;
  /** ISO-8601 timestamp. */
  timestamp: string;
  /** Short one-line summary of the checkpoint. */
  summary: string;
  /** Why the checkpoint exists: verification, deploy, manual, or restore. */
  origin?: string;
  /** The revision the checkpoint captured, matching the backend's
   *  `revision_id` field. Absent when nothing was captured. */
  revisionId?: string;
}

export type ProjectTimelineEntry = ProjectChangeSetEntry | ProjectDeployEntry | ProjectCheckpointEntry;

/** Sort direction for the timeline. Newest-first is the default the UI
 *  shows, matching how people scan recent history. */
export type ProjectTimelineOrder = 'oldest_first' | 'newest_first';

/** Label shown for each entry kind. */
export const TIMELINE_KIND_LABELS: Record<ProjectTimelineKind, string> = {
  change_set: 'Change set',
  deploy: 'Deploy',
  checkpoint: 'Checkpoint',
};

/**
 * A checkpoint can only be restored when it captured a revision — a
 * checkpoint with nothing captured (e.g. taken in an empty project) has
 * no state to return to, so its restore action is withheld rather than
 * offered and then failing.
 */
export function isRestorableCheckpoint(entry: ProjectTimelineEntry): entry is ProjectCheckpointEntry {
  return entry.kind === 'checkpoint' && typeof entry.revisionId === 'string' && entry.revisionId.length > 0;
}

/**
 * Sort timeline entries by timestamp in the requested order. The sort is
 * stable and array-copying: equal timestamps keep their input order (the
 * backend already applies a deterministic tie-break), and the caller's
 * array is never mutated.
 */
export function orderTimeline(
  entries: readonly ProjectTimelineEntry[],
  order: ProjectTimelineOrder = 'newest_first',
): ProjectTimelineEntry[] {
  const copy = entries.slice();
  copy.sort((a, b) => {
    const ta = Date.parse(a.timestamp);
    const tb = Date.parse(b.timestamp);
    if (Number.isNaN(ta) || Number.isNaN(tb) || ta === tb) return 0;
    return order === 'newest_first' ? tb - ta : ta - tb;
  });
  return copy;
}

/**
 * The restore confirm copy. Restoring returns the project to the state a
 * checkpoint captured and cannot be undone in place, so the message says
 * plainly what will happen.
 */
export function restoreConfirmMessage(entry: ProjectCheckpointEntry): string {
  const target = entry.revisionId ? `revision ${entry.revisionId}` : 'the captured state';
  return `Restore the project to ${target}? Files changed since the checkpoint will be reverted. The restore is recorded on the timeline, so nothing is lost.`;
}
