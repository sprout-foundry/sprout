import { useCallback, useRef } from 'react';
import type { AppState } from '../types/app';
import type { Message, ToolExecution, LogEntry, SubagentActivity } from '../types/app';
import { getWebUIClientId } from '../services/clientSession';
import { debugLog } from '../utils/log';
import { ensureCompletedAssistantMessage } from '../utils/chatCompletion';
import {
  shouldSuppressAgentMessageInChat,
  extractToolNameFromToolLogTarget,
  normalizeTodoList,
} from '../utils/agentMessages';

export interface UseWebSocketEventsOptions {
  state: AppState;
  setState: React.Dispatch<React.SetStateAction<AppState>>;
  setInputValue: React.Dispatch<React.SetStateAction<string>>;
  setQueuedMessages: React.Dispatch<React.SetStateAction<string[]>>;
  queuedMessagesRef: React.MutableRefObject<string[]>;
}

export interface UseWebSocketEventsReturn {
  handleEvent: (event: any) => void;
  activeChatIdRef: React.MutableRefObject<string | null>;
  activeRequestsRef: React.MutableRefObject<number>;
  /** Ref used by the main useEffect cleanup to clear a pending debounce timer */
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
}

export default function useWebSocketEvents({
  state,
  setState,
  setInputValue,
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

  // ── The monolithic WebSocket event handler ────────────────────────────
  const handleEvent = useCallback((event: any) => {
    // Filter out ping events and webpack dev server events early to prevent console spam
    const filteredEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'];
    if (filteredEvents.includes(event.type)) {
      return; // Don't process these events
    }

    // Per-chat event filtering: only process message events for the active chat
    const perChatEvents = new Set(['query_started', 'stream_chunk', 'query_completed', 'query_progress', 'tool_start', 'tool_end', 'todo_update', 'subagent_activity', 'agent_message', 'error']);
    if (perChatEvents.has(event.type) && event.data?.chat_id && activeChatIdRef.current && event.data.chat_id !== activeChatIdRef.current) {
      return; // event is for a different chat session
    }

    debugLog('[msg] Received event:', event.type, event.data);

    // Create log entry for all events
    const logEntry: LogEntry = {
      id: `${Date.now()}-${Math.random()}`,
      type: event.type,
      timestamp: new Date(),
      data: event.data,
      level: 'info',
      category: 'system'
    };

    // Determine log level and category based on event type
    switch(event.type) {
      case 'connection_status':
        if (event.data?.client_id && event.data.client_id !== getWebUIClientId()) {
          break;
        }
        logEntry.category = 'system';
        logEntry.level = event.data.connected ? 'success' : 'warning';
        const incomingSessionId = typeof event.data?.session_id === 'string' ? event.data.session_id : null;

        // Debounce connection status updates to prevent rapid re-renders
        const newConnectionState = event.data.connected;

        // Only update if state actually changed
        if (newConnectionState !== lastConnectionStateRef.current) {
          // Clear any pending timeout
          if (connectionTimeoutRef.current) {
            clearTimeout(connectionTimeoutRef.current);
          }

          // Debounce the state update
          connectionTimeoutRef.current = setTimeout(() => {
            lastConnectionStateRef.current = newConnectionState;
            setState(prev => ({
              ...prev,
              // NOTE:
              // WebSocket `session_id` is a transport connection id (ws_<timestamp>),
              // not a chat session id. It changes on reconnect and must never clear chat state.
              sessionId: prev.sessionId || incomingSessionId,
              isConnected: newConnectionState,
              logs: [...prev.logs, logEntry]
            }));
          }, 300); // Wait 300ms to confirm the connection state is stable
        }
        debugLog('[link] Connection status updated:', newConnectionState);
        break;

      case 'query_started':
        logEntry.category = 'query';
        logEntry.level = 'info';
        const startedQuery = event.data?.query || '';
        setState(prev => ({
          ...prev,
          isProcessing: true,
          lastError: null,
          queryCount: prev.queryCount + 1,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'user',
            content: startedQuery,
            timestamp: new Date()
          }],
          toolExecutions: [], // Clear previous tool executions
          fileEdits: [],      // Clear previous file edits for current-run status metrics
          subagentActivities: [],
          queryProgress: null, // Clear previous progress
          currentTodos: [],    // Clear previous todos
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[>>] Query started:', startedQuery);
        break;

      case 'query_progress':
        setState(prev => ({
          ...prev,
          queryProgress: event.data
        }));
        debugLog('[>>] Query progress:', event.data);
        break;

      case 'stream_chunk':
        logEntry.category = 'stream';
        logEntry.level = 'info';
        
        const chunkContent = event.data.chunk || '';
        const chunkType = event.data.content_type || 'assistant_text';
        
        setState(prev => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            if (chunkType === 'reasoning') {
              // Append to reasoning field
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                reasoning: (lastMessage.reasoning || '') + chunkContent
              };
            } else {
              // Append to content field (default behavior)
              newMessages[newMessages.length - 1] = {
                ...lastMessage,
                content: lastMessage.content + chunkContent
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
            messages: newMessages
          };
        });
        break;

      case 'query_completed':
        logEntry.category = 'query';
        logEntry.level = 'success';
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const completedQuery = String(event.data?.query || '').trim().toLowerCase();
        const completedResponse = event.data?.response;
        const wasClearCommand = completedQuery === '/clear';
        if (wasClearCommand) {
          queuedMessagesRef.current = [];
          setQueuedMessages([]);
        }
        setState(prev => {
          let nextMessages = wasClearCommand
            ? []
            : ensureCompletedAssistantMessage(prev.messages, completedResponse, (responseText) => ({
                id: Date.now().toString(),
                type: 'assistant',
                content: responseText,
                timestamp: new Date()
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
              nextMessages = [
                ...nextMessages.slice(0, -1),
                { ...lastMsg, reasoning: undefined },
              ];
            }
          }

          return {
            ...prev,
            messages: nextMessages,
            currentTodos: wasClearCommand ? [] : prev.currentTodos,
            isProcessing: activeRequestsRef.current > 0,
            lastError: null,
            queryProgress: null,
            toolExecutions: wasClearCommand
              ? []
              : prev.toolExecutions.map((tool) => {
                  if (tool.status === 'started' || tool.status === 'running') {
                    return {
                      ...tool,
                      status: 'completed',
                      endTime: tool.endTime || new Date()
                    };
                  }
                  return tool;
                }),
            logs: [...prev.logs, logEntry]
          };
        });
        debugLog('[OK] Query completed');
        break;

      case 'tool_start':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState(prev => {
          const toolCallID = String(event.data?.tool_call_id || '');
          const toolName = String(event.data?.tool_name || 'unknown_tool');
          const rawArgs = event.data?.arguments != null ? String(event.data.arguments) : undefined;
          const displayName = String(event.data?.display_name || toolName);
          const persona = typeof event.data?.persona === 'string' ? event.data.persona : undefined;
          const isSubagent = !!event.data?.is_subagent;
          const subagentType: ToolExecution['subagentType'] = event.data?.subagent_type === 'parallel'
            ? 'parallel'
            : isSubagent ? 'single' : undefined;

          // Check if we already have this tool from a legacy tool_execution event
          const existingIdx = prev.toolExecutions.findIndex(t => {
            const existingID = t.details?.tool_call_id || t.details?.id || t.id;
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
              details: event.data,
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
            details: event.data,
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
            logs: [...prev.logs, logEntry]
          };
        });
        debugLog('[tool] Tool start:', event.data?.tool_name);
        break;

      case 'tool_end':
        logEntry.category = 'tool';
        logEntry.level = event.data?.status === 'failed' ? 'error' : 'info';
        setState(prev => {
          const toolCallID = String(event.data?.tool_call_id || '');
          const status: ToolExecution['status'] = event.data?.status === 'failed' ? 'error' : 'completed';
          const result = event.data?.result != null ? String(event.data.result) : undefined;
          const error = event.data?.error != null ? String(event.data.error) : undefined;

          let matched = false;
          const updatedExecutions = prev.toolExecutions.map(t => {
            const existingID = t.details?.tool_call_id || t.id;
            const match = toolCallID && existingID === toolCallID;
            if (!match) {
              // Also try matching by tool name + no end time (for backward compat)
              const nameMatch = !toolCallID && t.tool === event.data?.tool_name && !t.endTime;
              if (!nameMatch) return t;
            }
            matched = true;

            return {
              ...t,
              status,
              endTime: new Date(),
              result: t.result || result || error,
              details: event.data,
              arguments: t.arguments,  // preserve arguments from tool_start
            };
          });

          if (!matched) {
            const fallbackExecution: ToolExecution = {
              id: toolCallID || `${event.data?.tool_name || 'tool'}-${Date.now()}`,
              tool: String(event.data?.tool_name || 'unknown_tool'),
              status,
              message: String(event.data?.display_name || event.data?.tool_name || 'Tool'),
              startTime: new Date(),
              endTime: new Date(),
              details: event.data,
              arguments: event.data?.arguments != null ? String(event.data.arguments) : undefined,
              result: result || error,
            };
            return {
              ...prev,
              toolExecutions: [...prev.toolExecutions, fallbackExecution],
              logs: [...prev.logs, logEntry]
            };
          }

          return { ...prev, toolExecutions: updatedExecutions, logs: [...prev.logs, logEntry] };
        });
        debugLog('[tool] Tool end:', event.data?.tool_name, event.data?.status);
        break;

      case 'subagent_activity':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        setState(prev => {
          const activity: SubagentActivity = {
            id: String(event.id || `${Date.now()}-${Math.random()}`),
            toolCallId: String(event.data?.tool_call_id || ''),
            toolName: String(event.data?.tool_name || 'run_subagent'),
            phase: event.data?.phase === 'spawn' || event.data?.phase === 'complete' ? event.data.phase : 'output',
            message: String(event.data?.message || '').trim(),
            timestamp: new Date(),
            taskId: typeof event.data?.task_id === 'string' ? event.data.task_id : undefined,
            persona: typeof event.data?.persona === 'string' ? event.data.persona : undefined,
            isParallel: event.data?.is_parallel === true,
            provider: typeof event.data?.provider === 'string' ? event.data.provider : undefined,
            model: typeof event.data?.model === 'string' ? event.data.model : undefined,
            taskCount: typeof event.data?.task_count === 'number' ? event.data.task_count : undefined,
            failures: typeof event.data?.failures === 'number' ? event.data.failures : undefined,
          };

          if (!activity.message) {
            return { ...prev, logs: [...prev.logs, logEntry] };
          }

          return {
            ...prev,
            subagentActivities: [...prev.subagentActivities, activity].slice(-500),
            logs: [...prev.logs, logEntry]
          };
        });
        break;

      case 'agent_message':
        {
          // Handle agent system messages from the backend
          let category = String(event.data?.category || 'info');
          const message = String(event.data?.message || '');

          // Clean ANSI codes from the message
          const cleanedMsg = message.replace(new RegExp(String.fromCharCode(27) + '\\[[0-9;]*[mGKHJABCD]', 'g'), '').trim();
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

            const toolAction = String(event.data?.action || 'tool');
            const toolTarget = String(event.data?.target || '');
            const parsedToolName = extractToolNameFromToolLogTarget(toolTarget);

            setState(prev => {
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

            setState(prev => {
              const newMessages = [...prev.messages];
              const lastMessage = newMessages[newMessages.length - 1];
              if (lastMessage && lastMessage.type === 'assistant') {
                const prefixedMsg = category === 'error'
                  ? `\n\nWarning: ${cleanedMsg}`
                  : `\n\nNote: ${cleanedMsg}`;
                newMessages[newMessages.length - 1] = {
                  ...lastMessage,
                  content: (lastMessage.content || '') + prefixedMsg
                };
              }
              return { ...prev, messages: newMessages, logs: [...prev.logs, logEntry] };
            });
          } else if (category === 'info_rendered' && cleanedMsg && !suppressInChat) {
            // Meaningful info messages should render in chat, but not inside reasoning.
            logEntry.category = 'system';
            logEntry.level = 'info';

            setState(prev => {
              const newMessages = [...prev.messages];
              const lastMessage = newMessages[newMessages.length - 1];
              if (lastMessage && lastMessage.type === 'assistant') {
                newMessages[newMessages.length - 1] = {
                  ...lastMessage,
                  content: (lastMessage.content || '') + `\n\nInfo: ${cleanedMsg}`
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

      case 'todo_update':
        logEntry.category = 'tool';
        logEntry.level = 'info';
        const normalizedTodos = normalizeTodoList(event.data?.todos);
        setState(prev => ({
          ...prev,
          currentTodos: normalizedTodos,
          logs: [...prev.logs, logEntry]
        }));
        break;

      case 'file_changed':
        logEntry.category = 'file';
        logEntry.level = 'info';
        setState(prev => {
          const newLogs = [...prev.logs, logEntry];

          // Track file edits
          const newFileEdit = {
            path: event.data.path || event.data.file_path || 'Unknown',
            action: event.data.action || event.data.operation || 'edited',
            timestamp: new Date(),
            linesAdded: event.data.lines_added,
            linesDeleted: event.data.lines_deleted
          };

          // Add to file edits (keep last 50)
          const updatedFileEdits = [...prev.fileEdits, newFileEdit].slice(-50);

          return { ...prev, logs: newLogs, fileEdits: updatedFileEdits };
        });
        debugLog('[edit] File changed:', event.data.path);
        break;

      case 'terminal_output':
        logEntry.category = 'system';
        logEntry.level = 'info';
        // Handle terminal output - this will be processed by the Terminal component
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[term] Terminal output received:', event.data);
        break;

      case 'error':
        logEntry.category = 'system';
        logEntry.level = 'error';
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const errorMessage = event.data?.message || 'Unknown error';
        setState(prev => ({
          ...prev,
          isProcessing: activeRequestsRef.current > 0,
          queryProgress: null,
          lastError: errorMessage,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'assistant',
            content: `[FAIL] Error: ${errorMessage}`,
            timestamp: new Date()
          }],
          logs: [...prev.logs, logEntry]
        }));
        console.error('[FAIL] Error event:', event.data);
        break;

      case 'metrics_update':
        logEntry.category = 'system';
        logEntry.level = 'info';
        setState(prev => ({
          ...prev,
          provider: event.data?.provider || prev.provider,
          model: event.data?.model || prev.model,
          stats: {
            ...prev.stats,
            ...event.data
          },
          logs: [...prev.logs, logEntry]
        }));
        break;

      case 'workspace_changed':
        logEntry.category = 'system';
        logEntry.level = 'info';
        debugLog('[workspace] Workspace changed:', event.data);
        if (!event.data?.client_id || event.data.client_id === getWebUIClientId()) {
          window.location.reload();
        }
        break;

      default:
        // Handle any unknown event types
        logEntry.level = 'warning';
        setState(prev => ({
          ...prev,
          logs: [...prev.logs, logEntry]
        }));
        debugLog('[?] Unknown event type:', event.type, event.data);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- intentionally empty: all external values are accessed via refs or closure-stable setState/setQueuedMessages

  return {
    handleEvent,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
  };
}
