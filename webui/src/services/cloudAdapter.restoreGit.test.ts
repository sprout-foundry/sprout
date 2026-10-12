import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  proxy: 'http://localhost/git-proxy' as string | undefined,
  origin: null as string | null,
  snapshotOf: null as string | null,
  gitClone: vi.fn(),
  gitInitSnapshot: vi.fn(),
  restoreGitWorkingTree: vi.fn(),
  loadRepoImport: vi.fn(),
  held: [] as Array<Promise<unknown>>,
}));

vi.mock('./gitCorsProxy', () => ({ gitCorsProxy: () => mocks.proxy }));
vi.mock('./browserGit', () => ({
  gitOriginUrl: () => Promise.resolve(mocks.origin),
  snapshotSource: () => Promise.resolve(mocks.snapshotOf),
  gitClone: mocks.gitClone,
  gitInitSnapshot: mocks.gitInitSnapshot,
  restoreGitWorkingTree: mocks.restoreGitWorkingTree,
  holdBrowserGitWorkTree: <T>(work: Promise<T>) => {
    mocks.held.push(work);
    return work;
  },
}));
vi.mock('./wasmShell', () => ({
  initWasmShell: vi.fn(() =>
    Promise.resolve({ writeFile: vi.fn(() => ''), readFile: vi.fn(() => ({ error: 'missing' })), listDir: vi.fn() }),
  ),
  resetWasmShell: vi.fn(),
}));
vi.mock('./repoImportCache', () => ({
  loadRepoImport: mocks.loadRepoImport,
  saveRepoImport: vi.fn(),
  setLastRepo: vi.fn(),
  getLastRepo: vi.fn(() => Promise.resolve(null)),
}));

import { CloudAdapter } from './cloudAdapter';
import { REPO_IMPORT_PROGRESS_EVENT } from './repoGitImport';
import { GIT_REPO_CHANGED_EVENT } from './workspaceClone';

const adapter = () => new CloudAdapter({ apiBase: 'http://localhost', wsUrl: 'ws://localhost/ws', navItems: [] });
const CACHED = { repo: 'acme/web', files: [{ path: 'a.txt', content: 'x' }] };

describe('CloudAdapter.restoreRepo with browser git', () => {
  beforeEach(() => {
    vi.stubGlobal('indexedDB', {});
    mocks.proxy = 'http://localhost/git-proxy';
    mocks.origin = null;
    mocks.snapshotOf = null;
    mocks.held = [];
    mocks.gitClone.mockResolvedValue({ message: 'ok' });
    mocks.gitInitSnapshot.mockResolvedValue('snapshot-oid');
    mocks.restoreGitWorkingTree.mockResolvedValue(0);
    mocks.loadRepoImport.mockResolvedValue(null);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('clones a repository that is not yet checked out, holding git operations meanwhile', async () => {
    const changed = vi.fn();
    window.addEventListener(GIT_REPO_CHANGED_EVENT, changed);
    const result = await adapter().restoreRepo('https://github.com/acme/web');
    window.removeEventListener(GIT_REPO_CHANGED_EVENT, changed);

    expect(result).toEqual({ success: true, repo: 'acme/web', fromCache: false, history: 'clone' });
    expect(mocks.gitClone).toHaveBeenCalledWith(
      'https://github.com/acme/web.git',
      expect.objectContaining({ maxBytes: expect.any(Number), onProgress: expect.any(Function) }),
    );
    expect(mocks.held).toHaveLength(1);
    expect(mocks.gitInitSnapshot).not.toHaveBeenCalled();
    expect(changed).toHaveBeenCalled();
  });

  it('publishes clone progress as window events', async () => {
    mocks.gitClone.mockImplementation(async (_url: string, opts: { onProgress: (p: unknown) => void }) => {
      opts.onProgress({ phase: 'Receiving objects', loaded: 1, total: 4 });
      opts.onProgress({ phase: 'Receiving objects', loaded: 4, total: 4 });
      return { message: 'ok' };
    });
    const seen: unknown[] = [];
    const onProgress = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(REPO_IMPORT_PROGRESS_EVENT, onProgress);
    await adapter().restoreRepo('acme/web');
    window.removeEventListener(REPO_IMPORT_PROGRESS_EVENT, onProgress);
    expect(seen).toEqual([
      { repo: 'acme/web', phase: 'Receiving objects', loaded: 1, total: 4 },
      { repo: 'acme/web', phase: 'Receiving objects', loaded: 4, total: 4 },
    ]);
  });

  it('only restores missing files when the same repository is already cloned', async () => {
    mocks.origin = 'https://github.com/Acme/Web.git';
    const result = await adapter().restoreRepo('acme/web');
    expect(result).toEqual({ success: true, repo: 'acme/web', fromCache: true, history: 'clone' });
    expect(mocks.restoreGitWorkingTree).toHaveBeenCalled();
    expect(mocks.gitClone).not.toHaveBeenCalled();
  });

  it('imports the files as a snapshot when the clone fails', async () => {
    mocks.gitClone.mockRejectedValue(new Error('HTTP Error: 401'));
    mocks.loadRepoImport.mockResolvedValue(CACHED);
    const result = await adapter().restoreRepo('https://github.com/acme/web');
    expect(result).toMatchObject({ success: true, fromCache: true, history: 'snapshot' });
    expect(mocks.loadRepoImport).toHaveBeenCalled();
    expect(mocks.gitInitSnapshot).toHaveBeenCalledWith('https://github.com/acme/web', 'acme/web');
  });

  it('imports the files as a snapshot when the host has no git proxy', async () => {
    mocks.proxy = undefined;
    mocks.loadRepoImport.mockResolvedValue(CACHED);
    const result = await adapter().restoreRepo('https://github.com/acme/web');
    expect(mocks.gitClone).not.toHaveBeenCalled();
    expect(mocks.gitInitSnapshot).toHaveBeenCalled();
    expect(result).toMatchObject({ success: true, history: 'snapshot' });
  });

  it('keeps an existing snapshot of the same repository (and its local commits) on reload', async () => {
    mocks.snapshotOf = 'https://github.com/acme/web';
    const result = await adapter().restoreRepo('acme/web');
    expect(result).toEqual({ success: true, repo: 'acme/web', fromCache: true, history: 'snapshot' });
    expect(mocks.restoreGitWorkingTree).toHaveBeenCalled();
    expect(mocks.gitClone).not.toHaveBeenCalled();
    expect(mocks.gitInitSnapshot).not.toHaveBeenCalled();
    expect(mocks.loadRepoImport).not.toHaveBeenCalled();
  });

  it('still reports the files import when recording the snapshot fails', async () => {
    mocks.proxy = undefined;
    mocks.loadRepoImport.mockResolvedValue(CACHED);
    mocks.gitInitSnapshot.mockRejectedValue(new Error('quota exceeded'));
    const result = await adapter().restoreRepo('acme/web');
    expect(result).toMatchObject({ success: true, history: 'none' });
  });

  it('reports a failed files import without recording a snapshot', async () => {
    mocks.proxy = undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ error: 'not found' }), { status: 404 })),
    );
    const result = await adapter().restoreRepo('acme/missing');
    expect(result).toMatchObject({ success: false, error: 'not found', history: 'none' });
    expect(mocks.gitInitSnapshot).not.toHaveBeenCalled();
  });
});

describe('CloudAdapter.restoreRepo without IndexedDB', () => {
  it('imports the files without git', async () => {
    mocks.loadRepoImport.mockResolvedValue(CACHED);
    const result = await adapter().restoreRepo('acme/web');
    expect(result).toMatchObject({ success: true, history: 'none' });
    expect(mocks.gitClone).not.toHaveBeenCalled();
    expect(mocks.gitInitSnapshot).not.toHaveBeenCalled();
  });
});
