import { useCallback, useRef, type DragEvent } from 'react';

export interface UseTabDragReorderParams {
  paneId?: string;
  reorderBuffers: (draggedId: string, targetId: string) => void;
  moveBufferToPane: (bufferId: string, targetPaneId: string) => void;
}

export interface UseTabDragReorderReturn {
  handleDragStart: (e: DragEvent, bufferId: string) => void;
  handleDrop: (targetBufferId: string, sourceBufferId?: string | null) => void;
  resolveDraggedBufferId: (e: DragEvent) => string | null;
  handlePaneDrop: (e: DragEvent) => void;
  handleDragEnd: () => void;
}

/**
 * Hook that encapsulates drag-and-drop tab reorder logic.
 * Manages the dragging state and provides handlers for dragging tabs
 * between positions and across panes.
 *
 * Uses a ref to track the dragging ID so that handlers remain stable
 * across renders (no stale-closure risk with useCallback).
 */
export function useTabDragReorder({
  paneId,
  reorderBuffers,
  moveBufferToPane,
}: UseTabDragReorderParams): UseTabDragReorderReturn {
  const draggingIdRef = useRef<string | null>(null);

  const handleDragStart = useCallback((e: DragEvent, bufferId: string) => {
    draggingIdRef.current = bufferId;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', bufferId);
  }, []);

  const handleDrop = useCallback(
    (targetBufferId: string, sourceBufferId?: string | null) => {
      const draggedId = sourceBufferId ?? draggingIdRef.current;
      if (!draggedId || draggedId === targetBufferId) {
        draggingIdRef.current = null;
        return;
      }
      reorderBuffers(draggedId, targetBufferId);
      draggingIdRef.current = null;
    },
    [reorderBuffers],
  );

  const resolveDraggedBufferId = useCallback((e: DragEvent): string | null => {
    return draggingIdRef.current || e.dataTransfer.getData('text/plain') || null;
  }, []);

  const handlePaneDrop = useCallback(
    (e: DragEvent) => {
      e.preventDefault();
      // Must use resolveDraggedBufferId (not raw ref) to handle cross-pane drops
      // where handleDragStart fired on a different EditorTabs/hook instance.
      const draggedId = resolveDraggedBufferId(e);
      if (!draggedId || !paneId) {
        draggingIdRef.current = null;
        return;
      }
      moveBufferToPane(draggedId, paneId);
      draggingIdRef.current = null;
    },
    [paneId, moveBufferToPane, resolveDraggedBufferId],
  );

  const handleDragEnd = useCallback(() => {
    draggingIdRef.current = null;
  }, []);

  return {
    handleDragStart,
    handleDrop,
    resolveDraggedBufferId,
    handlePaneDrop,
    handleDragEnd,
  };
}
