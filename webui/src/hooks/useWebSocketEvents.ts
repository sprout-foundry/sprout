import { useCallback, useRef } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppState } from '../types/app';
import type { Message, ToolExecution, LogEntry, SubagentActivity } from '../types/app';
import type { WsEvent } from '../services/websocket';
import { ApiService } from '../services/api';
import { getWebUIClientId } from '../services/clientSession';
import { debugLog, error as logError } from '../utils/log';
import { useNotifications } from '../contexts/NotificationContext';
import { ensureCompletedAssistantMessage } from '../utils/chatCompletion';
import {
  shouldSuppressAgentMessageInChat,
  extractToolNameFromToolLogTarget,
  normalizeTodoList,
} from '../utils/agentMessages';

export interface UseWebSocketEventsOptions {
  state: AppState;
  setState: Dispatch<SetStateAction<AppState>>;
  setInputValue: Dispatch<SetStateAction<string>>;
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
  setInputValue: _setInputValue,
  setQueuedMessages,
  queuedMessagesRef,
}: UseWebSocketEventsOptions): UseWebSocketEventsReturn {
  const { addNotification } = useNotifications();

  // ── Refs used by handleEvent ──────────────────────────────────────────
  const activeRequestsRef = useRef(0);
  const activeChatIdRef = useRef<string | null>(null);
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef<boolean>(false);

  // Keep the chat ID ref in sync with the derived state value (same pattern
  // as the original inline code — synchronous assignment, not in useEffect).
  activeChatIdRef.current = state.activeChatId;

  // ── The monolithic WebSocket event handler ────────────────────────────
  const handleEvent = useCallback((event: WsEvent) => {
    // Type eventData as a loosely-typed record so property lookups compile
    // without `any`.  All consumers already use defensive access patterns
    // (typeof / ternary / ??) anyway.
    const eventData = (event.data ?? {}) as Record<string, unknown>;

    // Filter out ping events and webpack dev server events early to prevent console spam
    const filteredEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'];
    if (filteredEvents.includes(event.type)) {
      return; // Don't process these events
    }

    // Per-chat event filtering: only process message events for the active chat
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
      eventData?.chat_id &&
      activeChatIdRef.current &&
      eventData.chat_id !== activeChatIdRef.current
    ) {
      return; // event is for a different chat session
    }

    debugLog('[msg] Received event:', event.type, eventData);

    // Create log entry for all events
    const logEntry: LogEntry = {
      id: `${Date.now()}-${Math.random()}`,
      type: event.type,
      timestamp: new Date(),
      data: eventData,
      level: 'info',
      category: 'system',
    };

    // Determine log level and category based on event type
    switch (event.type) {
      case 'connection_status': {
        if (eventData?.client_id && eventData.client_id !== getWebUIClientId()) {
          break;
        }
        logEntry.category = 'system';
        logEntry.level = eventData.connected ? 'success' : 'warning';
        const incomingSessionId = typeof eventData?.session_id === 'string' ? eventData.session_id : null;

        // Debounce connection status updates to prevent rapid re-renders
        const newConnectionState = eventData.connected === true;

        // Only update if state actually changed
        if (newConnectionState !== lastConnectionStateRef.current) {
          // Clear any pending timeout
          if (connectionTimeoutRef.current) {
            clearTimeout(connectionTimeoutRef.current);
          }

          // Debounce the state update
          connectionTimeoutRef.current = setTimeout(() => {
            lastConnectionStateRef.current = newConnectionState;
            setState((prev) => ({
              ...prev,
              // NOTE:
              // WebSocket `session_id` is a transport connection id (ws_<timestamp>),
              // not a chat session id. It changes on reconnect and must never clear chat state.
              sessionId: prev.sessionId || incomingSessionId,
              isConnected: newConnectionState,
              logs: [...prev.logs, logEntry],
            }));
          }, 300); // Wait 300ms to confirm the connection state is stable
        }
        debugLog('[link] Connection status updated:', newConnectionState);
        break;
      }

      case 'query_started': {
        logEntry.category = 'query';
        logEntry.level = 'info';
        const startedQuery = String(eventData?.query || '');
        setState((prev) => ({
          ...prev,
          isProcessing: true,
          lastError: null,
          queryCount: prev.queryCount + 1,
          messages: [
            ...prev.messages,
            {
              id: Date.now().toString(),
              type: 'user',
              content: startedQuery,
              timestamp: new Date(),
            },
          ],
          toolExecutions: [], // Clear previous tool executions
          fileEdits: [], // Clear previous file edits for current-run status metrics
          subagentActivities: [],
          queryProgress: null, // Clear previous progress
          currentTodos: [], // Clear previous todos
          logs: [...prev.logs, logEntry],
        }));
        debugLog('[>>] Query started:', startedQuery);
        break;
      }

      case 'query_progress':
        setState((prev) => ({
          ...prev,
          queryProgress: eventData,
        }));
        debugLog('[>>] Query progress:', eventData);
        break;

      case 'stream_chunk': {
        logEntry.category = 'stream';
        logEntry.level = 'info';

        const chunkContent = String(eventData.chunk || '');
        const chunkType = String(eventData.content_type || 'assistant_text');

        setState((prev) => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            if (chunkType === 'reasoning') {
              // Append to reasoning field
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                reasoning: (lastMessage.reasoning || '') + chunkContent,
              };
            } else {
              // Append to content field (default behavior)
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                content: lastMessage.content + chunkContent,
              };
            }
          } else {
            // Create new assistant message
            const newMsg: Message = {
              id: Date.now().toString(),
              type: 'assistant',
              content: chunkType === 'reasoning' ? '' : chunkContent,
              timestamp: new Date(),
            };
            if (chunkType === 'reasoning') {
              newMsg.reasoning = chunkContent;
            }
            newMessages.push(newMsg);
          }
          return {
            ...prev,
            messages: newMessages,
          };
        });
        break;
      }

      case 'query_completed': {
        logEntry.category = 'query';
        logEntry.level = 'success';
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const completedQuery = String(eventData?.query || '')
          .trim()
          .toLowerCase();
        const completedResponse = eventData?.response;
        const wasClearCommand = completedQuery === '/clear';
        if (wasClearCommand) {
          queuedMessagesRef.current = [];
          setQueuedMessages([]);
        }
        setState((prev) => {
          let nextMessages = wasClearCommand
            ? []
            : ensureCompletedAssistantMessage(prev.messages, completedResponse, (responseText) => ({
                id: Date.now().toString(),
                type: 'assistant',
                content: responseText,
                timestamp: new Date(),
              }));

          // Deduplication: some thinking models emit their entire response via the
          // reasoning_content field with no separate content field. The backend fallback
          // (GetResponse) copies reasoning→content so conversation history is intact, and
          // ensureCompletedAssistantMessage then fills message.content from that same text.
          // This causes the identical text to appear in both the Reasoning dropdown and the
          // main chat area. When they match, clear reasoning so the answer shows only once
          // in the main chat (not in a collapsed dropdown that users have to expand).
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

          return {
            ...prev,
            messages: nextMessages,
            currentTodos: wasClearCommand ? [] : prev.currentTodos,
            isProcessing: activeRequestsRef.current > 0,
            lastError: null,
            queryProgress: null,
            securityApprovalRequest: null,
            securityPromptRequest: null,
            toolExecutions: wasClearCommand
              ? []
              : prev.toolExecutions.map((tool) => {
                  if (tool.status === 'started' || tool.status === 'running') {
                    return {
                      ...tool,
                      status: 'completed',
                      endTime: tool.endTime || new Date(),
                    };
                  }
                  return tool;
                }),
            logs: [...prev.logs, logEntry],
          };
        });
        debugLog('[OK] Query completed');
        break;
      }

      case 'tool_start':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState((prev) => {
          const toolCallID = String(eventData?.tool_call_id || '');
          const toolName = String(eventData?.tool_name || 'unknown_tool');
          const rawArgs = eventData?.arguments != null ? String(eventData.arguments) : undefined;
          const displayName = String(eventData?.display_name || toolName);
          const persona = typeof eventData?.persona === 'string' ? eventData.persona : undefined;
          const isSubagent = !!eventData?.is_subagent;
          const subagentType: ToolExecution['subagentType'] =
            eventData?.subagent_type === 'parallel' ? 'parallel' : isSubagent ? 'single' : undefined;

          // Check if we already have this tool from a legacy tool_execution event
          const existingIdx = prev.toolExecutions.findIndex((t) => {
            const d = t.details as Record<string, unknown> | undefined;
            const existingID = d?.tool_call_id || d?.id || t.id;
            return toolCallID && existingID === toolCallID;
          });

          if (existingIdx >= 0) {
            // Update existing with richer start data
            const updated = [...prev.toolExecutions];
            updated[existingIdx] = {
              ...updated[existingIdx],
              tool: toolName,
              status: 'started',
              startTime: updated[existingIdx].startTime, // keep existing start time
              message: displayName,
              arguments: updated[existingIdx].arguments || rawArgs,
              details: eventData,
              persona: updated[existingIdx].persona || persona,
              subagentType: updated[existingIdx].subagentType || subagentType,
            };
            const messages = [...prev.messages];
            for (let i = messages.length - 1; i >= 0; i -= 1) {
              const msg = messages[i];
              if (msg.type !== 'assistant') continue;
              const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
              if (!toolRefs.some((ref) => ref.toolId === updated[existingIdx].id)) {
                toolRefs.push({
                  toolId: updated[existingIdx].id,
                  toolName,
                  label: displayName,
                  parallel: subagentType === 'parallel' || undefined,
                });
                messages[i] = { ...msg, toolRefs };
              }
              break;
            }
            return { ...prev, messages, toolExecutions: updated, logs: [...prev.logs, logEntry] };
          }

          // Add new tool execution from rich start event
          const newTool: ToolExecution = {
            id: toolCallID || `${toolName}-${Date.now()}`,
            tool: toolName,
            status: 'started',
            message: displayName,
            startTime: new Date(),
            details: eventData,
            arguments: rawArgs,
            persona,
            subagentType,
          };
          const messages = [...prev.messages];
          for (let i = messages.length - 1; i >= 0; i -= 1) {
            const msg = messages[i];
            if (msg.type !== 'assistant') continue;
            const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
            if (!toolRefs.some((ref) => ref.toolId === newTool.id)) {
              toolRefs.push({
                toolId: newTool.id,
                toolName,
                label: displayName,
                parallel: subagentType === 'parallel' || undefined,
              });
              messages[i] = { ...msg, toolRefs };
            }
            break;
          }

          return {
            ...prev,
            messages,
            toolExecutions: [...prev.toolExecutions, newTool],
            logs: [...prev.logs, logEntry],
          };
        });
        debugLog('[tool] Tool start:', eventData?.tool_name);
        break;

      case 'tool_end':
        logEntry.category = 'tool';
        logEntry.level = eventData?.status === 'failed' ? 'error' : 'info';
        setState((prev) => {
          const toolCallID = String(eventData?.tool_call_id || '');
          const status: ToolExecution['status'] = eventData?.status === 'failed' ? 'error' : 'completed';
          const result = eventData?.result != null ? String(eventData.result) : undefined;
          const error = eventData?.error != null ? String(eventData.error) : undefined;

          let matched = false;
          const updatedExecutions = prev.toolExecutions.map((t) => {
            const d = t.details as Record<string, unknown> | undefined;
            const existingID = d?.tool_call_id || t.id;
            const match = toolCallID && existingID === toolCallID;
            if (!match) {
              // Also try matching by tool name + no end time (for backward compat)
              const nameMatch = !toolCallID && t.tool === eventData?.tool_name && !t.endTime;
              if (!nameMatch) return t;
            }
            matched = true;

            return {
              ...t,
              status,
              endTime: new Date(),
              result: t.result || result || error,
              details: eventData,
              arguments: t.arguments, // preserve arguments from tool_start
            };
          });

          if (!matched) {
            const fallbackExecution: ToolExecution = {
              id: toolCallID || `${eventData?.tool_name || 'tool'}-${Date.now()}`,
              tool: String(eventData?.tool_name || 'unknown_tool'),
              status,
              message: String(eventData?.display_name || eventData?.tool_name || 'Tool'),
              startTime: new Date(),
              endTime: new Date(),
              details: eventData,
              arguments: eventData?.arguments != null ? String(eventData.arguments) : undefined,
              result: result || error,
            };
            return {
              ...prev,
              toolExecutions: [...prev.toolExecutions, fallbackExecution],
              logs: [...prev.logs, logEntry],
            };
          }

          return { ...prev, toolExecutions: updatedExecutions, logs: [...prev.logs, logEntry] };
        });
        debugLog('[tool] Tool end:', eventData?.tool_name, eventData?.status);
        break;

      case 'subagent_activity':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState((prev) => {
          const activity: SubagentActivity = {
            id: String(event.id || `${Date.now()}-${Math.random()}`),
            toolCallId: String(eventData?.tool_call_id || ''),
            toolName: String(eventData?.tool_name || 'run_subagent'),
            phase: eventData?.phase === 'spawn' || eventData?.phase === 'complete' ? eventData.phase : 'output',
            message: String(eventData?.message || '').trim(),
            timestamp: new Date(),
            taskId: typeof eventData?.task_id === 'string' ? eventData.task_id : undefined,
            persona: typeof eventData?.persona === 'string' ? eventData.persona : undefined,
            isParallel: eventData?.is_parallel === true,
            provider: typeof eventData?.provider === 'string' ? eventData.provider : undefined,
            model: typeof eventData?.model === 'string' ? eventData.model : undefined,
            taskCount: typeof eventData?.task_count === 'number' ? eventData.task_count : undefined,
            failures: typeof eventData?.failures === 'number' ? eventData.failures : undefined,
          };

          if (!activity.message) {
            return { ...prev, logs: [...prev.logs, logEntry] };
          }

          return {
            ...prev,
            subagentActivities: [...prev.subagentActivities, activity].slice(-500),
            logs: [...prev.logs, logEntry],
          };
        });
        break;

      case 'agent_message': {
        // Handle agent system messages from the backend
        let category = String(eventData?.category || 'info');
        const message = String(eventData?.message || '');

        // Clean ANSI codes from the message
        const cleanedMsg = message
          .replace(new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[mGKHJABCD]`, 'g'), '')
          .trim();
        const suppressInChat = shouldSuppressAgentMessageInChat(cleanedMsg);

        // Auto-classify info messages by content pattern so important ones render in chat
        if (category === 'info') {
          if (/^\[FAIL\]|\[!!\]/.test(cleanedMsg)) {
            category = 'error';
          } else if (/^\[WARN\]|\[~\]|\[!\]/.test(cleanedMsg)) {
            category = 'warning';
          } else if (/^\[OK\]|\[edit\]|\[chart\]/.test(cleanedMsg)) {
            category = 'info_rendered'; // meaningful info that should render
          }
        }

        if (category === 'tool_log' && cleanedMsg) {
          // Tool logs are operational breadcrumbs from the router.
          // Do not create synthetic tool execution rows from these logs; rich
          // tool_start/tool_end events are the source of truth for tool state.
          logEntry.category = 'tool';
          logEntry.level = 'info';

          const toolAction = String(eventData?.action || 'tool');
          const toolTarget = String(eventData?.target || '');
          const parsedToolName = extractToolNameFromToolLogTarget(toolTarget);

          setState((prev) => {
            // Best effort: if this log says a tool is executing, mark its
            // most recent started row as running (without adding a duplicate row).
            if (/^executing tool$/i.test(toolAction) && parsedToolName) {
              let touched = false;
              const updated = [...prev.toolExecutions];
              for (let i = updated.length - 1; i >= 0; i--) {
                const row = updated[i];
                if (row.tool !== parsedToolName || row.endTime) continue;
                if (row.status !== 'running') {
                  updated[i] = { ...row, status: 'running' };
                }
                touched = true;
                break;
              }
              if (touched) {
                return { ...prev, toolExecutions: updated, logs: [...prev.logs, logEntry] };
              }
            }

            return { ...prev, logs: [...prev.logs, logEntry] };
          });
        } else if ((category === 'warning' || category === 'error') && !suppressInChat) {
          // Warning/error messages are operational notices, not model reasoning.
          logEntry.category = 'system';
          logEntry.level = category === 'error' ? 'error' : 'warning';

          setState((prev) => {
            const newMessages = [...prev.messages];
            const lastMessage = newMessages[newMessages.length - 1];
            if (lastMessage && lastMessage.type === 'assistant') {
              const prefixedMsg = category === 'error' ? `\n\nWarning: ${cleanedMsg}` : `\n\nNote: ${cleanedMsg}`;
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                content: (lastMessage.content || '') + prefixedMsg,
              };
            }
            return { ...prev, messages: newMessages, logs: [...prev.logs, logEntry] };
          });
        } else if (category === 'info_rendered' && cleanedMsg && !suppressInChat) {
          // Meaningful info messages should render in chat, but not inside reasoning.
          logEntry.category = 'system';
          logEntry.level = 'info';

          setState((prev) => {
            const newMessages = [...prev.messages];
            const lastMessage = newMessages[newMessages.length - 1];
            if (lastMessage && lastMessage.type === 'assistant') {
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                content: `${lastMessage.content || ''}\n\nInfo: ${cleanedMsg}`,
              };
            }
            return { ...prev, messages: newMessages, logs: [...prev.logs, logEntry] };
          });
        }
        // For plain 'info' (unclassified): silently skip rendering in WebUI.
        // These include blank lines, iteration markers, context pruning messages, etc.
        // The meaningful assistant content comes through stream_chunk events.
        break;
      }

      case 'todo_update': {
        logEntry.category = 'tool';
        logEntry.level = 'info';
        const normalizedTodos = normalizeTodoList(eventData?.todos);
        setState((prev) => ({
          ...prev,
          currentTodos: normalizedTodos,
          logs: [...prev.logs, logEntry],
        }));
        break;
      }

      case 'file_changed':
        logEntry.category = 'file';
        logEntry.level = 'info';
        setState((prev) => {
          const newLogs = [...prev.logs, logEntry];

          // Track file edits
          const newFileEdit = {
            path: String(eventData.path || eventData.file_path || 'Unknown'),
            action: String(eventData.action || eventData.operation || 'edited'),
            timestamp: new Date(),
            linesAdded: typeof eventData.lines_added === 'number' ? eventData.lines_added : undefined,
            linesDeleted: typeof eventData.lines_deleted === 'number' ? eventData.lines_deleted : undefined,
          };

          // Add to file edits (keep last 50)
          const updatedFileEdits = [...prev.fileEdits, newFileEdit].slice(-50);

          return { ...prev, logs: newLogs, fileEdits: updatedFileEdits };
        });
        debugLog('[edit] File changed:', eventData.path);
        break;

      case 'file_content_changed':
        logEntry.category = 'file';
        logEntry.level = 'warning';
        {
          const { file_path: fpath, mod_time, size } = eventData;
          const detail = {
            path: fpath || '',
            mtime: typeof mod_time === 'number' ? mod_time : 0,
            size: typeof size === 'number' ? size : 0,
            deleted: (typeof size === 'number' ? size : 0) === 0 && (typeof mod_time === 'number' ? mod_time : 0) === 0,
          };
          document.dispatchEvent(new CustomEvent('file_externally_modified', { detail }));
          setState((prev) => ({ ...prev, logs: [...prev.logs, logEntry] }));
        }
        debugLog('[file] File content changed externally:', eventData?.file_path);
        break;

      case 'terminal_output':
        logEntry.category = 'system';
        logEntry.level = 'info';
        // Handle terminal output - this will be processed by the Terminal component
        setState((prev) => ({
          ...prev,
          logs: [...prev.logs, logEntry],
        }));
        debugLog('[term] Terminal output received:', eventData);
        break;

      case 'error': {
        logEntry.category = 'system';
        logEntry.level = 'error';
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const errorMessage = String(eventData?.message || 'Unknown error');
        setState((prev) => ({
          ...prev,
          isProcessing: activeRequestsRef.current > 0,
          queryProgress: null,
          lastError: errorMessage,
          messages: [
            ...prev.messages,
            {
              id: Date.now().toString(),
              type: 'assistant',
              content: `[FAIL] Error: ${errorMessage}`,
              timestamp: new Date(),
            },
          ],
          logs: [...prev.logs, logEntry],
        }));
        logError('[FAIL] Error event: ' + String(eventData));
        addNotification('error', 'Agent Error', errorMessage, 8000);
        break;
      }

      case 'metrics_update':
        logEntry.category = 'system';
        logEntry.level = 'info';
        setState((prev) => ({
          ...prev,
          provider: String(eventData?.provider || prev.provider),
          model: String(eventData?.model || prev.model),
          stats: {
            ...prev.stats,
            ...eventData,
          },
          logs: [...prev.logs, logEntry],
        }));
        break;

      case 'workspace_changed':
        logEntry.category = 'system';
        logEntry.level = 'info';
        debugLog('[workspace] Workspace changed:', eventData);
        if (!eventData?.client_id || eventData.client_id === getWebUIClientId()) {
          if (eventData?.source === 'worktree_switch' || eventData?.source === 'worktree_clear') {
            // Worktree chat switches and clears should NOT hard-reload — they
            // change the workspace root for the active chat but the browser
            // tab and all in-memory React state should stay intact.  Dispatch
            // a DOM event so workspace-dependent UI (file tree, breadcrumbs…)
            // refreshes.
            debugLog('[workspace] Worktree switch/clear detected — refreshing workspace state without page reload');
            setState((prev) => ({ ...prev, logs: [...prev.logs, logEntry] }));
            const detail = {
              workspaceRoot: String(eventData.workspace_root || ''),
              daemonRoot: String(eventData.daemon_root || ''),
            };
            window.dispatchEvent(new CustomEvent('ledit:workspace-changed', { detail }));
          } else {
            // Full workspace change (e.g. switch-to-worktree from the worktree
            // panel, SSH navigation, etc.) — reload to pick up the new root.
            window.location.reload();
          }
        }
        break;

      case 'security_approval_request':
        logEntry.category = 'system';
        logEntry.level = 'warning';
        // Skip status echo events that would briefly re-show the dialog
        if (eventData?.status === 'responded') {
          debugLog('[security] Approval response acknowledged:', eventData?.request_id);
          break;
        }
        setState((prev) => ({
          ...prev,
          securityApprovalRequest: {
            requestId: String(eventData?.request_id || ''),
            toolName: String(eventData?.tool_name || ''),
            riskLevel: String(eventData?.risk_level || 'CAUTION'),
            reasoning: String(eventData?.reasoning || ''),
            command: eventData?.command != null ? String(eventData.command) : undefined,
            riskType: eventData?.risk_type != null ? String(eventData.risk_type) : undefined,
            target: eventData?.target != null ? String(eventData.target) : undefined,
          },
          logs: [...prev.logs, logEntry],
        }));
        debugLog('[security] Approval request:', eventData?.tool_name, eventData?.risk_level);
        break;

      case 'security_prompt_request':
        logEntry.category = 'system';
        logEntry.level = 'warning';
        // Skip status echo events (e.g., {"status": "responded"}) that would
        // briefly re-show the dialog with empty data after the user responds.
        if (eventData?.status === 'responded') {
          debugLog('[security] Prompt response acknowledged:', eventData?.request_id);
          break;
        }
        // Only show dialog when there's an actual prompt (not a status echo)
        if (!eventData?.prompt) {
          break;
        }
        setState((prev) => ({
          ...prev,
          securityPromptRequest: {
            requestId: String(eventData?.request_id || ''),
            prompt: String(eventData?.prompt || ''),
            filePath: eventData?.file_path != null ? String(eventData.file_path) : undefined,
            concern: eventData?.concern != null ? String(eventData.concern) : undefined,
          },
          logs: [...prev.logs, logEntry],
        }));
        debugLog('[security] Prompt request:', eventData?.file_path, eventData?.concern);
        break;

      default:
        // Handle any unknown event types
        logEntry.level = 'warning';
        setState((prev) => ({
          ...prev,
          logs: [...prev.logs, logEntry],
        }));
        debugLog('[?] Unknown event type:', event.type, eventData);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- intentionally empty: all external values are accessed via refs or closure-stable setState/setQueuedMessages

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
            ...prev,
            isProcessing: false,
            queryProgress: null,
            lastError: null,
            // Finalise any tool executions that were left in a started/running state
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
