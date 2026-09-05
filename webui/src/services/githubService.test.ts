/**
 * githubService.test.ts — Unit tests for the GitHub account service.
 *
 * fetch() is stubbed globally (api.github.com must never be hit from tests).
 * Covers:
 * - validateToken: request shape (URL, Authorization, Accept, api version),
 *   happy path, 401 → clear message, network failure → friendly message
 * - listRepos: single page, two-page pagination, 401 → clear message,
 *   sorting/pagination query params
 * - storage helpers: token + user round-trips, clearGitHubAccount clears BOTH
 *   keys, malformed cached JSON is ignored
 * - security: the token never appears in thrown error messages
 */

import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  GITHUB_TOKEN_KEY,
  GITHUB_USER_KEY,
  clearGitHubAccount,
  clearToken,
  getStoredToken,
  getStoredUser,
  listRepos,
  storeToken,
  storeUser,
  validateToken,
} from './githubService';

const TOKEN = 'ghp_testtoken123';

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
    description: `Repo ${name}`,
    html_url: `https://github.com/octocat/${name}`,
    clone_url: `https://github.com/octocat/${name}.git`,
    default_branch: 'main',
    updated_at: '2026-01-02T03:04:05Z',
    owner: { login: 'octocat', avatar_url: 'https://avatars.githubusercontent.com/u/583231?v=4' },
    ...extra,
  };
}

describe('githubService', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    localStorage.clear();
    fetchMock = vi.fn().mockResolvedValue(jsonResponse(SAMPLE_USER));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  /* ── validateToken ─────────────────────────────────────────────── */

  describe('validateToken', () => {
    it('sends the documented headers to /user', async () => {
      await validateToken(TOKEN);

      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('https://api.github.com/user');
      expect(init.method).toBe('GET');
      const headers = init.headers as Record<string, string>;
      expect(headers.Authorization).toBe(`Bearer ${TOKEN}`);
      expect(headers.Accept).toBe('application/vnd.github+json');
      expect(headers['X-GitHub-Api-Version']).toBe('2022-11-28');
    });

    it('returns the normalized user profile', async () => {
      const user = await validateToken(TOKEN);
      expect(user).toEqual({
        login: 'octocat',
        name: 'The Octocat',
        avatar_url: SAMPLE_USER.avatar_url,
        html_url: SAMPLE_USER.html_url,
      });
    });

    it('nulls out a missing name and falls back for missing html_url', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ login: 'octocat', avatar_url: '' }));
      const user = await validateToken(TOKEN);
      expect(user.name).toBeNull();
      expect(user.html_url).toBe('https://github.com/octocat');
    });

    it('throws a clear error on 401 (and never leaks the token)', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ message: 'Bad credentials' }, 401));
      await expect(validateToken(TOKEN)).rejects.toThrow(/Invalid or expired GitHub token/i);
      await expect(validateToken(TOKEN)).rejects.toThrow(expect.not.stringContaining(TOKEN) as unknown as string);
    });

    it('throws a rate-limit message on 403', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ message: 'rate limited' }, 403));
      await expect(validateToken(TOKEN)).rejects.toThrow(/403.*rate limit|rate limiting/i);
    });

    it('throws a friendly error when the network fails', async () => {
      fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
      await expect(validateToken(TOKEN)).rejects.toThrow(/Could not reach api\.github\.com/);
    });

    it('rejects an empty token before fetching', async () => {
      await expect(validateToken('   ')).rejects.toThrow(/Enter a personal access token/);
      expect(fetchMock).not.toHaveBeenCalled();
    });
  });

  /* ── listRepos ─────────────────────────────────────────────────── */

  describe('listRepos', () => {
    it('requests /user/repos sorted by updated with per_page=100', async () => {
      fetchMock.mockResolvedValue(jsonResponse([sampleRepo(1, 'hello-world')]));
      await listRepos(TOKEN);

      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
      expect(url).toContain('https://api.github.com/user/repos?');
      expect(url).toContain('sort=updated');
      expect(url).toContain('per_page=100');
      expect((init.headers as Record<string, string>).Authorization).toBe(`Bearer ${TOKEN}`);
    });

    it('maps repos into the normalized shape', async () => {
      fetchMock.mockResolvedValue(
        jsonResponse([sampleRepo(1, 'public-repo'), sampleRepo(2, 'private-repo', { private: true })]),
      );
      const repos = await listRepos(TOKEN);

      expect(repos).toHaveLength(2);
      expect(repos[0]).toMatchObject({ name: 'public-repo', full_name: 'octocat/public-repo', private: false });
      expect(repos[1]).toMatchObject({ name: 'private-repo', private: true });
      expect(repos[0].clone_url).toBe('https://github.com/octocat/public-repo.git');
      expect(repos[0].owner.login).toBe('octocat');
    });

    it('stops after one page when fewer than 100 repos come back', async () => {
      fetchMock.mockResolvedValue(jsonResponse([sampleRepo(1, 'solo')]));
      const repos = await listRepos(TOKEN);
      expect(repos).toHaveLength(1);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it('paginates to a second page when the first page is full', async () => {
      const fullPage = Array.from({ length: 100 }, (_, i) => sampleRepo(i, `repo-${i}`));
      fetchMock
        .mockResolvedValueOnce(jsonResponse(fullPage))
        .mockResolvedValueOnce(jsonResponse([sampleRepo(999, 'page-two-repo')]));

      const repos = await listRepos(TOKEN);

      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect((fetchMock.mock.calls[1] as unknown[])[0]).toContain('page=2');
      expect(repos).toHaveLength(101);
      expect(repos[100].name).toBe('page-two-repo');
    });

    it('caps pagination at 2 pages by default', async () => {
      const fullPage = Array.from({ length: 100 }, (_, i) => sampleRepo(i, `repo-${i}`));
      // Fresh Response per call — a Response body can only be read once.
      fetchMock.mockImplementation(async () => jsonResponse(fullPage));

      await listRepos(TOKEN);

      expect(fetchMock).toHaveBeenCalledTimes(2);
    });

    it('throws a clear error on 401', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ message: 'Bad credentials' }, 401));
      await expect(listRepos(TOKEN)).rejects.toThrow(/Invalid or expired GitHub token/i);
    });

    it('throws a friendly error when the network fails', async () => {
      fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
      await expect(listRepos(TOKEN)).rejects.toThrow(/Could not reach api\.github\.com/);
    });

    it('rejects when no token is given', async () => {
      await expect(listRepos('  ')).rejects.toThrow(/Not signed in/);
      expect(fetchMock).not.toHaveBeenCalled();
    });
  });

  /* ── storage helpers ───────────────────────────────────────────── */

  describe('storage helpers', () => {
    it('getStoredToken returns null when signed out', () => {
      expect(getStoredToken()).toBeNull();
    });

    it('storeToken / getStoredToken round-trip under the agentGitTools key', () => {
      storeToken(TOKEN);
      expect(localStorage.getItem(GITHUB_TOKEN_KEY)).toBe(TOKEN);
      expect(getStoredToken()).toBe(TOKEN);
    });

    it('clearToken removes only the token', () => {
      storeToken(TOKEN);
      storeUser(SAMPLE_USER);
      clearToken();
      expect(localStorage.getItem(GITHUB_TOKEN_KEY)).toBeNull();
      expect(localStorage.getItem(GITHUB_USER_KEY)).not.toBeNull();
    });

    it('storeUser / getStoredUser round-trip', () => {
      storeUser(SAMPLE_USER);
      expect(getStoredUser()).toEqual(SAMPLE_USER);
    });

    it('getStoredUser tolerates malformed cached JSON', () => {
      localStorage.setItem(GITHUB_USER_KEY, '{not json');
      expect(getStoredUser()).toBeNull();
    });

    it('clearGitHubAccount clears BOTH keys (logout)', () => {
      storeToken(TOKEN);
      storeUser(SAMPLE_USER);
      clearGitHubAccount();
      expect(localStorage.getItem(GITHUB_TOKEN_KEY)).toBeNull();
      expect(localStorage.getItem(GITHUB_USER_KEY)).toBeNull();
    });
  });
});
