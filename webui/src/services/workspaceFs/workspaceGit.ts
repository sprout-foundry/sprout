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
import { createGitFs } from './gitFs';
import { normalizeFsPath } from './types';
import type { WorkspaceFs } from './types';

/** Standard repo directory for owner/name. */
export function repoDir(repo: string): string {
  const parts = repo.split('/');
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    throw new Error('repo must be in owner/name format');
  }
  return `repos/${parts[0]}/${parts[1]}`;
}

/** Parse `owner/name` from an https URL or shorthand. */
export function parseRepoRef(input: string): { owner: string; name: string; url: string } {
  let url = input.trim();
  if (!/^https:\/\//.test(url)) {
    if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(url)) {
      url = 'https://github.com/' + url;
    } else {
      throw new Error('Repository must be an https URL or owner/name');
    }
  }
  const m = url.replace(/\.git$/, '').match(/\/([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)$/);
  if (!m) throw new Error('Cannot parse owner/name from URL');
  return { owner: m[1], name: m[2], url: url.replace(/\.git$/, '') + '.git' };
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
  await fs.mkdir(dir);
  await git.clone({
    fs: gitFs as unknown as Parameters<typeof git.clone>[0]['fs'],
    http,
    dir,
    url,
    depth: opts.depth ?? 1,
    singleBranch: true,
    ref: opts.branch,
    onAuth: opts.token ? () => ({ username: 'git', token: opts.token }) : undefined,
    onProgress: opts.onProgress,
  });

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
    /* detached or missing HEAD — non-fatal */
  }
  return { repo: `${owner}/${name}`, dir, entries, defaultBranch };
}

/** List cloned repos (immediate children of repos/ that contain .git). */
export async function listRepos(fs?: WorkspaceFs): Promise<string[]> {
  const seam = fs ?? (await import('./index')).getWorkspaceFs();
  const rootListing = await seam.list('repos', 1);
  if (!rootListing.ok) return [];
  const repos: string[] = [];
  for (const ownerDir of rootListing.files.filter((f) => f.isDir)) {
    const owner = ownerDir.path.split('/')[1];
    const nameListing = await seam.list(ownerDir.path, 1);
    if (!nameListing.ok) continue;
    for (const nameDir of nameListing.files.filter((f) => f.isDir)) {
      const name = nameDir.path.split('/').pop();
      const dotGit = await seam.stat(`${ownerDir.path}/${name}/.git`);
      if (dotGit.ok && dotGit.isDir) {
        repos.push(`${owner}/${name}`);
      }
    }
  }
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
