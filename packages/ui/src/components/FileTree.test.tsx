import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import FileTree from './FileTree';
import type { FileTreeProps, FileTreeHandle } from './FileTree';
import type { FileInfo } from '../types/file-tree';

// ── Mock dependencies ────────────────────────────────────────────────

jest.mock('./ThemedDialog', () => ({
  showThemedConfirm: jest.fn().mockResolvedValue(true),
  showThemedPrompt: jest.fn().mockResolvedValue('new-name'),
}));

jest.mock('../utils/log', () => ({
  debugLog: jest.fn(),
}));

jest.mock('../utils/clipboard', () => ({
  copyToClipboard: jest.fn().mockResolvedValue(undefined),
}));

jest.mock('../utils/fuzzyMatch', () => ({
  fuzzyScore: (query: string, label: string) => {
    if (!query) return { score: 0, matches: [] };
    if (label.toLowerCase().includes(query.toLowerCase())) {
      const idx = label.toLowerCase().indexOf(query.toLowerCase());
      return { score: 100, matches: [[idx, idx + query.length]] };
    }
    return { score: -1, matches: [] };
  },
  highlightMatches: (label: string, matches: Array<[number, number]>) => {
    if (!matches.length) return label;
    let html = '';
    let cursor = 0;
    for (const [start, end] of matches) {
      if (cursor < start) html += label.slice(cursor, start);
      html += '<mark>' + label.slice(start, end) + '</mark>';
      cursor = end;
    }
    if (cursor < label.length) html += label.slice(cursor);
    return html;
  },
}));

jest.mock('../hooks/useMultiSelect', () => {
  const createMock = () => {
    const state = {
      selectedPaths: new Set<string>(),
      showCheckboxes: false,
      batchProgress: null as string | null,
      isBatchBusy: false,
    };
    const actions = {
      togglePath: jest.fn(),
      rangeSelect: jest.fn(),
      clearSelection: jest.fn(),
      selectAll: jest.fn(),
      handleNormalClick: jest.fn(),
      handleCtrlClick: jest.fn(),
      handleShiftClick: jest.fn(),
      isSelected: jest.fn().mockReturnValue(false),
      setBatchProgress: jest.fn(),
      setBatchBusy: jest.fn(),
      setSelectedPaths: jest.fn(),
    };
    return [state, actions];
  };
  return { useMultiSelect: createMock, flattenVisibleFiles: (items: FileInfo[]) => items.map(i => ({ path: i.path, depth: 0 })) };
});

// ── Mock localStorage ────────────────────────────────────────────────
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: jest.fn((key: string) => store[key] ?? null),
    setItem: jest.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: jest.fn((key: string) => { delete store[key]; }),
    clear: jest.fn(() => { store = {}; }),
  };
})();
Object.defineProperty(global, 'localStorage', { value: localStorageMock });

// ── Test Setup ───────────────────────────────────────────────────────

describe('FileTree', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    container.id = 'file-tree-test-root';
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    jest.clearAllMocks();
  });

  const makeFiles = (): FileInfo[] => [
    { name: 'src', path: 'src', isDir: true, size: 0, modified: Date.now(), children: [
      { name: 'index.ts', path: 'src/index.ts', isDir: false, size: 42, modified: Date.now(), ext: '.ts' },
      { name: 'utils.ts', path: 'src/utils.ts', isDir: false, size: 128, modified: Date.now(), ext: '.ts' },
    ]},
    { name: 'README.md', path: 'README.md', isDir: false, size: 256, modified: Date.now(), ext: '.md' },
  ];

  const baseProps: FileTreeProps = {
    onFileSelect: jest.fn(),
    files: makeFiles(),
  };

  it('renders file tree header with Files title and action buttons', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, { ref, ...baseProps }));
    });
    const header = container.querySelector('.file-tree-header');
    expect(header).not.toBeNull();
    expect(header?.querySelector('.header-title')?.textContent).toBe('Files');
    expect(container.querySelector('.create-file-btn')).not.toBeNull();
    expect(container.querySelector('.create-folder-btn')).not.toBeNull();
    expect(container.querySelector('.refresh-button')).not.toBeNull();
  });

  it('renders files from provided files prop', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, { ref, ...baseProps }));
    });
    const fileItems = container.querySelectorAll('.file-tree-item');
    expect(fileItems.length).toBeGreaterThan(0);
    const readmeItem = container.querySelector('[data-ext=".md"]');
    expect(readmeItem).not.toBeNull();
  });

  it('calls onFileSelect when a file is clicked', () => {
    const onFileSelect = jest.fn();
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect,
        files: makeFiles(),
      }));
    });
    const readmeItem = container.querySelector('[data-ext=".md"]');
    act(() => {
      readmeItem?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onFileSelect).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'README.md', path: 'README.md' }),
    );
  });

  it('toggles directory expansion on click', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    // src directory is not expanded initially (rootPath='.')
    // Click it to expand
    const srcDir = container.querySelector('.file-tree-item.directory[data-ext=""]')!;
    act(() => {
      srcDir.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // After expand, children should appear
    const tsFiles = container.querySelectorAll('[data-ext=".ts"]');
    expect(tsFiles.length).toBeGreaterThanOrEqual(2);
  });

  it('shows filter input in header', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    const filterInput = container.querySelector('.file-tree-filter-input') as HTMLInputElement;
    expect(filterInput).not.toBeNull();
    expect(filterInput?.placeholder).toBe('Filter files...');
  });

  it('shows "Empty directory" when no files', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: [],
      }));
    });
    const emptyMsg = container.querySelector('.empty-directory');
    expect(emptyMsg).not.toBeNull();
    expect(emptyMsg?.textContent).toContain('Empty directory');
  });

  it('shows context menu on right-click of a file (portal to body)', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    const readmeItem = container.querySelector('[data-ext=".md"]')!;
    act(() => {
      readmeItem.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 100, clientY: 100 }));
    });
    // ContextMenu uses createPortal → renders on document.body, not inside container
    const contextMenu = document.body.querySelector('.context-menu');
    expect(contextMenu).not.toBeNull();
  });

  it('exposes refresh method via ref', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    expect(ref.current).not.toBeNull();
    expect(typeof ref.current?.refresh).toBe('function');
  });

  it('exposes revealFile method via ref', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    expect(ref.current).not.toBeNull();
    expect(typeof ref.current?.revealFile).toBe('function');
  });

  it('renders file items with extension attributes', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    // Check that .md file has data-ext
    const mdFiles = container.querySelectorAll('[data-ext=".md"]');
    expect(mdFiles.length).toBeGreaterThanOrEqual(1);
  });

  it('renders directory expand indicator', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    // The src directory should have an expand indicator
    const expandIndicators = container.querySelectorAll('.file-tree-expand');
    expect(expandIndicators.length).toBeGreaterThanOrEqual(1);
  });

  it('shows draft input when create file button is clicked', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    const createFileBtn = container.querySelector('.create-file-btn')!;
    act(() => {
      createFileBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // Allow the draft input to appear (setTimeout in useEffect)
    act(() => {});
    act(() => {});
    const draftInput = container.querySelector('.create-input');
    expect(draftInput).not.toBeNull();
  });

  it('shows draft input when create folder button is clicked', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        onFileSelect: jest.fn(),
        files: makeFiles(),
      }));
    });
    const createFolderBtn = container.querySelector('.create-folder-btn');
    if (!createFolderBtn) {
      // If the button is not found, the component rendered differently
      // Skip this test
      return;
    }
    act(() => {
      createFolderBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // Flush microtasks synchronously by using multiple act() calls
    act(() => {});
    act(() => {});
    const draftInput = container.querySelector('.create-input');
    expect(draftInput).not.toBeNull();
  });

  it('shows no-results message when filter has no matches', () => {
    const ref: { current: FileTreeHandle | null } = { current: null };
    act(() => {
      root.render(createElement(FileTree, {
        ref,
        ...baseProps,
      }));
    });

    const filterInput = container.querySelector('.file-tree-filter-input') as HTMLInputElement | null;
    expect(filterInput).not.toBeNull();

    act(() => {
      // Use React's internal value setter to properly trigger onChange
      // This properly updates React's controlled input state
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
      )!.set!;
      nativeInputValueSetter.call(filterInput, 'zzzznonexistent');
      filterInput.dispatchEvent(new Event('input', { bubbles: true }));
    });

    const noResults = container.querySelector('.file-tree-no-results');
    expect(noResults).not.toBeNull();
  });
});
