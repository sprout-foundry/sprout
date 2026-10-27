/**
 * Tests for fullWorkspace.ts — the full-workspace availability probe and
 * the create-and-enter launch.
 *
 * The probe reads the typed availability endpoint (GET /workspace/availability):
 * a deployment without cloud compute answers `available: false` instead of
 * 503-ing the legacy list route on every project load. A definitive
 * `available: false` is the only answer that hides the "Full workspace"
 * option; a missing endpoint (a local daemon without the route), a transient
 * outage or a network failure fail open so the user can retry.
 *
 * The launch POSTs the host-agnostic cloud create (POST /workspace/txn with
 * host=cloud), not the legacy POST /workspace/fly start, and opens whichever
 * backend answered (the Fly url/session_token pair or the Firecracker
 * gateway_url/gateway_access_token pair) through <url>/auth/exchange.
 *
 * Network calls are stubbed on clientFetch, same interception style as
 * cloudTxn.test.ts.
 */

import { renderHook, waitFor } from '@testing-library/react';

import {
  __resetFullWorkspaceForTests,
  probeFullWorkspaces,
  startFullWorkspace,
  useFullWorkspacesAvailable,
} from './fullWorkspace';

const fetchMock = vi.fn();
vi.mock('./clientSession', () => ({
  clientFetch: (...a: unknown[]) => fetchMock(...a),
}));

/** A JSON availability response. */
function availabilityResponse(available: boolean, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => ({ available }),
  };
}

describe('probeFullWorkspaces', () => {
  beforeEach(() => {
    __resetFullWorkspaceForTests();
    fetchMock.mockReset();
  });

  it('probes the typed availability endpoint, not the legacy list route', async () => {
    fetchMock.mockResolvedValue(availabilityResponse(true));
    await probeFullWorkspaces();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith('/workspace/availability', { credentials: 'include' });
  });

  it('a definitive available: false hides the option', async () => {
    fetchMock.mockResolvedValue(availabilityResponse(false));
    const { result } = renderHook(() => useFullWorkspacesAvailable());
    // The probe is in flight: the option is still offered.
    expect(result.current).toBe(true);
    await waitFor(() => expect(result.current).toBe(false));
  });

  it('available: true keeps the option offered', async () => {
    fetchMock.mockResolvedValue(availabilityResponse(true));
    const { result } = renderHook(() => useFullWorkspacesAvailable());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(result.current).toBe(true);
  });

  it('a missing endpoint (404) fails open: the option stays on offer', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404, json: async () => ({}) });
    const { result } = renderHook(() => useFullWorkspacesAvailable());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(result.current).toBe(true);
  });

  it('a non-boolean answer is ignored: the option stays on offer', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ available: 'nope' }),
    });
    const { result } = renderHook(() => useFullWorkspacesAvailable());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(result.current).toBe(true);
  });

  it('a network failure fails open', async () => {
    fetchMock.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useFullWorkspacesAvailable());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(result.current).toBe(true);
  });

  it('probes once per page, not once per mount', async () => {
    fetchMock.mockResolvedValue(availabilityResponse(true));
    await probeFullWorkspaces();
    await probeFullWorkspaces();
    const { unmount } = renderHook(() => useFullWorkspacesAvailable());
    unmount();
    renderHook(() => useFullWorkspacesAvailable());
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('a disabled mount does not start the probe', async () => {
    const { result } = renderHook(() => useFullWorkspacesAvailable(false));
    await waitFor(() => expect(fetchMock).not.toHaveBeenCalled());
    expect(result.current).toBe(true);
  });
});

describe('startFullWorkspace', () => {
  /** A JSON create response with the given status. */
  function createResponse(body: unknown, status = 201) {
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    };
  }

  beforeEach(() => {
    __resetFullWorkspaceForTests();
    fetchMock.mockReset();
    // jsdom's location is read-only; the launch navigates by assignment.
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { href: 'http://localhost/' },
    });
  });

  it('POSTs the host-agnostic cloud create, not the legacy /workspace/fly start', async () => {
    fetchMock.mockResolvedValue(createResponse({ workspace_id: 'ws-1', backend: 'fly', created: true }));
    await startFullWorkspace('https://github.com/o/r');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/txn');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(JSON.parse(String(init.body))).toEqual({
      repo_url: 'https://github.com/o/r',
      host: 'cloud',
    });
  });

  it('opens a Fly answer through url/session_token', async () => {
    fetchMock.mockResolvedValue(
      createResponse({
        workspace_id: 'ws-1',
        backend: 'fly',
        created: false,
        url: 'https://ws-1.fly.dev',
        session_token: 'tok-fly',
      }),
    );
    const result = await startFullWorkspace('https://github.com/o/r');
    expect(result).toEqual({ kind: 'redirecting' });
    expect(window.location.href).toBe('https://ws-1.fly.dev/auth/exchange?token=tok-fly');
  });

  it('opens a Firecracker answer through gateway_url/gateway_access_token', async () => {
    fetchMock.mockResolvedValue(
      createResponse({
        workspace_id: 'ws-2',
        backend: 'firecracker',
        created: true,
        gateway_url: 'https://ws-2.ws.example.com',
        gateway_access_token: 'tok-fc',
      }),
    );
    const result = await startFullWorkspace('https://github.com/o/r');
    expect(result).toEqual({ kind: 'redirecting' });
    expect(window.location.href).toBe('https://ws-2.ws.example.com/auth/exchange?token=tok-fc');
  });

  it('a create without open fields reports its status instead of navigating', async () => {
    fetchMock.mockResolvedValue(createResponse({ workspace_id: 'ws-3', status: 'starting' }));
    const result = await startFullWorkspace('https://github.com/o/r');
    expect(result).toEqual({ kind: 'status', status: 'starting' });
    expect(window.location.href).toBe('http://localhost/');
  });

  it('a 503 "not available on this deployment" flips the option to unavailable', async () => {
    fetchMock.mockResolvedValue(
      createResponse({ error: 'workspaces are not available on this deployment' }, 503),
    );
    const result = await startFullWorkspace('https://github.com/o/r');
    expect(result).toEqual({ kind: 'unavailable' });
    // The availability store learned it: the option is hidden without a probe.
    const { result: available } = renderHook(() => useFullWorkspacesAvailable(false));
    expect(available.current).toBe(false);
  });

  it('any other failure is an error carrying the platform message', async () => {
    fetchMock.mockResolvedValue(createResponse({ error: 'at_capacity' }, 503));
    const result = await startFullWorkspace('https://github.com/o/r');
    expect(result).toEqual({ kind: 'error', message: 'at_capacity' });
  });
});
