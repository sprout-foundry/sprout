import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  proxy: 'http://localhost/git-proxy' as string | undefined,
  origin: null as string | null,
  gitClone: vi.fn(),
  restoreGitWorkingTree: vi.fn(),
  whenBrowserGitConfigured: vi.fn(),
  loadRepoImport: vi.fn(),
}));

vi.mock('./gitCorsProxy', () => ({ gitCorsProxy: () => mocks.proxy }));
vi.mock('./browserGit', () => ({
  gitOriginUrl: () => Promise.resolve(mocks.origin),
  gitClone: mocks.gitClone,
  restoreGitWorkingTree: mocks.restoreGitWorkingTree,
  whenBrowserGitConfigured: mocks.whenBrowserGitConfigured,
}));
vi.mock('./wasmShell', () => ({
  initWasmShell: vi.fn(() => Promise.resolve({ writeFile: vi.fn(), readFile: vi.fn(), listDir: vi.fn() })),
  resetWasmShell: vi.fn(),
}));
vi.mock('./repoImportCache', () => ({
  loadRepoImport: mocks.loadRepoImport,
  saveRepoImport: vi.fn(),
  setLastRepo: vi.fn(),
  getLastRepo: vi.fn(() => Promise.resolve(null)),
}));

import { CloudAdapter } from './cloudAdapter';
import { GIT_REPO_CHANGED_EVENT } from './workspaceClone';

const adapter = () => new CloudAdapter({ apiBase: 'http://localhost', wsUrl: 'ws://localhost/ws', navItems: [] });

describe('CloudAdapter.restoreRepo with browser git', () => {
  beforeEach(() => {
    mocks.proxy = 'http://localhost/git-proxy';
    mocks.origin = null;
    mocks.whenBrowserGitConfigured.mockResolvedValue(undefined);
    mocks.gitClone.mockResolvedValue({ message: 'ok' });
    mocks.restoreGitWorkingTree.mockResolvedValue(0);
    mocks.loadRepoImport.mockResolvedValue(null);
  });

  afterEach(() => vi.clearAllMocks());

  it('clones a repository that is not yet checked out', async () => {
    const changed = vi.fn();
    window.addEventListener(GIT_REPO_CHANGED_EVENT, changed);
    const result = await adapter().restoreRepo('https://github.com/acme/web');
    window.removeEventListener(GIT_REPO_CHANGED_EVENT, changed);

    expect(result).toEqual({ success: true, repo: 'acme/web', fromCache: false });
    expect(mocks.gitClone).toHaveBeenCalledWith('https://github.com/acme/web.git');
    expect(changed).toHaveBeenCalled();
  });

  it('only restores missing files when the same repository is already cloned', async () => {
    mocks.origin = 'https://github.com/Acme/Web.git';
    const result = await adapter().restoreRepo('acme/web');
    expect(result).toEqual({ success: true, repo: 'acme/web', fromCache: true });
    expect(mocks.restoreGitWorkingTree).toHaveBeenCalled();
    expect(mocks.gitClone).not.toHaveBeenCalled();
  });

  it('falls back to the file import when the clone fails', async () => {
    mocks.gitClone.mockRejectedValue(new Error('HTTP Error: 401'));
    mocks.loadRepoImport.mockResolvedValue({ repo: 'acme/web', files: [{ path: 'a.txt', content: 'x' }] });
    const result = await adapter().restoreRepo('https://github.com/acme/web');
    expect(result).toMatchObject({ success: true, fromCache: true });
    expect(mocks.loadRepoImport).toHaveBeenCalled();
  });

  it('skips git entirely outside the hosted IDE', async () => {
    mocks.proxy = undefined;
    mocks.loadRepoImport.mockResolvedValue({ repo: 'acme/web', files: [{ path: 'a.txt', content: 'x' }] });
    await adapter().restoreRepo('https://github.com/acme/web');
    expect(mocks.gitClone).not.toHaveBeenCalled();
    expect(mocks.whenBrowserGitConfigured).not.toHaveBeenCalled();
  });
});
