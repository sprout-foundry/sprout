import { useEffect, useRef, useCallback, useState } from 'react';
import type { EditorBuffer } from '../types/editor';
import { checkFilesModified } from '../services/apiFileCheck';
import { debugLog } from '../utils/log';

const POLL_INTERVAL_MS = 3000;
// Cooldown per path (ms) to prevent duplicate dialog popups when both
// WebSocket and polling detect the same change.
const COOLDOWN_MS = 5000;

interface WatcherOptions {
  buffers: Map<string, EditorBuffer>;
}

interface WatcherReturn {
  startWatching: () => void;
  stopWatching: () => void;
  forceCheck: () => Promise<void>;
}

function getWatchablePaths(buffers: Map<string, EditorBuffer>): { path: string; mtime: number }[] {
  const result: { path: string; mtime: number }[] = [];
  for (const buf of Array.from(buffers.values())) {
    if (buf.kind === 'file' && buf.file.path && !buf.file.path.startsWith('__workspace/')) {
      result.push({ path: buf.file.path, mtime: buf.file.modified });
    }
  }
  return result;
}

/**
 * Polls the backend to detect when files open in the editor have been
 * modified (or deleted) on disk. Dispatches `file_externally_modified`
 * custom DOM events so any component can react without coupling to this hook.
 */
export function useExternalFileWatcher({ buffers }: WatcherOptions): WatcherReturn {
  const buffersRef = useRef(buffers);
  buffersRef.current = buffers;

  // Known mtimes keyed by file path. Starts empty; populated from buffers
  // on the first poll, then updated after each successful check.
  const mtimeRef = useRef<Map<string, number>>(new Map());

  // Cooldown timestamps per path to suppress duplicate notifications.
  const cooldownRef = useRef<Map<string, number>>(new Map());

  // Tracks paths that have completed at least one successful poll cycle.
  // Prevents false positives when buffers are created with modified=0.
  const seenOnceRef = useRef<Set<string>>(new Set());

  const [running, setRunning] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const runningRef = useRef(running);
  runningRef.current = running;

  const performCheck = useCallback(async () => {
    // Skip polling when the tab is hidden.
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
      return;
    }

    const bufs = buffersRef.current;
    const mtimes = mtimeRef.current;
    const cooldowns = cooldownRef.current;
    const seenOnce = seenOnceRef.current;
    const now = Date.now();

    // Build the set of paths currently open in buffers.
    const openPaths = new Set<string>();
    const watchable = getWatchablePaths(bufs);
    for (const { path, mtime } of watchable) {
      openPaths.add(path);
      if (!mtimes.has(path)) {
        mtimes.set(path, mtime);
      }
    }

    // Prune mtimes and cooldowns for files that are no longer open.
    mtimes.forEach((_, path) => {
      if (!openPaths.has(path)) {
        mtimes.delete(path);
        cooldowns.delete(path);
        seenOnce.delete(path);
      }
    });

    // Build the list of files to check.
    const entries: { path: string; mtime: number }[] = [];
    mtimes.forEach((mtime, path) => {
      entries.push({ path, mtime });
    });

    if (entries.length === 0) return;

    try {
      const response = await checkFilesModified(entries);

      for (const result of response.modified) {
        const deleted = result.size === 0 && result.mod_time === 0;

        // Update known mtime.
        mtimes.set(result.path, result.mod_time);

        // On first poll for a new path, just learn the mtime — don't fire.
        // This prevents false positives when a buffer is created with modified=0.
        if (!seenOnce.has(result.path)) {
          seenOnce.add(result.path);
          continue;
        }

        // Skip if this path is on cooldown (duplicate notification guard).
        const lastFired = cooldowns.get(result.path) || 0;
        if (now - lastFired < COOLDOWN_MS) {
          continue;
        }
        cooldowns.set(result.path, now);

        const event = new CustomEvent('file_externally_modified', {
          detail: { path: result.path, mtime: result.mod_time, size: result.size, deleted },
        });
        document.dispatchEvent(event);
      }
    } catch (err) {
      debugLog('[file-watcher] check failed:', err);
    }
  }, []);

  const startWatching = useCallback(() => {
    if (runningRef.current) return;
    setRunning(true);
  }, []);

  const stopWatching = useCallback(() => {
    if (!runningRef.current) return;
    setRunning(false);
  }, []);

  const forceCheck = useCallback(async () => {
    await performCheck();
  }, [performCheck]);

  // When a file is saved from the editor, update our known mtime so we don't
  // falsely report the file as "changed by another process" on the next poll.
  useEffect(() => {
    const handleSave = (e: Event) => {
      const detail = (e as CustomEvent).detail as { path?: string; mtime?: number };
      if (detail.path && typeof detail.mtime === 'number') {
        mtimeRef.current.set(detail.path, detail.mtime);
      }
      // Also update mtime from the response mtime if available.
      if (detail.path && !detail.mtime) {
        // Use current time as a reasonable fallback (within 1-second precision).
        mtimeRef.current.set(detail.path, Math.floor(Date.now() / 1000));
      }
    };

    document.addEventListener('file:editor-saved', handleSave);
    return () => document.removeEventListener('file:editor-saved', handleSave);
  }, []);

  useEffect(() => {
    if (!running) {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    // Run an immediate check on start, then set up the interval.
    performCheck();
    intervalRef.current = setInterval(performCheck, POLL_INTERVAL_MS);

    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [running, performCheck]);

  // Trigger an immediate check when the tab becomes visible again.
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible' && runningRef.current) {
        performCheck();
      }
    };
    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange);
  }, [performCheck]);

  // When a WebSocket file_externally_modified event fires, update our known
  // mtime so the next polling check doesn't produce a duplicate notification.
  useEffect(() => {
    const handleExternalModified = (e: Event) => {
      const detail = (e as CustomEvent).detail as { path: string; mtime: number };
      if (detail.path && typeof detail.mtime === 'number') {
        mtimeRef.current.set(detail.path, detail.mtime);
      }
    };
    document.addEventListener('file_externally_modified', handleExternalModified);
    return () => document.removeEventListener('file_externally_modified', handleExternalModified);
  }, []);

  // Auto-start when there are file buffers, auto-stop when empty.
  useEffect(() => {
    const hasFiles = getWatchablePaths(buffers).length > 0;
    setRunning(hasFiles);
  }, [buffers]);

  return { startWatching, stopWatching, forceCheck };
}
