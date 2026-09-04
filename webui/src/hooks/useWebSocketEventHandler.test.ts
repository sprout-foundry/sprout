/**
 * useWebSocketEventHandler.test.ts — Unit tests for subagent_activity
 * status field handling in the WebSocket event handler (SP-037-3c).
 *
 * These tests verify that handleSubagentActivity correctly captures
 * the status field from event data into SubagentActivity objects.
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
let root: Root;

let hookHandleEvent: ((event: WsEvent) => void) | null = null;
let hookHandleReconnect: (() => void) | null = null;

const HookWrapper = ({
  stateHolder,
  setStateMock,
  activeChatIdRef,
  getStatsMock,
}: {
  stateHolder: { current: Record<string, unknown> };
  setStateMock: ReturnType<typeof vi.fn>;
  activeChatIdRef: MutableRefObject<string | null>;
  /**
   * Optional override for apiService.getStats(). Resolves by default with
   * a minimal stats object. Pass a rejecting mock to exercise the
   * handleReconnect .catch branch.
   */
  getStatsMock?: () => Promise<unknown>;
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
    getStats: getStatsMock ?? vi.fn().mockResolvedValue({ provider: 'openai', model: 'gpt-4' }),
  };

  const { handleEvent, handleReconnect } = useWebSocketEventHandler({
    setState: setStateMock as AppStoreSetState,
    refs,
    apiService,
  });

  hookHandleEvent = handleEvent;
  hookHandleReconnect = handleReconnect;

  return createElement('div', null, 'hook host');
};

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  hookHandleEvent = null;
  hookHandleReconnect = null;
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

// ---------------------------------------------------------------------------
// Tests: chat_run_restored gap handling (reconnect replay)
// ---------------------------------------------------------------------------

describe('chat_run_restored', () => {
  function setup(activeChatId: string | null) {
    const stateHolder = { current: createDefaultState() };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: activeChatId };
    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock, activeChatIdRef }));
    });
    const reloads: Array<string | undefined> = [];
    const onReload = (e: Event) => reloads.push((e as CustomEvent<{ chatId?: string }>).detail?.chatId);
    window.addEventListener('sprout:chat-gap-reload', onReload);
    return { reloads, cleanup: () => window.removeEventListener('sprout:chat-gap-reload', onReload) };
  }

  it('requests a reload when gap is true for the active chat', () => {
    const { reloads, cleanup } = setup('chat-1');
    act(() => {
      hookHandleEvent!({ id: 'e', type: 'chat_run_restored', data: { gap: true, chat_id: 'chat-1' } });
    });
    expect(reloads).toEqual(['chat-1']);
    cleanup();
  });

  it('does NOT reload when gap is false (replay is complete)', () => {
    const { reloads, cleanup } = setup('chat-1');
    act(() => {
      hookHandleEvent!({ id: 'e', type: 'chat_run_restored', data: { gap: false, chat_id: 'chat-1' } });
    });
    expect(reloads).toHaveLength(0);
    cleanup();
  });

  it('does NOT reload a chat the user is not viewing', () => {
    const { reloads, cleanup } = setup('chat-1');
    act(() => {
      hookHandleEvent!({ id: 'e', type: 'chat_run_restored', data: { gap: true, chat_id: 'chat-2' } });
    });
    expect(reloads).toHaveLength(0);
    cleanup();
  });
});

// ---------------------------------------------------------------------------
// Tests: handleReconnect clears lastError unconditionally (bug fix)
//
// Symptom: after a WebSocket reconnect, the "chat failed" red banner in
// ChatFooter (driven by lastError) used to stick around if the getStats()
// request following reconnect was slow or failed. The reconnect itself is
// the recovery signal — handleReconnect must clear lastError regardless of
// the getStats() outcome.
// ---------------------------------------------------------------------------

describe('handleReconnect', () => {
  // Helper: install the wrapper with a controllable getStats mock and seed
  // the starting state. Returns the live stateHolder and the setState spy
  // so the test can assert on call ordering and final values.
  function setup(opts: { initialLastError: string | null; getStatsImpl: () => Promise<unknown> }) {
    const stateHolder = {
      current: { ...createDefaultState(), lastError: opts.initialLastError },
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
    const getStatsMock = vi.fn().mockImplementation(opts.getStatsImpl);

    act(() => {
      root.render(
        createElement(HookWrapper, {
          stateHolder,
          setStateMock,
          activeChatIdRef,
          getStatsMock,
        }),
      );
    });

    return { stateHolder, setStateMock, getStatsMock };
  }

  it('clears lastError when getStats() rejects (regression for sticky banner)', async () => {
    const { stateHolder, setStateMock, getStatsMock } = setup({
      initialLastError: 'chat failed: connection lost',
      // Never resolves within the test window — the .then branch will not
      // run, so only the up-front + .catch clears can save the banner.
      getStatsImpl: () => new Promise(() => {}),
    });

    // Reset call-history so we can inspect only the handleReconnect calls.
    setStateMock.mockClear();
    getStatsMock.mockClear();

    await act(async () => {
      hookHandleReconnect!();
      // Flush microtasks so the synchronous up-front setState is recorded,
      // then await the never-resolving promise so .then stays pending.
      await Promise.resolve();
    });

    // The unconditional up-front clear must have run with lastError: null
    // before getStats() was called. Find the first setState call's payload.
    const firstSetStateCall = setStateMock.mock.calls[0];
    expect(firstSetStateCall).toBeDefined();
    const firstUpdater = firstSetStateCall[0] as (prev: unknown) => unknown;
    const firstResult = firstUpdater(stateHolder.current);
    expect(firstResult).toMatchObject({ lastError: null });

    // The applied state must reflect the clear.
    expect(stateHolder.current.lastError).toBeNull();
    // getStats must have been invoked.
    expect(getStatsMock).toHaveBeenCalledTimes(1);
  });

  it('clears lastError in the .catch branch when getStats() rejects', async () => {
    const getStatsImpl = vi.fn().mockRejectedValue(new Error('network down'));
    const { stateHolder, setStateMock } = setup({
      initialLastError: 'chat failed: send error',
      getStatsImpl,
    });

    setStateMock.mockClear();
    getStatsImpl.mockClear();

    await act(async () => {
      hookHandleReconnect!();
      // Allow the rejected promise's .catch to run.
      await Promise.resolve();
      await Promise.resolve();
    });

    // After both the up-front and the .catch clears run, lastError must
    // be null in the applied state.
    expect(stateHolder.current.lastError).toBeNull();

    // Sanity: at least one setState update with lastError: null was issued
    // (covers both the up-front clear and the defensive .catch clear).
    const clearingCalls = setStateMock.mock.calls.filter((call) => {
      const updater = call[0];
      if (typeof updater !== 'function') return false;
      try {
        const result = updater(stateHolder.current) as { lastError?: unknown };
        return result?.lastError === null;
      } catch {
        return false;
      }
    });
    expect(clearingCalls.length).toBeGreaterThanOrEqual(1);
  });

  it('clears lastError when getStats() resolves successfully', async () => {
    const { stateHolder } = setup({
      initialLastError: 'chat failed: send timeout',
      getStatsImpl: () => Promise.resolve({ provider: 'openai', model: 'gpt-4', is_processing: false }),
    });

    await act(async () => {
      hookHandleReconnect!();
      // Let the resolved .then run.
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(stateHolder.current.lastError).toBeNull();
  });

  it('is a no-op on lastError when called with lastError already null', async () => {
    const { stateHolder, setStateMock, getStatsMock } = setup({
      initialLastError: null,
      getStatsImpl: () => Promise.resolve({ provider: 'openai', model: 'gpt-4', is_processing: false }),
    });

    setStateMock.mockClear();
    getStatsMock.mockClear();

    await act(async () => {
      hookHandleReconnect!();
      await Promise.resolve();
      await Promise.resolve();
    });

    // lastError stays null — no exception thrown, no spurious non-null value.
    expect(stateHolder.current.lastError).toBeNull();
    // getStats is still called (it does real recovery work), so this is not
    // a guard but a smoke test that the unconditional clear does not flip
    // an already-clean value to a non-null sentinel.
    expect(getStatsMock).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: error event cause surfacing
//
// Bug: error events pair a short label (`message`, e.g. "Query failed")
// with the underlying cause (`error`, e.g. a provider connection failure),
// but handleError rendered only the label. The user saw "[FAIL] Error:
// Query failed" with no way to tell a dead provider from a bad API key.
// ---------------------------------------------------------------------------

describe('error cause surfacing', () => {
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

    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock, activeChatIdRef }));
    });

    return { stateHolder };
  }

  it('includes the cause in lastError and the [FAIL] bubble when present', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({
        type: 'error',
        data: { message: 'Query failed', error: 'provider unreachable after 4 attempts' },
      } as WsEvent);
    });

    expect(stateHolder.current.lastError).toBe('Query failed: provider unreachable after 4 attempts');
    const messages = stateHolder.current.messages as Array<{ content: string }>;
    expect(
      messages.some((m) => m.content === '[FAIL] Error: Query failed: provider unreachable after 4 attempts'),
    ).toBe(true);
  });

  it('falls back to the label alone when no cause is present', () => {
    const { stateHolder } = setup();

    act(() => {
      hookHandleEvent!({ type: 'error', data: { message: 'boom' } } as WsEvent);
    });

    expect(stateHolder.current.lastError).toBe('boom');
    const messages = stateHolder.current.messages as Array<{ content: string }>;
    expect(messages.some((m) => m.content === '[FAIL] Error: boom')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: workspace_changed handler — in-place refresh, NOT page reload
//
// Bug: the old handler called window.location.reload() unconditionally,
// which in service mode caused the user to land in the home directory
// because per-client server state was re-initialised from ws.workspaceRoot
// (home) after the reload destroyed in-memory React state.
//
// Fix: the handler now does an in-place refresh — LSP teardown, cache
// clearing, and a `sprout:workspace-changed` DOM event dispatch — instead
// of a hard reload.
// ---------------------------------------------------------------------------

describe('workspace_changed', () => {
  function setup(clientId?: string) {
    const stateHolder = { current: createDefaultState() };
    // Seed caches that should be cleared on workspace change.
    stateHolder.current.recentFiles = ['/old/file1.ts'];
    stateHolder.current.recentLogs = ['old log entry'];

    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: 'chat-1' };
    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock, activeChatIdRef }));
    });

    // Track sprout:workspace-changed DOM events.
    const events: Array<{ workspaceRoot: string; daemonRoot: string }> = [];
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      events.push({ workspaceRoot: detail?.workspaceRoot, daemonRoot: detail?.daemonRoot });
    };
    window.addEventListener('sprout:workspace-changed', handler);

    return { stateHolder, events, cleanup: () => window.removeEventListener('sprout:workspace-changed', handler) };
  }

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does NOT call window.location.reload on workspace_changed', () => {
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', {
      value: { reload: reloadSpy },
      writable: true,
    });

    setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'workspace_changed',
        data: { workspace_root: '/new/path', daemon_root: '/home', client_id: 'test-client-id' },
      });
    });

    expect(reloadSpy).not.toHaveBeenCalled();
  });

  it('dispatches sprout:workspace-changed DOM event with the new workspace root', () => {
    const { events, cleanup } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'workspace_changed',
        data: { workspace_root: '/new/worktree', daemon_root: '/home/user', client_id: 'test-client-id' },
      });
    });

    expect(events).toHaveLength(1);
    expect(events[0].workspaceRoot).toBe('/new/worktree');
    expect(events[0].daemonRoot).toBe('/home/user');
    cleanup();
  });

  it('clears recentFiles and recentLogs caches', () => {
    const { stateHolder, cleanup } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'workspace_changed',
        data: { workspace_root: '/new/path', daemon_root: '/home', client_id: 'test-client-id' },
      });
    });

    expect(stateHolder.current.recentFiles).toEqual([]);
    expect(stateHolder.current.recentLogs).toEqual([]);
    cleanup();
  });

  it('ignores workspace_changed events for a different client_id', () => {
    const { events, cleanup } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'workspace_changed',
        data: { workspace_root: '/other/path', daemon_root: '/home', client_id: 'different-client-id' },
      });
    });

    expect(events).toHaveLength(0);
    cleanup();
  });

  it('processes broadcast events (no client_id)', () => {
    const { events, cleanup } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'workspace_changed',
        data: { workspace_root: '/broadcast/path', daemon_root: '/home' },
      });
    });

    expect(events).toHaveLength(1);
    expect(events[0].workspaceRoot).toBe('/broadcast/path');
    cleanup();
  });
});

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Tests: edit_approval_request handler (SP-072-3)
//
// Verifies that the WebUI correctly parses the edit_approval_request event
// payload from the backend and populates the editApprovalRequest state
// with the hunks, file path, and request ID.
// ---------------------------------------------------------------------------

describe('edit_approval_request', () => {
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
    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock, activeChatIdRef }));
    });
    return { stateHolder, setStateMock };
  }

  it('populates editApprovalRequest with the Go payload shape', () => {
    const { stateHolder } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'edit_approval_request',
        data: {
          request_id: 'edit_42',
          file_path: 'src/main.go',
          unified_diff: '@@ -1,3 +1,3 @@\n-a\n+B\n c',
          timestamp: '2026-08-20T00:00:00Z',
          hunks: [
            {
              id: 'hunk-0',
              old_start: 1,
              old_lines: 3,
              new_start: 1,
              new_lines: 3,
              lines: [
                { type: 'remove', content: 'a' },
                { type: 'add', content: 'B' },
                { type: 'context', content: 'c' },
              ],
              add_count: 1,
              del_count: 1,
            },
          ],
        },
      });
    });

    const req = stateHolder.current.editApprovalRequest;
    expect(req).not.toBeNull();
    expect(req.requestId).toBe('edit_42');
    expect(req.filePath).toBe('src/main.go');
    expect(req.unifiedDiff).toBe('@@ -1,3 +1,3 @@\n-a\n+B\n c');
    expect(req.hunks).toHaveLength(1);
    const h = req.hunks[0];
    expect(h.id).toBe('hunk-0');
    expect(h.oldStart).toBe(1);
    expect(h.oldLines).toBe(3);
    expect(h.newStart).toBe(1);
    expect(h.newLines).toBe(3);
    expect(h.addCount).toBe(1);
    expect(h.delCount).toBe(1);
    expect(h.lines).toHaveLength(3);
    expect(h.lines[0]).toEqual({ type: 'remove', content: 'a' });
    expect(h.lines[1]).toEqual({ type: 'add', content: 'B' });
    expect(h.lines[2]).toEqual({ type: 'context', content: 'c' });
  });

  it('ignores events missing request_id or file_path', () => {
    const { stateHolder } = setup();

    // Missing file_path
    act(() => {
      hookHandleEvent!({
        id: 'e1',
        type: 'edit_approval_request',
        data: {
          request_id: 'edit_99',
          hunks: [],
        },
      });
    });
    expect(stateHolder.current.editApprovalRequest).toBeNull();

    // Missing request_id
    act(() => {
      hookHandleEvent!({
        id: 'e2',
        type: 'edit_approval_request',
        data: {
          file_path: 'foo.go',
          hunks: [],
        },
      });
    });
    expect(stateHolder.current.editApprovalRequest).toBeNull();
  });

  it('maps unknown line types to context', () => {
    const { stateHolder } = setup();
    act(() => {
      hookHandleEvent!({
        id: 'e',
        type: 'edit_approval_request',
        data: {
          request_id: 'edit_weird',
          file_path: 'src/main.go',
          unified_diff: '',
          hunks: [
            {
              id: 'hunk-0',
              old_start: 1,
              old_lines: 1,
              new_start: 1,
              new_lines: 1,
              lines: [{ type: 'weird', content: 'mystery line' }],
              add_count: 0,
              del_count: 0,
            },
          ],
        },
      });
    });

    const req = stateHolder.current.editApprovalRequest;
    expect(req).not.toBeNull();
    expect(req.hunks[0].lines[0].type).toBe('context');
    expect(req.hunks[0].lines[0].content).toBe('mystery line');
  });
});

// ---------------------------------------------------------------------------
// Tests: primary-agent content must not be attributed to a subagent run
//
// Bug: after an inline subagent run is spawned, the subagent-run message
// (isSubagentRun) sits at the END of the messages array. Every
// "append to the last assistant message" path then lands in that message,
// whose content renders inside the subagent's collapsible block — so all
// follow-up primary-agent output (streaming text, tool badges, warnings)
// looks like it came from the subagent.
// ---------------------------------------------------------------------------

describe('subagent-run attribution', () => {
  const mkSubagentRun = (id: string): Record<string, unknown> => ({
    id,
    type: 'assistant',
    content: '',
    reasoning: 'subagent output line\n',
    timestamp: new Date(),
    persona: 'coder',
    subagentDepth: 1,
    isSubagentRun: true,
    subagentRunComplete: true,
    subagentPersona: 'coder',
  });

  const mkPrimary = (id: string, content: string): Record<string, unknown> => ({
    id,
    type: 'assistant',
    content,
    timestamp: new Date(),
  });

  function setup(initialMessages: Array<Record<string, unknown>>) {
    const stateHolder = { current: { ...createDefaultState(), messages: initialMessages } };
    const setStateMock = vi.fn((updater: unknown) => {
      if (typeof updater === 'function') {
        const prev = stateHolder.current;
        stateHolder.current = { ...prev, ...(updater(prev) as object) };
      } else {
        stateHolder.current = updater as typeof stateHolder.current;
      }
    });
    const activeChatIdRef: MutableRefObject<string | null> = { current: null };
    act(() => {
      root.render(createElement(HookWrapper, { stateHolder, setStateMock, activeChatIdRef }));
    });
    return { stateHolder };
  }

  it('stream_chunk after a completed subagent run creates a NEW primary message, not appending to the subagent run', () => {
    const subagentRun = mkSubagentRun('subagent-tc-1');
    const { stateHolder } = setup([
      { id: 'u1', type: 'user', content: 'do the thing', timestamp: new Date() },
      mkPrimary('a1', 'Working on it. Delegating to coder.'),
      subagentRun,
    ]);

    act(() => {
      hookHandleEvent!({
        id: 'e1',
        type: 'stream_chunk',
        data: { chunk: 'The subagent finished. Summary of results.' },
      });
    });

    const messages = stateHolder.current.messages as Array<Record<string, unknown>>;
    // A new primary assistant message was appended — total 4.
    expect(messages).toHaveLength(4);
    // The subagent run message is untouched.
    expect(messages[2]).toBe(subagentRun);
    // The new message is a plain primary assistant message with the chunk.
    expect(messages[3].type).toBe('assistant');
    expect(messages[3].isSubagentRun).toBeFalsy();
    expect(messages[3].content).toBe('The subagent finished. Summary of results.');
  });

  it('stream_chunk still appends to the existing primary message when the subagent run is NOT the last message', () => {
    const { stateHolder } = setup([
      { id: 'u1', type: 'user', content: 'do the thing', timestamp: new Date() },
      mkPrimary('a1', 'partial answer '),
    ]);

    act(() => {
      hookHandleEvent!({ id: 'e2', type: 'stream_chunk', data: { chunk: 'more text' } });
    });

    const messages = stateHolder.current.messages as Array<Record<string, unknown>>;
    // No new message — appended to the existing primary assistant.
    expect(messages).toHaveLength(2);
    expect(messages[1].content).toBe('partial answer more text');
  });

  it('tool_start after a subagent run attaches the marker to a new primary message, not the subagent block', () => {
    const subagentRun = mkSubagentRun('subagent-tc-2');
    const { stateHolder } = setup([
      { id: 'u1', type: 'user', content: 'do the thing', timestamp: new Date() },
      mkPrimary('a1', 'Delegating to coder.'),
      subagentRun,
    ]);

    act(() => {
      hookHandleEvent!({
        id: 'e3',
        type: 'tool_start',
        data: { tool_call_id: 'tc-9', tool_name: 'edit_file', display_name: 'Edit file' },
      });
    });

    const messages = stateHolder.current.messages as Array<Record<string, unknown>>;
    expect(messages).toHaveLength(4);
    // Subagent run message untouched — no tool marker, no toolRefs.
    expect(messages[2]).toBe(subagentRun);
    // The tool marker + toolRef landed on the fresh primary message.
    const fresh = messages[3];
    expect(fresh.type).toBe('assistant');
    expect(fresh.isSubagentRun).toBeFalsy();
    expect(String(fresh.content)).toContain('[executing tool [edit_file]]');
    const toolRefs = fresh.toolRefs as Array<{ toolId: string }>;
    expect(toolRefs).toHaveLength(1);
    expect(toolRefs[0].toolId).toBe('tc-9');
  });

  it('agent_message warning after a subagent run appends to the last PRIMARY message, not the subagent run', () => {
    const subagentRun = mkSubagentRun('subagent-tc-3');
    const { stateHolder } = setup([
      { id: 'u1', type: 'user', content: 'do the thing', timestamp: new Date() },
      mkPrimary('a1', 'Delegating to coder.'),
      subagentRun,
    ]);

    act(() => {
      hookHandleEvent!({
        id: 'e4',
        type: 'agent_message',
        data: { category: 'warning', message: '[~] build cache is stale' },
      });
    });

    const messages = stateHolder.current.messages as Array<Record<string, unknown>>;
    expect(messages).toHaveLength(3);
    // Subagent run content is untouched.
    expect(messages[2].content).toBe('');
    // The note landed on the primary message.
    expect(String(messages[1].content)).toContain('Note: [~] build cache is stale');
  });

  it('does not append a warning when no primary assistant message exists yet', () => {
    const subagentRun = mkSubagentRun('subagent-tc-4');
    const { stateHolder } = setup([
      { id: 'u1', type: 'user', content: 'do the thing', timestamp: new Date() },
      subagentRun,
    ]);

    act(() => {
      hookHandleEvent!({
        id: 'e5',
        type: 'agent_message',
        data: { category: 'warning', message: '[~] something odd' },
      });
    });

    const messages = stateHolder.current.messages as Array<Record<string, unknown>>;
    expect(messages).toHaveLength(2);
    expect(messages[1].content).toBe('');
  });
});
