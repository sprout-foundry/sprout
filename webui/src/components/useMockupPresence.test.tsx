import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useMockupPresence } from './useMockupPresence';

// The hook reads the mockup path and re-probes on workspace file events.
// These tests pin the re-probe triggers: a draw lands mid-session with no
// focus change and no reload, so the event path is the one that matters.

const ctx = vi.hoisted(() => ({
  fetchFn: vi.fn(async () => ({ ok: false }) as Response),
  handlers: new Set<(event: unknown) => void>(),
}));

vi.mock('../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => ctx.fetchFn,
}));
vi.mock('../contexts/EventsContext', () => ({
  __esModule: true,
  useEvents: () => ({
    onEvent: (cb: (event: unknown) => void) => {
      ctx.handlers.add(cb);
    },
    removeEvent: (cb: (event: unknown) => void) => {
      ctx.handlers.delete(cb);
    },
  }),
}));

function emit(event: unknown): void {
  for (const handler of ctx.handlers) handler(event);
}

describe('useMockupPresence', () => {
  it('probes the mockup path on mount', async () => {
    ctx.fetchFn.mockClear();
    ctx.fetchFn.mockResolvedValue({ ok: true } as Response);
    const { result } = renderHook(() => useMockupPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(true);
    expect(ctx.fetchFn).toHaveBeenCalledTimes(1);
  });

  it('re-probes when a workspace file changes (a draw landing mid-session)', async () => {
    ctx.fetchFn.mockClear();
    ctx.fetchFn.mockResolvedValue({ ok: false } as Response);
    const { result } = renderHook(() => useMockupPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.present).toBe(false);

    // The agent writes design/mockup.html: the backend publishes file_changed.
    ctx.fetchFn.mockResolvedValue({ ok: true } as Response);
    emit({ type: 'file_changed', data: { file_path: 'design/mockup.html', action: 'write' } });

    await waitFor(() => expect(result.current.present).toBe(true));
    expect(ctx.fetchFn).toHaveBeenCalledTimes(2);
  });

  it('ignores events that cannot have written a file', async () => {
    ctx.fetchFn.mockClear();
    ctx.fetchFn.mockResolvedValue({ ok: false } as Response);
    const { result } = renderHook(() => useMockupPresence());
    await waitFor(() => expect(result.current.loading).toBe(false));

    emit({ type: 'agent_message', data: {} });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(ctx.fetchFn).toHaveBeenCalledTimes(1);
  });
});
