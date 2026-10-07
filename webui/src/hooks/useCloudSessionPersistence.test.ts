/**
 * The cloud session-persistence hook is gated on the host's `localTerminal`
 * capability: it mirrors the conversation into the browser-local store only in
 * a hosted shell (no local terminal). A local-terminal host keeps it a no-op —
 * the backend already persists conversations. Proves the flip with a
 * localTerminal:false host (persists) vs a localTerminal:true host (no-op).
 */

import { renderHook } from '@testing-library/react';
import { hostWrapper, makeTestHost } from '../host/testHost';
import type { AppState } from '../types/app';
import { useCloudSessionPersistence } from './useCloudSessionPersistence';

const store = vi.hoisted(() => ({
  saveSession: vi.fn().mockReturnValue('saved-id'),
  deleteSession: vi.fn(),
  restoreSession: vi.fn().mockReturnValue(null),
  startNewCloudSession: vi.fn().mockReturnValue('new-session-id'),
}));
vi.mock('../services/cloudSessionStore', () => store);

const chats = vi.hoisted(() => ({
  rebindChatTranscript: vi.fn(),
  transcriptIdForChat: vi.fn().mockReturnValue('chat-1-transcript'),
}));
vi.mock('../services/cloudChatSessions', () => chats);

vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

function makeState(overrides: Partial<AppState> = {}): AppState {
  return {
    messages: [{ id: 'm1', type: 'user', content: 'hi', timestamp: new Date() }] as never,
    isProcessing: false,
    activeChatId: 'chat-1',
    chatSessions: [],
    queryCount: 5,
    ...overrides,
  } as unknown as AppState;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('useCloudSessionPersistence', () => {
  it('persists the session in a hosted shell (no local terminal)', () => {
    const state = makeState();
    renderHook(() => useCloudSessionPersistence({ state }), {
      wrapper: hostWrapper(makeTestHost({ localTerminal: false })),
    });

    expect(store.saveSession).toHaveBeenCalledTimes(1);
  });

  it('is a no-op in a local-terminal host', () => {
    const state = makeState();
    renderHook(() => useCloudSessionPersistence({ state }), {
      wrapper: hostWrapper(makeTestHost({ localTerminal: true })),
    });

    expect(store.saveSession).not.toHaveBeenCalled();
    expect(store.deleteSession).not.toHaveBeenCalled();
    expect(store.startNewCloudSession).not.toHaveBeenCalled();
    expect(chats.rebindChatTranscript).not.toHaveBeenCalled();
  });

  it('deletes store entries for sessions removed from chatSessions (hosted shell only)', () => {
    const state = makeState({
      chatSessions: [{ id: 'gone-session' } as never],
    });
    const utils = renderHook(
      ({ chatSessions }: { chatSessions: AppState['chatSessions'] }) => {
        const s = { ...state, chatSessions };
        return useCloudSessionPersistence({ state: s });
      },
      {
        wrapper: hostWrapper(makeTestHost({ localTerminal: false })),
        initialProps: { chatSessions: state.chatSessions },
      },
    );

    // Drop the session from the list — the host-backed store must drop it too.
    utils.rerender({ chatSessions: [] });
    expect(store.deleteSession).toHaveBeenCalledWith('gone-session');
  });
});
