import { useEffect } from 'react';
import type { MutableRefObject } from 'react';
import { getActiveHost } from '../host/accessor';
import { productName } from '../host/branding';
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
 * "Leave site?" dialog. This is the last line of defense for unsaved work —
 * layout persistence stores paths/cursor/scroll but never buffer content,
 * so a reload with modified buffers loses those edits.
 *
 * Effect 2: Updates document.title to reflect the active buffer, using the
 * host's product name (Sprout unless the host brands itself):
 *   - Modified file: "● filename — <product name>"
 *   - Clean file:    "filename — <product name>"
 *   - Other / none:  "<product name>"
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
    // The host's own name (Sprout unless it brands itself), read through the
    // non-React accessor so this hook does not require a provider.
    const name = productName(getActiveHost());

    if (!activeBuffer || activeBuffer.kind !== 'file') {
      document.title = name;
      return;
    }

    const indicator = activeBuffer.isModified ? '● ' : '';
    document.title = `${indicator}${activeBuffer.file.name} — ${name}`;
  }, [activeBufferId, buffers]);
}
