// @vitest-environment jsdom

/**
 * useSecurityHandlers.test.ts — covers handleAskUserResponse for a hosted vs
 * local shell.
 *
 * Hosted (no local terminal): the agent loop runs in the WASM binary, so the
 * response must be POSTed to the wasm-local /api/ask-user/response endpoint
 * via clientFetch — NOT sent as a WebSocket event (no backend is listening
 * for it on a hosted shell).
 *
 * Local terminal: the response is delivered as an 'ask_user_response'
 * WebSocket event via eventsProvider.sendEvent — clientFetch must not be
 * touched.
 *
 * Both paths must clear the askUserRequest dialog state via setState.
 *
 * The hook reads its capability through the host, so the suite supplies it via
 * a HostProvider wrapper (localTerminal: false = hosted, true = local).
 */

import { act, renderHook } from '@testing-library/react';
import { createElement } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';

// ── Hoisted mocks ─────────────────────────────────────────────────────────
// vi.mock factories run before module code, so the clientFetch spy must live
// in a vi.hoisted block.

const mocks = vi.hoisted(() => ({
  clientFetch: vi.fn(),
}));

vi.mock('../services/clientSession', () => ({
  clientFetch: mocks.clientFetch,
}));

import { useSecurityHandlers } from './useSecurityHandlers';

// ── Test doubles ──────────────────────────────────────────────────────────

function makeEventsProvider() {
  return {
    sendEvent: vi.fn(),
    isConnected: vi.fn().mockReturnValue(true),
  };
}

// localTerminal: false = the hosted (WASM) shell (POST path); true = a local
// terminal (WebSocket path).
function renderSecurityHandlers(localTerminal: boolean) {
  const eventsProvider = makeEventsProvider();
  const setState = vi.fn();

  const { result } = renderHook(
    () =>
      useSecurityHandlers({
        eventsProvider,
        provider: 'anthropic',
        setState,
      }),
    {
      wrapper: ({ children }) => createElement(HostProvider, { host: makeTestHost({ localTerminal }) }, children),
    },
  );

  return { result, eventsProvider, setState };
}

beforeEach(() => {
  mocks.clientFetch.mockReset();
  mocks.clientFetch.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ── Tests ─────────────────────────────────────────────────────────────────

/** Flush pending microtasks so the clientFetch .then/.catch chain runs. */
async function flushMicrotasks() {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

/** Build a clientFetch mock response with a given status and JSON body. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('handleAskUserResponse (hosted shell — no local terminal)', () => {
  it('POSTs the response to the wasm-local endpoint via clientFetch', async () => {
    mocks.clientFetch.mockResolvedValue(jsonResponse(200, { delivered: true }));
    const { result } = renderSecurityHandlers(false);

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });

    expect(mocks.clientFetch).toHaveBeenCalledTimes(1);
    expect(mocks.clientFetch).toHaveBeenCalledWith(
      '/api/ask-user/response',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: 'req-123', response: 'yes please' }),
      }),
    );
    await flushMicrotasks();
  });

  it('clears the askUserRequest state when delivered:true', async () => {
    mocks.clientFetch.mockResolvedValue(jsonResponse(200, { delivered: true }));
    const { result, setState } = renderSecurityHandlers(false);

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(setState).toHaveBeenCalledTimes(1);
    const updater = setState.mock.calls[0][0];
    expect(updater({ askUserRequest: { requestId: 'req-123' } })).toEqual({
      askUserRequest: null,
    });
  });

  it('does NOT clear state when the endpoint returns 404 (delivered:false)', async () => {
    mocks.clientFetch.mockResolvedValue(jsonResponse(404, { error: 'not found' }));
    const { result, setState } = renderSecurityHandlers(false);

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(setState).toHaveBeenCalledTimes(1);
    const updater = setState.mock.calls[0][0];
    const next = updater({ askUserRequest: { requestId: 'req-123' } });
    // Dialog stays open (state preserved) with a visible delivery error.
    expect(next.askUserRequest).not.toBeNull();
    expect(next.askUserRequest?.requestId).toBe('req-123');
    expect(next.askUserRequest?.deliveryError).toBeTruthy();
  });

  it('does NOT clear state on a network error', async () => {
    mocks.clientFetch.mockRejectedValue(new Error('network down'));
    const { result, setState } = renderSecurityHandlers(false);

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(setState).toHaveBeenCalledTimes(1);
    const updater = setState.mock.calls[0][0];
    const next = updater({ askUserRequest: { requestId: 'req-123' } });
    expect(next.askUserRequest).not.toBeNull();
    expect(next.askUserRequest?.requestId).toBe('req-123');
    expect(next.askUserRequest?.deliveryError).toContain('network down');
  });

  it('does NOT send a WebSocket event', async () => {
    mocks.clientFetch.mockResolvedValue(jsonResponse(200, { delivered: true }));
    const { result, eventsProvider } = renderSecurityHandlers(false);

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(eventsProvider.sendEvent).not.toHaveBeenCalled();
  });
});

describe('handleAskUserResponse (local terminal)', () => {
  it('sends the ask_user_response WebSocket event with the correct payload', () => {
    const { result, eventsProvider } = renderSecurityHandlers(true);

    act(() => {
      result.current.handleAskUserResponse('req-456', 'no thanks');
    });

    expect(eventsProvider.sendEvent).toHaveBeenCalledTimes(1);
    expect(eventsProvider.sendEvent).toHaveBeenCalledWith({
      type: 'ask_user_response',
      data: { request_id: 'req-456', response: 'no thanks' },
    });
  });

  it('clears the askUserRequest state', () => {
    const { result, setState } = renderSecurityHandlers(true);

    act(() => {
      result.current.handleAskUserResponse('req-456', 'no thanks');
    });

    expect(setState).toHaveBeenCalledTimes(1);
    const updater = setState.mock.calls[0][0];
    expect(updater({ askUserRequest: { request_id: 'req-456' } })).toEqual({
      askUserRequest: null,
    });
  });

  it('does NOT call clientFetch', () => {
    const { result } = renderSecurityHandlers(true);

    act(() => {
      result.current.handleAskUserResponse('req-456', 'no thanks');
    });

    expect(mocks.clientFetch).not.toHaveBeenCalled();
  });
});
