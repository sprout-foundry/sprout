import { useEffect, useRef, type MutableRefObject } from 'react';
import { type EditorBuffer } from '../types/editor';
import { readFileWithConsent } from '../services/fileAccess';
import { debugLog } from '../utils/log';
import { notificationBus } from '../services/notificationBus';

// Per-path notification cooldown to prevent toast storms from rapid
// WebSocket-originated file_content_changed events (e.g. build tools
// touching a file multiple times in quick succession).
const NOTIFY_COOLDOWN_MS = 4000;

// Per-path "just saved" timestamp to suppress redundant reloads triggered by
// the editor's own save operation.  fsnotify fires immediately after any
// write (including the editor's own save), so we must debounce ourselves
// rather than relying solely on server-side mtime filtering.
export const JUST_SAVED_THRESHOLD_MS = 3500;
export const justSavedRef = new Map<string, number>();

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
    // Track files the editor just saved so we can suppress the redundant
    // fsnotify echo that arrives via WebSocket shortly after.
    const handleEditorSaved = (e: Event) => {
      const detail = (e as CustomEvent).detail as { path?: string };
      if (detail.path) {
        // Prune stale entries (files saved >1.5s ago no longer need cooldown)
        const now = Date.now();
        justSavedRef.forEach((ts, p) => {
          if (now - ts > JUST_SAVED_THRESHOLD_MS) justSavedRef.delete(p);
        });
        justSavedRef.set(detail.path, Date.now());
      }
    };
    document.addEventListener('file:editor-saved', handleEditorSaved);
    const cleanupSaveListener = () => document.removeEventListener('file:editor-saved', handleEditorSaved);

    const handleExternalChange = async (e: Event) => {
      const detail = (e as CustomEvent).detail as {
        path: string;
        mtime: number;
        size: number;
        deleted: boolean;
      };
      if (!detail.path) return;

      // Suppress redundant reload for files the editor just saved.
      const justSavedAt = justSavedRef.get(detail.path) ?? 0;
      if (Date.now() - justSavedAt < JUST_SAVED_THRESHOLD_MS) return;

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

        // Re-read the buffer after the async gap — the user may have
        // typed new characters while we were fetching from disk.
        const freshBuffer = buffersRef.current.get(bufferId);
        if (!freshBuffer || freshBuffer.kind !== 'file') return;
        if (freshBuffer.isModified) return; // User made new edits since event fired

        // Skip reload if content is identical — avoid unnecessary UI churn
        // and undo history pollution.
        if (content === freshBuffer.content) {
          // Content hasn't changed — just update the watcher's mtime tracking.
          document.dispatchEvent(
            new CustomEvent('file:editor-saved', {
              detail: { path: detail.path, mtime: detail.mtime },
            }),
          );
          return;
        }

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
          notificationBus.notify('info', 'File Reloaded', `${freshBuffer.file.name} was modified externally and has been reloaded.`, 4000);
        }
      } catch (err) {
        // Non-critical: read failures are expected for some file types
        debugLog('[useAutoReloadCleanBuffers] failed to re-read externally modified file:', err);
      }
    };

    document.addEventListener('file_externally_modified', handleExternalChange);
    return () => {
      document.removeEventListener('file_externally_modified', handleExternalChange);
      cleanupSaveListener();
    };
  }, [buffersRef, reloadBufferFromDisk, setBufferExternallyModified]);
};
