/**
 * useDesignPresence — probe the workspace's design/ directory.
 *
 * The mode switcher and surface both key on this probe (SP-140-3 §3a,
 * amended by the Design empty state: presence no longer gates availability,
 * it selects between the live surface and the onboarding empty state). The
 * hook performs an `/api/files` listing through the adapter-aware fetch (so
 * cloud and local modes behave alike) and evaluates two pure rules from
 * designVisibility: {@link classifyDesignTree} (none / foreign / recognized)
 * and {@link hasFrontendSignal} (greenfield vs code-first position).
 *
 * Failures resolve conservatively: a workspace we can't list reads as no
 * tree, never as an error state.
 *
 * Returns `{ present, loading, treeState, frontendLike, recheck }`.
 * `present` is false while the first listing is in flight so nothing
 * design-shaped flashes into the nav on boot. `recheck` re-runs the probe on
 * demand — the empty state's "Check again" uses it after the user (or the
 * agent) created `design/` elsewhere. Probes are monotonic (a sequence
 * counter, latest wins), so a recheck racing the mount probe cannot commit a
 * stale answer.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import type { FilesResponse } from '../../services/api';
import { classifyDesignTree, hasFrontendSignal, type DesignTreeState } from './designVisibility';

export interface DesignPresence {
  /** True only once a listing has confirmed a design/ directory. */
  present: boolean;
  /** True until the first listing settles. */
  loading: boolean;
  /** How design/ relates to Sprout's idiom: none, foreign, or recognized. */
  treeState: DesignTreeState;
  /** True when the workspace shows frontend code (position signal). */
  frontendLike: boolean;
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
  const [presence, setPresence] = useState({
    present: false,
    loading: true,
    treeState: 'none' as DesignTreeState,
    frontendLike: false,
  });

  // Keep the ref current without making it an effect dependency.
  useEffect(() => {
    fetchRef.current = fetchFn;
  }, [fetchFn]);

  const probe = useCallback(async (seq: number) => {
    try {
      const response = await fetchRef.current('/api/files');
      const files = response.ok ? ((await response.json()) as FilesResponse) : null;
      if (seq !== probeSeq.current) return;
      setPresence({
        present: files ? classifyDesignTree(files) !== 'none' : false,
        loading: false,
        treeState: classifyDesignTree(files),
        frontendLike: hasFrontendSignal(files),
      });
    } catch {
      if (seq !== probeSeq.current) return;
      setPresence({ present: false, loading: false, treeState: 'none', frontendLike: false });
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
