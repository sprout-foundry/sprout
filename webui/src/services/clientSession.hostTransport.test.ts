// @ts-nocheck
/**
 * clientFetch host-transport routing.
 *
 * The non-adapter path prefixes a relative API path with the active host's
 * `transport.apiBaseURL` when one is set, then the SSH proxy base
 * (`window.SPROUT_PROXY_BASE`), then leaves it same-origin. The local build
 * (no host) is unchanged: the proxy base still applies and a same-origin path
 * is untouched.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clientFetch } from './clientSession';

const activeHostRef = vi.hoisted(() => ({ value: null }));
vi.mock('../host/accessor', () => ({
  getActiveHost: () => activeHostRef.value,
}));

vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

function hostWithApiBase(apiBaseURL: string) {
  return { transport: { apiBaseURL, wsURL: '', authMode: 'none' } };
}

/**
 * The platform host once a project's workspace is running: the platform's own
 * base and socket on the transport, the daemon behind its workspace proxy.
 */
function platformDaemonHost(daemonBase = '/workspace/ws-1/proxy') {
  return {
    transport: {
      apiBaseURL: 'https://platform.test',
      wsURL: 'wss://platform.test/ws',
      authMode: 'bearer',
      agent: { kind: 'daemon', apiBaseURL: daemonBase, wsURL: 'wss://platform.test/workspace/ws-1/proxy/ws' },
    },
  };
}

describe('clientFetch host transport', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    activeHostRef.value = null;
    window.SPROUT_PROXY_BASE = '';
    fetchMock = vi.fn(async () => ({ ok: true, status: 200, headers: new Headers() }));
    global.fetch = fetchMock;
  });

  afterEach(() => {
    delete window.SPROUT_PROXY_BASE;
    vi.clearAllMocks();
  });

  it('prefixes a relative API path with the active host apiBaseURL', async () => {
    activeHostRef.value = hostWithApiBase('https://host.test/api');
    await clientFetch('/api/query', { method: 'POST' });
    expect(fetchMock).toHaveBeenCalledWith('https://host.test/api/api/query', expect.anything());
  });

  it('leaves a same-origin path untouched when the host apiBaseURL is the same-origin sentinel', async () => {
    activeHostRef.value = hostWithApiBase('');
    await clientFetch('/api/query');
    expect(fetchMock).toHaveBeenCalledWith('/api/query', expect.anything());
  });

  it('the local build (no host) is unchanged: the proxy base still prefixes', async () => {
    activeHostRef.value = null;
    window.SPROUT_PROXY_BASE = '/ssh/mac-mini%3A%3A%24HOME';
    await clientFetch('/api/query');
    expect(fetchMock).toHaveBeenCalledWith('/ssh/mac-mini%3A%3A%24HOME/api/query', expect.anything());
  });

  it('the local build (no host, no proxy) leaves a same-origin path untouched', async () => {
    activeHostRef.value = null;
    await clientFetch('/api/query');
    expect(fetchMock).toHaveBeenCalledWith('/api/query', expect.anything());
  });

  it('the host apiBaseURL wins over the proxy base when both are set', async () => {
    activeHostRef.value = hostWithApiBase('https://host.test');
    window.SPROUT_PROXY_BASE = '/ssh/mac-mini%3A%3A%24HOME';
    await clientFetch('/api/query');
    expect(fetchMock).toHaveBeenCalledWith('https://host.test/api/query', expect.anything());
  });

  it('does not prefix an absolute URL', async () => {
    activeHostRef.value = hostWithApiBase('https://host.test');
    await clientFetch('https://other.test/api/query');
    expect(fetchMock).toHaveBeenCalledWith('https://other.test/api/query', expect.anything());
  });

  describe('daemon agent backend', () => {
    it("prefixes /api/query with the daemon backend's base, not the host's", async () => {
      // The platform's transport base is the platform, which does not serve the
      // workspace's query route; the daemon's proxy base is where it lives.
      activeHostRef.value = platformDaemonHost();
      await clientFetch('/api/query', { method: 'POST' });
      expect(fetchMock).toHaveBeenCalledWith('/workspace/ws-1/proxy/api/query', expect.anything());
    });

    it('leaves an absolute host-store URL alone (the host owns its own store)', async () => {
      activeHostRef.value = platformDaemonHost();
      await clientFetch('https://platform.test/api/chat-sessions?project=acme');
      expect(fetchMock).toHaveBeenCalledWith('https://platform.test/api/chat-sessions?project=acme', expect.anything());
    });

    it("falls through to same-origin when the daemon's base is the sentinel", async () => {
      // A local daemon is same-origin; '' must not fall back to a transport
      // base the host named for something else.
      activeHostRef.value = platformDaemonHost('');
      activeHostRef.value.transport.apiBaseURL = '';
      await clientFetch('/api/query');
      expect(fetchMock).toHaveBeenCalledWith('/api/query', expect.anything());
    });
  });
});

describe('clientFetch with a daemon agent backend', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    window.SPROUT_PROXY_BASE = '';
    fetchMock = vi.fn(async () => ({ ok: true, status: 200, headers: new Headers() }));
    global.fetch = fetchMock;
  });

  afterEach(() => {
    delete window.SPROUT_PROXY_BASE;
    vi.clearAllMocks();
  });

  it("prefixes /api/query with the daemon backend's base, not the host's", async () => {
    // The platform's transport base is the platform, which does not serve the
    // workspace's query route; the daemon's proxy base is where it lives.
    activeHostRef.value = platformDaemonHost();
    await clientFetch('/api/query', { method: 'POST' });
    expect(fetchMock).toHaveBeenCalledWith('/workspace/ws-1/proxy/api/query', expect.anything());
  });

  it('leaves an absolute host-store URL alone (the host owns its own store)', async () => {
    activeHostRef.value = platformDaemonHost();
    await clientFetch('https://platform.test/api/chat-sessions?project=acme');
    expect(fetchMock).toHaveBeenCalledWith('https://platform.test/api/chat-sessions?project=acme', expect.anything());
  });

  it("falls through to same-origin when the daemon's base is the sentinel", async () => {
    // A local daemon is same-origin; '' must not fall back to a transport base
    // the host named for something else.
    const host = platformDaemonHost('');
    host.transport.apiBaseURL = '';
    activeHostRef.value = host;
    await clientFetch('/api/query');
    expect(fetchMock).toHaveBeenCalledWith('/api/query', expect.anything());
  });
});
