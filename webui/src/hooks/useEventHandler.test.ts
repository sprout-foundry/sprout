/**
 * useEventHandler.test.ts — Unit tests for the useEventHandler hook.
 *
 * Covers:
 * - Event filtering (ping/webpack dev server events ignored)
 * - Per-chat filtering (events for inactive chat ignored)
 * - connection_status: isConnected and sessionId updates
 * - query_started: isProcessing, user message, queryCount
 * - stream_chunk: append to existing / create new assistant message
 * - query_completed: isProcessing, queryProgress, /clear command
 * - tool_start: create new tool execution, attach toolRef
 * - tool_end: update tool execution status, fallback creation
 * - error: lastError, error message, activeRequestsRef decrement
 * - security_approval_request: set approval dialog, skip status echo
 * - security_prompt_request: set prompt dialog, skip status echo / missing prompt
 * - subagent_activity: append activity, skip empty message
 * - file_changed: track file edits
 * - metrics_update: update provider/model/stats
 * - agent_message: categorise and render in chat
 * - unknown event types: default handler
 */
// @ts-nocheck — mock objects don't fully implement all interfaces

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  error: vi.fn(),
}));

vi.mock('../contexts/NotificationContext', () => ({
  useNotifications: () => ({ addNotification: vi.fn() }),
}));

vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
  appendClientIdToUrl: vi.fn((url: string) => url),
}));

vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
}));

vi.mock('../utils/chatCompletion', () => ({
  ensureCompletedAssistantMessage: vi.fn((messages, response, createMsg) => {
    // Minimal implementation: if there's an existing assistant message, return as-is;
    // otherwise append a new one.
    const last = messages[messages.length - 1];
    if (last?.type === 'assistant') return messages;
    return typeof response === 'string' && response.trim() ? [...messages, createMsg(response)] : messages;
  }),
}));

vi.mock('../utils/agentMessages', () => ({
  shouldSuppressAgentMessageInChat: vi.fn(() => false),
  extractToolNameFromToolLogTarget: vi.fn((target) => {
    if (!target) return null;
    const trimmed = target.trim();
    if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) return null;
    const inner = trimmed.slice(1, -1).trim();
    return inner.split(/\s+/, 1)[0] || null;
  }),
  normalizeTodoList: vi.fn((raw) => {
    if (!Array.isArray(raw)) return [];
    return raw.map((t, i) => ({
      id: String(t.id ?? i),
      content: String(t.content ?? ''),
      status: t.status || 'pending',
    }));
  }),
}));

// Static import — Vitest hoists vi.mock above all imports automatically
import type { AppState } from '../types/app';
import { useEventHandler } from './useEventHandler';
import type { UseEventHandlerOptions, UseEventHandlerReturn } from './useEventHandler';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createDefaultState(overrides?: Partial<AppState>): AppState {
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
  delegateActivities: [],
    activeChatId: null,
    chatSessions: [],
    perChatCache: {},
    securityApprovalRequest: null,
    securityPromptRequest: null,
    ...overrides,
  };
}

/**
 * Set up the handler with a state-holder that lets us inspect setState results.
 * Returns control handles for making assertions.
 */
function setupHandler(overrides?: Partial<UseEventHandlerOptions>) {
  const stateHolder = { current: createDefaultState() };
  const setStateMock = vi.fn((updater: unknown) => {
    if (typeof updater === 'function') {
      const prev = stateHolder.current;
      const next = updater(prev);
      // Merge partial state like React does — preserve fields not in the updater result
      stateHolder.current = { ...prev, ...next };
    } else {
      stateHolder.current = updater;
    }
  });

  const activeChatIdRef = { current: null } as { current: string | null };
  const activeRequestsRef = { current: 0 } as { current: number };
  const connectionTimeoutRef = { current: null } as { current: ReturnType<typeof setTimeout> | null };
  const lastConnectionStateRef = { current: false } as { current: boolean };
  const queuedMessagesRef = { current: [] } as { current: string[] };
  const setQueuedMessages = vi.fn();

  const options: UseEventHandlerOptions = {
    setState: setStateMock,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
    queuedMessagesRef,
    setQueuedMessages,
    ...overrides,
  };

  return {
    stateHolder,
    setStateMock,
    activeChatIdRef,
    activeRequestsRef,
    connectionTimeoutRef,
    lastConnectionStateRef,
    queuedMessagesRef,
    setQueuedMessages,
    options,
  };
}

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: Event Filtering
// ---------------------------------------------------------------------------

describe('event filtering — ping/webpack dev server events', () => {
  it.each(['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'ping'])(
    'ignores "%s" event (no setState call)',
    (eventType) => {
      const { setStateMock } = setupHandler();

      act(() => {
        root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
      });

      const { handleEvent } = getHandleEvent();

      act(() => {
        handleEvent({ type: eventType, data: {} });
      });

      expect(setStateMock).not.toHaveBeenCalled();
    },
  );
});

// ---------------------------------------------------------------------------
// Tests: Per-chat filtering
// ---------------------------------------------------------------------------

describe('per-chat filtering', () => {
  const perChatEvents = [
    'query_started',
    'stream_chunk',
    'query_completed',
    'query_progress',
    'tool_start',
    'tool_end',
    'todo_update',
    'subagent_activity',
    'agent_message',
    'error',
  ];

  it.each(perChatEvents)('filters out "%s" when chat_id does not match activeChatId', (eventType) => {
    const { setStateMock, activeChatIdRef } = setupHandler();
    activeChatIdRef.current = 'chat-1';

    const wrapper = setupHandler({ setState: setStateMock, activeChatIdRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: eventType, data: { chat_id: 'chat-2' } });
    });

    expect(setStateMock).not.toHaveBeenCalled();
  });

  it('allows per-chat event when chat_id matches activeChatId', () => {
    const { setStateMock, activeChatIdRef, stateHolder } = setupHandler();
    activeChatIdRef.current = 'chat-1';

    const wrapper = setupHandler({ setState: setStateMock, activeChatIdRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_started', data: { chat_id: 'chat-1', query: 'hello' } });
    });

    expect(setStateMock).toHaveBeenCalled();
    expect(stateHolder.current.isProcessing).toBe(true);
  });

  it('allows per-chat event when no chat_id is present (no filtering)', () => {
    const { setStateMock, activeChatIdRef, stateHolder } = setupHandler();
    activeChatIdRef.current = 'chat-1';

    const wrapper = setupHandler({ setState: setStateMock, activeChatIdRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_started', data: { query: 'hello' } });
    });

    expect(setStateMock).toHaveBeenCalled();
    expect(stateHolder.current.isProcessing).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: connection_status
// ---------------------------------------------------------------------------

describe('connection_status', () => {
  it('schedules setState with isConnected=true and sessionId on connection', async () => {
    vi.useFakeTimers();
    const { setStateMock, stateHolder, lastConnectionStateRef, connectionTimeoutRef } = setupHandler();
    const wrapper = setupHandler({ setState: setStateMock, lastConnectionStateRef, connectionTimeoutRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'connection_status',
        data: { connected: true, session_id: 'ws-123' },
      });
    });

    // The setState is scheduled via setTimeout(300ms) — advance timers
    act(() => {
      vi.runOnlyPendingTimers();
    });

    // The debounced setState should have been called
    // We verify the ref was updated
    expect(lastConnectionStateRef.current).toBe(true);
    vi.useRealTimers();
  });

  it('does not schedule setState when connection state has not changed', () => {
    const { setStateMock, lastConnectionStateRef, connectionTimeoutRef } = setupHandler();
    lastConnectionStateRef.current = true;

    const wrapper = setupHandler({ setState: setStateMock, lastConnectionStateRef, connectionTimeoutRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'connection_status',
        data: { connected: true, session_id: 'ws-123' },
      });
    });

    // setState should NOT have been called because state didn't change
    expect(setStateMock).not.toHaveBeenCalled();
    expect(connectionTimeoutRef.current).toBe(null);
  });

  it('skips events with a different client_id', () => {
    const { setStateMock, lastConnectionStateRef } = setupHandler();

    const wrapper = setupHandler({ setState: setStateMock, lastConnectionStateRef });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'connection_status',
        data: { connected: true, client_id: 'other-client' },
      });
    });

    // Skipped due to client_id mismatch
    expect(setStateMock).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: query_started
// ---------------------------------------------------------------------------

describe('query_started', () => {
  it('sets isProcessing=true, adds user message, and increments queryCount', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'query_started',
        data: { query: 'Explain React hooks' },
      });
    });

    expect(stateHolder.current.isProcessing).toBe(true);
    expect(stateHolder.current.queryCount).toBe(1);
    expect(stateHolder.current.messages).toHaveLength(1);
    expect(stateHolder.current.messages[0].type).toBe('user');
    expect(stateHolder.current.messages[0].content).toBe('Explain React hooks');
    expect(stateHolder.current.lastError).toBe(null);
    expect(stateHolder.current.queryProgress).toBe(null);
    expect(stateHolder.current.currentTodos).toEqual([]);
    expect(stateHolder.current.fileEdits).toEqual([]);
    expect(stateHolder.current.subagentActivities).toEqual([]);
  });

  it('clears previous state (fileEdits, subagentActivities, queryProgress, currentTodos)', () => {
    const initState = createDefaultState({
      fileEdits: [{ path: 'foo.ts', action: 'edited', timestamp: new Date() }],
      subagentActivities: [
        { id: '1', toolCallId: '', toolName: '', phase: 'output' as const, message: '', timestamp: new Date() },
      ],
      queryProgress: { step: 2 },
      currentTodos: [{ id: '1', content: 'test', status: 'completed' as const }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_started', data: { query: 'new query' } });
    });

    expect(stateHolder.current.fileEdits).toEqual([]);
    expect(stateHolder.current.subagentActivities).toEqual([]);
    expect(stateHolder.current.queryProgress).toBe(null);
    expect(stateHolder.current.currentTodos).toEqual([]);
  });

  it('handles empty query gracefully', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_started', data: {} });
    });

    expect(stateHolder.current.messages[0].content).toBe('');
    expect(stateHolder.current.isProcessing).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: stream_chunk
// ---------------------------------------------------------------------------

describe('stream_chunk', () => {
  it('appends content to existing assistant message', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'stream_chunk', data: { chunk: ' World' } });
    });

    expect(stateHolder.current.messages).toHaveLength(1);
    expect(stateHolder.current.messages[0].content).toBe('Hello World');
  });

  it('creates new assistant message when no existing assistant message', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'user', content: 'Hi', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'stream_chunk', data: { chunk: 'New response' } });
    });

    expect(stateHolder.current.messages).toHaveLength(2);
    expect(stateHolder.current.messages[1].type).toBe('assistant');
    expect(stateHolder.current.messages[1].content).toBe('New response');
  });

  it('appends reasoning chunks to the reasoning field', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: '', reasoning: '', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'stream_chunk',
        data: { chunk: 'thinking...', content_type: 'reasoning' },
      });
    });

    expect(stateHolder.current.messages[0].reasoning).toBe('thinking...');
    expect(stateHolder.current.messages[0].content).toBe('');
  });

  it('creates new assistant message with reasoning when content_type=reasoning and no existing message', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'stream_chunk',
        data: { chunk: 'Let me think', content_type: 'reasoning' },
      });
    });

    expect(stateHolder.current.messages).toHaveLength(1);
    expect(stateHolder.current.messages[0].type).toBe('assistant');
    expect(stateHolder.current.messages[0].reasoning).toBe('Let me think');
    expect(stateHolder.current.messages[0].content).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Tests: query_completed
// ---------------------------------------------------------------------------

describe('query_completed', () => {
  it('decrements activeRequestsRef and sets isProcessing based on remaining requests', () => {
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    activeRequestsRef.current = 2;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_completed', data: { query: 'test', response: 'done' } });
    });

    expect(activeRequestsRef.current).toBe(1);
    expect(stateHolder.current.isProcessing).toBe(true); // 1 > 0
    expect(stateHolder.current.queryProgress).toBe(null);
    expect(stateHolder.current.lastError).toBe(null);
  });

  it('sets isProcessing=false when no active requests remain', () => {
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    activeRequestsRef.current = 1;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_completed', data: { query: 'test', response: 'done' } });
    });

    expect(activeRequestsRef.current).toBe(0);
    expect(stateHolder.current.isProcessing).toBe(false);
  });

  it('clears messages on /clear command', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'user', content: 'hello', timestamp: new Date() }],
      currentTodos: [{ id: '1', content: 'test', status: 'pending' as const }],
    });
    const {
      setStateMock,
      stateHolder,
      queuedMessagesRef,
      setQueuedMessages,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
    } = setupHandler();
    stateHolder.current = initState;
    queuedMessagesRef.current = ['msg1', 'msg2'];

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_completed', data: { query: '/clear', response: '' } });
    });

    expect(stateHolder.current.messages).toEqual([]);
    expect(stateHolder.current.currentTodos).toEqual([]);
    expect(stateHolder.current.toolExecutions).toEqual([]);
    expect(queuedMessagesRef.current).toEqual([]);
    expect(setQueuedMessages).toHaveBeenCalledWith([]);
  });

  it('marks pending tool executions as completed', () => {
    const initState = createDefaultState({
      toolExecutions: [
        { id: 't1', tool: 'read_file', status: 'started' as const, startTime: new Date() },
        { id: 't2', tool: 'write_file', status: 'completed' as const, startTime: new Date(), endTime: new Date() },
      ],
    });
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;
    activeRequestsRef.current = 1;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_completed', data: { query: 'test', response: '' } });
    });

    const tools = stateHolder.current.toolExecutions;
    expect(tools[0].status).toBe('completed');
    expect(tools[0].endTime).toBeDefined();
    expect(tools[1].status).toBe('completed'); // already completed, unchanged
  });

  it('preserves security dialogs on query_completed (non-clear)', () => {
    const initState = createDefaultState({
      securityApprovalRequest: { requestId: 'r1', toolName: 'shell', riskLevel: 'HIGH', reasoning: 'test' },
      securityPromptRequest: { requestId: 'p1', prompt: 'Confirm?' },
    });
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;
    activeRequestsRef.current = 1;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'query_completed', data: { query: 'test', response: '' } });
    });

    expect(stateHolder.current.securityApprovalRequest?.requestId).toBe('r1');
    expect(stateHolder.current.securityPromptRequest?.requestId).toBe('p1');
  });
});

// ---------------------------------------------------------------------------
// Tests: tool_start
// ---------------------------------------------------------------------------

describe('tool_start', () => {
  it('creates new tool execution and attaches toolRef to last assistant message', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Thinking...', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: {
          tool_call_id: 'tc-1',
          tool_name: 'read_file',
          display_name: 'Read file',
          arguments: '{ "path": "foo.txt" }',
        },
      });
    });

    expect(stateHolder.current.toolExecutions).toHaveLength(1);
    const tool = stateHolder.current.toolExecutions[0];
    expect(tool.id).toBe('tc-1');
    expect(tool.tool).toBe('read_file');
    expect(tool.status).toBe('started');
    expect(tool.message).toBe('Read file');
    expect(tool.arguments).toBe('{ "path": "foo.txt" }');

    // Check toolRef attached to assistant message
    const msg = stateHolder.current.messages[0];
    expect(msg.toolRefs).toHaveLength(1);
    expect(msg.toolRefs![0].toolId).toBe('tc-1');
    expect(msg.toolRefs![0].toolName).toBe('read_file');
  });

  it('creates tool execution without assistant message (no toolRef attached)', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: {
          tool_call_id: 'tc-2',
          tool_name: 'shell',
          display_name: 'Run shell',
        },
      });
    });

    expect(stateHolder.current.toolExecutions).toHaveLength(1);
    expect(stateHolder.current.toolExecutions[0].id).toBe('tc-2');
    expect(stateHolder.current.messages).toEqual([]);
  });

  it('sets subagentType=parallel for subagent_type=parallel', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: {
          tool_call_id: 'tc-3',
          tool_name: 'run_subagent',
          is_subagent: true,
          subagent_type: 'parallel',
        },
      });
    });

    expect(stateHolder.current.toolExecutions[0].subagentType).toBe('parallel');
  });

  it('sets subagentType=single for is_subagent=true without subagent_type', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: {
          tool_call_id: 'tc-4',
          tool_name: 'run_subagent',
          is_subagent: true,
        },
      });
    });

    expect(stateHolder.current.toolExecutions[0].subagentType).toBe('single');
  });

  it('updates existing tool execution by tool_call_id', () => {
    const initState = createDefaultState({
      toolExecutions: [
        {
          id: 'tc-1',
          tool: 'read_file',
          status: 'started' as const,
          startTime: new Date(),
          details: { tool_call_id: 'tc-1' },
        },
      ],
      messages: [{ id: '1', type: 'assistant', content: '', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: {
          tool_call_id: 'tc-1',
          tool_name: 'read_file',
          display_name: 'Read file updated',
          arguments: '{ "path": "bar.txt" }',
        },
      });
    });

    // Should still be 1 tool (updated in place)
    expect(stateHolder.current.toolExecutions).toHaveLength(1);
    expect(stateHolder.current.toolExecutions[0].message).toBe('Read file updated');
  });

  it('caps tool executions at MAX_TOOL_EXECUTIONS (200)', () => {
    // Create 201 tool executions so the cap is tested
    const tools = Array.from({ length: 201 }, (_, i) => ({
      id: `t-${i}`,
      tool: `tool-${i}`,
      status: 'completed' as const,
      startTime: new Date(),
      endTime: new Date(),
      details: { tool_call_id: `tc-${i}` },
    }));
    const initState = createDefaultState({ toolExecutions: tools });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_start',
        data: { tool_call_id: 'tc-new', tool_name: 'new_tool' },
      });
    });

    // Should be capped at 200 (drops the oldest one)
    expect(stateHolder.current.toolExecutions).toHaveLength(200);
  });
});

// ---------------------------------------------------------------------------
// Tests: tool_end
// ---------------------------------------------------------------------------

describe('tool_end', () => {
  it('updates existing tool execution by tool_call_id', () => {
    const initState = createDefaultState({
      toolExecutions: [
        {
          id: 'tc-1',
          tool: 'read_file',
          status: 'started' as const,
          startTime: new Date(),
          details: { tool_call_id: 'tc-1' },
        },
      ],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_end',
        data: {
          tool_call_id: 'tc-1',
          tool_name: 'read_file',
          result: 'file content',
        },
      });
    });

    expect(stateHolder.current.toolExecutions).toHaveLength(1);
    expect(stateHolder.current.toolExecutions[0].status).toBe('completed');
    expect(stateHolder.current.toolExecutions[0].endTime).toBeDefined();
    expect(stateHolder.current.toolExecutions[0].result).toBe('file content');
  });

  it('sets status=error for failed tools', () => {
    const initState = createDefaultState({
      toolExecutions: [
        {
          id: 'tc-1',
          tool: 'shell',
          status: 'started' as const,
          startTime: new Date(),
          details: { tool_call_id: 'tc-1' },
        },
      ],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_end',
        data: {
          tool_call_id: 'tc-1',
          tool_name: 'shell',
          status: 'failed',
          error: 'exit code 1',
        },
      });
    });

    expect(stateHolder.current.toolExecutions[0].status).toBe('error');
  });

  it('creates fallback tool execution when no existing match', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_end',
        data: {
          tool_call_id: 'tc-orphan',
          tool_name: 'read_file',
          result: 'orphaned result',
        },
      });
    });

    expect(stateHolder.current.toolExecutions).toHaveLength(1);
    expect(stateHolder.current.toolExecutions[0].id).toBe('tc-orphan');
    expect(stateHolder.current.toolExecutions[0].status).toBe('completed');
  });

  it('preserves arguments from tool_start', () => {
    const initState = createDefaultState({
      toolExecutions: [
        {
          id: 'tc-1',
          tool: 'read_file',
          status: 'started' as const,
          startTime: new Date(),
          arguments: '{ "path": "foo.txt" }',
          details: { tool_call_id: 'tc-1' },
        },
      ],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'tool_end',
        data: { tool_call_id: 'tc-1', result: 'ok' },
      });
    });

    expect(stateHolder.current.toolExecutions[0].arguments).toBe('{ "path": "foo.txt" }');
  });
});

// ---------------------------------------------------------------------------
// Tests: error event
// ---------------------------------------------------------------------------

describe('error', () => {
  it('sets lastError, adds error message, and decrements activeRequestsRef', () => {
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    activeRequestsRef.current = 1;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'error',
        data: { message: 'Connection timeout' },
      });
    });

    expect(stateHolder.current.lastError).toBe('Connection timeout');
    expect(activeRequestsRef.current).toBe(0);
    expect(stateHolder.current.isProcessing).toBe(false);
    expect(stateHolder.current.messages).toHaveLength(1);
    expect(stateHolder.current.messages[0].type).toBe('assistant');
    expect(stateHolder.current.messages[0].content).toContain('[FAIL] Error: Connection timeout');
    expect(stateHolder.current.queryProgress).toBe(null);
  });

  it('uses "Unknown error" when no message provided', () => {
    const {
      setStateMock,
      stateHolder,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    activeRequestsRef.current = 0;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeRequestsRef,
      activeChatIdRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'error', data: {} });
    });

    expect(stateHolder.current.lastError).toBe('Unknown error');
  });
});

// ---------------------------------------------------------------------------
// Tests: security_approval_request
// ---------------------------------------------------------------------------

describe('security_approval_request', () => {
  it('sets securityApprovalRequest state', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_approval_request',
        data: {
          request_id: 'req-1',
          tool_name: 'shell',
          risk_level: 'HIGH',
          reasoning: 'Potentially dangerous command',
          command: 'rm -rf /',
          risk_type: 'file_deletion',
          target: '/etc',
        },
      });
    });

    expect(stateHolder.current.securityApprovalRequest).not.toBeNull();
    expect(stateHolder.current.securityApprovalRequest?.requestId).toBe('req-1');
    expect(stateHolder.current.securityApprovalRequest?.toolName).toBe('shell');
    expect(stateHolder.current.securityApprovalRequest?.riskLevel).toBe('HIGH');
    expect(stateHolder.current.securityApprovalRequest?.command).toBe('rm -rf /');
    expect(stateHolder.current.securityApprovalRequest?.riskType).toBe('file_deletion');
    expect(stateHolder.current.securityApprovalRequest?.target).toBe('/etc');
  });

  it('skips status echo events (status=responded)', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_approval_request',
        data: { status: 'responded', request_id: 'req-1' },
      });
    });

    // Should not call setState (early break)
    expect(setStateMock).not.toHaveBeenCalled();
  });

  it('uses default values when fields are missing', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_approval_request',
        data: { request_id: 'req-2' },
      });
    });

    expect(stateHolder.current.securityApprovalRequest?.requestId).toBe('req-2');
    expect(stateHolder.current.securityApprovalRequest?.toolName).toBe('');
    expect(stateHolder.current.securityApprovalRequest?.riskLevel).toBe('CAUTION');
    expect(stateHolder.current.securityApprovalRequest?.reasoning).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Tests: security_prompt_request
// ---------------------------------------------------------------------------

describe('security_prompt_request', () => {
  it('sets securityPromptRequest state', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_prompt_request',
        data: {
          request_id: 'prompt-1',
          prompt: 'Are you sure you want to write to this file?',
          file_path: '/etc/passwd',
          concern: 'sensitive_file',
        },
      });
    });

    expect(stateHolder.current.securityPromptRequest).not.toBeNull();
    expect(stateHolder.current.securityPromptRequest?.requestId).toBe('prompt-1');
    expect(stateHolder.current.securityPromptRequest?.prompt).toBe('Are you sure you want to write to this file?');
    expect(stateHolder.current.securityPromptRequest?.filePath).toBe('/etc/passwd');
    expect(stateHolder.current.securityPromptRequest?.concern).toBe('sensitive_file');
  });

  it('skips status echo events (status=responded)', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_prompt_request',
        data: { status: 'responded', request_id: 'prompt-1' },
      });
    });

    expect(setStateMock).not.toHaveBeenCalled();
  });

  it('skips events without a prompt', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'security_prompt_request',
        data: { request_id: 'prompt-2' },
      });
    });

    // No prompt field → should break early without setState
    expect(setStateMock).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: subagent_activity
// ---------------------------------------------------------------------------

describe('subagent_activity', () => {
  it('appends subagent activity with correct fields', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-1',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-sub-1',
          tool_name: 'run_subagent',
          phase: 'spawn',
          message: 'Spawning subagent for file analysis',
          task_id: 'task-1',
          persona: 'file-analyst',
          is_parallel: true,
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    const activity = stateHolder.current.subagentActivities[0];
    expect(activity.toolCallId).toBe('tc-sub-1');
    expect(activity.phase).toBe('spawn');
    expect(activity.message).toBe('Spawning subagent for file analysis');
    expect(activity.isParallel).toBe(true);
  });

  it('skips empty message activities', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-sub-2',
          phase: 'output',
          message: '',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(0);
  });

  it('captures status field when present in event data', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-status',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-status',
          tool_name: 'run_subagent',
          phase: 'complete',
          message: 'Subagent completed successfully',
          task_id: 'task-1',
          status: 'completed',
          failures: 0,
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    const activity = stateHolder.current.subagentActivities[0];
    expect(activity.status).toBe('completed');
    expect(activity.failures).toBe(0);
  });

  it('handles status=started in subagent activity', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-started',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-started',
          tool_name: 'run_subagent',
          phase: 'spawn',
          message: 'Subagent started',
          task_id: 'task-2',
          status: 'started',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    expect(stateHolder.current.subagentActivities[0].status).toBe('started');
  });

  it('handles status=queued in subagent activity', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-queued',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-queued',
          tool_name: 'run_subagent',
          phase: 'spawn',
          message: 'Subagent queued',
          task_id: 'task-3',
          status: 'queued',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    expect(stateHolder.current.subagentActivities[0].status).toBe('queued');
  });

  it('handles status=cancelled in subagent activity', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-cancelled',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-cancelled',
          tool_name: 'run_subagent',
          phase: 'output',
          message: 'Subagent cancelled by user',
          task_id: 'task-4',
          status: 'cancelled',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    expect(stateHolder.current.subagentActivities[0].status).toBe('cancelled');
  });

  it('sets status to undefined when status field is absent from event data', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        id: 'evt-no-status',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-nostatus',
          tool_name: 'run_subagent',
          phase: 'output',
          message: 'Activity without status field',
          task_id: 'task-5',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(1);
    expect(stateHolder.current.subagentActivities[0].status).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Tests: file_changed
// ---------------------------------------------------------------------------

describe('file_changed', () => {
  it('tracks file edits with correct fields', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'file_changed',
        data: {
          path: 'src/main.ts',
          action: 'edit',
          lines_added: 5,
          lines_deleted: 2,
        },
      });
    });

    expect(stateHolder.current.fileEdits).toHaveLength(1);
    expect(stateHolder.current.fileEdits[0].path).toBe('src/main.ts');
    expect(stateHolder.current.fileEdits[0].action).toBe('edit');
    expect(stateHolder.current.fileEdits[0].linesAdded).toBe(5);
    expect(stateHolder.current.fileEdits[0].linesDeleted).toBe(2);
  });

  it('falls back to file_path when path is missing', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'file_changed',
        data: {
          file_path: 'backup.txt',
          action: 'created',
        },
      });
    });

    expect(stateHolder.current.fileEdits[0].path).toBe('backup.txt');
  });

  it('caps file edits at 50', () => {
    // Create 50 existing file edits
    const edits = Array.from({ length: 50 }, (_, i) => ({
      path: `file-${i}`,
      action: 'edit',
      timestamp: new Date(),
    }));
    const initState = createDefaultState({ fileEdits: edits });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'file_changed', data: { path: 'new-file', action: 'edit' } });
    });

    expect(stateHolder.current.fileEdits).toHaveLength(50);
    expect(stateHolder.current.fileEdits[49].path).toBe('new-file');
  });
});

// ---------------------------------------------------------------------------
// Tests: metrics_update
// ---------------------------------------------------------------------------

describe('metrics_update', () => {
  it('updates provider, model, and stats', () => {
    const initState = createDefaultState({
      provider: 'old-provider',
      model: 'old-model',
      stats: { tokens: 100 },
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'metrics_update',
        data: {
          provider: 'openai',
          model: 'gpt-4',
          tokens_used: 500,
        },
      });
    });

    expect(stateHolder.current.provider).toBe('openai');
    expect(stateHolder.current.model).toBe('gpt-4');
    expect(stateHolder.current.stats).toEqual({
      tokens: 100,
      provider: 'openai',
      model: 'gpt-4',
      tokens_used: 500,
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: todo_update
// ---------------------------------------------------------------------------

describe('todo_update', () => {
  it('updates currentTodos with normalized todo list', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'todo_update',
        data: {
          todos: [
            { id: '1', content: 'Task 1', status: 'completed' },
            { id: '2', content: 'Task 2', status: 'in_progress' },
          ],
        },
      });
    });

    expect(stateHolder.current.currentTodos).toHaveLength(2);
    expect(stateHolder.current.currentTodos[0].content).toBe('Task 1');
    expect(stateHolder.current.currentTodos[1].status).toBe('in_progress');
  });

  it('returns empty array for non-array todos', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({ type: 'todo_update', data: { todos: null } });
    });

    expect(stateHolder.current.currentTodos).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Tests: agent_message
// ---------------------------------------------------------------------------

describe('agent_message', () => {
  it('renders info_rendered messages in chat as Info:', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'info_rendered', message: '[OK] Task completed' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Info: [OK] Task completed');
  });

  it('renders error messages in chat as Warning:', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'error', message: 'Something went wrong' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Warning: Something went wrong');
  });

  it('renders warning messages in chat as Note:', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'warning', message: 'Warning message' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Note: Warning message');
  });

  it('auto-classifies [FAIL] messages as error', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'info', message: '[FAIL] Something failed' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Warning: [FAIL] Something failed');
  });

  it('auto-classifies [WARN] messages as warning', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'info', message: '[WARN] Watch out' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Note: [WARN] Watch out');
  });

  it('auto-classifies [OK] messages as info_rendered', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'info', message: '[OK] All good' },
      });
    });

    expect(stateHolder.current.messages[0].content).toContain('Info: [OK] All good');
  });

  it('does not render plain info messages in chat', () => {
    const initState = createDefaultState({
      messages: [{ id: '1', type: 'assistant', content: 'Hello', timestamp: new Date() }],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'info', message: 'Just a regular info message' },
      });
    });

    // Plain info messages are silently skipped — no setState call
    expect(setStateMock).not.toHaveBeenCalled();
  });

  it('does not append to chat when no assistant message exists', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: { category: 'error', message: 'Error without assistant message' },
      });
    });

    // setState is called but messages should remain empty (no assistant to append to)
    expect(stateHolder.current.messages).toEqual([]);
  });

  it('handles tool_log category and marks tool as running', () => {
    const initState = createDefaultState({
      toolExecutions: [
        {
          id: 't1',
          tool: 'read_file',
          status: 'started' as const,
          startTime: new Date(),
          details: { tool_call_id: 'tc-1' },
        },
      ],
    });
    const {
      setStateMock,
      stateHolder,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    } = setupHandler();
    stateHolder.current = initState;

    const wrapper = setupHandler({
      setState: setStateMock,
      activeChatIdRef,
      activeRequestsRef,
      connectionTimeoutRef,
      lastConnectionStateRef,
      queuedMessagesRef,
      setQueuedMessages,
    });

    act(() => {
      root.render(createElement(HookWrapper, { options: wrapper.options }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'agent_message',
        data: {
          category: 'tool_log',
          message: 'reading file',
          action: 'executing tool',
          target: '[read_file /path/to/file]',
        },
      });
    });

    expect(stateHolder.current.toolExecutions[0].status).toBe('running');
  });
});

// ---------------------------------------------------------------------------
// Tests: terminal_output
// ---------------------------------------------------------------------------

describe('terminal_output', () => {
  it('adds log entry without modifying other state', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'terminal_output',
        data: { output: 'build completed' },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].type).toBe('terminal_output');
    expect(stateHolder.current.messages).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Tests: query_progress
// ---------------------------------------------------------------------------

describe('query_progress', () => {
  it('sets queryProgress from event data', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'query_progress',
        data: { message: 'Writing code...', details: { step: 'writing_code', progress: 0.75 } },
      });
    });

    expect(stateHolder.current.queryProgress).toEqual({
      message: 'Writing code...',
      details: { step: 'writing_code', progress: 0.75 },
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Unknown event types (default handler)
// ---------------------------------------------------------------------------

describe('unknown event types', () => {
  it('adds log entry for unrecognised event types', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    act(() => {
      handleEvent({
        type: 'some_unknown_event',
        data: { foo: 'bar' },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].type).toBe('some_unknown_event');
  });
});

// ---------------------------------------------------------------------------
// Tests: file_content_changed
// ---------------------------------------------------------------------------

describe('file_content_changed', () => {
  it('dispatches file_externally_modified DOM event and adds log', () => {
    const { setStateMock, stateHolder } = setupHandler();

    act(() => {
      root.render(createElement(HookWrapper, { options: setupHandlerInner(setStateMock) }));
    });

    const { handleEvent } = getHandleEvent();

    const dispatchedEvent = { captured: false } as { captured: boolean; detail?: unknown };
    const handler = (e: Event) => {
      dispatchedEvent.captured = true;
      dispatchedEvent.detail = (e as CustomEvent).detail;
    };
    document.addEventListener('file_externally_modified', handler);

    act(() => {
      handleEvent({
        type: 'file_content_changed',
        data: { file_path: 'src/foo.ts', mod_time: 12345, size: 1024 },
      });
    });

    document.removeEventListener('file_externally_modified', handler);

    expect(dispatchedEvent.captured).toBe(true);
    expect(stateHolder.current.logs).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// Test wrapper components and helpers
// ---------------------------------------------------------------------------

/** Wrapper component that calls useEventHandler with provided options. */
function HookWrapper({ options }: { options: UseEventHandlerOptions }) {
  const hookReturn: UseEventHandlerReturn = useEventHandler(options);
  // Store handleEvent on a global for test access
  (window as any).__testEventHandler = hookReturn.handleEvent;
  return null;
}

/** Helper to get handleEvent from the window object after rendering. */
function getHandleEvent(): { handleEvent: (event: any) => void } {
  return { handleEvent: (window as any).__testEventHandler };
}

/** Minimal setup returning only setState for simple tests. */
function setupHandlerInner(setStateMock: ReturnType<typeof vi.fn>) {
  return setupHandler({ setState: setStateMock }).options;
}
