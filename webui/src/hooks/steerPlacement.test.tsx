// @ts-nocheck
/**
 * A steer sent mid-run reaches the model only at the run's next boundary
 * (steer_delivered). Until then the run is still answering the earlier input,
 * so its streamed text, tool badges and final reply belong above the steer —
 * inserting them below split the answer mid-word around the steer bubble.
 */

import type { MutableRefObject } from 'react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterAll, beforeAll, afterEach } from 'vitest';

vi.mock('../utils/log', () => ({ debugLog: vi.fn(), error: vi.fn() }));

// REAL completion logic — no mock. This is the code path that replaces
// streamed content (and live-inserted markers) at query_completed.
vi.mock('../utils/messageId', () => ({
  generateMessageId: vi.fn(() => `msg-${Math.random().toString(36).slice(2)}`),
}));

vi.mock('../utils/messageWindow', () => ({
  trimMessages: vi.fn((messages) => messages),
}));

vi.mock('../utils/logCap', () => ({
  appendCappedLog: vi.fn((logs, entry) => [...logs, entry]),
}));

vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
}));

vi.mock('../services/errorCodes', () => ({
  getServerErrorCode: vi.fn(() => null),
}));

vi.mock('../services/lspClientService', () => ({
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { Message } from '@sprout/ui';
import { isPendingSteer, pendingSteerBubble } from '../utils/pendingSteer';

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
});

function createDefaultState(): Record<string, unknown> {
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
let hookHandleEvent: ((event: unknown) => void) | null = null;

// Mount the hook host. Called at the top of each test — the
// testing-library/no-render-in-lifecycle rule forbids rendering inside
// beforeEach, and CI enforces it as an error.
function mountHook(): { stateHolder: { current: Record<string, unknown> } } {
  const stateHolder = { current: createDefaultState() };
  const setStateMock = vi.fn((updater: unknown) => {
    if (typeof updater === 'function') {
      stateHolder.current = { ...stateHolder.current, ...updater(stateHolder.current) };
    } else {
      stateHolder.current = updater;
    }
  });
  act(() => {
    root.render(createElement(HookWrapper, { stateHolder, setStateMock }));
  });
  return { stateHolder };
}

const HookWrapper = ({
  stateHolder,
  setStateMock,
}: {
  stateHolder: { current: Record<string, unknown> };
  setStateMock: ReturnType<typeof vi.fn>;
}) => {
  const activeRequestsRef: MutableRefObject<number> = { current: 0 };
  const activeChatIdRef: MutableRefObject<string | null> = { current: null };
  const pendingProviderRef: MutableRefObject<string> = { current: 'openai' };
  const pendingProviderChangeRef: MutableRefObject<boolean> = { current: false };
  const pendingProviderChangeValueRef: MutableRefObject<string | null> = { current: null };
  const connectionTimeoutRef: MutableRefObject<ReturnType<typeof setTimeout> | null> = { current: null };
  const lastConnectionStateRef: MutableRefObject<boolean> = { current: false };

  const refs: UseWebSocketEventHandlerRefs = {
    activeRequestsRef,
    activeChatIdRef,
    pendingProviderRef,
    pendingProviderChangeRef,
    pendingProviderChangeValueRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
  };

  const apiService = { getStats: vi.fn().mockResolvedValue({ provider: 'openai', model: 'gpt-4' }) };

  const { handleEvent } = useWebSocketEventHandler({
    setState: setStateMock as AppStoreSetState,
    refs,
    apiService,
  });

  hookHandleEvent = handleEvent;
  return createElement('div', null, 'hook host');
};

function fire(event: { type: string; data?: Record<string, unknown> }): void {
  act(() => {
    hookHandleEvent!({ id: `evt-${Math.random()}`, ...event });
  });
}

function summary(messages: Message[]): string[] {
  return messages.map(
    (m) => `${m.type}${isPendingSteer(m) ? '(pending)' : ''}:${m.content.replace(/\s+/g, ' ').trim()}`,
  );
}

describe('steer placement', () => {
  let stateHolder: { current: Record<string, unknown> };

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    hookHandleEvent = null;
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    container?.remove();
  });

  function sendSteer(content: string): void {
    stateHolder.current = {
      ...stateHolder.current,
      messages: [...(stateHolder.current.messages as Message[]), pendingSteerBubble('steer-1', content)],
    };
  }

  it('keeps the running answer above a steer until the model receives it', () => {
    ({ stateHolder } = mountHook());
    stateHolder.current = {
      ...stateHolder.current,
      isProcessing: true,
      messages: [{ id: 'q1', type: 'user', content: 'Run the sleep, then say done', timestamp: new Date() }],
    };

    fire({ type: 'stream_chunk', data: { chunk: "I'll wait for the background t", content_type: 'assistant_text' } });
    fire({ type: 'query_progress', data: {} });
    sendSteer('Now reply with exactly: QUEUED-OK');
    fire({ type: 'stream_chunk', data: { chunk: 'ask to finish.', content_type: 'assistant_text' } });
    fire({ type: 'tool_start', data: { tool_call_id: 'tc-1', tool_name: 'shell_command', display_name: 'shell' } });
    fire({ type: 'tool_end', data: { tool_call_id: 'tc-1', tool_name: 'shell_command', status: 'completed' } });

    let messages = stateHolder.current.messages as Message[];
    expect(summary(messages)).toEqual([
      'user:Run the sleep, then say done',
      "assistant:I'll wait for the background task to finish. [executing tool [shell_command]]",
      'user(pending):Now reply with exactly: QUEUED-OK',
    ]);
    expect(messages[1].toolRefs?.map((r) => r.toolId)).toEqual(['tc-1']);

    fire({ type: 'steer_delivered', data: { content: 'Now reply with exactly: QUEUED-OK' } });
    fire({ type: 'stream_chunk', data: { chunk: 'QUEUED-OK', content_type: 'assistant_text' } });
    fire({ type: 'query_completed', data: { query: 'Run the sleep, then say done', response: 'QUEUED-OK' } });

    messages = stateHolder.current.messages as Message[];
    expect(summary(messages)).toEqual([
      'user:Run the sleep, then say done',
      "assistant:I'll wait for the background task to finish. [executing tool [shell_command]]",
      'user:Now reply with exactly: QUEUED-OK',
      'assistant:QUEUED-OK',
    ]);
  });

  it('a run that ends before taking the steer finishes its own answer above it', () => {
    ({ stateHolder } = mountHook());
    stateHolder.current = {
      ...stateHolder.current,
      isProcessing: true,
      messages: [{ id: 'q1', type: 'user', content: 'Say hi', timestamp: new Date() }],
    };
    sendSteer('and bye');
    fire({ type: 'query_completed', data: { query: 'Say hi', response: 'Hi.' } });

    expect(summary(stateHolder.current.messages as Message[])).toEqual([
      'user:Say hi',
      'assistant:Hi.',
      'user(pending):and bye',
    ]);
  });

  it('a turn started while a steer waits puts its question above the steer', () => {
    ({ stateHolder } = mountHook());
    stateHolder.current = {
      ...stateHolder.current,
      messages: [
        { id: 'q1', type: 'user', content: 'Say hi', timestamp: new Date() },
        { id: 'a1', type: 'assistant', content: 'Hi.', timestamp: new Date() },
      ],
    };
    sendSteer('and bye');
    fire({
      type: 'query_started',
      data: { query: '[wakeup] task done', display_query: "Looking into 'sleep'…", source: 'auto-resume' },
    });

    const messages = stateHolder.current.messages as Message[];
    expect(messages[messages.length - 1].id).toBe('steer-1');
    expect(isPendingSteer(messages[messages.length - 1])).toBe(true);
    expect(messages[messages.length - 2].type).toBe('user');
  });
});
