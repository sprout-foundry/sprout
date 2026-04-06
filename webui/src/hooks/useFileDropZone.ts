import { useEffect, useRef, useState, type RefObject } from 'react';

interface UseFileDropZoneOptions {
  containerRef: RefObject<HTMLDivElement | null>;
  onFilesDropped: (files: File[]) => void;
}

interface UseFileDropZoneReturn {
  isDragging: boolean;
}

/** Maximum file size for dropped files (10 MB). */
const MAX_DROP_FILE_SIZE = 10 * 1024 * 1024;

/**
 * Hook that attaches drag-and-drop event listeners to a container element.
 * Tracks file drops from the OS and calls onFilesDropped with the dropped files.
 * Uses a drag counter to handle nested elements properly,
 * preventing flickering when the mouse moves between child elements.
 */
export function useFileDropZone({ containerRef, onFilesDropped }: UseFileDropZoneOptions): UseFileDropZoneReturn {
  const [isDragging, setIsDragging] = useState(false);
  const isFileDrag = useRef(false);
  const dragCounter = useRef(0);
  // Keep callback in a ref to avoid re-registering listeners when it changes.
  const onFilesDroppedRef = useRef(onFilesDropped);
  onFilesDroppedRef.current = onFilesDropped;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const handleDragEnter = (e: DragEvent) => {
      // Check if this is a file drag (OS files)
      const types = e.dataTransfer?.types || [];
      const hasFiles = types.includes('Files');

      // Only intercept file drags, let internal drags pass through naturally
      if (!hasFiles) {
        return;
      }

      e.preventDefault();
      e.stopPropagation();

      isFileDrag.current = true;
      dragCounter.current++;
      setIsDragging(true);
    };

    const handleDragOver = (e: DragEvent) => {
      // Only allow drop if we're tracking a file drag
      if (!isFileDrag.current) {
        return;
      }

      e.preventDefault();
      e.stopPropagation();

      if (e.dataTransfer) {
        e.dataTransfer.dropEffect = 'copy';
      }
    };

    const handleDragLeave = (e: DragEvent) => {
      // Only handle file drags
      if (!isFileDrag.current) {
        return;
      }

      e.preventDefault();
      e.stopPropagation();

      // Decrement counter and check if we've left the container completely
      dragCounter.current--;
      if (dragCounter.current === 0) {
        // Mouse actually left the container — reset everything
        isFileDrag.current = false;
        dragCounter.current = 0;
        setIsDragging(false);
      }
    };

    const handleDrop = (e: DragEvent) => {
      e.preventDefault();
      // Note: stopPropagation/stopImmediatePropagation only prevent other native
      // listeners on the same element from firing. React 18+ delegates events to
      // the root, so child React onDrop handlers (e.g. EditorTabs tab reorder)
      // may still fire — they guard against OS file drops internally by checking
      // for internal drag data.
      e.stopPropagation();
      e.stopImmediatePropagation();

      // Reset state
      isFileDrag.current = false;
      dragCounter.current = 0;
      setIsDragging(false);

      // Get dropped files if this was a file drag, filter by size
      if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
        const maxSize = MAX_DROP_FILE_SIZE;
        const droppedFiles = Array.from(e.dataTransfer.files).filter((file) => {
          if (file.size > maxSize) {
            console.warn(
              `[useFileDropZone] Dropped file "${file.name}" is too large (${(file.size / 1024 / 1024).toFixed(1)} MB, limit ${maxSize / 1024 / 1024} MB). Skipping.`,
            );
            return false;
          }
          return true;
        });
        if (droppedFiles.length > 0) {
          onFilesDroppedRef.current(droppedFiles);
        }
      }
    };

    // Safety net: for intra-page drags that were identified as file drags, the
    // browser may cancel the drag without firing drop/dragleave (e.g. source
    // element removed from DOM, Escape pressed while dragging a draggable).
    // Note: OS-initiated file drags don't fire dragend (no source element),
    // but they do fire dragleave which the counter already handles.
    const handleDragEnd = () => {
      isFileDrag.current = false;
      dragCounter.current = 0;
      setIsDragging(false);
    };

    // Attach event listeners
    container.addEventListener('dragenter', handleDragEnter);
    container.addEventListener('dragover', handleDragOver);
    container.addEventListener('dragleave', handleDragLeave);
    container.addEventListener('drop', handleDrop);
    document.addEventListener('dragend', handleDragEnd);

    // Cleanup on unmount
    return () => {
      container.removeEventListener('dragenter', handleDragEnter);
      container.removeEventListener('dragover', handleDragOver);
      container.removeEventListener('dragleave', handleDragLeave);
      container.removeEventListener('drop', handleDrop);
      document.removeEventListener('dragend', handleDragEnd);
    };
  }, [containerRef]);

  return { isDragging };
}
