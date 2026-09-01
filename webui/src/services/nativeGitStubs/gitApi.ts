/**
 * Track R (--native-git, R-4) stub for services/api/gitApi.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/api/gitApi for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_GIT=1 (scripts/build-webui-dist.mjs --native-git). The
 * git client API is provided natively by the shell, so the real module
 * (getGitStatus, getGitBranches, checkoutGitBranch, createGitBranch, pullGit,
 * pushGit, stageFile, unstageFile, discardChanges, stageAll, unstageAll,
 * createCommit, generateCommitMessage, getGitLog, getGitCommitDetail,
 * getGitCommitFileDiff, checkoutGitCommit, revertGitCommit, getGitDiff,
 * createPullRequest — all over fetchFn('/api/git/...')) is hard-excluded
 * from the bundle.
 *
 * This file is a no-op stand-in: it mirrors the REAL public signatures of
 * `gitApi` (copied from the real module so `tsc --noEmit` passes in the
 * --native-git build too) but every function is a safe no-op that NEVER
 * issues a fetch, NEVER logs, and never touches the network. It resolves
 * inert values matching the real return types. The type-only imports from
 * `../api/types` are erased at compile time, so they pull no real code into
 * the bundle.
 *
 * DECISION (documented in docs/adr-0008-webui-native-seams.md, git seam):
 * only the client API surface (this module) is aliased. The deeper
 * gitClient/browserGit modules are NOT aliased here — browserGit's working
 * tree IS the WASM VFS (via the configureBrowserGit callbacks) and gitClient
 * shares the lightning-fs IndexedDB namespace with browserGit, so a deeper
 * alias is unsafe for a *seam*. The seam is the API surface + the
 * compile-time short-circuits of the boot wiring (useAppInitialization).
 * See docs/WEBUI_DECOUPLING_AUDIT.md §1.4. In a --native-git dist the shell
 * owns git, so the webui never issues git HTTP itself.
 */

import type {
  GitStatusResponse,
  GitBranchesResponse,
  GitBranchResponse,
  GitPushPullResponse,
  GitStageResponse,
  GitStageAllResponse,
  GitCommitResponse,
  GitCommitMessageResponse,
  GitLogResponse,
  GitCommitDetailResponse,
  GitCommitFileDiffResponse,
  GitDiffResponse,
  PullRequestResponse,
} from '../api/types';

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/status. Resolves an inert GitStatusResponse-shaped value (no
 * network, no files).
 */
export async function getGitStatus(_fetchFn: typeof fetch): Promise<GitStatusResponse> {
  return {
    message: 'Git provided by the native shell',
    in_git_repo: false,
    status: {
      branch: '',
      ahead: 0,
      behind: 0,
      staged: [],
      modified: [],
      untracked: [],
      deleted: [],
      renamed: [],
      in_git_repo: false,
    },
    files: [],
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/branches. Resolves an inert GitBranchesResponse-shaped value.
 */
export async function getGitBranches(_fetchFn: typeof fetch): Promise<GitBranchesResponse> {
  return {
    message: 'Git provided by the native shell',
    current: '',
    branches: [],
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/checkout. Resolves an inert GitBranchResponse-shaped value.
 */
export async function checkoutGitBranch(_fetchFn: typeof fetch, _branch: string): Promise<GitBranchResponse> {
  return {
    message: 'Git provided by the native shell',
    branch: _branch,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/branch/create. Resolves an inert GitBranchResponse-shaped value.
 */
export async function createGitBranch(_fetchFn: typeof fetch, name: string): Promise<GitBranchResponse> {
  return {
    message: 'Git provided by the native shell',
    branch: name,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/pull. Resolves an inert GitPushPullResponse-shaped value.
 */
export async function pullGit(_fetchFn: typeof fetch): Promise<GitPushPullResponse> {
  return {
    message: 'Git provided by the native shell',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/push. Resolves an inert GitPushPullResponse-shaped value.
 */
export async function pushGit(_fetchFn: typeof fetch): Promise<GitPushPullResponse> {
  return {
    message: 'Git provided by the native shell',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/stage. Resolves an inert GitStageResponse-shaped value.
 */
export async function stageFile(_fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  return {
    message: 'Git provided by the native shell',
    path,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/unstage. Resolves an inert GitStageResponse-shaped value.
 */
export async function unstageFile(_fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  return {
    message: 'Git provided by the native shell',
    path,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/discard. Resolves an inert GitStageResponse-shaped value.
 */
export async function discardChanges(_fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  return {
    message: 'Git provided by the native shell',
    path,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/stage-all. Resolves an inert GitStageAllResponse-shaped value.
 */
export async function stageAll(_fetchFn: typeof fetch): Promise<GitStageAllResponse> {
  return {
    message: 'Git provided by the native shell',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/unstage-all. Resolves an inert GitStageAllResponse-shaped value.
 */
export async function unstageAll(_fetchFn: typeof fetch): Promise<GitStageAllResponse> {
  return {
    message: 'Git provided by the native shell',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/commit. Resolves an inert GitCommitResponse-shaped value.
 */
export async function createCommit(
  _fetchFn: typeof fetch,
  _message: string,
  _files?: string[],
): Promise<GitCommitResponse> {
  return {
    message: 'Git provided by the native shell',
    commit: '',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/commit-message. Resolves an inert GitCommitMessageResponse-shaped
 * value.
 */
export async function generateCommitMessage(_fetchFn: typeof fetch): Promise<GitCommitMessageResponse> {
  return {
    message: 'Git provided by the native shell',
    commit_message: '',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/log. Resolves an inert GitLogResponse-shaped value.
 */
export async function getGitLog(
  _fetchFn: typeof fetch,
  limit: number,
  offset: number,
  _opts?: { signal?: AbortSignal },
): Promise<GitLogResponse> {
  return {
    message: 'Git provided by the native shell',
    commits: [],
    offset,
    limit,
    total: 0,
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/commit/show. Resolves an inert GitCommitDetailResponse-shaped
 * value.
 */
export async function getGitCommitDetail(_fetchFn: typeof fetch, _hash: string): Promise<GitCommitDetailResponse> {
  return {
    message: 'Git provided by the native shell',
    hash: _hash,
    short_hash: '',
    author: '',
    date: '',
    subject: '',
    files: [],
    diff: '',
    stats: '',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/commit/show/file. Resolves an inert GitCommitFileDiffResponse-
 * shaped value.
 */
export async function getGitCommitFileDiff(
  _fetchFn: typeof fetch,
  hash: string,
  path: string,
): Promise<GitCommitFileDiffResponse> {
  return {
    message: 'Git provided by the native shell',
    hash,
    path,
    diff: '',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/checkout for a commit. Resolves an inert message-only value.
 */
export async function checkoutGitCommit(_fetchFn: typeof fetch, _commitHash: string): Promise<{ message: string }> {
  return { message: 'Git provided by the native shell' };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/revert. Resolves an inert message-only value.
 */
export async function revertGitCommit(_fetchFn: typeof fetch, _commitHash: string): Promise<{ message: string }> {
  return { message: 'Git provided by the native shell' };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/diff. Resolves an inert GitDiffResponse-shaped value.
 */
export async function getGitDiff(_fetchFn: typeof fetch, path: string): Promise<GitDiffResponse> {
  return {
    message: 'Git provided by the native shell',
    path,
    has_staged: false,
    has_unstaged: false,
    staged_diff: '',
    unstaged_diff: '',
    diff: '',
  };
}

/**
 * Track R: git is provided natively by the shell, so the webui never calls
 * /api/git/pull-request. Resolves an inert PullRequestResponse-shaped value.
 */
export async function createPullRequest(
  _fetchFn: typeof fetch,
  _params: { title: string; body?: string; base?: string; head?: string; draft?: boolean },
): Promise<PullRequestResponse> {
  return {
    success: false,
    url: '',
    number: 0,
    state: 'Git provided by the native shell',
  };
}
