/**
 * useAttachableSessions — tests for the attachable-sessions fetch + polling + WS
 * event pipeline hook extracted from Terminal.tsx during SP-075-extension.
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi, beforeAll } from 'vitest';

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

import { clientFetch } from '../services/clientSession';
import { useAttachableSessions } from './useAttachableSessions';

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type CapturedState = ReturnType<typeof useAttachableSessions>;

function Harness({ isExpanded, onState }: { isExpanded: boolean; onState: (state: CapturedState) => void }): null {
  const result = useAttachableSessions(isExpanded);
  onState(result);
  return null;
}

let container: HTMLDivElement;
let root: Root;
let lastState: CapturedState | null = null;
const stateCallback = (s: CapturedState) => {
  lastState = s;
};

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  vi.useFakeTimers();
  lastState = null;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  vi.useRealTimers();
  container.remove();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockClientFetchSessions(sessions: Array<{ id: string; name?: string; status?: string }>) {
  vi.mocked(clientFetch).mockResolvedValue({
    ok: true,
    json: async () => ({ sessions }),
  } as never);
}

function mockClientFetchError(message = 'fail') {
  vi.mocked(clientFetch).mockRejectedValue(new Error(message));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useAttachableSessions', () => {
  describe('initial fetch', () => {
    it('fetches on mount', async () => {
      mockClientFetchSessions([
        { id: 's1', name: 'agent-1', status: 'active' },
        { id: 's2', name: 'agent-2', status: 'inactive' },
      ]);

      // Render — initial fetch fires from the polling effect
      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      // Flush the microtask for the resolved promise
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toEqual([
        { id: 's1', name: 'agent-1', status: 'active' },
        { id: 's2', name: 'agent-2', status: 'inactive' },
      ]);
      expect(vi.mocked(clientFetch)).toHaveBeenCalledWith('/api/terminal/agent-sessions');
    });

    it('normalises missing name to id and unknown status to "inactive"', async () => {
      mockClientFetchSessions([{ id: 'only-id' }]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toEqual([{ id: 'only-id', name: 'only-id', status: 'inactive' }]);
    });

    it('handles empty sessions list', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toEqual([]);
    });

    it('swallows fetch error and clears list to []', async () => {
      mockClientFetchError('boom');

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toEqual([]);
    });

    it('handles !response.ok by treating as error', async () => {
      vi.mocked(clientFetch).mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({}),
      } as never);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toEqual([]);
    });
  });

  describe('concurrent fetch guard', () => {
    it('skips a re-entry while a fetch is in-flight', async () => {
      let resolveFirst: ((v: unknown) => void) | null = null;
      vi.mocked(clientFetch).mockReturnValue(
        new Promise((resolve) => {
          resolveFirst = resolve;
        }) as never,
      );

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      // First fetch is still pending — trigger second via the hook's fetcher
      await act(async () => {
        await lastState!.fetchAttachableSessions();
      });

      // Only the first fetch should have been issued
      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);

      // Resolve first
      await act(async () => {
        resolveFirst?.({ ok: true, json: async () => ({ sessions: [] }) });
        await vi.advanceTimersByTimeAsync(0);
      });

      // Still only 1 — second call was a no-op
      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);
    });
  });

  describe('polling', () => {
    it('polls every 5s while expanded', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: true, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      // 1 initial fetch + 0 polling (no 5s elapsed yet)
      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);

      // Advance 5s — should add 1 poll
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(2);

      // Advance 10s more — should add 2 more polls
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10000);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(4);
    });

    it('does not poll while collapsed', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);

      // Advance 20s — polling loop runs but conditional is false
      await act(async () => {
        await vi.advanceTimersByTimeAsync(20000);
      });

      // Still only the initial fetch — no polls
      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);
    });

    it('clears the interval on unmount', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: true, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      const beforeUnmount = vi.mocked(clientFetch).mock.calls.length;

      await act(async () => {
        root.unmount();
      });

      // Advance 30s — no new polls should fire
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30000);
      });

      expect(vi.mocked(clientFetch).mock.calls.length).toBe(beforeUnmount);
    });
  });

  describe('WS event re-fetch', () => {
    it('re-fetches on sprout:wsevent of terminal_output', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);

      await act(async () => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type: 'terminal_output' } }));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(2);
    });

    it('re-fetches on sprout:wsevent of pty_exit', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      await act(async () => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type: 'pty_exit' } }));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(2);
    });

    it('re-fetches on sprout:wsevent of agent_session_update', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      await act(async () => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type: 'agent_session_update' } }));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(2);
    });

    it('ignores other event types', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      await act(async () => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type: 'unrelated_event' } }));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);
    });

    it('removes the WS listener on unmount', async () => {
      mockClientFetchSessions([]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      await act(async () => {
        root.unmount();
      });

      await act(async () => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type: 'terminal_output' } }));
        await vi.advanceTimersByTimeAsync(0);
      });

      // No re-fetch after unmount
      expect(vi.mocked(clientFetch)).toHaveBeenCalledTimes(1);
    });
  });

  describe('setAttachableSessions', () => {
    it('updates state directly (used by useTerminalPanes)', async () => {
      mockClientFetchSessions([{ id: 'a' }]);

      await act(async () => {
        root.render(createElement(Harness, { isExpanded: false, onState: stateCallback }));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(lastState?.attachableSessions).toHaveLength(1);

      act(() => {
        lastState?.setAttachableSessions([{ id: 'manual', name: 'manual', status: 'active' }]);
      });

      expect(lastState?.attachableSessions).toEqual([{ id: 'manual', name: 'manual', status: 'active' }]);
    });
  });
});
