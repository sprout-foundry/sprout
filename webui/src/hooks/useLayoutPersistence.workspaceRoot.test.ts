// @ts-nocheck
// The tab's remembered workspace path keys the saved layout. The mount-time
// workspace fetch can return the server's interim default root (the home
// directory) while startup restores the tab's project, so it must not
// overwrite the remembered path; and the server returning to the root the
// tab already shows must not clear its buffers or layout.
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const session = vi.hoisted(() => ({ path: '/home/u/proj', persist: vi.fn() }));
const api = vi.hoisted(() => ({ getWorkspace: vi.fn() }));

vi.mock('../services/api', () => ({
  ApiService: { getInstance: () => ({ getWorkspace: api.getWorkspace }) },
}));
vi.mock('../services/clientSession', async (importOriginal) => ({
  ...(await importOriginal()),
  getTabWorkspacePath: () => session.path,
  persistTabWorkspacePath: (path: string) => {
    session.persist(path);
    session.path = path;
  },
}));

import { useLayoutPersistence } from './useLayoutPersistence';

function mkBuffer() {
  return {
    id: 'b1',
    kind: 'file',
    file: { name: 'a.ts', path: 'a.ts', isDir: false, size: 1, modified: 0 },
    content: 'x',
    originalContent: 'x',
    contentLoaded: true,
    cursorPosition: { line: 0, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    isModified: false,
    isActive: false,
    isClosable: true,
    paneId: 'pane-1',
  };
}

function mount() {
  const buffers = new Map([['b1', mkBuffer()]]);
  const buffersRef = { current: buffers };
  const panesRef = { current: [{ id: 'pane-1', bufferId: null }] };
  const setBuffers = vi.fn((updater) => {
    buffersRef.current = typeof updater === 'function' ? updater(buffersRef.current) : updater;
  });
  renderHook(() =>
    useLayoutPersistence({
      buffersRef,
      panesRef,
      buffers,
      panes: panesRef.current,
      setBuffers,
      setPanes: vi.fn(),
      activePaneId: 'pane-1',
      activeBufferId: null,
      setActivePaneId: vi.fn(),
      setActiveBufferId: vi.fn(),
      paneLayout: 'single',
      paneSizes: {},
    }),
  );
  return { buffersRef };
}

function workspaceChanged(root: string) {
  act(() => {
    window.dispatchEvent(new CustomEvent('sprout:workspace-changed', { detail: { workspaceRoot: root } }));
  });
}

describe('useLayoutPersistence — remembered workspace path', () => {
  beforeEach(() => {
    session.path = '/home/u/proj';
    session.persist.mockReset();
    api.getWorkspace.mockReset();
    api.getWorkspace.mockResolvedValue({ workspace_root: '/home/u', daemon_root: '/home/u' });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not persist the mount-time workspace root', async () => {
    mount();
    await vi.waitFor(() => expect(api.getWorkspace).toHaveBeenCalled());
    await act(async () => {});
    expect(session.persist).not.toHaveBeenCalled();
    expect(session.path).toBe('/home/u/proj');
  });

  it('keeps buffers when the server returns to the root the tab already shows', () => {
    const { buffersRef } = mount();
    workspaceChanged('/home/u/proj');
    expect(buffersRef.current.has('b1')).toBe(true);
    expect(session.persist).not.toHaveBeenCalled();
  });

  it('persists a different root and clears its stale buffers', () => {
    const { buffersRef } = mount();
    workspaceChanged('/home/u/other');
    expect(session.persist).toHaveBeenCalledWith('/home/u/other');
    expect(buffersRef.current.has('b1')).toBe(false);
  });
});
