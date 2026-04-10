import { useEffect, useRef, useCallback, useState } from 'react';
import type { EditorBuffer } from '../types/editor';
import { checkFilesModified } from '../services/apiFileCheck';
import { debugLog } from '../utils/log';

const POLL_INTERVAL_MS = 3000;
const COOLDOWN_MS = 5000; // Per-path cooldown to prevent duplicate dialog popups.
const BACKOFF_CAP_MS = 60000; // Max backoff delay when consecutive polls fail.

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
 * modified (or deleted) on disk. Uses exponential backoff on consecutive
 * failures (capped at 60s) and resets to base interval on any success.
 */
export function useExternalFileWatcher({ buffers }: WatcherOptions): WatcherReturn {
  const buffersRef = useRef(buffers);
  buffersRef.current = buffers;

  const mtimeRef = useRef<Map<string, number>>(new Map());
  const cooldownRef = useRef<Map<string, number>>(new Map());
  const seenOnceRef = useRef<Set<string>>(new Set());
  const consecutiveFailuresRef = useRef(0);

  const [running, setRunning] = useState(false);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const runningRef = useRef(running);
  runningRef.current = running;

  const clearTimeoutRef = useCallback(() => {
    if (timeoutRef.current !== null) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
  }, []);

  const performCheck = useCallback(async () => {
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return;

    const mtimes = mtimeRef.current;
    const cooldowns = cooldownRef.current;
    const seenOnce = seenOnceRef.current;
    const now = Date.now();

    // Snapshot open paths; learn mtimes for new paths.
    const openPaths = new Set<string>();
    for (const { path, mtime } of getWatchablePaths(buffersRef.current)) {
      openPaths.add(path);
      if (!mtimes.has(path)) mtimes.set(path, mtime);
    }

    // Prune state for closed files.
    mtimes.forEach((_, path) => {
      if (!openPaths.has(path)) {
        mtimes.delete(path);
        cooldowns.delete(path);
        seenOnce.delete(path);
      }
    });

    const entries: { path: string; mtime: number }[] = [];
    mtimes.forEach((mtime, path) => entries.push({ path, mtime }));
    if (entries.length === 0) { consecutiveFailuresRef.current = 0; return; }

    try {
      const response = await checkFilesModified(entries);
      consecutiveFailuresRef.current = 0;

      for (const result of response.modified) {
        const deleted = result.size === 0 && result.mod_time === 0;
        mtimes.set(result.path, result.mod_time);

        if (!seenOnce.has(result.path)) { seenOnce.add(result.path); continue; }
        const lastFired = cooldowns.get(result.path) || 0;
        if (now - lastFired < COOLDOWN_MS) continue;
        cooldowns.set(result.path, now);

        document.dispatchEvent(new CustomEvent('file_externally_modified', {
          detail: { path: result.path, mtime: result.mod_time, size: result.size, deleted },
        }));
      }
    } catch (err) {
      consecutiveFailuresRef.current += 1;
      debugLog(`[file-watcher] check failed (${consecutiveFailuresRef.current} consecutive):`, err);
    }
  }, []);

  const scheduleNext = useCallback((delay?: number) => {
    clearTimeoutRef();
    if (!runningRef.current) return;

    const failures = consecutiveFailuresRef.current;
    const backoff = Math.min(POLL_INTERVAL_MS * Math.pow(2, failures), BACKOFF_CAP_MS);
    timeoutRef.current = setTimeout(() => {
      timeoutRef.current = null;
      performCheck().then(() => scheduleNext());
    }, delay ?? backoff);
  }, [clearTimeoutRef, performCheck]);

  const startWatching = useCallback(() => { if (!runningRef.current) setRunning(true); }, []);
  const stopWatching = useCallback(() => { if (runningRef.current) setRunning(false); }, []);
  const forceCheck = useCallback(() => performCheck(), [performCheck]);

  // Update known mtime when a file is saved from the editor.
  useEffect(() => {
    const handleSave = (e: Event) => {
      const { path, mtime } = (e as CustomEvent).detail as { path?: string; mtime?: number };
      if (path) mtimeRef.current.set(path, mtime ?? Math.floor(Date.now() / 1000));
    };
    document.addEventListener('file:editor-saved', handleSave);
    return () => document.removeEventListener('file:editor-saved', handleSave);
  }, []);

  // Main polling loop: immediate check, then schedule with backoff.
  useEffect(() => {
    if (!running) { clearTimeoutRef(); return; }
    performCheck().then(() => scheduleNext());
    return () => clearTimeoutRef();
  }, [running, performCheck, scheduleNext, clearTimeoutRef]);

  // Immediate check when tab becomes visible (ignores backoff for quick recovery).
  useEffect(() => {
    const handler = () => {
      if (document.visibilityState === 'visible' && runningRef.current) {
        performCheck().then(() => scheduleNext());
      }
    };
    document.addEventListener('visibilitychange', handler);
    return () => document.removeEventListener('visibilitychange', handler);
  }, [performCheck, scheduleNext]);

  // Update known mtime from WebSocket file_externally_modified events.
  useEffect(() => {
    const handler = (e: Event) => {
      const { path, mtime } = (e as CustomEvent).detail as { path: string; mtime: number };
      if (path && typeof mtime === 'number') mtimeRef.current.set(path, mtime);
    };
    document.addEventListener('file_externally_modified', handler);
    return () => document.removeEventListener('file_externally_modified', handler);
  }, []);

  // Auto-start/stop based on whether there are file buffers.
  useEffect(() => {
    setRunning(getWatchablePaths(buffers).length > 0);
  }, [buffers]);

  return { startWatching, stopWatching, forceCheck };
}
