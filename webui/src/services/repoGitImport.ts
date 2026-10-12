/**
 * Opening an imported repository in the in-browser workspace as a git
 * repository, so source control (status, diff and the editor gutter, staging,
 * commit, branches, history) works through browser git.
 *
 * - With a git proxy from the host (gitCorsProxy), the repository is cloned:
 *   depth 1, one branch, its history and origin remote included, and the
 *   download capped at MAX_CLONE_BYTES. Push and pull go through the proxy.
 * - Without one, or when the clone fails, the files are imported the plain
 *   way and committed as an "imported snapshot": status and diff work against
 *   that baseline, and the git panel notes that remote history is missing.
 *
 * A reload of the same repository restores the files the VFS lost from the
 * browser repository instead of importing again, keeping local commits.
 * Git operations wait for the import (holdBrowserGitWorkTree), so none of
 * them sees a half-seeded tree.
 */

import { setActiveRepoURL } from './activeRepo';
import type * as BrowserGitModule from './browserGit';
import type { BrowserGitAuthor } from './browserGitBridge';
import { gitCorsProxy } from './gitCorsProxy';
import { MAX_CLONE_BYTES } from './gitHttpLimit';
import { NATIVE_FS_ENABLED } from './nativeFsStubs/nativeFsFlag';
import { NATIVE_GIT_ENABLED } from './nativeGitStubs/nativeGitFlag';
import type { WasmShell } from './wasmShell';
import { GIT_REPO_CHANGED_EVENT } from './workspaceClone';

/** How the imported repository's git state was set up. */
export type RepoImportHistory = 'clone' | 'snapshot' | 'none';

export interface RepoRestoreResult {
  success: boolean;
  repo?: string;
  error?: string;
  fromCache?: boolean;
  history?: RepoImportHistory;
}

export interface RepoGitImportDeps {
  /** The WASM shell whose workspace holds the files. */
  shell: () => Promise<WasmShell>;
  /** The files-only import (the local cache, else the host's import endpoint). */
  importFiles: (repoURL: string) => Promise<RepoRestoreResult>;
  author?: BrowserGitAuthor;
  /** Download limit for the clone (defaults to MAX_CLONE_BYTES). */
  maxCloneBytes?: number;
}

/** Window event carrying clone progress: `{ repo, phase, loaded, total }`. */
export const REPO_IMPORT_PROGRESS_EVENT = 'sprout:repo-import-progress';

type BrowserGit = typeof BrowserGitModule;
type RepoRef = { host: string; owner: string; name: string; url: string };
type ParseRepoRef = (input: string) => RepoRef;

export async function restoreRepoWithBrowserGit(repoURL: string, deps: RepoGitImportDeps): Promise<RepoRestoreResult> {
  // Browser git keeps its store in IndexedDB (lightning-fs).
  if (NATIVE_FS_ENABLED || NATIVE_GIT_ENABLED || typeof indexedDB === 'undefined') {
    return { ...(await deps.importFiles(repoURL)), history: 'none' };
  }
  let bg: BrowserGit;
  let parseRepoRef: ParseRepoRef;
  try {
    const shell = await deps.shell();
    const [gitModule, bridge, workspaceGit] = await Promise.all([
      import('./browserGit'),
      import('./browserGitBridge'),
      import('./workspaceFs/workspaceGit'),
    ]);
    bridge.ensureBrowserGitBridge(shell, deps.author);
    bg = gitModule;
    parseRepoRef = workspaceGit.parseRepoRef;
  } catch (err) {
    console.warn('[repo import] browser git unavailable; importing files only:', err);
    return { ...(await deps.importFiles(repoURL)), history: 'none' };
  }
  return bg.holdBrowserGitWorkTree(openRepo(bg, parseRepoRef, repoURL, deps));
}

async function openRepo(
  bg: BrowserGit,
  parseRepoRef: ParseRepoRef,
  repoURL: string,
  deps: RepoGitImportDeps,
): Promise<RepoRestoreResult> {
  let want: RepoRef | null = null;
  try {
    want = parseRepoRef(repoURL);
  } catch {
    // Not a git-host URL the clone understands; the files import decides.
  }
  const label = want ? `${want.owner}/${want.name}` : repoURL;
  const isSnapshot = await snapshotOfSameRepo(bg, parseRepoRef, repoURL, want);

  if (want && gitCorsProxy() && !isSnapshot) {
    const origin = await bg.gitOriginUrl();
    if (origin !== null && sameRepoRef(origin, want, parseRepoRef)) {
      await bg.restoreGitWorkingTree();
      return opened(repoURL, { success: true, repo: label, fromCache: true, history: 'clone' });
    }
    try {
      await bg.gitClone(want.url, {
        onProgress: progressReporter(label),
        maxBytes: deps.maxCloneBytes ?? MAX_CLONE_BYTES,
      });
      return opened(repoURL, { success: true, repo: label, fromCache: false, history: 'clone' });
    } catch (err) {
      console.warn('[repo import] git clone failed; importing the files as a snapshot:', err);
    }
  }

  if (isSnapshot) {
    await bg.restoreGitWorkingTree();
    return opened(repoURL, { success: true, repo: label, fromCache: true, history: 'snapshot' });
  }

  const result = await deps.importFiles(repoURL);
  if (!result.success) return { ...result, history: 'none' };
  try {
    await bg.gitInitSnapshot(repoURL, result.repo ?? label);
    return opened(repoURL, { ...result, history: 'snapshot' });
  } catch (err) {
    console.warn('[repo import] could not record the imported snapshot in git:', err);
    return { ...result, history: 'none' };
  }
}

function opened(repoURL: string, result: RepoRestoreResult): RepoRestoreResult {
  setActiveRepoURL(repoURL);
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(GIT_REPO_CHANGED_EVENT));
  return result;
}

async function snapshotOfSameRepo(
  bg: BrowserGit,
  parseRepoRef: ParseRepoRef,
  repoURL: string,
  want: RepoRef | null,
): Promise<boolean> {
  const source = await bg.snapshotSource();
  if (!source) return false;
  return want ? sameRepoRef(source, want, parseRepoRef) : source === repoURL;
}

/** Whether a remote URL points at owner/name (case-insensitive, .git optional). */
export function sameRepoRef(
  remoteURL: string,
  want: { host: string; owner: string; name: string },
  parseRepoRef: (input: string) => { host: string; owner: string; name: string },
): boolean {
  // Host and full path must both match: the same owner/name can exist on
  // several hosts, and owner may span segments (GitLab subgroups).
  try {
    const have = parseRepoRef(remoteURL);
    return (
      have.host === want.host &&
      have.owner.toLowerCase() === want.owner.toLowerCase() &&
      have.name.toLowerCase() === want.name.toLowerCase()
    );
  } catch {
    return false;
  }
}

/** Clone progress as window events, at most every 100 ms within a phase. */
function progressReporter(repo: string) {
  let lastPhase = '';
  let lastAt = 0;
  return (p: { phase: string; loaded: number; total: number }) => {
    const now = Date.now();
    if (p.phase === lastPhase && now - lastAt < 100 && p.loaded !== p.total) return;
    lastPhase = p.phase;
    lastAt = now;
    if (typeof window === 'undefined') return;
    window.dispatchEvent(
      new CustomEvent(REPO_IMPORT_PROGRESS_EVENT, {
        detail: { repo, phase: p.phase, loaded: p.loaded, total: p.total },
      }),
    );
  };
}
