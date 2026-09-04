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

function setupHook() {
  let state: AppState = {
    messages: [],
    isProcessing: true,
    inputValue: '',
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
      queuedMessagesRef: { current: [] },
      isProcessing: true,
    }),
  );

  return {
    ...utils,
    getState: () => state,
  };
}

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
        queuedMessagesRef: { current: [] },
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
