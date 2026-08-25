/**
 * backendHealth.ts — Health check polling service for backend reachability.
 *
 * Polls GET /health to detect backend availability.
 * Used when adapter requires health checks to provide graceful degradation when backend is unreachable.
 */

import { debugLog } from '../utils/log';
import { isCloud } from '../config/mode';
import { getAdapter } from './apiAdapter';
import { clientFetch } from './clientSession';

const DEFAULT_INTERVAL_MS = 5000; /* Poll every 5 seconds */
const HEALTH_ENDPOINT = '/health';

let pollTimer: NodeJS.Timeout | null = null;
let isReachable: boolean = false;
let isPolling: boolean = false;
let callbacks: Array<(isReachable: boolean) => void> = [];

/* Perform a single health check against the backend */
async function checkBackendHealth(): Promise<boolean> {
  try {
    /* In cloud mode, use a plain fetch (the adapter handles routing).
       In local mode, use clientFetch (handles proxy base + credentials). */
    const fetchFn = isCloud
      ? (input: RequestInfo | URL, init?: RequestInit) => {
          const adapter = getAdapter();
          if (adapter) return adapter.fetch(input, init);
          return fetch(input, init);
        }
      : clientFetch;
    const response = await fetchFn(HEALTH_ENDPOINT);

    /* Consider backend reachable if it responds with any 2xx status */
    const isOk = response.ok;

    /* Some backends may return 404 for /health but are otherwise functional.
       Only treat 404 as reachable if the response is JSON (indicating it's
       from our backend, not a static server's HTML 404 page). */
    if (!isOk && response.status === 404) {
      const contentType = response.headers.get('content-type') || '';
      if (!contentType.includes('application/json')) {
        debugLog('[backendHealth] Health check returned 404 with non-JSON content-type:', contentType);
        return false;
      }
      debugLog('[backendHealth] Health check returned 404 with JSON content-type, treating as reachable');
      return true;
    }

    if (!isOk) {
      debugLog('[backendHealth] Health check failed:', response.status, response.statusText);
    }

    return isOk;
  } catch (error) {
    /* Network errors (fetch failed) indicate backend is unreachable */
    debugLog('[backendHealth] Health check error:', error);
    return false;
  }
}

/* Notify all registered callbacks of reachability change */
function notifyReachabilityChange(newReachable: boolean): void {
  if (isReachable === newReachable) {
    return;
  }

  isReachable = newReachable;
  callbacks.forEach((callback) => {
    try {
      callback(newReachable);
    } catch (err) {
      debugLog('[backendHealth] Callback error:', err);
    }
  });
}

/* Start polling backend health */
export function startHealthPolling(
  config: { intervalMs?: number; onReachabilityChange?: (isReachable: boolean) => void } = {},
): void {
  if (isPolling) {
    /* Already polling — just register the additional callback */
    if (config.onReachabilityChange) {
      callbacks.push(config.onReachabilityChange);
    }
    return;
  }

  const intervalMs = config.intervalMs ?? DEFAULT_INTERVAL_MS;

  if (config.onReachabilityChange) {
    callbacks.push(config.onReachabilityChange);
  }

  isPolling = true;
  debugLog('[backendHealth] Starting health polling with interval:', intervalMs, 'ms');

  /* Perform initial check immediately */
  checkBackendHealth().then((reachable) => {
    notifyReachabilityChange(reachable);
  });

  /* Set up periodic polling */
  pollTimer = setInterval(() => {
    checkBackendHealth().then((reachable) => {
      notifyReachabilityChange(reachable);
    });
  }, intervalMs);
}

/* Stop polling backend health and reset all state */
export function stopHealthPolling(): void {
  if (!isPolling) {
    return;
  }

  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }

  isPolling = false;
  isReachable = false; /* Reset so next startHealthPolling starts fresh */
  callbacks = [];
  debugLog('[backendHealth] Stopped health polling');
}

/* Get current reachability status */
export function getIsReachable(): boolean {
  return isReachable;
}

/* Register a callback for reachability changes */
export function onReachabilityChange(callback: (isReachable: boolean) => void): () => void {
  callbacks.push(callback);

  /* Return cleanup function */
  return () => {
    callbacks = callbacks.filter((cb) => cb !== callback);
  };
}

/* Manually trigger a health check (useful for immediate status update) */
export async function triggerHealthCheck(): Promise<boolean> {
  const reachable = await checkBackendHealth();
  notifyReachabilityChange(reachable);
  return reachable;
}
