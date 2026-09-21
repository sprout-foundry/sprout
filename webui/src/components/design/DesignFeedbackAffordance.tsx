/**
 * DesignFeedbackAffordance — the detail pane's annotation write path
 * (SP-140-3 §3e; file schema SP-140-4 §4d).
 *
 * Scoped to a stub: with a design asset selected it offers a pin affordance
 * ("Add feedback") that opens a small note + area form, and on submit it calls
 * `designApi.writeFeedback(target, json)`, which writes
 * `design/feedback/<target>.json` in the SP-140-4d schema — the canonical
 * `DesignFeedbackFile` (`target`, `status`, `resolution`, and
 * `annotations: [{id, at, area, note, resolved, created}]`, all fields
 * present). The panel surfaces the file it wrote so the write path is visible
 * without leaving the pane.
 *
 * The target is the selected asset, resolved by the pure `feedbackWrite`
 * model: workspace-relative with the `design/` prefix, keyed by its stem
 * (`design/screens/login.html` → `design/feedback/login.json`). This is the
 * only write this item adds — the canvas still never writes `.mmd`, and
 * screens' text edits keep their own `writeAsset` path.
 *
 * Not this item (§3e): drawing the annotation on the rendered screen, marking
 * it resolved, and the agent-side feedback consumption (SP-140-4). Item 4.8
 * grows this affordance into the resolution flow; a future item renders the
 * existing annotations this write path produces.
 */

import { useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { FEEDBACK_AREAS, buildFeedbackFile, feedbackFilePath, feedbackTarget } from '../../design/feedbackWrite';
import { armPinPlacement, onPinPlacePoint } from '../../design/pinPlacement';
import { writeFeedback } from '../../services/api/designApi';
import type { DesignFeedbackFile, DesignWriteResult } from '../../services/api/types';

export interface DesignFeedbackAffordanceProps {
  /** The selected design asset path the annotation is attached to. */
  path?: string | null;
  /**
   * Write transport. Defaults to the context's consent-aware fetch, matching
   * the other tabs' writes (SP-140-3 §3f); tests inject a mock.
   */
  fetchFn?: typeof fetch;
  /** Override the write (hosts that own the consent-aware write, or tests). */
  onWriteFeedback?: (target: string, json: DesignFeedbackFile) => Promise<unknown>;
  /** Clock seam for the annotation's `created` timestamp. */
  now?: () => string;
}

/** The pane's pending-annotation write control (§3e). */
export default function DesignFeedbackAffordance({
  path,
  fetchFn,
  onWriteFeedback,
  now,
}: DesignFeedbackAffordanceProps) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');
  const [area, setArea] = useState<string>(FEEDBACK_AREAS[0]);
  const [writtenPath, setWrittenPath] = useState('');
  const [error, setError] = useState('');
  // §6e: the pinned point for the next annotation — null until placed by
  // click; the builder defaults to the center when no point was placed.
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);

  const target = feedbackTarget(path);
  if (!target) return null;

  const filePath = feedbackFilePath(target);

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = note.trim();
    if (!trimmed) return;
    setError('');
    const json = buildFeedbackFile(target, trimmed, area, (now ?? defaultNow)(), at ?? undefined);
    try {
      const result = onWriteFeedback
        ? await onWriteFeedback(target, json)
        : await writeFeedback(transport, target, json, transport);
      const written = (result as DesignWriteResult | undefined)?.path ?? filePath;
      setWrittenPath(written);
      setNote('');
      setAt(null);
      setOpen(false);
    } catch {
      setError(`Could not write ${filePath}.`);
    }
  };

  return (
    <section className="design-detail-feedback" data-testid="design-feedback-affordance" data-target={target}>
      <button
        type="button"
        className="design-feedback-open"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
        data-testid="design-feedback-add"
      >
        Add feedback
      </button>

      {open ? (
        <form className="design-feedback-form" onSubmit={handleSubmit} data-testid="design-feedback-form">
          <p className="design-feedback-target" data-testid="design-feedback-target">
            {target}
          </p>
          <label className="design-feedback-field" htmlFor="design-feedback-area">
            Area
            <select
              id="design-feedback-area"
              className="design-feedback-area"
              value={area}
              onChange={(event) => setArea(event.target.value)}
              data-testid="design-feedback-area"
            >
              {FEEDBACK_AREAS.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
          </label>
          <label className="design-feedback-field" htmlFor="design-feedback-note">
            Note
            <textarea
              id="design-feedback-note"
              className="design-feedback-note"
              rows={3}
              placeholder="What should change?"
              value={note}
              onChange={(event) => setNote(event.target.value)}
              data-testid="design-feedback-note"
            />
          </label>
          {/* §6e: place the pin by clicking the preview instead of the center
              default. The Screens tab listens for the arm and answers with a
              normalized point, which this form stores for the submit. */}
          <PlaceByClickButton at={at} onArm={() => setAt(null)} onPoint={(point) => setAt(point)} />
          <button
            type="submit"
            className="design-feedback-open"
            disabled={note.trim().length === 0}
            data-testid="design-feedback-submit"
          >
            Save feedback
          </button>
        </form>
      ) : null}

      {writtenPath ? (
        <p className="design-feedback-target" data-testid="design-feedback-written">
          Wrote {writtenPath}
        </p>
      ) : null}
      {error ? (
        <p className="design-feedback-error" data-testid="design-feedback-error">
          {error}
        </p>
      ) : null}
    </section>
  );
}

/** The annotation's `created` timestamp (ISO 8601, as in the §4d example). */
function defaultNow(): string {
  return new Date().toISOString();
}

/**
 * The §6e place-by-click control. Arms placement (the Screens tab's pin layer
 * listens); the placed point comes back as an event and lands in the form's
 * state. A successful submit clears the point, so the next annotation starts
 * from the center default again unless placed.
 */
function PlaceByClickButton({
  at,
  onArm,
  onPoint,
}: {
  at: { x: number; y: number } | null;
  onArm: () => void;
  onPoint: (point: { x: number; y: number }) => void;
}) {
  useEffect(() => {
    if (at) return;
    return onPinPlacePoint(onPoint);
  }, [at, onPoint]);

  return (
    <button
      type="button"
      className="design-feedback-place"
      data-testid="design-feedback-place"
      onClick={() => {
        onArm();
        armPinPlacement();
      }}
    >
      {at
        ? `Pin at ${Math.round(at.x * 100)}%, ${Math.round(at.y * 100)}% — click to re-place`
        : 'Place pin by clicking the preview'}
    </button>
  );
}
