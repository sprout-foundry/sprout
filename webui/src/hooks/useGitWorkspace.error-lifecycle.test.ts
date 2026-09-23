/**
 * Tests for useGitWorkspace's git status load lifecycle — specifically that a
 * successful status load clears a stale error banner from an earlier failed
 * attempt (regression: the boot call that fires before the in-browser WASM
 * shell is ready set gitActionError, and the post-import refresh populated
 * the panel data without ever clearing the banner).
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';

const getGitStatus = vi.fn();
const getGitBranches = vi.fn();

vi.mock('../services/api/gitApi', () => ({
  getGitStatus: (...args: unknown[]) => getGitStatus(...args),
  getGitBranches: (...args: unknown[]) => getGitBranches(...args),
}));

vi.mock('../contexts/EventsContext', () => ({
  useEvents: () => ({
    onEvent: vi.fn(),
    removeEvent: vi.fn(),
  }),
}));

const logError = vi.fn();
vi.mock('../utils/log', () => ({
  useLog: () => ({
    debug: vi.fn(),
    error: logError,
    warn: vi.fn(),
    info: vi.fn(),
  }),
  debugLog: vi.fn(),
  warn: vi.fn(),
}));

import { useGitWorkspace, type UseGitWorkspaceOptions } from './useGitWorkspace';

function makeOptions(): UseGitWorkspaceOptions {
  const noop = () => undefined;
  return {
    fetchFn: fetch,
    gitRefreshToken: 0,
    selectedGitFilePath: null,
    onViewChange: noop,
    onGitCommit: noop,
    onGitAICommit: noop,
    onGitStage: noop,
    onGitUnstage: noop,
    onGitDiscard: noop,
    openWorkspaceBuffer: noop,
  };
}

const options = makeOptions();

const okStatus = {
  message: 'success',
  in_git_repo: true,
  status: {
    branch: 'master',
    ahead: 0,
    behind: 0,
    staged: [] as string[],
    modified: ['README'],
    untracked: [] as string[],
    deleted: [] as string[],
    renamed: [] as string[],
  },
};

const okBranches = { message: 'success', current: 'master', branches: ['master'] };

function setupResponses() {
  getGitStatus.mockResolvedValue(okStatus);
  getGitBranches.mockResolvedValue(okBranches);
}

beforeEach(() => {
  vi.clearAllMocks();
  setupResponses();
});

describe('useGitWorkspace — gitActionError lifecycle (stale banner after recovery)', () => {
  it('clears a stale gitActionError when a later status load succeeds', async () => {
    // First attempt(s) fail (e.g. the boot call before the WASM shell is ready).
    // The hook fires several loads on mount, so use a persistent reject —
    // not mockRejectedValueOnce, which a second mount load would consume
    // and the success path would immediately clear the banner.
    getGitStatus.mockRejectedValue(new Error('Failed to fetch git status'));

    const { result } = renderHook(() => useGitWorkspace(options));

    await waitFor(() => expect(result.current.gitActionError).toBe('Failed to fetch git status'));
    expect(result.current.gitStatus).toBeNull();

    // Recovery attempt succeeds (post-import refresh)
    setupResponses();
    act(() => {
      result.current.refreshGitStatus();
    });

    await waitFor(() => expect(result.current.gitStatus).not.toBeNull());
    await waitFor(() => expect(result.current.gitActionError).toBeNull());
    expect(result.current.gitStatus?.branch).toBe('master');
    expect(result.current.gitStatus?.modified).toEqual(['README']);
  });

  it('clears a stale error when the workspace reports no git repository', async () => {
    getGitStatus.mockRejectedValue(new Error('Failed to fetch git status'));

    const { result } = renderHook(() => useGitWorkspace(options));
    await waitFor(() => expect(result.current.gitActionError).toBe('Failed to fetch git status'));

    // Subsequent load succeeds but reports no repo
    getGitStatus.mockResolvedValue({ message: 'success', in_git_repo: false });
    act(() => {
      result.current.refreshGitStatus();
    });

    await waitFor(() => expect(result.current.gitStatus).toBeNull());
    await waitFor(() => expect(result.current.gitActionError).toBeNull());
  });

  it('keeps the error banner when the status load fails', async () => {
    getGitStatus.mockRejectedValue(new Error('Failed to fetch git status'));

    const { result } = renderHook(() => useGitWorkspace(options));

    await waitFor(() => expect(result.current.gitActionError).toBe('Failed to fetch git status'));
    expect(result.current.gitStatus).toBeNull();
  });
});
