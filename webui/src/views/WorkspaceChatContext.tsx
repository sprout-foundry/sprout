/**
 * WorkspaceChatProvider + useWorkspaceChat — the chat slice as one unit.
 *
 * The chat surface is the most stateful part of the workspace: the transcript
 * and its per-chat caches, the WebSocket event reducer that fills them, the
 * chat-session manager (list/create/switch/delete/rename), the send/queue
 * operations, and the `chatProps`/`reviewProps`/`diffState` the view renders
 * from. In the standalone app that state was spread across `App.tsx`,
 * `AppContent.tsx` and a handful of hooks, so a host that mounted the chat
 * view had no way to give it data.
 *
 * This module collects it behind one provider and one hook. The provider
 * takes the two things a chat needs to talk to a backend — a `fetch` function
 * and an events provider — and owns everything else: the store, the event
 * reducer, the session manager and the queue. The standalone app mounts it
 * with its own `clientFetch` and `LocalEventsProvider`, so its behaviour is
 * unchanged; an embedding mounts it with its host's transport.
 *
 * The app-specific callbacks the chat view needs (opening a review buffer,
 * the model picker, session restore, fork) are not part of the chat state, so
 * they arrive as the `overrides` of `useWorkspaceChatProps`, which is where
 * the view props are assembled. That keeps the assembly in this unit while
 * the app keeps owning its own surfaces.
 */

import type { EventsProvider, WsEvent } from '@sprout/events';
import type { ChatProps, FileEdit, ToolExecution } from '@sprout/ui';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, type ReactNode } from 'react';
import { AppStoreProvider, createAppStore, useAppStoreSetState, useAppStoreState } from '../contexts/AppStore';
import type { AppStoreSetState } from '../contexts/AppStore';
import { useBackgroundChatSync } from '../hooks/useBackgroundChatSync';
import { useChatSessionManager } from '../hooks/useChatSessionManager';
import type { QueuedMessage, UseChatSessionManagerReturn } from '../hooks/useChatSessionManager';
import { useCurrentTodos } from '../hooks/useCurrentTodos';
import { useWebSocketEventHandler } from '../hooks/useWebSocketEventHandler';
import type { UseWebSocketEventHandlerRefs } from '../hooks/useWebSocketEventHandler';
import { ApiService } from '../services/api';
import { retractSteer, sendQuery, steerQuery, stopQuery } from '../services/api/chatApi';
import { createChatSessionsApi } from '../services/chatSessions';
import { clientFetch } from '../services/clientSession';
import type { AppState } from '../types/app';
import type { WorkspaceShellChat } from '../workspaces/shell';

// ── The context ──────────────────────────────────────────────────────────

/**
 * What `useWorkspaceChat()` exposes: the chat state, its store setter, the
 * event handlers and the session/queue operations. A host that wants to
 * compose its own chat surface reads these directly; the assembled view props
 * come from `useWorkspaceChatProps`.
 */
export interface WorkspaceChatValue {
  /** The chat slice of the app state (transcript, caches, sessions, …). */
  state: AppState;
  setState: AppStoreSetState;
  /** Feed a WebSocket event into the chat reducer. */
  handleEvent: (event: WsEvent) => void;
  /** Re-sync chat state after a reconnect. */
  handleReconnect: () => void;
  /** The session manager: list/create/switch/delete/rename and send/queue. */
  chat: UseChatSessionManagerReturn;
  /** Refs the app shares with its own handlers (provider-change tracking). */
  refs: WorkspaceChatRefs;
}

/**
 * The refs the chat unit owns. `pendingProviderRef` is shared with the app's
 * model/provider handlers, so it is exposed rather than kept private.
 */
export interface WorkspaceChatRefs {
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  queuedMessagesRef: React.MutableRefObject<QueuedMessage[]>;
  pendingProviderRef: React.MutableRefObject<string>;
  pendingProviderChangeRef: React.MutableRefObject<boolean>;
  pendingProviderChangeValueRef: React.MutableRefObject<string | null>;
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
  lastConnectionStateRef: React.MutableRefObject<boolean>;
}

const WorkspaceChatContext = createContext<WorkspaceChatValue | null>(null);

export interface WorkspaceChatProviderProps {
  /** The initial app state the chat store starts from. */
  initialState: AppState;
  /** The events transport the chat reducer subscribes to. */
  eventsProvider: EventsProvider;
  /**
   * The fetch every chat call goes through. Omitted, the app's
   * adapter-aware `clientFetch`; an embedding supplies its own so sends and
   * session calls reach its backend.
   */
  fetchFn?: typeof fetch;
  children: ReactNode;
}

/**
 * Mount the chat unit. Owns the app store (so the app's other consumers keep
 * reading it through the same context), the WebSocket event reducer, the chat
 * session manager and the queue, and subscribes the events provider to the
 * reducer.
 */
export function WorkspaceChatProvider({
  initialState,
  eventsProvider,
  fetchFn = clientFetch,
  children,
}: WorkspaceChatProviderProps): JSX.Element {
  const store = useMemo(() => createAppStore(initialState), [initialState]);
  return (
    <AppStoreProvider store={store}>
      <WorkspaceChatRuntime eventsProvider={eventsProvider} fetchFn={fetchFn}>
        {children}
      </WorkspaceChatRuntime>
    </AppStoreProvider>
  );
}

/**
 * The runtime below the store: it reads the store and wires the chat hooks.
 * Split from the provider so the hooks run inside the `AppStoreProvider` they
 * depend on.
 */
function WorkspaceChatRuntime({
  eventsProvider,
  fetchFn,
  children,
}: {
  eventsProvider: EventsProvider;
  fetchFn: typeof fetch;
  children: ReactNode;
}): JSX.Element {
  const state = useAppStoreState();
  const setState = useAppStoreSetState();
  const apiService = useMemo(() => ApiService.getInstance(), []);

  const activeRequestsRef = useRef(0);
  const activeChatIdRef = useRef<string | null>(null);
  activeChatIdRef.current = state.activeChatId;
  const queuedMessagesRef = useRef<QueuedMessage[]>([]);
  const pendingProviderRef = useRef('');
  const pendingProviderChangeRef = useRef(false);
  const pendingProviderChangeValueRef = useRef<string | null>(null);
  const connectionTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastConnectionStateRef = useRef(false);

  const refs: UseWebSocketEventHandlerRefs = {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
  };

  const { handleEvent, handleReconnect } = useWebSocketEventHandler({
    setState,
    refs,
    apiService,
    fetchFn,
  });

  const chat = useChatSessionManager({
    setState,
    activeRequestsRef,
    activeChatIdRef,
    queuedMessagesRef,
    isProcessing: state.isProcessing,
    workspaceBusy: state.workspaceBusy,
    api: useMemo(
      () => ({
        sendQuery: (query: string, chatId?: string, mode?: string) => sendQuery(fetchFn, query, chatId, mode),
        steerQuery: (query: string, chatId?: string) => steerQuery(fetchFn, query, chatId),
        stopQuery: (chatId?: string) => stopQuery(fetchFn, chatId),
        retractSteer: (chatId?: string) => retractSteer(fetchFn, chatId),
      }),
      [fetchFn],
    ),
    sessions: useMemo(() => createChatSessionsApi(fetchFn), [fetchFn]),
  });

  // Background chat panes refresh from the read-only messages endpoint when
  // their cached entry accumulates events, so both panes stream independently.
  useBackgroundChatSync({
    perChatCache: state.perChatCache,
    activeChatId: state.activeChatId,
    setState,
  });

  // Subscribe the events transport to the chat reducer. The provider owns the
  // subscription so an embedding needs only to hand over its provider; the
  // app's initialization hook does not register it again.
  useSubscribeEvents(eventsProvider, handleEvent, handleReconnect);

  const value = useMemo<WorkspaceChatValue>(
    () => ({
      state,
      setState,
      handleEvent,
      handleReconnect,
      chat,
      refs: {
        activeRequestsRef,
        activeChatIdRef,
        queuedMessagesRef,
        pendingProviderRef,
        pendingProviderChangeRef,
        pendingProviderChangeValueRef,
        connectionTimeoutRef,
        lastConnectionStateRef,
      },
    }),
    [state, setState, handleEvent, handleReconnect, chat],
  );

  return <WorkspaceChatContext.Provider value={value}>{children}</WorkspaceChatContext.Provider>;
}

/**
 * Register the event/reconnect callbacks with the transport and remove them on
 * unmount. The callbacks are read through refs so a re-render does not
 * re-register; only a provider swap does.
 */
function useSubscribeEvents(
  eventsProvider: EventsProvider,
  handleEvent: (event: WsEvent) => void,
  handleReconnect: () => void,
): void {
  const handleEventRef = useRef(handleEvent);
  handleEventRef.current = handleEvent;
  const handleReconnectRef = useRef(handleReconnect);
  handleReconnectRef.current = handleReconnect;

  useEffect(() => {
    const stableEvent = (event: WsEvent) => handleEventRef.current(event);
    const stableReconnect = () => handleReconnectRef.current();
    eventsProvider.onEvent(stableEvent);
    eventsProvider.onReconnect(stableReconnect);
    return () => {
      eventsProvider.removeEvent(stableEvent);
      eventsProvider.onReconnect(null);
    };
  }, [eventsProvider]);
}

/**
 * Read the chat unit. Throws outside `WorkspaceChatProvider` so a mis-mounted
 * chat surface fails loudly rather than rendering an empty transcript.
 */
export function useWorkspaceChat(): WorkspaceChatValue {
  const value = useContext(WorkspaceChatContext);
  if (!value) {
    throw new Error('useWorkspaceChat must be used within a WorkspaceChatProvider');
  }
  return value;
}

// ── View props assembly ──────────────────────────────────────────────────

/** The app-specific inputs to the chat view props that are not chat state. */
export interface WorkspaceChatPropsOverrides {
  /** The tool execution whose inline detail is open (app-owned). */
  activeToolDetail?: ToolExecution | null;
  onToolDetailToggle?: (toolId: string | null) => void;
  /** Optimistic clear for the New Session action. */
  onChatCleared?: () => void;
  /** Open a review buffer for one changed file. */
  onReviewChange?: (path: string, diff: { stats?: string; diff?: string }) => void;
  /** Restore a saved conversation (header history switcher). */
  onRestoreSession?: (sessionId: string, chatId?: string) => void | Promise<void>;
  /** Open the model picker for the active provider. */
  onModelClick?: (provider: string) => void;
  /** Fork the active chat at a breakpoint. */
  onForkAtBreakpoint?: (breakpointIndex: number) => void;
  isForking?: boolean;
  backendReachable?: boolean;
  onRetryConnection?: () => void;
  /** Dismiss the workspace-busy notice. Defaults to clearing it in the store. */
  onDismissBusy?: () => void;
  /** The per-turn change summaries shown in the transcript. */
  fileEdits?: FileEdit[];
  /** The review surface state (owned by the app's git workspace). */
  review?: WorkspaceChatReviewProps;
  /** The diff surface state (owned by the app's git workspace). */
  diff?: WorkspaceChatDiffState;
}

/** The review surface props the chat's change strip drives (the shell's shape). */
export type WorkspaceChatReviewProps = WorkspaceShellChat['reviewProps'];

/** The diff surface state the chat's change strip drives (the shell's shape). */
export type WorkspaceChatDiffState = WorkspaceShellChat['diffState'];

export interface WorkspaceChatViewProps {
  chatProps: ChatProps;
  reviewProps: WorkspaceChatReviewProps;
  diffState: WorkspaceChatDiffState;
}

/**
 * Assemble the props the chat view renders from: the chat state + queue from
 * the unit, and the app-specific callbacks from `overrides`. The queue is
 * mapped from every chat's held-back entries to this chat's, translating the
 * view's indices back to queue positions.
 */
export function useWorkspaceChatProps(overrides: WorkspaceChatPropsOverrides = {}): WorkspaceChatViewProps {
  const { state, setState, chat } = useWorkspaceChat();
  const inputValue = state.inputValue;
  const activeChatId = state.activeChatId;
  const { queuedMessages } = chat;

  const setInputValue = useCallback(
    (updater: React.SetStateAction<string>) => {
      setState((prev) => {
        const nextValue = typeof updater === 'function' ? updater(prev.inputValue) : updater;
        return { inputValue: nextValue };
      });
    },
    [setState],
  );

  const handleDismissBusy = useCallback(() => {
    setState((prev) => ({ ...prev, workspaceBusy: null }));
  }, [setState]);

  const currentTodos = useCurrentTodos(state.currentTodos, state.toolExecutions);

  const chatQueue = useMemo(() => {
    const positions: number[] = [];
    queuedMessages.forEach((entry, i) => {
      if (!entry.chatId || entry.chatId === activeChatId) positions.push(i);
    });
    return {
      messages: positions.map((i) => queuedMessages[i].message),
      remove: (index: number) => {
        if (positions[index] !== undefined) chat.handleRemoveQueuedMessage(positions[index]);
      },
      edit: (index: number, text: string) => {
        if (positions[index] !== undefined) chat.handleEditQueuedMessage(positions[index], text);
      },
      reorder: (from: number, to: number) => {
        if (positions[from] !== undefined && positions[to] !== undefined) {
          chat.handleReorderQueuedMessages(positions[from], positions[to]);
        }
      },
      clear: () => {
        if (positions.length === queuedMessages.length) {
          chat.handleClearQueuedMessages();
          return;
        }
        for (const i of [...positions].reverse()) chat.handleRemoveQueuedMessage(i);
      },
    };
  }, [queuedMessages, activeChatId, chat]);

  const chatProps = useMemo<ChatProps>(
    () => ({
      messages: state.messages,
      onSendMessage: chat.handleSendMessage,
      onQueueMessage: chat.handleQueueMessage,
      onQueueMessageRemove: chatQueue.remove,
      onQueueMessageEdit: chatQueue.edit,
      onQueueReorder: chatQueue.reorder,
      onClearQueuedMessages: chatQueue.clear,
      queuedMessages: chatQueue.messages,
      queuedMessagesCount: chatQueue.messages.length,
      inputValue,
      onInputChange: setInputValue,
      isProcessing: state.isProcessing,
      lastError: state.lastError,
      // The notice belongs to the chat whose send was held back, not whichever
      // chat is on screen.
      workspaceBusy: state.workspaceBusy?.chatId === (activeChatId ?? '') ? state.workspaceBusy : null,
      chatId: activeChatId ?? undefined,
      onDismissBusy: overrides.onDismissBusy ?? handleDismissBusy,
      pendingDraft: inputValue,
      toolExecutions: state.toolExecutions,
      queryProgress: state.queryProgress,
      currentTodos,
      onStopProcessing: chat.handleStopProcessing,
      onRetractSteer: chat.handleRetractSteer,
      onChatCleared: overrides.onChatCleared,
      fileEdits: overrides.fileEdits ?? state.fileEdits,
      onReviewChange: overrides.onReviewChange,
      onRestoreSession: overrides.onRestoreSession,
      queryCount: state.queryCount,
      activeToolDetail: overrides.activeToolDetail ?? null,
      onToolDetailToggle: overrides.onToolDetailToggle,
      stats: state.stats,
      isConnected: state.isConnected,
      onModelClick: overrides.onModelClick,
      backendReachable: overrides.backendReachable,
      onRetryConnection: overrides.onRetryConnection,
      subagentActivities: state.subagentActivities,
      outputVerbosity: state.outputVerbosity,
      onForkAtBreakpoint: overrides.onForkAtBreakpoint,
      isForking: overrides.isForking,
    }),
    [state, chat, chatQueue, inputValue, setInputValue, activeChatId, currentTodos, handleDismissBusy, overrides],
  );

  const reviewProps = useMemo<WorkspaceChatReviewProps>(
    () => overrides.review ?? ({} as WorkspaceChatReviewProps),
    [overrides.review],
  );
  const diffState = useMemo<WorkspaceChatDiffState>(
    () => overrides.diff ?? ({} as WorkspaceChatDiffState),
    [overrides.diff],
  );

  return { chatProps, reviewProps, diffState };
}
