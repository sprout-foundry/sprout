import { beforeEach, describe, expect, it } from 'vitest';
import { __resetRecentReposForTests, getRecentRepos, recordRecentRepo } from './recentRepos';

beforeEach(() => {
  window.localStorage.clear();
  __resetRecentReposForTests();
});

describe('recentRepos', () => {
  it('keeps the newest first without duplicates', () => {
    recordRecentRepo('https://github.com/acme/one');
    recordRecentRepo('https://github.com/acme/two.git');
    recordRecentRepo('https://github.com/acme/one');
    expect(getRecentRepos()).toEqual(['https://github.com/acme/one', 'https://github.com/acme/two']);
  });

  it('caps the list', () => {
    for (let i = 0; i < 10; i++) recordRecentRepo(`https://github.com/acme/r${i}`);
    expect(getRecentRepos()).toHaveLength(6);
    expect(getRecentRepos()[0]).toBe('https://github.com/acme/r9');
  });
});
