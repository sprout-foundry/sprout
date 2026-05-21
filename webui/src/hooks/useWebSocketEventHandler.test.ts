/**
 * useWebSocketEventHandler.test.ts — Unit tests for subagent_activity
 * status field handling in the WebSocket event handler (SP-037-3c).
 *
 * These tests verify that handleSubagentActivity correctly captures
 * the status field from event data into SubagentActivity objects.
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
// Tests: subagent_activity status field
// ---------------------------------------------------------------------------

describe('subagent_activity', () => {
  it('captures status=completed when present in event data', () => {
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-1',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-1',
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

  it('captures status=started from event data', () => {
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
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

  it('captures status=queued from event data', () => {
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
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

  it('captures status=cancelled from event data', () => {
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-nostatus',
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

  it('skips empty message activities (only logs, no subagentActivities)', () => {
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
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
        }),
      );
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-empty',
        type: 'subagent_activity',
        data: {
          tool_call_id: 'tc-empty',
          phase: 'output',
          message: '',
        },
      });
    });

    expect(stateHolder.current.subagentActivities).toHaveLength(0);
  });
});
