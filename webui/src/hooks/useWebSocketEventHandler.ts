import type { WsEvent } from '@sprout/events';
import type { Message, ToolExecution } from '@sprout/ui';
import { useCallback, useEffect, useMemo, useRef } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { emitAutomate, type AutomateEventType, type AutomateEventPayload } from '../services/automateEvents';
import { fetchChatSessionMessages } from '../services/chatSessions';
import { getServerErrorCode } from '../services/errorCodes';
import { NATIVE_CHAT_ENABLED } from '../services/nativeChatStubs/nativeChatFlag';
import { debugLog } from '../utils/log';
import { appendCappedLog } from '../utils/logCap';
import { trimMessages } from '../utils/messageWindow';
import { createLogEntry, type EventHandlerContext } from './webSocketEventHelpers';
import {
  handleAskUserRequest,
  handleDelegateClarificationRequested,
  handleDelegateClarificationResponded,
  handleEditApprovalRequest,
  handleInputRequired,
  handlePasswordRequest,
  handleSecurityApprovalRequest,
  handleSecurityPromptRequest,
  handleShellApprovalRequest,
} from './wsHandlers/approvals';
import {
  handleAgentChangesReverted,
  handleAgentMessage,
  handleChatRunRestored,
  handleConnectionStatus,
  handleError,
  handleFileChanged,
  handleSessionChanged,
  handleSessionDisplaced,
  handleSessionTerminated,
  handleTodoUpdate,
} from './wsHandlers/chat';
import {
  handleCompactCompleted,
  handleCompactStarted,
  handleContextManagementDiagnostic,
  handleDriftDetected,
  handleMetricsUpdate,
  handleProviderNoCredential,
  handleRateLimited,
  handleRecallDiagnostic,
  handleWorkspaceChanged,
  handleWorkspacePatch,
} from './wsHandlers/session';
import {
  handleQueryCompleted,
  handleQueryProgress,
  handleQueryStarted,
  handleStreamChunk,
  makeStreamFlusher,
  type PendingStreamChunks,
} from './wsHandlers/streaming';
import { handleSubagentActivity, handleToolEnd, handleToolStart } from './wsHandlers/tools';

// ── Hook Interface ───────────────────────────────────────────────────────

export interface UseWebSocketEventHandlerRefs {
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  pendingProviderRef: React.MutableRefObject<string>;
  pendingProviderChangeRef: React.MutableRefObject<boolean>;
  pendingProviderChangeValueRef: React.MutableRefObject<string | null>;
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
  lastConnectionStateRef: React.MutableRefObject<boolean>;
}

export interface UseWebSocketEventHandlerParams {
  setState: AppStoreSetState;
  refs: UseWebSocketEventHandlerRefs;
  apiService: { getStats: () => Promise<unknown> };
}

export interface UseWebSocketEventHandlerReturn {
  handleEvent: (event: WsEvent) => void;
  handleReconnect: () => void;
}

/**
 * Hook to handle WebSocket events and reconnection state synchronization.
 * Returns event handler and reconnect callback functions.
 */
export function useWebSocketEventHandler({
  setState,
  refs,
  apiService,
}: UseWebSocketEventHandlerParams): UseWebSocketEventHandlerReturn {
  const {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
  } = refs;

  // Stream-chunk buffer (see makeStreamFlusher) — refs survive re-renders;
  // the flusher closes over them and over setState.
  const streamBufferRef = useRef<PendingStreamChunks | null>(null);
  const streamTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const streamFlusher = useMemo(
    () => makeStreamFlusher(setState, streamBufferRef, streamTimerRef, activeChatIdRef),
    [setState, activeChatIdRef],
  );

  // Pending buffer holds un-flushed chunk text only across the flush
  // interval; dropping it on unmount avoids a post-unmount setState from
  // the timer (React warns and the state write is lost anyway).
  useEffect(() => {
    return () => streamFlusher.discard();
  }, [streamFlusher]);

  const handleEvent = useCallback(
    (event: WsEvent) => {
      const filteredEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'];
      if (filteredEvents.includes(event.type)) return;

      const eventData = (event.data ?? {}) as Record<string, unknown>;

      // Automate workflow lifecycle → the automateEvents pub-sub bus.
      //
      // AutomationsPanel and AutomationsSessionDetail subscribe via
      // subscribeAutomate() and were relying on this dispatch. The only
      // caller that ever emitted here lived in useEventHandler.ts, which
      // was deleted as "dead code" in 1dfd28e22 — silently stranding both
      // panels, since 6d94e49ad had already removed their polling
      // fallback. They rendered whatever they fetched on tab switch and
      // never updated again.
      //
      // Routed ABOVE the native-chat short-circuit and the per-chat
      // filter on purpose:
      //  - automate events carry no chat_id/client_id (the server targets
      //    them by `automate` channel subscription — see
      //    shouldForwardEventToConnection), so the per-chat filter would
      //    never match them and they'd fall through to the unknown-event
      //    branch.
      //  - they are not chat streaming events, so the NATIVE_CHAT_ENABLED
      //    short-circuit doesn't apply; the Automations panel exists in
      //    --native-chat dists too and must still receive them.
      if (event.type.startsWith('automate.')) {
        emitAutomate(event.type as AutomateEventType, eventData as AutomateEventPayload);
        return;
      }

      // Compile-time short-circuit (R-4): in a --native-chat dist the shell
      // provides the chat loop natively, so the webui's chat-event streaming
      // entry is never wired up — chat events (query_started / stream_chunk /
      // query_completed / tool / agent / todo / error …) are not processed
      // into React state. Dead branch in the default build (flag off →
      // today's exact behavior, byte-identical).
      if (NATIVE_CHAT_ENABLED) {
        return;
      }

      const perChatEvents = new Set([
        'query_started',
        'stream_chunk',
        'query_completed',
        'query_progress',
        'tool_start',
        'tool_end',
        'todo_update',
        'subagent_activity',
        'agent_message',
        'error',
      ]);
      if (
        perChatEvents.has(event.type) &&
        eventData.chat_id &&
        activeChatIdRef.current &&
        String(eventData.chat_id) !== activeChatIdRef.current
      ) {
        // Queue the event for the non-active chat instead of dropping it.
        // When the user switches back, the backend fetch provides authoritative
        // state. But if that fetch fails or hasn't caught up, the pendingEvents
        // signal that the cache is stale. Pending events also prevent the
        // stale-cache heuristic (Fix 3) from preferring shorter local state.
        const eventChatId = String(eventData.chat_id);
        setState((prev) => {
          const existingCache = prev.perChatCache[eventChatId];
          if (!existingCache) return {};
          const pendingEvents = existingCache.pendingEvents ?? [];
          // Mirror the active-chat error lifecycle into the cached entry.
          // The active handlers clear lastError on primary run boundaries
          // (query_started/query_completed, subagent runs excluded) and set
          // it on error events — without mirroring, a background chat's
          // cached banner freezes at whatever it showed when the user last
          // viewed the chat: a recovered run keeps showing "chat failed"
          // forever, and inactive chat panes (WorkspacePane) render the
          // stale value directly. Ordering is safe: a failed run publishes
          // query_completed BEFORE its terminal error event, so the error
          // wins the race. (session_terminated/session_displaced are not
          // mirrored: the backend sends those directly to the affected
          // connection, never through the per-chat event routing.)
          let cachedLastError = existingCache.lastError ?? null;
          const subagentDepth = Number(eventData.subagent_depth ?? 0);
          const isSubagentEvent = Number.isFinite(subagentDepth) && subagentDepth > 0;
          if ((event.type === 'query_started' || event.type === 'query_completed') && !isSubagentEvent) {
            cachedLastError = null;
          } else if (event.type === 'error') {
            if (getServerErrorCode(eventData) !== 'model_not_available') {
              const msg = eventData.message != null ? String(eventData.message) : 'Unknown error';
              cachedLastError = eventData.error != null ? `${msg}: ${String(eventData.error)}` : msg;
            }
          }
          return {
            perChatCache: {
              ...prev.perChatCache,
              [eventChatId]: {
                ...existingCache,
                pendingEvents: [...pendingEvents, event].slice(-200),
                lastError: cachedLastError,
              },
            },
          };
        });
        return;
      }

      debugLog('[msg] Received event:', event.type, eventData);

      // Flush buffered stream chunks BEFORE any other event routes. The
      // completion heuristics in query_completed compare streamed text
      // against the server's final response — they only work if the
      // chunks have landed in state first.
      streamFlusher.flush();

      const ctx: EventHandlerContext = {
        event,
        setState,
        activeRequestsRef,
        activeChatIdRef,
        apiService,
        pendingProviderRef,
        pendingProviderChangeRef,
        pendingProviderChangeValueRef,
        connectionTimeoutRef,
        lastConnectionStateRef,
      };

      switch (event.type) {
        case 'connection_status':
          return handleConnectionStatus(ctx);
        case 'query_started':
          return handleQueryStarted(ctx);
        case 'query_progress':
          return handleQueryProgress(ctx);
        case 'stream_chunk':
          return handleStreamChunk(ctx, streamFlusher);
        case 'query_completed':
          return handleQueryCompleted(ctx);
        case 'tool_start':
          return handleToolStart(ctx);
        case 'tool_end':
          return handleToolEnd(ctx);
        case 'subagent_activity':
          return handleSubagentActivity(ctx);
        case 'agent_message':
          return handleAgentMessage(ctx);
        case 'todo_update':
          return handleTodoUpdate(ctx);
        case 'file_changed':
          return handleFileChanged(ctx);
        case 'agent_changes_reverted':
          return handleAgentChangesReverted(ctx);
        case 'error':
          return handleError(ctx);
        case 'metrics_update':
          return handleMetricsUpdate(ctx);
        case 'workspace_changed':
          return handleWorkspaceChanged(ctx);
        case 'security_approval_request':
          return handleSecurityApprovalRequest(ctx);
        case 'security_prompt_request':
          return handleSecurityPromptRequest(ctx);
        case 'password_request':
          return handlePasswordRequest(ctx);
        case 'provider_no_credential':
          return handleProviderNoCredential(ctx);
        case 'rate_limited':
          return handleRateLimited(ctx);
        case 'compact_started':
          return handleCompactStarted(ctx);
        case 'compact_completed':
          return handleCompactCompleted(ctx);
        case 'session_changed':
          return handleSessionChanged(ctx);
        case 'ask_user_request':
          return handleAskUserRequest(ctx);
        case 'edit_approval_request':
          return handleEditApprovalRequest(ctx);
        case 'shell_approval_request':
          return handleShellApprovalRequest(ctx);
        case 'input_required':
          return handleInputRequired(ctx);
        case 'drift_detected':
          return handleDriftDetected(ctx);
        case 'context_management_diagnostic':
          return handleContextManagementDiagnostic(ctx);
        case 'chat_run_restored':
          return handleChatRunRestored(ctx);
        case 'session_terminated':
          return handleSessionTerminated(ctx);
        case 'delegate_clarification_requested':
          return handleDelegateClarificationRequested(ctx);
        case 'delegate_clarification_responded':
          return handleDelegateClarificationResponded(ctx);
        case 'workspace_patch':
          return handleWorkspacePatch(ctx);
        case 'recall_diagnostic':
          return handleRecallDiagnostic(ctx);
        case 'session_displaced':
          return handleSessionDisplaced(ctx);
        default:
          const logEntry = createLogEntry(event);
          logEntry.level = 'warning';
          setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
          debugLog('[?] Unknown event type:', event.type, event.data);
      }
    },
    [
      activeChatIdRef,
      lastConnectionStateRef,
      connectionTimeoutRef,
      pendingProviderChangeRef,
      pendingProviderChangeValueRef,
      activeRequestsRef,
      setState,
      apiService,
      pendingProviderRef,
      streamFlusher,
    ],
  );

  const handleReconnect = useCallback(() => {
    debugLog('[reconnect] syncing state after websocket reconnect');
    setState((prev) => ({ ...prev, lastError: null }));
    apiService
      .getStats()
      .then((stats: unknown) => {
        const statsRecord = stats as Record<string, unknown>;
        const backendProcessing = statsRecord.is_processing === true;
        activeRequestsRef.current = backendProcessing ? 1 : 0;

        const wasProcessing = (() => {
          // Read the current isProcessing before setState overwrites it.
          let processing = false;
          setState((prev) => {
            processing = prev.isProcessing;
            return prev;
          });
          return processing;
        })();

        setState((prev) => {
          const nextToolExecutions = backendProcessing
            ? prev.toolExecutions
            : prev.toolExecutions.map((tool: ToolExecution) => {
                if (tool.status === 'started' || tool.status === 'running') {
                  return {
                    ...tool,
                    status: 'error' as const,
                    endTime: tool.endTime || new Date(),
                    result: 'Interrupted while connection was paused/reconnecting',
                  };
                }
                return tool;
              });
          return {
            isConnected: true,
            isProcessing: backendProcessing,
            queryProgress: backendProcessing ? prev.queryProgress : null,
            lastError: null,
            toolExecutions: nextToolExecutions,
            stats: { ...prev.stats, ...statsRecord, connection_phase: 'reconnected' },
          };
        });

        // If a query was processing when we disconnected but the backend
        // says it's done now, the query_completed event was lost during
        // the disconnect. Reload messages for the active chat to recover
        // the assistant's response.
        if (wasProcessing && !backendProcessing) {
          const chatId = activeChatIdRef.current;
          if (chatId) {
            debugLog('[reconnect] query completed during disconnect — reloading messages for', chatId);
            // Read-only reload: a switchChatSession here would re-broadcast
            // session_changed("switch") and risk the same echo-reload loop the
            // session_changed handler avoids.
            fetchChatSessionMessages(chatId)
              .then((response) => {
                // Bail if user switched chats while we were loading.
                if (activeChatIdRef.current !== chatId) return;
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
                  // Backend is authoritative when it has caught up or the
                  // query is no longer active. The old length-only heuristic
                  // preferred shorter local state when streaming hadn't
                  // persisted yet, losing the assistant's response.
                  const useBackendMessages = backendMessages.length >= prev.messages.length || !backendProcessing;
                  return useBackendMessages ? { messages: trimMessages(backendMessages) } : {};
                });
              })
              .catch((err) => {
                debugLog('[reconnect] failed to reload messages:', err);
              });
          }
        }
      })
      .catch((error: unknown) => {
        debugLog('[reconnect] failed to sync backend state:', error);
        // Defensive: the up-front clear above already covers the no-flash
        // case, but re-clear here so any error path that re-sets lastError
        // (or a stale closure) still ends with a clean banner.
        setState((prev) => ({ ...prev, lastError: null }));
      });
  }, [apiService, activeRequestsRef, setState]);

  return { handleEvent, handleReconnect };
}
