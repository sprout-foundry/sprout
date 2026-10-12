/**
 * A local repository for a repository imported as plain files.
 *
 * When the host offers no git proxy (or the clone through it fails), the
 * import brings the files without their history. The workspace still gets a
 * real repository: one "imported snapshot" commit of exactly the files the
 * VFS holds, so status, diff, staging and commit work against that baseline.
 * There is no origin remote — the snapshot shares no history with the real
 * repository, so pushing it would be rejected — and the git panel says so.
 */

import * as git from 'isomorphic-git';
import {
  REPO_DIR,
  clearRepoDir,
  copyVfsToGitFs,
  ensureDir,
  getBrowserGitVfsBridge,
  getFs,
  markRepoInitialized,
  setAuthorConfig,
} from './browserGitStore';

/** Git config key recording which repository the snapshot was imported from. */
const SNAPSHOT_CONFIG = 'sprout.snapshot';

/** The repository URL a snapshot repository was imported from, or null. */
export async function snapshotSource(): Promise<string | null> {
  try {
    const value = await git.getConfig({ fs: getFs().promises, dir: REPO_DIR, path: SNAPSHOT_CONFIG });
    return typeof value === 'string' && value ? value : null;
  } catch {
    return null;
  }
}

/** The git panel's note for a snapshot repository. */
export function snapshotNote(): string {
  return (
    'Imported without git history: changes are tracked against the imported files. ' +
    "Remote history, pull and push aren't available in this in-browser workspace."
  );
}

/**
 * Replace the browser repository with one commit of the files the VFS holds
 * now, recorded as a snapshot of `repoURL`. Returns the commit's sha.
 */
export async function gitInitSnapshot(repoURL: string, label: string): Promise<string> {
  const bridge = getBrowserGitVfsBridge();
  if (!bridge) throw new Error('browserGit not configured');
  const fs = getFs().promises;
  await clearRepoDir();
  await ensureDir(REPO_DIR);
  await git.init({ fs, dir: REPO_DIR, defaultBranch: 'main' });
  await setAuthorConfig();
  const source = withoutCredentials(repoURL);
  await git.setConfig({ fs, dir: REPO_DIR, path: SNAPSHOT_CONFIG, value: source });
  markRepoInitialized();

  await copyVfsToGitFs();
  const matrix = await git.statusMatrix({ fs, dir: REPO_DIR });
  const paths = matrix.map(([filepath]) => filepath);
  if (paths.length > 0) await git.add({ fs, dir: REPO_DIR, filepath: paths });
  return git.commit({
    fs,
    dir: REPO_DIR,
    message: `Imported snapshot of ${label}\n\nImported as files without git history from ${source}.`,
    author: {
      name: bridge.name || 'Browser IDE',
      email: bridge.email || 'browser-ide@sprout.dev',
    },
  });
}

/** A URL with any user/password part removed, so no credential lands in .git/config. */
function withoutCredentials(repoURL: string): string {
  try {
    const url = new URL(repoURL);
    url.username = '';
    url.password = '';
    return url.toString();
  } catch {
    return repoURL;
  }
}
