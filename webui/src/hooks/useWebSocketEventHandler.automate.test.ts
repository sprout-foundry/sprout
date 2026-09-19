/**
 * Automate WS bridge regression tests.
 *
 * AutomationsPanel / AutomationsSessionDetail subscribe to the
 * automateEvents pub-sub bus, which is fed by emitAutomate(). The only
 * caller that ever invoked it lived in the deleted useEventHandler.ts
 * (removed as "dead code" in 1dfd28e22), so the panels went permanently
 * silent — and because 6d94e49ad had already removed their polling
 * fallback, they never refreshed at all.
 *
 * These tests pin the dispatch in the LIVE handler
 * (useWebSocketEventHandler) so the bridge can't be dropped again.
 */
// @ts-nocheck — mock objects don't fully implement all interfaces

import type { MutableRefObject } from 'react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  error: vi.fn(),
}));

vi.mock('../utils/chatCompletion', () => ({
  ensureCompletedAssistantMessage: vi.fn((messages) => messages),
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
  LSPClientService: { getInstance: vi.fn(() => ({ cleanup: vi.fn() })) },
}));

vi.mock('../services/notificationBus', () => ({
  notificationBus: { emit: vi.fn(), subscribe: vi.fn() },
}));

vi.mock('../services/desktopNotify', () => ({
  notifyIfHidden: vi.fn(),
}));

vi.mock('../services/chatSessions', () => ({
  fetchChatSessionMessages: vi.fn().mockResolvedValue({ chat_session: { messages: [] } }),
  listChatSessions: vi.fn().mockResolvedValue({ chat_sessions: [] }),
}));

// Spy on the bus so we can assert dispatch without pulling in subscribers.
const emitAutomateMock = vi.fn();
vi.mock('../services/automateEvents', () => ({
  emitAutomate: (...args: unknown[]) => emitAutomateMock(...args),
}));

import { useWebSocketEventHandler, type UseWebSocketEventHandlerRefs } from './useWebSocketEventHandler';
import type { WsEvent } from '@sprout/events';

let container: HTMLDivElement;
let root: Root;
let hookHandleEvent: ((event: WsEvent) => void) | null = null;

const HookWrapper = ({ setStateMock }: { setStateMock: (u: unknown) => void }) => {
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

  const { handleEvent } = useWebSocketEventHandler({
    setState: setStateMock as never,
    refs,
    apiService: { getStats: vi.fn().mockResolvedValue({}) } as never,
  });

  hookHandleEvent = handleEvent;
  return createElement('div', null, 'automate bridge host');
};

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  hookHandleEvent = null;
  emitAutomateMock.mockClear();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function mount() {
  act(() => {
    root.render(createElement(HookWrapper, { setStateMock: vi.fn() }));
  });
}

describe('automate.* WS → automateEvents bridge', () => {
  it('forwards automate.session_started to the bus', () => {
    mount();
    act(() => {
      hookHandleEvent!({
        id: 'e1',
        type: 'automate.session_started',
        data: { session_id: 'sess-1', workflow: 'wf.json', kind: 'automate' },
      });
    });

    expect(emitAutomateMock).toHaveBeenCalledTimes(1);
    expect(emitAutomateMock).toHaveBeenCalledWith('automate.session_started', {
      session_id: 'sess-1',
      workflow: 'wf.json',
      kind: 'automate',
    });
  });

  it('forwards automate.session_ended to the bus', () => {
    mount();
    act(() => {
      hookHandleEvent!({
        id: 'e2',
        type: 'automate.session_ended',
        data: { session_id: 'sess-2', status: 'success' },
      });
    });

    expect(emitAutomateMock).toHaveBeenCalledWith('automate.session_ended', {
      session_id: 'sess-2',
      status: 'success',
    });
  });

  it('forwards automate.output_chunk to the bus', () => {
    mount();
    act(() => {
      hookHandleEvent!({
        id: 'e3',
        type: 'automate.output_chunk',
        data: { session_id: 'sess-3', offset: 10, chunk_len: 5 },
      });
    });

    expect(emitAutomateMock).toHaveBeenCalledWith('automate.output_chunk', {
      session_id: 'sess-3',
      offset: 10,
      chunk_len: 5,
    });
  });

  it('forwards automate.budget_update to the bus', () => {
    mount();
    act(() => {
      hookHandleEvent!({
        id: 'e4',
        type: 'automate.budget_update',
        data: { session_id: 'sess-4', spent_usd: 1.5, budget_usd: 10 },
      });
    });

    expect(emitAutomateMock).toHaveBeenCalledWith('automate.budget_update', {
      session_id: 'sess-4',
      spent_usd: 1.5,
      budget_usd: 10,
    });
  });

  it('does not route automate events into chat state', () => {
    // Automate frames carry no chat_id and are not chat streaming events —
    // the per-chat filter would drop them and the fallthrough would log an
    // "unknown event type" warning. They must be dispatched and returned.
    const setStateMock = vi.fn();
    act(() => {
      root.render(createElement(HookWrapper, { setStateMock }));
    });
    act(() => {
      hookHandleEvent!({
        id: 'e5',
        type: 'automate.session_started',
        data: { session_id: 'sess-5' },
      });
    });

    expect(emitAutomateMock).toHaveBeenCalledTimes(1);
    expect(setStateMock).not.toHaveBeenCalled();
  });
});
