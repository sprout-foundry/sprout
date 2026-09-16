// @ts-nocheck
// Unit tests for the workspace-changed handler in useLayoutPersistence:
// modified buffers must survive a workspace/worktree switch — the handler
// used to delete every closable buffer including ones with unsaved edits.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useLayoutPersistence } from './useLayoutPersistence';

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
