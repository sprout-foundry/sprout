import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({ getProjectFolder: vi.fn() }));

vi.mock('./api', () => ({
  ApiService: { getInstance: () => ({ getProjectFolder: api.getProjectFolder }) },
}));

import {
  __resetProjectFolderStoreForTests,
  applyProjectFolder,
  ensureProjectFolder,
  getProjectFolder,
  getProjectFolderState,
  refreshProjectFolder,
  subscribeProjectFolder,
} from './projectFolderStore';

function ws(root: string, daemon = '/home/u') {
  return {
    project_folder: root,
    daemon_root: daemon,
    is_project: true,
    project_markers: [],
    needs_project_folder_selection: false,
    project_folder_is_home: false,
    home_dir: '/home/u',
    suggested_projects: [],
    recent_project_folders: [],
  };
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('projectFolderStore', () => {
  beforeEach(() => {
    __resetProjectFolderStoreForTests();
    api.getProjectFolder.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
    __resetProjectFolderStoreForTests();
  });

  it('shares one in-flight request between concurrent callers', async () => {
    const pending = deferred<ReturnType<typeof ws>>();
    api.getProjectFolder.mockReturnValue(pending.promise);

    const a = refreshProjectFolder();
    const b = refreshProjectFolder();
    const c = ensureProjectFolder();
    pending.resolve(ws('/home/u/proj'));

    await expect(Promise.all([a, b, c])).resolves.toHaveLength(3);
    expect(api.getProjectFolder).toHaveBeenCalledTimes(1);
    expect(getProjectFolder()).toBe('/home/u/proj');
  });

  it('drops a stale response that resolves after a newer request', async () => {
    const older = deferred<ReturnType<typeof ws>>();
    const newer = deferred<ReturnType<typeof ws>>();
    api.getProjectFolder.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);

    const first = refreshProjectFolder();
    const second = refreshProjectFolder({ force: true });
    newer.resolve(ws('/home/u/new'));
    await second;
    older.resolve(ws('/home/u'));

    await expect(first).resolves.toMatchObject({ project_folder: '/home/u/new' });
    expect(getProjectFolder()).toBe('/home/u/new');
  });

  it('discards a GET that started before an applied root', async () => {
    const before = deferred<ReturnType<typeof ws>>();
    api.getProjectFolder.mockReturnValueOnce(before.promise).mockResolvedValue(ws('/home/u/proj'));

    void refreshProjectFolder();
    await applyProjectFolder('/home/u/proj', '/home/u');
    before.resolve(ws('/home/u'));
    await Promise.resolve();

    expect(getProjectFolder()).toBe('/home/u/proj');
  });

  it('keeps the last known workspace on failure and retries', async () => {
    vi.useFakeTimers();
    api.getProjectFolder.mockResolvedValueOnce(ws('/home/u/proj'));
    await refreshProjectFolder();

    api.getProjectFolder.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(ws('/home/u/proj2'));
    await expect(refreshProjectFolder({ force: true })).rejects.toThrow('offline');
    expect(getProjectFolder()).toBe('/home/u/proj');
    expect(getProjectFolderState().error).toBe('offline');

    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.getProjectFolder).toHaveBeenCalledTimes(3);
    expect(getProjectFolder()).toBe('/home/u/proj2');
    expect(getProjectFolderState().error).toBeNull();
  });

  it('follows sprout:workspace-changed and notifies subscribers', async () => {
    api.getProjectFolder.mockResolvedValueOnce(ws('/home/u/a')).mockResolvedValue(ws('/home/u/b'));
    await refreshProjectFolder();
    const listener = vi.fn();
    const unsubscribe = subscribeProjectFolder(listener);

    window.dispatchEvent(
      new CustomEvent('sprout:workspace-changed', { detail: { projectFolder: '/home/u/b', daemonRoot: '/home/u' } }),
    );
    expect(getProjectFolder()).toBe('/home/u/b');
    await vi.waitFor(() => expect(api.getProjectFolder).toHaveBeenCalledTimes(2));
    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });

  it('serves a fresh cached workspace without refetching', async () => {
    api.getProjectFolder.mockResolvedValue(ws('/home/u/proj'));
    await refreshProjectFolder();
    await ensureProjectFolder();
    expect(api.getProjectFolder).toHaveBeenCalledTimes(1);
  });
});
