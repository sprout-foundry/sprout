/**
 * startupRepoImport — the in-browser workspace's startup repo import, shared
 * by the bootstrap's adapter install and a mounted workspace's.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  __resetStartupRepoImportForTests,
  claimHostProjectRepo,
  importStartupRepo,
  releaseHostProjectRepo,
} from './startupRepoImport';

const HOST_REPO = 'https://github.com/acme/widgets';
const DEEP_LINK = 'https://github.com/other/thing';

function adapter(result: { success: boolean; repo?: string; error?: string } = { success: true }) {
  return { restoreRepo: vi.fn(async (repo: string) => ({ repo, ...result })) };
}

afterEach(() => {
  __resetStartupRepoImportForTests();
  vi.restoreAllMocks();
  delete (window as unknown as Record<string, unknown>).__repoImported;
  delete (window as unknown as Record<string, unknown>).__repoImportFailed;
});

describe('importStartupRepo', () => {
  it('imports a repo once, however many installs ask for it', async () => {
    const first = adapter();
    const second = adapter();
    const imported = vi.fn();
    window.addEventListener('sprout:repo-imported', imported);

    await importStartupRepo(first, HOST_REPO, 'host');
    await importStartupRepo(second, HOST_REPO, 'host');
    await importStartupRepo(second, HOST_REPO, 'bootstrap');

    expect(first.restoreRepo).toHaveBeenCalledTimes(1);
    expect(second.restoreRepo).not.toHaveBeenCalled();
    expect(imported).toHaveBeenCalledTimes(1);
    expect((window as unknown as Record<string, unknown>).__repoImported).toBe(HOST_REPO);
    window.removeEventListener('sprout:repo-imported', imported);
  });

  it('lets a later caller retry a failed import', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const failing = adapter({ success: false, error: 'clone failed' });
    const working = adapter();

    expect((await importStartupRepo(failing, HOST_REPO, 'host'))?.success).toBe(false);
    expect((window as unknown as Record<string, unknown>).__repoImportFailed).toBe('clone failed');
    await importStartupRepo(working, HOST_REPO, 'host');

    expect(working.restoreRepo).toHaveBeenCalledTimes(1);
  });

  it("does not import the bootstrap's repo over a host project's claim", async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    claimHostProjectRepo(HOST_REPO);
    const bootstrap = adapter();

    expect(importStartupRepo(bootstrap, DEEP_LINK, 'bootstrap')).toBeNull();
    expect(bootstrap.restoreRepo).not.toHaveBeenCalled();

    // The claimed repo itself is fine from either source, and a released
    // claim no longer blocks the bootstrap.
    await importStartupRepo(bootstrap, HOST_REPO, 'bootstrap');
    releaseHostProjectRepo();
    await importStartupRepo(bootstrap, DEEP_LINK, 'bootstrap');
    expect(bootstrap.restoreRepo.mock.calls.map(([repo]) => repo)).toEqual([HOST_REPO, DEEP_LINK]);
  });
});
