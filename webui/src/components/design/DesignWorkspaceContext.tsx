/**
 * Design-workspace shared state (SP-140-5 workspace modes; live tree per
 * SP-140-6 §6a).
 *
 * The assets browser lives in the app sidebar (the mode's content pane) while
 * the canvas and detail pane live in the mode surface. Both render from the
 * same inventory and the same selection, so this context — not prop drilling
 * through AppContent — is the single owner: the sidebar pane and DesignView
 * are two views of one state.
 *
 * §6a live tree: the inventory is not fetched once per activation anymore —
 * it refetches when the window regains focus, on a slow interval while Design
 * mode is active, and on demand through `refresh` (the health strip's
 * control). An agent turn that edits design/ while the user is in the mode
 * therefore reaches the surface without a mode round-trip. A refetch that no
 * longer sees the selected asset clears the selection. The fetch stays gated
 * on `active` (a Code-mode session never pays for it).
 *
 * `useDesignWorkspace` returns null outside a provider so hosts without the
 * workspace shell (standalone DesignView, component tests) keep working:
 * DesignView falls back to its own fetch and selection in that case.
 */

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { createContext, useContext } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { listAssets } from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types';
import type { DesignTab } from './DesignView';

/** How often the live tree polls while Design mode is active (§6a). */
export const DESIGN_REFRESH_INTERVAL_MS = 30_000;

/** Every asset path an inventory carries — the selection-liveness set. */
function inventoryPaths(inventory: DesignInventory | null): Set<string> {
  const paths = new Set<string>();
  for (const asset of inventory?.assets ?? []) paths.add(asset.path);
  return paths;
}

export interface DesignWorkspaceState {
  /** The active section (flows/screens/tokens), driven by the mode rail. */
  tab: DesignTab;
  /** The design inventory, fetched while Design mode is active. */
  inventory: DesignInventory | null;
  /** The selected asset path (relative to the design/ root), or null. */
  selected: string | null;
  /** Select an asset; the sidebar pane, canvas, and detail pane all follow. */
  select: (path: string | null) => void;
  /** Refetch the inventory now (§6a: the health strip's refresh control). */
  refresh: () => void;
}

const DesignWorkspaceContext = createContext<DesignWorkspaceState | null>(null);

/** The shared design-workspace state, or null when no provider is mounted. */
export function useDesignWorkspace(): DesignWorkspaceState | null {
  return useContext(DesignWorkspaceContext);
}

export interface DesignWorkspaceProviderProps {
  /** The active section (from AppContent's design-section state). */
  tab: DesignTab;
  /**
   * True while Design mode is active. The inventory fetch is gated on this so
   * a Code-mode session never pays for a design list it cannot see.
   */
  active: boolean;
  children: ReactNode;
}

export function DesignWorkspaceProvider({ tab, active, children }: DesignWorkspaceProviderProps) {
  const fetchFn = useSproutFetch();
  const [inventory, setInventory] = useState<DesignInventory | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  // Monotonic fetch sequence: only the newest request may commit state, so a
  // slow stale response can never overwrite a fresh one.
  const fetchSeq = useRef(0);

  const fetchInventory = useCallback(
    async (seq: number) => {
      try {
        const next = await listAssets(fetchFn);
        if (seq !== fetchSeq.current) return;
        setInventory(next);
        // Selection liveness (§6a): the selected asset vanished from the
        // tree (deleted, renamed, or the tree itself is gone) — drop the
        // selection instead of rendering a detail pane for a ghost.
        setSelected((current) => (current && !inventoryPaths(next).has(current) ? null : current));
      } catch {
        if (seq !== fetchSeq.current) return;
        setInventory(null);
      }
    },
    [fetchFn],
  );

  useEffect(() => {
    if (!active) return;
    const refetch = () => fetchInventory(++fetchSeq.current);
    refetch();
    // §6a: focus + slow interval keep the tree live without a push channel.
    window.addEventListener('focus', refetch);
    const timer = window.setInterval(refetch, DESIGN_REFRESH_INTERVAL_MS);
    return () => {
      window.removeEventListener('focus', refetch);
      window.clearInterval(timer);
    };
  }, [active, fetchInventory]);

  const refresh = useCallback(() => {
    fetchInventory(++fetchSeq.current);
  }, [fetchInventory]);

  const select = useCallback((path: string | null) => setSelected(path), []);

  const state: DesignWorkspaceState = {
    tab,
    inventory,
    selected,
    select,
    refresh,
  };
  return <DesignWorkspaceContext.Provider value={state}>{children}</DesignWorkspaceContext.Provider>;
}
