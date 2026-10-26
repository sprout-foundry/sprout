/**
 * Tests for fullWorkspace.ts — the full-workspace availability probe.
 *
 * The probe reads the typed availability endpoint (GET /workspace/availability):
 * a deployment without cloud compute answers `available: false` instead of
 * 503-ing the legacy list route on every project load. A definitive
 * `available: false` is the only answer that hides the "Full workspace"
 * option; a missing endpoint (a local daemon without the route), a transient
 * outage or a network failure fail open so the user can retry.
 *
 * Network calls are stubbed on clientFetch, same interception style as
 * cloudTxn.test.ts.
 */

import { renderHook, waitFor } from '@testing-library/react';

import {
  __resetFullWorkspaceForTests,
  probeFullWorkspaces,
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
