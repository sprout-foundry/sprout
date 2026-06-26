/**
 * useWebSocketEvents.reattach.test.tsx — Tests for the reattach-related
 * functionality in the useWebSocketEvents hook.
 *
 * Specifically tests:
 * - The useEffect that syncs state.activeChatId to WebSocketService
 * - That setActiveChatId is called on mount and on chat ID changes
 */

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

vi.mock('../services/websocket', () => {
  const wsService = {
    setActiveChatId: vi.fn(),
    getLastSeq: vi.fn(),
    getActiveChatSeq: vi.fn(),
    getInstance: vi.fn(),
  };
  wsService.getInstance.mockReturnValue(wsService);
  return { WebSocketService: { getInstance: wsService.getInstance } };
});

vi.mock('./useEventHandler', () => ({
  useEventHandler: vi.fn().mockReturnValue({
    handleEvent: vi.fn(),
  }),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: vi.fn().mockReturnValue({
      getStats: vi.fn(),
    }),
  },
}));

vi.mock('../services/chatSessions', () => ({
  switchChatSession: vi.fn(),
  listChatSessions: vi.fn(),
}));

vi.mock('../services/clientSession', () => ({
  getWebUIClientId: vi.fn(() => 'test-client-id'),
  appendClientIdToUrl: vi.fn((url: string) => url),
}));

vi.mock('../utils/messageWindow', () => ({
  trimMessages: vi.fn((msgs) => msgs),
}));

// Static import — Vitest hoists vi.mock above all imports automatically
import { WebSocketService } from '../services/websocket';
import type { AppState } from '../types/app';
import useWebSocketEvents from './useWebSocketEvents';

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
    activeChatId: null,
    chatSessions: [],
    perChatCache: {},
    securityApprovalRequest: null,
    securityPromptRequest: null,
    askUserRequest: null,
    editApprovalRequest: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests: useEffect syncs activeChatId to WebSocketService
// ---------------------------------------------------------------------------

describe('useWebSocketEvents reattach sync', () => {
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

  it('calls setActiveChatId on mount with current activeChatId', () => {
    const setState = vi.fn();
    const setQueuedMessages = vi.fn();
    const queuedMessagesRef = { current: [] };

    const TestComponent = () => {
      const state = createDefaultState({ activeChatId: 'chat-123' });
      useWebSocketEvents({ state, setState, setQueuedMessages, queuedMessagesRef });
      return null;
    };

    act(() => {
      root.render(createElement(TestComponent));
    });

    // The useEffect inside useWebSocketEvents should have called setActiveChatId
    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith('chat-123');
  });

  it('calls setActiveChatId with null when activeChatId is null', () => {
    const setState = vi.fn();
    const setQueuedMessages = vi.fn();
    const queuedMessagesRef = { current: [] };

    const TestComponent = () => {
      const state = createDefaultState({ activeChatId: null });
      useWebSocketEvents({ state, setState, setQueuedMessages, queuedMessagesRef });
      return null;
    };

    act(() => {
      root.render(createElement(TestComponent));
    });

    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith(null);
  });

  it('calls setActiveChatId when activeChatId changes', () => {
    const setState = vi.fn();
    const setQueuedMessages = vi.fn();
    const queuedMessagesRef = { current: [] };

    // State that we can change
    let currentChatId: string | null = 'chat-a';

    const TestComponent = () => {
      const state = createDefaultState({ activeChatId: currentChatId });
      useWebSocketEvents({ state, setState, setQueuedMessages, queuedMessagesRef });
      return null;
    };

    // Initial render with chat-a
    act(() => {
      root.render(createElement(TestComponent));
    });
    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith('chat-a');

    // Re-render with chat-b — useEffect dependency changes, should call again
    act(() => {
      currentChatId = 'chat-b';
      root.render(createElement(TestComponent));
    });
    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith('chat-b');

    // Re-render with null
    act(() => {
      currentChatId = null;
      root.render(createElement(TestComponent));
    });
    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith(null);
  });

  it('does NOT call setActiveChatId when activeChatId stays the same', () => {
    const setState = vi.fn();
    const setQueuedMessages = vi.fn();
    const queuedMessagesRef = { current: [] };

    const TestComponent = () => {
      const state = createDefaultState({ activeChatId: 'chat-same' });
      useWebSocketEvents({ state, setState, setQueuedMessages, queuedMessagesRef });
      return null;
    };

    // First render
    act(() => {
      root.render(createElement(TestComponent));
    });
    expect(WebSocketService.getInstance().setActiveChatId).toHaveBeenCalledWith('chat-same');

    const callCountAfterFirst = (WebSocketService.getInstance().setActiveChatId as ReturnType<typeof vi.fn>).mock.calls
      .length;

    // Re-render with same value — useEffect should skip (dependency unchanged)
    act(() => {
      root.render(createElement(TestComponent));
    });
    const callCountAfterSecond = (WebSocketService.getInstance().setActiveChatId as ReturnType<typeof vi.fn>).mock.calls
      .length;

    expect(callCountAfterSecond).toBe(callCountAfterFirst);
  });
});
