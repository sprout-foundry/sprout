/**
 * Chat state persistence to localStorage.
 *
 * Persists the minimal set of app state needed to restore the chat view
 * after a page reload. Uses a tiered fallback strategy for quota errors
 * and deliberately excludes ephemeral data (logs, toolExecutions).
 */

import { useEffect } from 'react';
import type { AppState } from '../types/app';
import { getAppStateStorageKey } from '../services/appStatePersistence';

export function useChatPersistence(state: AppState): void {
  useEffect(() => {
    if (typeof window === 'undefined' || !window.localStorage) {
      return;
    }

    const storageKey = getAppStateStorageKey();
    // Only persist what is needed to restore the chat view. Logs and
    // toolExecutions are ephemeral — they are large and re-populated by
    // the WebSocket stream, so storing them wastes quota unnecessarily.
    const persistPayload = (messageCount: number) =>
      JSON.stringify({
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
          window.localStorage.removeItem(storageKey);
        } catch {
          /* nothing more we can do */
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
