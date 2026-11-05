// @vitest-environment jsdom

/**
 * useSecurityHandlers.test.ts — covers handleAskUserResponse per agent
 * backend.
 *
 * In-browser agent: the agent loop runs in the WASM binary, so the response
 * must be POSTed to the wasm-local /api/ask-user/response endpoint via
 * clientFetch — NOT sent as a WebSocket event (no backend is listening for
 * it).
 *
 * Daemon agent (a local daemon, or a hosted workspace's daemon reached
 * through the host): the response is delivered as an 'ask_user_response'
 * WebSocket event via eventsProvider.sendEvent — clientFetch must not be
 * touched. A hosted host declares no local terminal, so the routing must
 * follow the agent backend, not that capability.
 *
 * Both paths must clear the askUserRequest dialog state via setState.
 *
 * The hook reads the agent backend through the host, so the suite supplies it
 * via a HostProvider wrapper.
 */

import { act, renderHook } from '@testing-library/react';
import { createElement } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import type { SproutHost } from '../host/types';

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

type AgentHost = 'wasm' | 'local' | 'hosted-daemon';

/** A host per agent backend: the in-browser agent, a local daemon, a hosted daemon. */
function hostFor(kind: AgentHost): SproutHost {
  const host = makeTestHost({ localTerminal: kind === 'local' });
  if (kind === 'wasm') return { ...host, transport: { ...host.transport, agent: { kind: 'wasm', modelEndpoint: '' } } };
  if (kind === 'hosted-daemon') {
    return {
      ...host,
      transport: {
        ...host.transport,
        authMode: 'bearer',
        agent: {
          kind: 'daemon',
          apiBaseURL: '/workspace/ws-1/proxy',
          wsURL: 'wss://platform.test/workspace/ws-1/proxy/ws',
        },
      },
    };
  }
  return host;
}

function renderSecurityHandlers(kind: AgentHost) {
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
      wrapper: ({ children }) => createElement(HostProvider, { host: hostFor(kind) }, children),
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

describe('handleAskUserResponse (in-browser agent)', () => {
  it('POSTs the response to the wasm-local endpoint via clientFetch', async () => {
    mocks.clientFetch.mockResolvedValue(jsonResponse(200, { delivered: true }));
    const { result } = renderSecurityHandlers('wasm');

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
    const { result, setState } = renderSecurityHandlers('wasm');

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
    const { result, setState } = renderSecurityHandlers('wasm');

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
    const { result, setState } = renderSecurityHandlers('wasm');

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
    const { result, eventsProvider } = renderSecurityHandlers('wasm');

    act(() => {
      result.current.handleAskUserResponse('req-123', 'yes please');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(eventsProvider.sendEvent).not.toHaveBeenCalled();
  });
});

describe('handleAskUserResponse (local daemon)', () => {
  it('sends the ask_user_response WebSocket event with the correct payload', () => {
    const { result, eventsProvider } = renderSecurityHandlers('local');

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
    const { result, setState } = renderSecurityHandlers('local');

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
    const { result } = renderSecurityHandlers('local');

    act(() => {
      result.current.handleAskUserResponse('req-456', 'no thanks');
    });

    expect(mocks.clientFetch).not.toHaveBeenCalled();
  });
});

describe('handleAskUserResponse (hosted daemon)', () => {
  it('answers over the socket, not the in-browser endpoint, though the host has no local terminal', () => {
    const { result, eventsProvider } = renderSecurityHandlers('hosted-daemon');

    act(() => {
      result.current.handleAskUserResponse('req-789', 'go ahead');
    });

    expect(eventsProvider.sendEvent).toHaveBeenCalledWith({
      type: 'ask_user_response',
      data: { request_id: 'req-789', response: 'go ahead' },
    });
    expect(mocks.clientFetch).not.toHaveBeenCalled();
  });
});
