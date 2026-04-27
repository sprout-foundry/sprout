/**
 * useBackendReachable.ts — React hook for backend health status.
 *
 * Provides backend reachability state that updates via polling.
 * Only active in cloud mode; in local mode always returns reachable.
 */

import { useState, useEffect, useCallback } from 'react';
import { isCloud } from '../config/mode';
import {
  startHealthPolling,
  stopHealthPolling,
  triggerHealthCheck,
} from '../services/backendHealth';

export interface BackendReachableState {
  isReachable: boolean;
  isChecking: boolean;
  lastChecked: Date | null;
  checkNow: () => Promise<void>;
}

/**
 * Hook to track backend reachability.
 *
 * In cloud mode: polls /health endpoint every 5 seconds.
 * In local mode: always returns isReachable=true (same process).
 */
export function useBackendReachable(): BackendReachableState {
  /* In cloud mode, start with isReachable=false to avoid a flash of "connected"
     before the first health check completes. In local mode, always true. */
  const [isReachable, setIsReachable] = useState(!isCloud);
  const [isChecking, setIsChecking] = useState(isCloud);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);

  useEffect(() => {
    /* In local mode, backend is assumed reachable (same process) */
    if (!isCloud) {
      setIsReachable(true);
      setIsChecking(false);
      setLastChecked(new Date());
      return;
    }

    /* In cloud mode, start health polling */
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
