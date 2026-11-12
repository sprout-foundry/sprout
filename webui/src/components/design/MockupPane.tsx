/**
 * MockupPane — the mockup, rendered (SP-140).
 *
 * `draw_mockup` writes `design/mockup.html` and streams it while it is
 * drawn. This pane shows that document in a sandboxed iframe: the screens'
 * own runtime (screen switching, tabs, forms) runs inside it, and the
 * document's own styles apply without leaking into the shell.
 *
 * It polls the file while a draw is in flight (the tool's mockup_* events
 * say when) and on window focus otherwise, so a change made in the chat
 * appears without a manual reload.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';

/** The mockup's place in the workspace (draw_mockup's fixed path). */
export const MOCKUP_PATH = 'design/mockup.html';

/** How often the pane re-reads the mockup while a draw is in flight. */
const STREAM_POLL_MS = 750;

export interface MockupPaneProps {
  /** True when the workspace has a mockup to show. */
  present: boolean;
  /** Optional override of the polling interval (tests). */
  pollMs?: number;
}

/** Read the mockup document, or null when it does not exist yet. */
async function readMockup(fetcher: typeof fetch): Promise<string | null> {
  const res = await fetcher(`/api/file?path=${encodeURIComponent(MOCKUP_PATH)}`);
  if (!res.ok) return null;
  return res.text();
}

export default function MockupPane({ present, pollMs = STREAM_POLL_MS }: MockupPaneProps) {
  const fetcher = useSproutFetch();
  const [doc, setDoc] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const lastDoc = useRef<string | null>(null);

  const refresh = useCallback(async () => {
    if (!present) {
      setDoc(null);
      setMissing(false);
      return;
    }
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
  }, [fetcher, present]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // A draw rewrites the file repeatedly; poll while the document is changing
  // and settle once it stops. Window focus covers the idle case (the agent
  // rewrote it while the person was elsewhere).
  useEffect(() => {
    if (!present) return;
    const timer = window.setInterval(() => void refresh(), pollMs);
    const onFocus = () => void refresh();
    window.addEventListener('focus', onFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('focus', onFocus);
    };
  }, [present, pollMs, refresh]);

  if (!present) {
    return (
      <div className="design-mockup-empty" data-testid="design-mockup-empty">
        <p className="design-mockup-empty-title">No mockup yet</p>
        <p className="design-mockup-empty-hint">Ask for a screen in the chat — it appears here while it is drawn.</p>
      </div>
    );
  }

  if (missing || !doc) {
    return (
      <div className="design-mockup-empty" data-testid="design-mockup-missing">
        <p className="design-mockup-empty-title">Drawing…</p>
      </div>
    );
  }

  return (
    <iframe
      className="design-mockup-frame"
      data-testid="design-mockup-frame"
      title="Mockup"
      sandbox="allow-scripts allow-same-origin"
      srcDoc={doc}
    />
  );
}
