import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const cache = vi.hoisted(() => ({ last: null as string | null, set: vi.fn() }));

vi.mock('./repoImportCache', () => ({
  getLastRepo: () => Promise.resolve(cache.last),
  setLastRepo: (url: string) => {
    cache.set(url);
    return Promise.resolve();
  },
}));

import { __resetActiveRepoForTests, getActiveRepoURL, hydrateActiveRepoURL, setActiveRepoURL } from './activeRepo';

describe('activeRepo', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/webui/');
    cache.last = null;
    cache.set.mockClear();
    __resetActiveRepoForTests();
  });

  afterEach(() => {
    window.history.replaceState(null, '', '/');
  });

  it('prefers ?repo= over the last import', async () => {
    window.history.replaceState(null, '', '/webui/?repo=https://github.com/o/from-url');
    cache.last = 'https://github.com/o/last';
    __resetActiveRepoForTests();
    await hydrateActiveRepoURL();
    expect(getActiveRepoURL()).toBe('https://github.com/o/from-url');
  });

  it('falls back to the last import when the URL names no repo', async () => {
    cache.last = 'https://github.com/o/last';
    await hydrateActiveRepoURL();
    expect(getActiveRepoURL()).toBe('https://github.com/o/last');
  });

  it('records and persists a clone started in the editor', () => {
    setActiveRepoURL('https://github.com/o/cloned.git');
    expect(getActiveRepoURL()).toBe('https://github.com/o/cloned.git');
    expect(cache.set).toHaveBeenCalledWith('https://github.com/o/cloned.git');
  });
});
