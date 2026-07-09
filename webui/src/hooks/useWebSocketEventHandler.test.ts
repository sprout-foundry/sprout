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
  function setup(opts: {
    initialLastError: string | null;
    getStatsImpl: () => Promise<unknown>;
  }) {
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
