/**
 * Browser-native git using isomorphic-git + lightning-fs.
 *
 * In cloud mode, git runs entirely in the browser. This service:
 * - Uses lightning-fs (IndexedDB-backed) for the git repository state
 *   (browserGitStore.ts, which also mirrors files to and from the WASM VFS)
 * - Supports: init, status, add, commit, log, branch, checkout, push, pull, diff
 * - GitHub push/pull via the user's token or anonymous HTTPS clone
 */

import './bufferGlobal'; // isomorphic-git needs a global Buffer
import * as git from 'isomorphic-git';
import type { ProgressCallback } from 'isomorphic-git';
import http from 'isomorphic-git/http/web';
import { generateUnifiedDiff } from '../utils/simpleDiff';
import { snapshotNote, snapshotSource } from './browserGitSnapshot';
import {
  REPO_DIR,
  clearRepoDir,
  ensureDir,
  ensureInitialized,
  getBrowserGitVfsBridge,
  getFs,
  markRepoInitialized,
  syncGitFsToVfs,
  syncVfsToGitFs,
  workTreePaths,
} from './browserGitStore';
import { gitCorsProxy } from './gitCorsProxy';
import { CloneTooLargeError, byteLimitedHttp } from './gitHttpLimit';

export {
  configureBrowserGit,
  whenBrowserGitConfigured,
  __resetBrowserGitForTest,
  getBrowserGitVfsBridge,
  holdBrowserGitWorkTree,
  restoreGitWorkingTree,
} from './browserGitStore';
export type { BrowserGitConfig } from './browserGitStore';
export { gitInitSnapshot, snapshotSource } from './browserGitSnapshot';

const bridge = () => getBrowserGitVfsBridge();

/**
 * Honest empty-repo GitStatusResponse (the same shape the local daemon's
 * /api/git/status returns, and the platform's cloud fallback). Used by the
 * boot-time guard in executeGitOp so the git panel shows "No git repository
 * found" (neutral) instead of erroring before browser git is wired.
 */
function emptyGitStatus() {
  return {
    message: 'success',
    in_git_repo: false,
    status: {
      branch: '',
      ahead: 0,
      behind: 0,
      staged: [] as Array<{ path: string; status: string; staged?: boolean }>,
      modified: [] as Array<{ path: string; status: string; staged?: boolean }>,
      untracked: [] as Array<{ path: string; status: string; staged?: boolean }>,
      deleted: [] as Array<{ path: string; status: string; staged?: boolean }>,
      renamed: [] as Array<{ path: string; status: string; staged?: boolean }>,
      in_git_repo: false,
    },
    files: [] as Array<{ path: string; status: string; staged?: boolean }>,
  };
}

/**
 * Honest empty GitDiffResponse for the boot-time guard in `executeGitOp`
 * (the `diff` analogue of `emptyGitStatus`): browser git was never wired
 * (the VFS bridge is absent — e.g. an embedded `SproutWorkspace` that
 * imported a repo via `?repo=` and has no `.git` in the VFS), so there is
 * no history to diff. Return a neutral empty-diff shape (message `success`,
 * no staged/unstaged changes) instead of throwing `browserGit not
 * configured`, which the handler surfaces as a 500 → the editor's
 * "Failed to fetch git diff" toast on every file open. The editor's
 * diff-fetch handler then takes its empty branch (clears the gutter, a
 * debug log) rather than its error branch (the toast).
 */
function emptyGitDiff(path: string) {
  return {
    message: 'success',
    path,
    has_staged: false,
    has_unstaged: false,
    staged_diff: '',
    unstaged_diff: '',
    diff: '',
    original_content: '',
    modified_content: '',
  };
}

/** GitHub's git endpoints take a token as Basic auth, not Bearer. */
function gitHubTokenAuth(token: string): string {
  return `Basic ${btoa(`x-access-token:${token}`)}`;
}

/**
 * Auth headers for a request to remoteURL. The configured token is a GitHub
 * token, so it is only sent to github.com; for other hosts the platform's git
 * proxy adds the credential the user connected for that host.
 */
function getAuth(remoteURL: string | null | undefined) {
  const token = bridge()?.token;
  if (token && isGitHubURL(remoteURL)) {
    return { headers: { Authorization: gitHubTokenAuth(token) } };
  }
  return undefined;
}

function isGitHubURL(remoteURL: string | null | undefined): boolean {
  if (!remoteURL) return false;
  try {
    return new URL(remoteURL).host.toLowerCase() === 'github.com';
  } catch {
    return false;
  }
}

/** The origin remote's URL, or null when there is none. */
async function originURL(): Promise<string | null> {
  try {
    const url = await git.getConfig({ fs: getFs().promises, dir: REPO_DIR, path: 'remote.origin.url' });
    return typeof url === 'string' ? url : null;
  } catch {
    return null;
  }
}

// ── Public API ──────────────────────────────────────────────────

export async function gitStatus() {
  await ensureInitialized();
  await syncVfsToGitFs();

  let matrix: Array<[string, number, number, number]>;
  try {
    matrix = await git.statusMatrix({ fs: getFs().promises, dir: REPO_DIR });
  } catch {
    // Unborn HEAD (fresh git.init with no commits yet): statusMatrix cannot
    // resolve the HEAD tree and throws. Report the working files as
    // untracked instead of surfacing a 500 to the git panel.
    const files = await workTreePaths();
    return {
      staged: [] as Array<{ path: string; status: string; staged: boolean }>,
      unstaged: [] as Array<{ path: string; status: string; staged: boolean }>,
      untracked: files.map((path) => ({ path, status: 'new', staged: false })),
    };
  }

  const staged: Array<{ path: string; status: string; staged: boolean }> = [];
  const unstaged: Array<{ path: string; status: string; staged: boolean }> = [];
  const untracked: Array<{ path: string; status: string; staged: boolean }> = [];

  // isomorphic-git statusMatrix rows are [filepath, HEAD, WORKDIR, STAGE]:
  //   HEAD:    0=absent, 1=present
  //   WORKDIR: 0=absent, 1=identical-to-HEAD, 2=different-from-HEAD
  //   STAGE:   0=absent, 1=identical-to-HEAD, 2=identical-to-WORKDIR,
  //            3=different-from-WORKDIR
  // (see isomorphic-git docs "statusMatrix" reference table)
  for (const [filepath, HEAD, WORKDIR, STAGE] of matrix) {
    // Unmodified: present everywhere and all three copies identical.
    if (HEAD === 1 && WORKDIR === 1 && STAGE === 1) continue;

    if (HEAD === 0) {
      // New file (never committed).
      if (STAGE === 0) {
        // Not in the index → untracked.
        untracked.push({ path: filepath, status: 'new', staged: false });
      } else {
        // In the index → staged addition.
        staged.push({ path: filepath, status: 'new', staged: true });
      }
      continue;
    }

    // File exists in HEAD.
    if (STAGE === 0) {
      // Absent from the index → staged deletion (or delete-and-recreate).
      staged.push({ path: filepath, status: 'deleted', staged: true });
      continue;
    }

    if (WORKDIR === 0) {
      // Removed from the working tree but still in the index (index === HEAD).
      unstaged.push({ path: filepath, status: 'deleted', staged: false });
      continue;
    }

    // File present in the workdir (WORKDIR 1|2) and in the index (STAGE 1|2|3).
    if (STAGE === 1) {
      // Index === HEAD, so any workdir change is unstaged.
      if (WORKDIR === 2) {
        unstaged.push({ path: filepath, status: 'modified', staged: false });
      }
    } else {
      // STAGE 2 (index === workdir) or 3 (index !== workdir): staged change.
      staged.push({ path: filepath, status: 'modified', staged: true });
    }
  }

  return { staged, unstaged, untracked };
}

/**
 * Which of the given workspace-relative files differ from the repository's
 * checkout (the files a move-to-container must carry rather than re-clone).
 *
 * The browser repository's checkout is the committed HEAD; the working tree
 * is synced from the VFS before the comparison, so each row's WORKDIR is the
 * VFS's current bytes. A file differs when it is absent from the checkout
 * (a new file, HEAD 0) or its bytes differ from the checkout (WORKDIR 2); a
 * file identical to the checkout differs from nothing.
 *
 * When there is no checkout to compare against — browser git is not wired,
 * the repo is fresh (no commits), or the comparison fails — nothing can be
 * proven to match, so every path is reported as differing: a consumer
 * carries them all rather than skip one the container's clone may not hold.
 */
export async function gitCheckoutDiffers(paths: string[]): Promise<Map<string, boolean>> {
  const result = new Map<string, boolean>();
  if (paths.length === 0) return result;

  // Browser git was never wired (a VFS seeded without the git objects, or a
  // local daemon build): there is no checkout to compare against, so nothing
  // can be proven to match. Answer without touching the repository.
  if (!getBrowserGitVfsBridge()) {
    for (const p of paths) result.set(p, true);
    return result;
  }

  let matrix: Array<[string, number, number, number]>;
  try {
    await ensureInitialized();
    await syncVfsToGitFs();
    matrix = await git.statusMatrix({ fs: getFs().promises, dir: REPO_DIR });
  } catch {
    // No checkout to compare (a fresh repo, or the sync failed): carry them all.
    for (const p of paths) result.set(p, true);
    return result;
  }

  const byPath = new Map<string, [string, number, number, number]>();
  for (const row of matrix) byPath.set(row[0], row);
  for (const p of paths) {
    const row = byPath.get(p);
    if (!row) {
      // Not in the repository at all: it cannot match the checkout.
      result.set(p, true);
      continue;
    }
    const [, head, workdir] = row;
    result.set(p, head === 0 || workdir === 2);
  }
  return result;
}

export async function gitAdd(filepaths: string[]) {
  await ensureInitialized();
  await syncVfsToGitFs();
  for (const filepath of filepaths) {
    await git.add({ fs: getFs().promises, dir: REPO_DIR, filepath });
  }
  return { message: 'ok', staged: filepaths.length };
}

export async function gitCommit(message: string) {
  await ensureInitialized();
  await syncVfsToGitFs();
  const sha = await git.commit({
    fs: getFs().promises,
    dir: REPO_DIR,
    message,
    author: {
      name: bridge()?.name || 'Browser IDE',
      email: bridge()?.email || 'browser-ide@sprout.dev',
    },
  });
  return { message: 'ok', sha };
}

export async function gitLog(count = 50) {
  await ensureInitialized();
  const commits: Array<{ hash: string; message: string; author: string; date: string }> = [];
  try {
    const entries = await git.log({ fs: getFs().promises, dir: REPO_DIR, depth: count, ref: 'HEAD' });
    for (const entry of entries) {
      commits.push({
        hash: entry.oid,
        message: entry.commit.message,
        author: entry.commit.author.name,
        date: new Date(entry.commit.author.timestamp * 1000).toISOString(),
      });
    }
  } catch {
    // best-effort: no commits yet.
  }
  return commits;
}

export async function gitBranch() {
  await ensureInitialized();
  const branches: Array<{ name: string; current: boolean }> = [];
  try {
    const refs = await git.listBranches({ fs: getFs().promises, dir: REPO_DIR });
    const current = await git.currentBranch({ fs: getFs().promises, dir: REPO_DIR, fullname: false }).catch(() => null);
    for (const ref of refs) {
      branches.push({ name: ref, current: ref === current });
    }
  } catch {
    // no commits
  }
  return branches;
}

/** Current branch name ('' when the repo has no commits yet). */
async function currentBranchName(): Promise<string> {
  try {
    const current = await git.currentBranch({ fs: getFs().promises, dir: REPO_DIR, fullname: false }).catch(() => null);
    return current ?? '';
  } catch {
    return '';
  }
}

export async function gitCheckout(branch: string) {
  await ensureInitialized();
  await git.checkout({ fs: getFs().promises, dir: REPO_DIR, ref: branch });
  await syncGitFsToVfs();
  return { message: 'ok', branch };
}

export async function gitDiff(opts?: { path?: string; cached?: boolean }) {
  await ensureInitialized();
  await syncVfsToGitFs();
  const matrix = await git.statusMatrix({ fs: getFs().promises, dir: REPO_DIR });
  const changes: Array<{ path: string; type: string; content: string }> = [];

  for (const [filepath, HEAD, WORKDIR] of matrix) {
    if (opts?.path && filepath !== opts.path) continue;
    if (HEAD === 0 && WORKDIR === 1) {
      const content = await getFs().promises.readFile(`${REPO_DIR}/${filepath}`, 'utf8');
      changes.push({ path: filepath, type: 'added', content: String(content) });
    } else if (HEAD === 1 && WORKDIR === 0) {
      changes.push({ path: filepath, type: 'deleted', content: '' });
    } else if (HEAD !== WORKDIR) {
      const content = await getFs().promises.readFile(`${REPO_DIR}/${filepath}`, 'utf8');
      changes.push({ path: filepath, type: 'modified', content: String(content) });
    }
  }
  return changes;
}

/**
 * Working-tree diff for one file against HEAD, in the GitDiffResponse shape
 * the diff view renders (text diff plus both full versions for the merge
 * view). Browser git has no index distinct from the working tree, so every
 * change is reported as unstaged.
 */
export async function gitFileDiff(path: string) {
  await ensureInitialized();
  await syncVfsToGitFs();
  const fs = getFs().promises;
  const rel = path
    .replace(/^\/workspace\//, '')
    .replace(/^\/+/, '')
    .replace(/^repo\//, '');

  let original = '';
  try {
    const head = await git.resolveRef({ fs, dir: REPO_DIR, ref: 'HEAD' });
    const { blob } = await git.readBlob({ fs, dir: REPO_DIR, oid: head, filepath: rel });
    original = new TextDecoder().decode(blob);
  } catch {
    // New file, or no commits yet: the HEAD side is empty.
  }
  let modified = '';
  try {
    modified = String(await fs.readFile(`${REPO_DIR}/${rel}`, 'utf8'));
  } catch {
    // Deleted in the working tree: the new side is empty.
  }

  const body = generateUnifiedDiff(original, modified, `a/${rel}`, `b/${rel}`);
  let diff = '';
  if (body) {
    // generateUnifiedDiff lists the whole file after its ---/+++ headers;
    // present that as one hunk so the diff parser gets line numbers.
    const lines = body.split('\n');
    const fileHeaders = lines.slice(0, 2).join('\n');
    const hunkBody = lines.slice(2).join('\n');
    const oldLen = original === '' ? 0 : original.split('\n').length;
    const newLen = modified === '' ? 0 : modified.split('\n').length;
    diff =
      `diff --git a/${rel} b/${rel}\n${fileHeaders}\n` +
      `@@ -${oldLen ? 1 : 0},${oldLen} +${newLen ? 1 : 0},${newLen} @@\n${hunkBody}`;
  }
  return {
    message: 'success',
    path: rel,
    has_staged: false,
    has_unstaged: diff !== '',
    staged_diff: '',
    unstaged_diff: diff,
    diff,
    original_content: original,
    modified_content: modified,
  };
}

/** The browser repository's origin URL, or null when there is no repository or remote. */
export async function gitOriginUrl(): Promise<string | null> {
  try {
    const remotes = await git.listRemotes({ fs: getFs().promises, dir: REPO_DIR });
    return remotes.find((r) => r.remote === 'origin')?.url ?? null;
  } catch {
    return null;
  }
}

export interface GitCloneOptions {
  token?: string;
  /** Clone progress, as isomorphic-git reports it (phase, loaded, total). */
  onProgress?: ProgressCallback;
  /** Stop the download past this many bytes (CloneTooLargeError). */
  maxBytes?: number;
}

export async function gitClone(url: string, opts?: GitCloneOptions) {
  const fs = getFs().promises;
  // The previous repository's files leave the workspace with it, so two
  // repositories never mix in one tree.
  const previousFiles = await workTreePaths();
  if (previousFiles.length > 0) await bridge()?.deleteVfsFiles?.(previousFiles);
  await clearRepoDir();

  await ensureDir(REPO_DIR);
  // Per-call auth (authenticated clone from the GitHub repo picker) wins over
  // the module-level config token. The token is passed straight through to
  // isomorphic-git as a request header — it is never stored or logged here.
  const headers: Record<string, string> = { ...(getAuth(url)?.headers ?? {}) };
  if (opts?.token && isGitHubURL(url)) {
    headers.Authorization = gitHubTokenAuth(opts.token);
  }
  const limited = opts?.maxBytes ? byteLimitedHttp(http, opts.maxBytes) : null;
  try {
    await git.clone({
      fs,
      http: limited ?? http,
      corsProxy: gitCorsProxy(),
      dir: REPO_DIR,
      url,
      depth: 1,
      singleBranch: true,
      headers: Object.keys(headers).length > 0 ? headers : undefined,
      onProgress: opts?.onProgress,
    });
  } catch (err) {
    if (limited?.exceeded && opts?.maxBytes) {
      await clearRepoDir();
      throw new CloneTooLargeError(opts.maxBytes);
    }
    throw err;
  }
  markRepoInitialized();
  const files = await syncGitFsToVfs();
  let branch: string | null = null;
  try {
    branch = (await git.currentBranch({ fs, dir: REPO_DIR })) ?? null;
  } catch {
    // best-effort: detached HEAD reports no branch.
  }
  return { message: 'ok', url, branch, files };
}

export async function gitPush(remote = 'origin', branch?: string) {
  await ensureInitialized();
  const fs = getFs().promises;
  const remotes = await git.listRemotes({ fs, dir: REPO_DIR });
  if (!remotes.some((r) => r.remote === remote)) {
    if (await snapshotSource()) {
      throw new Error(
        'This workspace was imported without git history, so it has no remote to push to. Run the push from a cloud workspace instead.',
      );
    }
    throw new Error(
      `This repository has no "${remote}" remote to push to. Open the repository from its git host (Files › Add repository) to push.`,
    );
  }
  const ref = branch || (await git.currentBranch({ fs, dir: REPO_DIR })) || undefined;
  try {
    await git.push({
      fs,
      http,
      corsProxy: gitCorsProxy(),
      dir: REPO_DIR,
      remote,
      ref,
      headers: getAuth(await originURL())?.headers,
    });
  } catch (err) {
    throw new Error(describePushError(err));
  }
  return { message: 'ok', pushed: true };
}

/** Plain-language reason for a failed push; GitHub's auth failures surface as HTTP 401/403. */
function describePushError(err: unknown): string {
  const status = (err as { data?: { statusCode?: number } })?.data?.statusCode;
  if (status === 401 || status === 403) {
    return 'The git host rejected the push. Connect an account with write access to this repository (Settings › Git providers), then try again.';
  }
  const message = err instanceof Error ? err.message : String(err);
  if (/not a simple fast-forward|rejected/i.test(message)) {
    return "The remote has commits you don't have yet. Pull is not available in the browser; run the push from a cloud container instead.";
  }
  return `Push failed: ${message}`;
}

/**
 * Fast-forward the current branch from its remote. Only a clean working tree
 * is pulled, so no local edit can be overwritten; a diverged branch is
 * reported rather than merged.
 */
export async function gitPull() {
  const status = await gitStatus();
  if (status.staged.length > 0 || status.unstaged.length > 0) {
    throw new Error('Commit or undo your changes before pulling — pull only updates a clean working tree.');
  }
  const fs = getFs().promises;
  const ref = (await git.currentBranch({ fs, dir: REPO_DIR })) || undefined;
  const before = new Set(await workTreePaths());
  try {
    await git.fastForward({
      fs,
      http,
      corsProxy: gitCorsProxy(),
      dir: REPO_DIR,
      ref,
      singleBranch: true,
      headers: getAuth(await originURL())?.headers,
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    if (/fast-?forward/i.test(message)) {
      throw new Error('Your branch and the remote have diverged; merging is not available in the browser.');
    }
    throw new Error(`Pull failed: ${message}`);
  }
  await syncGitFsToVfs();
  const after = new Set(await workTreePaths());
  const removed = [...before].filter((p) => !after.has(p));
  if (removed.length > 0) await bridge()?.deleteVfsFiles?.(removed);
  return { message: 'ok', pulled: true };
}

export async function gitCreateBranch(name: string) {
  await ensureInitialized();
  const branch = (name ?? '').trim();
  if (!branch) throw new Error('Enter a branch name.');
  await git.branch({ fs: getFs().promises, dir: REPO_DIR, ref: branch, checkout: true });
  return { message: 'ok', branch };
}

export async function gitInit() {
  await ensureDir(REPO_DIR);
  await git.init({ fs: getFs().promises, dir: REPO_DIR });
  markRepoInitialized();
  return { message: 'ok', initialized: true };
}

/**
 * Update the remote-tracking refs without touching the working tree.
 * isomorphic-git has no standalone "fetch command"; git.fetch is the
 * primitive. This is what makes a PR branch reachable after a shallow clone.
 */
export async function gitFetch(opts?: {
  remote?: string;
  ref?: string;
  depth?: number;
  tags?: boolean;
  prune?: boolean;
}) {
  await ensureInitialized();
  const fs = getFs().promises;
  const remote = opts?.remote || 'origin';
  await git.fetch({
    fs,
    http,
    corsProxy: gitCorsProxy(),
    dir: REPO_DIR,
    remote,
    ref: opts?.ref,
    singleBranch: false,
    depth: opts?.depth,
    tags: opts?.tags ?? false,
    prune: opts?.prune ?? false,
    headers: getAuth(await originURL())?.headers,
  });
  return { message: 'ok', fetched: true };
}

/**
 * The remote-tracking branches under <remote>/ that are reachable locally.
 * Reads the packed/loose refs isomorphic-git can resolve, so it reflects the
 * most recent fetch.
 */
export async function gitRemoteBranches(remote = 'origin'): Promise<string[]> {
  await ensureInitialized();
  try {
    const refs = await git.listBranches({ fs: getFs().promises, dir: REPO_DIR, remote });
    return refs.map((b) => `${remote}/${b}`);
  } catch {
    // No remote configured (or no fetched refs yet): report none rather than
    // failing the caller.
    return [];
  }
}

/** Stage one or more removals (git rm); the VFS files are deleted too. */
export async function gitRemove(filepaths: string[]) {
  await ensureInitialized();
  await syncVfsToGitFs();
  for (const filepath of filepaths) {
    await git.remove({ fs: getFs().promises, dir: REPO_DIR, filepath });
  }
  await bridge()?.deleteVfsFiles?.(filepaths);
  return { message: 'ok', removed: filepaths.length };
}

/** Move/rename a tracked file (git mv), reflecting the move in the VFS. */
export async function gitMove(from: string, to: string) {
  await ensureInitialized();
  await syncVfsToGitFs();
  const fs = getFs().promises;
  await ensureDir(`${REPO_DIR}/${to}`.substring(0, `${REPO_DIR}/${to}`.lastIndexOf('/')));
  try {
    await fs.rename(`${REPO_DIR}/${from}`, `${REPO_DIR}/${to}`);
  } catch {
    // Fall back to read+write+unlink when rename isn't available.
    const content = String(await fs.readFile(`${REPO_DIR}/${from}`, 'utf8'));
    await fs.writeFile(`${REPO_DIR}/${to}`, content, 'utf8');
    await fs.unlink(`${REPO_DIR}/${from}`);
  }
  // Reflect the move in the VFS: the destination is written, the source removed.
  const moved = String(await fs.readFile(`${REPO_DIR}/${to}`, 'utf8'));
  await bridge()?.writeVfsFiles?.([{ path: to, content: moved }]);
  await bridge()?.deleteVfsFiles?.([from]);
  return { message: 'ok', from, to };
}

/** git list-branches helper mirroring the shell's ref view. */
export async function gitConfigGet(key: string): Promise<string | null> {
  await ensureInitialized();
  try {
    const value = await git.getConfig({ fs: getFs().promises, dir: REPO_DIR, path: key });
    return typeof value === 'string' ? value : null;
  } catch {
    // best-effort: an unset key reads as null.
    return null;
  }
}

/** Set a git config value (e.g. user.name) on the browser repo. */
export async function gitConfigSet(key: string, value: string): Promise<void> {
  await ensureInitialized();
  await git.setConfig({ fs: getFs().promises, dir: REPO_DIR, path: key, value });
}

/**
 * Git operations that browser mode does NOT support.
 *
 * Used by the UI to disable/hide buttons in cloud mode so users never
 * get a "not yet supported in browser mode" 500 error from a click that
 * looked like it would work. Kept in sync with the `executeGitOp` switch
 * below — any case that throws an "unsupported" error should be listed
 * here.
 */
export const BROWSER_GIT_UNSUPPORTED_OPS: ReadonlySet<string> = new Set([
  'unstage',
  'unstage-all',
  'reset',
  'discard',
  'revert',
  'commit-message',
  'pull-request',
  'show',
]);

export async function gitStageAll() {
  await ensureInitialized();
  await syncVfsToGitFs();
  const matrix = await git.statusMatrix({ fs: getFs().promises, dir: REPO_DIR });
  for (const [filepath, , WORKDIR] of matrix) {
    if (WORKDIR === 1) {
      await git.add({ fs: getFs().promises, dir: REPO_DIR, filepath });
    }
  }
  return { message: 'ok' };
}

/**
 * Execute a git operation by name (maps to the proxy API shape).
 *
 * Capability matrix for browser mode:
 *   ✓ status, add/stage, stage-all, commit, log, branch/branches, checkout,
 *     diff, push, clone, init
 *   ✗ unstage, unstage-all, reset, pull, discard, revert, commit-message,
 *     pull-request, show
 *
 * Unimplemented ops throw an Error with an honest "not yet supported in
 * browser mode" message rather than faking success. The handler in
 * browserGitHandler.ts surfaces that as an HTTP 500 error response, so the UI
 * shows a real failure instead of silently doing nothing.
 */
export async function executeGitOp(
  op: string,
  body?: Record<string, unknown>,
  query?: Record<string, string>,
): Promise<unknown> {
  switch (op) {
    case 'status': {
      // Boot-time guard: browser git is only wired up after the WASM shell is
      // ready (configureBrowserGit in useAppInitialization). The git panel's
      // initial status load runs before that, so without this the call would
      // throw "browserGit not configured" → HTTP 500 → a "Failed to load git
      // status" console error + transient error banner. Report an honest
      // empty-repo state instead; the ?repo= import's refresh re-fetches the
      // real status once browser git is wired and the repo is seeded.
      if (!getBrowserGitVfsBridge()) {
        return emptyGitStatus();
      }
      // HTTP surface: the git panel expects the canonical GitStatusResponse
      // (message/in_git_repo/status/files), the same contract as the local
      // daemon's /api/git/status. The internal gitStatus() shape
      // ({staged, unstaged, untracked}) stays for the shell/txn consumers.
      const s = await gitStatus();
      const branch = await currentBranchName();
      const note = (await snapshotSource()) ? snapshotNote() : undefined;
      const modified = s.unstaged.filter((f) => f.status !== 'new' && f.status !== 'deleted');
      const deleted = s.unstaged.filter((f) => f.status === 'deleted');
      return {
        message: 'success',
        in_git_repo: true,
        status: {
          branch,
          ahead: 0,
          behind: 0,
          staged: s.staged,
          modified,
          untracked: s.untracked,
          deleted,
          renamed: [] as Array<{ path: string; status: string; staged?: boolean }>,
          in_git_repo: true,
        },
        files: [...s.staged, ...modified, ...s.untracked, ...deleted],
        ...(note ? { note } : {}),
      };
    }
    case 'add':
    case 'stage': {
      const files = (body?.files as string[]) || (body?.path ? [body.path as string] : []);
      return gitAdd(files);
    }
    case 'stage-all':
      return gitStageAll();
    case 'commit':
      return gitCommit((body?.message as string) || 'commit');
    case 'log': {
      // HTTP surface: GitLogResponse. The UI pages with ?limit=&offset=;
      // shell consumers keep calling gitLog() for the bare list.
      const limit = Math.max(1, Number(query?.limit ?? body?.count ?? 50));
      const offset = Math.max(0, Number(query?.offset ?? 0));
      const all = await gitLog(offset + limit + 1);
      const page = all.slice(offset, offset + limit);
      return {
        message: 'success',
        commits: page.map((c) => ({ ...c, short_hash: c.hash.slice(0, 7) })),
        offset,
        limit,
        // One extra commit was fetched to tell whether another page exists.
        total: all.length > offset + limit ? offset + limit + 1 : all.length,
      };
    }
    case 'branch':
    case 'branches': {
      // Boot-time guard (matches the 'status' case above): before browser git
      // is wired, gitBranch() would throw → HTTP 500. Return an honest empty
      // branch list instead; the panel re-fetches after the ?repo= import.
      if (!getBrowserGitVfsBridge()) {
        return { message: 'success', current: '', branches: [] as string[] };
      }
      // HTTP surface: GitBranchesResponse (message/current/branches:string[]).
      // The internal gitBranch() returns [{name, current}] — shell consumers
      // keep using that directly.
      const list = await gitBranch();
      const current = list.find((b) => b.current)?.name ?? '';
      return { message: 'success', current, branches: list.map((b) => b.name) };
    }
    case 'checkout':
      return gitCheckout((body?.branch as string) || (body?.name as string));
    case 'branch/create':
      return gitCreateBranch(body?.name as string);
    case 'diff':
      // Boot-time guard (matches the 'status'/'branches' cases above): before
      // browser git is wired (configureBrowserGit — the full app's boot, or
      // the standalone escalation page, or a repository import, which wires
      // it itself) there is no VFS bridge and no .git to diff. Return an
      // honest empty diff (the gutter simply shows nothing, no error)
      // instead of throwing — a throw
      // surfaces as a 500 and the editor toasts "Failed to fetch git diff"
      // on every file open. Once browser git is wired (full-app mode), the
      // guard falls through to the real diff; a genuine diff failure there
      // still errors, which is the honest signal.
      if (!getBrowserGitVfsBridge()) {
        // Per-file HTTP surface (the panel always asks per path): an honest
        // empty GitDiffResponse. The no-path branch (the shell's change-list
        // diff, not the UI's per-file ask) answers with an empty list.
        return query?.path ? emptyGitDiff(query.path) : [];
      }
      // HTTP surface: GitDiffResponse for one file (the panel always asks per
      // path); shell consumers keep using gitDiff() for the change list.
      if (query?.path) return gitFileDiff(query.path);
      return gitDiff({ cached: query?.cached === 'true' });
    case 'push':
      return gitPush(body?.remote as string, body?.branch as string);
    case 'clone':
      return gitClone(body?.url as string);
    case 'init':
      return gitInit();
    // `show` is used to render a single commit's detail. Browser git can only
    // return the bare log entry, not the full file-diff detail the UI expects,
    // so report it as unsupported rather than returning a half-shape object.
    case 'show':
      throw new Error('show is not yet supported in browser mode');
    // The following operations require index/working-tree manipulation that
    // isomorphic-git does not expose in the way this app needs. They are
    // intentionally NOT faked — returning success would silently corrupt the
    // user's mental model of repo state.
    case 'unstage':
    case 'unstage-all':
    case 'reset':
      throw new Error('unstage/reset is not yet supported in browser mode');
    case 'discard':
      throw new Error('discard is not yet supported in browser mode');
    case 'pull':
      return gitPull();
    case 'revert':
      throw new Error('revert is not yet supported in browser mode');
    case 'commit-message':
      throw new Error('commit-message generation is not yet supported in browser mode');
    case 'pull-request':
      throw new Error('pull requests are not yet supported in browser mode');
    default:
      throw new Error(`Unsupported git operation: ${op}`);
  }
}
