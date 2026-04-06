import { useEffect } from 'react';
import type { MutableRefObject } from 'react';
import type { EditorBuffer } from '../types/editor';

interface UseUnsavedChangesWarningParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
}

/**
 * Hook to warn users about unsaved changes when closing the tab or browser.
 *
 * Registers a beforeunload event listener that checks if any real file buffer
 * has isModified === true. If so, triggers the browser's native "Leave site?"
 * dialog.
 */
export function useUnsavedChangesWarning({ buffersRef }: UseUnsavedChangesWarningParams): void {
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
}
