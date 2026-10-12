import type { WsEvent } from '@sprout/events';
import type { Message, ToolExecution } from '@sprout/ui';
import { useCallback, useEffect, useMemo, useRef } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { emitAutomate, type AutomateEventType, type AutomateEventPayload } from '../services/automateEvents';
import { fetchChatSessionMessagesForActiveStore } from '../services/hostChatSessions';
import { getServerErrorCode } from '../services/errorCodes';
import { appendTurnToHostStore } from '../services/hostChatSessions';
import { NATIVE_CHAT_ENABLED } from '../services/nativeChatStubs/nativeChatFlag';
import { fetchChatRunning } from '../services/runStatus';
import { WebSocketService } from '../services/websocket';
import { isRunEndingError, onChatReplay, PENDING_EVENTS_CAP } from '../utils/chatReplay';
import { noteRunStarted, runEpoch } from '../utils/runEpoch';
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
  handleWorkspaceStopping,
} from './wsHandlers/chat';
import {
  handleCompactCompleted,
  handleCompactStarted,
  handleContextManagementDiagnostic,
  handleMetricsUpdate,
  handleProviderNoCredential,
  handleRateLimited,
  handleWorkspaceChanged,
  handleWorkspacePatch,
} from './wsHandlers/session';
import {
  handleQueryCompleted,
  handleLanguageGuardReplacement,
  handleSteerDelivered,
  handleQueryProgress,
  handleQueryStarted,
  handleStreamChunk,
  makeStreamFlusher,
  type PendingStreamChunks,
} from './wsHandlers/streaming';
import { handleSubagentActivity, handleToolEnd, handleToolStart } from './wsHandlers/tools';
import { chatTranscriptToMessages } from '../utils/chatTranscript';

/** Reconnect run-status lookups: attempts and the first retry delay (doubling). */
const RECONNECT_STATUS_ATTEMPTS = 5;
const RECONNECT_STATUS_BASE_DELAY_MS = 500;

// ── Hook Interface ───────────────────────────────────────────────────────

export interface UseWebSocketEventHandlerRefs {
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  pendingProviderRef: React.MutableRefObject<string>;
  pendingProviderChangeRef: React.MutableRefObject<boolean>;
  pendingProviderChangeValueRef: React.MutableRefObject<string | null>;
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
  lastConnectionStateRef: React.MutableRefObject<boolean>;
  /** Mirror of the store's isProcessing, read when the socket drops and reconnects. */
  isProcessingRef?: React.MutableRefObject<boolean>;
}

export interface UseWebSocketEventHandlerParams {
  setState: AppStoreSetState;
  refs: UseWebSocketEventHandlerRefs;
  apiService: { getStats: () => Promise<unknown> };
  /**
   * The fetch the reconnect message-reload goes through. Omitted, the app's
   * adapter-aware `clientFetch`; an embedding supplies its own so the reload
   * reaches its backend.
   */
  fetchFn?: typeof fetch;
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
  fetchFn,
}: UseWebSocketEventHandlerParams): UseWebSocketEventHandlerReturn {
  const {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
    isProcessingRef,
  } = refs;

  // Whether the active chat was running when the socket dropped; read by the
  // reconnect sync to tell "finished while we were away" from "never ran".
  const processingAtDisconnectRef = useRef<boolean | null>(null);
  const reconnectGenerationRef = useRef(0);
  const isProcessingNow = useCallback(
    () => isProcessingRef?.current ?? activeRequestsRef.current > 0,
    [isProcessingRef, activeRequestsRef],
  );

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

      // Every primary run start bumps its chat's run epoch, so an in-flight
      // status snapshot taken before it cannot end the new run.
      const eventDepth = Number(eventData.subagent_depth ?? 0);
      const isPrimaryEvent = !(Number.isFinite(eventDepth) && eventDepth > 0);
      if (event.type === 'query_started' && isPrimaryEvent) {
        noteRunStarted(eventData.chat_id != null ? String(eventData.chat_id) : activeChatIdRef.current);
      }
      if (
        event.type === 'connection_status' &&
        eventData.connected === false &&
        processingAtDisconnectRef.current === null
      ) {
        processingAtDisconnectRef.current = isProcessingNow();
      }

      // Append finished turns to the host's chat session store when the host
      // serves one (`capabilities.chatSessions`). This runs for both agent
      // backends and for background chats alike: it sits above the per-chat
      // filter below, so a chat answering off screen still records its turn.
      // Subagent runs are excluded (they are not user turns), and /clear is a
      // reset, not a turn. A no-op without the capability.
      if (event.type === 'query_started' || event.type === 'query_completed') {
        const appendChatId = eventData.chat_id != null ? String(eventData.chat_id) : activeChatIdRef.current;
        const subDepth = Number((eventData as Record<string, unknown>).subagent_depth ?? 0);
        const isSubagent = Number.isFinite(subDepth) && subDepth > 0;
        const rawQuery = String(eventData.query ?? '');
        const isClear = rawQuery.trim().toLowerCase() === '/clear';
        if (!isSubagent && !isClear && rawQuery.trim()) {
          const response = event.type === 'query_completed' ? eventData.response : undefined;
          appendTurnToHostStore(appendChatId, rawQuery, typeof response === 'string' ? response : undefined);
        }
      }

      // Another chat's metrics belong to that chat's cache, not the chat on
      // screen (its tokens and cost were showing under the active chat).
      if (
        event.type === 'metrics_update' &&
        eventData.chat_id &&
        activeChatIdRef.current &&
        String(eventData.chat_id) !== activeChatIdRef.current
      ) {
        const metricsChatId = String(eventData.chat_id);
        setState((prev) => {
          const cached = prev.perChatCache[metricsChatId];
          if (!cached) return {};
          return {
            perChatCache: {
              ...prev.perChatCache,
              [metricsChatId]: { ...cached, stats: { ...(cached.stats ?? {}), ...eventData } },
            },
          };
        });
        return;
      }

      const perChatEvents = new Set([
        'query_started',
        'stream_chunk',
        'query_completed',
        'query_progress',
        'steer_delivered',
        'tool_start',
        'tool_end',
        'todo_update',
        'subagent_activity',
        'agent_message',
        'error',
        'language_guard_replacement',
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
        const bgSubagentDepth = Number(eventData.subagent_depth ?? 0);
        const endsBackgroundRun =
          event.type === 'error'
            ? isRunEndingError(eventData, true)
            : event.type === 'query_completed' && !(Number.isFinite(bgSubagentDepth) && bgSubagentDepth > 0);
        setState((prev) => {
          // The chat holding the workspace finished: release the chat whose
          // send was held back behind it (its queue drains next).
          const releaseBusy =
            endsBackgroundRun && prev.workspaceBusy?.runningChatId === eventChatId ? { workspaceBusy: null } : {};
          const existingCache = prev.perChatCache[eventChatId];
          if (!existingCache) return releaseBusy;
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
            ...releaseBusy,
            perChatCache: {
              ...prev.perChatCache,
              [eventChatId]: {
                ...existingCache,
                pendingEvents: [...pendingEvents, event].slice(-PENDING_EVENTS_CAP),
                pendingEventsTruncated:
                  existingCache.pendingEventsTruncated || pendingEvents.length >= PENDING_EVENTS_CAP || undefined,
                lastError: cachedLastError,
              },
            },
          };
        });
        return;
      }

      debugLog('[msg] Received event:', event.type, eventData);

      // Safety net: the active chat's primary run is visibly producing output
      // while the UI shows it idle (a missed or late query_started, a stale
      // snapshot). Its own output is proof it is running.
      if (
        isPrimaryEvent &&
        (event.type === 'stream_chunk' || event.type === 'tool_start' || event.type === 'query_progress') &&
        eventData.chat_id != null &&
        String(eventData.chat_id) === activeChatIdRef.current &&
        !isProcessingNow()
      ) {
        if (activeRequestsRef.current === 0) activeRequestsRef.current = 1;
        setState((prev) => (prev.isProcessing ? {} : { isProcessing: true }));
      }

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
        case 'steer_delivered':
          return handleSteerDelivered(ctx);
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
        case 'context_management_diagnostic':
          return handleContextManagementDiagnostic(ctx);
        case 'language_guard_replacement':
          return handleLanguageGuardReplacement(ctx);
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
        case 'session_displaced':
          return handleSessionDisplaced(ctx);
        case 'workspace_stopping':
          return handleWorkspaceStopping(ctx);
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
      isProcessingNow,
    ],
  );

  // Reconnect sync. The running flag comes from the per-chat query status
  // (or, for an in-page agent, the page's own run registry), retried with
  // backoff; an idle answer is dropped when a run started after it was
  // requested. Missed events themselves arrive through the reattach replay.
  const handleReconnect = useCallback(() => {
    debugLog('[reconnect] syncing state after websocket reconnect');
    const generation = ++reconnectGenerationRef.current;
    const chatId = activeChatIdRef.current;
    const wasProcessing = processingAtDisconnectRef.current ?? isProcessingNow();
    processingAtDisconnectRef.current = null;
    const epochAtRequest = runEpoch(chatId);
    setState(() => ({ lastError: null }));

    apiService
      .getStats()
      .then((stats: unknown) => {
        if (generation !== reconnectGenerationRef.current) return;
        const statsRecord = { ...(stats as Record<string, unknown>) };
        // The running flag is resolved per chat below; a client-wide stats
        // flag must not overwrite it.
        delete statsRecord.is_processing;
        setState((prev) => ({ stats: { ...prev.stats, ...statsRecord, connection_phase: 'reconnected' } }));
      })
      .catch((error: unknown) => debugLog('[reconnect] failed to refresh stats:', error));

    const applyRunning = (running: boolean) => {
      if (generation !== reconnectGenerationRef.current || activeChatIdRef.current !== chatId) return;
      if (running) {
        if (activeRequestsRef.current === 0) activeRequestsRef.current = 1;
        setState(() => ({ isConnected: true, isProcessing: true }));
        return;
      }
      if (runEpoch(chatId) !== epochAtRequest) {
        debugLog('[reconnect] idle status is older than a run that started since; keeping it running');
        return;
      }
      activeRequestsRef.current = 0;
      setState((prev) => ({
        isConnected: true,
        isProcessing: false,
        queryProgress: null,
        toolExecutions: prev.toolExecutions.map((tool: ToolExecution) =>
          tool.status === 'started' || tool.status === 'running'
            ? {
                ...tool,
                status: 'error' as const,
                endTime: tool.endTime || new Date(),
                result: 'Interrupted while connection was paused/reconnecting',
              }
            : tool,
        ),
      }));

      // The run ended while the socket was down. The reattach replay
      // normally delivers its tail; reload the transcript as well so the
      // answer shows even when the replay window had already closed.
      if (wasProcessing && chatId) {
        debugLog('[reconnect] query completed during disconnect — reloading messages for', chatId);
        // Read-only reload: a switchChatSession here would re-broadcast
        // session_changed("switch") and risk the same echo-reload loop the
        // session_changed handler avoids.
        fetchChatSessionMessagesForActiveStore(chatId, fetchFn)
          .then((response) => {
            if (activeChatIdRef.current !== chatId) return;
            const backendMessages: Message[] = chatTranscriptToMessages(chatId, response.chat_session.messages);
            setState((prev) =>
              backendMessages.length >= prev.messages.length || !response.chat_session.active_query
                ? { messages: trimMessages(backendMessages) }
                : {},
            );
          })
          .catch((err) => debugLog('[reconnect] failed to reload messages:', err));
      }
    };

    const attempt = (n: number) => {
      fetchChatRunning(chatId, fetchFn)
        .then(applyRunning)
        .catch((error: unknown) => {
          if (generation !== reconnectGenerationRef.current) return;
          if (n + 1 >= RECONNECT_STATUS_ATTEMPTS) {
            debugLog('[reconnect] giving up on run status after retries:', error);
            return;
          }
          const delay = RECONNECT_STATUS_BASE_DELAY_MS * 2 ** n;
          debugLog(`[reconnect] run status failed, retrying in ${delay}ms:`, error);
          setTimeout(() => attempt(n + 1), delay);
        });
    };
    attempt(0);
  }, [apiService, activeRequestsRef, activeChatIdRef, setState, fetchFn, isProcessingNow]);

  // Events a chat received in the background, replayed when it comes back on
  // screen: the cached state plus these is exactly its current state.
  useEffect(
    () =>
      onChatReplay((chatId, events, options) => {
        if (activeChatIdRef.current !== chatId) return;
        const socket = WebSocketService.getInstance();
        for (const event of events) {
          // A server snapshot overlaps the live stream; apply each event once.
          if (options.fromServer && !socket.markApplied(event, chatId)) continue;
          handleEvent(event);
        }
      }),
    [handleEvent, activeChatIdRef],
  );

  return { handleEvent, handleReconnect };
}
