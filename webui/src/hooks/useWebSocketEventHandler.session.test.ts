/**
 * useWebSocketEventHandler.session_test.ts — Tests for the session_terminated
 * event handler and the four log-only handlers (delegate_clarification_*,
 * workspace_patch, recall_diagnostic).
 *
 * Verifies that:
 * - session_terminated resets activeRequestsRef, sets lastError, and stops
 *   the running spinner by clearing isProcessing and queryProgress.
 * - The four log-only handlers produce a log entry without falling through
 *   to the default "unknown event" warning.
 */
// @ts-nocheck — mock objects don't fully implement all interfaces

import type { MutableRefObject } from 'react';
import { act, createElement, useState, MutableRefObject as Ref } from 'react';
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

vi.mock('../services/lspClientService', () => ({
  LSPClientService: {
    getInstance: vi.fn(() => ({ cleanup: vi.fn() })),
  },
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
    passwordRequest: null,
    editApprovalRequest: null,
    driftNotification: null,
    modelSelectionRequest: null,
    outputVerbosity: 'default' as const,
  };
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

let hookHandleEvent: ((event: WsEvent) => void) | null = null;

const HookWrapper = ({
  stateHolder,
  setStateMock,
  activeChatIdRef,
  activeRequestsRef,
}: {
  stateHolder: { current: Record<string, unknown> };
  setStateMock: ReturnType<typeof vi.fn>;
  activeChatIdRef: MutableRefObject<string | null>;
  activeRequestsRef: MutableRefObject<number>;
}) => {
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
// Tests: session_terminated handler
// ---------------------------------------------------------------------------

describe('session_terminated', () => {
  function setup(opts: { initialProcessing?: boolean; initialActiveRequests?: number; lastError?: string | null }) {
    const stateHolder = {
      current: {
        ...createDefaultState(),
        isProcessing: opts.initialProcessing ?? false,
        lastError: opts.lastError ?? null,
      },
    };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    const activeRequestsRef: MutableRefObject<number> = {
      current: opts.initialActiveRequests ?? 0,
    };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          activeRequestsRef,
        }),
      );
    });

    return { stateHolder, setStateMock, activeRequestsRef };
  }

  it('resets activeRequestsRef to 0 when there is an in-flight request', () => {
    const { stateHolder, activeRequestsRef } = setup({
      initialProcessing: true,
      initialActiveRequests: 1,
    });

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-1',
        type: 'session_terminated',
        data: {
          session_id: 'session-abc',
          status: 'error',
          code: 'internal_panic',
          message: 'Session terminated due to internal error',
        },
      });
    });

    expect(activeRequestsRef.current).toBe(0);
  });

  it('sets lastError to the event message', () => {
    const { stateHolder } = setup({ initialProcessing: true, initialActiveRequests: 1 });

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-2',
        type: 'session_terminated',
        data: {
          session_id: 'session-abc',
          status: 'error',
          code: 'internal_panic',
          message: 'Session terminated due to internal error',
        },
      });
    });

    expect(stateHolder.current.lastError).toBe('Session terminated due to internal error');
  });

  it('sets lastError to a default message when no message is provided', () => {
    const { stateHolder } = setup({ initialProcessing: true, initialActiveRequests: 1 });

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-3',
        type: 'session_terminated',
        data: { session_id: 'session-abc', status: 'error' },
      });
    });

    expect(stateHolder.current.lastError).toBe('Session terminated');
  });

  it('clears isProcessing so the running spinner stops', () => {
    const { stateHolder } = setup({ initialProcessing: true, initialActiveRequests: 1 });

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-4',
        type: 'session_terminated',
        data: { message: 'crashed' },
      });
    });

    expect(stateHolder.current.isProcessing).toBe(false);
  });

  it('clears queryProgress', () => {
    const { stateHolder } = setup({
      initialProcessing: true,
      initialActiveRequests: 1,
    });
    stateHolder.current.queryProgress = { message: 'Running...' };

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-5',
        type: 'session_terminated',
        data: { message: 'crashed' },
      });
    });

    expect(stateHolder.current.queryProgress).toBeNull();
  });

  it('appends an error-level log entry', () => {
    const { stateHolder } = setup({ initialProcessing: true, initialActiveRequests: 1 });

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-6',
        type: 'session_terminated',
        data: { message: 'crashed' },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    const logEntry = stateHolder.current.logs[0];
    expect(logEntry.level).toBe('error');
    expect(logEntry.category).toBe('system');
  });

  it('marks in-flight tool executions as errored', () => {
    const { stateHolder } = setup({ initialProcessing: true, initialActiveRequests: 1 });
    stateHolder.current.toolExecutions = [
      { id: 'tool-1', tool: 'read_file', status: 'started', startTime: new Date() },
      { id: 'tool-2', tool: 'edit_file', status: 'completed', startTime: new Date(), endTime: new Date() },
    ];

    act(() => {
      hookHandleEvent!({
        id: 'evt-term-7',
        type: 'session_terminated',
        data: { message: 'crashed' },
      });
    });

    const tools = stateHolder.current.toolExecutions as Array<{ status: string; result?: string }>;
    expect(tools[0].status).toBe('error');
    expect(tools[0].result).toBe('Session terminated');
    // Already-completed tools are left untouched.
    expect(tools[1].status).toBe('completed');
  });
});

// ---------------------------------------------------------------------------
// Tests: delegate_clarification_requested (log-only)
// ---------------------------------------------------------------------------

describe('delegate_clarification_requested', () => {
  function setup() {
    const stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    const activeRequestsRef: MutableRefObject<number> = { current: 0 };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          activeRequestsRef,
        }),
      );
    });

    return { stateHolder };
  }

  it('creates an info-level log entry with tool category', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-delegate-1',
        type: 'delegate_clarification_requested',
        data: { subagent_id: 'sa-1', request_id: 'req-1', question: 'Which file?' },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].level).toBe('info');
    expect(stateHolder.current.logs[0].category).toBe('tool');
  });

  it('does NOT set lastError (log-only handler)', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-delegate-2',
        type: 'delegate_clarification_requested',
        data: { subagent_id: 'sa-1', request_id: 'req-2', question: 'What next?' },
      });
    });

    expect(stateHolder.current.lastError).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: delegate_clarification_responded (log-only)
// ---------------------------------------------------------------------------

describe('delegate_clarification_responded', () => {
  function setup() {
    const stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    const activeRequestsRef: MutableRefObject<number> = { current: 0 };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          activeRequestsRef,
        }),
      );
    });

    return { stateHolder };
  }

  it('creates an info-level log entry with tool category', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-delegate-r',
        type: 'delegate_clarification_responded',
        data: { subagent_id: 'sa-1', request_id: 'req-1', response: 'main.go' },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].level).toBe('info');
    expect(stateHolder.current.logs[0].category).toBe('tool');
  });
});

// ---------------------------------------------------------------------------
// Tests: workspace_patch (log-only)
// ---------------------------------------------------------------------------

describe('workspace_patch', () => {
  function setup() {
    const stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    const activeRequestsRef: MutableRefObject<number> = { current: 0 };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          activeRequestsRef,
        }),
      );
    });

    return { stateHolder };
  }

  it('creates an info-level log entry with file category and redacts file contents', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-ws-patch',
        type: 'workspace_patch',
        data: { path: 'foo.txt', content: 'secret-data', action: 'write', seq: 1 },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].level).toBe('info');
    expect(stateHolder.current.logs[0].category).toBe('file');
    // File contents must NOT be persisted in React state — only path/action/seq.
    const loggedData = stateHolder.current.logs[0].data as Record<string, unknown> | undefined;
    expect(loggedData?.content).toBeUndefined();
    expect(loggedData?.path).toBe('foo.txt');
  });
});

// ---------------------------------------------------------------------------
// Tests: recall_diagnostic (log-only)
// ---------------------------------------------------------------------------

describe('recall_diagnostic', () => {
  function setup() {
    const stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    const activeRequestsRef: MutableRefObject<number> = { current: 0 };

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          activeRequestsRef,
        }),
      );
    });

    return { stateHolder };
  }

  it('creates an info-level log entry with system category', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        id: 'evt-recall',
        type: 'recall_diagnostic',
        data: { query: 'test', results: 3, elapsed_ms: 42 },
      });
    });

    expect(stateHolder.current.logs).toHaveLength(1);
    expect(stateHolder.current.logs[0].level).toBe('info');
    expect(stateHolder.current.logs[0].category).toBe('system');
  });
});
