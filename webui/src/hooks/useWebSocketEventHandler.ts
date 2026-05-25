import type {
  WsEvent,
  ConnectionStatusData,
  QueryStartedData,
  QueryProgressData,
  QueryCompletedData,
  StreamChunkData,
  ToolStartData,
  ToolEndData,
  SubagentActivityData,
  DelegateActivityData,
  AgentMessageData,
  TodoUpdateData,
  FileChangedData,
  ErrorData,
  MetricsUpdateData,
  WorkspaceChangedData,
  SecurityApprovalRequestData,
  SecurityPromptRequestData,
  AskUserRequestData,
  DriftDetectedData,
} from '@sprout/events';
import type { Message, ToolExecution, LogEntry, SubagentActivity, DelegateActivity, DelegateToolCallInfo } from '@sprout/ui';
import { useCallback } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { getWebUIClientId } from '../services/clientSession';
import { getServerErrorCode } from '../services/errorCodes';
import { toQueryProgress } from '../types/app';
import { ensureCompletedAssistantMessage } from '../utils/chatCompletion';
import { debugLog } from '../utils/log';
import { appendCappedLog } from '../utils/logCap';
import { generateMessageId } from '../utils/messageId';
import { trimMessages } from '../utils/messageWindow';

// ── Helper Functions ───────────────────────────────────────────────────

const getToolCallId = (details: unknown): string | undefined => {
  if (details && typeof details === 'object') {
    const d = details as Record<string, unknown>;
    return typeof (d.tool_call_id ?? d.id) === 'string' ? ((d.tool_call_id ?? d.id) as string) : undefined;
  }
  return undefined;
};

const AGENT_CHAT_LEAK_PATTERNS: RegExp[] = [
  /^\[\d+\s*-\s*\d+%\]\s*executing tool/i,
  /executing tool\s*\[[^\]]+\]/i,
  /\bTodoWrite\b/i,
  /\btodos=\d+/i,
  /\[\s*\]=\d+\s*\[~\]=\d+\s*\[x\]=\d+\s*\[-\]=\d+/i,
  /^Subagent:\s*\[\d+\s*-\s*\d+%\]/i,
];

const shouldSuppressAgentMessageInChat = (message: string): boolean => {
  const line = message.trim();
  if (!line) {
    return true;
  }
  return AGENT_CHAT_LEAK_PATTERNS.some((pattern) => pattern.test(line));
};

const extractToolNameFromToolLogTarget = (target: string): string | null => {
  if (!target) return null;
  const trimmed = target.trim();
  if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) return null;
  const inner = trimmed.slice(1, -1).trim();
  if (!inner) return null;
  const firstToken = inner.split(/\s+/, 1)[0] || '';
  return firstToken || null;
};

const TODO_STATUSES = new Set(['pending', 'in_progress', 'completed', 'cancelled']);

const normalizeTodoList = (
  rawTodos: unknown,
): Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }> => {
  if (!Array.isArray(rawTodos)) return [];
  const normalized: Array<{
    id: string;
    content: string;
    status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
  }> = [];
  const seen = new Set<string>();

  rawTodos.forEach((item, idx) => {
    if (!item || typeof item !== 'object') return;
    const t = item as Record<string, unknown>;
    const rawContent = typeof t.content === 'string' ? t.content.trim() : '';
    const rawStatus = typeof t.status === 'string' ? t.status.trim() : '';
    const rawID = typeof t.id === 'string' ? t.id.trim() : '';
    if (!rawContent || !TODO_STATUSES.has(rawStatus)) return;
    const status = rawStatus as 'pending' | 'in_progress' | 'completed' | 'cancelled';
    const id = rawID || `todo-${idx}-${rawStatus}-${rawContent.slice(0, 48)}`;
    const dedupeKey = `${id}::${status}::${rawContent}`;
    if (!seen.has(dedupeKey)) {
      seen.add(dedupeKey);
      normalized.push({ id, content: rawContent, status });
    }
  });

  return normalized;
};

// ── Event Handler Helpers ───────────────────────────────────────────────

interface EventHandlerContext {
  event: WsEvent;
  setState: AppStoreSetState;
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  apiService: { getStats: () => Promise<unknown> };
  pendingProviderRef: React.MutableRefObject<string>;
  pendingProviderChangeRef: React.MutableRefObject<boolean>;
  pendingProviderChangeValueRef: React.MutableRefObject<string | null>;
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
  lastConnectionStateRef: React.MutableRefObject<boolean>;
}

const createLogEntry = (event: WsEvent): LogEntry => ({
  id: `${Date.now()}-${Math.random()}`,
  type: event.type,
  timestamp: new Date(),
  data: event.data,
  level: 'info',
  category: 'system',
});

// Handle connection_status event
const handleConnectionStatus = (ctx: EventHandlerContext): void => {
  const { event, setState, connectionTimeoutRef, lastConnectionStateRef } = ctx;
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
    if (connectionTimeoutRef.current) clearTimeout(connectionTimeoutRef.current);
    connectionTimeoutRef.current = setTimeout(() => {
      lastConnectionStateRef.current = newConnectionState;
      setState((prev) => ({
        sessionId: incomingSessionId || prev.sessionId,
        isConnected: newConnectionState,
        stats: {
          ...prev.stats,
          connection_phase: phase,
          transport_session_id: incomingSessionId || prev.stats?.transport_session_id || prev.sessionId || '',
        },
        logs: appendCappedLog(prev.logs, logEntry),
      }));
    }, 300);
  }
  debugLog('[link] Connection status updated:', newConnectionState);
};

// Handle query_started event
const handleQueryStarted = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'query';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as QueryStartedData;
  const startedQuery = String(data.query || '');
  const isClearCommand = startedQuery.trim().toLowerCase() === '/clear';

  setState((prev) => {
    // Avoid duplicating the user message: handleSendMessage may have already
    // added it optimistically (e.g. for concurrent queries). Only add if the
    // last message is not already a user message with the same content.
    const lastMsg = prev.messages[prev.messages.length - 1];
    const alreadyPresent =
      lastMsg != null && lastMsg.type === 'user' && lastMsg.content === startedQuery;

    return {
      isProcessing: true,
      lastError: null,
      queryCount: prev.queryCount + 1,
      messages: isClearCommand
        ? prev.messages
        : alreadyPresent
          ? prev.messages
          : [...prev.messages, { id: generateMessageId(), type: 'user', content: startedQuery, timestamp: new Date() }],
      toolExecutions: [],
      fileEdits: [],
      subagentActivities: [],
  delegateActivities: [],
      queryProgress: null,
      currentTodos: [],
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[>>] Query started:', startedQuery);
};

// Handle query_progress event
const handleQueryProgress = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as QueryProgressData;
  setState((_prev) => ({ queryProgress: toQueryProgress(data as Record<string, unknown>) }));
  debugLog('[>>] Query progress:', data);
};

// Handle stream_chunk event
const handleStreamChunk = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'stream';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as StreamChunkData;
  const chunkContent = String(data.chunk || '');
  const chunkType = String(data.content_type || 'assistant_text');

  setState((prev) => {
    const newMessages = [...prev.messages];
    const lastMessage = newMessages[newMessages.length - 1];
    if (lastMessage && lastMessage.type === 'assistant') {
      if (chunkType === 'reasoning') {
        newMessages[newMessages.length - 1] = {
          ...lastMessage,
          reasoning: (lastMessage.reasoning || '') + chunkContent,
        };
      } else {
        newMessages[newMessages.length - 1] = { ...lastMessage, content: lastMessage.content + chunkContent };
      }
    } else {
      const newMsg: Message = {
        id: generateMessageId(),
        type: 'assistant',
        content: chunkType === 'reasoning' ? '' : chunkContent,
        timestamp: new Date(),
      };
      if (chunkType === 'reasoning') newMsg.reasoning = chunkContent;
      newMessages.push(newMsg);
    }
    return { messages: newMessages };
  });
};

// Handle query_completed event
const handleQueryCompleted = (ctx: EventHandlerContext): void => {
  const { event, setState, activeRequestsRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'query';
  logEntry.level = 'success';
  if (activeRequestsRef.current > 0) activeRequestsRef.current -= 1;
  const data = (event.data ?? {}) as QueryCompletedData;
  const completedQuery = String(data.query || '')
    .trim()
    .toLowerCase();
  const completedResponse = data.response;
  const wasClearCommand = completedQuery === '/clear';

  setState((prev) => {
    let nextMessages = wasClearCommand
      ? []
      : ensureCompletedAssistantMessage(prev.messages, completedResponse, (responseText) => ({
          id: generateMessageId(),
          type: 'assistant',
          content: responseText,
          timestamp: new Date(),
        }));

    if (!wasClearCommand && nextMessages.length > 0) {
      const lastMsg = nextMessages[nextMessages.length - 1] as Message;
      if (
        lastMsg.type === 'assistant' &&
        lastMsg.reasoning?.trim() &&
        lastMsg.content?.trim() &&
        lastMsg.content === lastMsg.reasoning
      ) {
        nextMessages = [...nextMessages.slice(0, -1), { ...lastMsg, reasoning: undefined }];
      }
    }

    if (!wasClearCommand) nextMessages = trimMessages(nextMessages);

    return {
      messages: nextMessages,
      currentTodos: wasClearCommand ? [] : prev.currentTodos,
      isProcessing: activeRequestsRef.current > 0,
      lastError: null,
      queryProgress: null,
      toolExecutions: wasClearCommand
        ? []
        : prev.toolExecutions.map((tool) => {
            if (tool.status === 'started' || tool.status === 'running') {
              return { ...tool, status: 'completed', endTime: tool.endTime || new Date() };
            }
            return tool;
          }),
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[OK] Query completed');
};

// Handle tool_start event
const handleToolStart = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as ToolStartData;
  const toolCallID = String(data.tool_call_id || '');
  const toolName = String(data.tool_name || 'unknown_tool');
  const rawArgs = data.arguments != null ? String(data.arguments) : undefined;
  const displayName = String(data.display_name || toolName);
  const persona = typeof data.persona === 'string' ? data.persona : undefined;
  const isSubagent = !!data.is_subagent;
  const subagentType: ToolExecution['subagentType'] =
    data.subagent_type === 'parallel' ? 'parallel' : isSubagent ? 'single' : undefined;
  const depth = Number((event.data as Record<string, unknown>)?.subagent_depth ?? 0);

  setState((prev) => {
    const messagesWithNewline = prev.messages.map((msg, idx) => {
      if (idx === prev.messages.length - 1 && msg.type === 'assistant' && msg.content && !msg.content.endsWith('\n')) {
        return { ...msg, content: msg.content + '\n' };
      }
      return msg;
    });

    const existingIdx = prev.toolExecutions.findIndex((t) => (getToolCallId(t.details) || t.id) === toolCallID);
    const addToolRefToMessage = (messages: Message[], toolId: string) => {
      for (let i = messages.length - 1; i >= 0; i -= 1) {
        const msg = messages[i];
        if (msg.type !== 'assistant') continue;
        const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
        if (!toolRefs.some((ref) => ref.toolId === toolId)) {
          toolRefs.push({ toolId, toolName, label: displayName, parallel: subagentType === 'parallel' || undefined });
          messages[i] = { ...msg, toolRefs };
          return;
        }
      }
    };

    if (existingIdx >= 0) {
      const updated = [...prev.toolExecutions];
      updated[existingIdx] = {
        ...updated[existingIdx],
        tool: toolName,
        status: 'started',
        startTime: updated[existingIdx].startTime,
        message: displayName,
        arguments: updated[existingIdx].arguments || rawArgs,
        details: event.data,
        persona: updated[existingIdx].persona || persona,
        subagentType: updated[existingIdx].subagentType || subagentType,
        depth: updated[existingIdx].depth ?? (depth > 0 ? depth : undefined),
      };
      const messages = [...messagesWithNewline];
      addToolRefToMessage(messages, updated[existingIdx].id);
      return { messages, toolExecutions: updated, logs: appendCappedLog(prev.logs, logEntry) };
    }

    const newTool: ToolExecution = {
      id: toolCallID || `${toolName}-${Date.now()}`,
      tool: toolName,
      status: 'started',
      message: displayName,
      startTime: new Date(),
      details: event.data,
      arguments: rawArgs,
      persona,
      subagentType,
      depth: depth > 0 ? depth : undefined,
    };
    const messages = [...messagesWithNewline];
    addToolRefToMessage(messages, newTool.id);
    return { messages, toolExecutions: [...prev.toolExecutions, newTool], logs: appendCappedLog(prev.logs, logEntry) };
  });
  debugLog('[tool] Tool start:', data.tool_name);
};

// Handle tool_end event
const handleToolEnd = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  const data = (event.data ?? {}) as ToolEndData;
  logEntry.level = data.status === 'failed' ? 'error' : 'info';
  const toolCallID = String(data.tool_call_id || '');
  const status: ToolExecution['status'] = data.status === 'failed' ? 'error' : 'completed';
  const result = data.result != null ? String(data.result) : undefined;
  const error = data.error != null ? String(data.error) : undefined;

  setState((prev) => {
    let matched = false;
    const updatedExecutions = prev.toolExecutions.map((t) => {
      const existingID = getToolCallId(t.details) || t.id;
      const match = toolCallID && existingID === toolCallID;
      if (!match) {
        const nameMatch = !toolCallID && t.tool === data.tool_name && !t.endTime;
        if (!nameMatch) return t;
      }
      matched = true;
      return {
        ...t,
        status,
        endTime: new Date(),
        result: t.result || result || error,
        details: event.data,
        arguments: t.arguments,
      };
    });

    if (!matched) {
      const fallbackExecution: ToolExecution = {
        id: toolCallID || `${data.tool_name || 'tool'}-${Date.now()}`,
        tool: String(data.tool_name || 'unknown_tool'),
        status,
        message: String(data.display_name || data.tool_name || 'Tool'),
        startTime: new Date(),
        endTime: new Date(),
        details: event.data,
        arguments: data.arguments != null ? String(data.arguments) : undefined,
        result: result || error,
      };
      return {
        toolExecutions: [...prev.toolExecutions, fallbackExecution],
        logs: appendCappedLog(prev.logs, logEntry),
      };
    }

    const messagesAfterTool = prev.messages.map((msg, idx) => {
      if (idx === prev.messages.length - 1 && msg.type === 'assistant' && msg.content && !msg.content.endsWith('\n')) {
        return { ...msg, content: msg.content + '\n' };
      }
      return msg;
    });
    return {
      messages: messagesAfterTool,
      toolExecutions: updatedExecutions,
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[tool] Tool end:', data.tool_name, data.status);
};

// Handle subagent_activity event
const handleSubagentActivity = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as SubagentActivityData;
  const activity: SubagentActivity = {
    id: String(event.id || `${Date.now()}-${Math.random()}`),
    toolCallId: String(data.tool_call_id || ''),
    toolName: String(data.tool_name || 'run_subagent'),
    phase:
      data.phase === 'spawn' || data.phase === 'complete' ? (data.phase as 'spawn' | 'complete' | 'output') : 'output',
    message: String(data.message || '').trim(),
    timestamp: new Date(),
    taskId: typeof data.task_id === 'string' ? data.task_id : undefined,
    persona: typeof data.persona === 'string' ? data.persona : undefined,
    isParallel: data.is_parallel === true,
    provider: typeof data.provider === 'string' ? data.provider : undefined,
    model: typeof data.model === 'string' ? data.model : undefined,
    taskCount: typeof data.task_count === 'number' ? data.task_count : undefined,
    failures: typeof data.failures === 'number' ? data.failures : undefined,
    status:
      typeof data.status === 'string'
        ? (data.status as 'queued' | 'started' | 'completed' | 'cancelled')
        : undefined,
    reason: typeof data.reason === 'string' ? data.reason : undefined,
    tokensUsed: typeof data.tokens_used === 'number' ? data.tokens_used : undefined,
    elapsedMs: typeof data.elapsed_ms === 'number' ? data.elapsed_ms : undefined,
  };

  if (!activity.message) {
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  } else {
    setState((prev) => ({
      subagentActivities: [...prev.subagentActivities, activity].slice(-500),
      logs: appendCappedLog(prev.logs, logEntry),
    }));
  }
};

// Handle delegate_activity event (SP-006-B)
function handleDelegateActivity(ctx: EventHandlerContext): void {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as DelegateActivityData;

  const toolsCalled: DelegateToolCallInfo[] = (data.tools_called ?? []).map((tc) => ({
    tool_name: tc.tool_name,
    input: tc.input,
    output: tc.output,
    timestamp: tc.timestamp,
    duration_ms: tc.duration_ms,
    success: tc.success,
  }));

  const status: DelegateActivity['status'] =
    data.action === 'completed' ? 'completed'
    : data.action === 'error' ? 'error'
    : 'running';

  setState((prev) => {
    const existing = prev.delegateActivities ?? [];
    const existingIdx = existing.findIndex(
      (d) => d.delegateId === data.delegate_id,
    );

    let updated: DelegateActivity[];
    if (existingIdx >= 0) {
      const current = existing[existingIdx];
      updated = [...existing];
      updated[existingIdx] = {
        ...current,
        action: data.action,
        summary: data.summary ?? current.summary,
        depth: data.depth ?? current.depth,
        // Backend sends cumulative totals only in "completed" events;
        // intermediate events omit these fields. Use ?? to preserve previous value.
        tokensUsed: data.tokens_used ?? current.tokensUsed,
        cost: data.cost ?? current.cost,
        toolsCalled: toolsCalled.length > 0
          ? (() => {
              const merged = [...current.toolsCalled];
              for (const tc of toolsCalled) {
                const dupIdx = merged.findIndex(
                  (e) => e.tool_name === tc.tool_name && e.timestamp === tc.timestamp,
                );
                if (dupIdx >= 0) {
                  merged[dupIdx] = { ...merged[dupIdx], ...tc };
                } else {
                  merged.push(tc);
                }
              }
              return merged;
            })()
          : current.toolsCalled,
        status: status === 'running' ? current.status : status,
      };
    } else {
      updated = [
        ...existing,
        {
          delegateId: data.delegate_id,
          action: data.action,
          summary: data.summary,
          depth: data.depth ?? 0,
          tokensUsed: data.tokens_used ?? 0,
          cost: data.cost ?? 0,
          toolsCalled,
          status,
        },
      ];
    }

    return { delegateActivities: updated.slice(-500) };
  });
}

// Handle agent_message event
const handleAgentMessage = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  const data = (event.data ?? {}) as AgentMessageData;
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
  } else if ((category === 'warning' || category === 'error') && !suppressInChat) {
    logEntry.category = 'system';
    logEntry.level = category === 'error' ? 'error' : 'warning';
    setState((prev) => {
      const newMessages = [...prev.messages];
      const lastMessage = newMessages[newMessages.length - 1];
      if (lastMessage && lastMessage.type === 'assistant') {
        const prefixedMsg = category === 'error' ? `\n\nWarning: ${cleanedMsg}` : `\n\nNote: ${cleanedMsg}`;
        newMessages[newMessages.length - 1] = { ...lastMessage, content: (lastMessage.content || '') + prefixedMsg };
      }
      return { messages: newMessages, logs: appendCappedLog(prev.logs, logEntry) };
    });
  } else if (category === 'info_rendered' && cleanedMsg && !suppressInChat) {
    logEntry.category = 'system';
    logEntry.level = 'info';
    setState((prev) => {
      const newMessages = [...prev.messages];
      const lastMessage = newMessages[newMessages.length - 1];
      if (lastMessage && lastMessage.type === 'assistant') {
        newMessages[newMessages.length - 1] = {
          ...lastMessage,
          content: (lastMessage.content || '') + `\n\nInfo: ${cleanedMsg}`,
        };
      }
      return { messages: newMessages, logs: appendCappedLog(prev.logs, logEntry) };
    });
  }
};

// Handle todo_update event
const handleTodoUpdate = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as TodoUpdateData;
  const normalizedTodos = normalizeTodoList(data.todos);
  setState((prev) => ({ currentTodos: normalizedTodos, logs: appendCappedLog(prev.logs, logEntry) }));
};

// Handle file_changed event
const handleFileChanged = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'file';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as FileChangedData;
  const newFileEdit = {
    path: String(data.path || data.file_path || 'Unknown'),
    action: String(data.action || data.operation || 'edited'),
    timestamp: new Date(),
    linesAdded: typeof data.lines_added === 'number' ? data.lines_added : undefined,
    linesDeleted: typeof data.lines_deleted === 'number' ? data.lines_deleted : undefined,
  };
  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
    fileEdits: [...prev.fileEdits, newFileEdit].slice(-50),
  }));
  debugLog('[edit] File changed:', data.path);
};

// Handle error event
const handleError = (ctx: EventHandlerContext): void => {
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
      lastError: errorMessage,
      messages: trimMessages([
        ...prev.messages,
        {
          id: generateMessageId(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMessage}`,
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
      lastError: errorMessage,
      messages: trimMessages([
        ...prev.messages,
        {
          id: generateMessageId(),
          type: 'assistant',
          content: `[FAIL] Error: ${errorMessage}`,
          timestamp: new Date(),
        },
      ]),
      logs: appendCappedLog(prev.logs, logEntry),
    }));
  }
  console.error('[FAIL] Error event:', data);
};

// Handle metrics_update event
const handleMetricsUpdate = (ctx: EventHandlerContext): void => {
  const { event, setState, pendingProviderChangeRef, pendingProviderChangeValueRef } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as MetricsUpdateData;

  if (pendingProviderChangeRef.current && data.provider === pendingProviderChangeValueRef.current) {
    pendingProviderChangeRef.current = false;
    pendingProviderChangeValueRef.current = null;
  }

  setState((prev) => ({
    provider: String(data.provider || prev.provider),
    model: String(data.model || prev.model),
    stats: { ...prev.stats, ...data },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
};

// Handle workspace_changed event
const handleWorkspaceChanged = (ctx: EventHandlerContext): void => {
  const { event } = ctx;
  const data = (event.data ?? {}) as WorkspaceChangedData;
  debugLog('[workspace] Workspace changed:', data);
  if (!data.client_id || String(data.client_id) === getWebUIClientId()) {
    window.location.reload();
  }
};

// Handle security_approval_request event
const handleSecurityApprovalRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as SecurityApprovalRequestData;
  if (data.status === 'responded') return;
  setState((prev) => ({
    securityApprovalRequest: {
      requestId: String(data.request_id || ''),
      toolName: String(data.tool_name || ''),
      riskLevel: String(data.risk_level || 'CAUTION'),
      reasoning: String(data.reasoning || ''),
      command: data.command != null ? String(data.command) : undefined,
      riskType: data.risk_type != null ? String(data.risk_type) : undefined,
      target: data.target != null ? String(data.target) : undefined,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[security] Approval request:', data.tool_name, data.risk_level);
};

// Handle security_prompt_request event
const handleSecurityPromptRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as SecurityPromptRequestData;
  if (data.status === 'responded') return;
  if (!data.prompt) return;
  setState((prev) => ({
    securityPromptRequest: {
      requestId: String(data.request_id || ''),
      prompt: String(data.prompt || ''),
      filePath: data.file_path != null ? String(data.file_path) : undefined,
      concern: data.concern != null ? String(data.concern) : undefined,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[security] Prompt request:', data.file_path, data.concern);
};

// Handle ask_user_request event
const handleAskUserRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as AskUserRequestData;
  if (data.status === 'responded') return;
  if (!data.question) return;
  setState((prev) => ({
    askUserRequest: {
      requestId: String(data.request_id || ''),
      question: String(data.question || ''),
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[ask_user] Question:', data.question);
};

/**
 * Handles drift_detected events: sets drift notification state so the
 * DriftNotification component can render a banner with action buttons.
 */
const handleDriftDetected = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const data = (event.data ?? {}) as DriftDetectedData;
  debugLog('[drift] Drift detected:', data);

  const similarity = data.similarity ?? 0;
  const threshold = data.threshold ?? 0;
  const sessionId = data.sessionId ?? '';
  const options = data.options ?? [];

  setState((prev) => ({
    driftNotification: { similarity, threshold, sessionId, options },
  }));
};

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

  const handleEvent = useCallback(
    (event: WsEvent) => {
      const filteredEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'];
      if (filteredEvents.includes(event.type)) return;

      const perChatEvents = new Set([
        'query_started',
        'stream_chunk',
        'query_completed',
        'query_progress',
        'tool_start',
        'tool_end',
        'todo_update',
        'subagent_activity',
        'delegate_activity',
        'agent_message',
        'error',
      ]);
      const eventData = (event.data ?? {}) as Record<string, unknown>;
      if (
        perChatEvents.has(event.type) &&
        eventData.chat_id &&
        activeChatIdRef.current &&
        String(eventData.chat_id) !== activeChatIdRef.current
      ) {
        return;
      }

      debugLog('[msg] Received event:', event.type, eventData);

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
          return handleStreamChunk(ctx);
        case 'query_completed':
          return handleQueryCompleted(ctx);
        case 'tool_start':
          return handleToolStart(ctx);
        case 'tool_end':
          return handleToolEnd(ctx);
        case 'subagent_activity':
          return handleSubagentActivity(ctx);
        case 'delegate_activity':
          return handleDelegateActivity(ctx);
        case 'agent_message':
          return handleAgentMessage(ctx);
        case 'todo_update':
          return handleTodoUpdate(ctx);
        case 'file_changed':
          return handleFileChanged(ctx);
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
        case 'ask_user_request':
          return handleAskUserRequest(ctx);
        case 'drift_detected':
          return handleDriftDetected(ctx);
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
    ],
  );

  const handleReconnect = useCallback(() => {
    debugLog('[reconnect] syncing state after websocket reconnect');
    apiService
      .getStats()
      .then((stats: unknown) => {
        const statsRecord = stats as Record<string, unknown>;
        const backendProcessing = statsRecord.is_processing === true;
        activeRequestsRef.current = backendProcessing ? 1 : 0;
        setState((prev) => {
          const nextToolExecutions = backendProcessing
            ? prev.toolExecutions
            : prev.toolExecutions.map((tool) => {
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
      })
      .catch((error: unknown) => {
        debugLog('[reconnect] failed to sync backend state:', error);
      });
  }, [apiService, activeRequestsRef, setState]);

  return { handleEvent, handleReconnect };
}
