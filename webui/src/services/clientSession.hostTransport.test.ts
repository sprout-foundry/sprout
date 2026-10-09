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
});
