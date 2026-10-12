import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppStoreSetState } from '../contexts/AppStore';
import { noteRunStarted, resetRunEpochs } from '../utils/runEpoch';
import { RUN_STATE_POLL_MS, useRunStateReconciler } from './useRunStateReconciler';

vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));
vi.mock('../services/runStatus', () => ({ fetchChatRunning: vi.fn() }));

import { fetchChatRunning } from '../services/runStatus';

const fetchRunning = fetchChatRunning as ReturnType<typeof vi.fn>;

function setup(isProcessing: boolean) {
  let state: Record<string, unknown> = { isProcessing };
  const setState = vi.fn((updater: unknown) => {
    const patch = typeof updater === 'function' ? (updater as (p: unknown) => object)(state) : updater;
    state = { ...state, ...(patch as object) };
  }) as unknown as AppStoreSetState;
  const activeRequestsRef = { current: isProcessing ? 1 : 0 };
  const activeChatIdRef = { current: 'chat-1' as string | null };
  const hook = renderHook(
    ({ processing }: { processing: boolean }) =>
      useRunStateReconciler({ isProcessing: processing, activeChatIdRef, activeRequestsRef, setState }),
    { initialProps: { processing: isProcessing } },
  );
  return { hook, getState: () => state, activeRequestsRef };
}

async function tick() {
  await act(async () => {
    vi.advanceTimersByTime(RUN_STATE_POLL_MS);
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  resetRunEpochs();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useRunStateReconciler', () => {
  it('clears a stuck running indicator after two disagreeing polls', async () => {
    fetchRunning.mockResolvedValue(false);
    const h = setup(true);
    await tick();
    expect(h.getState().isProcessing).toBe(true);
    await tick();
    expect(h.getState().isProcessing).toBe(false);
    expect(h.activeRequestsRef.current).toBe(0);
    expect(fetchRunning).toHaveBeenCalledWith('chat-1');
  });

  it('turns the indicator on when the server runs a chat the UI shows idle', async () => {
    fetchRunning.mockResolvedValue(true);
    const h = setup(false);
    await tick();
    await tick();
    expect(h.getState().isProcessing).toBe(true);
  });

  it('never acts on a single disagreement', async () => {
    fetchRunning.mockResolvedValueOnce(false).mockResolvedValueOnce(true).mockResolvedValueOnce(false);
    const h = setup(true);
    await tick();
    await tick();
    await tick();
    expect(h.getState().isProcessing).toBe(true);
  });

  it('starts counting again after a run starts between polls', async () => {
    fetchRunning.mockResolvedValue(false);
    const h = setup(true);
    await tick();
    noteRunStarted('chat-1');
    await tick();
    expect(h.getState().isProcessing).toBe(true);
    await tick();
    expect(h.getState().isProcessing).toBe(false);
  });
});
