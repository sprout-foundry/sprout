import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('resolveWebUIClientId boot race', () => {
  beforeEach(() => {
    vi.resetModules();
    window.sessionStorage.clear();
    window.localStorage.clear();
    document.cookie = 'sprout_client_id=; expires=Thu, 01 Jan 1970 00:00:00 GMT';
  });
  afterEach(() => vi.unstubAllGlobals());

  it('keeps the id the event socket stored while recovery awaited the server', async () => {
    let finishRecovery!: (r: Response) => void;
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((resolve) => (finishRecovery = resolve))),
    );
    const { resolveWebUIClientId, getWebUIClientId } = await import('./clientSession');

    const resolving = resolveWebUIClientId();
    // Meanwhile the event WebSocket connects and mints the tab's id.
    const socketId = getWebUIClientId();
    finishRecovery(new Response(null, { status: 200 }));

    expect(await resolving).toBe(socketId);
    expect(getWebUIClientId()).toBe(socketId);
  });
});

describe('resolveWebUIClientId recovery probe', () => {
  beforeEach(() => {
    vi.resetModules();
    window.sessionStorage.clear();
    window.localStorage.clear();
    window.name = '';
    document.cookie = 'sprout_client_id=; expires=Thu, 01 Jan 1970 00:00:00 GMT';
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.doUnmock('../host/accessor');
  });

  it("asks the agent's daemon, not the page origin, when the host names one", async () => {
    vi.doMock('../host/accessor', () => ({
      getActiveHost: () => ({
        transport: {
          apiBaseURL: '',
          wsURL: '',
          authMode: 'none',
          agent: { kind: 'daemon', apiBaseURL: '/workspace/ws-1/proxy', wsURL: '' },
        },
      }),
    }));
    const fetchMock = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const { resolveWebUIClientId } = await import('./clientSession');

    await resolveWebUIClientId();
    expect(fetchMock.mock.calls[0][0]).toBe('/workspace/ws-1/proxy/api/query/status');
  });
});
