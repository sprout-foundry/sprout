/**
 * useChatSessionManager steer-retraction tests.
 *
 * Validates the Up-arrow pull-back contract: after a steer send while a
 * query is active, handleRetractSteer calls /api/query/steer/retract via
 * ApiService, removes the optimistic user bubble, and restores the text
 * to inputValue. Also covers the nothing-pending and already-picked-up
 * paths.
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import type { AppState } from '../types/app';
import { useChatSessionManager } from './useChatSessionManager';

const apiDouble = vi.hoisted(() => ({
  steerQuery: vi.fn().mockResolvedValue(undefined),
  retractSteer: vi.fn(),
  sendQuery: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => apiDouble,
  },
}));

vi.mock('../services/chatSessions', () => ({
  listChatSessions: vi.fn().mockResolvedValue({ sessions: [] }),
  createChatSession: vi.fn().mockResolvedValue({ id: 'chat-1' }),
  deleteChatSession: vi.fn().mockResolvedValue(undefined),
  renameChatSession: vi.fn().mockResolvedValue(undefined),
  switchChatSession: vi.fn().mockResolvedValue({ messages: [] }),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

function setupHook() {
  let state: AppState = {
    messages: [],
    isProcessing: true,
    inputValue: '',
  } as unknown as AppState;

  const setState: AppStoreSetState = (updater) => {
    const partial = typeof updater === 'function' ? (updater as (prev: AppState) => Partial<AppState>)(state) : updater;
    state = { ...state, ...partial };
  };

  const utils = renderHook(() =>
    useChatSessionManager({
      setState,
      activeRequestsRef: { current: 1 },
      activeChatIdRef: { current: 'chat-1' },
      queuedMessagesRef: { current: [] },
      isProcessing: true,
    }),
  );

  return {
    ...utils,
    getState: () => state,
  };
}

describe('useChatSessionManager steer retraction', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiDouble.steerQuery.mockResolvedValue(undefined);
  });

  it('pulls back a pending steer: bubble removed, text restored to input', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('fix typo plz');
    });

    expect(apiDouble.steerQuery).toHaveBeenCalledWith('fix typo plz', 'chat-1');
    expect(hook.getState().messages).toHaveLength(1);
    expect(hook.getState().messages[0].content).toBe('fix typo plz');

    apiDouble.retractSteer.mockResolvedValue({ success: true, message: 'fix typo plz' });

    let retracted = false;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(true);
    expect(apiDouble.retractSteer).toHaveBeenCalledWith('chat-1');
    expect(hook.getState().messages).toHaveLength(0);
    expect(hook.getState().inputValue).toBe('fix typo plz');
  });

  it('returns false without an API call when no steer is pending', async () => {
    const hook = setupHook();

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
    expect(apiDouble.retractSteer).not.toHaveBeenCalled();
  });

  it('leaves the bubble when the steer was already picked up', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('already picked up');
    });

    apiDouble.retractSteer.mockResolvedValue({ success: false, message: '' });

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
    expect(hook.getState().messages).toHaveLength(1);
    expect(hook.getState().inputValue).toBe('');
  });

  it('survives a retract API failure without crashing', async () => {
    const hook = setupHook();

    await act(async () => {
      await hook.result.current.handleSendMessage('doomed steer');
    });

    apiDouble.retractSteer.mockRejectedValue(new Error('network down'));

    let retracted = true;
    await act(async () => {
      retracted = await hook.result.current.handleRetractSteer();
    });

    expect(retracted).toBe(false);
  });
});
