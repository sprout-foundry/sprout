/**
 * Tests for browserGit executeGitOp dispatch.
 *
 * Verifies the browser-git capability contract: implemented ops run the real
 * git helpers, and unimplemented ops throw an honest error instead of faking
 * success. The isomorphic-git + lightning-fs backends are mocked out so the
 * dispatch logic can be exercised without an IndexedDB backend (jsdom does not
 * provide one).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// ── Mocks (before importing the module under test) ──────────────────
//
// browserGit imports lightning-fs and isomorphic-git at module top level.
// Mock both so importing the module does not touch IndexedDB.
//
// vi.mock factories are hoisted above all imports, so the shared mock fns
// are created via vi.hoisted (also hoisted) and referenced from the
// factories by the same variable name.

const {
  mockGitAdd,
  mockGitCommit,
  mockGitStatusMatrix,
  mockGitInit,
  mockGitLog,
  mockGitPush,
  mockReadBlob,
  mockFsReadFile,
  mockCurrentBranch,
  mockListRemotes,
  mockGitBranch,
} = vi.hoisted(() => ({
  mockGitBranch: vi.fn(),
  mockCurrentBranch: vi.fn(),
  mockListRemotes: vi.fn(),
  mockReadBlob: vi.fn(),
  mockFsReadFile: vi.fn(),
  mockGitAdd: vi.fn(),
  mockGitCommit: vi.fn(),
  mockGitStatusMatrix: vi.fn(),
  mockGitInit: vi.fn(),
  mockGitLog: vi.fn(),
  mockGitPush: vi.fn(),
}));

vi.mock('@isomorphic-git/lightning-fs', () => {
  const promises = {
    mkdir: vi.fn().mockResolvedValue(undefined),
    stat: vi.fn().mockRejectedValue(new Error('not found')),
    readdir: vi.fn().mockResolvedValue([]),
    readFile: (...args: unknown[]) => mockFsReadFile(...args),
    writeFile: vi.fn().mockResolvedValue(undefined),
    unlink: vi.fn().mockResolvedValue(undefined),
  };
  return {
    default: class MockFS {
      promises = promises;
    },
  };
});

vi.mock('isomorphic-git', () => ({
  init: mockGitInit,
  add: mockGitAdd,
  commit: mockGitCommit,
  statusMatrix: mockGitStatusMatrix,
  log: mockGitLog,
  push: mockGitPush,
  setConfig: vi.fn(),
  listBranches: vi.fn().mockResolvedValue([]),
  currentBranch: (...args: unknown[]) => mockCurrentBranch(...args),
  listRemotes: (...args: unknown[]) => mockListRemotes(...args),
  branch: (...args: unknown[]) => mockGitBranch(...args),
  checkout: vi.fn(),
  clone: vi.fn(),
  resolveRef: vi.fn().mockResolvedValue('head-oid'),
  readBlob: mockReadBlob,
}));

vi.mock('isomorphic-git/http/web', () => ({ default: {} }));

// ── Imports ──────────────────────────────────────────────────────────

import { configureBrowserGit, executeGitOp, __resetBrowserGitForTest } from './browserGit';

describe('executeGitOp dispatch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCurrentBranch.mockResolvedValue(null);
    mockListRemotes.mockResolvedValue([{ remote: 'origin', url: 'https://github.com/o/n.git' }]);
    // Provide a no-op VFS bridge so ensureInitialized/syncVfsToGitFs succeed.
    configureBrowserGit({
      name: 'Test',
      email: 'test@example.com',
      readVfsFiles: async () => [],
      writeVfsFiles: async () => {},
    });
    mockGitStatusMatrix.mockResolvedValue([]);
    mockGitLog.mockResolvedValue([]);
    mockGitInit.mockResolvedValue(true);
    mockGitAdd.mockResolvedValue(undefined);
    mockGitCommit.mockResolvedValue('deadbeef');
    mockGitPush.mockResolvedValue(undefined);
  });

  describe('implemented operations run the real helpers', () => {
    it('status resolves and returns the canonical GitStatusResponse shape', async () => {
      const result = (await executeGitOp('status')) as {
        message: string;
        in_git_repo: boolean;
        status: { branch: string; ahead: number; behind: number; staged: unknown[]; untracked: unknown[] };
        files: unknown[];
      };
      // The git panel consumes the daemon-compatible contract (message +
      // in_git_repo + nested status), not the raw staged/unstaged shape.
      expect(result.message).toBe('success');
      expect(result.in_git_repo).toBe(true);
      expect(result.status).toMatchObject({ branch: '', ahead: 0, behind: 0 });
      expect(Array.isArray(result.status.staged)).toBe(true);
      expect(Array.isArray(result.status.untracked)).toBe(true);
      expect(Array.isArray(result.files)).toBe(true);
    });

    it('branches resolves and returns the canonical GitBranchesResponse shape', async () => {
      const result = (await executeGitOp('branches')) as {
        message: string;
        current: string;
        branches: string[];
      };
      expect(result.message).toBe('success');
      expect(result.current).toBe('');
      expect(result.branches).toEqual([]);
    });

    it('add/stage delegates to gitAdd', async () => {
      const result = await executeGitOp('add', { files: ['a.txt'] });
      expect(mockGitAdd).toHaveBeenCalledWith(expect.objectContaining({ filepath: 'a.txt' }));
      expect(result).toHaveProperty('staged', 1);
    });

    it('commit delegates to gitCommit and returns the sha', async () => {
      const result = await executeGitOp('commit', { message: 'msg' });
      expect(mockGitCommit).toHaveBeenCalled();
      expect(result).toEqual(expect.objectContaining({ sha: 'deadbeef' }));
    });

    it('log answers the GitLogResponse page the history panel reads', async () => {
      mockGitLog.mockResolvedValueOnce(
        ['c3', 'c2', 'c1'].map((oid, i) => ({
          oid: `${oid}0000000000`,
          commit: { message: `m${i}`, author: { name: 'A', timestamp: 1_790_000_000 + i } },
        })),
      );
      const result = (await executeGitOp('log', undefined, { limit: '2', offset: '0' })) as {
        commits: Array<{ hash: string; short_hash: string }>;
        limit: number;
        offset: number;
        total: number;
      };
      expect(result.commits.map((c) => c.short_hash)).toEqual(['c300000', 'c200000']);
      expect(result).toMatchObject({ limit: 2, offset: 0, total: 3 });
    });

    it('diff for a path answers a GitDiffResponse with both versions', async () => {
      mockGitStatusMatrix.mockResolvedValue([]);
      mockReadBlob.mockResolvedValueOnce({ blob: new TextEncoder().encode('Hello World!\n') });
      mockFsReadFile.mockResolvedValue('Hello World!\nmore\n');
      const result = (await executeGitOp('diff', undefined, { path: '/README' })) as {
        path: string;
        diff: string;
        has_unstaged: boolean;
        original_content: string;
        modified_content: string;
      };
      expect(result.path).toBe('README');
      expect(result.has_unstaged).toBe(true);
      expect(result.original_content).toBe('Hello World!\n');
      expect(result.modified_content).toBe('Hello World!\nmore\n');
      expect(result.diff).toContain('diff --git a/README b/README');
      expect(result.diff).toContain('@@ ');
      expect(result.diff).toContain('+more');
    });

    it('push delegates to gitPush', async () => {
      await executeGitOp('push', { remote: 'origin', branch: 'main' });
      expect(mockGitPush).toHaveBeenCalled();
    });

    it('push defaults to the current branch, not "main"', async () => {
      mockCurrentBranch.mockResolvedValue('master');
      await executeGitOp('push', {});
      expect(mockGitPush).toHaveBeenCalledWith(expect.objectContaining({ remote: 'origin', ref: 'master' }));
    });

    it('push explains a missing remote instead of failing inside git', async () => {
      mockListRemotes.mockResolvedValue([]);
      await expect(executeGitOp('push', {})).rejects.toThrow(/no "origin" remote/);
      expect(mockGitPush).not.toHaveBeenCalled();
    });

    it('branch/create creates and checks out the branch', async () => {
      await expect(executeGitOp('branch/create', { name: ' feature/x ' })).resolves.toEqual({
        message: 'ok',
        branch: 'feature/x',
      });
      expect(mockGitBranch).toHaveBeenCalledWith(expect.objectContaining({ ref: 'feature/x', checkout: true }));
    });

    it('push explains a GitHub auth rejection', async () => {
      mockGitPush.mockRejectedValueOnce(Object.assign(new Error('HTTP Error: 403'), { data: { statusCode: 403 } }));
      await expect(executeGitOp('push', {})).rejects.toThrow(/GitHub rejected the push/);
    });
  });

  describe('unimplemented operations throw honest errors (no fake success)', () => {
    // Each of these previously returned a fake { message: 'ok' } success.
    // They must now reject so the handler surfaces a real HTTP error instead
    // of silently appearing to succeed.
    const unsupportedOps = [
      'unstage',
      'unstage-all',
      'reset',
      'discard',
      'pull',
      'revert',
      'commit-message',
      'pull-request',
      'show',
    ];

    for (const op of unsupportedOps) {
      it(`${op} rejects with an honest "not yet supported" message`, async () => {
        await expect(executeGitOp(op, {})).rejects.toThrow(/not yet supported/i);
      });
    }

    it('an unknown op rejects (rather than returning an { error } object)', async () => {
      await expect(executeGitOp('totally-fake-op')).rejects.toThrow(/Unsupported git operation/);
    });

    it('no op returns a fake-success { message: "ok" } for stubbed cases', async () => {
      // Guard against regression: the old stub returned { message: 'ok' }
      // for unstage/reset/unstage-all. Ensure that shape is gone.
      for (const op of ['unstage', 'reset', 'unstage-all']) {
        let caught: unknown;
        try {
          await executeGitOp(op, {});
        } catch (e) {
          caught = e;
        }
        expect(caught).toBeInstanceOf(Error);
      }
    });
  });
});

// ── Unborn-HEAD (no commits) tolerance ─────────────────────────────
//
// After a ?repo= import the browser repo is a fresh git.init with no
// commits: statusMatrix cannot resolve the HEAD tree and rejects. gitStatus
// must fall back to "all working files are untracked" instead of throwing
// (a throw surfaces as a 500 → the git panel's "Failed to fetch git
// status" banner).

import LightningFS from '@isomorphic-git/lightning-fs';

describe('git status with no commits (unborn HEAD)', () => {
  const fs = new LightningFS();
  const readdirMock = fs.promises.readdir as unknown as { mockResolvedValue(v: unknown[]): void };
  const statMock = fs.promises.stat as unknown as {
    mockResolvedValue(v: unknown): void;
    mockRejectedValue(e: unknown): void;
  };

  afterEach(() => {
    // Restore the factory defaults so other suites are unaffected.
    readdirMock.mockResolvedValue([]);
    statMock.mockRejectedValue(new Error('not found'));
  });

  it('reports working files as untracked when statusMatrix cannot resolve HEAD', async () => {
    mockGitStatusMatrix.mockRejectedValue(new Error('Invalid ref: HEAD'));
    readdirMock.mockResolvedValue(['README', 'src.ts']);
    statMock.mockResolvedValue({ isDirectory: () => false });

    const result = (await executeGitOp('status')) as {
      message: string;
      in_git_repo: boolean;
      status: { untracked: Array<{ path: string; status: string }> };
      files: unknown[];
    };
    expect(result.message).toBe('success');
    expect(result.in_git_repo).toBe(true);
    expect(result.status.untracked).toEqual([
      { path: 'README', status: 'new', staged: false },
      { path: 'src.ts', status: 'new', staged: false },
    ]);
    expect(result.files).toHaveLength(2);
  });

  it('resolves to an empty state when the repo is empty', async () => {
    mockGitStatusMatrix.mockRejectedValue(new Error('Invalid ref: HEAD'));
    // readdir → [] (default), stat rejects (default): no files listed.
    const result = (await executeGitOp('status')) as {
      status: { untracked: unknown[]; staged: unknown[]; modified: unknown[] };
    };
    expect(result.status.untracked).toEqual([]);
    expect(result.status.staged).toEqual([]);
    expect(result.status.modified).toEqual([]);
  });
});

// ── Boot-time (not yet wired) state ─────────────────────────────────
//
// The git panel's initial status/branches load runs before the WASM shell
// calls configureBrowserGit (config is still null). The read-only ops must
// return an honest empty-repo response (HTTP 200, in_git_repo:false) rather
// than throw "browserGit not configured" (which the handler surfaces as a
// 500 → the panel's "Failed to load git status" console error). The ?repo=
// import's refresh re-fetches the real status once browser git is wired.

describe('executeGitOp before configureBrowserGit (boot state)', () => {
  it('status returns an honest empty-repo response instead of throwing', async () => {
    __resetBrowserGitForTest();
    const result = (await executeGitOp('status')) as {
      message: string;
      in_git_repo: boolean;
      status: { branch: string; in_git_repo: boolean };
      files: unknown[];
    };
    expect(result.message).toBe('success');
    expect(result.in_git_repo).toBe(false);
    expect(result.status.in_git_repo).toBe(false);
    expect(result.files).toEqual([]);
    // Must not have touched the git helpers.
    expect(mockGitStatusMatrix).not.toHaveBeenCalled();
  });

  it('branches returns an honest empty list instead of throwing', async () => {
    __resetBrowserGitForTest();
    const result = (await executeGitOp('branches')) as {
      message: string;
      current: string;
      branches: string[];
    };
    expect(result.message).toBe('success');
    expect(result.current).toBe('');
    expect(result.branches).toEqual([]);
  });
});
