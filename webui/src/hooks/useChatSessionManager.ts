import type { Message, ToolRef } from '@sprout/ui';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { ApiService } from '../services/api';
import {
  listChatSessions,
  createChatSession,
  deleteChatSession,
  renameChatSession,
  switchChatSession,
} from '../services/chatSessions';
import type { AppState } from '../types/app';
import { debugLog } from '../utils/log';
import { generateMessageId } from '../utils/messageId';
import { trimMessages } from '../utils/messageWindow';
import { NATIVE_CHAT_ENABLED } from '../services/nativeChatStubs/nativeChatFlag';

const TOOL_MARKER = /\[executing tool \[([^\]]+)\]/;
function extractToolRefsFromContent(content: string): ToolRef[] {
  const refs: ToolRef[] = [];
  const lines = content.split('\n');
  for (const line of lines) {
    const match = line.match(TOOL_MARKER);
    if (!match) continue;
    const toolName = match[1].split(' ')[0] || match[1];
    refs.push({
      toolId: `restored-tool-${refs.length}-${Date.now()}`,
      toolName,
      label: toolName,
    });
  }
  return refs;
}

export interface QueuedMessage {
  message: string;
  chatId: string | null;
}

export interface UseChatSessionManagerParams {
  setState: AppStoreSetState;
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  queuedMessagesRef: React.MutableRefObject<QueuedMessage[]>;
  isProcessing: boolean;
}

export interface UseChatSessionManagerReturn {
  loadChatSessions: () => Promise<void>;
  handleActiveChatChange: (id: string) => Promise<void>;
  handleCreateChat: () => Promise<string | null>;
  handleDeleteChat: (id: string) => Promise<void>;
  handleRenameChat: (id: string, name: string) => Promise<void>;
  handleSendMessage: (message: string, options?: { allowConcurrent?: boolean; chatId?: string }) => Promise<void>;
  handleQueueMessage: (message: string) => void;
  handleRemoveQueuedMessage: (index: number) => void;
  handleEditQueuedMessage: (index: number, newText: string) => void;
  handleReorderQueuedMessages: (fromIndex: number, toIndex: number) => void;
  handleClearQueuedMessages: () => void;
  handleStopProcessing: () => Promise<void>;
  handleRetractSteer: () => Promise<boolean>;
  /** All queued entries, each tagged with the chat it was queued for. */
  queuedMessages: QueuedMessage[];
  queuedMessagesCount: number;
  setQueuedMessagesCount: React.Dispatch<React.SetStateAction<number>>;
}

/**
 * Hook to manage chat sessions and message sending.
 * Returns all chat CRUD operations and message handling functions.
 */
export function useChatSessionManager({
  setState,
  activeRequestsRef,
  activeChatIdRef,
  queuedMessagesRef,
  isProcessing,
}: UseChatSessionManagerParams): UseChatSessionManagerReturn {
  const [queuedMessagesCount, setQueuedMessagesCount] = useState(0);
  // Mirror of queuedMessagesRef for rendering. The ref is the source of
  // truth for the drain effect (it must see through renders), but the
  // queue panel needs the array itself — CommandInput falls back to dead
  // no-op handlers when these props are missing, which is what the
  // 3b516bbb5 refactor regressed: the badge counted messages but the
  // panel rendered empty with non-functional buttons.
  const [queuedMessages, setQueuedMessages] = useState<QueuedMessage[]>([]);
  const apiService = ApiService.getInstance();

  // Retract state: tracks the last steer message so the user can pull it
  // back for editing with Up-arrow. Cleared when the query completes or
  // the user explicitly stops processing.
  const lastSteerMessageRef = useRef<string>('');
  const lastSteerBubbleIdRef = useRef<string>('');

  const loadChatSessions = useCallback(async () => {
    try {
      const response = await listChatSessions();
      const activeChatId = response.active_chat_id || null;
      let initialMessages: Message[] = [];
      if (activeChatId) {
        try {
          const switchResp = await switchChatSession(activeChatId);
          initialMessages = (switchResp.chat_session.messages ?? [])
            .filter((m) => m.role === 'user' || m.role === 'assistant')
            .map((m, i) => ({
              id: `chat-${activeChatId}-${i}`,
              type: m.role as 'user' | 'assistant',
              content: typeof m.content === 'string' ? m.content : '',
              timestamp: new Date(),
              ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
            }));
          if (!activeChatIdRef.current) {
            activeChatIdRef.current = activeChatId;
          }
        } catch (e) {
          debugLog('[chat] Failed to load initial messages:', e);
        }
      }
      setState((prev) => ({
        chatSessions: response.chat_sessions ?? [],
        activeChatId: prev.activeChatId || activeChatId,
        messages:
          prev.messages.length === 0 && initialMessages.length > 0 ? trimMessages(initialMessages) : prev.messages,
      }));
    } catch (error) {
      debugLog('[chat] Failed to load chat sessions:', error);
    }
  }, [setState, activeChatIdRef]);

  const handleActiveChatChange = useCallback(
    async (id: string) => {
      const currentId = activeChatIdRef.current;
      if (currentId === id) return;

      // Track the expected chat ID to detect stale async responses
      const switchId = id;
      activeChatIdRef.current = id;

      setState((prev) => {
        const cached = prev.perChatCache[id];
        // Check pending events for completion signals. If the cache says
        // isProcessing=true but a query_completed/session_terminated/error
        // event arrived while viewing another chat, the cached flag is stale.
        // The backend fetch below will confirm, but this prevents a brief
        // phantom spinner on switch-back.
        const hasCompletionInPending = (cached?.pendingEvents ?? []).some(
          (e) => e.type === 'query_completed' || e.type === 'session_terminated' || e.type === 'error',
        );
        const restoredIsProcessing = hasCompletionInPending ? false : (cached?.isProcessing ?? false);
        const newCache = currentId
          ? {
              ...prev.perChatCache,
              [currentId]: {
                messages: trimMessages(prev.messages),
                toolExecutions: prev.toolExecutions,
                fileEdits: prev.fileEdits,
                subagentActivities: prev.subagentActivities,
                currentTodos: prev.currentTodos,
                queryProgress: prev.queryProgress,
                lastError: prev.lastError,
                isProcessing: prev.isProcessing,
                provider: prev.provider,
                model: prev.model,
                queryCount: prev.queryCount,
              },
            }
          : prev.perChatCache;
        activeRequestsRef.current = restoredIsProcessing ? 1 : 0;
        return {
          activeChatId: id,
          messages: cached?.messages ?? [],
          isProcessing: restoredIsProcessing,
          toolExecutions: cached?.toolExecutions ?? [],
          fileEdits: cached?.fileEdits ?? [],
          subagentActivities: cached?.subagentActivities ?? [],
          currentTodos: cached?.currentTodos ?? [],
          // Only restore queryProgress if the chat is still processing.
          // A stale progress indicator from a query that completed while
          // viewing another chat is worse than no indicator.
          queryProgress: restoredIsProcessing ? (cached?.queryProgress ?? null) : null,
          lastError: cached?.lastError ?? null,
          perChatCache: newCache,
        };
      });

      try {
        const response = await switchChatSession(id);
        // Bail if user switched to yet another chat while we were loading
        if (activeChatIdRef.current !== switchId) return;
        const backendMessages: Message[] = (response.chat_session.messages ?? [])
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => ({
            id: `chat-${id}-${i}`,
            type: m.role as 'user' | 'assistant',
            content: typeof m.content === 'string' ? m.content : '',
            timestamp: new Date(),
            ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
          }));
        const backendIsActive = response.chat_session.active_query;

        setState((prev) => {
          // Backend is authoritative when it has at least as many messages,
          // OR the query is not active (backend has finalised and persisted
          // everything), OR the cache had pending events (stale signal).
          // Previously this used a naive length comparison that could prefer
          // shorter local state when streaming hadn't been persisted yet.
          const cached = prev.perChatCache[id];
          const hadPendingEvents = !!cached?.pendingEvents?.length;
          const useBackendMessages =
            backendMessages.length >= prev.messages.length || !backendIsActive || hadPendingEvents;
          // Drain pending events — the backend fetch is authoritative now.
          const newPerChatCache = { ...prev.perChatCache };
          if (cached && cached.pendingEvents) {
            newPerChatCache[id] = { ...cached };
            delete newPerChatCache[id].pendingEvents;
          }
          const finalIsProcessing = backendIsActive;
          activeRequestsRef.current = finalIsProcessing ? 1 : 0;
          return {
            activeChatId: response.active_chat_id,
            messages: useBackendMessages ? trimMessages(backendMessages) : prev.messages,
            isProcessing: finalIsProcessing,
            perChatCache: newPerChatCache,
          };
        });

        // Refresh the session list (tab titles/counts) without blocking the
        // switch — the messages are already on screen from the switch
        // response; the list refresh is cosmetic. The session_changed WS
        // event also updates this list, so a dropped refresh self-heals.
        void listChatSessions()
          .then((sessionsResp) => {
            if (activeChatIdRef.current !== switchId) return;
            setState((prev) => ({ chatSessions: sessionsResp.chat_sessions ?? [] }));
          })
          .catch((err) => {
            debugLog('[chat] Failed to refresh session list after switch:', err);
          });
      } catch (error) {
        if (activeChatIdRef.current !== switchId) return;
        activeChatIdRef.current = currentId;
        debugLog('[chat] Failed to switch chat session:', error);
      }
    },
    [setState, activeRequestsRef],
  );

  const handleCreateChat = useCallback(async (): Promise<string | null> => {
    try {
      const response = await createChatSession();
      const newId = response.chat_session.id;
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ chatSessions: sessionsResp.chat_sessions ?? [] }));
      return newId;
    } catch (error) {
      debugLog('[chat] Failed to create chat session:', error);
      const message = error instanceof Error ? error.message : 'Failed to create new chat';
      setState((prev) => ({ lastError: message }));
      return null;
    }
  }, [setState]);

  const handleDeleteChat = useCallback(
    async (id: string) => {
      try {
        await deleteChatSession(id);
        if (id === activeChatIdRef.current) {
          const sessionsResp = await listChatSessions();
          if (sessionsResp.chat_sessions.length > 0) {
            await handleActiveChatChange(sessionsResp.active_chat_id);
          } else {
            setState((prev) => ({ chatSessions: [], activeChatId: null, messages: [] }));
          }
        } else {
          const sessionsResp = await listChatSessions();
          setState((prev) => ({ chatSessions: sessionsResp.chat_sessions ?? [] }));
        }
      } catch (error) {
        debugLog('[chat] Failed to delete chat session:', error);
      }
    },
    [handleActiveChatChange, setState],
  );

  const handleRenameChat = useCallback(
    async (id: string, name: string) => {
      try {
        await renameChatSession(id, name);
        const sessionsResp = await listChatSessions();
        setState((prev) => ({ chatSessions: sessionsResp.chat_sessions ?? [] }));
      } catch (error) {
        debugLog('[chat] Failed to rename chat session:', error);
      }
    },
    [setState],
  );

  const handleSendMessage = useCallback(
    async (message: string, options?: { allowConcurrent?: boolean; chatId?: string }) => {
      if (!message.trim()) return;

      // Compile-time short-circuit (R-4): in a --native-chat dist the shell
      // provides the chat loop natively, so the webui never wires up the
      // webui chat session loop (no /api/query submission, no network). Dead
      // branch in the default build (flag off → today's exact behavior,
      // byte-identical).
      if (NATIVE_CHAT_ENABLED) {
        setState((prev) => ({ inputValue: '' }));
        return;
      }

      const trimmedMessage = message.trim();
      const isClearCommand = trimmedMessage.toLowerCase() === '/clear';
      const allowConcurrent = options?.allowConcurrent === true;
      // Queue drain targets the chat the entry was queued for; the active
      // chat's ID is only the fallback for direct (typed) sends.
      const targetChatId = options?.chatId ?? activeChatIdRef.current ?? undefined;

      // Intercept the /model and /provider slash commands client-side and
      // route them to the equivalent WebUI affordances (model picker
      // modal, settings panel focus). The backend's CLI command registry
      // writes its output to stdout (via fmt.Printf) which never reaches
      // the browser — so handing those off to native UI gives users the
      // result they expect instead of an empty "Executed command" echo.
      const lc = trimmedMessage.toLowerCase();
      if (lc === '/model' || lc === '/model select' || lc.startsWith('/model ')) {
        setState((prev) => ({
          ...prev,
          modelSelectionRequest: { provider: prev.provider },
        }));
        setState((prev) => ({ inputValue: '' }));
        return;
      }
      if (lc === '/provider' || lc.startsWith('/provider ')) {
        window.dispatchEvent(new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }));
        setState((prev) => ({ inputValue: '' }));
        return;
      }

      if (isClearCommand && !allowConcurrent && activeRequestsRef.current > 0) {
        try {
          await apiService.stopQuery();
        } catch (error) {
          debugLog('[chat] stopQuery failed during /clear recovery:', error);
        }

        activeRequestsRef.current = 0;
        queuedMessagesRef.current = [];
        setQueuedMessages([]);
        setQueuedMessagesCount(0);

        setState((prev) => ({
          isProcessing: false,
          lastError: null,
          queryProgress: null,
          messages: [],
          toolExecutions: [],
          fileEdits: [],
          subagentActivities: [],
          currentTodos: [],
        }));

        try {
          await apiService.sendQuery('/clear', targetChatId);
        } catch (error) {
          const errorMsg = error instanceof Error ? error.message : 'Failed to send clear command';
          setState((prev) => ({
            lastError: errorMsg,
            messages: [
              ...prev.messages,
              {
                id: generateMessageId(),
                type: 'assistant',
                content: `[FAIL] Error: ${errorMsg}`,
                timestamp: new Date(),
              },
            ],
          }));
        }

        setState((prev) => ({ inputValue: '' }));
        return;
      }

      if (!allowConcurrent && activeRequestsRef.current > 0) {
        const bubbleId = generateMessageId();
        setState((prev) => ({
          lastError: null,
          messages: trimMessages([
            ...prev.messages,
            {
              id: bubbleId,
              type: 'user',
              content: trimmedMessage,
              timestamp: new Date(),
            },
          ]),
        }));
        await apiService.steerQuery(trimmedMessage, targetChatId);
        // Remember the steer for possible retraction via Up-arrow.
        lastSteerMessageRef.current = trimmedMessage;
        lastSteerBubbleIdRef.current = bubbleId;
        setState((prev) => ({ inputValue: '' }));
        return;
      }

      activeRequestsRef.current += 1;

      setState((prev) => ({
        isProcessing: true,
        lastError: null,
        messages: trimMessages([
          ...prev.messages,
          {
            id: generateMessageId(),
            type: 'user',
            content: trimmedMessage,
            timestamp: new Date(),
          },
        ]),
      }));

      try {
        debugLog('[>>] Sending message:', trimmedMessage);
        await apiService.sendQuery(trimmedMessage, targetChatId);
        setState((prev) => ({ inputValue: '' }));
        debugLog('[OK] Message sent successfully');
      } catch (error) {
        // query_in_progress: the backend still has a query running for this
        // chat but our local counter says otherwise (counter desync — e.g. the
        // WebSocket flapped mid-query, or a completion event was missed).
        // The backend is authoritative: convert this into a steer so the
        // input reaches the running query instead of dead-ending as an error.
        if (error instanceof Error && (error as Error & { code?: string }).code === 'query_in_progress') {
          debugLog('[chat] backend reports query in progress — steering instead');
          // Counter was already incremented before the send attempt and the
          // backend query is still active — keep it at 1, don't double-count.
          setState((prev) => ({ isProcessing: true, lastError: null }));
          try {
            await apiService.steerQuery(trimmedMessage, targetChatId);
            lastSteerMessageRef.current = trimmedMessage;
            setState((prev) => ({ inputValue: '' }));
            return;
          } catch (steerErr) {
            if (activeRequestsRef.current > 0) {
              activeRequestsRef.current -= 1;
            }
            const steerMsg = steerErr instanceof Error ? steerErr.message : String(steerErr);
            setState((prev) => ({
              isProcessing: false,
              lastError: `Failed to steer active query: ${steerMsg}`,
              messages: trimMessages([
                ...prev.messages,
                {
                  id: generateMessageId(),
                  type: 'assistant',
                  content: `[FAIL] Error: ${steerMsg}`,
                  timestamp: new Date(),
                },
              ]),
            }));
            return;
          }
        }
        console.error('[FAIL] Failed to send message:', error);
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const errorMsg = error instanceof Error ? error.message : 'Failed to send message';
        setState((prev) => ({
          isProcessing: activeRequestsRef.current > 0,
          lastError: `Failed to send message: ${errorMsg}`,
          messages: trimMessages([
            ...prev.messages,
            {
              id: generateMessageId(),
              type: 'assistant',
              content: `[FAIL] Error: ${errorMsg}`,
              timestamp: new Date(),
            },
          ]),
        }));
      }
    },
    [apiService, activeRequestsRef, activeChatIdRef, queuedMessagesRef, setQueuedMessagesCount],
  );

  const handleQueueMessage = useCallback(
    (message: string) => {
      const trimmed = message.trim();
      if (!trimmed) return;
      queuedMessagesRef.current.push({
        message: trimmed,
        // Tag with the chat the user was viewing when queueing so the
        // drain can't fire it into a different conversation after a
        // chat switch.
        chatId: activeChatIdRef.current,
      });
      setQueuedMessages([...queuedMessagesRef.current]);
      setQueuedMessagesCount(queuedMessagesRef.current.length);
    },
    [activeChatIdRef],
  );

  const handleRemoveQueuedMessage = useCallback((index: number) => {
    const next = [...queuedMessagesRef.current];
    if (index < 0 || index >= next.length) return;
    next.splice(index, 1);
    queuedMessagesRef.current = next;
    setQueuedMessages(next);
    setQueuedMessagesCount(next.length);
  }, []);

  const handleEditQueuedMessage = useCallback((index: number, newText: string) => {
    const next = [...queuedMessagesRef.current];
    const trimmed = newText.trim();
    if (index < 0 || index >= next.length || !trimmed) return;
    next[index] = { ...next[index], message: trimmed };
    queuedMessagesRef.current = next;
    setQueuedMessages(next);
    setQueuedMessagesCount(next.length);
  }, []);

  const handleReorderQueuedMessages = useCallback((fromIndex: number, toIndex: number) => {
    const next = [...queuedMessagesRef.current];
    if (fromIndex < 0 || fromIndex >= next.length) return;
    if (toIndex < 0 || toIndex >= next.length) return;
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    queuedMessagesRef.current = next;
    setQueuedMessages(next);
    setQueuedMessagesCount(next.length);
  }, []);

  const handleClearQueuedMessages = useCallback(() => {
    queuedMessagesRef.current = [];
    setQueuedMessages([]);
    setQueuedMessagesCount(0);
  }, []);

  const handleStopProcessing = useCallback(async () => {
    try {
      await apiService.stopQuery();
      activeRequestsRef.current = 0;
      queuedMessagesRef.current = [];
      setQueuedMessages([]);
      setQueuedMessagesCount(0);
      lastSteerMessageRef.current = '';
      lastSteerBubbleIdRef.current = '';
      setState((prev) => ({
        isProcessing: false,
        queryProgress: null,
        lastError: null,
      }));
    } catch (error) {
      activeRequestsRef.current = 0;
      queuedMessagesRef.current = [];
      setQueuedMessages([]);
      setQueuedMessagesCount(0);
      lastSteerMessageRef.current = '';
      lastSteerBubbleIdRef.current = '';
      const errorMsg = error instanceof Error ? error.message : 'Failed to stop query';
      setState((prev) => ({
        isProcessing: false,
        queryProgress: null,
        lastError: errorMsg,
        messages: trimMessages([
          ...prev.messages,
          {
            id: generateMessageId(),
            type: 'assistant',
            content: `[FAIL] Error: ${errorMsg}`,
            timestamp: new Date(),
          },
        ]),
      }));
    }
  }, [apiService, setQueuedMessages, setQueuedMessagesCount]);

  // Pull back the newest un-picked steer message for editing. Removes the
  // optimistic bubble and restores the text to the input. Returns false when
  // nothing is retractable (already picked up, or none sent). Refs are only
  // cleared on success so a transient API failure can be retried.
  const retractInFlightRef = useRef(false);
  const handleRetractSteer = useCallback(async (): Promise<boolean> => {
    if (!lastSteerMessageRef.current || retractInFlightRef.current) {
      return false;
    }
    retractInFlightRef.current = true;
    const bubbleId = lastSteerBubbleIdRef.current;
    try {
      const response = await apiService.retractSteer(activeChatIdRef.current ?? undefined);
      if (!response.success || !response.message) {
        retractInFlightRef.current = false;
        return false;
      }
      lastSteerMessageRef.current = '';
      lastSteerBubbleIdRef.current = '';
      retractInFlightRef.current = false;
      setState((prev) => ({
        messages: prev.messages.filter((m) => m.id !== bubbleId),
        inputValue: response.message,
      }));
      return true;
    } catch {
      retractInFlightRef.current = false;
      return false;
    }
  }, [apiService]);

  // Handle session-restored window event
  useEffect(() => {
    const handleSessionRestored = (event: Event) => {
      const customEvent = event as CustomEvent<{ messages: Array<{ role: string; content: string }> }>;
      const rawMessages = customEvent.detail?.messages;
      if (!Array.isArray(rawMessages)) return;

      const restoredMessages: Message[] = rawMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => {
          const content = typeof m.content === 'string' ? m.content : '';
          const toolRefs = m.role === 'assistant' ? extractToolRefsFromContent(content) : undefined;
          return {
            id: `restored-${i}`,
            type: m.role as 'user' | 'assistant',
            content,
            timestamp: new Date(),
            ...(toolRefs && toolRefs.length > 0 ? { toolRefs } : {}),
          };
        });

      if (restoredMessages.length > 0) {
        setState((prev) => ({
          messages: trimMessages(restoredMessages),
          toolExecutions: [],
          fileEdits: [],
          subagentActivities: [],
          currentTodos: [],
          queryProgress: null,
          lastError: null,
          isProcessing: false,
        }));
      }
    };

    window.addEventListener('sprout:session-restored', handleSessionRestored);
    return () => window.removeEventListener('sprout:session-restored', handleSessionRestored);
  }, [setState]);

  // Drain queued messages when not processing. Entries are tagged with the
  // chat they were queued for; only entries for the currently-viewed (idle)
  // chat drain — a queued-for-chat-B message must not fire into chat A after
  // a switch (its optimistic bubble and response belong to B's transcript).
  // Entries for other chats wait until the user switches back and that chat
  // is idle.
  useEffect(() => {
    if (isProcessing || activeRequestsRef.current > 0) {
      return;
    }
    const activeChat = activeChatIdRef.current;
    const headIdx = queuedMessagesRef.current.findIndex(
      (entry) => entry.chatId === null || entry.chatId === activeChat,
    );
    if (headIdx < 0) {
      return;
    }
    const [entry] = queuedMessagesRef.current.splice(headIdx, 1);
    setQueuedMessages([...queuedMessagesRef.current]);
    setQueuedMessagesCount(queuedMessagesRef.current.length);
    if (!entry) return;

    handleSendMessage(entry.message, { chatId: entry.chatId ?? undefined }).catch((error) => {
      const errorMsg = error instanceof Error ? error.message : 'Failed to send queued message';
      setState((prev) => ({
        lastError: `Failed to send queued message: ${errorMsg}`,
        messages: trimMessages([
          ...prev.messages,
          {
            id: generateMessageId(),
            type: 'assistant',
            content: `[FAIL] Error: ${errorMsg}`,
            timestamp: new Date(),
          },
        ]),
      }));
    });
    // activeChatIdRef is a ref; isProcessing/queuedMessagesCount drive re-runs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isProcessing, handleSendMessage, queuedMessagesCount, activeChatIdRef.current]);

  // Reload the active chat's authoritative history from the backend. Triggered
  // when a reconnect reports a gap (the server's run buffer had already evicted
  // events this client missed), so we replace the possibly-corrupted local
  // transcript with the server's instead of splicing a partial replay onto it.
  // Re-switching to the already-active chat is a safe, idempotent fetch.
  const reloadActiveChatFromBackend = useCallback(
    async (id: string) => {
      try {
        const response = await switchChatSession(id);
        if (activeChatIdRef.current !== id) return; // user moved on while loading
        const backendMessages: Message[] = (response.chat_session.messages ?? [])
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => ({
            id: `chat-${id}-${i}`,
            type: m.role as 'user' | 'assistant',
            content: typeof m.content === 'string' ? m.content : '',
            timestamp: new Date(),
            ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
          }));
        const backendIsActive = response.chat_session.active_query;
        activeRequestsRef.current = backendIsActive ? 1 : 0;
        setState(() => ({
          messages: trimMessages(backendMessages),
          isProcessing: backendIsActive,
        }));
      } catch (error) {
        debugLog('[chat] gap reload failed:', error);
      }
    },
    [setState, activeChatIdRef, activeRequestsRef],
  );

  useEffect(() => {
    const onGapReload = (e: Event) => {
      const id = (e as CustomEvent<{ chatId?: string }>).detail?.chatId || activeChatIdRef.current;
      if (!id || (activeChatIdRef.current && activeChatIdRef.current !== id)) return;
      void reloadActiveChatFromBackend(id);
    };
    window.addEventListener('sprout:chat-gap-reload', onGapReload);
    return () => window.removeEventListener('sprout:chat-gap-reload', onGapReload);
  }, [reloadActiveChatFromBackend, activeChatIdRef]);

  // Refresh the chat session list sidebar. Triggered after a fork so the
  // user sees the updated session metadata.
  useEffect(() => {
    const onRefresh = () => void loadChatSessions();
    window.addEventListener('sprout:refresh-sessions', onRefresh);
    return () => window.removeEventListener('sprout:refresh-sessions', onRefresh);
  }, [loadChatSessions]);

  return {
    loadChatSessions,
    handleActiveChatChange,
    handleCreateChat,
    handleDeleteChat,
    handleRenameChat,
    handleSendMessage,
    handleQueueMessage,
    handleRemoveQueuedMessage,
    handleEditQueuedMessage,
    handleReorderQueuedMessages,
    handleClearQueuedMessages,
    handleStopProcessing,
    handleRetractSteer,
    queuedMessages,
    queuedMessagesCount,
    setQueuedMessagesCount,
  };
}
