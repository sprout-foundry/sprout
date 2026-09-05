/**
 * GitHubSettingsTab.test.tsx — Environment ▸ GitHub settings subsection.
 *
 * Verifies the SP-017 wiring end-to-end at the component level:
 *  - signed out → PAT sign-in form with the token-hint link
 *  - signed in (cached profile) → account card with login + sign-out
 *  - sign-out clears BOTH localStorage keys (github_pat + github_user)
 *  - sign-in validates then persists token + profile
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach, beforeAll, afterAll } from 'vitest';
import GitHubSettingsTab from './GitHubSettingsTab';

const TOKEN = 'ghp_settings_token';

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

let mountPoint: HTMLDivElement;
let root: Root | null = null;
let fetchMock: ReturnType<typeof vi.fn>;

function renderTab() {
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint);
    root.render(<GitHubSettingsTab />);
  });
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

beforeAll(() => {
  (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  fetchMock = vi.fn().mockResolvedValue(jsonResponse(SAMPLE_USER));
  vi.stubGlobal('fetch', fetchMock);
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    root?.unmount();
    root = null;
  });
  mountPoint.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe('GitHubSettingsTab', () => {
  it('renders the sign-in form when signed out', () => {
    renderTab();
    expect(document.querySelector('[data-testid="gh-signin-form"]')).not.toBeNull();
    expect(document.querySelector('[data-testid="gh-account-card"]')).toBeNull();
  });

  it('documents the github_pat storage key and the tokens link', () => {
    renderTab();
    const note = document.querySelector('.settings-info-note');
    expect(note?.textContent).toContain('github_pat');
    const link = document.querySelector('.gh-signin-hint a') as HTMLAnchorElement;
    expect(link?.getAttribute('href')).toBe('https://github.com/settings/tokens');
  });

  it('signs in: validates the PAT and persists token + profile', async () => {
    renderTab();

    setInputValue('[data-testid="gh-signin-input"]', TOKEN);
    click('[data-testid="gh-signin-submit"]');

    await act(async () => {
      await Promise.resolve();
    });

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.github.com/user',
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: `Bearer ${TOKEN}` }),
      }),
    );
    expect(localStorage.getItem('github_pat')).toBe(TOKEN);
    expect(JSON.parse(localStorage.getItem('github_user') ?? '{}').login).toBe('octocat');
    expect(document.querySelector('[data-testid="gh-account-card"]')).not.toBeNull();
  });

  it('keeps the form (and stores nothing) on an invalid token', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ message: 'Bad credentials' }, 401));

    renderTab();
    setInputValue('[data-testid="gh-signin-input"]', TOKEN);
    click('[data-testid="gh-signin-submit"]');

    await act(async () => {
      await Promise.resolve();
    });

    expect(document.querySelector('[data-testid="gh-signin-error"]')?.textContent).toMatch(
      /Invalid or expired GitHub token/i,
    );
    expect(localStorage.getItem('github_pat')).toBeNull();
  });

  it('shows the account card when signed in', () => {
    localStorage.setItem('github_pat', TOKEN);
    localStorage.setItem('github_user', JSON.stringify(SAMPLE_USER));

    renderTab();

    const card = document.querySelector('[data-testid="gh-account-card"]');
    expect(card).not.toBeNull();
    expect(card?.textContent).toContain('The Octocat');
    expect(card?.textContent).toContain('@octocat');
    expect(document.querySelector('[data-testid="gh-signin-form"]')).toBeNull();
  });

  it('sign-out clears both storage keys and returns to the form', () => {
    localStorage.setItem('github_pat', TOKEN);
    localStorage.setItem('github_user', JSON.stringify(SAMPLE_USER));

    renderTab();
    click('[data-testid="gh-signout-btn"]');

    expect(localStorage.getItem('github_pat')).toBeNull();
    expect(localStorage.getItem('github_user')).toBeNull();
    expect(document.querySelector('[data-testid="gh-signin-form"]')).not.toBeNull();
  });
});
