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
 * §3e scope: the write path and the file schema land here. Drawing annotations
 * on a rendered screen, marking them resolved, and the agent-side consumption
 * are SP-140-4.
 */

import type { DesignFeedbackFile } from '../services/api/types';

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
