/**
 * WebSocket events hook.
 *
 * Manages WebSocket event handling by maintaining refs and delegating
 * the actual event processing to useEventHandler. This hook keeps
 * track of connection state and provides the handleEvent callback
 * and handleReconnect recovery logic.
 */

import type { WsEvent } from '@sprout/events';
import type { Message } from '@sprout/ui';
import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { ApiService } from '../services/api';
import { switchChatSession, listChatSessions } from '../services/chatSessions';
import type { AppState } from '../types/app';
import { debugLog } from '../utils/log';
import { trimMessages } from '../utils/messageWindow';
import { WebSocketService } from '../services/websocket';
import { useEventHandler } from './useEventHandler';

export interface UseWebSocketEventsOptions {
  state: AppState;
  setState: AppStoreSetState;
  setQueuedMessages: Dispatch<SetStateAction<string[]>>;
  queuedMessagesRef: MutableRefObject<string[]>;
}

export interface UseWebSocketEventsReturn {
  handleEvent: (event: WsEvent) => void;
  activeChatIdRef: MutableRefObject<string | null>;
  activeRequestsRef: MutableRefObject<number>;
  /** Ref used by the main useEffect cleanup to clear a pending debounce timer */
  connectionTimeoutRef: MutableRefObject<NodeJS.Timeout | null>;
  /** Callback to register with WebSocketService.onReconnect() for stuck-processing recovery. */
  handleReconnect: () => void;
}

export default function useWebSocketEvents({
  state,
  setState,
  setQueuedMessages,
  queuedMessagesRef,
}: UseWebSocketEventsOptions): UseWebSocketEventsReturn {
  // ── Refs used by handleEvent ──────────────────────────────────────────
  const activeRequestsRef = useRef(0);
  const activeChatIdRef = useRef<string | null>(null);
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);

  // Keep the chat ID ref in sync with the derived state value (same pattern
  // as the original inline code — synchronous assignment, not in useEffect).
  activeChatIdRef.current = state.activeChatId;

  // ── Sync activeChatId with WebSocketService for reattach support ────────
  useLayoutEffect(() => {
    WebSocketService.getInstance().setActiveChatId(state.activeChatId);
  }, [state.activeChatId]);

  // ── Use the extracted event handler ────────────────────────────────────
  const { handleEvent } = useEventHandler({
    setState,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
    queuedMessagesRef,
    setQueuedMessages,
  });

  // ── Chat message recovery helper ───────────────────────────────────────
  // Fetches the latest messages from the server for the active chat and
  // merges them into state if the server has messages the frontend is
  // missing.  This recovers assistant messages that completed while the
  // WebSocket was disconnected (e.g. during tab freeze or network drop).
  const isRecoveringRef = useRef(false);

  const recoverChatMessages = useCallback(() => {
    if (isRecoveringRef.current) return;
    const chatId = activeChatIdRef.current;
    if (!chatId) return;

    isRecoveringRef.current = true;

    switchChatSession(chatId)
      .then((response) => {
        // Bail if the user switched to a different chat during the async fetch
        if (activeChatIdRef.current !== chatId) {
          debugLog('[reconnect] Chat ID changed during recovery — bailing out');
          return;
        }

        const backendMessages: Message[] = (response.chat_session.messages ?? [])
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => ({
            id: `chat-${chatId}-${i}`,
            type: m.role as 'user' | 'assistant',
            content: typeof m.content === 'string' ? m.content : '',
            timestamp: new Date(),
            ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
          }));

        setState((prev) => {
          // Only update if the server has more messages than the frontend
          if (backendMessages.length <= prev.messages.length) {
            return {};
          }
          debugLog(
            '[reconnect] Server has',
            backendMessages.length,
            'messages vs frontend',
            prev.messages.length,
            '— recovering missed messages',
          );
          return {
            messages: trimMessages(backendMessages),
          };
        });
      })
      .catch((err) => {
        debugLog('[reconnect] Failed to recover chat messages:', err);
      })
      .finally(() => {
        isRecoveringRef.current = false;
      });

    // Also refresh the chat sessions list
    listChatSessions()
      .then((response) => {
        setState((prev) => ({
          chatSessions: response.chat_sessions,
        }));
      })
      .catch((err) => {
        debugLog('[reconnect] Failed to refresh chat sessions:', err);
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- activeChatIdRef is a stable ref; setState is stable

  // ── Reconnect handler — recovers stuck processing state ──────────
  // When the WebSocket reconnects after a period of disconnection (tab
  // freeze, network drop, Chrome throttling, etc.), any query_completed
  // events that fired while we were offline are lost.  This handler asks
  // the backend for its actual processing state.  If the backend is idle
  // but the frontend still thinks a query is active, we reset the stuck
  // state so the user regains control of the UI.
  const handleReconnect = useCallback(() => {
    debugLog('[reconnect] Checking backend processing state for stuck-query recovery');
    ApiService.getInstance()
      .getStats()
      .then((stats) => {
        const backendProcessing = stats.is_processing === true;
        if (!backendProcessing && activeRequestsRef.current > 0) {
          debugLog('[reconnect] Backend idle but frontend had active request(s) — clearing stale state');
          activeRequestsRef.current = 0;
          setState((prev) => ({
            isProcessing: false,
            queryProgress: null,
            lastError: null,
            // Clear all tool executions — running tools either completed
            // while disconnected (results recovered via recoverChatMessages)
            // or were genuinely interrupted.  Stale "running" rows are
            // confusing either way.
            toolExecutions: [],
            subagentActivities: [],
            delegateActivities: [],
            currentTodos: [],
            // Clear stale security dialogs — if the backend is idle there
            // are no pending security requests; keeping them open would
            // result in unactionable dialogs once the backend timed them
            // out while we were disconnected.
            securityApprovalRequest: null,
            securityPromptRequest: null,
            askUserRequest: null,
          }));
        } else if (backendProcessing) {
          debugLog('[reconnect] Backend still processing but frontend is idle — restoring processing state');
          activeRequestsRef.current = 1;
          setState(() => ({
            isProcessing: true,
            lastError: null,
          }));
        } else {
          debugLog('[reconnect] Processing state is consistent — no recovery needed');
        }

        // After syncing processing state, recover any missed chat messages
        recoverChatMessages();
      })
      .catch((err) => {
        debugLog('[reconnect] Failed to fetch stats for recovery:', err);
      });
  }, [recoverChatMessages]); // eslint-disable-line react-hooks/exhaustive-deps -- activeRequestsRef is a stable ref; setState is stable

  return {
    handleEvent,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
    handleReconnect,
  };
}
