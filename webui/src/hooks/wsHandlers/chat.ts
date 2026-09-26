import type {
  AgentMessageData,
  ConnectionStatusData,
  ErrorData,
  FileChangedData,
  SessionChangedData,
  TodoUpdateData,
} from '@sprout/events';
import type { Message } from '@sprout/ui';
import { fetchChatSessionMessages, listChatSessions } from '../../services/chatSessions';
import { getWebUIClientId } from '../../services/clientSession';
import { getServerErrorCode } from '../../services/errorCodes';
import { debugLog } from '../../utils/log';
import { appendCappedLog } from '../../utils/logCap';
import { generateMessageId } from '../../utils/messageId';
import { trimMessages } from '../../utils/messageWindow';
import {
  createLogEntry,
  type EventHandlerContext,
  extractToolNameFromToolLogTarget,
  lastPrimaryAssistantIndex,
  normalizeTodoList,
  shouldSuppressAgentMessageInChat,
} from '../webSocketEventHelpers';

// Handle connection_status event
export const handleConnectionStatus = (ctx: EventHandlerContext): void => {
  const { event, setState, connectionTimeoutRef, lastConnectionStateRef, activeRequestsRef } = ctx;
  const logEntry = createLogEntry(event);
  const data = (event.data ?? {}) as ConnectionStatusData;
  if (data.client_id && String(data.client_id) !== getWebUIClientId()) return;
  logEntry.category = 'system';
  logEntry.level = data.connected === true ? 'success' : 'warning';
  const incomingSessionId = typeof data.session_id === 'string' ? data.session_id : null;
  const newConnectionState = data.connected === true;
  const phase = newConnectionState
    ? data.reconnected === true
      ? 'reconnected'
      : 'connected'
    : data.reconnecting === true
      ? 'reconnecting'
      : 'disconnected';

  if (newConnectionState !== lastConnectionStateRef.current) {
    // Update the ref immediately so subsequent events see the correct
    // connection state without waiting for the debounce timer.
    lastConnectionStateRef.current = newConnectionState;

    if (connectionTimeoutRef.current) clearTimeout(connectionTimeoutRef.current);

    // On disconnect, reset the active request counter immediately — any
    // in-flight turn's completion event will never arrive over a dead
    // socket. On reconnect, the backend replay (handleReconnect) will
    // set it correctly if the turn is still running server-side.
    if (!newConnectionState) {
      activeRequestsRef.current = 0;
    }

    setState((prev) => ({
      sessionId: incomingSessionId || prev.sessionId,
      isConnected: newConnectionState,
      isProcessing: newConnectionState ? prev.isProcessing : false,
      stats: {
        ...prev.stats,
        connection_phase: phase,
        transport_session_id: incomingSessionId || prev.stats?.transport_session_id || prev.sessionId || '',
      },
      logs: appendCappedLog(prev.logs, logEntry),
    }));
  }
  debugLog('[link] Connection status updated:', newConnectionState);
};

// Handle agent_message event
export const handleAgentMessage = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  const data = (event.data ?? {}) as AgentMessageData;

  // Subagent agent_message events (tool logs, warnings, etc.) should not
  // be appended to the primary chat's assistant message — they belong in
  // the SubagentActivityFeed. Only log them.
  const msgRaw = event.data as Record<string, unknown> | undefined;
  const msgDepth = Number(msgRaw?.subagent_depth ?? 0);
  if (Number.isFinite(msgDepth) && msgDepth > 0) {
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
    return;
  }

  let category = String(data.category || 'info');
  const message = String(data.message || '');
  const cleanedMsg = message.replace(new RegExp(String.fromCharCode(27) + '\\[[0-9;]*[mGKHJABCD]', 'g'), '').trim();
  const suppressInChat = shouldSuppressAgentMessageInChat(cleanedMsg);

  if (category === 'info') {
    if (/^\[FAIL\]|\[!!\]/.test(cleanedMsg)) category = 'error';
    else if (/^\[WARN\]|\[~\]|\[!\]/.test(cleanedMsg)) category = 'warning';
    else if (/^\[OK\]|\[edit\]|\[chart\]/.test(cleanedMsg)) category = 'info_rendered';
  }

  if (category === 'tool_log' && cleanedMsg) {
    logEntry.category = 'tool';
    const toolAction = String(data.action || 'tool');
    const toolTarget = String(data.target || '');
    const parsedToolName = extractToolNameFromToolLogTarget(toolTarget);

    setState((prev) => {
      if (/^executing tool$/i.test(toolAction) && parsedToolName) {
        const updated = [...prev.toolExecutions];
        for (let i = updated.length - 1; i >= 0; i--) {
          const row = updated[i];
          if (row.tool === parsedToolName && !row.endTime && row.status !== 'running') {
            updated[i] = { ...row, status: 'running' };
            return { toolExecutions: updated, logs: appendCappedLog(prev.logs, logEntry) };
          }
        }
      }
      return { logs: appendCappedLog(prev.logs, logEntry) };
    });
  } else if (category === 'provider_retry') {
    // Per-attempt provider failure notices from the retry loop
    // (agent.publishRetryEvent). Log-only: the retry is transient state —
    // bubbling it into chat would interleave "[FAIL]"-style text mid-turn,
    // and the turn's outcome (recovery or failure) is reported by
    // query_completed / the terminal error event.
    logEntry.category = 'system';
    logEntry.level = 'warning';
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  } else if ((category === 'warning' || category === 'error') && !suppressInChat) {
    logEntry.category = 'system';
    logEntry.level = category === 'error' ? 'error' : 'warning';
    setState((prev) => {
      const newMessages = [...prev.messages];
      const idx = lastPrimaryAssistantIndex(newMessages);
      if (idx >= 0) {
        const prefixedMsg = category === 'error' ? `\n\nWarning: ${cleanedMsg}` : `\n\nNote: ${cleanedMsg}`;
        newMessages[idx] = { ...newMessages[idx], content: (newMessages[idx].content || '') + prefixedMsg };
      }
      return { messages: newMessages, logs: appendCappedLog(prev.logs, logEntry) };
    });
  } else if (category === 'info_rendered' && cleanedMsg && !suppressInChat) {
    logEntry.category = 'system';
    logEntry.level = 'info';
    setState((prev) => {
      const newMessages = [...prev.messages];
      const idx = lastPrimaryAssistantIndex(newMessages);
      if (idx >= 0) {
        newMessages[idx] = {
          ...newMessages[idx],
          content: (newMessages[idx].content || '') + `\n\nInfo: ${cleanedMsg}`,
        };
      }
      return { messages: newMessages, logs: appendCappedLog(prev.logs, logEntry) };
    });
  }
};

// Handle todo_update event
export const handleTodoUpdate = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as TodoUpdateData;
  const normalizedTodos = normalizeTodoList(data.todos);
  setState((prev) => ({ currentTodos: normalizedTodos, logs: appendCappedLog(prev.logs, logEntry) }));
};

// Handle file_changed event
export const handleFileChanged = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'file';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as FileChangedData & { ts?: string };
  const path = String(data.path || data.file_path || 'Unknown');
  const baseFileEdit = {
    path,
    action: String(data.action || data.operation || 'edited'),
    timestamp: new Date(),
    // Server-side timestamp when present: ordering/revert floors must use
    // server time (browser clocks aren't comparable). Fallback keeps the
    // local clock for older servers.
    serverTs: data.ts,
    linesAdded: typeof data.lines_added === 'number' ? data.lines_added : undefined,
    linesDeleted: typeof data.lines_deleted === 'number' ? data.lines_deleted : undefined,
  };
  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
    // Turn correlation mirrors tool_start's queryId so the per-turn
    // change strip can group edits by the query that produced them.
    fileEdits: [...prev.fileEdits, { ...baseFileEdit, queryId: prev.queryCount }].slice(-50),
  }));
  debugLog('[edit] File changed:', path);
  // Bridge for window-level listeners: the Agent Changes panel listens
  // on this CustomEvent to flash the changed row and refresh its
  // manifest without prop-threading through every parent.
  window.dispatchEvent(new CustomEvent('agent-file-changed', { detail: { path } }));
};

// Handle agent_changes_reverted — the server publishes this after a
// successful revert so every open tab can refresh its Agent Changes
// panel (the tab that triggered the revert refreshes from its own API
// response; this bridge covers the OTHER tabs).
export const handleAgentChangesReverted = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'file';
  logEntry.level = 'info';
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  const data = (event.data ?? {}) as Record<string, unknown>;
  debugLog('[changes] Reverted:', data);
  window.dispatchEvent(new CustomEvent('agent-changes-reverted', { detail: { scope: data.scope } }));
};

// Handle error event
export const handleError = (ctx: EventHandlerContext): void => {
  const {
    event,
    setState,
    activeRequestsRef,
    apiService,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
  } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'error';
  if (activeRequestsRef.current > 0) activeRequestsRef.current -= 1;
  const data = (event.data ?? {}) as ErrorData;
  const errorMessage = String(data.message || 'Unknown error');
  // Error payloads pair a short label (message, e.g. "Query failed") with
  // the underlying cause (error, e.g. "dial tcp: connection refused").
  // Surfacing only the label leaves the user with "Error: Query failed"
  // and no way to tell a dead provider from a bad API key.
  const displayMessage = data.error ? `${errorMessage}: ${String(data.error)}` : errorMessage;
  const errorCode = getServerErrorCode(data);

  if (errorCode === 'model_not_available') {
    setState((prev) => ({
      isProcessing: activeRequestsRef.current > 0,
      queryProgress: null,
      modelSelectionRequest: { provider: prev.provider },
      logs: appendCappedLog(prev.logs, logEntry),
    }));
    debugLog('[model-not-available] Model not available, showing selection modal');
    return;
  }

  if (pendingProviderChangeRef.current) {
    pendingProviderChangeRef.current = false;
    pendingProviderChangeValueRef.current = null;
    setState((prev) => ({
      isProcessing: activeRequestsRef.current > 0,
      queryProgress: null,
      lastError: displayMessage,
      messages: trimMessages([
        ...prev.messages,
        {
          id: generateMessageId(),
          type: 'assistant',
          content: `[FAIL] Error: ${displayMessage}`,
          timestamp: new Date(),
        },
      ]),
      logs: appendCappedLog(prev.logs, logEntry),
    }));
    apiService
      .getStats()
      .then((stats: unknown) => {
        if (stats) {
          const statsRecord = stats as Record<string, unknown>;
          setState((prev) => ({
            provider: String(statsRecord.provider || prev.provider),
            model: String(statsRecord.model || prev.model),
          }));
        }
      })
      .catch((err: unknown) => {
        debugLog('[App] Failed to sync provider state after error:', {
          error: err instanceof Error ? err.message : String(err),
          stack: err instanceof Error ? err.stack : undefined,
          currentProvider: pendingProviderRef.current,
          isProviderChangePending: pendingProviderChangeRef.current,
        });
      });
  } else {
    setState((prev) => ({
      isProcessing: activeRequestsRef.current > 0,
      queryProgress: null,
      lastError: displayMessage,
      messages: trimMessages([
        ...prev.messages,
        {
          id: generateMessageId(),
          type: 'assistant',
          content: `[FAIL] Error: ${displayMessage}`,
          timestamp: new Date(),
        },
      ]),
      logs: appendCappedLog(prev.logs, logEntry),
    }));
  }
  console.error('[FAIL] Error event:', data);
};

// Handle session_changed (SP-034-3e). Rename/pin/unpin/switch mutations can
// come from another client (CLI, second tab); reconcile the local chat list
// so both views converge on the canonical server payload. A "switch" for
// the chat we're viewing means someone moved us — reload that chat's
// transcript; other changes only refresh the tab titles/metadata.
export const handleSessionChanged = (ctx: EventHandlerContext): void => {
  const { event, setState, activeChatIdRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as SessionChangedData;
  const summary = (data.summary ?? {}) as { id?: string };
  const chatId = typeof summary.id === 'string' ? summary.id : data.chat_id;

  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
  }));

  if (!chatId) return;
  debugLog('[session_changed]', data.change, chatId);

  // "clear" (New Session button via /api/command/execute) and "switch"
  // both mean "the transcript for the chat you're viewing changed
  // wholesale" — reload it. For clear the reload yields an empty
  // transcript, which is exactly the visible effect the user expects the
  // instant the button is pressed.
  const isTranscriptReset = data.change === 'switch' || data.change === 'clear';
  if (isTranscriptReset && activeChatIdRef.current && chatId === activeChatIdRef.current) {
    // Another client switched/cleared this chat's session — reload the
    // transcript. Use the read-only fetch, NOT switchChatSession: a back-end
    // switch would re-emit session_changed("switch") to every subscriber
    // (including this client), which would re-trigger this reload and loop
    // forever (blank-flash on every turn). We only need the new transcript,
    // not another side-effecting switch.
    fetchChatSessionMessages(chatId)
      .then((response) => {
        if (activeChatIdRef.current !== chatId) return;
        const backendMessages: Message[] = (response.chat_session.messages ?? [])
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => ({
            id: `chat-${chatId}-${i}`,
            type: m.role as 'user' | 'assistant',
            content: typeof m.content === 'string' ? m.content : '',
            timestamp: new Date(),
          }));
        setState((prev) => ({
          activeChatId: chatId,
          messages: backendMessages,
          // A cleared chat has no in-flight work.
          ...(data.change === 'clear'
            ? { isProcessing: false, toolExecutions: [], currentTodos: [], queryProgress: null }
            : {}),
        }));
      })
      .catch((err) => debugLog('[session_changed] switch reload failed:', err));
    return;
  }

  // rename/pin/unpin (and switch for non-active chats): refresh the session
  // list so titles and metadata stay canonical. Fire-and-forget.
  listChatSessions()
    .then((sessionsResp) => {
      setState((prev) => ({ chatSessions: sessionsResp.chat_sessions ?? prev.chatSessions }));
    })
    .catch((err) => debugLog('[session_changed] list refresh failed:', err));
};

// Handle the chat_run_restored control frame that leads a reattach replay.
// When the server sets gap=true it had already evicted events this client
// missed, so the partial replay that follows would splice onto a stale
// transcript (missing text, wrong content). Rather than corrupt the view, ask
// the chat manager to reload the active chat's authoritative history from the
// backend — subsequent live events then append cleanly. With gap=false the
// replay is complete, so this is a no-op and the following events apply as
// usual.
export const handleChatRunRestored = (ctx: EventHandlerContext): void => {
  const { event, activeChatIdRef } = ctx;
  const data = (event.data ?? {}) as { gap?: boolean; chat_id?: string };
  if (!data.gap) return;
  const chatId = data.chat_id ? String(data.chat_id) : activeChatIdRef.current || '';
  // Only force-reload the chat the user is actually viewing; others re-hydrate
  // from the backend when next selected.
  if (chatId && activeChatIdRef.current && chatId !== activeChatIdRef.current) return;
  debugLog('[reattach] gap on reconnect — reloading chat', chatId);
  window.dispatchEvent(new CustomEvent('sprout:chat-gap-reload', { detail: { chatId } }));
};

// Handle session_terminated event.
// The Go backend publishes this when a chat session crashes (panic recovery)
// or is force-closed. Without this handler the frontend keeps the "running"
// spinner / active-query state indefinitely since no query_completed or
// error event will ever follow — the session is dead. This handler resets
// the active request counter and surfaces an error banner so the user knows
// what happened and can send a new query.
export const handleSessionTerminated = (ctx: EventHandlerContext): void => {
  const { event, setState, activeRequestsRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'error';
  // The in-flight turn will never complete — the session is dead.
  if (activeRequestsRef.current > 0) activeRequestsRef.current = 0;
  const data = (event.data ?? {}) as { session_id?: string; status?: string; code?: string; message?: string };
  setState((prev) => ({
    isProcessing: false,
    lastError: data.message || 'Session terminated',
    queryProgress: null,
    // Mark in-flight tool executions as errored so they don't persist as
    // phantom "running" badges into the next query. Mirrors handleReconnect.
    toolExecutions: prev.toolExecutions.map((t) => {
      if (t.status === 'started' || t.status === 'running') {
        return { ...t, status: 'error' as const, endTime: new Date(), result: 'Session terminated' };
      }
      return t;
    }),
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[session] Session terminated:', data.code, data.message);
};

// Handle session_displaced event — another browser tab/connection has taken
// over this session. The WebSocket service already neutralized the old
// connection and stopped reconnecting (websocket.ts:326). Here we surface
// the takeover to the user so they understand why the UI went silent —
// without this, the user sees stale state with no spinner, no error, and
// no indication that their connection was stolen.
export const handleSessionDisplaced = (ctx: EventHandlerContext): void => {
  const { event, setState, activeRequestsRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as Record<string, unknown>;
  // Reset processing state — the displaced session can't receive
  // query_completed, so the spinner would hang forever.
  if (activeRequestsRef.current > 0) activeRequestsRef.current = 0;
  setState((prev) => ({
    isProcessing: false,
    queryProgress: null,
    lastError: String(data.message || 'This session was taken over by another tab or window.'),
    toolExecutions: prev.toolExecutions.map((t) => {
      if (t.status === 'started' || t.status === 'running') {
        return { ...t, status: 'error' as const, endTime: new Date(), result: 'Session displaced' };
      }
      return t;
    }),
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[session] Displaced by another connection:', data.message);
};
