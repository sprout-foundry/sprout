// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { getAdapter } from '../../services/apiAdapter';
import TeamPage from './TeamPage';

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
    json: () => Promise.resolve({ name: 'Team', members: [], invites: [] }),
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
    root.render(createElement(TeamPage));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TeamPage', () => {
  it('renders without crashing', () => {
    renderSync();
    expect(container.querySelector('.platform-page')).not.toBeNull();
  });

  it('displays page title and description', () => {
    renderSync();
    expect(container.querySelector('.platform-page-header')).not.toBeNull();
    expect(container.textContent).toContain('Team');
    expect(container.textContent).toContain('Manage team members');
  });

  it('shows loading state on initial render', () => {
    renderSync();
    expect(container.querySelector('.platform-page-loading')).not.toBeNull();
    expect(container.textContent).toContain('Loading team information...');
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
    expect(mockFetch).toHaveBeenCalledWith('/team/members');
  });

  it('fetches team data only once on mount', () => {
    renderSync();
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('does not show error state on initial render with adapter', () => {
    renderSync();
    expect(container.querySelector('.platform-page-error')).toBeNull();
  });

  it('does not show empty state on initial render', () => {
    renderSync();
    expect(container.querySelector('.platform-page-empty')).toBeNull();
  });
});
