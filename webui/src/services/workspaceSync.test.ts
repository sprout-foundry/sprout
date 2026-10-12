import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({ getWorkspace: vi.fn(), setWorkspace: vi.fn() }));
const tab = vi.hoisted(() => ({ path: '', persist: vi.fn() }));

vi.mock('./api', () => ({
  ApiService: { getInstance: () => ({ getWorkspace: api.getWorkspace, setWorkspace: api.setWorkspace }) },
}));
vi.mock('./clientSession', () => ({
  getTabWorkspacePath: () => tab.path,
  persistTabWorkspacePath: (path: string) => {
    tab.persist(path);
    tab.path = path;
  },
}));

import { __resetWorkspaceStoreForTests, getWorkspaceRoot } from './workspaceStore';
import { consentToHomeWorkspace, installWorkspaceReconnectSync, reconcileTabWorkspace } from './workspaceSync';

function ws(root: string, extra: Record<string, unknown> = {}) {
  return {
    workspace_root: root,
    daemon_root: '/home/u',
    is_project: root !== '/home/u',
    project_markers: [],
    needs_workspace_selection: root === '/home/u',
    workspace_is_home: root === '/home/u',
    home_dir: '/home/u',
    suggested_projects: [],
    recent_workspaces: [],
    ...extra,
  };
}

describe('reconcileTabWorkspace', () => {
  const changed = vi.fn();

  beforeEach(() => {
    __resetWorkspaceStoreForTests();
    api.getWorkspace.mockReset();
    api.setWorkspace.mockReset();
    tab.persist.mockReset();
    tab.path = '';
    changed.mockReset();
    window.addEventListener('sprout:workspace-changed', changed);
  });

  afterEach(() => {
    window.removeEventListener('sprout:workspace-changed', changed);
    __resetWorkspaceStoreForTests();
  });

  it('re-selects the remembered workspace when the server is back at its default root', async () => {
    tab.path = '/home/u/proj';
    api.setWorkspace.mockResolvedValue({ ...ws('/home/u/proj'), message: 'ok' });
    api.getWorkspace.mockResolvedValue(ws('/home/u/proj'));

    await expect(reconcileTabWorkspace(ws('/home/u'))).resolves.toBe('restored');

    expect(api.setWorkspace).toHaveBeenCalledWith('/home/u/proj');
    expect(changed).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => expect(getWorkspaceRoot()).toBe('/home/u/proj'));
  });

  it('follows a deliberate server-side change instead of fighting it', async () => {
    tab.path = '/home/u/proj';

    await expect(reconcileTabWorkspace(ws('/home/u/other'))).resolves.toBe('adopted');

    expect(api.setWorkspace).not.toHaveBeenCalled();
    expect(tab.persist).toHaveBeenCalledWith('/home/u/other');
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('leaves an agreeing tab alone', async () => {
    tab.path = '/home/u/proj';
    await expect(reconcileTabWorkspace(ws('/home/u/proj'))).resolves.toBe('unchanged');
    expect(api.setWorkspace).not.toHaveBeenCalled();
    expect(changed).not.toHaveBeenCalled();
  });

  it('defers to the gate when nothing is remembered', async () => {
    await expect(reconcileTabWorkspace(ws('/home/u'))).resolves.toBe('unresolved');
    expect(api.setWorkspace).not.toHaveBeenCalled();
  });

  it('does not re-select where the host disallows switching', async () => {
    tab.path = '/home/u/proj';
    await expect(reconcileTabWorkspace(ws('/home/u'), false)).resolves.toBe('unresolved');
    expect(api.setWorkspace).not.toHaveBeenCalled();
  });
});

describe('installWorkspaceReconnectSync', () => {
  beforeEach(() => {
    __resetWorkspaceStoreForTests();
    api.getWorkspace.mockReset();
    api.setWorkspace.mockReset();
    tab.path = '/home/u/proj';
  });

  it('restores the tab workspace after a reconnect to a server that lost it', async () => {
    const callbacks = new Set<(e: { type: string; data?: unknown }) => void>();
    const source = {
      onEvent: (cb: (e: { type: string; data?: unknown }) => void) => callbacks.add(cb),
      removeEvent: (cb: (e: { type: string; data?: unknown }) => void) => callbacks.delete(cb),
    };
    api.getWorkspace.mockResolvedValue(ws('/home/u'));
    api.setWorkspace.mockResolvedValue({ ...ws('/home/u/proj'), message: 'ok' });

    const uninstall = installWorkspaceReconnectSync(source as never);
    expect(installWorkspaceReconnectSync(source as never)).toBe(uninstall);
    const emit = (data: unknown) => callbacks.forEach((cb) => cb({ type: 'connection_status', data }));

    emit({ connected: true });
    await Promise.resolve();
    expect(api.getWorkspace).not.toHaveBeenCalled();

    emit({ connected: false });
    emit({ connected: true });
    await vi.waitFor(() => expect(api.setWorkspace).toHaveBeenCalledWith('/home/u/proj'));

    uninstall();
    expect(callbacks.size).toBe(0);
  });
});

describe('consentToHomeWorkspace', () => {
  beforeEach(() => {
    __resetWorkspaceStoreForTests();
    api.getWorkspace.mockReset();
  });

  it('consents with the freshly read home root', async () => {
    api.getWorkspace.mockResolvedValue(ws('/home/u'));
    const setWorkspace = vi.fn().mockResolvedValue(undefined);
    await expect(consentToHomeWorkspace(setWorkspace)).resolves.toBe(true);
    expect(setWorkspace).toHaveBeenCalledWith('/home/u', true);
  });

  it('skips consent when a project was restored since the gate rendered', async () => {
    api.getWorkspace.mockResolvedValue(ws('/home/u/proj'));
    const setWorkspace = vi.fn();
    await expect(consentToHomeWorkspace(setWorkspace)).resolves.toBe(false);
    expect(setWorkspace).not.toHaveBeenCalled();
  });
});
