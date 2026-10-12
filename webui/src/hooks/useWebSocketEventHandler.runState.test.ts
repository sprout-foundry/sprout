/**
 * The running indicator across disconnects, reconnects, errors and replays:
 * a dropped socket keeps the run, the reconnect sync asks per chat (retrying
 * and ignoring stale answers), only run-ending errors close the turn, the
 * active chat's own output proves it is running, and server snapshots are
 * applied once.
 */
import type { WsEvent } from '@sprout/events';
import { act, renderHook } from '@testing-library/react';
import type { MutableRefObject } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../utils/log', () => ({ debugLog: vi.fn(), error: vi.fn() }));
vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
  clientFetch: vi.fn(),
  appendClientIdToUrl: vi.fn((u: string) => u),
  getProxyBase: vi.fn(() => ''),
}));
vi.mock('../services/runStatus', () => ({ fetchChatRunning: vi.fn(), agentRunsInPage: vi.fn(() => false) }));
vi.mock('../services/hostChatSessions', () => ({
  appendTurnToHostStore: vi.fn(),
  fetchChatSessionMessagesForActiveStore: vi.fn(),
  listChatSessionsForActiveStore: vi.fn(() => Promise.resolve({ chat_sessions: [] })),
}));
vi.mock('../services/notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));
vi.mock('../services/lspClientService', () => ({
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

import type { AppStoreSetState } from '../contexts/AppStore';
import { fetchChatSessionMessagesForActiveStore } from '../services/hostChatSessions';
import { notificationBus } from '../services/notificationBus';
import { fetchChatRunning } from '../services/runStatus';
import { WebSocketService } from '../services/websocket';
import { requestChatReplay } from '../utils/chatReplay';
import { resetRunEpochs } from '../utils/runEpoch';
import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';

const fetchRunning = fetchChatRunning as ReturnType<typeof vi.fn>;
const fetchMessages = fetchChatSessionMessagesForActiveStore as ReturnType<typeof vi.fn>;
const notify = notificationBus.notify as ReturnType<typeof vi.fn>;

type State = Record<string, unknown>;

function setup(initial: Partial<State> = {}) {
  const holder: { current: State } = {
    current: {
      isConnected: true,
      isProcessing: false,
      messages: [],
      logs: [],
      toolExecutions: [],
      queryProgress: null,
      lastError: null,
      stats: {},
      perChatCache: {},
      subagentActivities: [],
      activeChatId: 'chat-1',
      ...initial,
    },
  };
  const isProcessingRef: MutableRefObject<boolean> = { current: holder.current.isProcessing === true };
  const setState = vi.fn((updater: unknown) => {
    const patch = typeof updater === 'function' ? (updater as (p: State) => State)(holder.current) : updater;
    holder.current = { ...holder.current, ...(patch as State) };
    isProcessingRef.current = holder.current.isProcessing === true;
  }) as unknown as AppStoreSetState;
  const refs: UseWebSocketEventHandlerRefs = {
    activeRequestsRef: { current: holder.current.isProcessing ? 1 : 0 },
    activeChatIdRef: { current: 'chat-1' },
    pendingProviderRef: { current: '' },
    pendingProviderChangeRef: { current: false },
    pendingProviderChangeValueRef: { current: null },
    connectionTimeoutRef: { current: null },
    lastConnectionStateRef: { current: true },
    isProcessingRef,
  };
  const apiService = { getStats: vi.fn().mockResolvedValue({ provider: 'p', model: 'm', is_processing: false }) };
  const { result } = renderHook(() => useWebSocketEventHandler({ setState, refs, apiService }));
  const emit = (type: string, data: Record<string, unknown> = {}) =>
    act(() => result.current.handleEvent({ type, data } as WsEvent));
  return { holder, refs, result, emit, apiService };
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

beforeEach(() => {
  vi.clearAllMocks();
  resetRunEpochs();
  fetchMessages.mockResolvedValue({ chat_session: { messages: [], active_query: false } });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('disconnect and reconnect', () => {
  it('keeps the run through a dropped socket', () => {
    const { holder, refs, emit } = setup({ isProcessing: true });
    emit('connection_status', { connected: false, reconnecting: true });
    expect(holder.current.isProcessing).toBe(true);
    expect(holder.current.isConnected).toBe(false);
    expect(refs.activeRequestsRef.current).toBe(1);
  });

  it('asks for the active chat and ends a run that finished while away', async () => {
    const { holder, emit, result, refs } = setup({ isProcessing: true });
    emit('connection_status', { connected: false });
    fetchRunning.mockResolvedValue(false);
    await act(async () => {
      result.current.handleReconnect();
      await flush();
    });
    expect(fetchRunning).toHaveBeenCalledWith('chat-1', undefined);
    expect(holder.current.isProcessing).toBe(false);
    expect(refs.activeRequestsRef.current).toBe(0);
    // It was running before the drop, so the finished answer is reloaded.
    expect(fetchMessages).toHaveBeenCalledWith('chat-1', undefined);
  });

  it('does not take the client-wide stats flag as the running state', async () => {
    const { holder, result, apiService } = setup({ isProcessing: true });
    apiService.getStats.mockResolvedValue({ is_processing: false });
    fetchRunning.mockResolvedValue(true);
    await act(async () => {
      result.current.handleReconnect();
      await flush();
    });
    expect(holder.current.isProcessing).toBe(true);
    expect((holder.current.stats as State).is_processing).toBeUndefined();
  });

  it('retries the status lookup with backoff', async () => {
    vi.useFakeTimers();
    const { holder, result } = setup({ isProcessing: false });
    fetchRunning.mockRejectedValueOnce(new Error('503')).mockResolvedValueOnce(true);
    await act(async () => {
      result.current.handleReconnect();
      await flush();
    });
    expect(holder.current.isProcessing).toBe(false);
    await act(async () => {
      vi.advanceTimersByTime(500);
      await flush();
    });
    expect(fetchRunning).toHaveBeenCalledTimes(2);
    expect(holder.current.isProcessing).toBe(true);
  });

  it('ignores an idle answer that is older than a run started since', async () => {
    const { holder, result, emit } = setup({ isProcessing: false });
    let resolve!: (v: boolean) => void;
    fetchRunning.mockReturnValue(new Promise<boolean>((r) => (resolve = r)));
    act(() => result.current.handleReconnect());
    emit('query_started', { chat_id: 'chat-1', query: 'next' });
    expect(holder.current.isProcessing).toBe(true);
    await act(async () => {
      resolve(false);
      await flush();
    });
    expect(holder.current.isProcessing).toBe(true);
  });
});

describe('error frames', () => {
  it('a chatless transport error is a toast and leaves the run going', () => {
    const { holder, emit, refs } = setup({ isProcessing: true });
    emit('error', { message: 'Cannot change provider while this chat has an active run' });
    expect(holder.current.isProcessing).toBe(true);
    expect(refs.activeRequestsRef.current).toBe(1);
    expect(holder.current.messages).toEqual([]);
    expect(notify).toHaveBeenCalledWith('error', 'Error', 'Cannot change provider while this chat has an active run');
  });

  it('a subagent error or a non-terminal error leaves the run going', () => {
    const { holder, emit } = setup({ isProcessing: true });
    emit('error', { message: 'sub failed', chat_id: 'chat-1', subagent_depth: 1 });
    emit('error', { message: 'retrying', chat_id: 'chat-1', terminal: false });
    expect(holder.current.isProcessing).toBe(true);
  });

  it('a terminal error ends the turn with a failure bubble', () => {
    const { holder, emit } = setup({ isProcessing: true });
    emit('error', { message: 'Query failed', error: 'boom', chat_id: 'chat-1', terminal: true });
    expect(holder.current.isProcessing).toBe(false);
    expect(holder.current.lastError).toBe('Query failed: boom');
  });

  it('an unmarked error on the chat primary run still ends it (older servers)', () => {
    const { holder, emit } = setup({ isProcessing: true });
    emit('error', { message: 'Query failed', chat_id: 'chat-1' });
    expect(holder.current.isProcessing).toBe(false);
  });
});

describe('running safety net', () => {
  it('primary output for the active chat marks it running', () => {
    const { holder, emit, refs } = setup({ isProcessing: false });
    emit('tool_start', { chat_id: 'chat-1', tool_name: 'read_file', tool_call_id: 't1' });
    expect(holder.current.isProcessing).toBe(true);
    expect(refs.activeRequestsRef.current).toBe(1);
  });

  it('subagent output or another chat does not', () => {
    const { holder, emit } = setup({ isProcessing: false });
    emit('query_progress', { chat_id: 'chat-1', subagent_depth: 2, message: 'x' });
    emit('stream_chunk', { chat_id: 'chat-2', chunk: 'x' });
    expect(holder.current.isProcessing).toBe(false);
  });
});

describe('server snapshot replay', () => {
  it('applies only the events the live stream has not delivered', () => {
    const { holder, refs } = setup({ isProcessing: true, perChatCache: {} });
    refs.activeChatIdRef.current = 'chat-replay';
    const socket = WebSocketService.getInstance();
    const seen = { type: 'tool_start', data: { chat_id: 'chat-replay', tool_name: 'a', tool_call_id: 'a', __seq: 11 } };
    socket.markApplied(seen as WsEvent);
    const fresh = {
      type: 'tool_start',
      data: { chat_id: 'chat-replay', tool_name: 'b', tool_call_id: 'b', __seq: 12 },
    };

    act(() => requestChatReplay('chat-replay', [seen, fresh] as WsEvent[], { fromServer: true }));

    const tools = (holder.current.toolExecutions as Array<{ id?: string; tool?: string }>).map((t) => t.tool);
    expect(tools).toEqual(['b']);
  });
});

describe('workspace_stopping', () => {
  it('warns with the time left and leaves the run alone', () => {
    const { holder, emit } = setup({ isProcessing: true });
    emit('workspace_stopping', { reason: 'idle', at: '2026-10-12T15:00:00Z', deadline_seconds: 600 });
    expect(notify).toHaveBeenCalledWith(
      'warning',
      'Workspace stopping',
      expect.stringContaining('in 10 minutes'),
      15000,
    );
    expect(holder.current.isProcessing).toBe(true);
  });
});
