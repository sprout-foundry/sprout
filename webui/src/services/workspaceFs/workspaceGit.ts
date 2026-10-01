/**
 * workspaceGit — clone/list/remove git repos THROUGH the workspaceFs seam.
 *
 * One service for every UI and agent path that touches git:
 *   - `cloneRepo(url, opts)`  — shallow clone into repos/<owner>/<name>,
 *     landing natively on device (native fs backend) or in the daemon's
 *     workspace on desktop. No sidecar filesystem, no fan-out sync.
 *   - `listRepos()`           — the repos/ directory from the same seam.
 *   - `removeRepo(repo)`      — recursive delete via the seam.
 *
 * Auth: optional PAT (GitHub or any git host) attached via isomorphic-git's
 * onAuth. Tokens never leave the app: they come from the native keychain
 * (git-credential conventions) or the login sheet, and are passed straight
 * into the git transport.
 */

import git from 'isomorphic-git';
import http from 'isomorphic-git/http/web';
import { gitCorsProxy } from '../gitCorsProxy';
import { createGitFs } from './gitFs';
import { normalizeFsPath } from './types';
import type { WorkspaceFs } from './types';

/** Standard repo directory for owner/name. */
export function repoDir(repo: string): string {
  const parts = repo.split('/');
  if (parts.length < 2 || parts.some((p) => !p || p === '.' || p === '..')) {
    throw new Error('repo must be in owner/name format');
  }
  return `repos/${parts.join('/')}`;
}

/**
 * Hosts whose repositories are always owner/name; anything deeper in their
 * URLs is a view inside the repo (…/tree/main). Other hosts (GitLab,
 * self-managed instances) nest repos under groups and subgroups.
 */
const TWO_SEGMENT_HOSTS = new Set(['github.com', 'bitbucket.org']);

const SEGMENT = /^[A-Za-z0-9_.-]+$/;

/**
 * Parse a repository from an https URL or `owner/name` shorthand (GitHub).
 * `owner` is everything before the last path segment, so a GitLab subgroup
 * repo gives owner "group/sub". `url` is the canonical clone URL.
 */
export function parseRepoRef(input: string): { owner: string; name: string; url: string; host: string } {
  let raw = input.trim();
  if (!/^https:\/\//.test(raw)) {
    if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(raw)) {
      raw = 'https://github.com/' + raw;
    } else {
      throw new Error('Repository must be an https URL or owner/name');
    }
  }
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error('Cannot parse owner/name from URL');
  }
  const host = parsed.host.toLowerCase();
  let path = parsed.pathname.replace(/^\/+|\/+$/g, '');
  const viewAt = path.indexOf('/-/');
  if (viewAt >= 0) path = path.slice(0, viewAt);
  path = path.replace(/\.git$/, '');
  let segments = path.split('/').filter(Boolean);
  if (TWO_SEGMENT_HOSTS.has(host)) segments = segments.slice(0, 2);
  if (segments.length < 2 || !segments.every((seg) => SEGMENT.test(seg) && seg !== '.' && seg !== '..')) {
    throw new Error('Cannot parse owner/name from URL');
  }
  const name = segments[segments.length - 1];
  const owner = segments.slice(0, -1).join('/');
  return { owner, name, url: `${parsed.protocol}//${parsed.host}/${segments.join('/')}.git`, host };
}

export interface CloneProgress {
  phase: string;
  loaded: number;
  total: number;
}

export interface CloneOpts {
  fs?: WorkspaceFs;
  token?: string;
  depth?: number;
  branch?: string;
  onProgress?: (p: CloneProgress) => void;
}

export interface CloneResult {
  repo: string;
  dir: string;
  entries: number;
  defaultBranch: string | null;
}

/** Clone `urlOrRef` into repos/<owner>/<name>/ through the seam. */
export async function cloneRepo(urlOrRef: string, opts: CloneOpts = {}): Promise<CloneResult> {
  const { owner, name, url } = parseRepoRef(urlOrRef);
  const fs = opts.fs ?? (await import('./index')).getWorkspaceFs();
  const dir = repoDir(`${owner}/${name}`);

  // Refuse to clobber an existing checkout (caller deletes first to re-clone).
  const existing = await fs.stat(dir);
  if (existing.ok) {
    throw new Error(`${dir} already exists — remove it first to re-clone`);
  }

  const gitFs = createGitFs(fs);
  try {
    await fs.mkdir(dir);
    await git.clone({
      fs: gitFs as unknown as Parameters<typeof git.clone>[0]['fs'],
      http,
      corsProxy: gitCorsProxy(),
      dir,
      url,
      depth: opts.depth ?? 1,
      singleBranch: true,
      ref: opts.branch,
      onAuth: opts.token ? () => ({ username: 'git', token: opts.token }) : undefined,
      onProgress: opts.onProgress,
    });
  } catch (err) {
    // Don't leave a half-written checkout in the workspace: remove the
    // repos/<owner>/<name> directory so a retry starts clean and the
    // workspace never shows a broken repo.
    try {
      await fs.remove(dir);
    } catch {
      /* best-effort cleanup */
    }
    throw err;
  }

  const listing = await fs.list(dir, 1);
  const entries = listing.ok ? listing.files.length : 0;
  let defaultBranch: string | null = null;
  try {
    const currentBranch = await git.currentBranch({
      fs: gitFs as unknown as Parameters<typeof git.currentBranch>[0]['fs'],
      dir,
    });
    defaultBranch = currentBranch ?? null;
  } catch {
    // best-effort: detached or missing HEAD — report no default branch.
  }
  return { repo: `${owner}/${name}`, dir, entries, defaultBranch };
}

/**
 * List cloned repos: directories under repos/ that contain .git, as their
 * path below repos/ ("owner/name", or "group/sub/name" for nested hosts).
 */
export async function listRepos(fs?: WorkspaceFs): Promise<string[]> {
  const seam = fs ?? (await import('./index')).getWorkspaceFs();
  const repos: string[] = [];
  // owner/name is depth 2; subgroups nest deeper. A repo's own directory is
  // never searched further.
  const MAX_DEPTH = 5;
  const walk = async (dir: string, depth: number): Promise<void> => {
    if (depth > MAX_DEPTH) return;
    const listing = await seam.list(dir, 1);
    if (!listing.ok) return;
    for (const child of listing.files.filter((f) => f.isDir)) {
      if (depth >= 2) {
        const dotGit = await seam.stat(`${child.path}/.git`);
        if (dotGit.ok && dotGit.isDir) {
          repos.push(child.path.replace(/^repos\//, ''));
          continue;
        }
      }
      await walk(child.path, depth + 1);
    }
  };
  await walk('repos', 1);
  return repos.sort();
}

/** Remove a cloned repo (recursive) via the seam. */
export async function removeRepo(repo: string, fs?: WorkspaceFs): Promise<boolean> {
  const seam = fs ?? (await import('./index')).getWorkspaceFs();
  const dir = normalizeFsPath(repoDir(repo));
  const stat = await seam.stat(dir);
  if (!stat.ok) return false;
  const r = await seam.remove(dir);
  return r.ok;
}
