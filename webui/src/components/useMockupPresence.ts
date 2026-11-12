/**
 * useMockupPresence — does this workspace have a mockup to show?
 *
 * `draw_mockup` writes `design/mockup.html`; the Code shell's preview panel
 * shows it when it exists and the dev server's output otherwise. This probe is
 * the only thing that decides between them, so it is deliberately cheap: one
 * file read, re-run on focus.
 */

import { useCallback, useEffect, useState } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { MOCKUP_PATH } from './mockupPath';

export interface MockupPresence {
  /** True while the probe is in flight. */
  loading: boolean;
  /** True when the workspace has a mockup. */
  present: boolean;
  /** Re-run the probe. */
  recheck: () => void;
}

export function useMockupPresence(): MockupPresence {
  const fetcher = useSproutFetch();
  const [loading, setLoading] = useState(true);
  const [present, setPresent] = useState(false);
  const [probe, setProbe] = useState(0);

  const recheck = useCallback(() => setProbe((n) => n + 1), []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetcher(`/api/file?path=${encodeURIComponent(MOCKUP_PATH)}`)
      .then((res) => {
        if (cancelled) return;
        setPresent(res.ok);
      })
      .catch(() => {
        if (!cancelled) setPresent(false);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [fetcher, probe]);

  // A mockup appears the moment a draw finishes; re-probe on focus so the
  // panel switches to it without a manual refresh.
  useEffect(() => {
    const onFocus = () => recheck();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [recheck]);

  return { loading, present, recheck };
}

export default useMockupPresence;
