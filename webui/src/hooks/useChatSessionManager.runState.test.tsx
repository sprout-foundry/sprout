import type { WsEvent } from '@sprout/events';
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { ChatSessionsApi } from '../services/chatSessions';
import { fetchChatRunning } from '../services/runStatus';
import type { AppState } from '../types/app';
import { onChatReplay } from '../utils/chatReplay';
import { noteRunStarted, resetRunEpochs } from '../utils/runEpoch';
import { useChatSessionManager, type ChatApi } from './useChatSessionManager';

vi.mock('../services/api', () => ({ ApiService: { getInstance: () => ({}) } }));
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));
vi.mock('../services/notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));
vi.mock('../services/runStatus', () => ({
  fetchChatRunning: vi.fn(),
  // The hook resolves a chat's running state through this; the cases here set
  // the store's own flag, which is the answer when the store runs the agent.
  resolveChatRunning: vi.fn((_chatId: string | null | undefined, storeSaysRunning: boolean | undefined) =>
    Promise.resolve(storeSaysRunning === true),
  ),
}));

const fetchRunning = fetchChatRunning as ReturnType<typeof vi.fn>;

function chatSession(overrides: Record<string, unknown> = {}) {
  return {
    chat_session: { id: 'chat-a', messages: [], active_query: false, run_events: [], ...overrides },
    active_chat_id: 'chat-a',
  };
}

function setup(opts: { sessions?: Partial<ChatSessionsApi>; api?: Partial<ChatApi>; state?: Partial<AppState> } = {}) {
  let state = {
    messages: [],
    isProcessing: false,
    perChatCache: {},
    workspaceBusy: null,
    activeChatId: null,
    ...opts.state,
  } as unknown as AppState;
  const activeRequestsRef = { current: state.isProcessing ? 1 : 0 };
  const activeChatIdRef = { current: (state.activeChatId as string | null) ?? null };
  const sessions = {
    listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [], active_chat_id: 'chat-a' }),
    switchChatSession: vi.fn().mockResolvedValue(chatSession()),
    createChatSession: vi.fn(),
    createChatSessionInWorktree: vi.fn(),
    deleteChatSession: vi.fn(),
    deleteAllChatSessions: vi.fn(),
    renameChatSession: vi.fn(),
    ...opts.sessions,
  } as unknown as ChatSessionsApi;
  const api: ChatApi = {
    sendQuery: vi.fn(),
    steerQuery: vi.fn(),
    stopQuery: vi.fn(),
    retractSteer: vi.fn(),
    ...opts.api,
  } as ChatApi;
  const utils = renderHook(() => {
    const setState: AppStoreSetState = (updater) => {
      const partial = typeof updater === 'function' ? (updater as (p: AppState) => Partial<AppState>)(state) : updater;
      state = { ...state, ...partial };
    };
    return useChatSessionManager({
      setState,
      activeRequestsRef,
      activeChatIdRef,
      queuedMessagesRef: { current: [] },
      isProcessing: state.isProcessing,
      api,
      sessions,
    });
  });
  return { ...utils, getState: () => state, activeRequestsRef, activeChatIdRef, sessions };
}

beforeEach(() => {
  vi.clearAllMocks();
  resetRunEpochs();
});

describe('boot into a running chat', () => {
  it('shows it running and replays its buffered run events', async () => {
    const runEvents = [{ type: 'query_started', data: { chat_id: 'chat-a', query: 'hi' } }];
    const replayed: Array<{ chatId: string; events: WsEvent[]; fromServer?: boolean }> = [];
    const off = onChatReplay((chatId, events, options) =>
      replayed.push({ chatId, events, fromServer: options.fromServer }),
    );
    const h = setup({
      sessions: {
        switchChatSession: vi.fn().mockResolvedValue(chatSession({ active_query: true, run_events: runEvents })),
      },
    });

    await act(async () => {
      await h.result.current.loadChatSessions();
    });
    off();

    expect(h.getState().isProcessing).toBe(true);
    expect(h.activeRequestsRef.current).toBe(1);
    expect(replayed).toEqual([{ chatId: 'chat-a', events: runEvents, fromServer: true }]);
  });

  it('leaves an idle chat idle', async () => {
    const h = setup();
    await act(async () => {
      await h.result.current.loadChatSessions();
    });
    expect(h.getState().isProcessing).toBe(false);
  });
});

describe('switch response older than a run start', () => {
  it('keeps the chat running when a run started after the switch was requested', async () => {
    let resolve!: (v: unknown) => void;
    const h = setup({
      state: { activeChatId: 'chat-z' } as Partial<AppState>,
      sessions: { switchChatSession: vi.fn(() => new Promise((r) => (resolve = r))) },
    });
    const switching = h.result.current.handleActiveChatChange('chat-a');
    // The run starts (query_started bumps the epoch, the handler flips running)
    // before the switch response arrives saying "not active".
    noteRunStarted('chat-a');
    h.getState().isProcessing = true;
    await act(async () => {
      resolve(chatSession({ active_query: false }));
      await switching;
    });
    expect(h.getState().isProcessing).toBe(true);
  });
});

describe('stop that fails', () => {
  it('keeps the run when the stop request fails and the server still runs it', async () => {
    fetchRunning.mockResolvedValue(true);
    const h = setup({
      state: { isProcessing: true, activeChatId: 'chat-a' } as Partial<AppState>,
      api: { stopQuery: vi.fn().mockRejectedValue(new Error('Failed to stop query')) },
    });
    await act(async () => {
      await h.result.current.handleStopProcessing();
    });
    expect(h.getState().isProcessing).toBe(true);
    expect(h.activeRequestsRef.current).toBe(1);
    expect(h.getState().lastError).toBe('Failed to stop query');
  });

  it('goes idle once the status check says the run is over', async () => {
    fetchRunning.mockResolvedValue(false);
    const h = setup({
      state: { isProcessing: true, activeChatId: 'chat-a' } as Partial<AppState>,
      api: { stopQuery: vi.fn().mockRejectedValue(new Error('Failed to stop query')) },
    });
    await act(async () => {
      await h.result.current.handleStopProcessing();
    });
    expect(fetchRunning).toHaveBeenCalledWith('chat-a');
    expect(h.getState().isProcessing).toBe(false);
  });
});

describe('send while the workspace is stopping', () => {
  it('rolls back the optimistic run without a second failure bubble', async () => {
    const stopping = Object.assign(new Error('The workspace is stopping'), { code: 'workspace_stopping' });
    const h = setup({
      state: { activeChatId: 'chat-a' } as Partial<AppState>,
      api: { sendQuery: vi.fn().mockRejectedValue(stopping) },
    });
    await act(async () => {
      await h.result.current.handleSendMessage('hello');
    });
    expect(h.getState().isProcessing).toBe(false);
    expect(h.getState().messages.filter((m) => m.content.startsWith('[FAIL]'))).toEqual([]);
  });
});
