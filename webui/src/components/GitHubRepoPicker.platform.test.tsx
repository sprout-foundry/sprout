import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const { mockClone, platform } = vi.hoisted(() => ({
  mockClone: vi.fn(),
  platform: { connected: true },
}));

vi.mock('../services/workspaceClone', () => ({
  cloneIntoWorkspace: (...args: unknown[]) => mockClone(...args),
}));
vi.mock('./ThemedDialog', () => ({ showThemedConfirm: vi.fn() }));
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));
vi.mock('../services/platformGitHub', () => ({
  usesPlatformGitHub: () => true,
  platformGitHubSettingsHref: () => '/?from=editor#/settings',
  fetchPlatformGitHubConnected: () => Promise.resolve(platform.connected),
  listPlatformRepos: () =>
    Promise.resolve([
      {
        id: 7,
        name: 'widgets',
        full_name: 'acme/widgets',
        private: true,
        description: null,
        html_url: 'https://github.com/acme/widgets',
        clone_url: 'https://github.com/acme/widgets.git',
        default_branch: 'main',
        updated_at: '2026-09-01T00:00:00Z',
        owner: { login: 'acme', avatar_url: '' },
      },
    ]),
}));

import GitHubRepoPicker from './GitHubRepoPicker';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  platform.connected = true;
  localStorage.clear();
  mockClone.mockReset().mockResolvedValue({ repo: 'acme/widgets', dir: '', entries: 1, defaultBranch: 'main' });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

async function open() {
  await act(async () => {
    root.render(<GitHubRepoPicker isOpen onClose={() => undefined} />);
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

describe('GitHubRepoPicker with the Foundry account connection', () => {
  it("lists the account's repositories and clones without a token", async () => {
    await open();
    expect(document.body.querySelector('[data-testid="platform-gh-card"]')?.textContent).toContain('connected');
    const item = document.body.querySelector<HTMLButtonElement>('[data-testid="gh-repo-acme/widgets"]');
    expect(item).toBeTruthy();
    await act(async () => item!.click());
    expect(mockClone).toHaveBeenCalledWith('https://github.com/acme/widgets.git', { token: undefined });
  });

  it('points to account settings when GitHub is not connected', async () => {
    platform.connected = false;
    await open();
    const link = document.body.querySelector<HTMLAnchorElement>('[data-testid="platform-gh-manage"]');
    expect(link?.textContent).toContain('Connect GitHub');
    expect(link?.getAttribute('href')).toBe('/?from=editor#/settings');
    expect(document.body.querySelector('input[aria-label="GitHub personal access token"]')).toBeNull();
  });
});
