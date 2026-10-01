import { afterEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  proxy: undefined as string | undefined,
  gitClone: vi.fn(),
  cloneRepo: vi.fn(),
}));

vi.mock('./gitCorsProxy', () => ({ gitCorsProxy: () => mocks.proxy }));
vi.mock('./browserGit', () => ({ gitClone: mocks.gitClone }));
vi.mock('./workspaceFs/workspaceGit', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./workspaceFs/workspaceGit')>()),
  cloneRepo: mocks.cloneRepo,
}));

import { cloneIntoWorkspace, GIT_REPO_CHANGED_EVENT } from './workspaceClone';

afterEach(() => {
  vi.clearAllMocks();
  mocks.proxy = undefined;
});

describe('cloneIntoWorkspace', () => {
  it('clones through browser git into the workspace root in the hosted IDE', async () => {
    mocks.proxy = 'http://localhost/git-proxy';
    mocks.gitClone.mockResolvedValue({ message: 'ok', url: 'x', branch: 'main', files: 3 });
    const announced = vi.fn();
    window.addEventListener(GIT_REPO_CHANGED_EVENT, announced);

    const result = await cloneIntoWorkspace('octocat/Hello-World', { token: 't' });

    window.removeEventListener(GIT_REPO_CHANGED_EVENT, announced);
    expect(mocks.gitClone).toHaveBeenCalledWith('octocat/Hello-World', { token: 't' });
    expect(mocks.cloneRepo).not.toHaveBeenCalled();
    expect(result).toEqual({ repo: 'octocat/Hello-World', dir: '', entries: 3, defaultBranch: 'main' });
    expect(announced).toHaveBeenCalledTimes(1);
  });

  it('uses the workspaceFs clone elsewhere', async () => {
    const cloned = { repo: 'o/n', dir: 'repos/o/n', entries: 1, defaultBranch: 'main' };
    mocks.cloneRepo.mockResolvedValue(cloned);
    await expect(cloneIntoWorkspace('o/n')).resolves.toBe(cloned);
    expect(mocks.gitClone).not.toHaveBeenCalled();
  });
});
