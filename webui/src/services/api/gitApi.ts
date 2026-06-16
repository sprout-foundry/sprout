/**
 * Git domain API — adapter-aware git operations.
 *
 * All functions accept a fetch function (from useSproutFetch or clientFetch)
 * so they work in both local and cloud modes.
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
} from './types';

export async function getGitStatus(fetchFn: typeof fetch): Promise<GitStatusResponse> {
  const response = await fetchFn('/api/git/status');
  if (!response.ok) throw new Error('Failed to fetch git status');
  return response.json();
}

export async function getGitBranches(fetchFn: typeof fetch): Promise<GitBranchesResponse> {
  const response = await fetchFn('/api/git/branches');
  if (!response.ok) throw new Error('Failed to fetch branches');
  return response.json();
}

export async function checkoutGitBranch(fetchFn: typeof fetch, branch: string): Promise<GitBranchResponse> {
  const response = await fetchFn('/api/git/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ branch }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Checkout failed' }));
    throw new Error(data.message || data.error || 'Checkout failed');
  }
  return response.json();
}

export async function createGitBranch(fetchFn: typeof fetch, name: string): Promise<GitBranchResponse> {
  const response = await fetchFn('/api/git/branch/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Create branch failed' }));
    throw new Error(data.message || data.error || 'Failed to create branch');
  }
  return response.json();
}

export async function pullGit(fetchFn: typeof fetch): Promise<GitPushPullResponse> {
  const response = await fetchFn('/api/git/pull', { method: 'POST' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Pull failed' }));
    throw new Error(data.message || data.error || 'Pull failed');
  }
  return response.json();
}

export async function pushGit(fetchFn: typeof fetch): Promise<GitPushPullResponse> {
  const response = await fetchFn('/api/git/push', { method: 'POST' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Push failed' }));
    throw new Error(data.message || data.error || 'Push failed');
  }
  return response.json();
}

export async function stageFile(fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  const response = await fetchFn('/api/git/stage', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Stage failed' }));
    throw new Error(data.message || data.error || 'Failed to stage file');
  }
  return response.json();
}

export async function unstageFile(fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  const response = await fetchFn('/api/git/unstage', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Unstage failed' }));
    throw new Error(data.message || data.error || 'Failed to unstage file');
  }
  return response.json();
}

export async function discardChanges(fetchFn: typeof fetch, path: string): Promise<GitStageResponse> {
  const response = await fetchFn('/api/git/discard', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Discard failed' }));
    throw new Error(data.message || data.error || 'Failed to discard changes');
  }
  return response.json();
}

export async function stageAll(fetchFn: typeof fetch): Promise<GitStageAllResponse> {
  const response = await fetchFn('/api/git/stage-all', { method: 'POST' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Stage all failed' }));
    throw new Error(data.message || data.error || 'Failed to stage all');
  }
  return response.json();
}

export async function unstageAll(fetchFn: typeof fetch): Promise<GitStageAllResponse> {
  const response = await fetchFn('/api/git/unstage-all', { method: 'POST' });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Unstage all failed' }));
    throw new Error(data.message || data.error || 'Failed to unstage all');
  }
  return response.json();
}

export async function createCommit(
  fetchFn: typeof fetch,
  message: string,
  files?: string[],
): Promise<GitCommitResponse> {
  const body: Record<string, unknown> = { message };
  if (files && files.length > 0) {
    body.files = files;
  }
  const response = await fetchFn('/api/git/commit', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Commit failed' }));
    throw new Error(data.message || data.error || 'Failed to create commit');
  }
  return response.json();
}

export async function generateCommitMessage(fetchFn: typeof fetch): Promise<GitCommitMessageResponse> {
  const response = await fetchFn('/api/git/commit-message', {
    method: 'POST',
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Generate commit message failed' }));
    throw new Error(data.message || data.error || 'Failed to generate commit message');
  }
  return response.json();
}

export async function getGitLog(
  fetchFn: typeof fetch,
  limit: number,
  offset: number,
  opts?: { signal?: AbortSignal },
): Promise<GitLogResponse> {
  const response = await fetchFn(`/api/git/log?limit=${limit}&offset=${offset}`, {
    signal: opts?.signal,
  });
  if (!response.ok) throw new Error('Failed to fetch git log');
  return response.json();
}

export async function getGitCommitDetail(fetchFn: typeof fetch, hash: string): Promise<GitCommitDetailResponse> {
  const response = await fetchFn(`/api/git/commit/show?hash=${encodeURIComponent(hash)}`);
  if (!response.ok) throw new Error('Failed to fetch commit detail');
  return response.json();
}

export async function getGitCommitFileDiff(
  fetchFn: typeof fetch,
  hash: string,
  path: string,
): Promise<GitCommitFileDiffResponse> {
  const response = await fetchFn(
    `/api/git/commit/show/file?hash=${encodeURIComponent(hash)}&path=${encodeURIComponent(path)}`,
  );
  if (!response.ok) throw new Error('Failed to fetch commit file diff');
  return response.json();
}

export async function checkoutGitCommit(fetchFn: typeof fetch, commitHash: string): Promise<{ message: string }> {
  const response = await fetchFn('/api/git/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ branch: commitHash }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Checkout commit failed' }));
    throw new Error(data.message || data.error || 'Failed to checkout commit');
  }
  return response.json();
}

export async function revertGitCommit(fetchFn: typeof fetch, commitHash: string): Promise<{ message: string }> {
  const response = await fetchFn('/api/git/revert', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ commit_hash: commitHash }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Revert failed' }));
    throw new Error(data.message || data.error || 'Failed to revert commit');
  }
  return response.json();
}

export async function getGitDiff(fetchFn: typeof fetch, path: string): Promise<GitDiffResponse> {
  const response = await fetchFn(`/api/git/diff?path=${encodeURIComponent(path)}`);
  if (!response.ok) throw new Error('Failed to fetch git diff');
  return response.json();
}

export async function createPullRequest(
  fetchFn: typeof fetch,
  params: { title: string; body?: string; base?: string; head?: string; draft?: boolean },
): Promise<PullRequestResponse> {
  const response = await fetchFn('/api/git/pull-request', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      title: params.title,
      body: params.body || '',
      base: params.base || '',
      head: params.head || '',
      draft: params.draft || false,
    }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ error: 'Pull request creation failed' }));
    throw new Error(data.error || data.message || 'Pull request creation failed');
  }
  return response.json();
}
