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
import { debugLog } from '../utils/log';

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
    } catch (err) {
      // QuotaExceededError: retry with fewer messages, then give up gracefully.
      debugLog('[useChatPersistence] failed to persist chat state (20 messages), retrying with fewer:', err);
      try {
        window.localStorage.setItem(storageKey, persistPayload(5));
      } catch (err2) {
        debugLog('[useChatPersistence] failed to persist chat state (5 messages), clearing storage:', err2);
        try {
          window.localStorage.removeItem(storageKey);
        } catch (err3) {
          /* nothing more we can do */
          debugLog('[useChatPersistence] failed to remove chat state from storage:', err3);
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
