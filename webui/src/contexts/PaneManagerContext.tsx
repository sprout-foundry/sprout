import React, { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import type { EditorPane, PaneLayout, PaneSize } from '../types/editor';

// ---------------------------------------------------------------------------
// Pane configuration constants
// ---------------------------------------------------------------------------

export const MIN_PANE_WIDTH_PERCENT = 8;

/** Normalize pane sizes so they sum to exactly 100 */
export function normalizePaneSize(size: number, total: number): number {
  if (total === 0) return size;
  return (size / total) * 100;
}

// ---------------------------------------------------------------------------
// Pane Context Interface
// ---------------------------------------------------------------------------

interface PaneManagerContextValue {
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  paneSizes: PaneSize;
  isLinkedScrollEnabled: boolean;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updatePaneSize: (paneId: string, size: number) => void;
  toggleLinkedScroll: () => void;
  setActiveBufferId: (id: string | null) => void;
  setActivePaneId: (id: string | null) => void;
  setPanes: React.Dispatch<React.SetStateAction<EditorPane[]>>;
  setPaneLayoutState: (layout: PaneLayout) => void;
  setPaneSizes: React.Dispatch<React.SetStateAction<PaneSize>>;
}

const PaneManagerContext = createContext<PaneManagerContextValue | null>(null);

export const usePaneManager = () => {
  const context = useContext(PaneManagerContext);
  if (!context) {
    throw new Error('usePaneManager must be used within PaneManagerProvider');
  }
  return context;
};

interface PaneManagerProviderProps {
  children: ReactNode;
  maxPanes: number;
  closeBuffer: (bufferId: string) => void;
}

export const PaneManagerProvider: React.FC<PaneManagerProviderProps> = ({ 
  children, 
  maxPanes, 
  closeBuffer 
}) => {
  const [panes, setPanes] = useState<EditorPane[]>([
    { id: 'pane-1', bufferId: 'buffer-chat', isActive: true, position: 'primary' }
  ]);
  const [paneLayout, setPaneLayoutState] = useState<PaneLayout>('single');
  const [activePaneId, setActivePaneId] = useState<string | null>('pane-1');
  const [activeBufferId, setActiveBufferId] = useState<string | null>('buffer-chat');
  const [paneSizes, setPaneSizes] = useState<PaneSize>({ 'pane-1': 100 });
  const [isLinkedScrollEnabled, setIsLinkedScrollEnabled] = useState(false);

  const switchPane = useCallback((paneId: string) => {
    setActivePaneId(paneId);
    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) {
      setActiveBufferId(pane.bufferId);
    }
  }, [panes]);

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const splitPane = useCallback((paneId: string, direction: 'vertical' | 'horizontal') => {
    if (panes.length >= maxPanes) return null;

    const newPaneId = `pane-${Date.now()}`;

    // Determine the position for the new pane based on current count
    const positionValues: Array<'primary' | 'secondary' | 'tertiary' | 'quaternary' | 'quinary' | 'senary'> =
      ['primary', 'secondary', 'tertiary', 'quaternary', 'quinary', 'senary'];
    const newPosition = positionValues[panes.length];

    const newPanes: EditorPane[] = [
      ...panes,
      {
        id: newPaneId,
        bufferId: null,
        isActive: false,
        position: newPosition
      }
    ];

    setPanes(newPanes);

    // Update layout and distribute sizes evenly
    const newPaneCount = panes.length + 1;
    if (panes.length === 1) {
      setPaneLayoutState(direction === 'vertical' ? 'split-vertical' : 'split-horizontal');
      setPaneSizes({
        [panes[0].id]: 50,
        [newPaneId]: 50
      });
    } else {
      const evenSize = 100 / newPaneCount;
      const newSizes: Record<string, number> = {};
      newPanes.forEach(pane => {
        newSizes[pane.id] = evenSize;
      });
      setPaneSizes(newSizes);
    }

    setActivePaneId(newPaneId);
    return newPaneId;
  }, [panes, maxPanes]);

  const closePane = useCallback((paneId: string) => {
    if (panes.length === 1) return;

    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) {
      closeBuffer(pane.bufferId);
    }

    const remaining = panes.filter(p => p.id !== paneId);
    setPanes(remaining);

    const evenSize = remaining.length === 1 ? 100 : 100 / remaining.length;
    const newSizes: Record<string, number> = {};
    remaining.forEach(p => {
      newSizes[p.id] = evenSize;
    });
    setPaneSizes(newSizes);

    if (paneId === activePaneId) {
      setActivePaneId(remaining[0]?.id || null);
    }

    if (remaining.length === 1) {
      setPaneLayoutState('single');
    }
  }, [panes, activePaneId, closeBuffer]);

  const closeSplit = useCallback(() => {
    const activePane = panes.find(p => p.id === activePaneId);

    panes.forEach(pane => {
      if (pane.position !== 'primary' && pane.id !== activePaneId) {
        closePane(pane.id);
      }
    });

    setPanes(prev => {
      const primaryPane = prev.find(p => p.position === 'primary');
      return primaryPane ? [primaryPane] : prev;
    });

    setPaneLayoutState('single');
    setActivePaneId(panes[0]?.id || null);
    setPaneSizes({ [panes[0]?.id || 'pane-1']: 100 });

    const remainingBuffer = activePane?.bufferId;
    if (remainingBuffer) {
      setActiveBufferId(remainingBuffer);
    }
  }, [panes, activePaneId, closePane]);

  const setPaneLayout = useCallback((layout: PaneLayout) => {
    setPaneLayoutState(layout);

    if (layout === 'single') {
      setPanes(prev => {
        const primary = prev.find(p => p.position === 'primary');
        return primary ? [primary] : prev;
      });
      activePaneId && setActivePaneId(activePaneId);
    }
  }, [activePaneId]);

  const moveBufferToPane = useCallback((bufferId: string, paneId: string) => {
    setPanes((prev) => prev.map((pane) => {
      if (pane.id === paneId) {
        return { ...pane, bufferId };
      }
      if (pane.bufferId === bufferId) {
        return { ...pane, bufferId: null };
      }
      return pane;
    }));

    if (activePaneId === paneId) {
      setActiveBufferId(bufferId);
    }
  }, [activePaneId]);

  const updatePaneSize = useCallback((paneId: string, size: number) => {
    setPaneSizes(prev => {
      const actualPaneKeys = Object.keys(prev).filter(
        key => !key.startsWith('group:') && !key.startsWith('nested:') && !key.startsWith('grid:')
      );
      const currentPanesCount = actualPaneKeys.length;

      const maxAllowedSize = 100 - MIN_PANE_WIDTH_PERCENT * (currentPanesCount - 1);
      const clampedSize = Math.max(MIN_PANE_WIDTH_PERCENT, Math.min(maxAllowedSize, size));
      return {
        ...prev,
        [paneId]: clampedSize
      };
    });
  }, []);

  const toggleLinkedScroll = useCallback(() => {
    setIsLinkedScrollEnabled(prev => !prev);
  }, []);

  const value: PaneManagerContextValue = {
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    paneSizes,
    isLinkedScrollEnabled,
    moveBufferToPane,
    closePane,
    switchPane,
    splitPane,
    closeSplit,
    setPaneLayout,
    updatePaneSize,
    toggleLinkedScroll,
    setActiveBufferId,
    setActivePaneId,
    setPanes,
    setPaneLayoutState,
    setPaneSizes,
  };

  return (
    <PaneManagerContext.Provider value={value}>
      {children}
    </PaneManagerContext.Provider>
  );
};
