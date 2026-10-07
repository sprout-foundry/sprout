/**
 * usePreviewStatus — the preview pane's polling + actions.
 *
 * Pins the load-bearing behaviors:
 *   1. Polls once on open and reflects the current state (stopped/failed/
 *      running, with url and the server's reason).
 *   2. Settles `starting -> running` on the fast (1s) poll cadence.
 *   3. Uses a slow (5s) heartbeat when not starting.
 *   4. Never polls while the surface is closed; polls once on reopen.
 *   5. start/restart/stop POST to the right endpoints and adopt the state.
 *   6. A refused action surfaces the server's error (actionError).
 *   7. hosted state passes through untouched.
 *   8. A debounced file-change event bumps the reload token (and a
 *      non-file event does not).
 *   9. A hung status poll never stacks requests (one in flight at a time)
 *      and is aborted when the surface closes.
 */

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePreviewStatus } from './usePreviewStatus';

type PreviewBody = {
  status: 'starting' | 'running' | 'stopped' | 'failed';
  url?: string;
  error?: string;
  detected?: boolean;
  hosted?: boolean;
};

// ---------------------------------------------------------------------------
// Mock the adapter fetch (the only transport the hook touches)
// ---------------------------------------------------------------------------

let statusBody: PreviewBody = { status: 'stopped' };
let statusErrorCode = 0; // 0 = ok (200); otherwise the status fetch returns this
let statusErrorMessage = '';
let statusHang = false; // a hung status transport (never resolves unless aborted)
let postBodies: Record<string, PreviewBody> = {};
let postStatusCodes: Record<string, number> = {};
let postCodes: Record<string, string> = {};
let postMessages: Record<string, string> = {};
let statusCalls = 0;
let abortedSignals = 0;
let posted: string[] = [];

const fetchMock = (input: unknown, init?: RequestInit): Promise<Response> => {
  const url = String(input);
  const method = String(init?.method ?? 'GET');
  if (url === '/api/preview/status') {
    statusCalls += 1;
    if (statusHang) {
      // A hung status transport: it settles only if the caller aborts (the
      // way a stuck real request behaves when given a signal).
      return new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => {
          abortedSignals += 1;
          reject(new DOMException('The operation was aborted.', 'AbortError'));
        });
      });
    }
    if (statusErrorCode !== 0) {
      const payload = { error: statusErrorMessage, code: 'transport' };
      return Promise.resolve(new Response(JSON.stringify(payload), { status: statusErrorCode }));
    }
    return Promise.resolve(new Response(JSON.stringify(statusBody), { status: 200 }));
  }
  posted.push(`${method} ${url}`);
  const body = postBodies[url] ?? { status: 'starting' };
  const status = postStatusCodes[url] ?? 200;
  const payload =
    status === 200 ? body : { error: postMessages[url] ?? 'request failed', code: postCodes[url] ?? 'error' };
  return Promise.resolve(new Response(JSON.stringify(payload), { status }));
};

vi.mock('../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => fetchMock,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Flush microtasks (a poll's fetch + setState) without advancing time. */
const flush = async (): Promise<void> => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
};

beforeEach(() => {
  vi.useFakeTimers();
  statusBody = { status: 'stopped' };
  statusErrorCode = 0;
  statusErrorMessage = '';
  statusHang = false;
  postBodies = {};
  postStatusCodes = {};
  postCodes = {};
  postMessages = {};
  statusCalls = 0;
  abortedSignals = 0;
  posted = [];
});

afterEach(() => {
  vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Polling lifecycle
// ---------------------------------------------------------------------------

describe('usePreviewStatus polling', () => {
  it('polls once on open and reflects a running state with the url', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('running');
    expect(result.current.url).toBe('http://localhost:3000');
    expect(statusCalls).toBe(1);
  });

  it('settles starting -> running on the fast poll cadence', async () => {
    statusBody = { status: 'starting' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('starting');

    // The dev server comes up; the next fast poll (1s) reports running.
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(result.current.status).toBe('running');
    expect(result.current.url).toBe('http://localhost:3000');
  });

  it('reports a stopped state with the server reason', async () => {
    statusBody = {
      status: 'stopped',
      error: 'no starter manifest: the project declares no dev server',
    };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('stopped');
    expect(result.current.error).toBe('no starter manifest: the project declares no dev server');
  });

  it('reports a failed state with the error reason', async () => {
    statusBody = { status: 'failed', error: 'dev server exited with code 1' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('failed');
    expect(result.current.error).toBe('dev server exited with code 1');
  });

  it('uses a slow heartbeat (not the fast cadence) when not starting', async () => {
    statusBody = { status: 'stopped' };
    renderHook(() => usePreviewStatus(true));
    await flush(); // initial poll
    const afterInit = statusCalls;
    // Under 5s: no further poll.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000);
    });
    expect(statusCalls).toBe(afterInit);
    // Cross the 5s heartbeat: one more poll.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(statusCalls).toBe(afterInit + 1);
  });

  it('does not poll while the surface is closed, and polls once on reopen', async () => {
    statusBody = { status: 'stopped' };
    const { rerender } = renderHook(({ enabled }: { enabled: boolean }) => usePreviewStatus(enabled), {
      initialProps: { enabled: false },
    });
    // Closed: no polling, even over a long span.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(statusCalls).toBe(0);

    // Reopen: exactly one poll.
    rerender({ enabled: true });
    await flush();
    expect(statusCalls).toBe(1);
  });

  it('keeps the last known state when a poll cannot reach the server', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('running');

    // The next heartbeat cannot reach the server (500).
    statusErrorCode = 500;
    statusErrorMessage = 'upstream timeout';
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    // A transport failure is not a state: the last known state is retained.
    expect(result.current.status).toBe('running');
    expect(result.current.url).toBe('http://localhost:3000');
  });

  it('never stacks status requests: a hung poll blocks the later ticks', async () => {
    statusBody = { status: 'stopped' };
    statusHang = true;
    renderHook(() => usePreviewStatus(true));
    await flush(); // the initial poll hangs (one request in flight)
    expect(statusCalls).toBe(1);

    // Ticks keep arriving while the poll is hung: each one skips rather
    // than starting a second in-flight request.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(statusCalls).toBe(1);
    // Nothing was aborted while the surface stayed open.
    expect(abortedSignals).toBe(0);
  });

  it('aborts the in-flight poll when the surface closes', async () => {
    statusBody = { status: 'stopped' };
    statusHang = true;
    const { unmount } = renderHook(() => usePreviewStatus(true));
    await flush(); // the initial poll hangs
    expect(statusCalls).toBe(1);

    // Closing the surface (unmount) aborts the pending poll.
    await act(async () => {
      unmount();
    });
    expect(abortedSignals).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

describe('usePreviewStatus actions', () => {
  it('start() posts to /api/preview/start and adopts the returned state', async () => {
    statusBody = { status: 'stopped' };
    postBodies['/api/preview/start'] = { status: 'starting' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('stopped');

    act(() => {
      result.current.start();
    });
    await flush();
    expect(posted).toContain('POST /api/preview/start');
    expect(result.current.status).toBe('starting');
  });

  it('restart() posts to /api/preview/restart', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    postBodies['/api/preview/restart'] = { status: 'starting' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('running');

    act(() => {
      result.current.restart();
    });
    await flush();
    expect(posted).toContain('POST /api/preview/restart');
    expect(result.current.status).toBe('starting');
  });

  it('stop() posts to /api/preview/stop and settles to stopped', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    postBodies['/api/preview/stop'] = { status: 'stopped' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('running');

    act(() => {
      result.current.stop();
    });
    await flush();
    expect(posted).toContain('POST /api/preview/stop');
    expect(result.current.status).toBe('stopped');
    expect(result.current.url).toBeUndefined();
  });

  it('surfaces a refused action (no dev command) as actionError', async () => {
    statusBody = { status: 'stopped' };
    postStatusCodes['/api/preview/start'] = 404;
    postCodes['/api/preview/start'] = 'no_dev_command';
    postMessages['/api/preview/start'] = 'no starter manifest: the project declares no dev server';
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();

    act(() => {
      result.current.start();
    });
    await flush();
    expect(result.current.actionError).toBe('no_dev_command: no starter manifest: the project declares no dev server');
    // The failed action leaves the state as it was (still stopped).
    expect(result.current.status).toBe('stopped');
  });

  it('passes through a hosted (platform-registered) preview untouched', async () => {
    statusBody = { status: 'running', url: 'https://preview.example.com/abc', hosted: true };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.status).toBe('running');
    expect(result.current.url).toBe('https://preview.example.com/abc');
    expect(result.current.hosted).toBe(true);
    expect(result.current.detected).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Reload on file changes
// ---------------------------------------------------------------------------

describe('usePreviewStatus reload key', () => {
  const emit = (type: string, data: Record<string, unknown> = {}): void => {
    window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: { type, data } }));
  };

  it('bumps the reload key (debounced) on a file-changing event', async () => {
    statusBody = { status: 'stopped' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();
    expect(result.current.reloadKey).toBe(0);

    emit('file_changed', { action: 'created' });
    // Debounce not yet elapsed.
    expect(result.current.reloadKey).toBe(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(800);
    });
    expect(result.current.reloadKey).toBe(1);
  });

  it('coalesces a burst of file changes into one reload', async () => {
    statusBody = { status: 'stopped' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();

    emit('tool_end', { tool_name: 'write_file' });
    emit('file_changed', { action: 'created' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(800);
    });
    expect(result.current.reloadKey).toBe(1);
  });

  it('does not bump the reload key for a non-file event', async () => {
    statusBody = { status: 'stopped' };
    const { result } = renderHook(() => usePreviewStatus(true));
    await flush();

    emit('query_completed');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(800);
    });
    expect(result.current.reloadKey).toBe(0);
  });
});
