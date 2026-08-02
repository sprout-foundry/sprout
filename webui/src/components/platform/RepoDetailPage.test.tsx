// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import RepoDetailPage from './RepoDetailPage';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => ({ name: 'test', fetch: (...args: any[]) => mockFetch(...args) })),
}));

vi.mock('../../services/gitClient', () => ({
  get gitClient() {
    return {
      exists: (...a: any[]) => mockExists(...a),
      clone: (...a: any[]) => mockClone(...a),
      push: (...a: any[]) => mockPush(...a),
      pull: (...a: any[]) => mockPull(...a),
      status: (...a: any[]) => mockStatus(...a),
      checkout: (...a: any[]) => mockCheckout(...a),
      writeFile: (...a: any[]) => mockWriteFile(...a),
      mkdir: (...a: any[]) => mockMkdir(...a),
    };
  },
}));

vi.mock('../../services/repoVfsBridge', () => ({
  syncRepoToWasmVfs: (...a: any[]) => mockSyncRepoToWasmVfs(...a),
}));

vi.mock('../../services/repoDownload', () => ({
  downloadRepoAsZip: (...a: any[]) => mockDownloadRepoAsZip(...a),
}));

vi.mock('../../utils/log', () => ({
  useLog: () => ({
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    debug: vi.fn(),
  }),
}));

const mockFetch = vi.fn();
const mockExists = vi.fn();
const mockClone = vi.fn();
const mockPush = vi.fn();
const mockPull = vi.fn();
const mockStatus = vi.fn();
const mockCheckout = vi.fn();
const mockWriteFile = vi.fn();
const mockMkdir = vi.fn();
const mockSyncRepoToWasmVfs = vi.fn();
const mockDownloadRepoAsZip = vi.fn();

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

  // Default: repo already exists locally (skip clone)
  mockExists.mockResolvedValue(true);

  // Default API responses
  mockFetch.mockImplementation((url: string) => {
    if (url.includes('/branches')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([{ name: 'main' }, { name: 'dev' }]),
      });
    }
    if (url.includes('/commits')) {
      return Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve([
            {
              sha: 'abcdef0123456789',
              commit: { message: 'Initial commit', author: { name: 'Alice', date: '2024-01-01' } },
            },
          ]),
      });
    }
    if (url.includes('/pulls')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([{ number: 1, title: 'Fix bug', state: 'open' }]),
      });
    }
    // Base repo info
    return Promise.resolve({
      ok: true,
      json: () =>
        Promise.resolve({
          id: 1,
          name: 'my-repo',
          full_name: 'owner/my-repo',
          html_url: 'https://github.com/owner/my-repo',
          description: 'A test repo',
          default_branch: 'main',
          language: 'TypeScript',
        }),
    });
  });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderSync(props: any) {
  const defaultProps = {
    repoOwner: 'owner',
    repoName: 'my-repo',
    onBack: vi.fn(),
  };
  act(() => {
    root.render(createElement(RepoDetailPage, { ...defaultProps, ...props }));
  });
}

async function flushAsync() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RepoDetailPage', () => {
  it('renders loading state initially', () => {
    mockFetch.mockReturnValue(new Promise(() => {})); // never resolves
    renderSync({});
    expect(container.querySelector('.platform-loading')).toBeTruthy();
  });

  it('renders repo header with full_name after load', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('owner/my-repo');
  });

  it('renders description when present', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('A test repo');
  });

  it('shows branches section when branches exist', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Branches');
    expect(container.textContent).toContain('main');
    expect(container.textContent).toContain('dev');
  });

  it('shows recent commits', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Recent Commits');
    expect(container.textContent).toContain('Initial commit');
    expect(container.textContent).toContain('Alice');
  });

  it('shows pull requests section', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Pull Requests');
    expect(container.textContent).toContain('Fix bug');
  });

  it('calls onBack when back button clicked', async () => {
    const onBack = vi.fn();
    renderSync({ onBack });
    await flushAsync();

    const backBtn = container.querySelector('button.btn-ghost') as HTMLElement;
    expect(backBtn).toBeTruthy();
    act(() => {
      backBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onBack).toHaveBeenCalled();
  });

  it('shows push button when clone is ready', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Push');
  });

  it('shows pull button when clone is ready', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Pull');
  });

  it('shows ZIP download button when clone is ready', async () => {
    renderSync({});
    await flushAsync();
    const zipBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('ZIP'),
    );
    expect(zipBtn).toBeTruthy();
  });

  it('triggers gitClient.push when Push button clicked', async () => {
    localStorage.setItem('github_pat', 'ghp_test_token');
    renderSync({});
    await flushAsync();

    const pushBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Push'),
    ) as HTMLElement;

    await act(async () => {
      pushBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(mockPush).toHaveBeenCalled();
    localStorage.removeItem('github_pat');
  });

  it('triggers downloadRepoAsZip when ZIP button clicked', async () => {
    mockDownloadRepoAsZip.mockResolvedValue(undefined);
    renderSync({});
    await flushAsync();

    const zipBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('ZIP'),
    ) as HTMLElement;

    await act(async () => {
      zipBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(mockDownloadRepoAsZip).toHaveBeenCalledWith('/repos/owner/my-repo', 'owner-my-repo');
  });

  it('shows language badge when repo has language', async () => {
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('TypeScript');
  });

  it('renders repo file tree when clone is ready', async () => {
    renderSync({});
    await flushAsync();
    // The Files tab should be active by default
    expect(container.textContent).toContain('Files');
  });

  it('shows error message when repo fetch fails', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'));
    renderSync({});
    await flushAsync();
    expect(container.textContent).toContain('Failed to load repo details');
  });

  it('shows needs-auth state when clone throws auth error', async () => {
    mockExists.mockResolvedValue(false);
    mockClone.mockRejectedValue(new Error('HTTP 401: Unauthorized'));
    renderSync({});
    await flushAsync();

    expect(container.textContent).toContain('private');
  });
});
