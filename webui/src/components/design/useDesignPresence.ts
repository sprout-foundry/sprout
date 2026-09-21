/**
 * useDesignPresence — probe the workspace for a `design/` directory.
 *
 * The mode switcher and surface both key on directory presence (SP-140-3 §3a,
 * amended by the Design empty state: presence no longer gates availability,
 * it selects between the live surface and the onboarding empty state). This
 * hook performs an `/api/files` listing through the adapter-aware fetch (so
 * cloud and local modes behave alike) and evaluates the pure
 * {@link hasDesignDir} rule. Failures resolve to `false`: a workspace we
 * can't list is treated as having no design tree, never as an error state.
 *
 * Returns `{ present, loading, recheck }`. `present` is false while the first
 * listing is in flight so nothing design-shaped flashes into the nav on boot.
 * `recheck` re-runs the probe on demand — the empty state's "Check again"
 * uses it after the user (or the agent) created `design/` elsewhere. Probes
 * are monotonic (a monotonically increasing sequence, latest wins), so a
 * recheck racing a mount probe cannot commit a stale answer.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import type { FilesResponse } from '../../services/api';
import { hasDesignDir } from './designVisibility';

export interface DesignPresence {
  /** True only once a listing has confirmed a design/ directory. */
  present: boolean;
  /** True until the first listing settles. */
  loading: boolean;
  /** Re-run the probe now (latest result wins). */
  recheck: () => void;
}

export function useDesignPresence(): DesignPresence {
  const fetchFn = useSproutFetch();
  const fetchRef = useRef(fetchFn);
  // Monotonic probe sequence: only the newest request may commit state, so a
  // slow stale response can never overwrite a fresh one (same shape as the
  // workspace context's inventory fetch).
  const probeSeq = useRef(0);
  const [presence, setPresence] = useState({ present: false, loading: true });

  // Keep the ref current without making it an effect dependency.
  useEffect(() => {
    fetchRef.current = fetchFn;
  }, [fetchFn]);

  const probe = useCallback(async (seq: number) => {
    try {
      const response = await fetchRef.current('/api/files');
      const files = response.ok ? ((await response.json()) as FilesResponse) : null;
      if (seq !== probeSeq.current) return;
      setPresence({ present: hasDesignDir(files), loading: false });
    } catch {
      if (seq !== probeSeq.current) return;
      setPresence({ present: false, loading: false });
    }
  }, []);

  useEffect(() => {
    void probe(++probeSeq.current);
  }, [probe]);

  const recheck = useCallback(() => {
    void probe(++probeSeq.current);
  }, [probe]);

  return { ...presence, recheck };
}
