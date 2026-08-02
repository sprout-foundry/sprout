// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { RepoFileTree } from './RepoFileTree';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../services/gitClient', () => ({
  get gitClient() {
    return {
      listDir: mockListDir,
      readFile: mockReadFile,
    };
  },
  FileEntry: {},
}));

const mockListDir = vi.fn();
const mockReadFile = vi.fn();

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderSync(props: any) {
  const defaultProps = {
    dir: '/repos/owner/repo',
    onFileClick: vi.fn(),
  };
  act(() => {
    root.render(createElement(RepoFileTree, { ...defaultProps, ...props }));
  });
}

// Flush microtasks (for async useEffect callbacks)
async function flushAsync() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MOCK_FILES: any[] = [
  { name: 'README.md', path: '/README.md', type: 'file', size: 200 },
  { name: 'src', path: '/src', type: 'dir', size: 0 },
];

const MOCK_DIR_CHILDREN: any[] = [{ name: 'index.ts', path: '/src/index.ts', type: 'file', size: 500 }];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RepoFileTree', () => {
  it('renders loading state initially', () => {
    mockListDir.mockReturnValue(new Promise(() => {})); // never resolves
    renderSync({});
    expect(container.textContent).toContain('Loading');
  });

  it('renders file entries after load', async () => {
    mockListDir.mockResolvedValue(MOCK_FILES);
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('README.md');
    expect(container.textContent).toContain('src');
  });

  it('calls onFileClick with filepath and content when a file row is clicked', async () => {
    mockListDir.mockResolvedValue(MOCK_FILES);
    mockReadFile.mockResolvedValue('file content here');
    const onFileClick = vi.fn();
    renderSync({ onFileClick });
    await flushAsync();

    // Click the README.md file row
    const fileRow = Array.from(container.querySelectorAll('.tree-node-row')).find((el) =>
      (el as HTMLElement).textContent?.includes('README.md'),
    ) as HTMLElement;
    expect(fileRow).toBeTruthy();
    act(() => {
      fileRow.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    expect(mockReadFile).toHaveBeenCalledWith('/repos/owner/repo', '/README.md');
    expect(onFileClick).toHaveBeenCalledWith('/README.md', 'file content here');
  });

  it('shows error state when listDir fails', async () => {
    mockListDir.mockRejectedValue(new Error('Permission denied'));
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Permission denied');
  });

  it('shows empty state message when listDir returns empty array', async () => {
    mockListDir.mockResolvedValue([]);
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('No files in this repo');
  });

  it('shows create buttons when onCreateFile/onCreateFolder are provided', async () => {
    mockListDir.mockResolvedValue([]);
    renderSync({ onCreateFile: vi.fn(), onCreateFolder: vi.fn() });
    await flushAsync();
    const buttons = container.querySelectorAll('.tree-toolbar-btn');
    expect(buttons.length).toBeGreaterThanOrEqual(2);
    expect(container.textContent).toContain('+ File');
    expect(container.textContent).toContain('+ Folder');
  });

  it('does NOT show create buttons when onCreateFile/onCreateFolder are absent', async () => {
    mockListDir.mockResolvedValue(MOCK_FILES);
    renderSync({});
    await flushAsync();
    expect(container.textContent).not.toContain('+ File');
    expect(container.textContent).not.toContain('+ Folder');
  });

  it('expands directory and lazy-loads children on click', async () => {
    // Root listing returns a dir; second listDir call returns children
    mockListDir.mockResolvedValueOnce(MOCK_FILES).mockResolvedValueOnce(MOCK_DIR_CHILDREN);
    renderSync({});
    await flushAsync();

    // Find the 'src' directory row
    const dirRow = Array.from(container.querySelectorAll('.tree-node-row')).find((el) =>
      (el as HTMLElement).textContent?.includes('src'),
    ) as HTMLElement;
    expect(dirRow).toBeTruthy();

    act(() => {
      dirRow.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    // Children should now be rendered
    expect(container.textContent).toContain('index.ts');
  });

  it('shows file size for files with size > 0', async () => {
    mockListDir.mockResolvedValue([{ name: 'big.ts', path: '/big.ts', type: 'file', size: 2048 }]);
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('2.0KB');
  });

  it('shows error for files exceeding MAX_FILE_SIZE (1MB)', async () => {
    mockListDir.mockResolvedValue([{ name: 'huge.bin', path: '/huge.bin', type: 'file', size: 2_000_000 }]);
    renderSync({});
    await flushAsync();

    const fileRow = Array.from(container.querySelectorAll('.tree-node-row')).find((el) =>
      (el as HTMLElement).textContent?.includes('huge.bin'),
    ) as HTMLElement;

    act(() => {
      fileRow.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    expect(container.textContent).toContain('File too large');
    expect(mockReadFile).not.toHaveBeenCalled();
  });

  it('shows create input when "+ File" button is clicked', async () => {
    const onCreateFile = vi.fn().mockResolvedValue(undefined);
    mockListDir.mockResolvedValue([]);
    renderSync({ onCreateFile });
    await flushAsync();

    const fileBtn = Array.from(container.querySelectorAll('.tree-toolbar-btn')).find((b) =>
      (b as HTMLElement).textContent?.includes('+ File'),
    ) as HTMLElement;
    act(() => {
      fileBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.tree-create-input')).toBeTruthy();
  });

  it('calls onCreateFile and refreshes tree when create input is submitted', async () => {
    const onCreateFile = vi.fn().mockResolvedValue(undefined);
    mockListDir.mockResolvedValue([]);
    renderSync({ onCreateFile });
    await flushAsync();

    // Click "+ File"
    const fileBtn = Array.from(container.querySelectorAll('.tree-toolbar-btn')).find((b) =>
      (b as HTMLElement).textContent?.includes('+ File'),
    ) as HTMLElement;
    act(() => {
      fileBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Type a filename using native input value setter (React controlled input)
    const input = container.querySelector('.tree-create-input') as HTMLInputElement;
    expect(input).toBeTruthy();
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    act(() => {
      nativeInputValueSetter.call(input, 'newfile.ts');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Click "Create" button
    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => (b as HTMLElement).textContent?.trim() === 'Create',
    ) as HTMLElement;
    expect(createBtn).toBeTruthy();
    await act(async () => {
      createBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(onCreateFile).toHaveBeenCalledWith('newfile.ts');
  });
});
