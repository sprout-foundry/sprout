// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { getAdapter } from '../../services/apiAdapter';
import TasksPage from './TasksPage';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../services/apiAdapter', () => ({
  getAdapter: vi.fn(),
}));

vi.mock('../../utils/log', () => ({
  useLog: () => ({
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    debug: vi.fn(),
  }),
}));

vi.mock('../../contexts/AppStore', () => ({
  useAppStoreSetState: () => vi.fn(),
}));

vi.mock('../../services/crossTabSync', () => ({
  getEditorSync: () => ({
    subscribe: () => () => {},
  }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let mockFetch: vi.Mock;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  mockFetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ tasks: [], count: 0 }),
  });
  getAdapter.mockReturnValue({ name: 'test-adapter', fetch: mockFetch });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderSync() {
  act(() => {
    root.render(createElement(TasksPage));
  });
}

// ---------------------------------------------------------------------------
// Tests — synchronous rendering
// ---------------------------------------------------------------------------

describe('TasksPage', () => {
  it('renders without crashing', () => {
    renderSync();
    expect(container.querySelector('.platform-page')).not.toBeNull();
  });

  it('displays page title and description', () => {
    renderSync();
    expect(container.querySelector('.platform-page-header')).not.toBeNull();
    expect(container.textContent).toContain('Tasks');
    expect(container.textContent).toContain('View and manage your background tasks');
  });

  it('shows loading state on initial render', () => {
    getAdapter.mockReturnValue({ name: 'test-adapter', fetch: mockFetch });
    renderSync();
    expect(container.querySelector('.platform-page-loading')).not.toBeNull();
    expect(container.textContent).toContain('Loading tasks...');
  });

  it('shows local mode error when no adapter is installed', () => {
    getAdapter.mockReturnValue(null);
    renderSync();

    expect(container.querySelector('.platform-page-error')).not.toBeNull();
    expect(container.textContent).toContain('Not available - running in local mode');
    expect(container.querySelector('.platform-page-loading')).toBeNull();
  });

  it('calls getAdapter to check for cloud adapter', () => {
    renderSync();
    expect(getAdapter).toHaveBeenCalled();
  });

  it('calls adapter.fetch with the correct endpoint on mount', () => {
    renderSync();
    // The fetch is initiated inside useEffect but may not have completed yet.
    // Verify the call was initiated.
    expect(mockFetch).toHaveBeenCalledWith('/api/tasks');
  });

  it('fetches tasks only once on mount', () => {
    renderSync();
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('shows loading spinner when adapter is present', () => {
    getAdapter.mockReturnValue({ name: 'test-adapter', fetch: mockFetch });
    renderSync();
    // Loading state should be visible before the fetch completes
    expect(container.querySelector('.platform-page-loading')).not.toBeNull();
  });

  it('does not show error state on initial render with adapter', () => {
    getAdapter.mockReturnValue({ name: 'test-adapter', fetch: mockFetch });
    renderSync();
    expect(container.querySelector('.platform-page-error')).toBeNull();
  });

  it('does not show empty state on initial render', () => {
    getAdapter.mockReturnValue({ name: 'test-adapter', fetch: mockFetch });
    renderSync();
    expect(container.querySelector('.platform-page-empty')).toBeNull();
  });

  it('shows New Task button in header', () => {
    renderSync();
    const buttons = container.querySelectorAll('button');
    const newTaskButton = Array.from(buttons).find((b) => b.textContent.includes('New Task'));
    expect(newTaskButton).not.toBeUndefined();
  });
});
