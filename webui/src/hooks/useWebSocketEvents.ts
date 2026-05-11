/**
 * WebSocket events hook.
 *
 * Manages WebSocket event handling by maintaining refs and delegating
 * the actual event processing to useEventHandler. This hook keeps
 * track of connection state and provides the handleEvent callback
 * and handleReconnect recovery logic.
 */

import { useCallback, useRef } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppState } from '../types/app';
import type { WsEvent } from '../services/websocket';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import { useEventHandler } from './useEventHandler';
import type { AppStoreSetState } from '../contexts/AppStore';

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
          debugLog(
            '[reconnect] Backend idle but frontend has',
            activeRequestsRef.current,
            'active request(s) — resetting stuck processing state',
          );
          activeRequestsRef.current = 0;
          setState((prev) => ({
            isProcessing: false,
            queryProgress: null,
            lastError: null,
            toolExecutions: prev.toolExecutions.map((tool) => {
              if (tool.status === 'started' || tool.status === 'running') {
                return {
                  ...tool,
                  status: 'error' as const,
                  endTime: tool.endTime || new Date(),
                  result: 'Interrupted — connection lost during execution',
                };
              }
              return tool;
            }),
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
      })
      .catch((err) => {
        debugLog('[reconnect] Failed to fetch stats for recovery:', err);
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- activeRequestsRef is a stable ref; setState is stable

  return {
    handleEvent,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
    handleReconnect,
  };
}
