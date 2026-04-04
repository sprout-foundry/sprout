import { useEffect, type MutableRefObject } from 'react';
import { type EditorBuffer } from '../types/editor';
import { readFileWithConsent } from '../services/fileAccess';

// ---------------------------------------------------------------------------
// useAutoReloadCleanBuffers
// ---------------------------------------------------------------------------
// Listens for `file_externally_modified` DOM events and automatically
// re-reads clean (unmodified) file buffers from disk. Modified buffers are
// left to EditorPane's conflict dialog via the `setBufferExternallyModified`
// path that is handled upstream.
// ---------------------------------------------------------------------------

interface UseAutoReloadCleanBuffersOptions {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  reloadBufferFromDisk: (bufferId: string, content: string, mtime?: number) => void;
}

export const useAutoReloadCleanBuffers = ({
  buffersRef,
  reloadBufferFromDisk,
}: UseAutoReloadCleanBuffersOptions): void => {
  // Auto-reload clean (unmodified) buffers when they change on disk.
  // Modified files are left to EditorPane's conflict dialog.
  useEffect(() => {
    const handleExternalChange = async (e: Event) => {
      const detail = (e as CustomEvent).detail as {
        path: string;
        mtime: number;
        size: number;
        deleted: boolean;
      };
      if (!detail.path || detail.deleted) return;

      // Find the buffer for this file
      let targetBufferId: string | null = null;
      buffersRef.current.forEach((b, id) => {
        if (b.kind === 'file' && b.file.path === detail.path) {
          targetBufferId = id;
        }
      });

      if (!targetBufferId) return;

      const targetBuffer = buffersRef.current.get(targetBufferId);
      if (!targetBuffer || targetBuffer.kind !== 'file') return;

      // Only auto-reload clean (unmodified) buffers; let EditorPane handle modified ones
      if (targetBuffer.isModified) return;

      const bufferId: string = targetBufferId;

      // Re-read the file from disk
      try {
        const response = await readFileWithConsent(detail.path);
        if (!response.ok) return;
        const content = await response.text();

        reloadBufferFromDisk(bufferId, content, detail.mtime);

        document.dispatchEvent(
          new CustomEvent('file:editor-saved', {
            detail: { path: detail.path, mtime: detail.mtime },
          }),
        );

        // Dispatch an event so EditorPane can reload the CodeMirror view
        document.dispatchEvent(
          new CustomEvent('file:auto-reloaded', {
            detail: { bufferId, content },
          }),
        );
      } catch {
        // Silently ignore read failures
      }
    };

    document.addEventListener('file_externally_modified', handleExternalChange);
    return () => document.removeEventListener('file_externally_modified', handleExternalChange);
  }, [buffersRef, reloadBufferFromDisk]);
};
