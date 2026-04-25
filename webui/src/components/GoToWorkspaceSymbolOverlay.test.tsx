// @ts-nocheck

import ReactDOM from 'react-dom';
import { act } from 'react-dom/test-utils';
import GoToWorkspaceSymbolOverlay from './GoToWorkspaceSymbolOverlay';

// ── Mocks ────────────────────────────────────────────────────────────────

const defaultMockResponse = {
  message: 'ok',
  files: [
    {
      file: 'src/foo.ts',
      symbols: [
        { name: 'myFunc', kind: 'func', line: 10 },
        { name: 'MyClass', kind: 'class', line: 20 },
      ],
    },
  ],
  total: 2,
};

const mockGetWorkspaceSymbols = jest.fn().mockResolvedValue(defaultMockResponse);

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      getWorkspaceSymbols: mockGetWorkspaceSymbols,
    }),
  },
}));

jest.mock('lucide-react', () => ({
  Loader2: () => <span data-testid="loader">Loading</span>,
}));

// ── JSDOM polyfills and compat ───────────────────────────────────────────

const originalError = console.error;
beforeAll(() => {
  Element.prototype.scrollIntoView = jest.fn();
  Element.prototype.requestAnimationFrame = (cb) => setTimeout(cb, 0);
  console.error = (...args: any[]) => {
    if (typeof args[0] === 'string' && args[0].includes('ReactDOM.render is no longer supported')) return;
    originalError.call(console, ...args);
  };
});
afterAll(() => {
  console.error = originalError;
});

beforeEach(() => {
  mockGetWorkspaceSymbols.mockClear();
  mockGetWorkspaceSymbols.mockResolvedValue(defaultMockResponse);
});

// ── Helpers ──────────────────────────────────────────────────────────────

function renderOverlay(props: {
  visible?: boolean;
  onSelectSymbol?: (filePath: string, line?: number) => void;
  onClose?: () => void;
}) {
  const container = document.createElement('div');
  document.body.appendChild(container);

  const {
    visible = true,
    onSelectSymbol = jest.fn(),
    onClose = jest.fn(),
  } = props;

  act(() => {
    ReactDOM.render(
      <GoToWorkspaceSymbolOverlay
        visible={visible}
        onSelectSymbol={onSelectSymbol}
        onClose={onClose}
      />,
      container,
    );
  });

  return {
    container,
    el: container,
    unmount: () =>
      act(() => {
        ReactDOM.unmountComponentAtNode(container);
        document.body.removeChild(container);
      }),
  };
}

/**
 * Wait for all pending promise microtasks to flush.
 * Uses a double tick since the component's async fetchSymbols
 * resolves in a microtask, then React's setState update is also microtasked.
 */
async function waitForAsync() {
  await act(async () => {
    jest.runAllTimers();
    await Promise.resolve();
    await Promise.resolve();
  });
}

// ── Tests ────────────────────────────────────────────────────────────────

describe('GoToWorkspaceSymbolOverlay', () => {
  // 1. Renders nothing when visible=false
  it('renders nothing when visible=false', () => {
    const view = renderOverlay({ visible: false });
    expect(view.container.innerHTML).toBe('');
    view.unmount();
  });

  // 2. Renders the overlay when visible=true with input and placeholder text
  it('renders the overlay when visible=true with input and placeholder', () => {
    const view = renderOverlay({ visible: true });
    expect(view.el.querySelector('.goto-workspace-symbol-overlay')).not.toBeNull();
    const input = view.el.querySelector('.goto-workspace-symbol-input');
    expect(input).not.toBeNull();
    expect(input.getAttribute('placeholder')).toBe('Go to Symbol in Workspace');
    view.unmount();
  });

  // 3. Shows loading state when fetching
  it('shows loading state when fetching', async () => {
    // Make the fetch hang (never resolves) so loading stays true
    const pendingPromise = new Promise(() => {});
    mockGetWorkspaceSymbols.mockReturnValueOnce(pendingPromise);

    const view = renderOverlay({ visible: true });

    // The component immediately calls fetchSymbols('') on mount
    expect(view.el.querySelector('[data-testid="loader"]')).not.toBeNull();
    const loadingEl = view.el.querySelector('.goto-workspace-symbol-loading');
    expect(loadingEl).not.toBeNull();
    expect(loadingEl.textContent).toContain('Loading symbols...');

    view.unmount();
  });

  // 4. Shows error state when fetch fails
  it('shows error state when fetch fails', async () => {
    mockGetWorkspaceSymbols.mockRejectedValueOnce(new Error('Network error'));

    const view = renderOverlay({ visible: true });
    await waitForAsync();

    const errorEl = view.el.querySelector('.goto-workspace-symbol-empty');
    expect(errorEl).not.toBeNull();
    expect(errorEl.textContent).toBe('Failed to fetch symbols');

    view.unmount();
  });

  // 5. Shows empty state when no results
  it('shows empty state when no results', async () => {
    mockGetWorkspaceSymbols.mockResolvedValueOnce({
      message: 'ok',
      files: [],
      total: 0,
    });

    const view = renderOverlay({ visible: true });
    await waitForAsync();

    const emptyEl = view.el.querySelector('.goto-workspace-symbol-empty');
    expect(emptyEl).not.toBeNull();
    expect(emptyEl.textContent).toBe('No symbols in workspace');

    view.unmount();
  });

  // 6. Calls onSelectSymbol with correct filePath and line when a symbol item is clicked
  it('calls onSelectSymbol with correct filePath and line when item is clicked', async () => {
    const onSelectSymbol = jest.fn();

    const view = renderOverlay({ visible: true, onSelectSymbol });
    await waitForAsync();

    // Click on the myFunc symbol item (first item)
    const items = view.el.querySelectorAll('.goto-workspace-symbol-item');
    expect(items.length).toBe(2);

    act(() => {
      items[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSelectSymbol).toHaveBeenCalledTimes(1);
    expect(onSelectSymbol).toHaveBeenCalledWith('src/foo.ts', 10);

    view.unmount();
  });

  // 7. Calls onClose when Escape is pressed
  it('calls onClose when Escape is pressed', async () => {
    const onClose = jest.fn();

    const view = renderOverlay({ visible: true, onClose });
    await waitForAsync();

    const input = view.el.querySelector('.goto-workspace-symbol-input');
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
      }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);

    view.unmount();
  });

  // 8. Debounces API calls (only one call after typing multiple characters)
  it('debounces API calls when typing multiple characters', async () => {
    jest.useFakeTimers();

    const view = renderOverlay({ visible: true });

    // Wait for initial mount fetch (empty query)
    await act(async () => {
      jest.advanceTimersByTime(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    // Initial call with empty query
    expect(mockGetWorkspaceSymbols).toHaveBeenCalledTimes(1);
    mockGetWorkspaceSymbols.mockClear();

    const input = view.el.querySelector('.goto-workspace-symbol-input');

    // Simulate typing 'abc' character by character using nativeInputValueSetter
    // to bypass React's controlled input and trigger onChange
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;

    act(() => {
      nativeInputValueSetter.call(input, 'a');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    act(() => {
      nativeInputValueSetter.call(input, 'ab');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    act(() => {
      nativeInputValueSetter.call(input, 'abc');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // Before debounce fires: should NOT have called again yet
    expect(mockGetWorkspaceSymbols).not.toHaveBeenCalled();

    // Advance timers past the debounce delay (300ms)
    await act(async () => {
      jest.advanceTimersByTime(350);
      await Promise.resolve();
      await Promise.resolve();
    });

    // After debounce: exactly one call with the final query
    expect(mockGetWorkspaceSymbols).toHaveBeenCalledTimes(1);
    expect(mockGetWorkspaceSymbols).toHaveBeenCalledWith('abc');

    view.unmount();
    jest.useRealTimers();
  });
});
