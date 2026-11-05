// @ts-nocheck — the store stub mirrors only the AppState fields the handler touches
/**
 * A subagent publishes one `subagent_activity` event per line of its output
 * (pkg/agent/subagent_task.go). Routing each one straight into the store meant
 * a whole-app render per line. The dispatch loop now coalesces those types into
 * one dispatch per window: this test counts renders of a store consumer, so a
 * regression back to per-event rendering shows up as a number, not a feeling.
 */

import type { MutableRefObject } from 'react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

vi.mock('../utils/log', () => ({ debugLog: vi.fn(), error: vi.fn() }));
vi.mock('../utils/messageId', () => ({ generateMessageId: vi.fn(() => 'msg-1') }));
vi.mock('../utils/messageWindow', () => ({ trimMessages: vi.fn((messages) => messages) }));
vi.mock('../utils/logCap', () => ({ appendCappedLog: vi.fn((logs, entry) => [...logs, entry]) }));
vi.mock('../services/clientSession', () => ({ getWebUIClientId: vi.fn(() => 'test-client-id') }));
vi.mock('../services/lspClientService', () => ({
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

import { AppStoreProvider, createAppStore, useAppStateField, useAppStoreSetState } from '../contexts/AppStore';
import type { WsEvent } from '@sprout/events';
import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';

const renders = { probe: 0 };

function Probe() {
  const activities = useAppStateField('subagentActivities');
  renders.probe += 1;
  return createElement('span', { 'data-testid': 'count' }, String(activities.length));
}

function Harness({ refs }: { refs: UseWebSocketEventHandlerRefs }) {
  const setState = useAppStoreSetState();
  const { handleEvent } = useWebSocketEventHandler({
    setState,
    refs,
    apiService: { getStats: vi.fn().mockResolvedValue({}) },
  });
  eventHandler.current = handleEvent;
  return createElement(Probe);
}

const eventHandler: { current: ((event: WsEvent) => void) | null } = { current: null };

function makeRefs(): UseWebSocketEventHandlerRefs {
  return {
    activeRequestsRef: { current: 0 } as MutableRefObject<number>,
    activeChatIdRef: { current: 'chat-1' } as MutableRefObject<string | null>,
    pendingProviderRef: { current: 'openai' } as MutableRefObject<string>,
    pendingProviderChangeRef: { current: false } as MutableRefObject<boolean>,
    pendingProviderChangeValueRef: { current: null } as MutableRefObject<string | null>,
    connectionTimeoutRef: { current: null } as MutableRefObject<ReturnType<typeof setTimeout> | null>,
    lastConnectionStateRef: { current: false } as MutableRefObject<boolean>,
  };
}

function subagentOutput(index: number): WsEvent {
  return {
    id: `sub-${index}`,
    type: 'subagent_activity',
    data: {
      tool_call_id: 'tc-1',
      tool_name: 'run_subagent',
      phase: 'output',
      message: `line ${index}`,
      chat_id: 'chat-1',
    },
  } as unknown as WsEvent;
}

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  renders.probe = 0;
  eventHandler.current = null;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

describe('high-frequency event coalescing', () => {
  it('renders a burst of subagent output lines in one batch', () => {
    vi.useFakeTimers();
    try {
      const store = createAppStore({
        messages: [],
        logs: [],
        subagentActivities: [],
        toolExecutions: [],
        fileEdits: [],
        perChatCache: {},
        isProcessing: false,
        stats: {},
        queryProgress: null,
      } as never);

      act(() => {
        root.render(createElement(AppStoreProvider, { store }, createElement(Harness, { refs: makeRefs() })));
      });
      const afterMount = renders.probe;

      // Ten output lines, all inside one coalescing window.
      act(() => {
        for (let i = 0; i < 10; i += 1) eventHandler.current?.(subagentOutput(i));
      });
      const duringBurst = renders.probe;
      // The first line of the window routes immediately (leading edge); the
      // rest wait for the window to close.
      expect(duringBurst - afterMount).toBe(1);
      expect(store.getSnapshot().subagentActivities).toHaveLength(1);

      act(() => {
        vi.advanceTimersByTime(200);
      });

      expect(store.getSnapshot().subagentActivities).toHaveLength(10);
      // One render for the leading event, one for the batch.
      expect(renders.probe - afterMount).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('drops a pending batch when the user switches chats before it flushes', () => {
    vi.useFakeTimers();
    try {
      const store = createAppStore({
        messages: [],
        logs: [],
        subagentActivities: [],
        toolExecutions: [],
        fileEdits: [],
        perChatCache: {},
        isProcessing: false,
        stats: {},
        queryProgress: null,
      } as never);

      const refs = makeRefs();
      act(() => {
        root.render(createElement(AppStoreProvider, { store }, createElement(Harness, { refs })));
      });

      act(() => {
        eventHandler.current?.(subagentOutput(0));
        eventHandler.current?.(subagentOutput(1));
      });
      // One line applied (leading edge), one queued for the window.
      expect(store.getSnapshot().subagentActivities).toHaveLength(1);

      // The user switches chats while the batch waits. Routing it now would
      // apply the old chat's events to the newly active chat.
      refs.activeChatIdRef.current = 'chat-2';
      act(() => {
        vi.advanceTimersByTime(200);
      });

      expect(store.getSnapshot().subagentActivities).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('drains a pending batch before an event that must not be reordered', () => {
    vi.useFakeTimers();
    try {
      const store = createAppStore({
        messages: [],
        logs: [],
        subagentActivities: [],
        toolExecutions: [],
        fileEdits: [],
        perChatCache: {},
        isProcessing: false,
        stats: {},
        queryProgress: null,
      } as never);

      act(() => {
        root.render(createElement(AppStoreProvider, { store }, createElement(Harness, { refs: makeRefs() })));
      });

      act(() => {
        for (let i = 0; i < 3; i += 1) eventHandler.current?.(subagentOutput(i));
      });
      expect(store.getSnapshot().subagentActivities).toHaveLength(1);

      // query_completed is not coalesced: the backlog must land first.
      act(() => {
        eventHandler.current?.({
          id: 'done',
          type: 'query_completed',
          data: { query: 'do it', response: 'done', chat_id: 'chat-1' },
        } as unknown as WsEvent);
      });

      expect(store.getSnapshot().subagentActivities).toHaveLength(3);
    } finally {
      vi.useRealTimers();
    }
  });
});
