import { useEffect, useRef } from 'react';
import type { Message } from '@sprout/ui';
import { fetchChatSessionMessages } from '../services/chatSessions';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { PerChatState } from '../types/app';
import { debugLog } from '../utils/log';
import { trimMessages } from '../utils/messageWindow';
import { chatTranscriptToMessages } from '../utils/chatTranscript';
import { endsRun } from '../utils/chatReplay';

/** Debounce for background refreshes. Streaming emits many events; one fetch
 * per burst is enough — the response is the whole transcript. */
const BACKGROUND_REFRESH_DEBOUNCE_MS = 600;

/**
 * Keeps background chat panes live.
 *
 * A chat buffer open in a NON-active pane renders from
 * perChatCache[chatId], which the WS handler refreshes by queueing events
 * into `pendingEvents` — content is never applied, so the pane froze at the
 * moment the user switched away. This hook watches for pending events on
 * chats with populated caches, debounces, and fetches the authoritative
 * transcript via the read-only /api/chat-sessions/messages endpoint (which,
 * unlike /switch, does not move the server-side active chat or workspace).
 *
 * The active chat is skipped: it streams through the normal event path, and
 * a background fetch here would fight the streaming reducer.
 */
export const useBackgroundChatSync = (params: {
  perChatCache: Record<string, PerChatState>;
  activeChatId: string | null;
  setState: AppStoreSetState;
}): void => {
  const { perChatCache, activeChatId, setState } = params;
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const inFlightRef = useRef<Set<string>>(new Set());
  // Latest refs so the timer callback sees fresh values without re-arming.
  const activeChatIdRef = useRef(activeChatId);
  activeChatIdRef.current = activeChatId;

  useEffect(() => {
    for (const [chatId, cache] of Object.entries(perChatCache)) {
      if (chatId === activeChatIdRef.current) continue;
      if (!cache.pendingEvents?.length) continue;
      // The stored transcript is written when a run ends; mid-run it lacks
      // the turn in progress, and adopting it blanked the pane (and, on
      // switch-back, the chat). Until then the pane keeps its last state and
      // the queued events wait for the switch to replay them.
      if (!endsRun(cache.pendingEvents)) continue;

      if (timersRef.current.has(chatId) || inFlightRef.current.has(chatId)) continue;

      const timer = setTimeout(() => {
        timersRef.current.delete(chatId);
        // Became active while the debounce ran — the normal streaming path
        // owns it now; a fetch here would fight the live reducer.
        if (activeChatIdRef.current === chatId) return;
        if (inFlightRef.current.has(chatId)) return;
        inFlightRef.current.add(chatId);
        // Snapshot the pending events that motivated this fetch; on
        // completion anything NOT in the snapshot (arrived mid-fetch) stays
        // pending and re-triggers the debounce.
        const seenEvents = new Set(perChatCache[chatId]?.pendingEvents ?? []);
        void (async () => {
          try {
            const response = await fetchChatSessionMessages(chatId);
            const fetched: Message[] = chatTranscriptToMessages(chatId, response.chat_session.messages);
            // Bail if the user switched to this chat while we fetched — the
            // switch path already installed authoritative state.
            if (activeChatIdRef.current === chatId) return;
            setState((prev) => {
              const existing = prev.perChatCache[chatId];
              if (!existing) return {};
              // The run-end save can land a moment after query_completed; a
              // copy with fewer messages than the pane is from before it.
              if (fetched.length < existing.messages.length) return {};
              const remaining = (existing.pendingEvents ?? []).filter((e) => !seenEvents.has(e));
              return {
                perChatCache: {
                  ...prev.perChatCache,
                  [chatId]: {
                    ...existing,
                    messages: trimMessages(fetched),
                    isProcessing: response.chat_session.active_query ?? false,
                    pendingEvents: remaining.length > 0 ? remaining : undefined,
                    pendingEventsTruncated: undefined,
                  },
                },
              };
            });
          } catch (err) {
            debugLog('[chat] background pane refresh failed:', chatId, err);
          } finally {
            inFlightRef.current.delete(chatId);
          }
        })();
      }, BACKGROUND_REFRESH_DEBOUNCE_MS);
      timersRef.current.set(chatId, timer);
    }
  }, [perChatCache, setState]);

  // Clear all pending timers on unmount.
  useEffect(() => {
    const timers = timersRef.current;
    return () => {
      timers.forEach((t) => clearTimeout(t));
      timers.clear();
    };
  }, []);
};
