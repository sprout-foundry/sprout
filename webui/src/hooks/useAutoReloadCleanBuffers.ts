import { useEffect, useRef, type MutableRefObject } from 'react';
import { type EditorBuffer } from '../types/editor';
import { readFileWithConsent } from '../services/fileAccess';
import { debugLog } from '../utils/log';
import { notificationBus } from '../services/notificationBus';

// Per-path notification cooldown to prevent toast storms from rapid
// WebSocket-originated file_content_changed events (e.g. build tools
// touching a file multiple times in quick succession).
const NOTIFY_COOLDOWN_MS = 4000;

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
  setBufferExternallyModified?: (bufferId: string, diskContent: string, mtime?: number) => void;
}

export const useAutoReloadCleanBuffers = ({
  buffersRef,
  reloadBufferFromDisk,
  setBufferExternallyModified,
}: UseAutoReloadCleanBuffersOptions): void => {
  // Per-path last-notification timestamp to deduplicate rapid events.
  const lastNotifiedRef = useRef<Map<string, number>>(new Map());

  // Paths that have already been notified as deleted. Cleared when the
  // file reappears (non-deleted event) or the buffer is removed.
  const deletedNotifiedRef = useRef<Set<string>>(new Set());

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
      if (!detail.path) return;

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

      // Handle deleted files on clean buffers
      if (detail.deleted) {
        if (deletedNotifiedRef.current.has(detail.path)) return;
        deletedNotifiedRef.current.add(detail.path);
        const now = Date.now();
        const lastNotified = lastNotifiedRef.current.get(detail.path) ?? 0;
        if (now - lastNotified >= NOTIFY_COOLDOWN_MS) {
          lastNotifiedRef.current.set(detail.path, now);
          notificationBus.notify('warning', 'File Deleted', `${targetBuffer.file.name} has been deleted from disk.`, 6000);
        }
        if (setBufferExternallyModified) {
          setBufferExternallyModified(bufferId, '');
        }
        return;
      }

      // Re-read the file from disk
      try {
        // If the file was previously notified as deleted, clear that tracking
        // now that it has reappeared.
        deletedNotifiedRef.current.delete(detail.path);

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

        // Show notification for successful auto-reload (debounced per-path)
        const reloadNow = Date.now();
        const lastReloadNotify = lastNotifiedRef.current.get(detail.path) ?? 0;
        if (reloadNow - lastReloadNotify >= NOTIFY_COOLDOWN_MS) {
          lastNotifiedRef.current.set(detail.path, reloadNow);
          notificationBus.notify('info', 'File Reloaded', `${targetBuffer.file.name} was modified externally and has been reloaded.`, 4000);
        }
      } catch (err) {
        // Non-critical: read failures are expected for some file types
        debugLog('[useAutoReloadCleanBuffers] failed to re-read externally modified file:', err);
      }
    };

    document.addEventListener('file_externally_modified', handleExternalChange);
    return () => document.removeEventListener('file_externally_modified', handleExternalChange);
  }, [buffersRef, reloadBufferFromDisk, setBufferExternallyModified]);
};
