/**
 * WorkspaceChatProvider — the chat unit driven by a fake transport.
 *
 * The unit's contract is that it needs only a fetch function and an events
 * provider: delivered events fill the transcript, and a send goes out through
 * the fetch as a `/api/query` POST. These tests drive the real provider,
 * reducer and session manager with a fake fetch and a fake events provider —
 * no WebSocket, no backend — and assert exactly those two things, plus the
 * view-props assembly the chat surface renders from.
 */

import type { EventsProvider, WsEvent } from '@sprout/events';
import { act, render, screen } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it, vi } from 'vitest';
import { HostProvider, headlessHost } from '../host';
import type { SproutHost } from '../host';
import type { AppState } from '../types/app';
import {
  WorkspaceChatProvider,
  createEmptyChatState,
  useWorkspaceChat,
  useWorkspaceChatProps,
} from './WorkspaceChatContext';

const here = dirname(fileURLToPath(import.meta.url));

// The chat unit reaches the app's service singletons for the reconnect stats
// probe; the tests never exercise it, so a minimal stub keeps the module graph
// off the network.
vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      getStats: vi.fn().mockResolvedValue({}),
      getSettings: vi.fn().mockResolvedValue({}),
    }),
  },
}));

vi.mock('../services/notificationBus', () => ({
  notificationBus: { notify: vi.fn() },
}));

// The background-pane refresh and the chat-session list are not under test;
// their module functions are replaced with no-op doubles so nothing reaches a
// real transport. The two session-API factories are spied so the selection
// (host store vs daemon) is observable.
const sessionsModule = vi.hoisted(() => ({
  createChatSessionsApi: vi.fn(),
  listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [], active_chat_id: '' }),
}));

const hostSessionsModule = vi.hoisted(() => ({
  createHostChatSessionsApi: vi.fn(),
}));

vi.mock('../services/chatSessions', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/chatSessions')>();
  return {
    ...actual,
    listChatSessions: sessionsModule.listChatSessions,
    createChatSessionsApi: sessionsModule.createChatSessionsApi,
  };
});

vi.mock('../services/hostChatSessions', () => ({
  createHostChatSessionsApi: hostSessionsModule.createHostChatSessionsApi,
  appendTurnToHostStore: vi.fn(),
}));

function createInitialState(): AppState {
  return {
    isConnected: true,
    provider: 'openai',
    model: 'gpt-4',
    sessionId: null,
    queryCount: 0,
    messages: [],
    logs: [],
    isProcessing: false,
    lastError: null,
    workspaceBusy: null,
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
    shellApprovalRequest: null,
    modelSelectionRequest: null,
    outputVerbosity: 'default',
    inputValue: '',
  };
}

/** A fake events provider that records the callback the unit registers. */
function createEventsProvider() {
  let handler: ((event: WsEvent) => void) | null = null;
  const provider = {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onEvent: vi.fn((cb: (event: WsEvent) => void) => {
      handler = cb;
    }),
    removeEvent: vi.fn(),
    sendEvent: vi.fn(),
    isConnected: vi.fn(() => true),
    onReconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn(() => 0),
    flushQueuedMessages: vi.fn(() => 0),
  } as unknown as EventsProvider;
  return {
    provider,
    deliver: (event: WsEvent) => {
      if (!handler) throw new Error('no event handler registered');
      handler(event);
    },
  };
}

/** A fake fetch that answers the chat endpoints the unit calls. */
function createFetch() {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  const fetchFn = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return {
      ok: true,
      status: 200,
      json: async () => ({}),
      text: async () => '',
    } as unknown as Response;
  });
  return { fetchFn: fetchFn as unknown as typeof fetch, calls };
}

/** Renders the unit and exposes its value + assembled view props. */
function Harness({ onValue }: { onValue: (value: ReturnType<typeof useWorkspaceChat>) => void }) {
  const value = useWorkspaceChat();
  const { chatProps, reviewProps, diffState } = useWorkspaceChatProps({
    review: { review: null, reviewError: null } as never,
    diff: { activeDiffPath: null, activeDiff: null, diffMode: 'combined' } as never,
  });
  onValue(value);
  return (
    <div>
      <span data-testid="message-count">{value.state.messages.length}</span>
      <span data-testid="last-message">{value.state.messages.at(-1)?.content ?? ''}</span>
      <span data-testid="is-processing">{String(value.state.isProcessing)}</span>
      <span data-testid="chat-props-messages">{chatProps.messages.length}</span>
      <span data-testid="review-has-key">{String('review' in reviewProps)}</span>
      <span data-testid="diff-mode">{diffState.diffMode as string}</span>
    </div>
  );
}

/** A minimal ChatSessionsApi double. */
function fakeSessionsApi(label: string) {
  return {
    __label: label,
    listChatSessions: vi
      .fn()
      .mockResolvedValue({ message: 'ok', chat_sessions: [], active_chat_id: '', total_sessions: 0 }),
    createChatSession: vi.fn(),
    deleteChatSession: vi.fn(),
    deleteAllChatSessions: vi.fn(),
    renameChatSession: vi.fn(),
    switchChatSession: vi.fn(),
    createChatSessionInWorktree: vi.fn(),
  };
}

/** A ChatSessionsApi double whose store holds one chat with a transcript. */
function fakeSessionsApiWithTranscript(chatId: string, messages: Array<{ role: string; content: string }>) {
  const api = fakeSessionsApi('host');
  api.listChatSessions.mockResolvedValue({
    message: 'ok',
    chat_sessions: [{ id: chatId } as never],
    active_chat_id: chatId,
    total_sessions: 1,
  });
  api.switchChatSession.mockResolvedValue({
    message: 'ok',
    active_chat_id: chatId,
    chat_session: { messages } as never,
  });
  return api;
}

const DEFAULT_TEST_HOST: SproutHost = headlessHost();

function renderUnit(overrides: { fetchFn?: typeof fetch; host?: SproutHost } = {}) {
  const events = createEventsProvider();
  const fetch = createFetch();
  let value: ReturnType<typeof useWorkspaceChat> | null = null;
  const tree = (
    <WorkspaceChatProvider
      initialState={createInitialState()}
      eventsProvider={events.provider}
      fetchFn={overrides.fetchFn ?? fetch.fetchFn}
    >
      <Harness
        onValue={(v) => {
          value = v;
        }}
      />
    </WorkspaceChatProvider>
  );
  const utils = render(<HostProvider host={overrides.host ?? DEFAULT_TEST_HOST}>{tree}</HostProvider>);
  return { ...utils, events, fetch, getValue: () => value! };
}

describe('WorkspaceChatProvider', () => {
  beforeEach(() => {
    sessionsModule.createChatSessionsApi.mockReset();
    hostSessionsModule.createHostChatSessionsApi.mockReset();
    sessionsModule.createChatSessionsApi.mockImplementation(() => fakeSessionsApi('daemon'));
    hostSessionsModule.createHostChatSessionsApi.mockImplementation(() => fakeSessionsApi('host'));
  });

  it('subscribes the events provider to the chat reducer on mount', () => {
    const { events } = renderUnit();
    expect(events.provider.onEvent).toHaveBeenCalledTimes(1);
    expect(events.provider.onReconnect).toHaveBeenCalled();
  });

  it('produces a message from a delivered query_started event', async () => {
    const { events } = renderUnit();

    act(() => {
      events.deliver({
        type: 'query_started',
        data: { query: 'explain the build', chat_id: 'chat-1' },
      } as unknown as WsEvent);
    });

    expect(screen.getByTestId('message-count').textContent).toBe('1');
    expect(screen.getByTestId('last-message').textContent).toBe('explain the build');
    expect(screen.getByTestId('is-processing').textContent).toBe('true');
  });

  it('calls /api/query on send through the supplied fetch', async () => {
    const { fetch, getValue } = renderUnit();

    await act(async () => {
      await getValue().chat.handleSendMessage('hello world');
    });

    const queryCall = fetch.calls.find((c) => c.url === '/api/query');
    expect(queryCall).toBeDefined();
    expect(queryCall?.init?.method).toBe('POST');
    expect(JSON.parse(String(queryCall?.init?.body))).toMatchObject({ query: 'hello world' });
  });

  it('assembles the chat view props from the chat state', async () => {
    const { events } = renderUnit();

    act(() => {
      events.deliver({
        type: 'query_started',
        data: { query: 'a question', chat_id: 'chat-1' },
      } as unknown as WsEvent);
    });

    expect(screen.getByTestId('chat-props-messages').textContent).toBe('1');
    expect(screen.getByTestId('review-has-key').textContent).toBe('true');
    expect(screen.getByTestId('diff-mode').textContent).toBe('combined');
  });

  it('unsubscribes the events provider on unmount', () => {
    const { events, unmount } = renderUnit();
    unmount();
    expect(events.provider.removeEvent).toHaveBeenCalled();
  });

  it('provides an empty chat state a composition can start from', () => {
    const empty = createEmptyChatState();
    // A blank chat: no transcript, nothing processing, no sessions.
    expect(empty.messages).toEqual([]);
    expect(empty.isProcessing).toBe(false);
    expect(empty.activeChatId).toBeNull();
    expect(empty.chatSessions).toEqual([]);
    expect(empty.inputValue).toBe('');
    // It is a complete state — every field the store reads is present.
    expect(Object.keys(empty).sort()).toEqual(Object.keys(createInitialState()).sort());
  });

  it('is mounted by the app with its own transport, and the app does not subscribe twice', () => {
    // The unit owns the event subscription; the app's initialization hook must
    // therefore stand down (`manageEventSubscription: false`) or every event
    // would be delivered twice. Source-shape assertion, matching the app's
    // composition test.
    const appSource = readFileSync(resolve(here, '../App.tsx'), 'utf-8');
    expect(appSource).toContain('<WorkspaceChatProvider');
    expect(appSource).toMatch(/fetchFn=\{clientFetch\}/);
    expect(appSource).toMatch(/manageEventSubscription:\s*false/);
  });

  it('uses the daemon session API when the host does not advertise a session store', () => {
    renderUnit({ host: { ...headlessHost(), capabilities: { ...headlessHost().capabilities, chat: true } } });
    expect(sessionsModule.createChatSessionsApi).toHaveBeenCalledTimes(1);
    expect(hostSessionsModule.createHostChatSessionsApi).not.toHaveBeenCalled();
  });

  it('uses the host session API (at the host base) when the capability is on', () => {
    const host: SproutHost = {
      ...headlessHost(),
      transport: { apiBaseURL: 'https://host.test/backend', wsURL: '', authMode: 'bearer' },
      capabilities: { ...headlessHost().capabilities, chat: true, chatSessions: true },
    };
    renderUnit({ host });
    expect(hostSessionsModule.createHostChatSessionsApi).toHaveBeenCalledWith('https://host.test/backend');
    expect(sessionsModule.createChatSessionsApi).not.toHaveBeenCalled();
  });

  it('restores the transcript from the host store on load (reload path)', async () => {
    const host: SproutHost = {
      ...headlessHost(),
      transport: { apiBaseURL: 'https://host.test/backend', wsURL: '', authMode: 'bearer' },
      capabilities: { ...headlessHost().capabilities, chat: true, chatSessions: true },
    };
    hostSessionsModule.createHostChatSessionsApi.mockReturnValue(
      fakeSessionsApiWithTranscript('host-chat-1', [
        { role: 'user', content: 'restored question' },
        { role: 'assistant', content: 'restored answer' },
      ]),
    );
    const { getValue } = renderUnit({ host });

    await act(async () => {
      await getValue().chat.loadChatSessions();
    });

    expect(getValue().state.activeChatId).toBe('host-chat-1');
    expect(getValue().state.messages.map((m) => m.content)).toEqual(['restored question', 'restored answer']);
  });
});
