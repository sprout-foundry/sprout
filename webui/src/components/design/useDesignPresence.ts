/**
 * useDesignPresence — does this workspace have a mockup to show?
 *
 * Design mode is always offered; this only tells the surface whether there is
 * a `design/mockup.html` yet, so it can show the empty state instead of a
 * blank pane.
 */

import { useCallback, useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { MOCKUP_PATH } from './MockupPane';

export interface DesignPresence {
  /** True while the probe is in flight. */
  loading: boolean;
  /** True when the workspace has a mockup. */
  present: boolean;
  /** Re-run the probe. */
  recheck: () => void;
}

export function useDesignPresence(): DesignPresence {
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
  // empty state gives way without a manual refresh.
  useEffect(() => {
    const onFocus = () => recheck();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [recheck]);

  return { loading, present, recheck };
}

export default useDesignPresence;
