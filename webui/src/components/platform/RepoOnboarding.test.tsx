// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import RepoOnboarding from './RepoOnboarding';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => ({ name: 'test', fetch: (...a: any[]) => mockFetch(...a) })),
}));

vi.mock('../../services/gitClient', () => ({
  get gitClient() {
    return {
      clone: (...a: any[]) => mockClone(...a),
      mkdir: (...a: any[]) => mockMkdir(...a),
      writeFile: (...a: any[]) => mockWriteFile(...a),
      getFs: () => ({ promises: mockPfs }),
    };
  },
}));

vi.mock('isomorphic-git', () => ({
  default: {
    init: vi.fn().mockResolvedValue(undefined),
    add: vi.fn().mockResolvedValue(undefined),
    commit: vi.fn().mockResolvedValue('abc1234567890'),
  },
}));

const mockFetch = vi.fn();
const mockClone = vi.fn();
const mockMkdir = vi.fn();
const mockWriteFile = vi.fn();

const mockPfs = {
  mkdir: vi.fn().mockResolvedValue(undefined),
  writeFile: vi.fn().mockResolvedValue(undefined),
};

// Mock localStorage
const mockLocalStorage = {
  store: {} as Record<string, string>,
  getItem(key: string) {
    return mockLocalStorage.store[key] ?? null;
  },
  setItem(key: string, value: string) {
    mockLocalStorage.store[key] = value;
  },
  removeItem(key: string) {
    delete mockLocalStorage.store[key];
  },
  clear() {
    mockLocalStorage.store = {};
  },
  get length() {
    return Object.keys(mockLocalStorage.store).length;
  },
  key(index: number) {
    return Object.keys(mockLocalStorage.store)[index];
  },
};

Object.defineProperty(globalThis, 'localStorage', {
  value: mockLocalStorage,
  writable: true,
  configurable: true,
});

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
  mockLocalStorage.clear();
  mockClone.mockResolvedValue(undefined);
  mockMkdir.mockResolvedValue(undefined);
  mockWriteFile.mockResolvedValue(undefined);
  mockPfs.mkdir.mockResolvedValue(undefined);
  mockPfs.writeFile.mockResolvedValue(undefined);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderSync(props: any) {
  const defaultProps = {
    onRepoSelected: vi.fn(),
  };
  act(() => {
    root.render(createElement(RepoOnboarding, { ...defaultProps, ...props }));
  });
}

async function flushAsync() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50));
  });
}

// React controlled inputs need native value setter to trigger onChange
function setReactInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

// ---------------------------------------------------------------------------
// Tests — Rendering
// ---------------------------------------------------------------------------

describe('RepoOnboarding — rendering', () => {
  it('renders hero section with "Get Started" heading', () => {
    renderSync({});
    expect(container.textContent).toContain('Get Started');
  });

  it('renders description text about repositories', () => {
    renderSync({});
    expect(container.textContent).toContain('Clone an existing repository');
  });

  it('renders Import URL and New Repo tabs (no PAT)', () => {
    renderSync({});
    expect(container.textContent).toContain('Import URL');
    expect(container.textContent).toContain('New Repo');
  });

  it('renders Select Repo tab when PAT exists in localStorage', () => {
    localStorage.setItem('github_pat', 'ghp_testtoken');
    renderSync({});
    expect(container.textContent).toContain('Select Repo');
  });

  it('does NOT render Select Repo tab when no PAT', () => {
    renderSync({});
    expect(container.textContent).not.toContain('Select Repo');
  });
});

// ---------------------------------------------------------------------------
// Tests — URL Import
// ---------------------------------------------------------------------------

describe('RepoOnboarding — URL import', () => {
  it('shows URL input with placeholder', () => {
    renderSync({});
    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    expect(input).toBeTruthy();
    expect(input.placeholder).toContain('github.com');
  });

  it('parses https://github.com/owner/repo URL format and clones', async () => {
    const onRepoSelected = vi.fn();
    renderSync({ onRepoSelected });

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'https://github.com/octocat/hello-world');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 100));
    });

    expect(mockClone).toHaveBeenCalledWith(
      'https://github.com/octocat/hello-world',
      '/repos/octocat/hello-world',
      expect.objectContaining({ depth: 1, branch: 'main' }),
    );
    expect(onRepoSelected).toHaveBeenCalledWith('octocat', 'hello-world');
  });

  it('parses git@github.com:owner/repo SSH URL format', async () => {
    const onRepoSelected = vi.fn();
    renderSync({ onRepoSelected });

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'git@github.com:octocat/hello-world');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 100));
    });

    expect(mockClone).toHaveBeenCalledWith(
      'https://github.com/octocat/hello-world',
      '/repos/octocat/hello-world',
      expect.any(Object),
    );
    expect(onRepoSelected).toHaveBeenCalledWith('octocat', 'hello-world');
  });

  it('parses owner/repo shorthand format', async () => {
    const onRepoSelected = vi.fn();
    renderSync({ onRepoSelected });

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'octocat/hello-world');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 100));
    });

    expect(mockClone).toHaveBeenCalledWith(
      'https://github.com/octocat/hello-world',
      '/repos/octocat/hello-world',
      expect.any(Object),
    );
  });

  it('shows error for invalid URL', async () => {
    renderSync({});

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'not-a-valid-url');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(container.textContent).toContain('Invalid GitHub URL');
    expect(mockClone).not.toHaveBeenCalled();
  });

  it('shows auth error when clone fails with 401', async () => {
    mockClone.mockRejectedValue(new Error('HTTP 401: Unauthorized'));
    renderSync({});

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'https://github.com/private/repo');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(container.textContent).toContain('token');
  });

  it('shows generic error when clone fails for other reasons', async () => {
    mockClone.mockRejectedValue(new Error('Network timeout'));
    renderSync({});

    const input = container.querySelector('input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'https://github.com/owner/repo');
    });

    const cloneBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Clone'),
    ) as HTMLElement;

    await act(async () => {
      cloneBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(container.textContent).toContain('Network timeout');
  });
});

// ---------------------------------------------------------------------------
// Tests — PAT Repo List
// ---------------------------------------------------------------------------

describe('RepoOnboarding — PAT repo list', () => {
  it('fetches and displays repos when PAT exists', async () => {
    localStorage.setItem('github_pat', 'ghp_testtoken');
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve([
          { name: 'repo-a', full_name: 'user/repo-a', description: 'First', language: 'Go', private: false },
          { name: 'repo-b', full_name: 'user/repo-b', description: 'Second', language: 'Rust', private: true },
        ]),
    });

    renderSync({});
    await flushAsync();

    // Click the Select Repo tab
    const selectTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('Select Repo'),
    ) as HTMLElement;
    act(() => {
      selectTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    expect(container.textContent).toContain('repo-a');
    expect(container.textContent).toContain('repo-b');
  });

  it('calls onRepoSelected when a repo card is clicked', async () => {
    localStorage.setItem('github_pat', 'ghp_testtoken');
    const onRepoSelected = vi.fn();
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve([
          { name: 'repo-a', full_name: 'user/repo-a', description: '', language: null, private: false },
        ]),
    });

    renderSync({ onRepoSelected });
    await flushAsync();

    // Click Select Repo tab
    const selectTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('Select Repo'),
    ) as HTMLElement;
    act(() => {
      selectTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    // Click repo card
    const repoCard = Array.from(container.querySelectorAll('.onboarding-repo-card')).find((el) =>
      (el as HTMLElement).textContent?.includes('repo-a'),
    ) as HTMLElement;
    expect(repoCard).toBeTruthy();
    act(() => {
      repoCard.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onRepoSelected).toHaveBeenCalledWith('user', 'repo-a');
  });

  it('shows empty state when user has no repos', async () => {
    localStorage.setItem('github_pat', 'ghp_testtoken');
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    });

    renderSync({});
    await flushAsync();

    const selectTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('Select Repo'),
    ) as HTMLElement;
    act(() => {
      selectTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await flushAsync();

    expect(container.textContent).toContain('No repositories found');
  });
});

// ---------------------------------------------------------------------------
// Tests — Create New Repo
// ---------------------------------------------------------------------------

describe('RepoOnboarding — create new repo', () => {
  it('shows create dialog when "New Repository" button is clicked', async () => {
    renderSync({});

    // Click the "New Repo" tab
    const createTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repo'),
    ) as HTMLElement;
    act(() => {
      createTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Click "New Repository" button
    const newRepoBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repository'),
    ) as HTMLElement;
    await act(async () => {
      newRepoBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(container.querySelector('.onboarding-dialog')).toBeTruthy();
    expect(container.textContent).toContain('Create New Repository');
  });

  it('validates repo name — rejects spaces and special characters', async () => {
    renderSync({});

    // Go to Create tab and open dialog
    const createTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repo'),
    ) as HTMLElement;
    act(() => {
      createTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const newRepoBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repository'),
    ) as HTMLElement;
    await act(async () => {
      newRepoBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 0));
    });

    // Type an invalid name
    const input = container.querySelector('.onboarding-dialog input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'repo with spaces!');
    });

    // Click "Create Repository"
    const createBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Create Repository'),
    ) as HTMLElement;
    await act(async () => {
      createBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(container.textContent).toContain('can only contain');
  });

  it('creates local repo and calls onRepoSelected', async () => {
    const onRepoSelected = vi.fn();
    renderSync({ onRepoSelected });

    // Go to Create tab and open dialog
    const createTab = Array.from(container.querySelectorAll('button.onboarding-tab')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repo'),
    ) as HTMLElement;
    act(() => {
      createTab.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const newRepoBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('New Repository'),
    ) as HTMLElement;
    await act(async () => {
      newRepoBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 0));
    });

    // Type a valid name
    const input = container.querySelector('.onboarding-dialog input[type="text"]') as HTMLInputElement;
    act(() => {
      setReactInputValue(input, 'my-new-repo');
    });

    // Click "Create Repository"
    const createBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      (b as HTMLElement).textContent?.includes('Create Repository'),
    ) as HTMLElement;
    await act(async () => {
      createBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 600));
    });

    expect(mockMkdir).toHaveBeenCalledWith('/repos/local', '/my-new-repo');
    expect(onRepoSelected).toHaveBeenCalledWith('local', 'my-new-repo');
  });
});

// ---------------------------------------------------------------------------
// Tests — Quick Tips
// ---------------------------------------------------------------------------

describe('RepoOnboarding — quick tips', () => {
  it('renders quick tips section', () => {
    renderSync({});
    expect(container.textContent).toContain('Quick Tips');
  });

  it('mentions terminal tip', () => {
    renderSync({});
    expect(container.textContent).toContain('terminal');
  });

  it('mentions ZIP download tip', () => {
    renderSync({});
    expect(container.textContent).toContain('ZIP');
  });
});
