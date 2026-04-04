/**
 * Chat session CRUD operations.
 *
 * Manages loading, switching, creating, deleting, and renaming chat sessions.
 * Depends on refs from useWebSocketEvents for per-chat event filtering
 * during async operations.
 */

import { useCallback } from 'react';
import type { AppState, Message } from '../types/app';
import {
  listChatSessions,
  createChatSession,
  deleteChatSession,
  renameChatSession,
  switchChatSession,
} from '../services/chatSessions';
import { debugLog } from '../utils/log';

export interface UseChatSessionsOptions {
  setState: React.Dispatch<React.SetStateAction<AppState>>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  activeRequestsRef: React.MutableRefObject<number>;
}

export interface UseChatSessionsReturn {
  loadChatSessions: () => Promise<void>;
  handleActiveChatChange: (id: string) => Promise<void>;
  handleCreateChat: () => Promise<string | null>;
  handleDeleteChat: (id: string) => Promise<void>;
  handleRenameChat: (id: string, name: string) => Promise<void>;
}

export function useChatSessions({
  setState,
  activeChatIdRef,
  activeRequestsRef,
}: UseChatSessionsOptions): UseChatSessionsReturn {
  const loadChatSessions = useCallback(async () => {
    try {
      const response = await listChatSessions();
      const activeChatId = response.active_chat_id || null;
      // Load message history for the currently active chat so history shows on first load
      let initialMessages: Message[] = [];
      if (activeChatId) {
        try {
          const switchResp = await switchChatSession(activeChatId);
          initialMessages = switchResp.chat_session.messages
            .filter((m) => m.role === 'user' || m.role === 'assistant')
            .map((m, i) => ({
              id: `chat-${activeChatId}-${i}`,
              type: m.role as 'user' | 'assistant',
              content: typeof m.content === 'string' ? m.content : '',
              timestamp: new Date(),
              ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
            }));
          // Eagerly update the ref in case this is the initial load
          if (!activeChatIdRef.current) {
            activeChatIdRef.current = activeChatId;
          }
        } catch (e) {
          debugLog('[chat] Failed to load initial messages:', e);
        }
      }
      setState((prev) => ({
        ...prev,
        chatSessions: response.chat_sessions,
        activeChatId: prev.activeChatId || activeChatId,
        // Only set initial messages if we have none yet (don't clobber live messages)
        messages: prev.messages.length === 0 && initialMessages.length > 0 ? initialMessages : prev.messages,
      }));
    } catch (error) {
      debugLog('[chat] Failed to load chat sessions:', error);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleActiveChatChange = useCallback(async (id: string) => {
    const currentId = activeChatIdRef.current;
    if (currentId === id) return; // Already active

    // Update the ref immediately so WebSocket events for the new chat are
    // accepted and events for the old chat are filtered during the switch.
    activeChatIdRef.current = id;

    // Phase 1: instant UI update from cache — no blank flash while API loads.
    // Saves the outgoing chat's full state (including messages) and restores
    // the incoming chat's previously-cached state atomically.
    setState((prev) => {
      const cached = prev.perChatCache[id];
      const newCache = currentId
        ? {
            ...prev.perChatCache,
            [currentId]: {
              messages: prev.messages,
              toolExecutions: prev.toolExecutions,
              fileEdits: prev.fileEdits,
              subagentActivities: prev.subagentActivities,
              currentTodos: prev.currentTodos,
              queryProgress: prev.queryProgress,
              lastError: prev.lastError,
              isProcessing: prev.isProcessing,
            },
          }
        : prev.perChatCache;
      const restoredIsProcessing = cached?.isProcessing ?? false;
      // Sync the requests counter with cached processing state so the Chat
      // input/stop-button reflects the correct state without waiting for the
      // backend response.
      activeRequestsRef.current = restoredIsProcessing ? 1 : 0;
      return {
        ...prev,
        // Use id optimistically — overwritten by response.active_chat_id below
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
      const backendMessages: Message[] = (response.chat_session.messages ?? [])
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `chat-${id}-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date(),
          ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
        }));

      // Use backend active_query as the authoritative source of truth for
      // isProcessing — it knows whether the query actually completed, even
      // if we missed the query_completed WebSocket event while on another chat.
      const backendIsActive = !!(response.chat_session as Record<string, unknown>).active_query;

      setState((prev) => {
        // Only update messages if backend has equal or more messages than what's
        // already shown from cache. If backend has fewer (query still in flight
        // so AgentState not yet synced), keep the cache messages which include
        // the optimistically-added user message and any streamed chunks.
        const useBackendMessages = backendMessages.length >= prev.messages.length;
        const finalIsProcessing = backendIsActive;
        activeRequestsRef.current = finalIsProcessing ? 1 : 0;
        return {
          ...prev,
          activeChatId: response.active_chat_id,
          messages: useBackendMessages ? backendMessages : prev.messages,
          isProcessing: finalIsProcessing,
        };
      });

      // Refresh session list to reflect updated active state
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
    } catch (error) {
      // Rollback the eagerly-updated ref so subsequent switches aren't confused
      activeChatIdRef.current = currentId;
      debugLog('[chat] Failed to switch chat session:', error);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleCreateChat = useCallback(async (): Promise<string | null> => {
    try {
      const response = await createChatSession();
      const newId = response.chat_session.id;
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      return newId;
    } catch (error) {
      debugLog('[chat] Failed to create chat session:', error);
      const message = error instanceof Error ? error.message : 'Failed to create new chat';
      setState((prev) => ({ ...prev, lastError: message }));
      return null;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleDeleteChat = useCallback(
    async (id: string) => {
      try {
        await deleteChatSession(id);
        if (id === activeChatIdRef.current) {
          const sessionsResp = await listChatSessions();
          if (sessionsResp.chat_sessions.length > 0) {
            // Switch to the active session (deprecated alias kept for useChatSessions internal use).
            // We reference handleActiveChatChange through a local alias to avoid circular deps.
            await handleActiveChatChange(sessionsResp.active_chat_id);
          } else {
            setState((prev) => ({ ...prev, chatSessions: [], activeChatId: null, messages: [] }));
          }
        } else {
          const sessionsResp = await listChatSessions();
          setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
        }
      } catch (error) {
        debugLog('[chat] Failed to delete chat session:', error);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [handleActiveChatChange],
  );

  const handleRenameChat = useCallback(async (id: string, name: string) => {
    try {
      await renameChatSession(id, name);
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
    } catch (error) {
      debugLog('[chat] Failed to rename chat session:', error);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    loadChatSessions,
    handleActiveChatChange,
    handleCreateChat,
    handleDeleteChat,
    handleRenameChat,
  };
}
