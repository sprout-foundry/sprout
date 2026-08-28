/**
 * shellGitAdapter.test.ts — per-subcommand tests for the browser-side git
 * backing of the WASM shell's `git` command.
 *
 * browserGit is mocked: these tests pin the command shapes (status/diff/log/
 * branch/…) and the escalation contract (unknown subcommand → 127).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./browserGit', () => ({
  gitStatus: vi.fn(),
  gitDiff: vi.fn(),
  gitLog: vi.fn(),
  gitBranch: vi.fn(),
  getBrowserGitVfsBridge: vi.fn(),
}));

import {
  SHELL_GIT_SUBCOMMANDS,
  registerShellGitGlobal,
} from './shellGitAdapter';
import { gitStatus, gitDiff, gitLog, gitBranch } from './browserGit';

const mockStatus = vi.mocked(gitStatus);
const mockDiff = vi.mocked(gitDiff);
const mockLog = vi.mocked(gitLog);
const mockBranch = vi.mocked(gitBranch);

beforeEach(() => {
  vi.clearAllMocks();
});

describe('git status', () => {
  it('reports a clean tree', async () => {
    mockStatus.mockResolvedValue({ staged: [], unstaged: [], untracked: [] });
    const r = await SHELL_GIT_SUBCOMMANDS.status([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('nothing to commit, working tree clean');
  });

  it('lists staged and unstaged files', async () => {
    mockStatus.mockResolvedValue({
      staged: [{ path: 'a.go', status: 'modified', staged: true }],
      unstaged: [{ path: 'b.ts', status: 'new', staged: false }],
      untracked: [],
    });
    const r = await SHELL_GIT_SUBCOMMANDS.status([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('a.go');
    expect(r.stdout).toContain('b.ts');
  });

  it('-s prints short-format lines', async () => {
    mockStatus.mockResolvedValue({
      staged: [],
      unstaged: [{ path: 'x.py', status: 'modified', staged: false }],
      untracked: [],
    });
    const r = await SHELL_GIT_SUBCOMMANDS.status(['-s']);
    expect(r.stdout.split('\n')[0]).toBe(' M x.py');
  });
});

describe('git diff', () => {
  it('returns empty for a clean tree', async () => {
    mockDiff.mockResolvedValue([]);
    const r = await SHELL_GIT_SUBCOMMANDS.diff([]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe('');
  });

  it('formats added files as new-file patches', async () => {
    mockDiff.mockResolvedValue([
      { path: 'new.go', type: 'added', content: 'package main\n' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS.diff([]);
    expect(r.stdout).toContain('diff --git a/new.go b/new.go');
    expect(r.stdout).toContain('new file mode 100644');
    expect(r.stdout).toContain('+package main');
  });

  it('formats deleted files', async () => {
    mockDiff.mockResolvedValue([
      { path: 'gone.go', type: 'deleted', content: '' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS.diff([]);
    expect(r.stdout).toContain('deleted file mode 100644');
  });
});

describe('git log', () => {
  it('--oneline prints short entries', async () => {
    mockLog.mockResolvedValue([
      { hash: 'abcdef1234567890', message: 'feat: thing\n\nbody', author: 'A', date: '2026-01-01T00:00:00Z' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS.log(['--oneline']);
    expect(r.stdout).toContain('abcdef1 feat: thing');
  });

  it('default format includes commit metadata', async () => {
    mockLog.mockResolvedValue([
      { hash: 'abcdef1234567890', message: 'msg', author: 'Al <a@b.c>', date: '2026-01-01T00:00:00Z' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS.log([]);
    expect(r.stdout).toContain('commit abcdef1234567890');
    expect(r.stdout).toContain('Author: Al <a@b.c>');
  });

  it('-n limits the count', async () => {
    mockLog.mockResolvedValue([]);
    await SHELL_GIT_SUBCOMMANDS.log(['-n', '3']);
    expect(mockLog).toHaveBeenCalledWith(3);
  });
});

describe('git branch', () => {
  it('marks the current branch', async () => {
    mockBranch.mockResolvedValue([
      { name: 'main', current: true },
      { name: 'dev', current: false },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS.branch([]);
    expect(r.stdout.split('\n')[0]).toBe('* main');
    expect(r.stdout).toContain('  dev');
  });
});

describe('git rev-parse / rev-list / symbolic-ref', () => {
  it('rev-parse HEAD returns the hash', async () => {
    mockLog.mockResolvedValue([
      { hash: 'deadbeefdeadbeef', message: 'm', author: 'a', date: 'd' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS['rev-parse']([]);
    expect(r.stdout.trim()).toBe('deadbeefdeadbeef');
  });

  it('rev-parse on an empty repo fails', async () => {
    mockLog.mockResolvedValue([]);
    const r = await SHELL_GIT_SUBCOMMANDS['rev-parse']([]);
    expect(r.exitCode).not.toBe(0);
  });

  it('rev-list --count returns the commit count', async () => {
    mockLog.mockResolvedValue([
      { hash: 'a', message: 'm', author: 'a', date: 'd' },
      { hash: 'b', message: 'm', author: 'a', date: 'd' },
      { hash: 'c', message: 'm', author: 'a', date: 'd' },
    ]);
    const r = await SHELL_GIT_SUBCOMMANDS['rev-list'](['--count']);
    expect(r.stdout.trim()).toBe('3');
  });

  it('symbolic-ref returns the current branch', async () => {
    mockBranch.mockResolvedValue([{ name: 'feature/x', current: true }]);
    const r = await SHELL_GIT_SUBCOMMANDS['symbolic-ref']([]);
    expect(r.stdout.trim()).toBe('feature/x');
  });
});

describe('global registration', () => {
  it('installs __sproutShellGit and routes subcommands', async () => {
    registerShellGitGlobal();
    const g = globalThis.__sproutShellGit;
    expect(g).toBeTruthy();

    mockStatus.mockResolvedValue({ staged: [], unstaged: [], untracked: [] });
    const r = await g!.execute('status', []);
    expect(r.exitCode).toBe(0);
    expect(typeof r.stdout).toBe('string');
  });

  it('unknown subcommands stay 127 (escalate to container)', async () => {
    registerShellGitGlobal();
    const r = await globalThis.__sproutShellGit!.execute('commit', ['-m', 'x']);
    expect(r.exitCode).toBe(127);
  });

  it('executor errors surface as exit 1 with stderr', async () => {
    registerShellGitGlobal();
    mockStatus.mockRejectedValue(new Error('IndexedDB unavailable'));
    const r = await globalThis.__sproutShellGit!.execute('status', []);
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('IndexedDB unavailable');
  });
});
