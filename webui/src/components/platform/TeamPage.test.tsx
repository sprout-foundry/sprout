// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import TeamPage from './TeamPage';
import { getAdapter } from '../../services/apiAdapter';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../../services/apiAdapter', () => ({
  getAdapter: jest.fn(),
}));

jest.mock('../../utils/log', () => ({
  useLog: () => ({
    error: jest.fn(),
    warn: jest.fn(),
    info: jest.fn(),
    debug: jest.fn(),
  }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let mockFetch: jest.Mock;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
  mockFetch = jest.fn().mockResolvedValue({
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
    expect(mockFetch).toHaveBeenCalledWith('/api/foundry/team');
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
