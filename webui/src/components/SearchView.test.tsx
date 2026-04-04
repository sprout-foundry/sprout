import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { copyToClipboard } from '../utils/clipboard';
import SearchView from './SearchView';
import { ApiService } from '../services/api';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../utils/clipboard', () => ({
  copyToClipboard: jest.fn().mockResolvedValue(undefined),
}));

jest.mock('../services/clientSession', () => ({
  clientFetch: jest.fn(),
}));

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: jest.fn(),
  },
}));

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MOCK_SEARCH_RESPONSE = {
  results: [
    {
      file: './src/components/App.tsx',
      matches: [
        {
          line_number: 42,
          line: '  const handleClick = () => {',
          column_start: 10,
          column_end: 20,
          context_before: ['function App() {'],
          context_after: ['    return null;'],
        },
      ],
      match_count: 1,
    },
    {
      file: './src/utils/helpers.ts',
      matches: [
        {
          line_number: 10,
          line: '  export function formatName() {',
          column_start: 20,
          column_end: 30,
          context_before: [],
          context_after: ['  }'],
        },
      ],
      match_count: 1,
    },
  ],
  total_matches: 2,
  total_files: 2,
  truncated: false,
  query: 'handleClick',
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

// Mock requestAnimationFrame so close-listener effect fires synchronously.
// jest does not auto-flush rAF; without this, close listeners never attach.
let rafId = 0;
const syncRAF = ((cb: FrameRequestCallback) => {
  rafId += 1;
  cb(Date.now());
  return rafId;
}) as typeof requestAnimationFrame;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

const mockSearchFn = jest.fn().mockResolvedValue(MOCK_SEARCH_RESPONSE);

beforeEach(() => {
  jest.useFakeTimers();
  // Re-mock rAF afterjest.useFakeTimers overrides it
  global.requestAnimationFrame = syncRAF;
  global.cancelAnimationFrame = jest.fn();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);

  // Reset mockReturnValue on getInstance, but do NOT use clearAllMocks
  // because it wipes mockResolvedValue from mockSearchFn.
  (ApiService.getInstance as jest.Mock).mockClear();
  (ApiService.getInstance as jest.Mock).mockReturnValue({
    search: mockSearchFn,
  });

  // Clear call history on specific mocks (preserve implementations)
  mockSearchFn.mockClear();
  mockSearchFn.mockResolvedValue(MOCK_SEARCH_RESPONSE);
  (copyToClipboard as jest.Mock).mockClear();
  defaultOnFileClick.mockClear();
});

afterEach(() => {
  act(() => {
    if (root) root.unmount();
  });
  if (container) container.remove();
  // Clean up any portal containers leftover
  document.querySelectorAll('.context-menu').forEach((el) => el.remove());
  jest.useRealTimers();
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

const defaultOnFileClick = jest.fn();

/** Render SearchView and trigger debounced search. */
async function renderSearch(props: { onFileClick?: jest.Mock } = {}) {
  const onFileClick = props.onFileClick || defaultOnFileClick;

  await act(async () => {
    root!.render(createElement(SearchView, { onFileClick }));
  });

  // Type a search query
  const input = container!.querySelector<HTMLInputElement>('.search-text-input');
  expect(input).not.toBeNull();

  // Use React's internal value setter to trigger onChange
  const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  await act(() => {
    nativeInputValueSetter.call(input, 'handleClick');
    input!.dispatchEvent(new Event('input', { bubbles: true }));
  });

  // Advance past debounce delay
  await act(async () => {
    jest.advanceTimersByTime(400);
  });
  await flushPromises();

  // Auto-expand all file groups so match rows are rendered
  const headers = container!.querySelectorAll('.search-file-header');
  for (const header of Array.from(headers)) {
    await act(() => {
      header.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
  }
}

/** Fire a contextmenu event on a search match hit row. */
function fireContextMenuOnMatch() {
  const hitRow = document.querySelector('.search-match-row--hit');
  if (!hitRow) throw new Error('Could not find match hit row');
  hitRow.dispatchEvent(
    new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 100,
      clientY: 200,
    }),
  );
}

/** Fire a contextmenu event on a file header. */
function fireContextMenuOnFileHeader() {
  const header = document.querySelector('.search-file-header');
  if (!header) throw new Error('Could not find file header');
  header.dispatchEvent(
    new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 100,
      clientY: 200,
    }),
  );
}

/** Get all context menu item elements. */
function getContextItems() {
  return Array.from(document.querySelectorAll('.context-menu-item'));
}

/** Get the text content of all context menu items (trimmed). */
function getContextMenuTexts() {
  return getContextItems().map((item) => (item.textContent || '').trim());
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SearchView context menu - match row', () => {
  it('appears on right-click of a match row', async () => {
    await renderSearch();

    expect(document.querySelector('.context-menu')).toBeNull();

    fireContextMenuOnMatch();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();
  });

  it('shows Copy line text, Open in editor, Copy file path, Exclude folder for match rows', async () => {
    await renderSearch();

    fireContextMenuOnMatch();
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts.some((t) => t.includes('Copy line text'))).toBe(true);
    expect(texts.some((t) => t.includes('Open in editor'))).toBe(true);
    expect(texts.some((t) => t.includes('Copy file path'))).toBe(true);
    expect(texts.some((t) => t.includes('Exclude folder'))).toBe(true);
  });
});

describe('SearchView context menu - file header', () => {
  it('appears on right-click of a file header', async () => {
    await renderSearch();

    expect(document.querySelector('.context-menu')).toBeNull();

    fireContextMenuOnFileHeader();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();
  });

  it('does NOT show Copy line text or Open in editor on file header', async () => {
    await renderSearch();

    fireContextMenuOnFileHeader();
    await flushPromises();

    const texts = getContextMenuTexts();
    expect(texts.some((t) => t.includes('Copy line text'))).toBe(false);
    expect(texts.some((t) => t.includes('Open in editor'))).toBe(false);
    expect(texts.some((t) => t.includes('Copy file path'))).toBe(true);
    expect(texts.some((t) => t.includes('Exclude file'))).toBe(true);
  });
});

describe('SearchView context menu - clipboard and editor actions', () => {
  it('"Copy line text" copies the match text to clipboard', async () => {
    await renderSearch();

    fireContextMenuOnMatch();
    await flushPromises();

    const copyBtn = getContextItems().find((item) => item.textContent?.includes('Copy line text'));
    expect(copyBtn).toBeDefined();

    await act(async () => {
      (copyBtn as HTMLElement).click();
    });
    await flushPromises();

    expect(copyToClipboard).toHaveBeenCalledWith('  const handleClick = () => {');
  });

  it('"Copy file path" copies the relative path to clipboard', async () => {
    await renderSearch();

    fireContextMenuOnMatch();
    await flushPromises();

    const copyBtn = getContextItems().find((item) => item.textContent?.includes('Copy file path'));
    expect(copyBtn).toBeDefined();

    await act(async () => {
      (copyBtn as HTMLElement).click();
    });
    await flushPromises();

    expect(copyToClipboard).toHaveBeenCalledWith('src/components/App.tsx');
  });

  it('"Open in editor" calls onFileClick with correct path and line number', async () => {
    const onFileClick = jest.fn();
    await renderSearch({ onFileClick });

    fireContextMenuOnMatch();
    await flushPromises();

    const openBtn = getContextItems().find((item) => item.textContent?.includes('Open in editor'));
    expect(openBtn).toBeDefined();

    await act(async () => {
      (openBtn as HTMLElement).click();
    });
    await flushPromises();

    expect(onFileClick).toHaveBeenCalledTimes(1);
    expect(onFileClick).toHaveBeenCalledWith('./src/components/App.tsx', 42);
  });
});

describe('SearchView context menu - exclude functionality', () => {
  it('"Exclude folder from search" filters out the excluded folder from results', async () => {
    await renderSearch();

    // Should initially have 2 file groups
    expect(document.querySelectorAll('.search-file-group').length).toBe(2);

    fireContextMenuOnMatch();
    await flushPromises();

    const excludeBtn = getContextItems().find((item) => item.textContent?.includes('Exclude folder'));
    expect(excludeBtn).toBeDefined();

    await act(async () => {
      (excludeBtn as HTMLElement).click();
    });
    await flushPromises();

    // Menu should close
    expect(document.querySelector('.context-menu')).toBeNull();

    // Exclude indicator should appear
    expect(document.querySelector('.search-exclude-indicator')).not.toBeNull();
    expect(document.querySelector('.search-exclude-patterns')!.textContent).toContain('src/components/');

    // Now only 1 file group should be visible (utils not excluded)
    expect(document.querySelectorAll('.search-file-group').length).toBe(1);
  });

  it('"Exclude file from search" from file header filters out the file', async () => {
    await renderSearch();

    expect(document.querySelectorAll('.search-file-group').length).toBe(2);

    fireContextMenuOnFileHeader();
    await flushPromises();

    const excludeBtn = getContextItems().find((item) => item.textContent?.includes('Exclude file'));
    expect(excludeBtn).toBeDefined();

    await act(async () => {
      (excludeBtn as HTMLElement).click();
    });
    await flushPromises();

    expect(document.querySelector('.search-exclude-indicator')).not.toBeNull();
    expect(document.querySelector('.search-exclude-patterns')!.textContent).toContain('src/components/App.tsx');

    // Only 1 file group should remain
    expect(document.querySelectorAll('.search-file-group').length).toBe(1);
  });

  it('non-excluded files show exclude action as enabled after another file is excluded', async () => {
    await renderSearch();

    // Exclude from file header (exclude the file, not folder)
    fireContextMenuOnFileHeader();
    await flushPromises();
    const excludeBtn = getContextItems().find((item) => item.textContent?.includes('Exclude file'));
    await act(async () => {
      (excludeBtn as HTMLElement).click();
    });
    await flushPromises();

    // Re-open context menu on the remaining file header — its exclude
    // action should NOT be disabled since it has not been excluded.
    const remainingHeaders = document.querySelectorAll('.search-file-header');
    expect(remainingHeaders.length).toBe(1);
    remainingHeaders[0].dispatchEvent(
      new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 100,
        clientY: 200,
      }),
    );
    await flushPromises();

    // The remaining file is NOT excluded, so the button should NOT be disabled
    const excludeBtn2 = getContextItems().find((item) => item.textContent?.includes('Exclude file'));
    expect(excludeBtn2).toBeDefined();
    expect((excludeBtn2 as HTMLElement).classList.contains('disabled')).toBe(false);
  });

  it('clear exclude patterns removes the exclude filter', async () => {
    await renderSearch();

    // Exclude first
    fireContextMenuOnMatch();
    await flushPromises();
    const excludeBtn = getContextItems().find((item) => item.textContent?.includes('Exclude folder'));
    await act(async () => {
      (excludeBtn as HTMLElement).click();
    });
    await flushPromises();

    // Confirm filtered
    expect(document.querySelectorAll('.search-file-group').length).toBe(1);

    // Click clear
    const clearBtn = document.querySelector('.search-exclude-clear');
    expect(clearBtn).not.toBeNull();

    await act(async () => {
      (clearBtn as HTMLElement).click();
    });
    await flushPromises();

    // Exclude indicator should be gone
    expect(document.querySelector('.search-exclude-indicator')).toBeNull();

    // Both files should be back
    expect(document.querySelectorAll('.search-file-group').length).toBe(2);
  });
});

describe('SearchView context menu - dismissal', () => {
  it('closes on click outside', async () => {
    await renderSearch();

    fireContextMenuOnMatch();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();

    // Simulate mousedown on body (outside the menu)
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, clientX: 0, clientY: 0 }));
    });
    await flushPromises();

    expect(document.querySelector('.context-menu')).toBeNull();
  });

  it('closes on Escape key', async () => {
    await renderSearch();

    fireContextMenuOnMatch();
    await flushPromises();

    expect(document.querySelector('.context-menu')).not.toBeNull();

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    await flushPromises();

    expect(document.querySelector('.context-menu')).toBeNull();
  });
});
