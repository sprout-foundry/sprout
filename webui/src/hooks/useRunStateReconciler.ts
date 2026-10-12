import { useEffect, useRef } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { fetchChatRunning } from '../services/runStatus';
import { debugLog } from '../utils/log';
import { runEpoch } from '../utils/runEpoch';

/** How often the active chat's running state is checked against the server. */
export const RUN_STATE_POLL_MS = 5000;

/** Consecutive disagreeing polls needed before the UI is corrected. */
export const RUN_STATE_MISMATCH_POLLS = 2;

export interface UseRunStateReconcilerParams {
  isProcessing: boolean;
  activeChatIdRef: React.MutableRefObject<string | null>;
  activeRequestsRef: React.MutableRefObject<number>;
  setState: AppStoreSetState;
  /** Status transport; omitted, the app's adapter-aware fetch. */
  fetchFn?: typeof fetch;
  /** Polling stays off while false (no backend connection). */
  enabled?: boolean;
}

/**
 * Safety net for the running indicator: every few seconds ask the server (or
 * the in-page agent) whether the active chat is running, and correct the UI
 * when it has disagreed for {@link RUN_STATE_MISMATCH_POLLS} polls in a row.
 * One disagreeing poll is normal — a send or a completion can be in flight —
 * so a single mismatch never flips anything. A run started between polls
 * resets the count.
 */
export function useRunStateReconciler({
  isProcessing,
  activeChatIdRef,
  activeRequestsRef,
  setState,
  fetchFn,
  enabled = true,
}: UseRunStateReconcilerParams): void {
  const isProcessingRef = useRef(isProcessing);
  isProcessingRef.current = isProcessing;

  useEffect(() => {
    if (!enabled) return;
    let mismatches = 0;
    let lastChat: string | null = null;
    let lastEpoch = -1;
    let inFlight = false;
    let cancelled = false;

    const poll = async () => {
      if (inFlight || (typeof document !== 'undefined' && document.visibilityState === 'hidden')) return;
      const chatId = activeChatIdRef.current;
      const epoch = runEpoch(chatId);
      if (chatId !== lastChat || epoch !== lastEpoch) {
        mismatches = 0;
        lastChat = chatId;
        lastEpoch = epoch;
      }
      inFlight = true;
      let running: boolean;
      try {
        running = await (fetchFn ? fetchChatRunning(chatId, fetchFn) : fetchChatRunning(chatId));
      } catch (err) {
        debugLog('[run-state] status poll failed:', err);
        return;
      } finally {
        inFlight = false;
      }
      if (cancelled || activeChatIdRef.current !== chatId || runEpoch(chatId) !== epoch) return;
      if (running === isProcessingRef.current) {
        mismatches = 0;
        return;
      }
      mismatches += 1;
      if (mismatches < RUN_STATE_MISMATCH_POLLS) return;
      mismatches = 0;
      debugLog('[run-state] correcting running indicator to', running, 'for chat', chatId);
      activeRequestsRef.current = running ? Math.max(activeRequestsRef.current, 1) : 0;
      setState(() => (running ? { isProcessing: true } : { isProcessing: false, queryProgress: null }));
    };

    const timer = setInterval(() => void poll(), RUN_STATE_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [enabled, activeChatIdRef, activeRequestsRef, setState, fetchFn]);
}
