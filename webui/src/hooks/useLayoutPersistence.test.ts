// @ts-nocheck
// Unit tests for the workspace-changed handler in useLayoutPersistence:
// modified buffers must survive a workspace/worktree switch — the handler
// used to delete every closable buffer including ones with unsaved edits.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useLayoutPersistence } from './useLayoutPersistence';
import {
  getLayoutStorageKey,
  getPanesStorageKey,
  getPaneLayoutStorageKey,
  getPaneSizesStorageKey,
} from '../services/layoutPersistence';

function mkBuffer(overrides = {}) {
  return {
    id: 'b1',
    kind: 'file',
    file: { name: 'a.ts', path: '/w/a.ts', isDir: false, size: 10, modified: 0 },
    content: 'x',
    originalContent: 'x',
    contentLoaded: true,
    cursorPosition: { line: 0, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    isModified: false,
    isActive: false,
    isClosable: true,
    paneId: 'pane-1',
    ...overrides,
  };
}

describe('useLayoutPersistence — workspace-changed buffer survival', () => {
  const noop = vi.fn();

  function mount(buffers) {
    const buffersRef = { current: buffers };
    const panesRef = { current: [{ id: 'pane-1', bufferId: null }] };
    let latest = buffers;
    const setBuffers = vi.fn((updater) => {
      latest = updater(buffersRef.current);
      buffersRef.current = latest;
    });
    const setPanes = vi.fn();

    renderHook(() =>
      useLayoutPersistence({
        buffersRef,
        panesRef,
        buffers,
        panes: panesRef.current,
        setBuffers,
        setPanes,
        activePaneId: 'pane-1',
        activeBufferId: null,
        setActivePaneId: noop,
        setActiveBufferId: noop,
        paneLayout: 'single',
        paneSizes: {},
      }),
    );
    return { setBuffers, getLatest: () => latest };
  }

  function fireWorkspaceChanged() {
    workspaceHandler?.(
      new CustomEvent('sprout:workspace-changed', {
        detail: { workspaceRoot: '/other' },
      }),
    );
  }

  let workspaceHandler: EventListener | null = null;

  beforeEach(() => {
    workspaceHandler = null;
    vi.spyOn(window, 'addEventListener').mockImplementation((type, fn) => {
      if (type === 'sprout:workspace-changed') workspaceHandler = fn as EventListener;
    });
    vi.spyOn(window, 'removeEventListener').mockImplementation(() => {});
    vi.spyOn(Date, 'now').mockReturnValue(1_000_000);
    // Debounced writes — keep timers real but no-op storage.
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('keeps MODIFIED file buffers when the workspace changes', () => {
    const modified = mkBuffer({ id: 'b-mod', isModified: true });
    const clean = mkBuffer({ id: 'b-clean', isModified: false });
    const { setBuffers } = mount(
      new Map([
        ['b-mod', modified],
        ['b-clean', clean],
      ]),
    );

    fireWorkspaceChanged();

    expect(setBuffers).toHaveBeenCalled();
    const updater = setBuffers.mock.calls.at(-1)[0];
    const result = updater(
      new Map([
        ['b-mod', modified],
        ['b-clean', clean],
      ]),
    );
    expect(result.has('b-mod')).toBe(true);
    expect(result.has('b-clean')).toBe(false);
  });

  it('still deletes clean closable file buffers on workspace change', () => {
    const clean = mkBuffer({ id: 'b-clean', isModified: false });
    const { setBuffers } = mount(new Map([['b-clean', clean]]));

    fireWorkspaceChanged();

    const updater = setBuffers.mock.calls.at(-1)[0];
    const result = updater(new Map([['b-clean', clean]]));
    expect(result.has('b-clean')).toBe(false);
  });

  it('keeps modified buffers even when not pinned', () => {
    const unsaved = mkBuffer({ id: 'b-unsaved', isModified: true, isPinned: false, isClosable: true });
    const { setBuffers } = mount(new Map([['b-unsaved', unsaved]]));

    fireWorkspaceChanged();

    const updater = setBuffers.mock.calls.at(-1)[0];
    const result = updater(new Map([['b-unsaved', unsaved]]));
    expect(result.has('b-unsaved')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Restoring a layout whose surviving pane is not pane-1
// ---------------------------------------------------------------------------
//
// Regression (browser build): closing the primary pane and keeping a
// split-created pane persisted a pane list without pane-1 while paneSizes and
// paneLayout were written by their own effects. Restore skipped the pane block
// (loadSavedPanes required pane-1 and >= 2 panes), so the app booted with a
// default pane-1 while paneSizes on disk still described the survivor — the
// geometry was unusable until the window was restarted.
describe('useLayoutPersistence — restore with a non-primary surviving pane', () => {
  // Exactly the localStorage state observed from the broken session.
  const SURVIVOR = 'pane-1792502003579';

  beforeEach(() => {
    window.localStorage.clear();
    window.localStorage.setItem(getPanesStorageKey(), JSON.stringify([{ id: SURVIVOR, position: 'secondary' }]));
    window.localStorage.setItem(getPaneLayoutStorageKey(), 'single');
    window.localStorage.setItem(getPaneSizesStorageKey(), JSON.stringify({ [SURVIVOR]: 100 }));
  });

  function mountForRestore() {
    const buffersRef = { current: new Map() };
    const panesRef = { current: [{ id: 'pane-1', bufferId: null, position: 'primary', isActive: true }] };
    const setPanes = vi.fn();
    const setPaneSizes = vi.fn();
    const setPaneLayout = vi.fn();
    const setActivePaneId = vi.fn();

    const { result } = renderHook(() =>
      useLayoutPersistence({
        buffersRef,
        panesRef,
        buffers: new Map(),
        panes: panesRef.current,
        setBuffers: vi.fn(),
        setPanes,
        activePaneId: 'pane-1',
        activeBufferId: null,
        setActivePaneId,
        setActiveBufferId: vi.fn(),
        paneLayout: 'single',
        paneSizes: {},
        setPaneLayout,
        setPaneSizes,
      }),
    );
    return { result, setPanes, setPaneSizes, setPaneLayout, setActivePaneId, panesRef };
  }

  it('restores the surviving pane instead of falling back to pane-1', () => {
    const { result, setPanes } = mountForRestore();
    result.current.restoreLayout();

    const restored = setPanes.mock.calls.at(-1)[0];
    expect(restored).toHaveLength(1);
    expect(restored[0].id).toBe(SURVIVOR);
  });

  it('never makes a non-existent pane the active pane', () => {
    const { result, setActivePaneId } = mountForRestore();
    result.current.restoreLayout();

    const active = setActivePaneId.mock.calls.at(-1)[0];
    expect(active).toBe(SURVIVOR);
  });

  it('forces single layout for one restored pane', () => {
    const { result, setPaneLayout } = mountForRestore();
    result.current.restoreLayout();

    expect(setPaneLayout.mock.calls.at(-1)[0]).toBe('single');
  });

  it('reconciles sizes to the surviving pane only', () => {
    const { result, setPaneSizes } = mountForRestore();
    result.current.restoreLayout();

    const sizes = setPaneSizes.mock.calls.at(-1)[0];
    expect(sizes).toEqual({ [SURVIVOR]: 100 });
  });
});

// A restored file buffer whose saved pane no longer exists must land in a
// pane that does exist — otherwise it belongs to no pane's tab strip while
// staying in state, and is re-saved on every boot.
describe('useLayoutPersistence — orphaned snapshot buffer pane', () => {
  const SURVIVOR = 'pane-1792502003579';

  beforeEach(() => {
    window.localStorage.clear();
    window.localStorage.setItem(getPanesStorageKey(), JSON.stringify([{ id: SURVIVOR, position: 'secondary' }]));
    window.localStorage.setItem(getPaneLayoutStorageKey(), 'single');
    window.localStorage.setItem(getPaneSizesStorageKey(), JSON.stringify({ [SURVIVOR]: 100 }));
    // A file was open in pane-1 before the user closed it.
    window.localStorage.setItem(
      'sprout.editor.layoutState:_default',
      JSON.stringify({
        version: 1,
        activePaneId: 'pane-1',
        activeBufferFilePath: '/w/a.ts',
        buffers: [
          {
            filePath: '/w/a.ts',
            paneId: 'pane-1',
            isActive: true,
            cursorPosition: { line: 0, column: 0 },
            scrollPosition: { top: 0, left: 0 },
          },
        ],
        bufferOrder: ['/w/a.ts'],
      }),
    );
  });

  it('reassigns an orphaned buffer to a pane that exists', () => {
    const buffersRef = { current: new Map() };
    const panesRef = { current: [{ id: 'pane-1', bufferId: null, position: 'primary', isActive: true }] };
    let latestBuffers = buffersRef.current;
    const setBuffers = vi.fn((updater) => {
      latestBuffers = typeof updater === 'function' ? updater(buffersRef.current) : updater;
      buffersRef.current = latestBuffers;
    });

    const { result } = renderHook(() =>
      useLayoutPersistence({
        buffersRef,
        panesRef,
        buffers: buffersRef.current,
        panes: panesRef.current,
        setBuffers,
        setPanes: vi.fn(),
        activePaneId: 'pane-1',
        activeBufferId: null,
        setActivePaneId: vi.fn(),
        setActiveBufferId: vi.fn(),
        paneLayout: 'single',
        paneSizes: {},
        setPaneLayout: vi.fn(),
        setPaneSizes: vi.fn(),
      }),
    );

    result.current.restoreLayout();

    const fileBuffers = Array.from(latestBuffers.values()).filter((b) => b.kind === 'file');
    expect(fileBuffers.length).toBeGreaterThan(0);
    for (const b of fileBuffers) {
      expect(b.paneId).toBe(SURVIVOR);
    }
  });
});
