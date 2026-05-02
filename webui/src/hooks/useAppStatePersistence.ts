/**
 * App state persistence hook.
 *
 * Saves application state to localStorage with debouncing and quota handling.
 */

import { useEffect } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import type { AppState } from '../types/app';
import { getAppStateStorageKey } from '../services/appStatePersistence';
import { debugLog } from '../utils/log';

export interface UseAppStatePersistenceOptions {
  state: AppState;
}

/**
 * Hook to persist application state to localStorage.
 * Only persists what is needed to restore the chat view (provider, model,
 * sessionId, queryCount, currentView, last 20 messages, last 20 fileEdits).
 * Logs and toolExecutions are ephemeral and not stored.
 */
export function useAppStatePersistence({ state }: UseAppStatePersistenceOptions): void {
  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    const storageKey = getAppStateStorageKey();
    const persistPayload = (messageCount: number) => JSON.stringify({
      provider: state.provider,
      model: state.model,
      sessionId: state.sessionId,
      queryCount: state.queryCount,
      currentView: state.currentView,
      messages: state.messages.slice(-messageCount),
      fileEdits: state.fileEdits.slice(-20),
    });

    try {
      window.localStorage.setItem(storageKey, persistPayload(20));
    } catch {
      // QuotaExceededError: retry with fewer messages, then give up gracefully.
      try {
        window.localStorage.setItem(storageKey, persistPayload(5));
      } catch {
        try {
          window.localStorage.setItem(storageKey, persistPayload(0));
        } catch {
          try {
            window.localStorage.removeItem(storageKey);
          } catch { /* nothing more we can do */ }
        }
      }
    }
  }, [
    state.provider,
    state.model,
    state.sessionId,
    state.queryCount,
    state.currentView,
    state.messages,
    state.fileEdits,
  ]);
}
