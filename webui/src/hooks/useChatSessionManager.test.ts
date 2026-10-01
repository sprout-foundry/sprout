/**
 * useChatSessionManager steer-retraction tests.
 *
 * Validates the Up-arrow pull-back contract: after a steer send while a
 * query is active, handleRetractSteer calls /api/query/steer/retract via
 * ApiService, removes the optimistic user bubble, and restores the text
 * to inputValue. Also covers the nothing-pending and already-picked-up
 * paths.
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { AppState } from '../types/app';
import { onChatReplay } from '../utils/chatReplay';
import { useChatSessionManager } from './useChatSessionManager';

const apiDouble = vi.hoisted(() => ({
  steerQuery: vi.fn().mockResolvedValue(undefined),
  retractSteer: vi.fn(),
  sendQuery: vi.fn().mockResolvedValue(undefined),
}));

const chatSessionsDouble = vi.hoisted(() => ({
  listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [], active_chat_id: null }),
  createChatSession: vi.fn().mockResolvedValue({ chat_session: { id: 'chat-1' } }),
  deleteChatSession: vi.fn().mockResolvedValue(undefined),
  deleteAllChatSessions: vi.fn().mockResolvedValue({ deleted_count: 3, active_chat_id: 'chat-default' }),
  renameChatSession: vi.fn().mockResolvedValue(undefined),
  switchChatSession: vi.fn().mockResolvedValue({
    active_chat_id: 'chat-1',
    chat_session: { messages: [], active_query: false },
  }),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => apiDouble,
  },
}));

vi.mock('../services/chatSessions', () => chatSessionsDouble);

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('../services/notificationBus', () => ({
  notificationBus: {
    notify: vi.fn(),
  },
}));

vi.mock('../utils/errorMessage', () => ({
  toUserErrorMessage: vi.fn((_e: unknown, fallback: string) => fallback),
}));

function setupHook() {
  let state: AppState = {
    messages: [],
    isProcessing: true,
    inputValue: '',
    perChatCache: {},
  } as unknown as AppState;

  const setState: AppStoreSetState = (updater) => {
    const partial = typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
    state = { ...state, ...partial };
  };

  const utils = renderHook(() =>
    useChatSessionManager({
      setState,
      activeRequestsRef: { current: 1 },
      activeChatIdRef: { current: 'chat-1' },
      queuedMessagesRef: { current: [] as import('./useChatSessionManager').QueuedMessage[] },
      isProcessing: true,
    }),
  );

  return {
    ...utils,
    getState: () => state,
  };
}

// ---------------------------------------------------------------------------
// Tests: chat deletion (worktree flag + delete-all)
// ---------------------------------------------------------------------------

describe('chat deletion', () => {
  beforeEach(() => {
    chatSessionsDouble.deleteChatSession.mockClear();
    chatSessionsDouble.deleteAllChatSessions.mockClear();
    chatSessionsDouble.listChatSessions.mockClear();
  });

  it('passes removeWorktree=false by default', async () => {
    const { result } = setupHook();

    await act(async () => {
      await result.current.handleDeleteChat('chat-9');
    });

    expect(chatSessionsDouble.deleteChatSession).toHaveBeenCalledWith('chat-9', false);
  });

  it('forwards the removeWorktree option', async () => {
    const { result } = setupHook();

    await act(async () => {
      await result.current.handleDeleteChat('chat-9', { removeWorktree: true });
    });

    expect(chatSessionsDouble.deleteChatSession).toHaveBeenCalledWith('chat-9', true);
  });

  it('handleDeleteAllChats calls the API and switches to the returned active chat', async () => {
    chatSessionsDouble.listChatSessions.mockResolvedValue({
      chat_sessions: [{ id: 'chat-default', name: 'Chat' }],
      active_chat_id: 'chat-default',
    });
    const { result, getState } = setupHook();

    await act(async () => {
      await result.current.handleDeleteAllChats();
    });

    expect(chatSessionsDouble.deleteAllChatSessions).toHaveBeenCalled();
    // Mode-less switch (the post-delete fallback keeps the legacy shape —
    // the server's default chat is lane-less).
    expect(chatSessionsDouble.switchChatSession).toHaveBeenCalledWith('chat-default', undefined);
    // The post-delete switch lands on the chat it switched to, even though
    // the mocked response names another active chat.
    expect(getState().activeChatId).toBe('chat-default');
  });

  it('handleDeleteAllChats surfaces a notification on failure', async () => {
    chatSessionsDouble.deleteAllChatSessions.mockRejectedValueOnce(new Error('boom'));
    const { result } = setupHook();

    await act(async () => {
      await result.current.handleDeleteAllChats();
    });

    const { notificationBus } = await import('../services/notificationBus');
    expect(notificationBus.notify).toHaveBeenCalledWith('error', 'Chat', expect.any(String), 5000);
  });
});

describe('loading the transcript after a reload', () => {
  it('keeps the conversation when a message was sent before it arrived', async () => {
    chatSessionsDouble.listChatSessions.mockResolvedValueOnce({
      chat_sessions: [{ id: 'chat-1' }],
      active_chat_id: 'chat-1',
    });
    chatSessionsDouble.switchChatSession.mockResolvedValueOnce({
      active_chat_id: 'chat-1',
      chat_session: {
        messages: [
          { role: 'user', content: 'Reply with exactly: FIRST-CONVO' },
          { role: 'assistant', content: 'FIRST-CONVO' },
        ],
        active_query: false,
      },
    });
    const hook = setupHook();
    // Typed during the load: its bubble is already on screen.
    act(() => {
      hook.getState().messages = [
        { id: 'local-q', type: 'user', content: 'What word did you reply with?', timestamp: new Date() },
      ] as never;
    });

    await act(async () => {
      await hook.result.current.loadChatSessions();
    });

    expect(hook.getState().messages.map((m) => `${m.type}:${m.content}`)).toEqual([
      'user:Reply with exactly: FIRST-CONVO',
      'assistant:FIRST-CONVO',
      'user:What word did you reply with?',
    ]);
  });

  it('does not repeat messages that are already on screen', async () => {
    const transcript = [
      { role: 'user', content: 'Say hi' },
      { role: 'assistant', content: 'Hi.' },
    ];
    chatSessionsDouble.listChatSessions.mockResolvedValueOnce({
      chat_sessions: [{ id: 'chat-1' }],
      active_chat_id: 'chat-1',
    });
    chatSessionsDouble.switchChatSession.mockResolvedValueOnce({
      active_chat_id: 'chat-1',
      chat_session: { messages: transcript, active_query: false },
    });
    const hook = setupHook();
    act(() => {
      hook.getState().messages = [
        { id: 'm1', type: 'user', content: 'Say hi', timestamp: new Date() },
        { id: 'm2', type: 'assistant', content: 'Hi.', timestamp: new Date() },
      ] as never;
    });

    await act(async () => {
      await hook.result.current.loadChatSessions();
    });

    expect(hook.getState().messages).toHaveLength(2);
  });
});

// ---------------------------------------------------------------------------
// Tests: per-chat lastError snapshot/restore across chat switches
//
// The banner lifecycle for background chats lives in useWebSocketEventHandler
// (events for non-active chats mirror the error lifecycle into the cache).
// These tests pin the session-manager half: switch-away snapshots the live
// banner into the outgoing cache, and switch-back restores it verbatim —
// identical to active-chat semantics, where the banner shows the most recent
// failure and clears when the next run starts (query_started) or completes.
// ---------------------------------------------------------------------------

describe('per-chat lastError snapshot/restore', () => {
  function setupSwitch(initial: Partial<AppState>) {
    let state: AppState = {
      messages: [],
      isProcessing: false,
      lastError: null,
      activeChatId: 'other-chat',
      perChatCache: {},
      ...initial,
    } as unknown as AppState;

    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };

    const activeChatIdRef = { current: 'other-chat' as string | null };
    const utils = renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef: { current: 0 },
        activeChatIdRef,
        queuedMessagesRef: { current: [] as import('./useChatSessionManager').QueuedMessage[] },
        isProcessing: false,
      }),
    );

    return {
      ...utils,
      getState: () => state,
      ref: activeChatIdRef,
    };
  }

  it('snapshots the live banner into the outgoing cache on switch-away', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-1',
      chat_session: { messages: [], active_query: false },
    });

    const hook = setupSwitch({
      lastError: 'Query failed: chat failed: dial tcp: connection refused',
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-1');
    });

    // The chat we left carries its banner into the cache so a switch back
    // (or an inactive pane render) shows what the user last saw.
    expect(hook.getState().perChatCache['other-chat']?.lastError).toBe(
      'Query failed: chat failed: dial tcp: connection refused',
    );
  });

  it('restores the cached banner verbatim on switch-back (background failure visibility)', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-1',
      chat_session: { messages: [], active_query: false },
    });

    const hook = setupSwitch({
      perChatCache: {
        'chat-1': {
          messages: [],
          lastError: 'Query failed: chat failed: dial tcp: connection refused',
          isProcessing: false,
          queryCount: 0,
        },
      },
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-1');
    });

    // A cached error that was never followed by a recovery event stays
    // visible — the background lifecycle (useWebSocketEventHandler) is what
    // clears it when the chat's next run completes.
    expect(hook.getState().lastError).toBe('Query failed: chat failed: dial tcp: connection refused');
  });

  it('restores null after a background recovery cleared the cache', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-1',
      chat_session: { messages: [{ role: 'user', content: 'hi' }], active_query: false },
    });

    const hook = setupSwitch({
      perChatCache: {
        // Simulate the post-fix state: an error arrived, then the chat's
        // recovery run completed and the background lifecycle cleared it.
        'chat-1': {
          messages: [],
          lastError: null,
          isProcessing: false,
          queryCount: 0,
        },
      },
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-1');
    });

    expect(hook.getState().lastError).toBeNull();
  });
});

describe('useChatSessionManager steer retraction', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiDouble.steerQuery.mockResolvedValue(undefined);
  });

  it('pulls back a pending steer: bubble removed, text restored to input', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('fix typo plz');
    });

    expect(apiDouble.steerQuery).toHaveBeenCalledWith('fix typo plz', 'chat-1');
    expect(hook.getState().messages).toHaveLength(1);
    expect(hook.getState().messages[0].content).toBe('fix typo plz');

    apiDouble.retractSteer.mockResolvedValue({ success: true, message: 'fix typo plz' });

    let retracted = false;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(true);
    expect(apiDouble.retractSteer).toHaveBeenCalledWith('chat-1');
    expect(hook.getState().messages).toHaveLength(0);
    expect(hook.getState().inputValue).toBe('fix typo plz');
  });

  it('returns false without an API call when no steer is pending', async () => {
    const hook = setupHook();

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
    expect(apiDouble.retractSteer).not.toHaveBeenCalled();
  });

  it('leaves the bubble when the steer was already picked up', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('already picked up');
    });

    apiDouble.retractSteer.mockResolvedValue({ success: false, message: '' });

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
    expect(hook.getState().messages).toHaveLength(1);
    expect(hook.getState().inputValue).toBe('');
  });

  it('survives a retract API failure without crashing', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('doomed steer');
    });

    apiDouble.retractSteer.mockRejectedValue(new Error('network down'));

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: queue CRUD (regression for 3b516bbb5)
//
// The refactor deleted the useQueuedMessages hook but kept only the bare
// ref + count, leaving CommandInput's queue panel fed by default props
// (empty list, no-op remove/edit/reorder/clear). These tests pin the full
// CRUD surface: the panel's data and handlers must exist, mutate the ref
// (the drain effect's source of truth), and keep count in sync.
// ---------------------------------------------------------------------------

describe('queued message management', () => {
  function setupQueueHook() {
    let state: AppState = {
      messages: [],
      isProcessing: true,
      inputValue: '',
    } as unknown as AppState;

    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };

    const queuedMessagesRef = { current: [] as import('./useChatSessionManager').QueuedMessage[] };
    const utils = renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef: { current: 1 },
        activeChatIdRef: { current: 'chat-1' },
        queuedMessagesRef,
        isProcessing: true,
      }),
    );

    return {
      ...utils,
      getState: () => state,
      queuedMessagesRef,
    };
  }

  it('exposes the queued messages array and keeps it in sync with additions', () => {
    const hook = setupQueueHook();

    act(() => {
      hook.result.current.handleQueueMessage('first queued');
    });
    act(() => {
      hook.result.current.handleQueueMessage('second queued');
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual(['first queued', 'second queued']);
    expect(hook.result.current.queuedMessagesCount).toBe(2);
    expect(hook.queuedMessagesRef.current.map((e) => e.message)).toEqual(['first queued', 'second queued']);
  });

  it('ignores blank queue additions', () => {
    const hook = setupQueueHook();

    act(() => {
      hook.result.current.handleQueueMessage('   ');
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual([]);
    expect(hook.result.current.queuedMessagesCount).toBe(0);
  });

  it('removes a queued message by index', () => {
    const hook = setupQueueHook();
    act(() => {
      hook.result.current.handleQueueMessage('a');
    });
    act(() => {
      hook.result.current.handleQueueMessage('b');
    });
    act(() => {
      hook.result.current.handleQueueMessage('c');
    });

    act(() => {
      hook.result.current.handleRemoveQueuedMessage(1);
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual(['a', 'c']);
    expect(hook.queuedMessagesRef.current.map((e) => e.message)).toEqual(['a', 'c']);
    expect(hook.result.current.queuedMessagesCount).toBe(2);
  });

  it('ignores out-of-range removals', () => {
    const hook = setupQueueHook();
    act(() => {
      hook.result.current.handleQueueMessage('only');
    });

    act(() => {
      hook.result.current.handleRemoveQueuedMessage(5);
    });
    act(() => {
      hook.result.current.handleRemoveQueuedMessage(-1);
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual(['only']);
  });

  it('edits a queued message in place', () => {
    const hook = setupQueueHook();
    act(() => {
      hook.result.current.handleQueueMessage('original');
    });

    act(() => {
      hook.result.current.handleEditQueuedMessage(0, 'edited');
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual(['edited']);
    expect(hook.queuedMessagesRef.current.map((e) => e.message)).toEqual(['edited']);
  });

  it('reorders queued messages', () => {
    const hook = setupQueueHook();
    for (const m of ['one', 'two', 'three']) {
      act(() => {
        hook.result.current.handleQueueMessage(m);
      });
    }

    act(() => {
      hook.result.current.handleReorderQueuedMessages(2, 0);
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual(['three', 'one', 'two']);
    expect(hook.queuedMessagesRef.current.map((e) => e.message)).toEqual(['three', 'one', 'two']);
  });

  it('clears the whole queue', () => {
    const hook = setupQueueHook();
    for (const m of ['one', 'two']) {
      act(() => {
        hook.result.current.handleQueueMessage(m);
      });
    }

    act(() => {
      hook.result.current.handleClearQueuedMessages();
    });

    expect(hook.result.current.queuedMessages.map((e) => e.message)).toEqual([]);
    expect(hook.result.current.queuedMessagesCount).toBe(0);
    expect(hook.queuedMessagesRef.current).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Tests: queue drain chat routing (cross-chat regression)
//
// A message queued while viewing chat A must not fire into chat B after a
// switch. The drain only dispatches entries whose chatId matches the active
// (idle) chat; entries for other chats stay queued.
// ---------------------------------------------------------------------------

describe('queue drain routing', () => {
  it('tags entries with the active chat at queue time', () => {
    let state: AppState = { messages: [], isProcessing: true } as unknown as AppState;
    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    const queuedMessagesRef = { current: [] as import('./useChatSessionManager').QueuedMessage[] };
    const activeChatIdRef = { current: 'chat-A' };

    const hook = renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef: { current: 1 },
        activeChatIdRef,
        queuedMessagesRef,
        isProcessing: true,
      }),
    );

    act(() => {
      hook.result.current.handleQueueMessage('for chat A');
    });

    expect(hook.result.current.queuedMessages[0]).toEqual({ message: 'for chat A', chatId: 'chat-A' });
  });

  it('drains only entries for the active chat', async () => {
    let state: AppState = { messages: [], isProcessing: false } as unknown as AppState;
    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    const queuedMessagesRef = {
      current: [
        { message: 'for B', chatId: 'chat-B' },
        { message: 'for A', chatId: 'chat-A' },
      ] as import('./useChatSessionManager').QueuedMessage[],
    };
    const activeChatIdRef = { current: 'chat-A' };
    apiDouble.sendQuery.mockClear();

    const hook = renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef: { current: 0 },
        activeChatIdRef,
        queuedMessagesRef,
        isProcessing: false,
      }),
    );

    await act(async () => {
      await Promise.resolve();
    });

    // Only the chat-A entry fired, and it fired AT chat A.
    expect(apiDouble.sendQuery).toHaveBeenCalledTimes(1);
    expect(apiDouble.sendQuery).toHaveBeenCalledWith('for A', 'chat-A');
    // The chat-B entry stays queued.
    expect(queuedMessagesRef.current.map((e) => e.message)).toEqual(['for B']);
  });

  it('drains entries with null chatId into the active chat', async () => {
    let state: AppState = { messages: [], isProcessing: false } as unknown as AppState;
    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    const queuedMessagesRef = {
      current: [{ message: 'untagged', chatId: null }] as import('./useChatSessionManager').QueuedMessage[],
    };
    const activeChatIdRef = { current: 'chat-A' };
    apiDouble.sendQuery.mockClear();

    renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef: { current: 0 },
        activeChatIdRef,
        queuedMessagesRef,
        isProcessing: false,
      }),
    );

    await act(async () => {
      await Promise.resolve();
    });

    expect(apiDouble.sendQuery).toHaveBeenCalledWith('untagged', 'chat-A');
  });
});

describe('switching back to a chat that ran in the background', () => {
  function setupSwitch(initial: Partial<AppState>) {
    let state: AppState = {
      messages: [],
      isProcessing: false,
      lastError: null,
      activeChatId: 'other-chat',
      perChatCache: {},
      ...initial,
    } as unknown as AppState;
    const setState: AppStoreSetState = (updater) => {
      const partial =
        typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    const activeRequestsRef = { current: 0 };
    const utils = renderHook(() =>
      useChatSessionManager({
        setState,
        activeRequestsRef,
        activeChatIdRef: { current: 'other-chat' as string | null },
        queuedMessagesRef: { current: [] as import('./useChatSessionManager').QueuedMessage[] },
        isProcessing: false,
      }),
    );
    const replays: Array<{ chatId: string; types: string[] }> = [];
    const stop = onChatReplay((chatId, events) => replays.push({ chatId, types: events.map((e) => e.type) }));
    return { ...utils, getState: () => state, replays, stop, activeRequestsRef };
  }

  const question = { id: 'q1', type: 'user' as const, content: 'Run the build', timestamp: new Date() };
  const answerSoFar = { id: 'a1', type: 'assistant' as const, content: 'Starting', timestamp: new Date() };

  it('replays its queued events instead of adopting the shorter stored copy of a running chat', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-a',
      chat_session: { messages: [], active_query: true },
    });
    const pending = [
      { type: 'stream_chunk', data: { chat_id: 'chat-a', chunk: ' the build' } },
      { type: 'tool_start', data: { chat_id: 'chat-a', tool_name: 'shell_command', tool_call_id: 't1' } },
    ];
    const hook = setupSwitch({
      perChatCache: {
        'chat-a': { messages: [question, answerSoFar], isProcessing: true, queryCount: 1, pendingEvents: pending },
      } as unknown as AppState['perChatCache'],
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-a');
    });
    hook.stop();

    expect(hook.replays).toEqual([{ chatId: 'chat-a', types: ['stream_chunk', 'tool_start'] }]);
    expect(hook.getState().messages.map((m) => m.id)).toEqual(['q1', 'a1']);
    expect(hook.getState().perChatCache['chat-a']?.pendingEvents).toBeUndefined();
    expect(hook.activeRequestsRef.current).toBe(1);
  });

  it('falls back to the stored copy plus the server run replay when events were dropped', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-a',
      chat_session: {
        messages: [],
        active_query: true,
        run_events: [{ type: 'query_started', data: { chat_id: 'chat-a', query: 'Run the build' } }],
      },
    });
    const hook = setupSwitch({
      perChatCache: {
        'chat-a': {
          messages: [question],
          isProcessing: true,
          queryCount: 1,
          pendingEvents: [{ type: 'stream_chunk', data: { chat_id: 'chat-a', chunk: 'x' } }],
          pendingEventsTruncated: true,
        },
      } as unknown as AppState['perChatCache'],
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-a');
    });
    hook.stop();

    expect(hook.replays).toEqual([{ chatId: 'chat-a', types: ['query_started'] }]);
  });

  it('does not replay the server run over a cached view that already has it', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-a',
      chat_session: {
        messages: [],
        active_query: true,
        run_events: [{ type: 'query_started', data: { chat_id: 'chat-a', query: 'Run the build' } }],
      },
    });
    const hook = setupSwitch({
      perChatCache: {
        'chat-a': { messages: [question, answerSoFar], isProcessing: true, queryCount: 1 },
      } as unknown as AppState['perChatCache'],
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-a');
    });
    hook.stop();

    expect(hook.replays).toEqual([]);
    expect(hook.getState().messages.map((m) => m.id)).toEqual(['q1', 'a1']);
  });

  it('counts a run that started and finished in the background as done', async () => {
    chatSessionsDouble.switchChatSession.mockResolvedValue({
      active_chat_id: 'chat-a',
      chat_session: { messages: [], active_query: false },
    });
    const hook = setupSwitch({
      perChatCache: {
        'chat-a': {
          messages: [question],
          isProcessing: false,
          queryCount: 1,
          pendingEvents: [
            { type: 'query_started', data: { chat_id: 'chat-a' } },
            { type: 'query_completed', data: { chat_id: 'chat-a' } },
          ],
        },
      } as unknown as AppState['perChatCache'],
    });

    await act(async () => {
      await hook.result.current.handleActiveChatChange('chat-a');
    });
    hook.stop();

    expect(hook.activeRequestsRef.current).toBe(0);
    expect(hook.getState().messages.map((m) => m.id)).toEqual(['q1']);
  });
});
