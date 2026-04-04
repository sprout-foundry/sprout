import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
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
}

/** Pane layout management: close, switch, split, grid, resize. */
export function usePaneManagement({
  panes, activePaneId, closeBuffer,
  setBuffers, setPanes, setPaneLayoutState, setActivePaneId, setActiveBufferId,
  setPaneSizes, setIsLinkedScrollEnabled,
}: UsePaneManagementParams) {
  const closePane = useCallback((paneId: string) => {
    if (panes.length === 1) return;
    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) closeBuffer(pane.bufferId);
    setPanes(prev => prev.filter(p => p.id !== paneId));
    if (paneId === activePaneId) {
      const remaining = panes.filter(p => p.id !== paneId);
      setActivePaneId(remaining[0]?.id || null);
    }
    // The useEffect on panes.length auto-syncs layout to 'single' when
    // panes are reduced to 1.
  }, [panes, activePaneId, closeBuffer, setPanes, setActivePaneId]);

  const switchPane = useCallback((paneId: string) => {
    setActivePaneId(paneId);
    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) setActiveBufferId(pane.bufferId);
  }, [panes, setActivePaneId, setActiveBufferId]);

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const splitPane = useCallback((paneId: string, direction: 'vertical' | 'horizontal') => {
    if (panes.length >= 4) return null;
    const usedIds = new Set(panes.map(p => p.id));
    const stableIds = ['pane-2', 'pane-3', 'pane-4'];
    const newPaneId = stableIds.find(id => !usedIds.has(id)) || `pane-${Date.now()}`;
    const newPanes: EditorPane[] = [
      ...panes,
      { id: newPaneId, bufferId: null, isActive: false,
        position: panes.length === 1 ? 'secondary' : panes.length === 2 ? 'tertiary' : 'quaternary' },
    ];
    setPanes(newPanes);
    if (panes.length === 1) {
      setPaneLayoutState(direction === 'vertical' ? 'split-vertical' : 'split-horizontal');
      setPaneSizes({ [panes[0].id]: 50, [newPaneId]: 50 });
    } else {
      setPaneSizes(prev => ({ ...prev, [newPaneId]: 50 }));
    }
    setActivePaneId(newPaneId);
    return newPaneId;
  }, [panes, setPanes, setPaneLayoutState, setPaneSizes, setActivePaneId]);

  const splitIntoGrid = useCallback(() => {
    const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
    if (!primaryPane) return panes.map(p => p.id);

    // Detach buffers from panes that will be replaced (non-primary).
    const primaryPaneId = primaryPane.id;
    const displacedPaneIds = new Set(panes.filter(p => p.id !== primaryPaneId).map(p => p.id));
    if (displacedPaneIds.size > 0) {
      setBuffers(prev => {
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

    const usedIds = new Set(panes.map(p => p.id));
    const newPaneIds = ['pane-2', 'pane-3', 'pane-4'].filter(id => !usedIds.has(id));

    const newPanes: EditorPane[] = [
      { ...primaryPane, position: 'primary' },
      { id: newPaneIds[0], bufferId: null, isActive: false, position: 'secondary' },
      { id: newPaneIds[1], bufferId: null, isActive: false, position: 'tertiary' },
      { id: newPaneIds[2], bufferId: null, isActive: false, position: 'quaternary' },
    ];

    setPanes(newPanes);
    setPaneLayoutState('split-grid');
    setActivePaneId(primaryPane.id);

    // Initialize grid pane sizes using restored values (if available) or 50/50
    setPaneSizes((prev) => ({
      'grid:col': (typeof prev['grid:col'] === 'number' && isFinite(prev['grid:col'])) ? prev['grid:col'] : 50,
      'grid:row': (typeof prev['grid:row'] === 'number' && isFinite(prev['grid:row'])) ? prev['grid:row'] : 50,
    }));

    return [primaryPane.id, ...newPaneIds];
  }, [panes, setBuffers, setPanes, setPaneLayoutState, setActivePaneId, setPaneSizes]);

  const closeSplit = useCallback(() => {
    const activePane = panes.find(p => p.id === activePaneId);

    // Close all non-primary panes
    panes.forEach(pane => {
      if (pane.position !== 'primary') closePane(pane.id);
    });

    const primaryPane = panes.find(p => p.position === 'primary');
    setPanes(prev => {
      const primary = prev.find(p => p.position === 'primary');
      return primary ? [primary] : prev;
    });
    setPaneLayoutState('single');
    setActivePaneId(primaryPane?.id || null);
    setPaneSizes({ [primaryPane?.id || 'pane-1']: 100 });

    // Preserve the buffer that was active
    const activeBufferToRestore = activePane?.bufferId || primaryPane?.bufferId;
    if (activeBufferToRestore) setActiveBufferId(activeBufferToRestore);
  }, [panes, activePaneId, closePane, setPanes, setPaneLayoutState, setActivePaneId, setPaneSizes, setActiveBufferId]);

  const setPaneLayout = useCallback((layout: PaneLayout) => {
    setPaneLayoutState(layout);
    if (layout === 'single') {
      setPanes(prev => {
        const primary = prev.find(p => p.position === 'primary');
        return primary ? [primary] : prev;
      });
      activePaneId && setActivePaneId(activePaneId);
    }
  }, [activePaneId, setPaneLayoutState, setPanes, setActivePaneId]);

  const updatePaneSize = useCallback((paneId: string, size: number) => {
    setPaneSizes(prev => ({ ...prev, [paneId]: size }));
  }, [setPaneSizes]);

  const toggleLinkedScroll = useCallback(() => {
    setIsLinkedScrollEnabled(prev => !prev);
  }, [setIsLinkedScrollEnabled]);

  return {
    closePane, switchPane, splitPane, splitIntoGrid,
    closeSplit, setPaneLayout, updatePaneSize, toggleLinkedScroll,
  };
}
