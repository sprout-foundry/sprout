/**
 * Browser-native git using isomorphic-git + lightning-fs.
 *
 * In cloud mode, git runs entirely in the browser. This service:
 * - Uses lightning-fs (IndexedDB-backed) for the git repository state
 * - Syncs files between the WASM VFS and lightning-fs before/after git ops
 * - Supports: init, status, add, commit, log, branch, checkout, push, pull, diff
 * - GitHub push/pull via the user's token or anonymous HTTPS clone
 */

import FS from '@isomorphic-git/lightning-fs';
import * as git from 'isomorphic-git';
import http from 'isomorphic-git/http/web';

const FS_NAME = 'sprout-git';
const REPO_DIR = '/repo';

let pfsInstance: FS | null = null;

function getFs(): FS {
  if (!pfsInstance) {
    pfsInstance = new FS(FS_NAME);
  }
  return pfsInstance;
}

let repoInitialized = false;

export interface BrowserGitConfig {
  token?: string;
  name?: string;
  email?: string;
  readVfsFiles: () => Promise<Array<{ path: string; content: string }>>;
  writeVfsFiles: (files: Array<{ path: string; content: string }>) => Promise<void>;
}

let config: BrowserGitConfig | null = null;

export function configureBrowserGit(cfg: BrowserGitConfig) {
  config = cfg;
  repoInitialized = false;
}

async function ensureDir(path: string) {
  const fs = getFs().promises;
  try {
    await fs.mkdir(path);
  } catch {
    // may already exist
  }
}

async function ensureInitialized() {
  if (repoInitialized) return;
  const fs = getFs().promises;
  await ensureDir(REPO_DIR);

  try {
    await fs.stat(`${REPO_DIR}/.git/config`);
    repoInitialized = true;
    return;
  } catch {
    // not initialized
  }

  await git.init({ fs: getFs().promises, dir: REPO_DIR });

  if (config?.name) {
    await git.setConfig({ fs: getFs().promises, dir: REPO_DIR, path: 'user.name', value: config.name });
  }
  if (config?.email) {
    await git.setConfig({ fs: getFs().promises, dir: REPO_DIR, path: 'user.email', value: config.email });
  }
  repoInitialized = true;
}

async function readdirRecursive(dir: string, prefix = ''): Promise<string[]> {
  const fs = getFs().promises;
  const results: string[] = [];
  let entries: string[];
  try {
    entries = await fs.readdir(dir);
  } catch {
    return results;
  }
  for (const entry of entries) {
    const fullPath = `${dir}/${entry}`;
    const relPath = prefix ? `${prefix}/${entry}` : entry;
    try {
      const stat = await fs.stat(fullPath);
      if (stat.isDirectory()) {
        results.push(...(await readdirRecursive(fullPath, relPath)));
      } else {
        results.push(relPath);
      }
    } catch {
      // skip
    }
  }
  return results;
}

async function syncVfsToGitFs() {
  if (!config) throw new Error('browserGit not configured — call configureBrowserGit first');
  const fs = getFs().promises;
  const files = await config.readVfsFiles();

  // Clear non-.git files in repo dir
  try {
    const existing = await readdirRecursive(REPO_DIR);
    for (const relPath of existing) {
      if (!relPath.startsWith('.git/') && !relPath.startsWith('.git')) {
        try {
          await fs.unlink(`${REPO_DIR}/${relPath}`);
        } catch {
          /* gone */
        }
      }
    }
  } catch {
    // fresh repo
  }

  // Write VFS files
  for (const file of files) {
    const fullPath = `${REPO_DIR}/${file.path}`;
    const dir = fullPath.substring(0, fullPath.lastIndexOf('/'));
    if (dir && dir !== REPO_DIR) {
      await ensureDir(dir);
    }
    await fs.writeFile(fullPath, file.content, 'utf8');
  }
}

async function syncGitFsToVfs() {
  if (!config) throw new Error('browserGit not configured');
  const entries = await readdirRecursive(REPO_DIR);
  const files: Array<{ path: string; content: string }> = [];
  for (const relPath of entries) {
    if (relPath.startsWith('.git')) continue;
    try {
      const content = await getFs().promises.readFile(`${REPO_DIR}/${relPath}`, 'utf8');
      files.push({ path: relPath, content: String(content) });
    } catch {
      // skip binary/unreadable
    }
  }
  await config.writeVfsFiles(files);
}

function getAuth() {
  if (config?.token) {
    return { headers: { Authorization: `Bearer ${config.token}` } };
  }
  return undefined;
}

// ── Public API ──────────────────────────────────────────────────

export async function gitStatus() {
  await ensureInitialized();
  await syncVfsToGitFs();
  const matrix = await git.statusMatrix({ fs: getFs().promises, dir: REPO_DIR });

  const staged: Array<{ path: string; status: string; staged: boolean }> = [];
  const unstaged: Array<{ path: string; status: string; staged: boolean }> = [];

  for (const [filepath, HEAD, WORKDIR, STAGE] of matrix) {
    if (HEAD === 0 && WORKDIR === 1 && STAGE === 2) {
      staged.push({ path: filepath, status: 'new', staged: true });
    } else if (HEAD === 0 && WORKDIR === 1) {
      unstaged.push({ path: filepath, status: 'new', staged: false });
    } else if (HEAD === 1 && WORKDIR === 0) {
      unstaged.push({ path: filepath, status: 'deleted', staged: false });
    } else if (HEAD !== WORKDIR && STAGE === HEAD) {
      unstaged.push({ path: filepath, status: 'modified', staged: false });
    } else if (STAGE !== HEAD && STAGE !== WORKDIR) {
      staged.push({ path: filepath, status: 'modified', staged: true });
    }
  }

  return { staged, unstaged, untracked: unstaged.filter((f) => f.status === 'new') };
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
      name: config?.name || 'Browser IDE',
      email: config?.email || 'browser-ide@sprout.dev',
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
    // no commits yet
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

export async function gitClone(url: string) {
  const fs = getFs().promises;
  // Clear existing repo contents
  try {
    const existing = await readdirRecursive(REPO_DIR);
    for (const relPath of existing) {
      try {
        await fs.unlink(`${REPO_DIR}/${relPath}`);
      } catch {
        /* gone */
      }
    }
  } catch {
    /* fresh */
  }

  await ensureDir(REPO_DIR);
  await git.clone({
    fs,
    http,
    dir: REPO_DIR,
    url,
    depth: 1,
    singleBranch: true,
    headers: getAuth()?.headers,
  });
  repoInitialized = true;
  await syncGitFsToVfs();
  return { message: 'ok', url };
}

export async function gitPush(remote = 'origin', branch = 'main') {
  await ensureInitialized();
  await git.push({
    fs: getFs().promises,
    http,
    dir: REPO_DIR,
    remote,
    ref: branch,
    headers: getAuth()?.headers,
  });
  return { message: 'ok', pushed: true };
}

export async function gitInit() {
  await ensureDir(REPO_DIR);
  await git.init({ fs: getFs().promises, dir: REPO_DIR });
  repoInitialized = true;
  return { message: 'ok', initialized: true };
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
  'pull',
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
    case 'status':
      return gitStatus();
    case 'add':
    case 'stage': {
      const files = (body?.files as string[]) || (body?.path ? [body.path as string] : []);
      return gitAdd(files);
    }
    case 'stage-all':
      return gitStageAll();
    case 'commit':
      return gitCommit((body?.message as string) || 'commit');
    case 'log':
      return gitLog(Number(body?.count ?? 50));
    case 'branch':
    case 'branches':
      return gitBranch();
    case 'checkout':
      return gitCheckout((body?.branch as string) || (body?.name as string));
    case 'diff':
      return gitDiff({ path: query?.path, cached: query?.cached === 'true' });
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
      throw new Error('pull is not yet supported in browser mode');
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
