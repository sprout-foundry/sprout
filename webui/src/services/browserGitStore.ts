/**
 * The browser repository's storage: the lightning-fs (IndexedDB) volume that
 * holds `.git` and a copy of the working tree, the VFS bridge the app wires
 * in, and the mirror between that copy and the WASM VFS (the files the
 * editor, the agent and its shell see).
 *
 * The VFS is the source of truth for the working tree; before a git operation
 * its files are copied into the repository, and after one that rewrites the
 * tree (clone, checkout, pull) the repository's files are copied back. Files
 * the VFS cannot hold as text (binary, or larger than MAX_MIRROR_BYTES) stay
 * in the repository only and are left alone by the VFS → repository copy, so
 * they never show up as deleted.
 */

import FS from '@isomorphic-git/lightning-fs';
import * as git from 'isomorphic-git';

const FS_NAME = 'sprout-git';
export const REPO_DIR = '/repo';

/** Largest file mirrored into the VFS (the file import's per-file limit). */
export const MAX_MIRROR_BYTES = 1_000_000;

let pfsInstance: FS | null = null;

export function getFs(): FS {
  if (!pfsInstance) {
    pfsInstance = new FS(FS_NAME);
  }
  return pfsInstance;
}

export interface BrowserGitConfig {
  token?: string;
  name?: string;
  email?: string;
  readVfsFiles: () => Promise<Array<{ path: string; content: string }>>;
  writeVfsFiles: (files: Array<{ path: string; content: string }>) => Promise<void>;
  deleteVfsFiles?: (paths: string[]) => Promise<void>;
}

let config: BrowserGitConfig | null = null;
let repoInitialized = false;

/** Repository files that are not mirrored into the VFS (binary or too large). */
const gitOnlyPaths = new Set<string>();

/** An import that is (re)building the repository; git operations wait for it. */
let pendingWorkTree: Promise<unknown> | null = null;

// Resolves once the app wires the VFS bridge; boot-time work (the ?repo=
// deep link) starts before that and waits on it.
let markConfigured: () => void = () => undefined;
let configured = new Promise<void>((resolve) => {
  markConfigured = resolve;
});

export function configureBrowserGit(cfg: BrowserGitConfig) {
  config = cfg;
  repoInitialized = false;
  markConfigured();
}

/** Wait for configureBrowserGit, failing after timeoutMs. */
export function whenBrowserGitConfigured(timeoutMs = 30000): Promise<void> {
  if (config) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('browser git was not configured in time')), timeoutMs);
    void configured.then(() => {
      clearTimeout(timer);
      resolve();
    });
  });
}

/** Test-only: restore the pre-bootstrap (never-configured) state. */
export function __resetBrowserGitForTest(): void {
  config = null;
  repoInitialized = false;
  gitOnlyPaths.clear();
  pendingWorkTree = null;
  configured = new Promise<void>((resolve) => {
    markConfigured = resolve;
  });
}

/**
 * The VFS bridge configureBrowserGit was called with, or null when browser
 * git was never wired up (local mode / pre-bootstrap). The ETH-2 txn path
 * uses it to enumerate files for push and write pulled files back.
 */
export function getBrowserGitVfsBridge(): BrowserGitConfig | null {
  return config;
}

export function markRepoInitialized(): void {
  repoInitialized = true;
}

/**
 * Hold git operations until `work` settles: a repository import replaces the
 * working tree in both stores, and a status taken halfway through would copy a
 * half-seeded VFS over the repository.
 */
export function holdBrowserGitWorkTree<T>(work: Promise<T>): Promise<T> {
  const settled = work.then(
    () => undefined,
    () => undefined,
  );
  pendingWorkTree = settled;
  void settled.then(() => {
    if (pendingWorkTree === settled) pendingWorkTree = null;
  });
  return work;
}

async function workTreeReady(): Promise<void> {
  while (pendingWorkTree) await pendingWorkTree;
}

export async function ensureDir(path: string) {
  const fs = getFs().promises;
  // lightning-fs mkdir creates one level: make each ancestor in turn, or a
  // file two folders deep under a new folder fails with ENOENT.
  const parts = path.split('/').filter(Boolean);
  for (let i = 1; i <= parts.length; i++) {
    try {
      await fs.mkdir(`/${parts.slice(0, i).join('/')}`);
    } catch {
      // best-effort: may already exist.
    }
  }
}

export async function ensureInitialized() {
  if (repoInitialized) return;
  const fs = getFs().promises;
  await ensureDir(REPO_DIR);

  try {
    await fs.stat(`${REPO_DIR}/.git/config`);
    repoInitialized = true;
    return;
  } catch {
    // best-effort: not initialized — fall through to git.init.
  }

  await git.init({ fs: getFs().promises, dir: REPO_DIR });
  await setAuthorConfig();
  repoInitialized = true;
}

export async function setAuthorConfig(): Promise<void> {
  if (config?.name) {
    await git.setConfig({ fs: getFs().promises, dir: REPO_DIR, path: 'user.name', value: config.name });
  }
  if (config?.email) {
    await git.setConfig({ fs: getFs().promises, dir: REPO_DIR, path: 'user.email', value: config.email });
  }
}

export async function readdirRecursive(dir: string, prefix = ''): Promise<string[]> {
  const fs = getFs().promises;
  const results: string[] = [];
  let entries: string[];
  try {
    entries = await fs.readdir(dir);
  } catch {
    // best-effort: unreadable directory contributes no entries.
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
      // best-effort: skip entries that can't be stat'd.
    }
  }
  return results;
}

const isGitPath = (relPath: string) => relPath === '.git' || relPath.startsWith('.git/');

/** Remove everything under the repository directory, `.git` included. */
export async function clearRepoDir(): Promise<void> {
  const fs = getFs().promises;
  for (const relPath of await readdirRecursive(REPO_DIR)) {
    try {
      await fs.unlink(`${REPO_DIR}/${relPath}`);
    } catch {
      // best-effort: already gone.
    }
  }
  gitOnlyPaths.clear();
  repoInitialized = false;
}

/** The repository's working-tree paths (everything outside `.git`). */
export async function workTreePaths(): Promise<string[]> {
  return (await readdirRecursive(REPO_DIR)).filter((p) => !isGitPath(p));
}

/**
 * Copy the VFS working tree into the repository, waiting for an import in
 * progress first. Git operations call this before reading the tree.
 */
export async function syncVfsToGitFs() {
  await workTreeReady();
  await copyVfsToGitFs();
}

/** The VFS → repository copy itself; imports call it directly (no wait). */
export async function copyVfsToGitFs() {
  if (!config) throw new Error('browserGit not configured — call configureBrowserGit first');
  const fs = getFs().promises;
  const files = await config.readVfsFiles();
  const inVfs = new Set(files.map((f) => f.path));

  for (const relPath of await workTreePaths()) {
    if (gitOnlyPaths.has(relPath) && !inVfs.has(relPath)) continue;
    try {
      await fs.unlink(`${REPO_DIR}/${relPath}`);
    } catch {
      // best-effort: already gone.
    }
  }

  for (const file of files) {
    const fullPath = `${REPO_DIR}/${file.path}`;
    const dir = fullPath.substring(0, fullPath.lastIndexOf('/'));
    if (dir && dir !== REPO_DIR) {
      await ensureDir(dir);
    }
    await fs.writeFile(fullPath, file.content, 'utf8');
    gitOnlyPaths.delete(file.path);
  }
}

/** A repository file as VFS text, or null when it stays in the repository only. */
async function readMirrorable(relPath: string): Promise<string | null> {
  const raw = (await getFs().promises.readFile(`${REPO_DIR}/${relPath}`)) as Uint8Array | string;
  if (typeof raw === 'string') return raw;
  if (raw.byteLength > MAX_MIRROR_BYTES) return null;
  try {
    const text = new TextDecoder('utf-8', { fatal: true }).decode(raw);
    return text.includes('\0') ? null : text;
  } catch {
    return null;
  }
}

/**
 * Copy repository files into the VFS. With `onlyMissing`, files the VFS
 * already holds (including edits not yet seen by git) are left alone.
 * Returns how many files were written.
 */
export async function syncGitFsToVfs(opts?: { onlyMissing?: boolean }): Promise<number> {
  if (!config) throw new Error('browserGit not configured');
  const present = opts?.onlyMissing ? new Set((await config.readVfsFiles()).map((f) => f.path)) : null;
  const files: Array<{ path: string; content: string }> = [];
  for (const relPath of await workTreePaths()) {
    if (present?.has(relPath)) continue;
    try {
      const content = await readMirrorable(relPath);
      if (content === null) {
        gitOnlyPaths.add(relPath);
        continue;
      }
      gitOnlyPaths.delete(relPath);
      files.push({ path: relPath, content });
    } catch {
      // best-effort: skip unreadable files.
    }
  }
  if (files.length > 0) await config.writeVfsFiles(files);
  return files.length;
}

/**
 * Put the repository's files back into the VFS where they are missing,
 * leaving every file already there (including unsaved-to-git edits) alone.
 * Returns how many files were written.
 */
export function restoreGitWorkingTree(): Promise<number> {
  return syncGitFsToVfs({ onlyMissing: true });
}
