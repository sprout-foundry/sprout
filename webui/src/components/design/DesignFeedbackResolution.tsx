/**
 * DesignFeedbackResolution — the detail pane's resolution flow
 * (SP-140-4 §4d, item 4.8).
 *
 * The SP-140-3e affordance writes a feedback file; this component reads it back
 * and closes the loop the spec describes:
 *
 * 1. **Mark annotation `resolved` from the detail pane.** Each §4d annotation
 *    is listed with its area, note, and `resolved` state, and toggled with one
 *    control. The toggle is a pure transform (`toggleAnnotationResolved`)
 *    written straight back through `designApi.writeFeedback` — the same
 *    file-write path the affordance uses, so there is one writer for feedback
 *    files and the schema round-trips untouched.
 * 2. **Close the loop with a `resolution` note.** The note field records the
 *    agent's summary of the changes it made; saving it derives `status`
 *    (`applyResolutionNote`: `resolved` once every annotation is resolved or a
 *    note closes an annotation-less file, `changes-requested` while anything is
 *    still open) and writes the document back.
 *
 * Read and write go through `designApi.readFeedback` / `designApi.writeFeedback`
 * over the existing workspace-file endpoints (zero new HTTP endpoints, §3f);
 * both take the pane's transport, and a consent-aware `readFn`/`writeFn` pair
 * works exactly as it does for the other tabs. `onReadFeedback` /
 * `onWriteFeedback` are the test/host seams.
 *
 * A target with no feedback file yet is not an error: the pane still offers the
 * resolution note (the empty document), so an agent that resolved a change it
 * found by other means can record why. Nothing is written until the user acts.
 */

import { useCallback, useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import {
  annotationCounts,
  applyResolutionNote,
  feedbackFilePath,
  feedbackTarget,
  feedbackWriteTarget,
  isResolvableAnnotation,
  toggleAnnotationResolved,
} from '../../design/feedbackWrite';
import { readFeedback, writeFeedback } from '../../services/api/designApi';
import type { DesignFeedbackFile, DesignWriteResult } from '../../services/api/types';

export interface DesignFeedbackResolutionProps {
  /** The selected design asset path the feedback file is keyed on. */
  path?: string | null;
  /**
   * Read transport for the §4d file. Defaults to the context's consent-aware
   * fetch, matching the affordance's write seam (SP-140-3 §3f).
   */
  fetchFn?: typeof fetch;
  /** Consent-aware read override, e.g. `readFileWithConsent` (§3f). */
  readFn?: typeof fetch;
  /** Consent-aware write override, e.g. `writeFileWithConsent` (§3f). */
  writeFn?: typeof fetch;
  /** Override the read (hosts that own the consent-aware read, or tests). */
  onReadFeedback?: (target: string) => Promise<DesignFeedbackFile | null>;
  /** Override the write (hosts that own the consent-aware write, or tests). */
  onWriteFeedback?: (target: string, json: DesignFeedbackFile) => Promise<unknown>;
}

/** The pane's resolution control (§4d). */
export default function DesignFeedbackResolution({
  path,
  fetchFn,
  readFn,
  writeFn,
  onReadFeedback,
  onWriteFeedback,
}: DesignFeedbackResolutionProps) {
  const contextFetch = useSproutFetch();
  const transport = fetchFn ?? contextFetch;
  const readTransport = readFn ?? transport;
  const writeTransport = writeFn ?? transport;

  const target = feedbackTarget(path);
  const filePath = feedbackFilePath(target);

  const [file, setFile] = useState<DesignFeedbackFile | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');
  const [writtenPath, setWrittenPath] = useState('');
  const [error, setError] = useState('');

  // Load the §4d file for the selected target. A failed read surfaces as an
  // error line and leaves the note form usable (nothing is written).
  useEffect(() => {
    if (!target) return;
    let cancelled = false;
    setLoading(true);
    setError('');
    setWrittenPath('');
    (async () => {
      try {
        const loaded = onReadFeedback
          ? await onReadFeedback(target)
          : await readFeedback(readTransport, target, readTransport);
        if (cancelled) return;
        const next = loaded ?? emptyFeedbackFile(target);
        setFile(next);
        setNote(next.resolution ?? '');
      } catch {
        if (cancelled) return;
        setFile(null);
        setError(`Could not read ${filePath}.`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [target, filePath, onReadFeedback, readTransport]);

  /** Persist a §4d document through the pane's write seam. */
  const persist = useCallback(
    async (next: DesignFeedbackFile) => {
      setBusy(true);
      setError('');
      try {
        const result = onWriteFeedback
          ? await onWriteFeedback(target, next)
          : // The write seam keys its URL on the file stem, not the asset path.
            await writeFeedback(writeTransport, feedbackWriteTarget(target), next, writeTransport);
        setFile(next);
        setWrittenPath((result as DesignWriteResult | undefined)?.path ?? filePath);
      } catch {
        setError(`Could not write ${filePath}.`);
      } finally {
        setBusy(false);
      }
    },
    [target, filePath, onWriteFeedback, writeTransport],
  );

  const handleToggle = useCallback(
    (annotationId: string) => {
      if (!file || busy) return;
      void persist(toggleAnnotationResolved(file, annotationId));
    },
    [file, busy, persist],
  );

  const handleSaveNote = useCallback(
    (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!file || busy) return;
      void persist(applyResolutionNote(file, note));
    },
    [file, busy, note, persist],
  );

  if (!target) return null;

  const counts = file ? annotationCounts(file) : { total: 0, resolved: 0 };
  const status = file?.status ?? '';
  const annotations = (file?.annotations ?? []).filter(isResolvableAnnotation);

  return (
    <section className="design-detail-resolution" data-testid="design-feedback-resolution" data-target={target}>
      <header className="design-resolution-header">
        <h3 className="design-resolution-heading">Feedback</h3>
        {status ? (
          <span className="design-resolution-status" data-testid="design-feedback-status" data-status={status}>
            {status}
          </span>
        ) : null}
      </header>

      {loading ? (
        <p className="design-resolution-note" data-testid="design-feedback-loading">
          Loading feedback…
        </p>
      ) : null}

      {!loading && annotations.length === 0 && !error ? (
        <p className="design-resolution-note" data-testid="design-feedback-empty">
          No annotations for {target}.
        </p>
      ) : null}

      {annotations.length > 0 ? (
        <>
          <p className="design-resolution-count" data-testid="design-feedback-count">
            {counts.resolved} of {counts.total} resolved
          </p>
          <ul className="design-resolution-list" data-testid="design-feedback-annotations">
            {annotations.map((annotation) => (
              <li
                key={annotation.id}
                className={`design-resolution-item ${annotation.resolved ? 'resolved' : ''}`}
                data-testid={`design-feedback-annotation-${annotation.id}`}
                data-resolved={annotation.resolved ? 'true' : 'false'}
              >
                <div className="design-resolution-item-main">
                  <span className="design-resolution-area" data-testid={`design-feedback-area-${annotation.id}`}>
                    {annotation.area || 'note'}
                  </span>
                  <p className="design-resolution-text">{annotation.note}</p>
                </div>
                <button
                  type="button"
                  className="design-resolution-toggle"
                  aria-pressed={annotation.resolved}
                  disabled={busy}
                  onClick={() => handleToggle(annotation.id)}
                  data-testid={`design-feedback-toggle-${annotation.id}`}
                >
                  {annotation.resolved ? 'Mark unresolved' : 'Mark resolved'}
                </button>
              </li>
            ))}
          </ul>
        </>
      ) : null}

      <form className="design-resolution-note-form" onSubmit={handleSaveNote}>
        <label className="design-feedback-field" htmlFor="design-feedback-resolution">
          Resolution note
          <textarea
            id="design-feedback-resolution"
            className="design-feedback-note"
            rows={3}
            placeholder="Summarize the changes that resolve this feedback"
            value={note}
            disabled={loading}
            onChange={(event) => setNote(event.target.value)}
            data-testid="design-feedback-resolution-note"
          />
        </label>
        <button
          type="submit"
          className="design-feedback-open"
          disabled={loading || busy || !file}
          data-testid="design-feedback-resolution-save"
        >
          Save resolution
        </button>
      </form>

      {writtenPath ? (
        <p className="design-resolution-note" data-testid="design-feedback-resolution-written">
          Wrote {writtenPath}
        </p>
      ) : null}
      {error ? (
        <p className="design-feedback-error" data-testid="design-feedback-resolution-error">
          {error}
        </p>
      ) : null}
    </section>
  );
}

/** The §4d document for a target that has no file yet (or an unparseable one). */
function emptyFeedbackFile(target: string): DesignFeedbackFile {
  return { target, status: '', resolution: '', annotations: [] };
}
