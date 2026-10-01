import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  UNREPORTED_CONTEXT_WINDOW,
  loadManagedContextWindow,
  platformProviderConfig,
  reportedManagedContextWindow,
  resetManagedContextWindow,
} from './platformProvider';

const modelsResponse = (body: unknown, ok = true) => vi.fn(async () => ({ ok, json: async () => body }) as Response);

afterEach(() => {
  vi.unstubAllGlobals();
  resetManagedContextWindow();
});

describe('managed model context window', () => {
  it('uses the window the platform reports', async () => {
    const fetchMock = modelsResponse({ data: [{ id: 'managed', context_length: 262144 }] });
    vi.stubGlobal('fetch', fetchMock);

    await loadManagedContextWindow('https://app.test');

    expect(fetchMock).toHaveBeenCalledWith('https://app.test/proxy/chat/models', { credentials: 'include' });
    expect(reportedManagedContextWindow()).toBe(262144);
    expect(platformProviderConfig('https://app.test', reportedManagedContextWindow()).context_size).toBe(262144);
  });

  it.each([
    ['no window reported', modelsResponse({ data: [{ id: 'managed' }] })],
    ['an error status', modelsResponse({}, false)],
    [
      'an unreachable platform',
      vi.fn(async () => {
        throw new Error('offline');
      }),
    ],
  ])('leaves the window unknown on %s, without assuming a small model', async (_case, fetchMock) => {
    vi.stubGlobal('fetch', fetchMock);

    await loadManagedContextWindow('https://app.test');

    expect(reportedManagedContextWindow()).toBeUndefined();
    const size = platformProviderConfig('https://app.test', undefined).context_size;
    expect(size).toBe(UNREPORTED_CONTEXT_WINDOW);
    expect(size).toBeGreaterThanOrEqual(132_000);
  });
});
