import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { AppState } from '../types/app';
import { useChatSessionManager, type QueuedMessage } from './useChatSessionManager';

const apiDouble = vi.hoisted(() => ({
  sendQuery: vi.fn(),
  steerQuery: vi.fn(),
  stopQuery: vi.fn(),
}));

vi.mock('../services/api', () => ({ ApiService: { getInstance: () => apiDouble } }));
vi.mock('../services/chatSessions', () => ({
  listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [], active_chat_id: 'chat-b' }),
  switchChatSession: vi.fn(),
  createChatSession: vi.fn(),
  deleteChatSession: vi.fn(),
  deleteAllChatSessions: vi.fn(),
  renameChatSession: vi.fn(),
  createChatSessionInWorktree: vi.fn(),
  fetchChatSessionMessages: vi.fn(),
}));
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));
vi.mock('../services/notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));

function busyError() {
  return Object.assign(new Error('busy'), {
    code: 'workspace_busy',
    runningChatId: 'chat-a',
    runningChatName: 'Chat A',
  });
}

function setup() {
  let state = {
    messages: [],
    isProcessing: false,
    inputValue: '',
    perChatCache: {},
    workspaceBusy: null,
  } as unknown as AppState;
  const queuedMessagesRef = { current: [] as QueuedMessage[] };
  const utils = renderHook(() => {
    const setState: AppStoreSetState = (updater) => {
      const partial = typeof updater === 'function' ? (updater as (p: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    return useChatSessionManager({
      setState,
      activeRequestsRef: { current: 0 },
      activeChatIdRef: { current: 'chat-b' },
      queuedMessagesRef,
      isProcessing: state.isProcessing,
      workspaceBusy: state.workspaceBusy,
    });
  });
  return {
    ...utils,
    queuedMessagesRef,
    getState: () => state,
    setBusy: (v: AppState['workspaceBusy']) => {
      state = { ...state, workspaceBusy: v };
      utils.rerender();
    },
  };
}

describe('send held back by another chat (workspace_busy)', () => {
  it('queues the message for its chat and holds it until the running chat finishes', async () => {
    apiDouble.sendQuery.mockRejectedValueOnce(busyError()).mockResolvedValue(undefined);
    const h = setup();

    await act(async () => {
      await h.result.current.handleSendMessage('what codeword?');
    });
    h.rerender();

    expect(h.getState().workspaceBusy).toMatchObject({ chatId: 'chat-b', runningChatId: 'chat-a' });
    expect(h.getState().messages).toEqual([]);
    expect(h.queuedMessagesRef.current).toEqual([{ message: 'what codeword?', chatId: 'chat-b' }]);
    expect(apiDouble.sendQuery).toHaveBeenCalledTimes(1);

    // Chat A finishes: the hold lifts and the queued message goes out.
    act(() => h.setBusy(null));
    await waitFor(() => expect(apiDouble.sendQuery).toHaveBeenCalledTimes(2));
    expect(apiDouble.sendQuery).toHaveBeenLastCalledWith('what codeword?', 'chat-b', undefined);
    expect(h.queuedMessagesRef.current).toEqual([]);
  });

  it('stops the chat on screen and notes the stop when nothing had streamed', async () => {
    apiDouble.stopQuery.mockResolvedValue(undefined);
    const h = setup();
    h.getState().messages.push({ id: 'q', type: 'user', content: 'count to 40', timestamp: new Date() } as never);

    await act(async () => {
      await h.result.current.handleStopProcessing();
    });

    expect(apiDouble.stopQuery).toHaveBeenCalledWith('chat-b');
    expect(h.getState().messages.map((m) => m.content)).toEqual(['count to 40', '_Stopped._']);
  });
});
