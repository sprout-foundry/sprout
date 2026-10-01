import { gitCorsProxy } from './gitCorsProxy';
import type { CloneResult } from './workspaceFs/workspaceGit';

/** Fired on window after a clone replaces or adds a repository. */
export const GIT_REPO_CHANGED_EVENT = 'sprout:git-repo-changed';

function announceRepoChanged(): void {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(GIT_REPO_CHANGED_EVENT));
}

/**
 * Clone a repository into the workspace the way this build stores git.
 *
 * The hosted browser IDE keeps git objects in lightning-fs (IndexedDB is
 * binary-safe; the WASM VFS is string-backed) and mirrors the working tree
 * into the VFS, with the workspace root as the repository — the model the
 * Git panel and terminal git use. Other builds (native shells, local daemon)
 * clone through the workspaceFs seam into repos/<owner>/<name>/.
 */
export async function cloneIntoWorkspace(url: string, opts: { token?: string } = {}): Promise<CloneResult> {
  const { cloneRepo, parseRepoRef } = await import('./workspaceFs/workspaceGit');
  if (!gitCorsProxy()) {
    const cloned = await cloneRepo(url, opts);
    announceRepoChanged();
    return cloned;
  }

  const { owner, name } = parseRepoRef(url);
  const { gitClone } = await import('./browserGit');
  const result = await gitClone(url, opts);
  announceRepoChanged();
  return { repo: `${owner}/${name}`, dir: '', entries: result.files, defaultBranch: result.branch };
}
