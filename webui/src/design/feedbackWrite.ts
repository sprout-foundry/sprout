/**
 * Feedback write-path model (SP-140-3 §3e, SP-140-4 §4d).
 *
 * Pure functions only — no React, no DOM, no I/O. They own the two rules the
 * feedback affordance needs: what the annotation's `target` is (the selected
 * design asset, recorded workspace-relative with the `design/` prefix) and
 * what the SP-140-4d document written for it looks like.
 *
 * The document is the canonical `DesignFeedbackFile` (`services/api/types`) —
 * this module never redefines the schema, it only fills it: every field the
 * §4d shape declares is present, including the top-level `resolution` note and
 * the per-annotation `resolved` flag that the later resolution flow (SP-140-4
 * item 4.8) sets from this same pane.
 *
 * §3e scope: the write path and the file schema land here.
 * §4d (item 4.8) scope: the resolution helpers — toggling an annotation's
 * `resolved` flag and writing the top-level `resolution` note — are pure
 * transforms over a parsed document, so they live here beside the builder and
 * the component only wires them to the read/write seam.
 */

import type { DesignFeedbackAnnotation, DesignFeedbackFile } from '../services/api/types';

/** The path prefix that separates the workspace-relative asset path from its design-root-relative form. */
const DESIGN_PREFIX = 'design/';

/** Normalise a path to workspace-relative POSIX form (the pane's `path` prop's shape). */
export function normaliseAssetPath(path: string | null | undefined): string {
  return (path ?? '').replace(/\\/g, '/').replace(/^\.\//, '');
}

/**
 * The workspace-relative target recorded in `design/feedback/<target>.json`:
 * the selected asset path with the `design/` prefix restored, so an asset
 * handed in either form (`design/screens/login.html` or `screens/login.html`)
 * records one stable target. A missing selection has no target.
 */
export function feedbackTarget(path: string | null | undefined): string {
  const normalized = normaliseAssetPath(path);
  if (!normalized) return '';
  return normalized.startsWith(DESIGN_PREFIX) ? normalized : `${DESIGN_PREFIX}${normalized}`;
}

/**
 * The stem `writeFeedback` keys the file on: the target's last path segment,
 * without its extension, so `design/screens/login.html` and
 * `design/wireframes/login.svg` both land in `design/feedback/login.json`
 * (§3e: the detail pane's annotation, the designApi's existing stem handling).
 */
export function feedbackStem(target: string): string {
  const basename = normaliseAssetPath(target).split('/').pop() ?? '';
  const dot = basename.lastIndexOf('.');
  return dot > 0 ? basename.slice(0, dot) : basename;
}

/** The feedback file's own path, i.e. what `writeFeedback` resolves the target to. */
export function feedbackFilePath(target: string): string {
  const stem = feedbackStem(target);
  return stem ? `${DESIGN_PREFIX}feedback/${stem}.json` : '';
}

/**
 * Pending marker for an annotation the human just wrote but the agent has not
 * read (§4d: `changes-requested` is the status the agent's loop starts on).
 */
export const PENDING_FEEDBACK_STATUS = 'changes-requested';

/** `at` coordinates are normalized 0–1 (§4d) — resolution-independent. */
export const DEFAULT_ANNOTATION_POINT = { x: 0.5, y: 0.5 };

/**
 * Annotation areas offered by the affordance: the §4a critique rubric's
 * vocabulary (hierarchy, affordance, consistency, spacing, contrast, touch
 * targets). A stub's picker — SP-140-4's annotation UI colours the selection
 * by area instead.
 */
export const FEEDBACK_AREAS = [
  'hierarchy',
  'affordance',
  'consistency',
  'spacing',
  'contrast',
  'touch-targets',
] as const;

/** Stable annotation id: `a1`, `a2`, … in creation order (the §4d example's spelling). */
export function annotationId(index: number): string {
  return `a${Math.max(1, Math.floor(index))}`;
}

/**
 * The document written for a new annotation (SP-140-4 §4d): canonical
 * `DesignFeedbackFile` with all fields present — `status` pending, `resolution`
 * empty for the agent to fill when it closes the loop, and the annotation
 * carrying `resolved: false` plus its creation timestamp.
 *
 * `at` defaults to the asset's centre because the affordance is a stub with no
 * drawing surface yet (§3e); the area picker and the note are what the user
 * chooses here.
 */
export function buildFeedbackFile(
  target: string,
  note: string,
  area: string,
  created: string,
  at: { x: number; y: number } = DEFAULT_ANNOTATION_POINT,
): DesignFeedbackFile {
  return {
    target,
    status: PENDING_FEEDBACK_STATUS,
    resolution: '',
    annotations: [
      {
        id: annotationId(1),
        at: { x: at.x, y: at.y },
        area,
        note,
        resolved: false,
        created,
      },
    ],
  };
}

/* -------------------------------------------------------------------------- */
/* Resolution flow (§4d, SP-140-4 item 4.8) — pure transforms over a document  */
/* -------------------------------------------------------------------------- */

/** The status the agent writes when it closes the loop (§4d). */
export const RESOLVED_FEEDBACK_STATUS = 'resolved';

/**
 * A §4d annotation with an id and a note is what the resolution flow lists and
 * toggles; anything else (a stray blank entry in a hand-edited file) is not a
 * resolvable annotation and is ignored rather than rendered as a ghost row.
 */
export function isResolvableAnnotation(annotation: DesignFeedbackAnnotation | undefined | null): boolean {
  return Boolean(annotation && annotation.id && annotation.note);
}

/** How many annotations the document carries and how many are `resolved` (§4d). */
export function annotationCounts(file: DesignFeedbackFile): { total: number; resolved: number } {
  const annotations = file.annotations ?? [];
  return {
    total: annotations.length,
    resolved: annotations.filter((a) => a.resolved === true).length,
  };
}

/** True when every annotation is `resolved` (a vacuously-true empty list included). */
export function isFullyResolved(file: DesignFeedbackFile): boolean {
  return (file.annotations ?? []).every((a) => a.resolved === true);
}

/**
 * Toggle one annotation's `resolved` flag (§4d: each annotation can be marked
 * resolved from the detail pane, and back). Pure and non-mutating — the caller
 * hands the result straight to `designApi.writeFeedback`.
 *
 * The status stays the file's own unless the toggle flips the whole document
 * into or out of a fully-resolved state, in which case it is derived (see
 * `deriveFeedbackStatus`): resolving the last open annotation flips the file to
 * `resolved`, and un-resolving one reopens it to `changes-requested`.
 */
export function toggleAnnotationResolved(file: DesignFeedbackFile, annotationIdToToggle: string): DesignFeedbackFile {
  const annotations = (file.annotations ?? []).map((annotation) =>
    annotation.id === annotationIdToToggle ? { ...annotation, resolved: !annotation.resolved } : annotation,
  );
  const next: DesignFeedbackFile = { ...file, annotations };
  return { ...next, status: deriveFeedbackStatus(next, file.status) };
}

/**
 * The `target` argument `designApi.writeFeedback` expects: the file stem, not
 * the asset path. `writeFeedback` keys its URL on `target.replace(/\.json$/,
 * '')`, so handing it the full asset path would write
 * `design/feedback/design/screens/login.html.json` — the asset path is only
 * for the API's read path (`readFeedback`), which stems it itself.
 *
 * Kept beside the other path rules so the write seam's unit is one spelling.
 */
export function feedbackWriteTarget(target: string): string {
  return feedbackStem(target);
}

/**
 * Set the top-level `resolution` note (§4d: the agent closes the loop by
 * summarizing the changes it made). Pure; trims the note so a blank submission
 * records no note — never a whitespace string. The status is derived by the
 * caller (`deriveFeedbackStatus`), which is what makes a note on a fully
 * resolved document close it and a note on an open one leave it pending.
 */
export function withResolutionNote(file: DesignFeedbackFile, resolution: string): DesignFeedbackFile {
  return { ...file, resolution: resolution.trim() };
}

/**
 * The status a document should carry given its annotations and note (§4d):
 * `resolved` once there is nothing left to resolve — every annotation is
 * `resolved`, or the file never had annotations and a resolution note has been
 * written — and `changes-requested` while any annotation is still open.
 *
 * While work is open the pending marker wins over `currentStatus`: a document
 * with unresolved annotations *is* `changes-requested`, whatever it said
 * before, so a stale `resolved` (an annotation re-opened) can never survive a
 * toggle. A document that never had a status and is not closable falls back to
 * the pending marker.
 */
export function deriveFeedbackStatus(file: DesignFeedbackFile, currentStatus: string): string {
  const annotations = file.annotations ?? [];
  const hasNote = file.resolution.trim().length > 0;
  const ableToClose = annotations.length === 0 ? hasNote : isFullyResolved(file);
  if (ableToClose) return RESOLVED_FEEDBACK_STATUS;
  if (annotations.some((a) => a.resolved !== true)) return PENDING_FEEDBACK_STATUS;
  return currentStatus || PENDING_FEEDBACK_STATUS;
}

/**
 * Apply a resolution-note edit and derive the resulting status in one step —
 * the form's save path. Equivalent to
 * `deriveFeedbackStatus(withResolutionNote(file, note), file.status)`.
 */
export function applyResolutionNote(file: DesignFeedbackFile, resolution: string): DesignFeedbackFile {
  const next = withResolutionNote(file, resolution);
  return { ...next, status: deriveFeedbackStatus(next, file.status) };
}
