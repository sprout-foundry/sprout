import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({ getProjectFolder: vi.fn(), setProjectFolder: vi.fn() }));
const tab = vi.hoisted(() => ({ path: '', persist: vi.fn() }));

vi.mock('./api', () => ({
  ApiService: {
    getInstance: () => ({ getProjectFolder: api.getProjectFolder, setProjectFolder: api.setProjectFolder }),
  },
}));
vi.mock('./clientSession', () => ({
  getTabProjectFolderPath: () => tab.path,
  persistTabProjectFolderPath: (path: string) => {
    tab.persist(path);
    tab.path = path;
  },
}));

import { __resetProjectFolderStoreForTests, getProjectFolder } from './workspaceStore';
import {
  consentToHomeProjectFolder,
  installProjectFolderReconnectSync,
  reconcileTabProjectFolder,
} from './workspaceSync';

function ws(root: string, extra: Record<string, unknown> = {}) {
  return {
    project_folder: root,
    daemon_root: '/home/u',
    is_project: root !== '/home/u',
    project_markers: [],
    needs_project_folder_selection: root === '/home/u',
    project_folder_is_home: root === '/home/u',
    home_dir: '/home/u',
    suggested_projects: [],
    recent_project_folders: [],
    ...extra,
  };
}

describe('reconcileTabWorkspace', () => {
  const changed = vi.fn();

  beforeEach(() => {
    __resetProjectFolderStoreForTests();
    api.getProjectFolder.mockReset();
    api.setProjectFolder.mockReset();
    tab.persist.mockReset();
    tab.path = '';
    changed.mockReset();
    window.addEventListener('sprout:workspace-changed', changed);
  });

  afterEach(() => {
    window.removeEventListener('sprout:workspace-changed', changed);
    __resetProjectFolderStoreForTests();
  });

  it('re-selects the remembered workspace when the server is back at its default root', async () => {
    tab.path = '/home/u/proj';
    api.setProjectFolder.mockResolvedValue({ ...ws('/home/u/proj'), message: 'ok' });
    api.getProjectFolder.mockResolvedValue(ws('/home/u/proj'));

    await expect(reconcileTabProjectFolder(ws('/home/u'))).resolves.toBe('restored');

    expect(api.setProjectFolder).toHaveBeenCalledWith('/home/u/proj');
    expect(changed).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => expect(getProjectFolder()).toBe('/home/u/proj'));
  });

  it('follows a deliberate server-side change instead of fighting it', async () => {
    tab.path = '/home/u/proj';

    await expect(reconcileTabProjectFolder(ws('/home/u/other'))).resolves.toBe('adopted');

    expect(api.setProjectFolder).not.toHaveBeenCalled();
    expect(tab.persist).toHaveBeenCalledWith('/home/u/other');
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('leaves an agreeing tab alone', async () => {
    tab.path = '/home/u/proj';
    await expect(reconcileTabProjectFolder(ws('/home/u/proj'))).resolves.toBe('unchanged');
    expect(api.setProjectFolder).not.toHaveBeenCalled();
    expect(changed).not.toHaveBeenCalled();
  });

  it('defers to the gate when nothing is remembered', async () => {
    await expect(reconcileTabProjectFolder(ws('/home/u'))).resolves.toBe('unresolved');
    expect(api.setProjectFolder).not.toHaveBeenCalled();
  });

  it('does not re-select where the host disallows switching', async () => {
    tab.path = '/home/u/proj';
    await expect(reconcileTabProjectFolder(ws('/home/u'), false)).resolves.toBe('unresolved');
    expect(api.setProjectFolder).not.toHaveBeenCalled();
  });
});

describe('installWorkspaceReconnectSync', () => {
  beforeEach(() => {
    __resetProjectFolderStoreForTests();
    api.getProjectFolder.mockReset();
    api.setProjectFolder.mockReset();
    tab.path = '/home/u/proj';
  });

  it('restores the tab workspace after a reconnect to a server that lost it', async () => {
    const callbacks = new Set<(e: { type: string; data?: unknown }) => void>();
    const source = {
      onEvent: (cb: (e: { type: string; data?: unknown }) => void) => callbacks.add(cb),
      removeEvent: (cb: (e: { type: string; data?: unknown }) => void) => callbacks.delete(cb),
    };
    api.getProjectFolder.mockResolvedValue(ws('/home/u'));
    api.setProjectFolder.mockResolvedValue({ ...ws('/home/u/proj'), message: 'ok' });

    const uninstall = installProjectFolderReconnectSync(source as never);
    expect(installProjectFolderReconnectSync(source as never)).toBe(uninstall);
    const emit = (data: unknown) => callbacks.forEach((cb) => cb({ type: 'connection_status', data }));

    emit({ connected: true });
    await Promise.resolve();
    expect(api.getProjectFolder).not.toHaveBeenCalled();

    emit({ connected: false });
    emit({ connected: true });
    await vi.waitFor(() => expect(api.setProjectFolder).toHaveBeenCalledWith('/home/u/proj'));

    uninstall();
    expect(callbacks.size).toBe(0);
  });
});

describe('consentToHomeWorkspace', () => {
  beforeEach(() => {
    __resetProjectFolderStoreForTests();
    api.getProjectFolder.mockReset();
  });

  it('consents with the freshly read home root', async () => {
    api.getProjectFolder.mockResolvedValue(ws('/home/u'));
    const setProjectFolder = vi.fn().mockResolvedValue(undefined);
    await expect(consentToHomeProjectFolder(setProjectFolder)).resolves.toBe(true);
    expect(setProjectFolder).toHaveBeenCalledWith('/home/u', true);
  });

  it('skips consent when a project was restored since the gate rendered', async () => {
    api.getProjectFolder.mockResolvedValue(ws('/home/u/proj'));
    const setProjectFolder = vi.fn();
    await expect(consentToHomeProjectFolder(setProjectFolder)).resolves.toBe(false);
    expect(setProjectFolder).not.toHaveBeenCalled();
  });
});
