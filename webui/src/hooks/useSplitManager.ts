import { useCallback, useState, useEffect } from 'react';
import type { EditorPane, PaneLayout } from '../types/editor';

interface NestedSplit {
  hostPaneId: string;
  nestedPaneId: string;
  direction: 'vertical' | 'horizontal';
}

export interface UseSplitManagerOptions {
  activePaneId: string | null;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  splitIntoGrid: () => string[];
  closeSplit: () => void;
  closePane: (paneId: string) => void;
  updatePaneSize: (paneId: string, size: number) => void;
  switchToBuffer: (bufferId: string) => void;
}

export interface UseSplitManagerReturn {
  handleSplitRequest: (direction: 'vertical' | 'horizontal' | 'grid') => void;
  handleCloseAllSplits: () => void;
  nestedSplit: NestedSplit | null;
  onNestedSplitChange: React.Dispatch<React.SetStateAction<NestedSplit | null>>;
  canSplit: boolean;
  canSplitGrid: boolean;
  canCloseSplit: boolean;
}

/**
 * Manages editor pane splitting logic including nested splits, grid layout,
 * and derived split capability booleans.
 */
export function useSplitManager({
  activePaneId,
  panes,
  paneLayout,
  splitPane,
  splitIntoGrid,
  closeSplit,
  closePane,
  updatePaneSize,
  switchToBuffer,
}: UseSplitManagerOptions): UseSplitManagerReturn {
  const [nestedSplit, setNestedSplit] = useState<NestedSplit | null>(null);

  const handleSplitRequest = useCallback(
    (direction: 'vertical' | 'horizontal' | 'grid') => {
      if (direction === 'grid') {
        if (paneLayout === 'split-grid' && panes.length === 4) {
          const primaryPane = panes.find((p) => p.position === 'primary') || panes[0];
          if (primaryPane) {
            const bufId = primaryPane.bufferId;
            closeSplit();
            if (bufId) switchToBuffer(bufId);
          }
          return;
        }
        const primaryPane = panes.find((p) => p.position === 'primary') || panes[0];
        const bufId = primaryPane?.bufferId;
        splitIntoGrid();
        if (bufId) switchToBuffer(bufId);
        return;
      }

      if (!activePaneId) return;

      const previousPaneCount = panes.length;
      const newPaneId = splitPane(activePaneId, direction);
      if (!newPaneId) return;

      if (previousPaneCount === 2) {
        setNestedSplit({
          hostPaneId: activePaneId,
          nestedPaneId: newPaneId,
          direction,
        });
        updatePaneSize(`group:${activePaneId}`, 50);
        updatePaneSize(`nested:${activePaneId}`, 50);
      }
    },
    [activePaneId, panes, paneLayout, splitPane, splitIntoGrid, closeSplit, updatePaneSize, switchToBuffer],
  );

  const handleCloseAllSplits = useCallback(() => {
    if (paneLayout === 'split-grid' && panes.length === 4) {
      const primaryPane = panes.find((p) => p.position === 'primary') || panes[0];
      const bufId = primaryPane?.bufferId;
      closeSplit();
      if (bufId) switchToBuffer(bufId);
      return;
    }
    if (nestedSplit) {
      closePane(nestedSplit.nestedPaneId);
      setNestedSplit(null);
    } else {
      closeSplit();
    }
  }, [closeSplit, closePane, nestedSplit, paneLayout, panes, switchToBuffer]);

  const canSplit = panes.length < 3;
  const canSplitGrid = paneLayout !== 'split-grid';
  const canCloseSplit = panes.length > 1;

  // Clean up nested split state when panes are reduced below 3
  useEffect(() => {
    if (panes.length < 3 && nestedSplit) {
      setNestedSplit(null);
    }
  }, [nestedSplit, panes.length]);

  return {
    handleSplitRequest,
    handleCloseAllSplits,
    nestedSplit,
    onNestedSplitChange: setNestedSplit,
    canSplit,
    canSplitGrid,
    canCloseSplit,
  };
}
