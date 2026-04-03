// @ts-nocheck

import React from 'react';
import { createRoot, Root } from 'react-dom/client';
import { act } from 'react';
import { Simulate } from 'react-dom/test-utils';
import FileTree from './FileTree';
import { ApiService } from '../services/api';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../services/clientSession', () => ({
  clientFetch: jest.fn(),
}));

import { clientFetch } from '../services/clientSession';

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: jest.fn(),
  },
}));

// Mock navigator.clipboard
const mockClipboardWriteText = jest.fn().mockResolvedValue(undefined);
Object.assign(navigator, {
  clipboard: { writeText: mockClipboardWriteText },
});

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MOCK_FILES = [
  { name: 'src', path: 'src', is_dir: true, size: 0, mod_time: 0 },
  { name: 'main.go', path: 'main.go', is_dir: false, size: 100, mod_time: 1000 },
  { name: 'README.md', path: 'README.md', is_dir: false, size: 200, mod_time: 2000 },
];

const MOCK_DIR_CHILDREN = [
  { name: 'app.tsx', path: 'src/app.tsx', is_dir: false, size: 50, mod_time: 500 },
  { name: 'utils', path: 'src/utils', is_dir: true, size: 0, mod_time: 300 },
];

function mockFetchResponse(files: any[] = MOCK_FILES) {
  return {
    ok: true,
    status: 200,
    json: () =>
      Promise.resolve({
        message: 'success',
        path: '.',
        files,
      }),
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock requestAnimationFrame so ContextMenu's close-listener effect fires synchronously.
  let rafId = 0;
  global.requestAnimationFrame = ((cb) => { rafId += 1; cb(Date.now()); return rafId; }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn();
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);

  // Default: API call succeeds
  (ApiService.getInstance as jest.Mock).mockReturnValue({
    createItem: jest.fn().mockResolvedValue({}),
    deleteItem: jest.fn().mockResolvedValue({}),
    renameItem: jest.fn().mockResolvedValue({}),
  });

  // Default: clientFetch returns a file listing with files & dirs
  (clientFetch as jest.Mock).mockImplementation((url: string) => {
    if (url.includes('path=.')) return Promise.resolve(mockFetchResponse());
    if (url.includes('path=src')) return Promise.resolve(mockFetchResponse(MOCK_DIR_CHILDREN));
    return Promise.resolve(mockFetchResponse([]));
  });

  // Prevent confirm() from blocking tests
  window.confirm = jest.fn(() => false);

  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const defaultOnFileSelect = jest.fn();

/** Render FileTree and wait for initial data to load. */
async function renderTree(props: Partial<React.ComponentProps<typeof FileTree>> = {}) {
  await act(async () => {
    root.render(
      <FileTree
        onFileSelect={props.onFileSelect ?? defaultOnFileSelect}
        rootPath={props.rootPath ?? '.'}
        workspaceRoot={props.workspaceRoot}
        selectedFile={props.selectedFile}
        onRefresh={props.onRefresh}
        onItemCreated={props.onItemCreated}
        onDeleteItem={props.onDeleteItem}
      />
    );
  });
  // Wait for clientFetch to resolve + state updates
  await flushPromises();
}

/**
 * Find a file tree row by its displayed name and fire a contextmenu event.
 */
function fireContextMenuOnFile(fileName: string): void {
  const allItems = document.querySelectorAll('.file-tree-item');
  for (const item of Array.from(allItems)) {
    const nameEl = item.querySelector('.file-tree-name');
    if (nameEl && nameEl.textContent === fileName) {
      act(() => {
        item.dispatchEvent(
          new MouseEvent('contextmenu', {
            bubbles: true,
            cancelable: true,
            clientX: 100,
            clientY: 200,
          })
        );
      });
      return;
    }
  }
  throw new Error(`Could not find file tree item with name "${fileName}"`);
}

/** Return all context menu buttons currently rendered in the portal. */
function getContextButtons(): HTMLButtonElement[] {
  return Array.from(
    document.querySelectorAll('.context-menu .context-menu-item')
  );
}

/** Get the text content of all context menu buttons (trimmed). */
function getContextMenuTexts(): string[] {
  return getContextButtons().map((btn) => btn.textContent?.trim() ?? '');
}

/**
 * Right-click the background area of the file list (empty space).
 */
function fireContextMenuOnBackground(): void {
  const fileList = document.querySelector('.file-list');
  if (!fileList) throw new Error('Could not find .file-list element');
  act(() => {
    fileList.dispatchEvent(
      new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 300,
        clientY: 400,
      })
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('FileTree context menu – file items', () => {
  it('shows "Copy relative path" and "Open in editor" for files', async () => {
    await renderTree();

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toContain('Copy relative path');
    expect(texts).toContain('Open in editor');
    // Delete should also be present
    expect(texts).toContain('Delete');
  });

  it('does NOT show copy/open items for directories', async () => {
    await renderTree();

    fireContextMenuOnFile('src');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Copy relative path');
    expect(texts).not.toContain('Open in editor');
    expect(texts).not.toContain('Copy absolute path');
    // Directories should only see Add file, Add folder, Rename, Delete
    expect(texts).toContain('Add file');
    expect(texts).toContain('Add folder');
    expect(texts).toContain('Rename');
    expect(texts).toContain('Delete');
  });

  it('shows "Copy absolute path" when workspaceRoot is provided', async () => {
    await renderTree({ workspaceRoot: '/home/user/project' });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toContain('Copy absolute path');
  });

  it('does NOT show "Copy absolute path" when workspaceRoot is not provided', async () => {
    await renderTree({ workspaceRoot: undefined });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Copy absolute path');
  });
});

describe('FileTree context menu – clipboard & editor actions', () => {
  let onFileSelect: jest.Mock;

  beforeEach(() => {
    onFileSelect = jest.fn();
  });

  it('"Copy relative path" calls clipboard with the file path', async () => {
    await renderTree({ onFileSelect });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const copyRelBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy relative path'
    );
    expect(copyRelBtn).toBeDefined();

    await act(async () => {
      copyRelBtn!.click();
    });
    await flushPromises();

    expect(mockClipboardWriteText).toHaveBeenCalledWith('main.go');
  });

  it('"Copy absolute path" calls clipboard with workspaceRoot/file.path', async () => {
    await renderTree({ workspaceRoot: '/home/user/project', onFileSelect });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const copyAbsBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy absolute path'
    );
    expect(copyAbsBtn).toBeDefined();

    await act(async () => {
      copyAbsBtn!.click();
    });
    await flushPromises();

    expect(mockClipboardWriteText).toHaveBeenCalledWith('/home/user/project/main.go');
  });

  it('"Open in editor" calls onFileSelect with the file info object', async () => {
    await renderTree({ onFileSelect });

    fireContextMenuOnFile('README.md');
    await flushPromises();

    const openBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Open in editor'
    );
    expect(openBtn).toBeDefined();

    await act(async () => {
      openBtn!.click();
    });
    await flushPromises();

    expect(onFileSelect).toHaveBeenCalledTimes(1);
    // The callback receives a FileInfo-like object; verify the path field
    expect(onFileSelect.mock.calls[0][0].path).toBe('README.md');
    expect(onFileSelect.mock.calls[0][0].isDir).toBe(false);
  });

  it('context menu is dismissed after a copy action', async () => {
    await renderTree();

    fireContextMenuOnFile('main.go');
    await flushPromises();

    // Menu should be in the DOM now
    expect(document.querySelector('.context-menu')).not.toBeNull();

    const copyRelBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'Copy relative path'
    );

    await act(async () => {
      copyRelBtn!.click();
    });
    await flushPromises();

    // Menu should be removed
    expect(document.querySelector('.context-menu')).toBeNull();
  });
});

describe('FileTree background context menu', () => {
  it('background context menu appears when right-clicking empty space', async () => {
    await renderTree();

    expect(document.querySelector('.context-menu')).toBeNull();

    fireContextMenuOnBackground();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();
  });

  it('shows only "New File" and "New Folder"', async () => {
    await renderTree();

    fireContextMenuOnBackground();
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).toEqual(['New File', 'New Folder']);
    expect(texts).not.toContain('Rename');
    expect(texts).not.toContain('Delete');
    expect(texts).not.toContain('Copy relative path');
    expect(texts).not.toContain('Open in editor');
  });

  it('"New File" triggers draft mode at root', async () => {
    await renderTree();

    fireContextMenuOnBackground();
    await flushPromises();

    const newFileBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'New File'
    );
    expect(newFileBtn).toBeDefined();

    await act(async () => {
      newFileBtn!.click();
    });
    await flushPromises();

    // A draft row for creating a file should appear at root level
    expect(document.querySelector('.file-tree-draft-row')).not.toBeNull();
  });

  it('"New Folder" triggers draft mode at root', async () => {
    await renderTree();

    fireContextMenuOnBackground();
    await flushPromises();

    const newFolderBtn = getContextButtons().find(
      (btn) => btn.textContent?.trim() === 'New Folder'
    );
    expect(newFolderBtn).toBeDefined();

    await act(async () => {
      newFolderBtn!.click();
    });
    await flushPromises();

    // A draft row for creating a folder should appear at root level
    expect(document.querySelector('.file-tree-draft-row')).not.toBeNull();
  });

  it('background context menu is dismissed on Escape', async () => {
    await renderTree();

    fireContextMenuOnBackground();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();

    // ContextMenu attaches Escape listener on document via a deferred
    // requestAnimationFrame. The RAF mock fires synchronously inside act(),
    // but only after the effect runs. We need an extra act() to flush
    // the pending effect (which schedules the RAF inside the next act).
    await act(async () => {
      // no-op tick — lets React flush the useLayoutEffect + useEffect chain
      await Promise.resolve();
    });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    await flushPromises();
    await flushPromises();

    expect(document.querySelector('.context-menu')).toBeNull();
  });

  it('clicking a file item context menu does not show background menu', async () => {
    await renderTree();

    // Right-click directly on a file item (which calls stopPropagation)
    fireContextMenuOnFile('main.go');
    await flushPromises();

    // The file item context menu should be showing
    expect(document.querySelector('.context-menu')).not.toBeNull();

    // It should show file-specific items, NOT the background menu items
    const texts = getContextMenuTexts();
    expect(texts).not.toContain('New File');
    expect(texts).not.toContain('New Folder');
    expect(texts).toContain('Rename');
    expect(texts).toContain('Delete');
  });
});

// ---------------------------------------------------------------------------
// File tree filter tests
// ---------------------------------------------------------------------------

describe('FileTree filter', () => {
  /**
   * Helper: type a query into the filter input and wait for re-render.
   * Throws if the filter input cannot be found.
   */
  async function typeFilter(query: string) {
    const filterInput = container.querySelector('.file-tree-filter-input');
    if (!filterInput) throw new Error('Filter input not found');
    await act(async () => {
      // React's Simulate properly triggers the synthetic onChange handler
      // for controlled inputs, unlike native DOM dispatch.
      Simulate.change(filterInput, { target: { value: query } });
    });
    await flushPromises();
  }

  /**
   * Helper: return the set of file names currently visible in the tree.
   */
  function getVisibleFileNames(): string[] {
    const items = document.querySelectorAll('.file-tree-item .file-tree-name');
    return Array.from(items).map((el) => el.textContent ?? '');
  }

  /**
   * Helper: click the filter clear (X) button.
   */
  async function clickClearFilter() {
    const clearBtn = container.querySelector('.file-tree-filter-clear');
    if (!clearBtn) throw new Error('Clear button not found');
    await act(async () => {
      clearBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
  }

  it('renders the filter input in the header', async () => {
    await renderTree();

    const filterInput = container.querySelector('.file-tree-filter-input');
    expect(filterInput).not.toBeNull();
    expect((filterInput as HTMLInputElement).placeholder).toBe('Filter files...');
  });

  it('filters files by name, hiding non-matching items', async () => {
    await renderTree();

    // Verify all root items are present before filtering
    const before = getVisibleFileNames();
    expect(before).toContain('src');
    expect(before).toContain('main.go');
    expect(before).toContain('README.md');

    await typeFilter('main');

    // After filtering for "main", only main.go should be visible at root level
    const after = getVisibleFileNames();
    expect(after).toContain('main.go');
    expect(after).not.toContain('README.md');
    // "src" directory does not fuzzy-match "main" so it should be hidden
    expect(after).not.toContain('src');
  });

  it('shows matching files inside directories when query matches path segments', async () => {
    await renderTree();

    // First expand the src directory so its children are loaded into state.
    const srcItem = document.querySelector('.file-tree-item.directory');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    await typeFilter('app');

    // "app.tsx" is at path "src/app.tsx", so typing "app" should match it.
    // The "src" directory should still appear as an ancestor container.
    const after = getVisibleFileNames();
    expect(after).toContain('src');
    expect(after).toContain('app.tsx');
  });

  it('shows no results message when nothing matches', async () => {
    await renderTree();

    await typeFilter('zzzzz');

    const noResults = container.querySelector('.file-tree-no-results');
    expect(noResults).not.toBeNull();
    expect(noResults!.textContent).toContain('No files matching');
  });

  it('clearing the filter restores all files', async () => {
    await renderTree();

    // Filter to only show main.go
    await typeFilter('main');
    let names = getVisibleFileNames();
    expect(names).not.toContain('README.md');

    // Clear the filter via the X button
    await clickClearFilter();

    // All original items should be visible again
    names = getVisibleFileNames();
    expect(names).toContain('src');
    expect(names).toContain('main.go');
    expect(names).toContain('README.md');
  });

  it('Escape key clears the filter', async () => {
    await renderTree();

    await typeFilter('main');
    let names = getVisibleFileNames();
    expect(names).not.toContain('README.md');

    // Press Escape on the filter input
    const filterInput = container.querySelector('.file-tree-filter-input');
    if (!filterInput) throw new Error('Filter input not found');
    await act(async () => {
      Simulate.keyDown(filterInput, { key: 'Escape' });
    });
    await flushPromises();

    // All files should be visible again and the input should be empty
    names = getVisibleFileNames();
    expect(names).toContain('src');
    expect(names).toContain('main.go');
    expect(names).toContain('README.md');
    expect((filterInput as HTMLInputElement).value).toBe('');
  });

  it('auto-expands directories when filtering', async () => {
    await renderTree();

    // First, expand the src directory so its children get loaded into state.
    const srcItem = document.querySelector('.file-tree-item.directory');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // Now collapse the src directory back.
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // Verify the src directory is collapsed — no children wrapper visible.
    const childrenBefore = document.querySelector('.file-tree-children');
    expect(childrenBefore).toBeNull();

    // Type a query that matches something inside the src/ directory.
    // "app" matches "src/app.tsx".
    await typeFilter('app');

    // The src directory should now be auto-expanded — its children should be visible.
    const childrenAfter = document.querySelector('.file-tree-children');
    expect(childrenAfter).not.toBeNull();

    // Verify app.tsx is visible inside the tree
    const names = getVisibleFileNames();
    expect(names).toContain('app.tsx');
  });
});
