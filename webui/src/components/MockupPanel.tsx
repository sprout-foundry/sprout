/**
 * MockupPanel — the mockup, rendered in the Code surface's preview slot.
 *
 * `draw_mockup` writes `design/mockup.html` and streams it while it is drawn.
 * This pane shows that document in an iframe: the document's own runtime
 * (screen switching, tabs, forms) runs inside it, and its styles apply without
 * leaking into the shell. The sandbox allows same-origin so the document can
 * load its own relative assets; that also lets it reach the shell's origin, so
 * a mockup is trusted to the same degree as the code in the workspace.
 *
 * It is the preview panel's other body: a workspace with a mockup shows it
 * here instead of a dev server's output, because a mockup is the same kind of
 * thing — a rendered view of the work — and the person wants it beside the
 * code they are writing from it, not in a mode of its own.
 *
 * The document is re-read on window focus, so a change made in the chat
 * appears without a manual reload.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import ErrorBoundary from './ErrorBoundary';
import { MOCKUP_PATH } from './mockupPath';
import './MockupPanel.css';

export { MOCKUP_PATH };

export interface MockupPanelProps {
  /** Collapse the panel (wired to the pane's close affordance). */
  onClose: () => void;
}

/** Read the mockup document, or null when it does not exist yet. */
async function readMockup(fetcher: typeof fetch): Promise<string | null> {
  const res = await fetcher(`/api/file?path=${encodeURIComponent(MOCKUP_PATH)}`);
  if (!res.ok) return null;
  return res.text();
}

export function MockupPanel({ onClose }: MockupPanelProps): JSX.Element {
  const fetcher = useSproutFetch();
  const [doc, setDoc] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const lastDoc = useRef<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const next = await readMockup(fetcher);
      if (next === null) {
        setMissing(true);
        return;
      }
      setMissing(false);
      if (next !== lastDoc.current) {
        lastDoc.current = next;
        setDoc(next);
      }
    } catch {
      // A transient read failure leaves the last good document on screen.
    }
  }, [fetcher]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // The agent rewrites the file while the person is reading it elsewhere;
  // coming back to the window is the point to pick that up.
  useEffect(() => {
    const onFocus = () => void refresh();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [refresh]);

  const body =
    missing || !doc ? (
      <div className="mockup-panel__empty" data-testid="mockup-panel-empty">
        <p className="mockup-panel__empty-title">No mockup yet</p>
        <p className="mockup-panel__empty-hint">Ask the agent to draw one; it appears here as it is drawn.</p>
      </div>
    ) : (
      <iframe
        className="mockup-panel__frame"
        data-testid="mockup-panel-frame"
        title="Mockup"
        sandbox="allow-scripts allow-same-origin"
        srcDoc={doc}
      />
    );

  return (
    <ErrorBoundary panelName="Mockup">
      <aside className="preview-panel mockup-panel" data-testid="mockup-panel">
        <div className="preview-pane">
          <div className="preview-pane__header">
            <span className="preview-pane__title" data-testid="mockup-panel-title">
              Mockup
            </span>
            <button
              type="button"
              className="preview-pane__close"
              data-testid="mockup-panel-close"
              onClick={onClose}
              aria-label="Close mockup"
            >
              Close
            </button>
          </div>
          <div className="preview-pane__body">{body}</div>
        </div>
      </aside>
    </ErrorBoundary>
  );
}

export default MockupPanel;
