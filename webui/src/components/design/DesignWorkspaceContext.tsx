/**
 * Design-workspace shared state (SP-140-5 workspace modes).
 *
 * The assets browser lives in the app sidebar (the mode's content pane) while
 * the canvas and detail pane live in the mode surface. Both render from the
 * same inventory and the same selection, so this context — not prop drilling
 * through AppContent — is the single owner: the sidebar pane and DesignView
 * are two views of one state.
 *
 * `useDesignWorkspace` returns null outside a provider so hosts without the
 * workspace shell (standalone DesignView, component tests) keep working:
 * DesignView falls back to its own fetch and selection in that case.
 */

import { useEffect, useState, type ReactNode } from 'react';
import { createContext, useContext } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { listAssets } from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types';
import type { DesignTab } from './DesignView';

export interface DesignWorkspaceState {
  /** The active section (flows/screens/tokens), driven by the mode rail. */
  tab: DesignTab;
  /** The design inventory, fetched while the mode is active. */
  inventory: DesignInventory | null;
  /** The selected asset path (relative to the design/ root), or null. */
  selected: string | null;
  /** Select an asset; the sidebar pane, canvas, and detail pane all follow. */
  select: (path: string | null) => void;
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

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    (async () => {
      try {
        const next = await listAssets(fetchFn);
        if (!cancelled) setInventory(next);
      } catch {
        if (!cancelled) setInventory(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [active, fetchFn]);

  const state: DesignWorkspaceState = {
    tab,
    inventory,
    selected,
    select: setSelected,
  };
  return <DesignWorkspaceContext.Provider value={state}>{children}</DesignWorkspaceContext.Provider>;
}
