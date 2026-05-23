import type { Message } from '@sprout/ui';
import { useCallback, useEffect, useState } from 'react';
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

export interface UseChatSessionManagerParams {
  setState: AppStoreSetState;
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  queuedMessagesRef: React.MutableRefObject<string[]>;
  setInputValue: React.Dispatch<React.SetStateAction<string>>;
  isProcessing: boolean;
}

export interface UseChatSessionManagerReturn {
  loadChatSessions: () => Promise<void>;
  handleActiveChatChange: (id: string) => Promise<void>;
  handleCreateChat: () => Promise<string | null>;
  handleDeleteChat: (id: string) => Promise<void>;
  handleRenameChat: (id: string, name: string) => Promise<void>;
  handleSendMessage: (message: string, options?: { allowConcurrent?: boolean }) => Promise<void>;
  handleQueueMessage: (message: string) => void;
  handleStopProcessing: () => Promise<void>;
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
  setInputValue,
  isProcessing,
}: UseChatSessionManagerParams): UseChatSessionManagerReturn {
  const [queuedMessagesCount, setQueuedMessagesCount] = useState(0);
  const apiService = ApiService.getInstance();

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
        chatSessions: response.chat_sessions,
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
        const restoredIsProcessing = cached?.isProcessing ?? false;
        activeRequestsRef.current = restoredIsProcessing ? 1 : 0;
        return {
          activeChatId: id,
          messages: cached?.messages ?? [],
          isProcessing: restoredIsProcessing,
          toolExecutions: cached?.toolExecutions ?? [],
          fileEdits: cached?.fileEdits ?? [],
          subagentActivities: cached?.subagentActivities ?? [],
          currentTodos: cached?.currentTodos ?? [],
          queryProgress: cached?.queryProgress ?? null,
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
          const useBackendMessages = backendMessages.length >= prev.messages.length;
          const finalIsProcessing = backendIsActive;
          activeRequestsRef.current = finalIsProcessing ? 1 : 0;
          return {
            activeChatId: response.active_chat_id,
            messages: useBackendMessages ? trimMessages(backendMessages) : prev.messages,
            isProcessing: finalIsProcessing,
          };
        });

        const sessionsResp = await listChatSessions();
        if (activeChatIdRef.current !== switchId) return;
        setState((prev) => ({ chatSessions: sessionsResp.chat_sessions }));
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
      setState((prev) => ({ chatSessions: sessionsResp.chat_sessions }));
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
          setState((prev) => ({ chatSessions: sessionsResp.chat_sessions }));
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
        setState((prev) => ({ chatSessions: sessionsResp.chat_sessions }));
      } catch (error) {
        debugLog('[chat] Failed to rename chat session:', error);
      }
    },
    [setState],
  );

  const handleSendMessage = useCallback(
    async (message: string, options?: { allowConcurrent?: boolean }) => {
      if (!message.trim()) return;
      const trimmedMessage = message.trim();
      const isClearCommand = trimmedMessage.toLowerCase() === '/clear';
      const allowConcurrent = options?.allowConcurrent === true;

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
        setInputValue('');
        return;
      }
      if (lc === '/provider' || lc.startsWith('/provider ')) {
        window.dispatchEvent(
          new CustomEvent('sprout:open-settings-focus', { detail: { focus: 'provider' } }),
        );
        setInputValue('');
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
          await apiService.sendQuery('/clear', activeChatIdRef.current ?? undefined);
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

        setInputValue('');
        return;
      }

      if (!allowConcurrent && activeRequestsRef.current > 0) {
        setState((prev) => ({
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
        await apiService.steerQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
        setInputValue('');
        return;
      }

      activeRequestsRef.current += 1;

      setState((prev) => ({
        isProcessing: true,
        lastError: null,
      }));

      try {
        debugLog('[>>] Sending message:', trimmedMessage);
        await apiService.sendQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
        setInputValue('');
        debugLog('[OK] Message sent successfully');
      } catch (error) {
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
    [apiService, activeRequestsRef, activeChatIdRef, queuedMessagesRef, setInputValue, setQueuedMessagesCount],
  );

  const handleQueueMessage = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) return;
    queuedMessagesRef.current.push(trimmed);
    setQueuedMessagesCount(queuedMessagesRef.current.length);
  }, []);

  const handleStopProcessing = useCallback(async () => {
    try {
      await apiService.stopQuery();
      activeRequestsRef.current = 0;
      queuedMessagesRef.current = [];
      setQueuedMessagesCount(0);
      setState((prev) => ({
        isProcessing: false,
        queryProgress: null,
        lastError: null,
      }));
    } catch (error) {
      activeRequestsRef.current = 0;
      queuedMessagesRef.current = [];
      setQueuedMessagesCount(0);
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
  }, [apiService, setQueuedMessagesCount]);

  // Handle session-restored window event
  useEffect(() => {
    const handleSessionRestored = (event: Event) => {
      const customEvent = event as CustomEvent<{ messages: Array<{ role: string; content: string }> }>;
      const rawMessages = customEvent.detail?.messages;
      if (!Array.isArray(rawMessages)) return;

      const restoredMessages: Message[] = rawMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `restored-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date(),
        }));

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

  // Drain queued messages when not processing
  useEffect(() => {
    if (isProcessing || activeRequestsRef.current > 0) {
      return;
    }
    if (queuedMessagesRef.current.length === 0) {
      return;
    }

    const next = queuedMessagesRef.current.shift();
    setQueuedMessagesCount(queuedMessagesRef.current.length);
    if (!next) return;

    handleSendMessage(next).catch((error) => {
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
  }, [isProcessing, handleSendMessage, queuedMessagesCount]);

  return {
    loadChatSessions,
    handleActiveChatChange,
    handleCreateChat,
    handleDeleteChat,
    handleRenameChat,
    handleSendMessage,
    handleQueueMessage,
    handleStopProcessing,
    queuedMessagesCount,
    setQueuedMessagesCount,
  };
}
