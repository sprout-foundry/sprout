import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useBackgroundChatSync } from './useBackgroundChatSync';
import type { PerChatState } from '../types/app';

const mocks = vi.hoisted(() => ({
  fetchMessages: vi.fn(),
}));

vi.mock('../services/chatSessions', () => ({
  fetchChatSessionMessages: mocks.fetchMessages,
}));

let container: HTMLDivElement;
let root: Root;
let applied: Array<Record<string, unknown>> = [];

function Probe(props: { perChatCache: Record<string, PerChatState>; activeChatId: string | null }) {
  useBackgroundChatSync({
    perChatCache: props.perChatCache,
    activeChatId: props.activeChatId,
    setState: ((updater: (prev: { perChatCache: Record<string, PerChatState> }) => Partial<unknown>) => {
      applied.push(updater({ perChatCache: props.perChatCache }) as Record<string, unknown>);
    }) as never,
  });
  return null;
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  applied = [];
  mocks.fetchMessages.mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

const emptyCache = (
  pending: number,
  messages = [{ id: 'm1', type: 'user', content: 'hi', timestamp: new Date() }],
) => ({
  messages,
  toolExecutions: [],
  fileEdits: [],
  subagentActivities: [],
  currentTodos: [],
  queryProgress: null,
  lastError: null,
  isProcessing: false,
  provider: '',
  model: '',
  queryCount: 0,
  pendingEvents: Array.from({ length: pending }, (_, i) => ({ type: 'stream_chunk', data: { i } })),
});

describe('useBackgroundChatSync', () => {
  it('fetches for a background chat with pending events after debounce', async () => {
    mocks.fetchMessages.mockResolvedValue({
      chat_session: {
        id: 'chat-b',
        messages: [{ role: 'user', content: 'hello' }],
        active_query: false,
      },
    });

    act(() => {
      root.render(createElement(Probe, { perChatCache: { 'chat-b': emptyCache(2) }, activeChatId: 'chat-a' }));
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(700);
    });

    expect(mocks.fetchMessages).toHaveBeenCalledTimes(1);
    expect(mocks.fetchMessages).toHaveBeenCalledWith('chat-b');
    expect(applied.length).toBe(1);
    const update = applied[0].perChatCache as Record<string, PerChatState>;
    expect(update['chat-b'].messages[0].content).toBe('hello');
    expect(update['chat-b'].pendingEvents).toBeUndefined();
  });

  it('never fetches for the active chat', async () => {
    act(() => {
      root.render(createElement(Probe, { perChatCache: { 'chat-a': emptyCache(5) }, activeChatId: 'chat-a' }));
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(700);
    });
    expect(mocks.fetchMessages).not.toHaveBeenCalled();
  });

  it('does not fetch for chats with no cache entry', async () => {
    act(() => {
      root.render(createElement(Probe, { perChatCache: {}, activeChatId: 'chat-a' }));
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(700);
    });
    expect(mocks.fetchMessages).not.toHaveBeenCalled();
  });
});
