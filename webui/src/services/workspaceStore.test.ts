import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({ getWorkspace: vi.fn() }));

vi.mock('./api', () => ({
  ApiService: { getInstance: () => ({ getWorkspace: api.getWorkspace }) },
}));

import {
  __resetWorkspaceStoreForTests,
  applyWorkspaceRoot,
  ensureWorkspace,
  getWorkspaceRoot,
  getWorkspaceState,
  refreshWorkspace,
  subscribeWorkspace,
} from './workspaceStore';

function ws(root: string, daemon = '/home/u') {
  return {
    workspace_root: root,
    daemon_root: daemon,
    is_project: true,
    project_markers: [],
    needs_workspace_selection: false,
    workspace_is_home: false,
    home_dir: '/home/u',
    suggested_projects: [],
    recent_workspaces: [],
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

describe('workspaceStore', () => {
  beforeEach(() => {
    __resetWorkspaceStoreForTests();
    api.getWorkspace.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
    __resetWorkspaceStoreForTests();
  });

  it('shares one in-flight request between concurrent callers', async () => {
    const pending = deferred<ReturnType<typeof ws>>();
    api.getWorkspace.mockReturnValue(pending.promise);

    const a = refreshWorkspace();
    const b = refreshWorkspace();
    const c = ensureWorkspace();
    pending.resolve(ws('/home/u/proj'));

    await expect(Promise.all([a, b, c])).resolves.toHaveLength(3);
    expect(api.getWorkspace).toHaveBeenCalledTimes(1);
    expect(getWorkspaceRoot()).toBe('/home/u/proj');
  });

  it('drops a stale response that resolves after a newer request', async () => {
    const older = deferred<ReturnType<typeof ws>>();
    const newer = deferred<ReturnType<typeof ws>>();
    api.getWorkspace.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);

    const first = refreshWorkspace();
    const second = refreshWorkspace({ force: true });
    newer.resolve(ws('/home/u/new'));
    await second;
    older.resolve(ws('/home/u'));

    await expect(first).resolves.toMatchObject({ workspace_root: '/home/u/new' });
    expect(getWorkspaceRoot()).toBe('/home/u/new');
  });

  it('discards a GET that started before an applied root', async () => {
    const before = deferred<ReturnType<typeof ws>>();
    api.getWorkspace.mockReturnValueOnce(before.promise).mockResolvedValue(ws('/home/u/proj'));

    void refreshWorkspace();
    await applyWorkspaceRoot('/home/u/proj', '/home/u');
    before.resolve(ws('/home/u'));
    await Promise.resolve();

    expect(getWorkspaceRoot()).toBe('/home/u/proj');
  });

  it('keeps the last known workspace on failure and retries', async () => {
    vi.useFakeTimers();
    api.getWorkspace.mockResolvedValueOnce(ws('/home/u/proj'));
    await refreshWorkspace();

    api.getWorkspace.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(ws('/home/u/proj2'));
    await expect(refreshWorkspace({ force: true })).rejects.toThrow('offline');
    expect(getWorkspaceRoot()).toBe('/home/u/proj');
    expect(getWorkspaceState().error).toBe('offline');

    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.getWorkspace).toHaveBeenCalledTimes(3);
    expect(getWorkspaceRoot()).toBe('/home/u/proj2');
    expect(getWorkspaceState().error).toBeNull();
  });

  it('follows sprout:workspace-changed and notifies subscribers', async () => {
    api.getWorkspace.mockResolvedValueOnce(ws('/home/u/a')).mockResolvedValue(ws('/home/u/b'));
    await refreshWorkspace();
    const listener = vi.fn();
    const unsubscribe = subscribeWorkspace(listener);

    window.dispatchEvent(
      new CustomEvent('sprout:workspace-changed', { detail: { workspaceRoot: '/home/u/b', daemonRoot: '/home/u' } }),
    );
    expect(getWorkspaceRoot()).toBe('/home/u/b');
    await vi.waitFor(() => expect(api.getWorkspace).toHaveBeenCalledTimes(2));
    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });

  it('serves a fresh cached workspace without refetching', async () => {
    api.getWorkspace.mockResolvedValue(ws('/home/u/proj'));
    await refreshWorkspace();
    await ensureWorkspace();
    expect(api.getWorkspace).toHaveBeenCalledTimes(1);
  });
});
