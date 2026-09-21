/**
 * Pin-placement bridge (SP-140-6 §6e, TODO item 6.5).
 *
 * The "Add feedback" affordance lives in the detail pane; the rendered
 * preview (and its pin layer) lives in the Screens tab body. Placement is
 * one shared intent between those two components, so it crosses through a
 * window CustomEvent — the same decoupling pattern as the shell's
 * `agent-file-changed` bridge — with the point passed in event detail.
 *
 * Pure helpers only: dispatch/listen wrappers plus a parse guard, so both
 * sides test without a DOM component.
 */

import type { DesignFeedbackAnnotation } from '../services/api/types';

/** The CustomEvent names (one namespace, two directions). */
export const PIN_PLACE_ARM_EVENT = 'sprout-design-place-arm';
export const PIN_PLACE_POINT_EVENT = 'sprout-design-place-point';

/** A placed point, normalized 0–1 (§4d `at`). */
export interface PinPlacePoint {
  x: number;
  y: number;
}

/** Arm placement: the affordance asks the preview for a click. */
export function armPinPlacement(): void {
  window.dispatchEvent(new CustomEvent(PIN_PLACE_ARM_EVENT));
}

/** Disarm placement (affordance closed or submitted). */
export function disarmPinPlacement(): void {
  window.dispatchEvent(new CustomEvent(PIN_PLACE_ARM_EVENT, { detail: { cancel: true } }));
}

/** Publish a placed point (the pin layer's click). */
export function publishPinPlacePoint(point: PinPlacePoint): void {
  window.dispatchEvent(new CustomEvent(PIN_PLACE_POINT_EVENT, { detail: point }));
}

/** Listen for arm/cancel intents. Returns the unsubscribe function. */
export function onPinPlacementArm(handler: (cancel: boolean) => void): () => void {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent).detail as { cancel?: boolean } | undefined;
    handler(Boolean(detail?.cancel));
  };
  window.addEventListener(PIN_PLACE_ARM_EVENT, listener);
  return () => window.removeEventListener(PIN_PLACE_ARM_EVENT, listener);
}

/** Listen for placed points. Returns the unsubscribe function. */
export function onPinPlacePoint(handler: (point: PinPlacePoint) => void): () => void {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent).detail as Partial<PinPlacePoint> | undefined;
    // A malformed point is ignored rather than clamped into a ghost pin.
    if (typeof detail?.x !== 'number' || typeof detail?.y !== 'number') return;
    if (!Number.isFinite(detail.x) || !Number.isFinite(detail.y)) return;
    handler({ x: detail.x, y: detail.y });
  };
  window.addEventListener(PIN_PLACE_POINT_EVENT, listener);
  return () => window.removeEventListener(PIN_PLACE_POINT_EVENT, listener);
}

/**
 * The pending count for a card badge: annotations not yet resolved. Takes the
 * §4d document's counts (the inventory's feedback entry carries them) — 0 for
 * a missing entry.
 */
export function pendingCountForStem(
  feedback: Array<{ name: string; annotationCount: number; resolvedCount: number }>,
  stem: string,
): number {
  const entry = feedback.find((f) => f.name === `${stem}.json`);
  if (!entry) return 0;
  return Math.max(0, entry.annotationCount - entry.resolvedCount);
}

/** Type re-export for consumers that pass full annotations. */
export type PinAnnotation = DesignFeedbackAnnotation;
