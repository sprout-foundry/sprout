/**
 * useWebSocketEventHandler.delegate.test.ts — Unit tests for delegate_activity
 * event handling in the WebSocket event handler.
 *
 * Verifies that delegate_activity events correctly create, update, and manage
 * DelegateActivity entries in the chat state.
 */
// @ts-nocheck — mock objects don't fully implement all interfaces

import { act, createElement, useState, MutableRefObject, MutableRefObject as Ref } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  error: vi.fn(),
}));

vi.mock('../utils/chatCompletion', () => ({
  ensureCompletedAssistantMessage: vi.fn((messages, response, createMsg) => {
    const last = messages[messages.length - 1];
    if (last?.type === 'assistant') return messages;
    return typeof response === 'string' && response.trim() ? [...messages, createMsg(response)] : messages;
  }),
}));

vi.mock('../utils/messageId', () => ({
  generateMessageId: vi.fn(() => `msg-${Date.now()}`),
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

import type { AppStoreSetState } from '../contexts/AppStore';
import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';
import type { WsEvent } from '@sprout/events';

// ---------------------------------------------------------------------------
// Minimal state (mirrors AppStore fields used by the handler)
// ---------------------------------------------------------------------------

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
    delegateActivities: [],
    activeChatId: null,
    chatSessions: [],
    perChatCache: {},
    securityApprovalRequest: null,
    securityPromptRequest: null,
    askUserRequest: null,
    driftNotification: null,
    modelSelectionRequest: null,
  };
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

let hookHandleEvent: ((event: WsEvent) => void) | null = null;

const HookWrapper = ({
  stateHolder,
  setStateMock,
  activeChatIdRef,
}: {
  stateHolder: { current: Record<string, unknown> };
  setStateMock: ReturnType<typeof vi.fn>;
  activeChatIdRef: MutableRefObject<string | null>;
}) => {
  const activeRequestsRef: MutableRefObject<number> = { current: 0 };
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

  const apiService = {
    getStats: vi.fn().mockResolvedValue({ provider: 'openai', model: 'gpt-4' }),
  };

  const { handleEvent } = useWebSocketEventHandler({
    setState: setStateMock as AppStoreSetState,
    refs,
    apiService,
  });

  hookHandleEvent = handleEvent;

  return createElement('div', null, 'hook host');
};

function setup(activeChatId: string | null = null) {
  const stateHolder = { current: createDefaultState() };
  const setStateMock = vi.fn((updater: unknown) => {
    if (typeof updater === 'function') {
      const prev = stateHolder.current;
      const next = updater(prev);
      stateHolder.current = { ...prev, ...next };
    } else {
      stateHolder.current = updater;
    }
  });
  const activeChatIdRef: MutableRefObject<string | null> = { current: activeChatId };

  act(() => {
    root.render(
      createElement(HookWrapper, {
        stateHolder,
        setStateMock,
        activeChatIdRef,
      }),
    );
  });

  return { stateHolder, setStateMock, activeChatIdRef };
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  hookHandleEvent = null;
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: delegate_activity
// ---------------------------------------------------------------------------

describe('delegate_activity', () => {
  it('creates new delegate on started event', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-1',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'Testing new feature',
          depth: 1,
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
          chat_id: 'chat-1',
        },
      });
    });

    expect(stateHolder.current.delegateActivities).toHaveLength(1);
    const delegate = stateHolder.current.delegateActivities[0];
    expect(delegate.delegateId).toBe('delegate-1');
    expect(delegate.action).toBe('started');
    expect(delegate.summary).toBe('Testing new feature');
    expect(delegate.depth).toBe(1);
    expect(delegate.tokensUsed).toBe(100);
    expect(delegate.cost).toBe(0.01);
    expect(delegate.toolsCalled).toEqual([]);
    expect(delegate.status).toBe('running');
  });

  it('accumulates tool calls across events for same delegate', () => {
    const { stateHolder } = setup();

    // Started event
    act(() => {
      hookHandleEvent!({
        id: 'evt-started',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'Testing',
          depth: 1,
          tokens_used: 50,
          cost: 0.005,
          tools_called: [],
        },
      });
    });

    // Tool call event
    act(() => {
      hookHandleEvent!({
        id: 'evt-tool',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tokens_used: 150,
          cost: 0.015,
          tools_called: [
            {
              tool_name: 'grep',
              input: 'search for foo',
              output: 'found 3 results',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 45,
              success: true,
            },
          ],
        },
      });
    });

    expect(stateHolder.current.delegateActivities).toHaveLength(1);
    const delegate = stateHolder.current.delegateActivities[0];
    expect(delegate.toolsCalled).toHaveLength(1);
    expect(delegate.toolsCalled[0].tool_name).toBe('grep');
    expect(delegate.tokensUsed).toBe(150);
    expect(delegate.cost).toBe(0.015);
  });

  it('marks delegate as completed', () => {
    const { stateHolder } = setup();

    // Start
    act(() => {
      hookHandleEvent!({
        id: 'evt-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
        },
      });
    });

    // Complete
    act(() => {
      hookHandleEvent!({
        id: 'evt-done',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'completed',
          summary: 'Done with testing',
          tokens_used: 200,
          cost: 0.02,
          tools_called: [],
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].status).toBe('completed');
    expect(stateHolder.current.delegateActivities[0].summary).toBe('Done with testing');
  });

  it('marks delegate as error', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          tools_called: [],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-err',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'error',
          summary: 'Failed to execute',
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].status).toBe('error');
    expect(stateHolder.current.delegateActivities[0].summary).toBe('Failed to execute');
  });

  it('accumulates tokens and cost across events', () => {
    const { stateHolder } = setup();

    // Started
    act(() => {
      hookHandleEvent!({
        id: 'evt-1',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
        },
      });
    });

    // Tool call
    act(() => {
      hookHandleEvent!({
        id: 'evt-2',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tokens_used: 200,
          cost: 0.02,
          tools_called: [
            {
              tool_name: 'grep',
              input: 'test',
              output: 'result',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 10,
              success: true,
            },
          ],
        },
      });
    });

    // Completed
    act(() => {
      hookHandleEvent!({
        id: 'evt-3',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'completed',
          tokens_used: 300,
          cost: 0.03,
          tools_called: [],
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].tokensUsed).toBe(300);
    expect(stateHolder.current.delegateActivities[0].cost).toBe(0.03);
  });

  it('tracks multiple delegates independently', () => {
    const { stateHolder } = setup();

    // Create first delegate
    act(() => {
      hookHandleEvent!({
        id: 'evt-d1-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'First delegate',
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
        },
      });
    });

    // Create second delegate
    act(() => {
      hookHandleEvent!({
        id: 'evt-d2-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-2',
          action: 'started',
          summary: 'Second delegate',
          tokens_used: 200,
          cost: 0.02,
          tools_called: [],
        },
      });
    });

    // Update only the second delegate
    act(() => {
      hookHandleEvent!({
        id: 'evt-d2-tool',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-2',
          action: 'tool_call',
          tokens_used: 300,
          cost: 0.03,
          tools_called: [
            {
              tool_name: 'cat',
              input: 'file.txt',
              output: 'contents',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 5,
              success: true,
            },
          ],
        },
      });
    });

    expect(stateHolder.current.delegateActivities).toHaveLength(2);

    // First delegate unchanged
    expect(stateHolder.current.delegateActivities[0].delegateId).toBe('delegate-1');
    expect(stateHolder.current.delegateActivities[0].tokensUsed).toBe(100);
    expect(stateHolder.current.delegateActivities[0].cost).toBe(0.01);
    expect(stateHolder.current.delegateActivities[0].toolsCalled).toHaveLength(0);

    // Second delegate updated
    expect(stateHolder.current.delegateActivities[1].delegateId).toBe('delegate-2');
    expect(stateHolder.current.delegateActivities[1].tokensUsed).toBe(300);
    expect(stateHolder.current.delegateActivities[1].cost).toBe(0.03);
    expect(stateHolder.current.delegateActivities[1].toolsCalled).toHaveLength(1);
  });

  it('preserves existing summary when not provided in update', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'Original summary',
          tools_called: [],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-tool',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tools_called: [
            {
              tool_name: 'ls',
              input: '',
              output: 'files',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 2,
              success: true,
            },
          ],
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].summary).toBe('Original summary');
  });

  it('preserves running status during intermediate events', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          tools_called: [],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-tool',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tools_called: [
            {
              tool_name: 'grep',
              input: 'foo',
              output: 'bar',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 10,
              success: true,
            },
          ],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-result',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_result',
          tools_called: [],
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].status).toBe('running');
  });

  it('appends tool calls from multiple tool_call events', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-start',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          tools_called: [],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-tool-1',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tools_called: [
            {
              tool_name: 'grep',
              input: 'foo',
              output: 'bar',
              timestamp: '2024-01-01T00:00:00Z',
              duration_ms: 10,
              success: true,
            },
          ],
        },
      });
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-tool-2',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'tool_call',
          tools_called: [
            {
              tool_name: 'cat',
              input: 'file.txt',
              output: 'contents',
              timestamp: '2024-01-01T00:00:01Z',
              duration_ms: 5,
              success: true,
            },
          ],
        },
      });
    });

    expect(stateHolder.current.delegateActivities[0].toolsCalled).toHaveLength(2);
    expect(stateHolder.current.delegateActivities[0].toolsCalled[0].tool_name).toBe('grep');
    expect(stateHolder.current.delegateActivities[0].toolsCalled[1].tool_name).toBe('cat');
  });

  it('is filtered when chat_id does not match active chat', () => {
    const { stateHolder } = setup('chat-active');

    // Send event for a different chat
    act(() => {
      hookHandleEvent!({
        id: 'evt-wrong-chat',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'Wrong chat',
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
          chat_id: 'chat-different',
        },
      });
    });

    // Should be filtered out
    expect(stateHolder.current.delegateActivities).toHaveLength(0);
  });

  it('does NOT filter when chat_id matches active chat', () => {
    const { stateHolder } = setup('chat-active');

    act(() => {
      hookHandleEvent!({
        id: 'evt-correct-chat',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'Correct chat',
          tokens_used: 100,
          cost: 0.01,
          tools_called: [],
          chat_id: 'chat-active',
        },
      });
    });

    expect(stateHolder.current.delegateActivities).toHaveLength(1);
  });

  it('does NOT filter when no active chat is set (no activeChatId)', () => {
    const { stateHolder } = setup(null);

    act(() => {
      hookHandleEvent!({
        id: 'evt-no-active-chat',
        type: 'delegate_activity',
        data: {
          delegate_id: 'delegate-1',
          action: 'started',
          summary: 'No active chat',
          tools_called: [],
        },
      });
    });

    // When no activeChatId is set, events should pass through
    expect(stateHolder.current.delegateActivities).toHaveLength(1);
  });
});
