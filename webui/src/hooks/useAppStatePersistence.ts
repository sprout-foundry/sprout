/**
 * App state persistence hook.
 *
 * Saves application state to localStorage with debouncing and quota handling.
 * Optimized to avoid writes during message streaming.
 */

import { useEffect, useRef } from 'react';
import type { AppState } from '../types/app';
import { getAppStateStorageKey } from '../services/appStatePersistence';

export interface UseAppStatePersistenceOptions {
  state: AppState;
}

/**
 * Hook to persist application state to localStorage.
 * Only persists what is needed to restore the chat view (provider, model,
 * sessionId, queryCount, currentView, last 20 messages, last 20 fileEdits).
 * Logs and toolExecutions are ephemeral and not stored.
 *
 * Performance optimization: Does NOT watch state.messages. During streaming,
 * messages change on every character which would cause excessive localStorage
 * writes. Instead:
 * - Metadata changes (provider, model, etc.) trigger immediate persistence
 * - The isProcessing → false transition triggers persistence of full state
 */
export function useAppStatePersistence({ state }: UseAppStatePersistenceOptions): void {
  // Track previous isProcessing state to detect transitions from true → false
  const prevIsProcessingRef = useRef<boolean>(state.isProcessing);

  const persistState = (messageCount: number) => {
    const storageKey = getAppStateStorageKey();
    const persistPayload = JSON.stringify({
      provider: state.provider,
      model: state.model,
      sessionId: state.sessionId,
      queryCount: state.queryCount,
      currentView: state.currentView,
      messages: state.messages.slice(-messageCount),
      fileEdits: state.fileEdits.slice(-20),
    });

    try {
      window.localStorage.setItem(storageKey, persistPayload);
    } catch {
      // QuotaExceededError: retry with fewer messages, then give up gracefully.
      try {
        window.localStorage.setItem(storageKey, JSON.stringify({
          provider: state.provider,
          model: state.model,
          sessionId: state.sessionId,
          queryCount: state.queryCount,
          currentView: state.currentView,
          messages: state.messages.slice(-5),
          fileEdits: state.fileEdits.slice(-20),
        }));
      } catch {
        try {
          window.localStorage.setItem(storageKey, JSON.stringify({
            provider: state.provider,
            model: state.model,
            sessionId: state.sessionId,
            queryCount: state.queryCount,
            currentView: state.currentView,
            messages: [],
            fileEdits: state.fileEdits.slice(-20),
          }));
        } catch {
          try {
            window.localStorage.removeItem(storageKey);
          } catch { /* nothing more we can do */ }
        }
      }
    }
  };

  // Effect 1: Metadata persistence
  // Persists immediately when provider, model, sessionId, queryCount, currentView, or fileEdits change.
  // Uses state.messages by reference (no dependency) to capture current messages without triggering writes on message changes.
  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    persistState(20);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- persistState reads state.messages by reference intentionally
  }, [
    state.provider,
    state.model,
    state.sessionId,
    state.queryCount,
    state.currentView,
    state.fileEdits,
  ]);

  // Effect 2: isProcessing transition persistence
  // Persists full state (including messages) when isProcessing transitions from true → false.
  // This captures the final message state after a query completes, avoiding excessive writes during streaming.
  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    const prevIsProcessing = prevIsProcessingRef.current;
    const currentIsProcessing = state.isProcessing;

    // Update the ref for next comparison
    prevIsProcessingRef.current = currentIsProcessing;

    // Only persist when transitioning from true to false (query just completed)
    if (prevIsProcessing === true && currentIsProcessing === false) {
      persistState(20);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- persistState reads state intentionally; prevIsProcessingRef is stable
  }, [state.isProcessing]);
}
