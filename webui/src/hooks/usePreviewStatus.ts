/**
 * usePreviewStatus — drives the preview pane from
 * the dev-server lifecycle API (pkg/webui/api_preview.go).
 *
 * Polling lifecycle (the pane's states): poll once when the surface becomes
 * active (the panel opens), then maintain an interval — fast while `starting`
 * (readiness settles in the background after a start), a slow heartbeat
 * otherwise (to catch a running dev server dying or being stopped). All
 * polling stops when the surface goes inactive, so a closed panel never
 * polls. At most ONE status request is ever in flight: a tick that arrives
 * while a poll is still pending (e.g. behind a hung fetch) skips rather than
 * stacks, and the poll loop's own AbortController cancels an in-flight poll
 * when the surface closes, unmounts, or the cadence switches. The `start` /
 * `restart` / `stop` actions POST to the matching endpoints and adopt the
 * returned state (they are never aborted); a pending action owns the
 * transition so a stale in-flight poll cannot clobber it.
 *
 * `reloadKey` bumps (debounced) when workspace files change, so the pane can
 * re-mount its iframe and reload the embedded app.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { getPreviewStatus, restartPreview, startPreview, stopPreview } from '../services/api/previewApi';
import type { PreviewState } from '../services/api/types/preview';
import type { SproutEvent } from '../types/events';
import { changesWorkspaceFiles } from './workspaceFileEvents';

/** Poll cadence while the app is starting (readiness settles in the background). */
const STARTING_POLL_MS = 1000;
/** Poll cadence otherwise (a slow heartbeat that catches a server dying). */
const HEARTBEAT_POLL_MS = 5000;
/** Debounce for file-change reloads so a burst of edits reloads once. */
const RELOAD_DEBOUNCE_MS = 800;

export interface UsePreviewStatusReturn {
  /** Lifecycle state of the dev server (the pane's `status` prop). */
  status: PreviewState['status'];
  /** Embed URL of the running app (present while running). */
  url?: string;
  /** The server's reason (a stop/failed explanation) — the pane's `error`. */
  error?: string;
  /** True when the running server was detected, not started by the manager. */
  detected?: boolean;
  /** True when the URL is a platform-registered (hosted) preview. */
  hosted?: boolean;
  /** The last start/restart/stop failure message, if any. */
  actionError?: string;
  /** Opaque reload token for the pane's iframe (bumps on file changes). */
  reloadKey: number;
  /** Start (or detect) the dev server from the manifest's dev command. */
  start: () => void;
  /** Stop the manager's dev server and start it again. */
  restart: () => void;
  /** Stop the dev server the manager started. */
  stop: () => void;
}

/**
 * Drives the preview pane from the dev-server lifecycle API.
 *
 * @param enabled Whether the preview surface is active (the panel is open).
 *   Polling and reload tracking run only while enabled, so a closed panel
 *   makes no requests.
 */
export function usePreviewStatus(enabled: boolean): UsePreviewStatusReturn {
  const fetchFn = useSproutFetch();
  const [state, setState] = useState<PreviewState>({ status: 'stopped' });
  const [actionError, setActionError] = useState<string | undefined>(undefined);
  const [reloadKey, setReloadKey] = useState(0);

  // The latest state, for pollOnce to report on a transport failure.
  const stateRef = useRef(state);
  stateRef.current = state;
  // True while a start/restart/stop is in flight: a pending action owns the
  // state transition, so a stale in-flight poll must not apply over it.
  const actionInFlightRef = useRef(false);

  // True while a poll is in flight: a pending poll owns the request, so a
  // new tick (or an immediate re-poll after a cadence switch) cannot stack a
  // second status request behind a hung one — at most one is ever in flight.
  const pollInFlightRef = useRef(false);
  // The enabled value on the previous render (prev-value pattern): the poll
  // effect uses it to tell a fresh open (poll immediately) from a cadence
  // switch (just re-arm the interval).
  const prevEnabledRef = useRef(false);

  const pollOnce = useCallback(
    async (signal?: AbortSignal): Promise<PreviewState> => {
      if (pollInFlightRef.current) {
        // A poll is still pending (possibly hung): skip this tick rather than
        // stacking another request behind it.
        return stateRef.current;
      }
      pollInFlightRef.current = true;
      try {
        const st = await getPreviewStatus(fetchFn, signal);
        if (!actionInFlightRef.current) setState(st);
        return st;
      } catch {
        // A transport failure — including an abort of the in-flight poll when
        // the surface closes — leaves the last known state; the next poll
        // retries.
        return stateRef.current;
      } finally {
        pollInFlightRef.current = false;
      }
    },
    [fetchFn],
  );

  // Poll while the surface is active: once immediately on a fresh open (or
  // reopen), then on an interval — fast while `starting` (readiness settles
  // in the background after a start), a slow heartbeat otherwise (to catch a
  // running dev server dying or being stopped). A cadence switch only
  // re-arms the interval (no immediate re-poll: one fired right after an
  // action's state adoption could race the action's own completion and
  // clobber the adopted state with a stale read). The effect owns an
  // AbortController for its in-flight poll: on cleanup (the surface closes,
  // the component unmounts, or the cadence switches) the pending poll is
  // aborted and the in-flight flag reset, so a hung status request never
  // outlives the poll loop. Cleared when the surface goes inactive, so a
  // closed panel never polls.
  useEffect(() => {
    if (!enabled) {
      prevEnabledRef.current = false;
      return;
    }
    const justEnabled = !prevEnabledRef.current;
    prevEnabledRef.current = true;
    const controller = new AbortController();
    if (justEnabled) {
      void pollOnce(controller.signal);
    }
    const cadence = state.status === 'starting' ? STARTING_POLL_MS : HEARTBEAT_POLL_MS;
    const timer = setInterval(() => {
      void pollOnce(controller.signal);
    }, cadence);
    return () => {
      clearInterval(timer);
      controller.abort();
      // The in-flight poll (if any) is discarded; its finally would reset the
      // flag too, but the next effect body must not be blocked by it.
      pollInFlightRef.current = false;
    };
  }, [enabled, state.status, pollOnce]);

  // Bump the reload token when workspace files change (the agent's file tools,
  // shell commands, saves), debounced so a burst reloads once. The pane
  // re-mounts its iframe on a new key. Tracked only while active (a closed
  // panel has no iframe to reload).
  useEffect(() => {
    if (!enabled) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const onEvent = (e: Event) => {
      if (!changesWorkspaceFiles((e as CustomEvent<SproutEvent>).detail)) return;
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        setReloadKey((k) => k + 1);
      }, RELOAD_DEBOUNCE_MS);
    };
    window.addEventListener('sprout:wsevent', onEvent);
    return () => {
      window.removeEventListener('sprout:wsevent', onEvent);
      if (timer !== null) clearTimeout(timer);
    };
  }, [enabled]);

  const runAction = useCallback(async (action: () => Promise<PreviewState>): Promise<void> => {
    actionInFlightRef.current = true;
    try {
      const st = await action();
      setState(st);
      setActionError(undefined);
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      actionInFlightRef.current = false;
    }
  }, []);

  const start = useCallback(() => {
    void runAction(() => startPreview(fetchFn));
  }, [runAction, fetchFn]);

  const restart = useCallback(() => {
    void runAction(() => restartPreview(fetchFn));
  }, [runAction, fetchFn]);

  const stop = useCallback(() => {
    void runAction(() => stopPreview(fetchFn));
  }, [runAction, fetchFn]);

  return {
    status: state.status,
    url: state.url,
    error: state.error,
    detected: state.detected,
    hosted: state.hosted,
    actionError,
    reloadKey,
    start,
    restart,
    stop,
  };
}
