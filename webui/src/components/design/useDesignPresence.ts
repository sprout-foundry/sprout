/**
 * useDesignPresence — probe the workspace for a `design/` directory.
 *
 * The DesignView nav affordance and route both gate on directory presence
 * (SP-140-3 §3a). This hook performs a single `/api/files` listing through
 * the adapter-aware fetch (so cloud and local modes behave alike) and
 * evaluates the pure {@link hasDesignDir} rule. Failures resolve to `false`:
 * a workspace we can't list is treated as having no design tree, never as an
 * error state.
 *
 * Returns `{ present, loading }`. `present` is false while the first listing
 * is in flight so nothing design-shaped flashes into the nav on boot.
 *
 * The probe runs once per mount. `fetchFn`'s identity is not a dependency:
 * `useSproutFetch` can hand back a fresh callback on any adapter re-render,
 * and keying the effect on it would re-list the whole workspace on every
 * unrelated render. Presence is a boot-time fact, so a stable one-shot
 * effect (reading the latest fetch through a ref) is both cheaper and
 * deterministic.
 */

import { useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import type { FilesResponse } from '../../services/api';
import { hasDesignDir } from './designVisibility';

export interface DesignPresence {
  /** True only once a listing has confirmed a design/ directory. */
  present: boolean;
  /** True until the first listing settles. */
  loading: boolean;
}

export function useDesignPresence(): DesignPresence {
  const fetchFn = useSproutFetch();
  const fetchRef = useRef(fetchFn);
  const [presence, setPresence] = useState<DesignPresence>({ present: false, loading: true });

  // Keep the ref current without making it an effect dependency.
  useEffect(() => {
    fetchRef.current = fetchFn;
  }, [fetchFn]);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const response = await fetchRef.current('/api/files');
        const files = response.ok ? ((await response.json()) as FilesResponse) : null;
        if (cancelled) return;
        setPresence({ present: hasDesignDir(files), loading: false });
      } catch {
        if (cancelled) return;
        setPresence({ present: false, loading: false });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  return presence;
}
