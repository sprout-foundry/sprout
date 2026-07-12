/**
 * Cloud session persistence hook.
 *
 * In cloud mode the conversation lives only in React state + the in-memory
 * WASM agent — refreshing the browser wipes it. This hook mirrors the
 * active conversation into the browser-local {@link cloudSessionStore} so
 * that:
 *
 *   • refresh preserves the conversation (it is reloaded on mount via the
 *     existing `/api/sessions` → restore flow in useAppInitialization),
 *   • the session picker lists previous conversations,
 *   • `/clear` rotates the current conversation into history before the
 *     new empty one takes over.
 *
 * The hook is a no-op outside cloud mode and when there are no messages.
 *
 * Save triggers (kept deliberately coarse to avoid thrashing localStorage
 * during streaming):
 *   1. When `isProcessing` transitions true → false (a query just finished).
 *   2. When `activeChatId` changes (user switched chats — flush the new one).
 *   3. On `beforeunload` / `pagehide` (tab close / reload / discard).
 */

import { useEffect, useRef } from 'react';
import { isCloud } from '../config/mode';
import { saveSession, deleteSession, resetActiveSessionId } from '../services/cloudSessionStore';
import { debugLog } from '../utils/log';
import type { AppState } from '../types/app';

export interface UseCloudSessionPersistenceOptions {
  state: AppState;
}

/**
 * Persist the current conversation to localStorage. Exported so callers
 * (e.g. a manual "save now" action) can trigger a flush outside the hook's
 * normal effect-driven cadence.
 */
export function persistCurrentCloudSession(state: AppState): string | null {
  // Only persist real user/assistant turns. Empty state (right after /clear)
  // is intentionally not saved as a session — clearing should rotate the
  // *previous* conversation into history, not persist a blank one.
  if (!state.messages || state.messages.length === 0) return null;
  return saveSession(state.messages, {
    totalTokens: state.queryCount,
  });
}

export function useCloudSessionPersistence({ state }: UseCloudSessionPersistenceOptions): void {
  const stateRef = useRef(state);
  stateRef.current = state;

  // Keep the last session id we persisted so `/clear` can rotate it into
  // history (preserve it) before a fresh empty session starts.
  const lastPersistedSessionIdRef = useRef<string | null>(null);

  // ── Save on query completion & chat switch ───────────────────────
  useEffect(() => {
    if (!isCloud) return;
    const messages = state.messages;
    if (!messages || messages.length === 0) return;

    // The store tracks the active session id (set on restore). Passing no
    // explicit id means "reuse the active one, or generate a new one".
    const id = saveSession(messages, {
      totalTokens: state.queryCount,
    });
    if (id) lastPersistedSessionIdRef.current = id;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- persist uses a snapshot of state; tracking message-array identity + activeChatId would be more precise but triggers during streaming
  }, [state.isProcessing, state.activeChatId]);

  // ── Rotate on /clear: when messages go non-empty → empty, the previous
  //    conversation has already been saved (by the effect above on the last
  //    query completion). Reset the store's active id so the next save
  //    creates a fresh session record while the cleared one stays in
  //    history. ─────────────────────────────────────────────────────────
  useEffect(() => {
    if (!isCloud) return;
    if (state.messages.length === 0) {
      resetActiveSessionId();
      lastPersistedSessionIdRef.current = null;
    }
  }, [state.messages.length]);

  // ── Save on page unload (reload / close / tab discard) ────────────
  useEffect(() => {
    if (!isCloud) return;
    if (typeof window === 'undefined') return;

    const flush = () => {
      const current = stateRef.current;
      try {
        const id = persistCurrentCloudSession(current);
        if (id) lastPersistedSessionIdRef.current = id;
      } catch (err) {
        debugLog('[cloudSession] beforeunload save failed:', err);
      }
    };

    // pagehide covers more cases than beforeunload on mobile / bfcache.
    window.addEventListener('beforeunload', flush);
    window.addEventListener('pagehide', flush);
    return () => {
      window.removeEventListener('beforeunload', flush);
      window.removeEventListener('pagehide', flush);
    };
  }, []);

  // ── Delete from store when a session is removed from chatSessions ──
  // This keeps localStorage in sync when a chat is deleted via the UI.
  const prevChatIdsRef = useRef<Set<string>>(new Set(state.chatSessions.map((s) => s.id)));
  useEffect(() => {
    if (!isCloud) return;
    const currentIds = new Set(state.chatSessions.map((s) => s.id));
    const prev = prevChatIdsRef.current;
    for (const id of prev) {
      if (!currentIds.has(id)) {
        deleteSession(id);
      }
    }
    prevChatIdsRef.current = currentIds;
  }, [state.chatSessions]);
}
