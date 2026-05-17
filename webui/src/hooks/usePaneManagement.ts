import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { MIN_PANE_WIDTH_PERCENT } from '../contexts/EditorManagerContext';
import type { EditorBuffer, EditorPane, PaneLayout, PaneSize } from '../types/editor';

interface UsePaneManagementParams {
  panes: EditorPane[];
  activePaneId: string | null;
  activeBufferId: string | null;
  closeBuffer: (bufferId: string) => void;
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
  setPanes: Dispatch<SetStateAction<EditorPane[]>>;
  setPaneLayoutState: Dispatch<SetStateAction<PaneLayout>>;
  setActivePaneId: Dispatch<SetStateAction<string | null>>;
  setActiveBufferId: Dispatch<SetStateAction<string | null>>;
  setPaneSizes: Dispatch<SetStateAction<PaneSize>>;
  setIsLinkedScrollEnabled: Dispatch<SetStateAction<boolean>>;
  maxPanes?: number; // Optional: configurable max panes limit (default: 6)
}

// Pane position values supporting up to 6 panes
const PANE_POSITIONS = ['primary', 'secondary', 'tertiary', 'quaternary', 'quinary', 'senary'] as const;
const STABLE_PANE_IDS = ['pane-2', 'pane-3', 'pane-4', 'pane-5', 'pane-6'] as const;

/** Pane layout management: close, switch, split, grid, resize. */
export function usePaneManagement({
  panes,
  activePaneId,
  closeBuffer,
  setBuffers,
  setPanes,
  setPaneLayoutState,
  setActivePaneId,
  setActiveBufferId,
  setPaneSizes,
  setIsLinkedScrollEnabled,
  maxPanes = 6,
}: UsePaneManagementParams) {
  const closePane = useCallback(
    (paneId: string) => {
      if (panes.length === 1) return;
      const pane = panes.find((p) => p.id === paneId);
      if (pane?.bufferId) closeBuffer(pane.bufferId);
      setPanes((prev) => prev.filter((p) => p.id !== paneId));
      if (paneId === activePaneId) {
        const remaining = panes.filter((p) => p.id !== paneId);
        setActivePaneId(remaining[0]?.id || null);
      }
      // The useEffect on panes.length auto-syncs layout to 'single' when
      // panes are reduced to 1.
    },
    [panes, activePaneId, closeBuffer, setPanes, setActivePaneId],
  );

  const switchPane = useCallback(
    (paneId: string) => {
      setActivePaneId(paneId);
      const pane = panes.find((p) => p.id === paneId);
      if (pane?.bufferId) setActiveBufferId(pane.bufferId);
    },
    [panes, setActivePaneId, setActiveBufferId],
  );

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const splitPane = useCallback(
    (paneId: string, direction: 'vertical' | 'horizontal') => {
      if (panes.length >= maxPanes) return null;
      const usedIds = new Set(panes.map((p) => p.id));
      const newPaneId = STABLE_PANE_IDS.find((id) => !usedIds.has(id)) || `pane-${Date.now()}`;
      // Dynamic position assignment based on current panes.length index
      const positionIndex = panes.length; // New pane index (0-based: primary already at 0)
      // We never exceed bounds because we check panes.length < maxPanes above
      const newPosition = PANE_POSITIONS[positionIndex] as (typeof PANE_POSITIONS)[number];
      const newPanes: EditorPane[] = [
        ...panes,
        {
          id: newPaneId,
          bufferId: null,
          isActive: false,
          position: newPosition,
        },
      ];
      setPanes(newPanes);
      if (panes.length === 1) {
        setPaneLayoutState(direction === 'vertical' ? 'split-vertical' : 'split-horizontal');
        setPaneSizes({ [panes[0].id]: 50, [newPaneId]: 50 });
      } else {
        setPaneSizes((prev) => ({ ...prev, [newPaneId]: 50 }));
      }
      setActivePaneId(newPaneId);
      return newPaneId;
    },
    [panes, maxPanes, setPanes, setPaneLayoutState, setPaneSizes, setActivePaneId],
  );

  const splitIntoGrid = useCallback(() => {
    const primaryPane = panes.find((p) => p.position === 'primary') || panes[0];
    if (!primaryPane) return panes.map((p) => p.id);

    // Detach buffers from panes that will be replaced (non-primary).
    const primaryPaneId = primaryPane.id;
    const displacedPaneIds = new Set(panes.filter((p) => p.id !== primaryPaneId).map((p) => p.id));
    if (displacedPaneIds.size > 0) {
      setBuffers((prev) => {
        const next = new Map(prev);
        let changed = false;
        next.forEach((buf, id) => {
          if (buf.paneId && displacedPaneIds.has(buf.paneId)) {
            next.set(id, { ...buf, paneId: undefined, isActive: false });
            changed = true;
          }
        });
        return changed ? next : prev;
      });
    }

    const usedIds = new Set(panes.map((p) => p.id));
    const newPaneIds = STABLE_PANE_IDS.filter((id) => !usedIds.has(id));

    // Grid is always 2×2 = 4 panes (primary + 3 additional)
    const numAdditionalPanes = Math.min(3, newPaneIds.length);
    const newPanes: EditorPane[] = [
      { ...primaryPane, position: 'primary' },
      ...Array.from({ length: numAdditionalPanes }, (_, i) => ({
        id: newPaneIds[i],
        bufferId: null,
        isActive: false,
        // We never exceed bounds because numAdditionalPanes is capped at 3 (PANE_POSITIONS has 6 entries)
        position: PANE_POSITIONS[i + 1] as (typeof PANE_POSITIONS)[number],
      })),
    ];

    setPanes(newPanes);
    setPaneLayoutState('split-grid');
    setActivePaneId(primaryPane.id);

    // Initialize grid pane sizes using restored values (if available) or 50/50
    setPaneSizes((prev) => ({
      'grid:col': typeof prev['grid:col'] === 'number' && isFinite(prev['grid:col']) ? prev['grid:col'] : 50,
      'grid:row': typeof prev['grid:row'] === 'number' && isFinite(prev['grid:row']) ? prev['grid:row'] : 50,
    }));

    const createdPaneIds = newPanes.map((p) => p.id);
    return createdPaneIds;
  }, [panes, maxPanes, setBuffers, setPanes, setPaneLayoutState, setActivePaneId, setPaneSizes]);

  const closeSplit = useCallback(() => {
    const activePane = panes.find((p) => p.id === activePaneId);

    // Close all non-primary panes
    panes.forEach((pane) => {
      if (pane.position !== 'primary') closePane(pane.id);
    });

    const primaryPane = panes.find((p) => p.position === 'primary');
    setPanes((prev) => {
      const primary = prev.find((p) => p.position === 'primary');
      return primary ? [primary] : prev;
    });
    setPaneLayoutState('single');
    setActivePaneId(primaryPane?.id || null);
    setPaneSizes({ [primaryPane?.id || 'pane-1']: 100 });

    // Preserve the buffer that was active
    const activeBufferToRestore = activePane?.bufferId || primaryPane?.bufferId;
    if (activeBufferToRestore) setActiveBufferId(activeBufferToRestore);
  }, [panes, activePaneId, closePane, setPanes, setPaneLayoutState, setActivePaneId, setPaneSizes, setActiveBufferId]);

  const setPaneLayout = useCallback(
    (layout: PaneLayout) => {
      setPaneLayoutState(layout);
      if (layout === 'single') {
        setPanes((prev) => {
          const primary = prev.find((p) => p.position === 'primary');
          return primary ? [primary] : prev;
        });
        activePaneId && setActivePaneId(activePaneId);
      }
    },
    [activePaneId, setPaneLayoutState, setPanes, setActivePaneId],
  );

  const updatePaneSize = useCallback(
    (paneId: string, size: number) => {
      setPaneSizes((prev) => {
        // Count only actual pane IDs (exclude group:*, nested:*, grid:* keys)
        const actualPaneKeys = Object.keys(prev).filter(
          (key) => !key.startsWith('group:') && !key.startsWith('nested:') && !key.startsWith('grid:'),
        );
        const currentPanesCount = actualPaneKeys.length;

        const maxAllowedSize = 100 - MIN_PANE_WIDTH_PERCENT * (currentPanesCount - 1);
        // Clamp the size to ensure no pane goes below minimum width
        const clampedSize = Math.max(MIN_PANE_WIDTH_PERCENT, Math.min(maxAllowedSize, size));
        return { ...prev, [paneId]: clampedSize };
      });
    },
    [setPaneSizes],
  );

  const toggleLinkedScroll = useCallback(() => {
    setIsLinkedScrollEnabled((prev) => !prev);
  }, [setIsLinkedScrollEnabled]);

  return {
    closePane,
    switchPane,
    splitPane,
    splitIntoGrid,
    closeSplit,
    setPaneLayout,
    updatePaneSize,
    toggleLinkedScroll,
  };
}
