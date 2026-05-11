/**
 * useBackendReachable.ts — React hook for backend health status.
 *
 * Provides backend reachability state that updates via polling.
 * Only active when adapter requires health checks; in local mode always returns reachable.
 */

import { useState, useEffect, useCallback } from 'react';
import { requiresBackendHealthCheck } from '../services/apiAdapter';
import { startHealthPolling, stopHealthPolling, triggerHealthCheck } from '../services/backendHealth';

export interface BackendReachableState {
  isReachable: boolean;
  isChecking: boolean;
  lastChecked: Date | null;
  checkNow: () => Promise<void>;
}

/**
 * Hook to track backend reachability.
 *
 * When adapter requires health checks: polls /health endpoint every 5 seconds.
 * Otherwise (no adapter or adapter doesn't require checks): always returns isReachable=true (same process or direct connection).
 */
export function useBackendReachable(): BackendReachableState {
  /* If adapter requires health checks, start with isReachable=false to avoid a flash of "connected"
     before the first health check completes. Otherwise, always true. */
  const needsHealthCheck = requiresBackendHealthCheck();
  const [isReachable, setIsReachable] = useState(!needsHealthCheck);
  const [isChecking, setIsChecking] = useState(needsHealthCheck);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);

  // Adapter is installed once at startup and never changes, so needsHealthCheck is effectively constant.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    /* If no health check needed, backend is assumed reachable */
    if (!needsHealthCheck) {
      setIsReachable(true);
      setIsChecking(false);
      setLastChecked(new Date());
      return;
    }

    /* Start health polling when adapter requires it */
    startHealthPolling({
      intervalMs: 5000,
      onReachabilityChange: (reachable: boolean) => {
        setIsReachable(reachable);
        setIsChecking(false);
        setLastChecked(new Date());
      },
    });

    return () => {
      stopHealthPolling();
    };
  }, []);

  const checkNow = useCallback(async () => {
    setIsChecking(true);
    const reachable = await triggerHealthCheck();
    setIsReachable(reachable);
    setIsChecking(false);
    setLastChecked(new Date());
  }, []);

  return { isReachable, isChecking, lastChecked, checkNow };
}
