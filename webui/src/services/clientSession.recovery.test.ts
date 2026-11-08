import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

/** A host whose agent daemon is on another origin (the API behind a tunnel). */
function crossOriginDaemonHost() {
  return {
    transport: {
      apiBaseURL: '',
      wsURL: '',
      authMode: 'none',
      agent: { kind: 'daemon', apiBaseURL: 'https://daemon.example.test', wsURL: '' },
    },
  };
}

/** The platform host once its workspace is up: the daemon behind a same-origin proxy path. */
function proxiedDaemonHost() {
  return {
    transport: {
      apiBaseURL: '',
      wsURL: '',
      authMode: 'none',
      agent: { kind: 'daemon', apiBaseURL: '/workspace/ws-1/proxy', wsURL: '' },
    },
  };
}

describe('resolveWebUIClientId boot race', () => {
  beforeEach(() => {
    vi.resetModules();
    window.sessionStorage.clear();
    window.localStorage.clear();
    document.cookie = 'sprout_client_id=; expires=Thu, 01 Jan 1970 00:00:00 GMT';
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.doUnmock('../host/accessor');
  });

  it('keeps the id the event socket stored while recovery awaited the server', async () => {
    vi.doMock('../host/accessor', () => ({
      getActiveHost: () => crossOriginDaemonHost(),
    }));
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

  it("asks the agent's daemon when it is on another origin", async () => {
    vi.doMock('../host/accessor', () => ({ getActiveHost: () => crossOriginDaemonHost() }));
    const fetchMock = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const { resolveWebUIClientId } = await import('./clientSession');

    await resolveWebUIClientId();
    expect(fetchMock.mock.calls[0][0]).toBe('https://daemon.example.test/api/query/status');
  });

  it('asks nothing when the server shares the page origin', async () => {
    for (const host of [null, proxiedDaemonHost()]) {
      vi.resetModules();
      window.sessionStorage.clear();
      vi.doMock('../host/accessor', () => ({ getActiveHost: () => host }));
      const fetchMock = vi.fn(async () => new Response(null, { status: 200 }));
      vi.stubGlobal('fetch', fetchMock);
      const { resolveWebUIClientId } = await import('./clientSession');

      expect(await resolveWebUIClientId()).toBeTruthy();
      expect(fetchMock).not.toHaveBeenCalled();
    }
  });
});
