/**
 * Chat session CRUD operations.
 *
 * Manages loading, switching, creating, deleting, and renaming chat sessions.
 * Depends on refs from useWebSocketEvents for per-chat event filtering
 * during async operations.
 */

import { useCallback } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppState, Message } from '../types/app';
import {
  listChatSessions,
  createChatSession,
  deleteChatSession,
  deleteAllChatSessions,
  renameChatSession,
  switchChatSession,
  getChatSessionWorktree,
  setChatSessionWorktree,
  switchChatSessionWorktree,
  listWorktrees,
  createWorktree,
  createChatSessionInWorktree,
} from '../services/chatSessions';
import { debugLog } from '../utils/log';

export interface UseChatSessionsOptions {
  setState: Dispatch<SetStateAction<AppState>>;
  activeChatIdRef: MutableRefObject<string | null>;
  activeRequestsRef: MutableRefObject<number>;
}

export interface UseChatSessionsReturn {
  loadChatSessions: () => Promise<void>;
  handleActiveChatChange: (id: string) => Promise<void>;
  handleCreateChat: () => Promise<string | null>;
  handleDeleteChat: (id: string, options?: { removeWorktree?: boolean }) => Promise<void>;
  handleDeleteAllChats: () => Promise<void>;
  handleRenameChat: (id: string, name: string) => Promise<void>;
  // Worktree operations
  getChatSessionWorktree: (chatId: string) => Promise<string>;
  setChatSessionWorktree: (chatId: string, worktreePath: string) => Promise<void>;
  switchChatSessionWorktree: (chatId: string, worktreePath: string) => Promise<void>;
  listWorktrees: () => Promise<Array<{ path: string; branch: string; is_main: boolean; is_current: boolean }>>;
  createWorktree: (path: string, branch: string, baseRef?: string) => Promise<void>;
  createChatInWorktree: (branch: string, baseRef?: string, name?: string, autoSwitch?: boolean) => Promise<string | null>;
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
      const session = prev.chatSessions.find((s) => s.id === currentId);
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
              provider: prev.provider,
              model: prev.model,
              worktreePath: session?.worktree_path,
              queryCount: prev.queryCount,
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
        provider: cached?.provider ?? prev.provider,
        model: cached?.model ?? prev.model,
        queryCount: cached?.queryCount ?? prev.queryCount,
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
        const backendProvider = (response.chat_session as Record<string, unknown>).provider as string | undefined;
        const backendModel = (response.chat_session as Record<string, unknown>).model as string | undefined;
        return {
          ...prev,
          activeChatId: response.active_chat_id,
          messages: useBackendMessages ? backendMessages : prev.messages,
          isProcessing: finalIsProcessing,
          ...(backendProvider ? { provider: backendProvider } : {}),
          ...(backendModel ? { model: backendModel } : {}),
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
    async (id: string, options?: { removeWorktree?: boolean }) => {
      try {
        await deleteChatSession(id, options?.removeWorktree);
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

  const handleDeleteAllChats = useCallback(async () => {
    try {
      const response = await deleteAllChatSessions();
      // Reload chat sessions after deletion
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      // Switch to the active/default session returned by the API
      await handleActiveChatChange(response.active_chat_id);
    } catch (error) {
      debugLog('[chat] Failed to delete all chat sessions:', error);
      const message = error instanceof Error ? error.message : 'Failed to delete all chat sessions';
      setState((prev) => ({ ...prev, lastError: message }));
      throw error;
    }
  }, [setState, handleActiveChatChange]);

  // Worktree operations
  const fetchChatSessionWorktree = useCallback(async (chatId: string): Promise<string> => {
    try {
      const response = await getChatSessionWorktree(chatId);
      return (response as { worktree_path: string }).worktree_path || '';
    } catch (error) {
      debugLog('[worktree] Failed to get chat session worktree:', error);
      return '';
    }
  }, []);

  const updateChatSessionWorktree = useCallback(async (chatId: string, worktreePath: string) => {
    try {
      await setChatSessionWorktree(chatId, worktreePath);
      // Refresh session list to reflect updated worktree
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
    } catch (error) {
      debugLog('[worktree] Failed to set chat session worktree:', error);
      const message = error instanceof Error ? error.message : 'Failed to set worktree';
      setState((prev) => ({ ...prev, lastError: message }));
      throw error;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const switchChatSessionWorktreeLocal = useCallback(async (chatId: string, worktreePath: string) => {
    try {
      const response = await switchChatSessionWorktree(chatId, worktreePath);
      // Refresh session list to reflect updated worktree
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      // Also update the active chat ID if this is the active chat
      if (chatId === activeChatIdRef.current) {
        const backendMessages: Message[] = (response.chat_session.messages ?? [])
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => ({
            id: `chat-${chatId}-${i}`,
            type: m.role as 'user' | 'assistant',
            content: typeof m.content === 'string' ? m.content : '',
            timestamp: new Date(),
            ...(m.reasoning_content ? { reasoning: m.reasoning_content } : {}),
          }));
        const backendIsActive = !!(response.chat_session as Record<string, unknown>).active_query;
        setState((prev) => ({
          ...prev,
          messages: backendMessages,
          isProcessing: backendIsActive,
        }));
      }
    } catch (error) {
      debugLog('[worktree] Failed to switch chat session worktree:', error);
      const message = error instanceof Error ? error.message : 'Failed to switch worktree';
      setState((prev) => ({ ...prev, lastError: message }));
      throw error;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const fetchWorktrees = useCallback(async (): Promise<Array<{ path: string; branch: string; is_main: boolean; is_current: boolean }>> => {
    try {
      const response = await listWorktrees();
      return (response as { worktrees: Array<{ path: string; branch: string; is_main: boolean; is_current: boolean }> }).worktrees || [];
    } catch (error) {
      debugLog('[worktree] Failed to list worktrees:', error);
      return [];
    }
  }, []);

  const createLocalWorktree = useCallback(async (path: string, branch: string, baseRef?: string) => {
    try {
      await createWorktree(path, branch, baseRef);
      // Refresh worktree list
      await fetchWorktrees();
    } catch (error) {
      debugLog('[worktree] Failed to create worktree:', error);
      const message = error instanceof Error ? error.message : 'Failed to create worktree';
      setState((prev) => ({ ...prev, lastError: message }));
      throw error;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchWorktrees]);

  const createChatInWorktree = useCallback(async (branch: string, baseRef?: string, name?: string, autoSwitch?: boolean): Promise<string | null> => {
    try {
      const response = await createChatSessionInWorktree({ branch, base_ref: baseRef, name, auto_switch_workspace: autoSwitch });
      const newId = (response.chat_session as Record<string, unknown>).id as string;
      const sessionsResp = await listChatSessions();
      setState((prev) => ({ ...prev, chatSessions: sessionsResp.chat_sessions }));
      
      // If auto-switch was requested, switch to the new chat
      if (autoSwitch && newId) {
        await handleActiveChatChange(newId);
      }
      
      return newId;
    } catch (error) {
      debugLog('[chat] Failed to create chat in worktree:', error);
      const message = error instanceof Error ? error.message : 'Failed to create chat in worktree';
      setState((prev) => ({ ...prev, lastError: message }));
      throw error;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [handleActiveChatChange]);

  return {
    loadChatSessions,
    handleActiveChatChange,
    handleCreateChat,
    handleDeleteChat,
    handleDeleteAllChats,
    handleRenameChat,
    // Worktree operations
    getChatSessionWorktree: fetchChatSessionWorktree,
    setChatSessionWorktree: updateChatSessionWorktree,
    switchChatSessionWorktree: switchChatSessionWorktreeLocal,
    listWorktrees: fetchWorktrees,
    createWorktree: createLocalWorktree,
    createChatInWorktree,
  };
}
