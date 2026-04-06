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
 * Uses a counter to handle nested dragenter/dragleave events properly,
 * with a relatedTarget check to prevent flickering in some browsers.
 */
export function useFileDropZone({ containerRef, onFilesDropped }: UseFileDropZoneOptions): UseFileDropZoneReturn {
  const [isDragging, setIsDragging] = useState(false);
  const dragCounter = useRef(0);
  const isFileDrag = useRef(false);
  // Keep callback in a ref to avoid re-registering listeners when it changes.
  const onFilesDroppedRef = useRef(onFilesDropped);
  onFilesDroppedRef.current = onFilesDropped;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const handleDragEnter = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();

      // Check if this is a file drag (OS files)
      const types = e.dataTransfer?.types || [];
      const hasFiles = types.includes('Files');

      if (hasFiles) {
        isFileDrag.current = true;
        dragCounter.current++;
        setIsDragging(true);
      }
    };

    const handleDragOver = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();

      // Only allow drop if we're tracking a file drag and dataTransfer is available
      if (isFileDrag.current && e.dataTransfer) {
        e.dataTransfer.dropEffect = 'copy';
      }
    };

    const handleDragLeave = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();

      if (!isFileDrag.current) return;

      // Use relatedTarget to detect when the mouse moves between child elements
      // vs. actually leaving the container. This prevents flickering in some browsers.
      const relatedTarget = e.relatedTarget as Node | null;
      if (relatedTarget && container.contains(relatedTarget)) return;

      // Mouse actually left the container — reset everything
      dragCounter.current = 0;
      isFileDrag.current = false;
      setIsDragging(false);
    };

    const handleDrop = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();

      // Reset state
      dragCounter.current = 0;
      isFileDrag.current = false;
      setIsDragging(false);

      // Get dropped files if this was a file drag, filter by size
      if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
        const maxSize = MAX_DROP_FILE_SIZE;
        const droppedFiles = Array.from(e.dataTransfer.files).filter((file) => {
          if (file.size > maxSize) {
            console.warn(
              `[useFileDropZone] Dropped file "${file.name}" is too large (${(file.size / 1024 / 1024).toFixed(1)} MB, limit ${(maxSize / 1024 / 1024)} MB). Skipping.`,
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

    // Attach event listeners
    container.addEventListener('dragenter', handleDragEnter);
    container.addEventListener('dragover', handleDragOver);
    container.addEventListener('dragleave', handleDragLeave);
    container.addEventListener('drop', handleDrop);

    // Cleanup on unmount
    return () => {
      container.removeEventListener('dragenter', handleDragEnter);
      container.removeEventListener('dragover', handleDragOver);
      container.removeEventListener('dragleave', handleDragLeave);
      container.removeEventListener('drop', handleDrop);
    };
  }, [containerRef]);

  return { isDragging };
}
