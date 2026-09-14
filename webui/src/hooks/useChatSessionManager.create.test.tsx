import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import { useChatSessionManager } from './useChatSessionManager';
import type { AppState } from '../types/app';
import type { Message } from '@sprout/ui';

// ---------------------------------------------------------------------------
// Mocks — chatSessions service drives the create/list round-trips.
// ---------------------------------------------------------------------------

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  list: vi.fn(),
}));

vi.mock('../services/chatSessions', () => ({
  listChatSessions: mocks.list,
  createChatSession: mocks.create,
  createChatSessionInWorktree: vi.fn(),
  deleteChatSession: vi.fn(),
  deleteAllChatSessions: vi.fn(),
  renameChatSession: vi.fn(),
  switchChatSession: vi.fn(),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({ sendQuery: vi.fn(), steerQuery: vi.fn(), stopQuery: vi.fn(), retractSteer: vi.fn() }),
  },
}));

vi.mock('../services/nativeChatStubs/nativeChatFlag', () => ({
  NATIVE_CHAT_ENABLED: false,
}));

// ---------------------------------------------------------------------------
// Harness — render the hook through a minimal React test renderer.
// ---------------------------------------------------------------------------

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

let container: HTMLDivElement;
let root: Root;
let latest: ReturnType<typeof useChatSessionManager>;

function Probe() {
  latest = useChatSessionManager({
    setState: ((updater: (prev: AppState) => Partial<AppState>) => {}) as unknown as AppStoreSetState,
    activeRequestsRef: { current: 0 },
    activeChatIdRef: { current: null },
    queuedMessagesRef: { current: [] },
    isProcessing: false,
  });
  return null;
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  mocks.create.mockReset();
  mocks.list.mockReset();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  vi.clearAllMocks();
});

describe('handleCreateChat in-flight guard', () => {
  it('concurrent create calls produce exactly one session', async () => {
    // Create resolves slowly; list resolves immediately.
    let resolveCreate: (v: { chat_session: { id: string } }) => void = () => {};
    mocks.create.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve;
        }),
    );
    mocks.list.mockResolvedValue({ active_chat_id: 's1', chat_sessions: [{ id: 's1' }] });

    act(() => {
      root.render(createElement(Probe));
    });

    let p1: Promise<string | null> | null = null;
    await act(async () => {
      p1 = latest.handleCreateChat();
      const p2 = latest.handleCreateChat();
      const p3 = latest.handleCreateChat();
      const [r2, r3] = await Promise.all([p2, p3]);
      expect(r2).toBeNull();
      expect(r3).toBeNull();
    });

    // While the first create is pending, the guard short-circuits the rest.
    expect(mocks.create).toHaveBeenCalledTimes(1);

    // Resolve the create; the first call returns the id and refreshes once.
    let r1: string | null = null;
    await act(async () => {
      resolveCreate({ chat_session: { id: 's-new' } });
      r1 = await p1;
    });
    expect(r1).toBe('s-new');
    expect(mocks.list).toHaveBeenCalledTimes(1);

    // After completion, a new create is allowed again.
    mocks.create.mockResolvedValue({ chat_session: { id: 's-new2' } });
    let second: string | null = null;
    await act(async () => {
      second = await latest.handleCreateChat();
    });
    expect(second).toBe('s-new2');
    expect(mocks.create).toHaveBeenCalledTimes(2);
  });
});
