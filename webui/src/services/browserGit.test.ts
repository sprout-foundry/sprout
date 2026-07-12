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
} = vi.hoisted(() => ({
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
    readFile: vi.fn().mockResolvedValue(''),
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
  currentBranch: vi.fn().mockResolvedValue(null),
  checkout: vi.fn(),
  clone: vi.fn(),
}));

vi.mock('isomorphic-git/http/web', () => ({ default: {} }));

// ── Imports ──────────────────────────────────────────────────────────

import { configureBrowserGit, executeGitOp } from './browserGit';

describe('executeGitOp dispatch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
    it('status resolves and returns a staged/unstaged shape', async () => {
      const result = await executeGitOp('status');
      expect(result).toHaveProperty('staged');
      expect(result).toHaveProperty('unstaged');
      expect(result).toHaveProperty('untracked');
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

    it('log delegates to gitLog', async () => {
      const result = await executeGitOp('log', { count: 5 });
      expect(mockGitLog).toHaveBeenCalled();
      expect(Array.isArray(result)).toBe(true);
    });

    it('push delegates to gitPush', async () => {
      await executeGitOp('push', { remote: 'origin', branch: 'main' });
      expect(mockGitPush).toHaveBeenCalled();
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
