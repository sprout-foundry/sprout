/**
 * GitHub through the user's Foundry account, for the hosted editor.
 *
 * The platform stores the account's GitHub connection and injects it into
 * git traffic at /git-proxy, so the hosted editor never holds a token: it
 * asks the platform whether GitHub is connected and lists repositories from
 * the platform's cached copy of the account's repo list.
 */

import { platformHref } from '../utils/platformUrl';
import { gitCorsProxy } from './gitCorsProxy';
import type { GitHubRepo } from './githubService';

/** True when GitHub access comes from the Foundry account, not a local token. */
export function usesPlatformGitHub(): boolean {
  return gitCorsProxy() !== undefined;
}

/** Where the account's GitHub connection is managed. */
export function platformGitHubSettingsHref(): string {
  return platformHref('/?from=editor#/settings');
}

export async function fetchPlatformGitHubConnected(): Promise<boolean> {
  const res = await fetch(platformHref('/user/me'), { credentials: 'include' });
  if (!res.ok) throw new Error(`Could not load your account (HTTP ${res.status}).`);
  const me = (await res.json()) as { github_connected?: boolean };
  return me.github_connected === true;
}

interface PlatformRepo {
  id: number;
  full_name: string;
  html_url: string;
  description?: string;
  default_branch: string;
  private: boolean;
  updated_at: string;
}

const MAX_PAGES = 3;

export async function listPlatformRepos(): Promise<GitHubRepo[]> {
  const repos: GitHubRepo[] = [];
  let cursor: string | null = null;
  for (let page = 0; page < MAX_PAGES; page++) {
    const qs = new URLSearchParams({ limit: '100' });
    if (cursor) qs.set('cursor', cursor);
    const res = await fetch(platformHref(`/user/me/repos?${qs}`), { credentials: 'include' });
    if (!res.ok) throw new Error(`Could not list your repositories (HTTP ${res.status}).`);
    const body = (await res.json()) as { repos?: PlatformRepo[]; next_cursor?: string | null };
    for (const r of body.repos ?? []) repos.push(toGitHubRepo(r));
    cursor = body.next_cursor ?? null;
    if (!cursor) break;
  }
  return repos;
}

function toGitHubRepo(r: PlatformRepo): GitHubRepo {
  const [owner = '', name = r.full_name] = r.full_name.split('/');
  return {
    id: r.id,
    name,
    full_name: r.full_name,
    private: r.private,
    description: r.description ?? null,
    html_url: r.html_url,
    clone_url: `${r.html_url}.git`,
    default_branch: r.default_branch,
    updated_at: r.updated_at,
    owner: { login: owner, avatar_url: '' },
  };
}

/** Why a repository couldn't be created, in terms the form can act on. */
export class CreateRepoError extends Error {
  constructor(
    message: string,
    readonly code: 'invalid_name' | 'name_unavailable' | 'github_not_connected' | 'failed',
  ) {
    super(message);
  }
}

/**
 * Creates a repository in the account's GitHub (with an initial commit, so it
 * opens straight away) and returns its web URL.
 */
export async function createPlatformRepo(opts: { name: string; private: boolean }): Promise<string> {
  const res = await fetch(platformHref('/user/me/repos'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ name: opts.name, private: opts.private }),
  });
  const body = (await res.json().catch(() => ({}))) as {
    html_url?: string;
    error?: string | { code?: string; message?: string };
    code?: string;
  };
  if (res.ok && body.html_url) return body.html_url;
  if (typeof body.error === 'object' && body.error?.code === 'github_reauth_required') {
    throw new CreateRepoError('Connect GitHub to your account to create repositories.', 'github_not_connected');
  }
  const message = typeof body.error === 'string' ? body.error : `Could not create the repository (HTTP ${res.status}).`;
  if (body.code === 'invalid_name' || body.code === 'name_unavailable') throw new CreateRepoError(message, body.code);
  throw new CreateRepoError(message, 'failed');
}
