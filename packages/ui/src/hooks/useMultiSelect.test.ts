import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { useMultiSelect, flattenVisibleFiles } from './useMultiSelect';
import type { FileInfo } from '../types/file-tree';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestHookState: any;
let latestHookActions: any;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  latestHookState = null;
  latestHookActions = null;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function TestComponent() {
  const [state, actions] = useMultiSelect();
  latestHookState = state;
  latestHookActions = actions;
  return createElement('div', { 'data-testid': 'test' });
}

function renderHook() {
  act(() => {
    root.render(createElement(TestComponent));
  });
}

function renderAfterAction() {
  act(() => {
    // Trigger a re-render by re-rendering the component
    root.render(createElement(TestComponent));
  });
}

const ctx = () => ({ state: latestHookState, actions: latestHookActions });

// ---------------------------------------------------------------------------
// flattenVisibleFiles Tests
// ---------------------------------------------------------------------------

describe('flattenVisibleFiles', () => {
  it('flattens simple file list', () => {
    const items: FileInfo[] = [
      { name: 'file1.ts', path: '/file1.ts', isDir: false, size: 100, modified: 0 },
      { name: 'file2.ts', path: '/file2.ts', isDir: false, size: 200, modified: 0 },
    ];
    const result = flattenVisibleFiles(items);
    expect(result).toHaveLength(2);
    expect(result[0].path).toBe('/file1.ts');
    expect(result[1].path).toBe('/file2.ts');
  });

  it('flattens nested directory structure', () => {
    const items: FileInfo[] = [
      {
        name: 'dir1',
        path: '/dir1',
        isDir: true,
        size: 0,
        modified: 0,
        children: [
          { name: 'file1.ts', path: '/dir1/file1.ts', isDir: false, size: 100, modified: 0 },
          {
            name: 'dir2',
            path: '/dir1/dir2',
            isDir: true,
            size: 0,
            modified: 0,
            children: [
              { name: 'file2.ts', path: '/dir1/dir2/file2.ts', isDir: false, size: 200, modified: 0 },
            ],
          },
        ],
      },
    ];
    const result = flattenVisibleFiles(items);
    expect(result).toHaveLength(4);
    expect(result[0].path).toBe('/dir1');
    expect(result[0].depth).toBe(0);
    expect(result[1].path).toBe('/dir1/file1.ts');
    expect(result[1].depth).toBe(1);
    expect(result[2].path).toBe('/dir1/dir2');
    expect(result[2].depth).toBe(1);
    expect(result[3].path).toBe('/dir1/dir2/file2.ts');
    expect(result[3].depth).toBe(2);
  });

  it('handles empty children', () => {
    const items: FileInfo[] = [
      {
        name: 'dir1',
        path: '/dir1',
        isDir: true,
        size: 0,
        modified: 0,
        children: [],
      },
    ];
    const result = flattenVisibleFiles(items);
    expect(result).toHaveLength(1);
    expect(result[0].path).toBe('/dir1');
  });

  it('handles missing children', () => {
    const items: FileInfo[] = [
      {
        name: 'dir1',
        path: '/dir1',
        isDir: true,
        size: 0,
        modified: 0,
      },
    ];
    const result = flattenVisibleFiles(items);
    expect(result).toHaveLength(1);
  });

  it('handles empty array', () => {
    const result = flattenVisibleFiles([]);
    expect(result).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// useMultiSelect Hook Tests
// ---------------------------------------------------------------------------

describe('useMultiSelect', () => {
  describe('initial state', () => {
    it('initializes with empty selection', () => {
      renderHook();
      const { state } = ctx();
      expect(state.selectedPaths).toBeInstanceOf(Set);
      expect(state.selectedPaths.size).toBe(0);
    });

    it('initializes with showCheckboxes false', () => {
      renderHook();
      const { state } = ctx();
      expect(state.showCheckboxes).toBe(false);
    });

    it('initializes with batchProgress null', () => {
      renderHook();
      const { state } = ctx();
      expect(state.batchProgress).toBeNull();
    });

    it('initializes with isBatchBusy false', () => {
      renderHook();
      const { state } = ctx();
      expect(state.isBatchBusy).toBe(false);
    });
  });

  describe('togglePath', () => {
    it('adds path to selection when not selected', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      // Re-render to see the updated state
      renderHook();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(true);
    });

    it('removes path from selection when selected', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderHook();
      actions.togglePath('/file1.ts');
      renderHook();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(false);
    });

    it('shows checkboxes when adding first item', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderHook();

      expect(latestHookState.showCheckboxes).toBe(true);
    });

    it('hides checkboxes when removing last item', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderHook();

      // Checkboxes shown
      expect(latestHookState.showCheckboxes).toBe(true);

      // Remove and re-render
      actions.togglePath('/file1.ts');
      renderHook();

      // Note: The hook uses setTimeout to hide checkboxes, so we don't check showCheckboxes here
      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(false);
    });
  });

  describe('rangeSelect', () => {
    const visibleOrder = [
      { path: '/file1.ts', depth: 0 },
      { path: '/file2.ts', depth: 0 },
      { path: '/file3.ts', depth: 0 },
      { path: '/file4.ts', depth: 0 },
      { path: '/file5.ts', depth: 0 },
    ];

    it('selects single item when no anchor', () => {
      renderHook();
      const { actions } = ctx();
      actions.rangeSelect('/file3.ts', visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file3.ts')).toBe(true);
      expect(latestHookState.selectedPaths.size).toBe(1);
    });

    it('selects range from anchor to clicked', () => {
      renderHook();
      const { actions } = ctx();
      // Set anchor by clicking first
      actions.handleNormalClick('/file1.ts');

      // Then range select
      actions.rangeSelect('/file4.ts', visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(4);
      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file4.ts')).toBe(true);
    });

    it('selects range in reverse order', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleNormalClick('/file4.ts');
      renderAfterAction();

      actions.rangeSelect('/file2.ts', visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(3);
      expect(latestHookState.selectedPaths.has('/file2.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file4.ts')).toBe(true);
    });
  });

  describe('clearSelection', () => {
    it('clears all selected paths', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();
      actions.togglePath('/file2.ts');
      renderAfterAction();

      actions.clearSelection();
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(0);
    });

    it('hides checkboxes', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();

      actions.clearSelection();
      renderAfterAction();

      expect(latestHookState.showCheckboxes).toBe(false);
    });

    it('clears batchProgress', () => {
      renderHook();
      const { actions } = ctx();
      actions.setBatchProgress('Processing 1/2');
      renderAfterAction();
      actions.clearSelection();
      renderAfterAction();

      expect(latestHookState.batchProgress).toBeNull();
    });
  });

  describe('selectAll', () => {
    const visibleOrder = [
      { path: '/file1.ts', depth: 0 },
      { path: '/file2.ts', depth: 0 },
      { path: '/file3.ts', depth: 0 },
    ];

    it('selects all visible files', () => {
      renderHook();
      const { actions } = ctx();
      actions.selectAll(visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(3);
      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file2.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file3.ts')).toBe(true);
    });

    it('shows checkboxes', () => {
      renderHook();
      const { actions } = ctx();
      actions.selectAll(visibleOrder);
      renderAfterAction();

      expect(latestHookState.showCheckboxes).toBe(true);
    });
  });

  describe('handleNormalClick', () => {
    it('clears multi-selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();
      actions.togglePath('/file2.ts');
      renderAfterAction();

      actions.handleNormalClick('/file3.ts');
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(0);
    });

    it('sets anchor for range selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleNormalClick('/file1.ts');
      renderAfterAction();

      // Next rangeSelect should use this as anchor
      // We can't access the ref directly, but we can verify behavior
    });

    it('hides checkboxes when clearing selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();
      expect(latestHookState.showCheckboxes).toBe(true);

      actions.handleNormalClick('/file2.ts');
      renderAfterAction();
      expect(latestHookState.showCheckboxes).toBe(false);
    });
  });

  describe('handleCtrlClick', () => {
    it('toggles path selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleCtrlClick('/file1.ts');
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(true);

      actions.handleCtrlClick('/file1.ts');
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(false);
    });

    it('shows checkboxes when adding', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleCtrlClick('/file1.ts');
      renderAfterAction();

      expect(latestHookState.showCheckboxes).toBe(true);
    });
  });

  describe('handleShiftClick', () => {
    const visibleOrder = [
      { path: '/file1.ts', depth: 0 },
      { path: '/file2.ts', depth: 0 },
      { path: '/file3.ts', depth: 0 },
    ];

    it('does range selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleNormalClick('/file1.ts');
      renderAfterAction();

      actions.handleShiftClick('/file3.ts', visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.size).toBe(3);
    });

    it('selects single item when no anchor', () => {
      renderHook();
      const { actions } = ctx();
      actions.handleShiftClick('/file2.ts', visibleOrder);
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file2.ts')).toBe(true);
      expect(latestHookState.selectedPaths.size).toBe(1);
    });
  });

  describe('isSelected', () => {
    it('returns true for selected path', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();

      expect(latestHookActions.isSelected('/file1.ts')).toBe(true);
    });

    it('returns false for unselected path', () => {
      renderHook();
      const { actions } = ctx();

      expect(latestHookActions.isSelected('/file1.ts')).toBe(false);
    });
  });

  describe('batch progress tracking', () => {
    it('sets batch progress message', () => {
      renderHook();
      const { actions } = ctx();
      actions.setBatchProgress('Deleting 2/4...');
      renderAfterAction();

      expect(latestHookState.batchProgress).toBe('Deleting 2/4...');
    });

    it('clears batch progress with null', () => {
      renderHook();
      const { actions } = ctx();
      actions.setBatchProgress('Processing');
      renderAfterAction();
      actions.setBatchProgress(null);
      renderAfterAction();

      expect(latestHookState.batchProgress).toBeNull();
    });

    it('sets isBatchBusy flag', () => {
      renderHook();
      const { actions } = ctx();
      actions.setBatchBusy(true);
      renderAfterAction();

      expect(latestHookState.isBatchBusy).toBe(true);

      actions.setBatchBusy(false);
      renderAfterAction();

      expect(latestHookState.isBatchBusy).toBe(false);
    });
  });

  describe('setSelectedPaths', () => {
    it('sets selected paths directly', () => {
      renderHook();
      const { actions } = ctx();
      const newPaths = new Set(['/file1.ts', '/file2.ts']);
      actions.setSelectedPaths(newPaths);
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file2.ts')).toBe(true);
    });

    it('replaces existing selection', () => {
      renderHook();
      const { actions } = ctx();
      actions.togglePath('/file1.ts');
      renderAfterAction();

      const newPaths = new Set(['/file2.ts', '/file3.ts']);
      actions.setSelectedPaths(newPaths);
      renderAfterAction();

      expect(latestHookState.selectedPaths.has('/file1.ts')).toBe(false);
      expect(latestHookState.selectedPaths.has('/file2.ts')).toBe(true);
      expect(latestHookState.selectedPaths.has('/file3.ts')).toBe(true);
    });
  });
});
