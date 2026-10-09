/**
 * The host chat session store is appended from the shared event-handler seam.
 *
 * When the active host advertises `capabilities.chatSessions`, a finished turn
 * is appended to the host store (POST `/api/chat-sessions/turn`) from
 * `useWebSocketEventHandler`, above the per-chat filter — so both agent
 * backends and background chats record their turns. Without the capability the
 * seam is a no-op and the daemon/browser-local store owns the turn.
 */
// @ts-nocheck — mock objects don't fully implement all interfaces

import type { WsEvent } from '@sprout/events';
import type { MutableRefObject } from 'react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import type { SproutHost } from '../host/types';
import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';

vi.mock('../utils/log', () => ({ debugLog: vi.fn(), error: vi.fn() }));
vi.mock('../utils/chatCompletion', () => ({
  ensureCompletedAssistantMessage: vi.fn((messages) => messages),
}));
vi.mock('../utils/messageId', () => ({ generateMessageId: vi.fn(() => 'msg-1') }));
vi.mock('../utils/messageWindow', () => ({ trimMessages: vi.fn((m) => m) }));
vi.mock('../utils/logCap', () => ({ appendCappedLog: vi.fn((logs, entry) => [...logs, entry]) }));
vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
  // The host store append goes through clientFetch; a passthrough keeps the
  // seam under test (the URL the host store is reached at) observable.
  clientFetch: (input: RequestInfo | URL, init?: RequestInit) => fetch(input, init),
}));
vi.mock('../services/lspClientService', () => ({
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

function createDefaultState(): Record<string, unknown> {
  return {
    isConnected: false,
    provider: '',
    model: '',
    sessionId: null,
    queryCount: 0,
    messages: [],
    logs: [],
    isProcessing: false,
    lastError: null,
    currentView: 'chat',
    toolExecutions: [],
    queryProgress: null,
    stats: {},
    currentTodos: [],
    fileEdits: [],
    subagentActivities: [],
    activeChatId: null,
    chatSessions: [],
    perChatCache: {},
    securityApprovalRequest: null,
    securityPromptRequest: null,
    askUserRequest: null,
    passwordRequest: null,
    editApprovalRequest: null,
    modelSelectionRequest: null,
    outputVerbosity: 'default',
  };
}

let container: HTMLDivElement;
let root: Root;
let hookHandleEvent: ((event: WsEvent) => void) | null = null;

function HookWrapper({
  setStateMock,
  activeChatIdRef,
}: {
  setStateMock: unknown;
  activeChatIdRef: MutableRefObject<string | null>;
}) {
  const refs: UseWebSocketEventHandlerRefs = {
    activeRequestsRef: { current: 0 },
    activeChatIdRef,
    pendingProviderRef: { current: 'openai' },
    pendingProviderChangeRef: { current: false },
    pendingProviderChangeValueRef: { current: null },
    connectionTimeoutRef: { current: null },
    lastConnectionStateRef: { current: false },
  };
  const apiService = { getStats: vi.fn().mockResolvedValue({ provider: 'openai', model: 'gpt-4' }) };
  const { handleEvent } = useWebSocketEventHandler({ setState: setStateMock, refs, apiService });
  hookHandleEvent = handleEvent;
  return createElement('div', null, 'hook host');
}

function mount(activeChatId: string | null = 'chat-1') {
  const state = { current: createDefaultState() };
  const setStateMock = vi.fn((updater: unknown) => {
    if (typeof updater === 'function') state.current = { ...state.current, ...updater(state.current) };
    else state.current = updater;
  });
  const activeChatIdRef: MutableRefObject<string | null> = { current: activeChatId };
  act(() => {
    root.render(createElement(HookWrapper, { setStateMock, activeChatIdRef }));
  });
  return { state, activeChatIdRef };
}

function hostWithStore(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    transport: { apiBaseURL: 'https://host.test/backend', wsURL: '', authMode: 'bearer' },
    capabilities: { ...base.capabilities, chat: true, chatSessions: true },
  };
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  hookHandleEvent = null;
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => root?.unmount());
  container?.remove();
  setActiveHost(headlessHost());
  vi.unstubAllGlobals();
});

describe('host chat session store append seam', () => {
  it('appends the question on query_started and the answer on query_completed', async () => {
    setActiveHost(hostWithStore());
    const fetchSpy = vi.fn(async () => new Response('{}', { status: 200 }));
    vi.stubGlobal('fetch', fetchSpy);
    mount('chat-1');

    act(() => {
      hookHandleEvent!({
        type: 'query_started',
        data: { query: 'what is 2+2?', chat_id: 'chat-1' },
      } as unknown as WsEvent);
    });
    act(() => {
      hookHandleEvent!({
        type: 'query_completed',
        data: { query: 'what is 2+2?', response: '4', chat_id: 'chat-1' },
      } as unknown as WsEvent);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    const turns = fetchSpy.mock.calls
      .filter((c) => String(c[0]).endsWith('/api/chat-sessions/turn'))
      .map((c) => JSON.parse(String((c[1] as RequestInit).body)));
    expect(turns).toEqual([
      { chat_id: 'chat-1', query: 'what is 2+2?' },
      { chat_id: 'chat-1', query: 'what is 2+2?', response: '4' },
    ]);
  });

  it('records a background chat’s turn (above the per-chat filter)', async () => {
    setActiveHost(hostWithStore());
    const fetchSpy = vi.fn(async () => new Response('{}', { status: 200 }));
    vi.stubGlobal('fetch', fetchSpy);
    mount('chat-active');

    act(() => {
      hookHandleEvent!({
        type: 'query_started',
        data: { query: 'background q', chat_id: 'chat-other' },
      } as unknown as WsEvent);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    const turns = fetchSpy.mock.calls
      .filter((c) => String(c[0]).endsWith('/api/chat-sessions/turn'))
      .map((c) => JSON.parse(String((c[1] as RequestInit).body)));
    expect(turns).toEqual([{ chat_id: 'chat-other', query: 'background q' }]);
  });

  it('is a no-op without the capability', async () => {
    setActiveHost(headlessHost());
    const fetchSpy = vi.fn(async () => new Response('{}', { status: 200 }));
    vi.stubGlobal('fetch', fetchSpy);
    mount('chat-1');

    act(() => {
      hookHandleEvent!({ type: 'query_started', data: { query: 'q', chat_id: 'chat-1' } } as unknown as WsEvent);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(fetchSpy.mock.calls.some((c) => String(c[0]).includes('/api/chat-sessions/turn'))).toBe(false);
  });

  it('skips subagent runs and /clear', async () => {
    setActiveHost(hostWithStore());
    const fetchSpy = vi.fn(async () => new Response('{}', { status: 200 }));
    vi.stubGlobal('fetch', fetchSpy);
    mount('chat-1');

    act(() => {
      hookHandleEvent!({
        type: 'query_started',
        data: { query: 'subagent task', chat_id: 'chat-1', subagent_depth: 1 },
      } as unknown as WsEvent);
    });
    act(() => {
      hookHandleEvent!({ type: 'query_started', data: { query: '/clear', chat_id: 'chat-1' } } as unknown as WsEvent);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(fetchSpy.mock.calls.some((c) => String(c[0]).includes('/api/chat-sessions/turn'))).toBe(false);
  });
});
