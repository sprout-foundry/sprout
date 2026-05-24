// @ts-nocheck

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { Simulate } from 'react-dom/test-utils';
import { FileTree, type FileInfo } from '@sprout/ui';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// clientFetch and ApiService are no longer used by FileTree
// (removed during refactoring to callback-props pattern)

vi.mock('./ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(false),
  showThemedPrompt: vi.fn().mockResolvedValue(null),
}));

vi.mock('../../../packages/ui/src/components/ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(false),
  showThemedPrompt: vi.fn().mockResolvedValue(null),
}));

// Mock navigator.clipboard
const mockClipboardWriteText = vi.fn().mockResolvedValue(undefined);
Object.assign(navigator, {
  clipboard: { writeText: mockClipboardWriteText },
});

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// Parsed FileInfo versions (what onFetchFiles callback returns)
const MOCK_FILES_PARSED: FileInfo[] = [
  { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' },
  { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
  { name: 'README.md', path: 'README.md', isDir: false, size: 200, modified: 2000, ext: '.md' },
  { name: 'node_modules', path: 'node_modules', isDir: true, size: 0, modified: 0, ext: '', gitStatus: 'ignored' },
  { name: 'dist', path: 'dist', isDir: true, size: 0, modified: 0, ext: '', gitStatus: 'ignored' },
  { name: '.env', path: '.env', isDir: false, size: 50, modified: 500, ext: '', gitStatus: 'ignored' },
];

const MOCK_DIR_CHILDREN_PARSED: FileInfo[] = [
  { name: 'app.tsx', path: 'src/app.tsx', isDir: false, size: 50, modified: 500, ext: '.tsx' },
  { name: 'utils', path: 'src/utils', isDir: true, size: 0, modified: 300, ext: '' },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock requestAnimationFrame so ContextMenu's close-listener effect fires synchronously.
  let rafId = 0;
  global.requestAnimationFrame = ((cb) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = vi.fn();
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);

  // Reset the default onFetchFiles mock
  defaultOnFetchFiles.mockClear();
  defaultOnFetchFiles.mockImplementation(async (path: string): Promise<FileInfo[]> => {
    if (path === '.') return MOCK_FILES_PARSED;
    if (path === 'src') return MOCK_DIR_CHILDREN_PARSED;
    return [];
  });

  // Reset other default mocks
  defaultOnOpenInFileBrowser.mockClear().mockResolvedValue(undefined);
  defaultOnRenamePath.mockClear().mockResolvedValue(undefined);
  defaultOnDeletePath.mockClear().mockResolvedValue(undefined);
  defaultOnCreateFile.mockClear().mockResolvedValue(undefined);
  defaultOnCreateFolder.mockClear().mockResolvedValue(undefined);

  // Prevent confirm() from blocking tests
  window.confirm = vi.fn(() => false);

  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  localStorage.removeItem('filetree-show-ignored');
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const defaultOnFileSelect = vi.fn();
const defaultOnOpenInFileBrowser = vi.fn().mockResolvedValue(undefined);
const defaultOnRenamePath = vi.fn().mockResolvedValue(undefined);
const defaultOnDeletePath = vi.fn().mockResolvedValue(undefined);
const defaultOnCreateFile = vi.fn().mockResolvedValue(undefined);
const defaultOnCreateFolder = vi.fn().mockResolvedValue(undefined);

/** Default onFetchFiles that returns parsed mock data. */
const defaultOnFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
  if (path === '.') return MOCK_FILES_PARSED;
  if (path === 'src') return MOCK_DIR_CHILDREN_PARSED;
  return [];
});

/** Render FileTree and wait for initial data to load. */
async function renderTree(props: Partial<React.ComponentProps<typeof FileTree>> = {}) {
  // eslint-disable-next-line testing-library/no-unnecessary-act
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
        onFetchFiles={props.onFetchFiles ?? defaultOnFetchFiles}
        onCreateFile={props.onCreateFile ?? defaultOnCreateFile}
        onCreateFolder={props.onCreateFolder ?? defaultOnCreateFolder}
        onDeletePath={props.onDeletePath ?? defaultOnDeletePath}
        onRenamePath={props.onRenamePath ?? defaultOnRenamePath}
        onOpenInFileBrowser={props.onOpenInFileBrowser ?? defaultOnOpenInFileBrowser}
        files={props.files}
      />,
    );
  });
  // Wait for onFetchFiles to resolve + state updates
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
          }),
        );
      });
      return;
    }
  }
  throw new Error(`Could not find file tree item with name "${fileName}"`);
}

/** Return all context menu buttons currently rendered in the portal. */
function getContextButtons(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll('.context-menu .context-menu-item'));
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
      }),
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

  it('does NOT show "Open in editor" for directories but shows path copy and file browser', async () => {
    await renderTree();

    fireContextMenuOnFile('src');
    await flushPromises();

    const texts = getContextMenuTexts();
    // Directories should see Add file, Add folder, Rename, Open in file browser, Copy relative path, Delete
    expect(texts).toContain('Add file');
    expect(texts).toContain('Add folder');
    expect(texts).toContain('Rename');
    expect(texts).toContain('Delete');
    expect(texts).toContain('Copy relative path');
    expect(texts).toContain('Open in file browser');
    // But NOT "Open in editor" (only for files)
    expect(texts).not.toContain('Open in editor');
    // And NOT "Copy absolute path" when workspaceRoot is not set
    expect(texts).not.toContain('Copy absolute path');
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
  let onFileSelect: vi.Mock;

  beforeEach(() => {
    onFileSelect = vi.fn();
  });

  it('"Copy relative path" calls clipboard with the file path', async () => {
    await renderTree({ onFileSelect });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const copyRelBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Copy relative path');
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

    const copyAbsBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Copy absolute path');
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

    const openBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Open in editor');
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

    const copyRelBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Copy relative path');

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

    const newFileBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'New File');
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

    const newFolderBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'New Folder');
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

  it('does not show ignored files in filter results when toggle is off', async () => {
    await renderTree();

    // Hide ignored files
    const toggleBtn = container.querySelector('.toggle-ignored-btn');
    if (!toggleBtn) throw new Error('Toggle ignored button not found');
    await act(async () => {
      toggleBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();

    // Filter for an ignored file name
    const filterInput = container.querySelector('.file-tree-filter-input');
    if (!filterInput) throw new Error('Filter input not found');
    await act(async () => {
      Simulate.change(filterInput, { target: { value: '.env' } });
    });
    await flushPromises();

    // The ignored file should not appear in results
    const items = document.querySelectorAll('.file-tree-item .file-tree-name');
    const names = Array.from(items).map((el) => el.textContent ?? '');
    expect(names).not.toContain('.env');
  });

  it('highlights matching text in file names with &lt;mark&gt; tags', async () => {
    await renderTree();

    await typeFilter('main');

    // Find the .file-tree-item.file elements (non-directories)
    const fileItems = document.querySelectorAll('.file-tree-item.file .file-tree-name');
    const nameElements = Array.from(fileItems);
    const mainGo = nameElements.find((el) => el.textContent === 'main.go');
    expect(mainGo).toBeDefined();
    expect(mainGo!.innerHTML).toContain('<mark>');
    expect(mainGo!.innerHTML).toContain('main');
  });

  it('does not highlight when no filter is active', async () => {
    await renderTree();

    const fileItems = document.querySelectorAll('.file-tree-item .file-tree-name');
    for (const item of Array.from(fileItems)) {
      expect(item.innerHTML).not.toContain('<mark>');
    }
  });
});

// ---------------------------------------------------------------------------
// Ignored files toggle tests
// ---------------------------------------------------------------------------

describe('FileTree ignored files toggle', () => {
  /**
   * Helper: return the set of file names currently visible in the tree.
   */
  function getVisibleFileNames(): string[] {
    const items = document.querySelectorAll('.file-tree-item .file-tree-name');
    return Array.from(items).map((el) => el.textContent ?? '');
  }

  /**
   * Helper: click the toggle-ignored button.
   */
  async function clickToggleIgnoredBtn() {
    const btn = container.querySelector('.toggle-ignored-btn');
    if (!btn) throw new Error('Toggle ignored button not found');
    await act(async () => {
      btn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
  }

  it('renders ignored files by default', async () => {
    await renderTree();

    const names = getVisibleFileNames();
    expect(names).toContain('node_modules');
    expect(names).toContain('dist');
    expect(names).toContain('.env');
  });

  it('hides ignored files when toggle is clicked', async () => {
    await renderTree();

    await clickToggleIgnoredBtn();

    const names = getVisibleFileNames();
    expect(names).not.toContain('node_modules');
    expect(names).not.toContain('dist');
    expect(names).not.toContain('.env');

    // Non-ignored files should still be visible
    expect(names).toContain('src');
    expect(names).toContain('main.go');
    expect(names).toContain('README.md');
  });

  it('shows ignored files again when toggled back on', async () => {
    await renderTree();

    // Hide ignored files
    await clickToggleIgnoredBtn();
    let names = getVisibleFileNames();
    expect(names).not.toContain('node_modules');

    // Show them again
    await clickToggleIgnoredBtn();
    names = getVisibleFileNames();
    expect(names).toContain('node_modules');
    expect(names).toContain('dist');
    expect(names).toContain('.env');
  });

  it('persists preference to localStorage', async () => {
    await renderTree();

    const storageSpy = vi.spyOn(Storage.prototype, 'setItem');

    // Toggle to hide ignored files
    await clickToggleIgnoredBtn();

    expect(storageSpy).toHaveBeenCalledWith('filetree-show-ignored', 'false');

    storageSpy.mockRestore();
  });

  it('respects localStorage value on mount', async () => {
    // Set localStorage to hide ignored files before rendering
    localStorage.setItem('filetree-show-ignored', 'false');

    await renderTree();

    const names = getVisibleFileNames();
    expect(names).not.toContain('node_modules');
    expect(names).not.toContain('dist');
    expect(names).not.toContain('.env');

    // Clean up
    localStorage.removeItem('filetree-show-ignored');
  });

  it('toggle button has active class when ignored files are shown', async () => {
    await renderTree();

    const btn = container.querySelector('.toggle-ignored-btn');
    expect(btn).not.toBeNull();
    expect(btn!.classList.contains('active')).toBe(true);

    await clickToggleIgnoredBtn();
    expect(btn!.classList.contains('active')).toBe(false);
  });

  it('keeps a non-ignored directory visible when it contains only ignored children', async () => {
    const customFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') {
        return [
          { name: 'output', path: 'output', isDir: true, size: 0, modified: 0, ext: '' },
          { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
        ];
      }
      if (path === 'output') {
        return [
          {
            name: 'build.log',
            path: 'output/build.log',
            isDir: false,
            size: 50,
            modified: 500,
            ext: '.log',
            gitStatus: 'ignored',
          },
        ];
      }
      return [];
    });

    await renderTree({ onFetchFiles: customFetchFiles });

    // Expand the output directory
    const outputItem = document.querySelector('.file-tree-item.directory');
    if (outputItem) {
      await act(async () => {
        outputItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // Verify both items visible before toggle
    let names = getVisibleFileNames();
    expect(names).toContain('output');
    expect(names).toContain('main.go');

    // Hide ignored files
    await clickToggleIgnoredBtn();

    // The 'output' directory should still be visible even though all its children are ignored
    names = getVisibleFileNames();
    expect(names).toContain('output');
    expect(names).toContain('main.go');
    expect(names).not.toContain('build.log');
  });
});

// ---------------------------------------------------------------------------
// Drag-and-drop tests
// ---------------------------------------------------------------------------

describe('FileTree drag-and-drop', () => {
  /**
   * Helper: get a file tree item DOM element by name.
   */
  function getFileItem(fileName: string): HTMLElement | null {
    const allItems = document.querySelectorAll('.file-tree-item');
    for (const item of Array.from(allItems)) {
      const nameEl = item.querySelector('.file-tree-name');
      if (nameEl && nameEl.textContent === fileName) return item as HTMLElement;
    }
    return null;
  }

  /**
   * Helper: simulate drag start on a file tree item.
   * Returns a mock DataTransfer with the file path data set.
   */
  function fireDragStart(fileName: string): DataTransfer | null {
    const item = getFileItem(fileName);
    if (!item) return null;
    const dataStore: Record<string, string> = {};
    const dataTransfer = {
      dropEffect: 'none',
      effectAllowed: 'none',
      data: dataStore,
      setData(type: string, value: string) {
        this.data[type] = value;
      },
      getData(type: string) {
        return this.data[type] || '';
      },
      clearData() {
        Object.keys(this.data).forEach((k) => delete this.data[k]);
      },
      setDragImage() {},
      items: [],
      types: [] as string[],
    } as unknown as DataTransfer;
    const event = new Event('dragstart', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer });
    Object.defineProperty(event, 'currentTarget', { value: item });
    act(() => {
      item.dispatchEvent(event);
    });
    return dataTransfer;
  }

  /**
   * Helper: simulate dragover on a file tree item.
   * Uses relatedTarget: null so handleDragLeave doesn't skip clearing.
   */
  function fireDragOverOnItem(fileName: string, dataTransfer: DataTransfer): void {
    const item = getFileItem(fileName);
    if (!item) return;
    const event = new Event('dragover', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer });
    Object.defineProperty(event, 'relatedTarget', { value: null });
    Object.defineProperty(event, 'preventDefault', { value: vi.fn() });
    Object.defineProperty(event, 'stopPropagation', { value: vi.fn() });
    act(() => {
      item.dispatchEvent(event);
    });
  }

  /**
   * Helper: simulate drop on a file tree item.
   */
  async function fireDropOnItem(fileName: string, dataTransfer: DataTransfer): Promise<void> {
    const item = getFileItem(fileName);
    if (!item) return;
    const event = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer });
    Object.defineProperty(event, 'preventDefault', { value: vi.fn() });
    Object.defineProperty(event, 'stopPropagation', { value: vi.fn() });
    act(() => {
      item.dispatchEvent(event);
    });
    await flushPromises();
    // Extra tick for async renameItem
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    await flushPromises();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    await flushPromises();
  }

  /**
   * Helper: simulate dragover on file-list background.
   */
  function fireDragOverOnBackground(dataTransfer: DataTransfer): void {
    const fileList = document.querySelector('.file-list');
    if (!fileList) return;
    const event = new Event('dragover', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer });
    Object.defineProperty(event, 'relatedTarget', { value: null });
    Object.defineProperty(event, 'preventDefault', { value: vi.fn() });
    Object.defineProperty(event, 'stopPropagation', { value: vi.fn() });
    act(() => {
      fileList.dispatchEvent(event);
    });
  }

  /**
   * Helper: simulate drop on file-list background.
   */
  async function fireDropOnBackground(dataTransfer: DataTransfer): Promise<void> {
    const fileList = document.querySelector('.file-list');
    if (!fileList) return;
    const event = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(event, 'dataTransfer', { value: dataTransfer });
    Object.defineProperty(event, 'preventDefault', { value: vi.fn() });
    Object.defineProperty(event, 'stopPropagation', { value: vi.fn() });
    act(() => {
      fileList.dispatchEvent(event);
    });
    await flushPromises();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    await flushPromises();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    await flushPromises();
  }

  it('sets draggable attribute on file tree items', async () => {
    await renderTree();
    const item = getFileItem('main.go');
    expect(item?.getAttribute('draggable')).toBe('true');
  });

  it('stores file path in dataTransfer on drag start', async () => {
    await renderTree();
    const dt = fireDragStart('main.go');
    expect(dt?.getData('application/x-sprout-filepath')).toBe('main.go');
  });

  it('applies "dragging" class to the dragged item', async () => {
    await renderTree();
    fireDragStart('main.go');
    await flushPromises();
    const item = getFileItem('main.go');
    expect(item?.classList.contains('dragging')).toBe(true);
  });

  it('clears drag state on drag end', async () => {
    await renderTree();
    fireDragStart('main.go');
    await flushPromises();

    const item = getFileItem('main.go');
    if (item) {
      act(() => {
        item.dispatchEvent(new Event('dragend', { bubbles: true }));
      });
    }
    await flushPromises();

    expect(item?.classList.contains('dragging')).toBe(false);
  });

  it('shows drop-target class when dragging over a directory', async () => {
    await renderTree();
    const dt = fireDragStart('main.go')!;
    await flushPromises();

    fireDragOverOnItem('src', dt);
    await flushPromises();

    const srcItem = getFileItem('src');
    expect(srcItem?.classList.contains('drop-target')).toBe(true);
  });

  it('does NOT show drop-target class when dragging over a file', async () => {
    await renderTree();
    const dt = fireDragStart('README.md')!;
    await flushPromises();

    fireDragOverOnItem('main.go', dt);
    await flushPromises();

    const mainGoItem = getFileItem('main.go');
    expect(mainGoItem?.classList.contains('drop-target')).toBe(false);
  });

  it('calls onRenamePath when dropping a file onto a directory', async () => {
    const onRenamePath = vi.fn().mockResolvedValue(undefined);
    await renderTree({ onRenamePath });

    const dt = fireDragStart('main.go')!;
    await flushPromises();

    fireDragOverOnItem('src', dt);
    await flushPromises();

    await fireDropOnItem('src', dt);

    expect(onRenamePath).toHaveBeenCalledWith('main.go', 'src/main.go');
  });

  it('does NOT call onRenamePath when dropping onto self', async () => {
    const onRenamePath = vi.fn().mockResolvedValue(undefined);
    await renderTree({ onRenamePath });

    // Try to drop src onto itself
    const dt = fireDragStart('src')!;
    await flushPromises();

    // handleDragOver should refuse — setDropTargetPath(null)
    fireDragOverOnItem('src', dt);
    await flushPromises();

    const srcItem = getFileItem('src');
    expect(srcItem?.classList.contains('drop-target')).toBe(false);

    await fireDropOnItem('src', dt);
    expect(onRenamePath).not.toHaveBeenCalled();
  });

  it('shows drop-on-root class when dragging over file-list background with a nested file', async () => {
    const customFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') return [{ name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' }];
      if (path === 'src')
        return [{ name: 'utils.go', path: 'src/utils.go', isDir: false, size: 50, modified: 500, ext: '.go' }];
      return [];
    });

    await renderTree({ onFetchFiles: customFetchFiles });

    // Expand src to see its children
    const srcItem = getFileItem('src');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // Start dragging the nested file
    const dt = fireDragStart('utils.go')!;
    await flushPromises();

    // Drag over the file-list background
    fireDragOverOnBackground(dt);
    await flushPromises();

    const fileList = document.querySelector('.file-list');
    expect(fileList?.classList.contains('drop-on-root')).toBe(true);
  });

  it('does NOT show drop-on-root when dragging an already-root file over background', async () => {
    await renderTree();

    const dt = fireDragStart('main.go')!;
    await flushPromises();

    fireDragOverOnBackground(dt);
    await flushPromises();

    const fileList = document.querySelector('.file-list');
    // main.go's parent is already rootPath ('.'), so drop-on-root should NOT show
    expect(fileList?.classList.contains('drop-on-root')).toBe(false);
  });

  it('calls onRenamePath when dropping a nested file on the background (root)', async () => {
    const onRenamePath = vi.fn().mockResolvedValue(undefined);
    const customFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') return [{ name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' }];
      if (path === 'src')
        return [{ name: 'helper.go', path: 'src/helper.go', isDir: false, size: 50, modified: 500, ext: '.go' }];
      return [];
    });

    await renderTree({ onFetchFiles: customFetchFiles, onRenamePath });

    // Expand src to see children
    const srcItem = getFileItem('src');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    const dt = fireDragStart('helper.go')!;
    await flushPromises();

    fireDragOverOnBackground(dt);
    await flushPromises();

    await fireDropOnBackground(dt);

    expect(onRenamePath).toHaveBeenCalledWith('src/helper.go', 'helper.go');
  });

  it('clears drop-on-root class when hovering over a specific directory', async () => {
    const customFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') {
        return [
          { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' },
          { name: 'lib', path: 'lib', isDir: true, size: 0, modified: 0, ext: '' },
        ];
      }
      if (path === 'src')
        return [{ name: 'app.tsx', path: 'src/app.tsx', isDir: false, size: 50, modified: 500, ext: '.tsx' }];
      return [];
    });

    await renderTree({ onFetchFiles: customFetchFiles });

    // Expand src
    const srcItem = getFileItem('src');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    const dt = fireDragStart('app.tsx')!;
    await flushPromises();

    // Hover background first — should show drop-on-root
    fireDragOverOnBackground(dt);
    await flushPromises();
    let fileList = document.querySelector('.file-list');
    expect(fileList?.classList.contains('drop-on-root')).toBe(true);

    // Now hover over a specific directory — should clear drop-on-root
    fireDragOverOnItem('lib', dt);
    await flushPromises();
    fileList = document.querySelector('.file-list');
    expect(fileList?.classList.contains('drop-on-root')).toBe(false);
    // And show drop-target on the directory
    expect(getFileItem('lib')?.classList.contains('drop-target')).toBe(true);
  });

  it('clears drop-on-root on drag end', async () => {
    const customFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') return [{ name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' }];
      if (path === 'src')
        return [{ name: 'nested.go', path: 'src/nested.go', isDir: false, size: 50, modified: 500, ext: '.go' }];
      return [];
    });

    await renderTree({ onFetchFiles: customFetchFiles });

    // Expand src
    const srcItem = getFileItem('src');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    fireDragStart('nested.go');
    await flushPromises();

    fireDragOverOnBackground({
      dropEffect: 'move',
      getData: () => 'src/nested.go',
      setData: () => {},
      types: [] as string[],
      items: [],
      clearData: () => {},
      setDragImage: () => {},
    } as unknown as DataTransfer);
    await flushPromises();

    let fileList = document.querySelector('.file-list');
    expect(fileList?.classList.contains('drop-on-root')).toBe(true);

    // Fire dragend
    const nestedItem = getFileItem('nested.go');
    if (nestedItem) {
      act(() => {
        nestedItem.dispatchEvent(new Event('dragend', { bubbles: true }));
      });
    }
    await flushPromises();

    fileList = document.querySelector('.file-list');
    expect(fileList?.classList.contains('drop-on-root')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Callback props tests
// ---------------------------------------------------------------------------

describe('FileTree callback props – onFetchFiles', () => {
  const mockFiles: FileInfo[] = [
    { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '', children: [] },
    { name: 'index.ts', path: 'index.ts', isDir: false, size: 100, modified: 1000, ext: '.ts' },
  ];

  it('calls onFetchFiles with root path on mount', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    expect(onFetchFiles).toHaveBeenCalledWith('.');
  });

  it('renders files returned by onFetchFiles', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    const names = document.querySelectorAll('.file-tree-item .file-tree-name');
    const nameTexts = Array.from(names).map((el) => el.textContent ?? '');
    expect(nameTexts).toContain('src');
    expect(nameTexts).toContain('index.ts');
  });

  it('does NOT call default onFetchFiles when custom one is provided', async () => {
    const customFetchFiles = vi.fn().mockResolvedValue(mockFiles);

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={customFetchFiles} />);
    });
    await flushPromises();

    expect(customFetchFiles).toHaveBeenCalledWith('.');
    expect(defaultOnFetchFiles).not.toHaveBeenCalled();
  });

  it('renders empty tree when onFetchFiles is NOT provided', async () => {
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." />);
    });
    await flushPromises();

    // With no onFetchFiles, the tree should render empty
    const names = document.querySelectorAll('.file-tree-item .file-tree-name');
    const nameTexts = Array.from(names).map((el) => el.textContent ?? '');
    expect(nameTexts).toEqual([]);
  });
});

describe('FileTree callback props – onDeletePath', () => {
  const mockFiles: FileInfo[] = [
    { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
    { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '', children: [] },
  ];

  async function renderWithCallbacks(callbacks = {}) {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} {...callbacks} />);
    });
    await flushPromises();
    return { onFetchFiles };
  }

  it.skip('calls onDeletePath when deleting a file via context menu', async () => {
    const onDeletePath = vi.fn().mockResolvedValue(undefined);
    const { onFetchFiles } = await renderWithCallbacks({ onDeletePath });

    // Mock showThemedConfirm to return true
    const { showThemedConfirm } = require('./ThemedDialog');
    showThemedConfirm.mockResolvedValueOnce(true);

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const deleteBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Delete');
    expect(deleteBtn).toBeDefined();

    await act(async () => {
      deleteBtn!.click();
    });
    await flushPromises();

    expect(onDeletePath).toHaveBeenCalledWith('main.go', false);
  });

  it.skip('calls onDeletePath with correct arguments when deleting a file', async () => {
    const onDeletePath = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onDeletePath });

    const { showThemedConfirm } = require('./ThemedDialog');
    showThemedConfirm.mockResolvedValueOnce(true);

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const deleteBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Delete');
    await act(async () => {
      deleteBtn!.click();
    });
    await flushPromises();

    // When onDeletePath is provided, the component uses it directly
    // (no more apiService fallback)
    expect(onDeletePath).toHaveBeenCalledWith('main.go', false);
  });
});

describe('FileTree callback props – onRenamePath', () => {
  const mockFiles: FileInfo[] = [
    { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
    { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '', children: [] },
  ];

  async function renderWithCallbacks(callbacks = {}) {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} {...callbacks} />);
    });
    await flushPromises();
    return { onFetchFiles };
  }

  it('calls onRenamePath when confirming a rename draft', async () => {
    const onRenamePath = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onRenamePath });

    // Right-click file and select "Rename"
    fireContextMenuOnFile('main.go');
    await flushPromises();

    const renameBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Rename');
    expect(renameBtn).toBeDefined();

    await act(async () => {
      renameBtn!.click();
    });
    await flushPromises();

    // Type new name in the inline rename input
    const renameInput = document.querySelector('.file-tree-inline-editor .create-input');
    expect(renameInput).not.toBeNull();
    await act(async () => {
      Simulate.change(renameInput, { target: { value: 'main_renamed.go' } });
    });
    await flushPromises();

    // Click confirm
    const confirmBtn = document.querySelector('.file-tree-inline-editor .create-confirm');
    expect(confirmBtn).not.toBeNull();
    await act(async () => {
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
    await act(async () => {
      await Promise.resolve();
    });
    await flushPromises();

    expect(onRenamePath).toHaveBeenCalledWith('main.go', 'main_renamed.go');
  });

  it('calls onRenamePath for rename (no ApiService fallback)', async () => {
    const onRenamePath = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onRenamePath });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const renameBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Rename');
    await act(async () => {
      renameBtn!.click();
    });
    await flushPromises();

    const renameInput = document.querySelector('.file-tree-inline-editor .create-input');
    await act(async () => {
      Simulate.change(renameInput, { target: { value: 'renamed.go' } });
    });
    await flushPromises();

    const confirmBtn = document.querySelector('.file-tree-inline-editor .create-confirm');
    await act(async () => {
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
    await act(async () => {
      await Promise.resolve();
    });
    await flushPromises();

    // onRenamePath was called (no ApiService fallback used)
    expect(onRenamePath).toHaveBeenCalledWith('main.go', 'renamed.go');
  });
});

describe('FileTree callback props – onCreateFile and onCreateFolder', () => {
  const mockFiles: FileInfo[] = [
    { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
  ];

  async function renderWithCallbacks(callbacks = {}) {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} {...callbacks} />);
    });
    await flushPromises();
    return { onFetchFiles };
  }

  it('calls onCreateFile when confirming a new file draft', async () => {
    const onCreateFile = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onCreateFile });

    fireContextMenuOnBackground();
    await flushPromises();

    const newFileBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'New File');
    await act(async () => {
      newFileBtn!.click();
    });
    await flushPromises();

    // Type a name in the draft input
    const draftInput = document.querySelector('.file-tree-draft-row .create-input');
    expect(draftInput).not.toBeNull();
    await act(async () => {
      Simulate.change(draftInput, { target: { value: 'newfile.ts' } });
    });
    await flushPromises();

    // Click confirm
    const confirmBtn = document.querySelector('.file-tree-draft-row .create-confirm');
    expect(confirmBtn).not.toBeNull();
    await act(async () => {
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
    await act(async () => {
      await Promise.resolve();
    });
    await flushPromises();

    expect(onCreateFile).toHaveBeenCalledWith('.', 'newfile.ts');
  });

  it('calls onCreateFolder when confirming a new folder draft', async () => {
    const onCreateFolder = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onCreateFolder });

    fireContextMenuOnBackground();
    await flushPromises();

    const newFolderBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'New Folder');
    await act(async () => {
      newFolderBtn!.click();
    });
    await flushPromises();

    const draftInput = document.querySelector('.file-tree-draft-row .create-input');
    expect(draftInput).not.toBeNull();
    await act(async () => {
      Simulate.change(draftInput, { target: { value: 'myfolder' } });
    });
    await flushPromises();

    const confirmBtn = document.querySelector('.file-tree-draft-row .create-confirm');
    await act(async () => {
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
    await act(async () => {
      await Promise.resolve();
    });
    await flushPromises();

    expect(onCreateFolder).toHaveBeenCalledWith('.', 'myfolder');
  });

  it('calls onCreateFile for file creation (no ApiService fallback)', async () => {
    const onCreateFile = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onCreateFile });

    fireContextMenuOnBackground();
    await flushPromises();

    const newFileBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'New File');
    await act(async () => {
      newFileBtn!.click();
    });
    await flushPromises();

    const draftInput = document.querySelector('.file-tree-draft-row .create-input');
    await act(async () => {
      Simulate.change(draftInput, { target: { value: 'test.ts' } });
    });
    await flushPromises();

    const confirmBtn = document.querySelector('.file-tree-draft-row .create-confirm');
    await act(async () => {
      confirmBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushPromises();
    await act(async () => {
      await Promise.resolve();
    });
    await flushPromises();

    // onCreateFile was called (no ApiService fallback used)
    expect(onCreateFile).toHaveBeenCalledWith('.', 'test.ts');
  });
});

describe('FileTree callback props – onOpenInFileBrowser', () => {
  const mockFiles: FileInfo[] = [
    { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
  ];

  async function renderWithCallbacks(callbacks = {}) {
    const onFetchFiles = vi.fn().mockResolvedValue(mockFiles);
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} {...callbacks} />);
    });
    await flushPromises();
  }

  it('calls onOpenInFileBrowser when context menu item is clicked', async () => {
    const onOpenInFileBrowser = vi.fn().mockResolvedValue(undefined);
    await renderWithCallbacks({ onOpenInFileBrowser });

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const openBtn = getContextButtons().find((btn) => btn.textContent?.trim() === 'Open in file browser');
    expect(openBtn).toBeDefined();

    await act(async () => {
      openBtn!.click();
    });
    await flushPromises();

    expect(onOpenInFileBrowser).toHaveBeenCalledWith('main.go');
  });

  it('does NOT show "Open in file browser" when callback is not provided', async () => {
    // When onOpenInFileBrowser is not provided, the button should NOT appear
    await renderWithCallbacks({});

    fireContextMenuOnFile('main.go');
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts).not.toContain('Open in file browser');
  });
});

// ---------------------------------------------------------------------------
// FileTree data prop – files
// ---------------------------------------------------------------------------

describe('FileTree data prop – files', () => {
  /** Helper: return the set of file names currently visible in the tree. */
  function getVisibleFileNames(): string[] {
    const items = document.querySelectorAll('.file-tree-item .file-tree-name');
    return Array.from(items).map((el) => el.textContent ?? '');
  }

  it('renders files from the files prop without calling onFetchFiles on mount', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue([]);
    const initialFiles: FileInfo[] = [
      { name: 'alpha.ts', path: 'alpha.ts', isDir: false, size: 10, modified: 100, ext: '.ts' },
      { name: 'beta.ts', path: 'beta.ts', isDir: false, size: 20, modified: 200, ext: '.ts' },
    ];

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." files={initialFiles} onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    const names = getVisibleFileNames();
    expect(names).toContain('alpha.ts');
    expect(names).toContain('beta.ts');

    // onFetchFiles should NOT have been called for the initial load
    expect(onFetchFiles).not.toHaveBeenCalled();
  });

  it('updates the tree when the files prop changes', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue([]);
    const firstFiles: FileInfo[] = [
      { name: 'alpha.ts', path: 'alpha.ts', isDir: false, size: 10, modified: 100, ext: '.ts' },
    ];
    const secondFiles: FileInfo[] = [
      { name: 'gamma.rs', path: 'gamma.rs', isDir: false, size: 30, modified: 300, ext: '.rs' },
      { name: 'delta.rs', path: 'delta.rs', isDir: false, size: 40, modified: 400, ext: '.rs' },
    ];

    // Render with first files
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." files={firstFiles} onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    let names = getVisibleFileNames();
    expect(names).toContain('alpha.ts');
    expect(names).not.toContain('gamma.rs');

    // Re-render with different files
    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." files={secondFiles} onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    names = getVisibleFileNames();
    expect(names).not.toContain('alpha.ts');
    expect(names).toContain('gamma.rs');
    expect(names).toContain('delta.rs');
  });

  it('uses files prop for initial load but onFetchFiles for directory expansion', async () => {
    const dirChildren: FileInfo[] = [
      { name: 'app.tsx', path: 'src/app.tsx', isDir: false, size: 50, modified: 500, ext: '.tsx' },
    ];
    const onFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === 'src') return dirChildren;
      return [];
    });

    const initialFiles: FileInfo[] = [
      { name: 'src', path: 'src', isDir: true, size: 0, modified: 0, ext: '' },
      { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
    ];

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." files={initialFiles} onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    // Initial load should use files prop, not call onFetchFiles
    const names = getVisibleFileNames();
    expect(names).toContain('src');
    expect(names).toContain('main.go');
    expect(onFetchFiles).not.toHaveBeenCalled();

    // Expand the src directory by clicking on it
    const srcItem = document.querySelector('.file-tree-item.directory');
    expect(srcItem).not.toBeNull();
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // onFetchFiles should now have been called for directory expansion
    expect(onFetchFiles).toHaveBeenCalledWith('src');

    // The children should be visible
    const namesAfter = getVisibleFileNames();
    expect(namesAfter).toContain('app.tsx');
  });

  it('calls onFetchFiles on mount when files prop is not provided', async () => {
    const onFetchFiles = vi.fn().mockImplementation(async (path: string): Promise<FileInfo[]> => {
      if (path === '.') {
        return [{ name: 'hello.go', path: 'hello.go', isDir: false, size: 80, modified: 800, ext: '.go' }];
      }
      return [];
    });

    await act(async () => {
      root.render(<FileTree onFileSelect={vi.fn()} rootPath="." onFetchFiles={onFetchFiles} />);
    });
    await flushPromises();

    // onFetchFiles should have been called for the root path on mount
    expect(onFetchFiles).toHaveBeenCalledWith('.');

    const names = getVisibleFileNames();
    expect(names).toContain('hello.go');
  });

  it('does NOT call onFetchFiles when files prop provides pre-loaded children', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue([]);
    const initialFiles: FileInfo[] = [
      {
        name: 'src',
        path: 'src',
        isDir: true,
        size: 0,
        modified: 0,
        ext: '',
        children: [{ name: 'app.tsx', path: 'src/app.tsx', isDir: false, size: 50, modified: 500, ext: '.tsx' }],
      },
      { name: 'main.go', path: 'main.go', isDir: false, size: 100, modified: 1000, ext: '.go' },
    ];

    await renderTree({ files: initialFiles, onFetchFiles });

    // Verify the tree rendered the files prop data
    const names = getVisibleFileNames();
    expect(names).toContain('src');
    expect(names).toContain('main.go');

    // onFetchFiles should NOT have been called for initial load
    expect(onFetchFiles).not.toHaveBeenCalled();

    // Expand the src directory by clicking on it — children are already loaded
    const srcItem = document.querySelector('.file-tree-item.directory');
    if (srcItem) {
      await act(async () => {
        srcItem.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      await flushPromises();
    }

    // onFetchFiles should STILL not have been called — children were pre-loaded
    expect(onFetchFiles).not.toHaveBeenCalled();

    // The children should be visible
    const namesAfterExpand = getVisibleFileNames();
    expect(namesAfterExpand).toContain('app.tsx');
  });

  it('imperative refresh() uses files prop as base when provided', async () => {
    const onFetchFiles = vi.fn().mockResolvedValue([]);
    const initialFiles: FileInfo[] = [
      { name: 'alpha.ts', path: 'alpha.ts', isDir: false, size: 10, modified: 100, ext: '.ts' },
    ];

    const treeRef = React.createRef<{ refresh: () => void; revealFile: (filePath: string) => void }>();

    await act(async () => {
      root.render(
        <FileTree ref={treeRef} onFileSelect={vi.fn()} rootPath="." files={initialFiles} onFetchFiles={onFetchFiles} />,
      );
    });
    await flushPromises();

    // Initial render shows alpha.ts from files prop
    let names = getVisibleFileNames();
    expect(names).toContain('alpha.ts');
    expect(onFetchFiles).not.toHaveBeenCalled();

    // Call refresh via imperative handle
    await act(async () => {
      treeRef.current?.refresh();
    });
    await flushPromises();

    // After refresh, should still show the same data (from filesPropRef)
    names = getVisibleFileNames();
    expect(names).toContain('alpha.ts');

    // onFetchFiles should still not be called since files prop was provided
    expect(onFetchFiles).not.toHaveBeenCalled();
  });
});
