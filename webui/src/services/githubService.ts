/**
 * GitHub account service — PAT login, repo listing, and token storage.
 *
 * Talks directly to https://api.github.com with plain fetch(). GitHub's REST
 * API sends `Access-Control-Allow-Origin: *`, so this works from the desktop
 * build, the browser, and the native WebView — no bridge/proxy needed.
 *
 * ### Token storage contract
 *
 * The PAT is stored in localStorage under the key `github_pat`. That key is
 * the compat target read by agentGitTools.ts (git_push / git_pull), which we
 * must not modify — signing in here makes agent push/pull work automatically.
 *
 * The signed-in user profile is cached under `github_user` so the UI can
 * render account info without a network round-trip.
 *
 * ### Security
 *
 * The token is never logged, thrown, or echoed into error messages.
 */

import { debugLog } from '../utils/log';

export const GITHUB_API_BASE = 'https://api.github.com';
export const GITHUB_API_VERSION = '2022-11-28';
export const GITHUB_TOKENS_URL = 'https://github.com/settings/tokens';

/** localStorage keys. `GITHUB_TOKEN_KEY` is the agentGitTools.ts contract. */
export const GITHUB_TOKEN_KEY = 'github_pat';
export const GITHUB_USER_KEY = 'github_user';

/** Max pages fetched by listRepos (100 repos/page → 200 repos max). */
const MAX_REPO_PAGES = 2;
const PER_PAGE = 100;

/* ─── Types ─────────────────────────────────────────────────────────────── */

export interface GitHubUser {
  login: string;
  name: string | null;
  avatar_url: string;
  html_url: string;
}

export interface GitHubRepo {
  id: number;
  name: string;
  full_name: string;
  private: boolean;
  description: string | null;
  html_url: string;
  clone_url: string;
  default_branch: string;
  updated_at: string;
  owner: { login: string; avatar_url: string };
}

/* ─── Storage helpers ───────────────────────────────────────────────────── */

function hasLocalStorage(): boolean {
  try {
    return typeof localStorage !== 'undefined';
  } catch {
    // best-effort: feature detection — localStorage can throw on access in
    // sandboxed contexts.
    return false;
  }
}

/** The stored GitHub PAT, or null when signed out. */
export function getStoredToken(): string | null {
  if (!hasLocalStorage()) return null;
  try {
    return localStorage.getItem(GITHUB_TOKEN_KEY) || null;
  } catch {
    // best-effort: unreadable storage reads as "signed out".
    return null;
  }
}

/** Persist the PAT (also used by agent git tools via the same key). */
export function storeToken(token: string): void {
  if (!hasLocalStorage()) return;
  try {
    localStorage.setItem(GITHUB_TOKEN_KEY, token);
  } catch (err) {
    // storage unavailable (privacy mode / quota) — sign-in still works for
    // this session; nothing we can durably do here.
    debugLog('[githubService] failed to persist GitHub token:', err);
  }
}

/** Clear the PAT. */
export function clearToken(): void {
  if (!hasLocalStorage()) return;
  try {
    localStorage.removeItem(GITHUB_TOKEN_KEY);
  } catch {
    // best-effort: a key we can't remove is orphaned storage; the in-memory
    // session state is cleared by the caller regardless.
  }
}

/** Cached signed-in profile, or null. */
export function getStoredUser(): GitHubUser | null {
  if (!hasLocalStorage()) return null;
  try {
    const raw = localStorage.getItem(GITHUB_USER_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as GitHubUser;
    return parsed && typeof parsed.login === 'string' ? parsed : null;
  } catch {
    // best-effort: unreadable/corrupt cache reads as "no cached profile".
    return null;
  }
}

/** Cache the signed-in profile for offline rendering. */
export function storeUser(user: GitHubUser): void {
  if (!hasLocalStorage()) return;
  try {
    localStorage.setItem(GITHUB_USER_KEY, JSON.stringify(user));
  } catch {
    // best-effort: profile is a render-only cache; sign-in still succeeds.
  }
}

/** Sign out: clears BOTH the token and the cached profile. */
export function clearGitHubAccount(): void {
  clearToken();
  if (hasLocalStorage()) {
    try {
      localStorage.removeItem(GITHUB_USER_KEY);
    } catch {
      // best-effort: same as clearToken — orphaned key at worst.
    }
  }
}

/* ─── API helpers ───────────────────────────────────────────────────────── */

function authHeaders(token: string): Record<string, string> {
  return {
    Authorization: `Bearer ${token}`,
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': GITHUB_API_VERSION,
  };
}

/** Surface a human-readable message for a failed API response. */
async function responseErrorMessage(response: Response): Promise<string> {
  if (response.status === 401) {
    return 'Invalid or expired GitHub token. Sign out and paste a new personal access token.';
  }
  if (response.status === 403) {
    return 'GitHub rejected the request (403). This is usually rate limiting — try again in a minute.';
  }
  if (response.status === 404) {
    return 'Not found on GitHub. The token may not have access to this resource.';
  }
  let detail = '';
  try {
    const body = (await response.json()) as { message?: string };
    detail = typeof body?.message === 'string' ? body.message : '';
  } catch {
    // best-effort: non-JSON body — keep the status-derived message.
  }
  return detail ? `GitHub error (${response.status}): ${detail}` : `GitHub request failed (${response.status})`;
}

/* ─── Public API ────────────────────────────────────────────────────────── */

/**
 * Validate a PAT by fetching the authenticated user.
 * Throws a clear Error on 401 / network failure. Never includes the token.
 */
export async function validateToken(token: string): Promise<GitHubUser> {
  const trimmed = token.trim();
  if (!trimmed) throw new Error('Enter a personal access token.');

  let response: Response;
  try {
    response = await fetch(`${GITHUB_API_BASE}/user`, {
      method: 'GET',
      headers: authHeaders(trimmed),
    });
  } catch {
    throw new Error('Could not reach api.github.com. Check your network connection.');
  }

  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }

  const data = (await response.json()) as GitHubUser & { avatar_url?: string; name?: string | null };
  return {
    login: data.login,
    name: typeof data.name === 'string' ? data.name : null,
    avatar_url: typeof data.avatar_url === 'string' ? data.avatar_url : '',
    html_url: typeof data.html_url === 'string' ? data.html_url : `https://github.com/${data.login}`,
  };
}

/**
 * List the repositories visible to this token, most recently updated first.
 *
 * Fine-grained PATs only return repos the token was granted access to —
 * that is expected. Paginates up to `opts.maxPages` (default 2) pages of
 * `per_page=100`.
 */
export async function listRepos(token: string, opts?: { maxPages?: number }): Promise<GitHubRepo[]> {
  const trimmed = token.trim();
  if (!trimmed) throw new Error('Not signed in to GitHub.');

  const maxPages = Math.max(1, opts?.maxPages ?? MAX_REPO_PAGES);
  const repos: GitHubRepo[] = [];

  for (let page = 1; page <= maxPages; page++) {
    const url = `${GITHUB_API_BASE}/user/repos?sort=updated&per_page=${PER_PAGE}&page=${page}`;

    let response: Response;
    try {
      response = await fetch(url, { method: 'GET', headers: authHeaders(trimmed) });
    } catch {
      throw new Error('Could not reach api.github.com. Check your network connection.');
    }

    if (!response.ok) {
      throw new Error(await responseErrorMessage(response));
    }

    const pageRepos = (await response.json()) as Array<Record<string, unknown>>;
    if (!Array.isArray(pageRepos)) break;

    for (const repo of pageRepos) {
      repos.push({
        id: Number(repo.id),
        name: String(repo.name ?? ''),
        full_name: String(repo.full_name ?? ''),
        private: Boolean(repo.private),
        description: typeof repo.description === 'string' ? repo.description : null,
        html_url: String(repo.html_url ?? ''),
        clone_url: String(repo.clone_url ?? ''),
        default_branch: String(repo.default_branch ?? 'main'),
        updated_at: String(repo.updated_at ?? ''),
        owner: {
          login: String((repo.owner as { login?: string } | undefined)?.login ?? ''),
          avatar_url: String((repo.owner as { avatar_url?: string } | undefined)?.avatar_url ?? ''),
        },
      });
    }

    if (pageRepos.length < PER_PAGE) break; // last page reached
  }

  return repos;
}
