/**
 * useMockupPresence — does this workspace have a mockup to show?
 *
 * `draw_mockup` writes `design/mockup.html`; the preview shows it when it
 * exists and the dev server's output otherwise. This probe is the only thing
 * that decides between them, so it is deliberately cheap: one file read.
 *
 * It re-probes when a workspace file changes (the draw lands mid-session, with
 * no focus event and no reload) and on window focus, which catches a write
 * that happened while the tab was in the background.
 */

import { useCallback, useEffect, useState } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { useEvents } from '../contexts/EventsContext';
import type { SproutEvent } from '../types/events';
import { changesWorkspaceFiles } from '../hooks/workspaceFileEvents';
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
  const events = useEvents();
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

  // A draw writes the mockup while the person is working, so the probe has to
  // follow the workspace's file events; waiting for focus would leave the
  // mockup invisible until they clicked away and back.
  useEffect(() => {
    const handleEvent = (event: SproutEvent): void => {
      if (changesWorkspaceFiles(event)) recheck();
    };
    events.onEvent(handleEvent);
    return () => {
      events.removeEvent(handleEvent);
    };
  }, [events, recheck]);

  // A write that landed while the tab was backgrounded produces no event we
  // saw; coming back is the point to pick it up.
  useEffect(() => {
    const onFocus = () => recheck();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [recheck]);

  return { loading, present, recheck };
}

export default useMockupPresence;
