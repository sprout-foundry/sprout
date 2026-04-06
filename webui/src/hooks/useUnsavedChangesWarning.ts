import { useEffect } from 'react';
import type { MutableRefObject } from 'react';
import type { EditorBuffer } from '../types/editor';

interface UseUnsavedChangesWarningParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  buffers: Map<string, EditorBuffer>;
  activeBufferId: string | null;
}

/**
 * Hook to warn users about unsaved changes when closing the tab or browser,
 * and to keep the document.title in sync with the active buffer's state.
 *
 * Effect 1: Registers a beforeunload event listener that checks if any real
 * file buffer has isModified === true. If so, triggers the browser's native
 * "Leave site?" dialog.
 *
 * Effect 2: Updates document.title to reflect the active buffer:
 *   - Modified file: "● filename — ledit"
 *   - Clean file:    "filename — ledit"
 *   - Other / none:  "ledit — AI Code Editor"
 */
export function useUnsavedChangesWarning({
  buffersRef,
  buffers,
  activeBufferId,
}: UseUnsavedChangesWarningParams): void {
  // Effect 1: Warn on beforeunload if any file buffer is modified
  useEffect(() => {
    const handler = (event: BeforeUnloadEvent): void => {
      const hasModifiedFileBuffers = Array.from(buffersRef.current.values()).some(
        (buffer) => buffer.kind === 'file' && buffer.isModified,
      );

      if (hasModifiedFileBuffers) {
        event.preventDefault();
        // Legacy returnValue for cross-browser compatibility (MDN recommendation)
        event.returnValue = '';
      }
    };

    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [buffersRef]);

  // Effect 2: Update document.title based on active buffer's modified state
  useEffect(() => {
    const activeBuffer = activeBufferId ? buffers.get(activeBufferId) : undefined;

    if (!activeBuffer || activeBuffer.kind !== 'file') {
      document.title = 'ledit — AI Code Editor';
      return;
    }

    const indicator = activeBuffer.isModified ? '● ' : '';
    document.title = `${indicator}${activeBuffer.file.name} — ledit`;
  }, [activeBufferId, buffers]);
}
