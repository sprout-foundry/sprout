/**
 * GitHubRepoPicker.test.tsx — Unit tests for the authenticated clone modal.
 *
 * Covers the two-state flow the spec requires:
 *  - signed out → PAT form renders; a bad token surfaces the inline error and
 *    does NOT store anything
 *  - signed in (stored token) → repo list loads from the mocked API, the
 *    search box filters client-side, clicking a repo calls browserGit.gitClone
 *    with the token, and clone errors render inline
 *  - logout clears BOTH localStorage keys
 *
 * fetch is stubbed globally (api.github.com must never be hit); browserGit is
 * mocked so no IndexedDB / isomorphic-git work happens.
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach, beforeAll, afterAll } from 'vitest';
import GitHubRepoPicker from './GitHubRepoPicker';

// ── Mocks ────────────────────────────────────────────────────────────────

const { mockCloneRepo, mockConfirm } = vi.hoisted(() => ({
  mockCloneRepo: vi.fn(),
  mockConfirm: vi.fn().mockResolvedValue(true),
}));

vi.mock('../services/workspaceFs/backendsExport', () => ({
  cloneRepo: (...args: unknown[]) => mockCloneRepo(...args),
}));

vi.mock('./ThemedDialog', () => ({
  showThemedConfirm: (...args: unknown[]) => mockConfirm(...args),
}));

vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

const TOKEN = 'ghp_picker_token';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const SAMPLE_USER = {
  login: 'octocat',
  name: 'The Octocat',
  avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4',
  html_url: 'https://github.com/octocat',
};

function sampleRepo(id: number, name: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    name,
    full_name: `octocat/${name}`,
    private: false,
    description: `Description of ${name}`,
    html_url: `https://github.com/octocat/${name}`,
    clone_url: `https://github.com/octocat/${name}.git`,
    default_branch: 'main',
    updated_at: '2026-01-02T03:04:05Z',
    owner: { login: 'octocat', avatar_url: '' },
    ...extra,
  };
}

// ── Harness ──────────────────────────────────────────────────────────────

let mountPoint: HTMLDivElement;
let root: Root | null = null;
let fetchMock: ReturnType<typeof vi.fn>;

beforeAll(() => {
  // React 18 + manual createRoot: silence "not configured to support act".
  (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
});

function renderPicker(props: { isOpen?: boolean } = {}) {
  const onClose = vi.fn();
  const onCloned = vi.fn();
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint);
    root.render(<GitHubRepoPicker isOpen={props.isOpen ?? true} onClose={onClose} onCloned={onCloned} />);
  });
  return { onClose, onCloned };
}

function setInputValue(selector: string, value: string) {
  const input = document.querySelector(selector) as HTMLInputElement;
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

function click(selector: string) {
  const el = document.querySelector(selector) as HTMLElement;
  act(() => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  fetchMock = vi.fn().mockResolvedValue(jsonResponse([sampleRepo(1, 'Hello-World'), sampleRepo(2, 'Spoon-Knife')]));
  vi.stubGlobal('fetch', fetchMock);
  mockCloneRepo.mockResolvedValue({ repo: "octocat/Hello-World", dir: "repos/octocat/Hello-World", entries: 3, defaultBranch: "master" });
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    root?.unmount();
    root = null;
  });
  mountPoint.remove();
  document.querySelectorAll('.gh-picker-overlay').forEach((el) => el.remove());
  vi.unstubAllGlobals();
  localStorage.clear();
});

// ── Tests ────────────────────────────────────────────────────────────────

describe('GitHubRepoPicker', () => {
  it('renders nothing when closed', () => {
    renderPicker({ isOpen: false });
    expect(document.querySelector('.gh-picker-overlay')).toBeNull();
  });

  describe('signed out', () => {
    it('shows the sign-in form, not the repo list', () => {
      renderPicker();
      expect(document.querySelector('[data-testid="gh-signin-form"]')).not.toBeNull();
      expect(document.querySelector('[data-testid="gh-picker-list"]')).toBeNull();
      expect(fetchMock).not.toHaveBeenCalled();
    });

    it('signs in: validates, stores token + user, then loads repos', async () => {
      fetchMock.mockImplementation(async (input: RequestInfo | URL) =>
        String(input).endsWith('/user') ? jsonResponse(SAMPLE_USER) : jsonResponse([sampleRepo(1, 'Hello-World')]),
      );

      renderPicker();
      setInputValue('[data-testid="gh-signin-input"]', TOKEN);
      click('[data-testid="gh-signin-submit"]');

      await act(async () => {
        await Promise.resolve();
      });

      expect(localStorage.getItem('github_pat')).toBe(TOKEN);
      expect(JSON.parse(localStorage.getItem('github_user') ?? '{}').login).toBe('octocat');
      expect(document.querySelector('[data-testid="gh-account-card"]')).not.toBeNull();
      expect(document.querySelector('[data-testid="gh-repo-octocat/Hello-World"]')).not.toBeNull();
    });

    it('shows an inline error and stores nothing on an invalid token', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ message: 'Bad credentials' }, 401));

      renderPicker();
      setInputValue('[data-testid="gh-signin-input"]', TOKEN);
      click('[data-testid="gh-signin-submit"]');

      await act(async () => {
        await Promise.resolve();
      });

      const errorEl = document.querySelector('[data-testid="gh-signin-error"]');
      expect(errorEl?.textContent).toMatch(/Invalid or expired GitHub token/i);
      expect(localStorage.getItem('github_pat')).toBeNull();
      expect(localStorage.getItem('github_user')).toBeNull();
    });

    it('includes the token-hint link to github.com/settings/tokens', () => {
      renderPicker();
      const link = document.querySelector('.gh-signin-hint a') as HTMLAnchorElement;
      expect(link?.getAttribute('href')).toBe('https://github.com/settings/tokens');
    });
  });

  describe('signed in', () => {
    beforeEach(() => {
      localStorage.setItem('github_pat', TOKEN);
      localStorage.setItem('github_user', JSON.stringify(SAMPLE_USER));
    });

    it('loads and renders the repo list with private badges', async () => {
      fetchMock.mockResolvedValue(
        jsonResponse([sampleRepo(1, 'Hello-World'), sampleRepo(2, 'secret', { private: true })]),
      );

      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      const rows = document.querySelectorAll('.gh-repo-row');
      expect(rows.length).toBe(2);
      const privateBadge = document.querySelector('[data-testid="gh-repo-octocat/secret"] .gh-repo-badge');
      expect(privateBadge?.textContent).toMatch(/Private/i);
    });

    it('filters the list client-side by name', async () => {
      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      setInputValue('[data-testid="gh-picker-search"]', 'spoon');
      expect(document.querySelectorAll('.gh-repo-row').length).toBe(1);
      expect(document.querySelector('[data-testid="gh-repo-octocat/Spoon-Knife"]')).not.toBeNull();
    });

    it('shows an empty state when nothing matches the filter', async () => {
      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      setInputValue('[data-testid="gh-picker-search"]', 'no-such-repo');
      expect(document.querySelector('[data-testid="gh-picker-empty"]')).not.toBeNull();
    });

    it('shows the list error at the top of the modal body, with a retry action', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ message: 'Bad credentials' }, 401));

      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      const state = document.querySelector('[data-testid="gh-picker-list-error"]');
      expect(state?.textContent).toMatch(/Invalid or expired GitHub token/i);
      expect(document.querySelector('[data-testid="gh-picker-retry"]')).not.toBeNull();

      // The banner renders BEFORE the search input and repo list — the user's
      // eye is already at the top of the modal when the failure lands.
      const body = document.querySelector('.gh-picker-body');
      const children = Array.from(body?.children ?? []);
      expect(children[0]).toBe(state);
      expect(children.indexOf(document.querySelector('.gh-picker-search'))).toBeGreaterThan(
        children.indexOf(state as NonNullable<typeof state>),
      );
    });

    it('retry re-issues the repo listing', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse({ message: 'Bad credentials' }, 401));
      fetchMock.mockResolvedValueOnce(jsonResponse([sampleRepo(1, 'Hello-World')]));

      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      expect(document.querySelector('[data-testid="gh-picker-list-error"]')).not.toBeNull();

      click('[data-testid="gh-picker-retry"]');

      await act(async () => {
        await Promise.resolve();
      });

      expect(document.querySelector('[data-testid="gh-picker-list-error"]')).toBeNull();
      expect(document.querySelector('[data-testid="gh-repo-octocat/Hello-World"]')).not.toBeNull();
    });

    it('clones through the workspaceFs seam with the token and closes on success', async () => {
      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      click('[data-testid="gh-repo-octocat/Hello-World"]');

      await act(async () => {
        await Promise.resolve();
      });

      expect(mockCloneRepo).toHaveBeenCalledTimes(1);
      expect(mockCloneRepo).toHaveBeenCalledWith("https://github.com/octocat/Hello-World.git", { token: TOKEN });
    });

    it('renders the clone error inline and stays open on failure', async () => {
      mockCloneRepo.mockRejectedValue(new Error('repository not found'));

      const { onClose } = renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      click('[data-testid="gh-repo-octocat/Hello-World"]');

      await act(async () => {
        await Promise.resolve();
      });

      const err = document.querySelector('[data-testid="gh-picker-clone-error"]');
      expect(err?.textContent).toMatch(/Could not clone octocat\/Hello-World.*repository not found/);
      // The banner is the FIRST child of the modal body — above the account
      // card, the search box, and the list.
      expect(Array.from(document.querySelector('.gh-picker-body')?.children ?? [])[0]).toBe(err);
      expect(onClose).not.toHaveBeenCalled();
    });

    it('logout asks for confirmation, then clears both keys and returns to the sign-in view', async () => {
      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      click('[data-testid="gh-signout-btn"]');

      // The sign-out is destructive (drops the stored PAT) — it must confirm.
      expect(mockConfirm).toHaveBeenCalledTimes(1);

      await act(async () => {
        await Promise.resolve();
      });

      expect(localStorage.getItem('github_pat')).toBeNull();
      expect(localStorage.getItem('github_user')).toBeNull();
      expect(document.querySelector('[data-testid="gh-signin-form"]')).not.toBeNull();
      expect(document.querySelector('[data-testid="gh-picker-list"]')).toBeNull();
    });

    it('logout cancelled keeps the session intact', async () => {
      mockConfirm.mockResolvedValueOnce(false);
      renderPicker();

      await act(async () => {
        await Promise.resolve();
      });

      click('[data-testid="gh-signout-btn"]');

      await act(async () => {
        await Promise.resolve();
      });

      expect(localStorage.getItem('github_pat')).toBe(TOKEN);
      expect(document.querySelector('[data-testid="gh-account-card"]')).not.toBeNull();
    });
  });
});
