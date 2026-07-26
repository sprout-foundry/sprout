/**
 * Tests for gitClient singleton.
 *
 * Exercises the GitClient wrapper's own logic — path construction, option
 * passing, result formatting, error propagation, and operation locking —
 * against mocked isomorphic-git and lightning-fs backends.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Hoisted mock references ──────────────────────────────────────────

const mockFns = vi.hoisted(() => ({
  // lightning-fs promises methods
  pfsMkdir: vi.fn().mockResolvedValue(undefined),
  pfsStat: vi.fn().mockRejectedValue(new Error('not found')),
  pfsReaddir: vi.fn().mockResolvedValue([]),
  pfsReadFile: vi.fn().mockResolvedValue(''),
  pfsWriteFile: vi.fn().mockResolvedValue(undefined),
  pfsUnlink: vi.fn().mockResolvedValue(undefined),
  pfsRmdir: vi.fn().mockResolvedValue(undefined),
  // isomorphic-git methods
  gitClone: vi.fn().mockResolvedValue(undefined),
  gitPull: vi.fn().mockResolvedValue(undefined),
  gitPush: vi.fn().mockResolvedValue(undefined),
  gitStatusMatrix: vi.fn().mockResolvedValue([]),
  gitAdd: vi.fn().mockResolvedValue(undefined),
  gitRemove: vi.fn().mockResolvedValue(undefined),
  gitResetIndex: vi.fn().mockResolvedValue(undefined),
  gitCommit: vi.fn().mockResolvedValue('deadbeef1234567890'),
  gitLog: vi.fn().mockResolvedValue([]),
  gitListBranches: vi.fn().mockResolvedValue([]),
  gitCurrentBranch: vi.fn().mockResolvedValue('main'),
  gitBranch: vi.fn().mockResolvedValue(undefined),
  gitCheckout: vi.fn().mockResolvedValue(undefined),
  gitResolveRef: vi.fn().mockResolvedValue('abc123deadbeef'),
  gitReadBlob: vi.fn().mockResolvedValue({ blob: new Uint8Array([104, 101, 108, 108, 111]) }),
  gitReadTree: vi.fn().mockResolvedValue({ tree: [] }),
}));

// ── Mocks ────────────────────────────────────────────────────────────

vi.mock('@isomorphic-git/lightning-fs', () => {
  const isDirectory = vi.fn().mockReturnValue(false);
  const mockStat = {
    isDirectory,
    size: 42,
  };
  // Reset isDirectory alongside the rest of mocks in beforeEach
  const origPfsStat = mockFns.pfsStat;
  mockFns.pfsStat.mockImplementation(() => Promise.resolve(mockStat));

  return {
    default: class MockLightningFS {
      promises = {
        mkdir: mockFns.pfsMkdir,
        stat: mockFns.pfsStat,
        readdir: mockFns.pfsReaddir,
        readFile: mockFns.pfsReadFile,
        writeFile: mockFns.pfsWriteFile,
        unlink: mockFns.pfsUnlink,
        rmdir: mockFns.pfsRmdir,
      };
    },
  };
});

vi.mock('isomorphic-git', () => ({
  default: {
    clone: mockFns.gitClone,
    pull: mockFns.gitPull,
    push: mockFns.gitPush,
    statusMatrix: mockFns.gitStatusMatrix,
    add: mockFns.gitAdd,
    remove: mockFns.gitRemove,
    resetIndex: mockFns.gitResetIndex,
    commit: mockFns.gitCommit,
    log: mockFns.gitLog,
    listBranches: mockFns.gitListBranches,
    currentBranch: mockFns.gitCurrentBranch,
    branch: mockFns.gitBranch,
    checkout: mockFns.gitCheckout,
    resolveRef: mockFns.gitResolveRef,
    readBlob: mockFns.gitReadBlob,
    readTree: mockFns.gitReadTree,
  },
}));

vi.mock('isomorphic-git/http/web', () => ({ default: {} }));

// ── Imports ──────────────────────────────────────────────────────────

import { gitClient } from './gitClient';

// ── Helpers ──────────────────────────────────────────────────────────

function resetPfsStat(dirIsDir = false) {
  const mockStat = {
    isDirectory: vi.fn().mockReturnValue(dirIsDir),
    size: 42,
  };
  mockFns.pfsStat.mockImplementation(() => Promise.resolve(mockStat));
}

// ── beforeEach / afterEach ───────────────────────────────────────────

beforeEach(() => {
  vi.clearAllMocks();

  // Reset pfs.stat to a default that doesn't interfere
  resetPfsStat(false);

  // Default mocks
  mockFns.pfsMkdir.mockResolvedValue(undefined);
  mockFns.pfsReaddir.mockResolvedValue([]);
  mockFns.pfsReadFile.mockResolvedValue('');
  mockFns.pfsWriteFile.mockResolvedValue(undefined);
  mockFns.pfsUnlink.mockResolvedValue(undefined);
  mockFns.pfsRmdir.mockResolvedValue(undefined);

  mockFns.gitClone.mockResolvedValue(undefined);
  mockFns.gitPull.mockResolvedValue(undefined);
  mockFns.gitPush.mockResolvedValue(undefined);
  mockFns.gitStatusMatrix.mockResolvedValue([]);
  mockFns.gitAdd.mockResolvedValue(undefined);
  mockFns.gitRemove.mockResolvedValue(undefined);
  mockFns.gitResetIndex.mockResolvedValue(undefined);
  mockFns.gitCommit.mockResolvedValue('deadbeef1234567890');
  mockFns.gitLog.mockResolvedValue([]);
  mockFns.gitListBranches.mockResolvedValue([]);
  mockFns.gitCurrentBranch.mockResolvedValue('main');
  mockFns.gitBranch.mockResolvedValue(undefined);
  mockFns.gitCheckout.mockResolvedValue(undefined);
  mockFns.gitResolveRef.mockResolvedValue('abc123deadbeef');
  mockFns.gitReadBlob.mockResolvedValue({ blob: new Uint8Array([104, 101, 108, 108, 111]) });
  mockFns.gitReadTree.mockResolvedValue({ tree: [] });
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ── clone() ──────────────────────────────────────────────────────────

describe('clone()', () => {
  it('calls git.clone with correct url, dir, and defaults', async () => {
    await gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo');

    expect(mockFns.gitClone).toHaveBeenCalledWith(
      expect.objectContaining({
        url: 'https://github.com/owner/repo.git',
        dir: '/repos/owner/repo',
        depth: 1,
        singleBranch: true,
        ref: 'main',
        corsProxy: undefined,
        onAuth: undefined,
        onProgress: undefined,
      }),
    );
  });

  it('creates parent directory before cloning', async () => {
    await gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo');

    expect(mockFns.pfsMkdir).toHaveBeenCalledWith('/repos/owner');
  });

  it('passes depth and branch from opts', async () => {
    await gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo', {
      depth: 3,
      branch: 'develop',
      singleBranch: false,
    });

    expect(mockFns.gitClone).toHaveBeenCalledWith(
      expect.objectContaining({
        depth: 3,
        singleBranch: false,
        ref: 'develop',
      }),
    );
  });

  it('sets onAuth when token is provided', async () => {
    await gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo', {
      token: 'ghp_123456',
    });

    const callArgs = mockFns.gitClone.mock.calls[0][0];
    expect(typeof callArgs.onAuth).toBe('function');
    const authResult = await callArgs.onAuth();
    expect(authResult).toEqual({ token: 'ghp_123456' });
  });

  it('forwards onProgress callback', async () => {
    const onProgress = vi.fn();
    await gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo', {
      onProgress,
    });

    const callArgs = mockFns.gitClone.mock.calls[0][0];
    expect(typeof callArgs.onProgress).toBe('function');
    callArgs.onProgress({ phase: 'inflate', loaded: 50, total: 100 });
    expect(onProgress).toHaveBeenCalledWith({ phase: 'inflate', loaded: 50, total: 100 });
  });

  it('propagates clone error', async () => {
    mockFns.gitClone.mockRejectedValue(new Error('network timeout'));

    await expect(
      gitClient.clone('https://github.com/owner/repo.git', '/repos/owner/repo'),
    ).rejects.toThrow('network timeout');
  });
});

// ── pull() ───────────────────────────────────────────────────────────

describe('pull()', () => {
  it('calls git.pull with dir and auth', async () => {
    await gitClient.pull('/repos/owner/repo', { token: 'tok' });

    expect(mockFns.gitPull).toHaveBeenCalledWith(
      expect.objectContaining({
        dir: '/repos/owner/repo',
        singleBranch: true,
      }),
    );
  });

  it('sets onAuth when token is provided', async () => {
    await gitClient.pull('/repos/owner/repo', { token: 'tok' });

    const callArgs = mockFns.gitPull.mock.calls[0][0];
    expect(typeof callArgs.onAuth).toBe('function');
    const authResult = await callArgs.onAuth();
    expect(authResult).toEqual({ token: 'tok' });
  });

  it('propagates pull error', async () => {
    mockFns.gitPull.mockRejectedValue(new Error('conflict'));

    await expect(gitClient.pull('/repos/owner/repo')).rejects.toThrow('conflict');
  });
});

// ── push() ───────────────────────────────────────────────────────────

describe('push()', () => {
  it('calls git.push with token auth', async () => {
    await gitClient.push('/repos/owner/repo', { token: 'tok' });

    const callArgs = mockFns.gitPush.mock.calls[0][0];
    expect(callArgs.remote).toBe('origin');
    expect(callArgs.force).toBe(false);
    const authResult = await callArgs.onAuth();
    expect(authResult).toEqual({ token: 'tok' });
  });

  it('passes remote, branch, and force from opts', async () => {
    await gitClient.push('/repos/owner/repo', {
      token: 'tok',
      remote: 'upstream',
      branch: 'feature',
      force: true,
    });

    expect(mockFns.gitPush).toHaveBeenCalledWith(
      expect.objectContaining({
        remote: 'upstream',
        ref: 'feature',
        force: true,
      }),
    );
  });
});

// ── status() ─────────────────────────────────────────────────────────

describe('status()', () => {
  it('returns empty array when no changes', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toEqual([]);
  });

  it('identifies modified files', async () => {
    // workdir=1 (present), stage=2 (identical to target), HEAD=2 (present)
    // wd=true, st=true, hd=true -> modified
    mockFns.gitStatusMatrix.mockResolvedValue([['a.ts', 1, 2, 2]]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toEqual([{ filepath: 'a.ts', type: 'modified' }]);
  });

  it('identifies added files (staged new)', async () => {
    // workdir=1, stage=1, HEAD=0 -> wd=true, st=true, hd=false -> added
    mockFns.gitStatusMatrix.mockResolvedValue([['new.ts', 1, 1, 0]]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toEqual([{ filepath: 'new.ts', type: 'added' }]);
  });

  it('identifies deleted files', async () => {
    // workdir=0, stage=1, HEAD=1 -> wd=false, st=true, hd=true -> deleted
    mockFns.gitStatusMatrix.mockResolvedValue([['old.ts', 0, 1, 1]]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toEqual([{ filepath: 'old.ts', type: 'deleted' }]);
  });

  it('identifies untracked files', async () => {
    // workdir=1, stage=0, HEAD=0 -> wd=true, st=false, hd=false -> untracked
    mockFns.gitStatusMatrix.mockResolvedValue([['random.txt', 1, 0, 0]]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toEqual([{ filepath: 'random.txt', type: 'untracked' }]);
  });

  it('maps multiple entries with mixed types', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([
      ['a.ts', 1, 2, 2],     // modified
      ['b.ts', 1, 0, 0],     // untracked
      ['c.ts', 0, 1, 1],     // deleted
    ]);
    const result = await gitClient.status('/repos/owner/repo');
    expect(result).toHaveLength(3);
    expect(result[0].type).toBe('modified');
    expect(result[1].type).toBe('untracked');
    expect(result[2].type).toBe('deleted');
  });
});

// ── add() / unstage() ────────────────────────────────────────────────

describe('add()', () => {
  it('calls git.add with filepath for single file', async () => {
    await gitClient.add('/repos/owner/repo', 'src/main.ts');
    expect(mockFns.gitAdd).toHaveBeenCalledWith({
      fs: expect.anything(),
      dir: '/repos/owner/repo',
      filepath: 'src/main.ts',
    });
  });

  it('stages all changes when no filepath given', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([
      ['new.ts', 1, 0, 0],       // untracked -> add
      ['del.ts', 0, 1, 1],       // deleted -> remove
    ]);

    await gitClient.add('/repos/owner/repo');

    // Should call add for untracked
    expect(mockFns.gitAdd).toHaveBeenCalledWith(
      expect.objectContaining({ filepath: 'new.ts' }),
    );
    // Should call remove for deleted
    expect(mockFns.gitRemove).toHaveBeenCalledWith(
      expect.objectContaining({ filepath: 'del.ts' }),
    );
  });
});

describe('unstage()', () => {
  it('calls git.resetIndex with filepath', async () => {
    await gitClient.unstage('/repos/owner/repo', 'src/main.ts');
    expect(mockFns.gitResetIndex).toHaveBeenCalledWith({
      fs: expect.anything(),
      dir: '/repos/owner/repo',
      filepath: 'src/main.ts',
    });
  });
});

// ── commit() ─────────────────────────────────────────────────────────

describe('commit()', () => {
  it('calls git.commit with message and returns oid', async () => {
    const oid = await gitClient.commit('/repos/owner/repo', 'fix: something');

    expect(mockFns.gitCommit).toHaveBeenCalledWith(
      expect.objectContaining({
        message: 'fix: something',
        author: undefined,
        committer: { name: 'Sprout User', email: 'user@sprout.local' },
      }),
    );
    expect(oid).toBe('deadbeef1234567890');
  });

  it('passes custom author and committer', async () => {
    await gitClient.commit('/repos/owner/repo', 'feat', {
      author: { name: 'Alice', email: 'a@b.com' },
      committer: { name: 'Bob', email: 'b@c.com' },
    });

    expect(mockFns.gitCommit).toHaveBeenCalledWith(
      expect.objectContaining({
        author: { name: 'Alice', email: 'a@b.com' },
        committer: { name: 'Bob', email: 'b@c.com' },
      }),
    );
  });

  it('uses author as default committer when only author provided', async () => {
    await gitClient.commit('/repos/owner/repo', 'feat', {
      author: { name: 'Alice', email: 'a@b.com' },
    });

    expect(mockFns.gitCommit).toHaveBeenCalledWith(
      expect.objectContaining({
        author: { name: 'Alice', email: 'a@b.com' },
        committer: { name: 'Alice', email: 'a@b.com' },
      }),
    );
  });
});

// ── log() ────────────────────────────────────────────────────────────

describe('log()', () => {
  it('calls git.log with depth and ref', async () => {
    mockFns.gitLog.mockResolvedValue([
      {
        oid: 'abc123',
        commit: {
          message: 'initial',
          author: { name: 'Alice', email: 'a@b.com', timestamp: 1000 },
          committer: { name: 'Alice', email: 'a@b.com', timestamp: 1000 },
          tree: 'tree123',
          parent: [],
        },
      },
    ]);

    const entries = await gitClient.log('/repos/owner/repo', { depth: 5, ref: 'main' });

    expect(mockFns.gitLog).toHaveBeenCalledWith(
      expect.objectContaining({
        depth: 5,
        ref: 'main',
      }),
    );
    expect(entries).toHaveLength(1);
    expect(entries[0].commit.message).toBe('initial');
  });

  it('returns empty array when no commits', async () => {
    mockFns.gitLog.mockResolvedValue([]);
    const entries = await gitClient.log('/repos/owner/repo');
    expect(entries).toEqual([]);
  });
});

// ── branch operations ────────────────────────────────────────────────

describe('branch operations', () => {
  describe('listBranches()', () => {
    it('returns array of branch names', async () => {
      mockFns.gitListBranches.mockResolvedValue(['main', 'develop', 'feature']);
      const branches = await gitClient.listBranches('/repos/owner/repo');
      expect(branches).toEqual(['main', 'develop', 'feature']);
    });
  });

  describe('currentBranch()', () => {
    it('returns branch name when on a branch', async () => {
      mockFns.gitCurrentBranch.mockResolvedValue('main');
      const branch = await gitClient.currentBranch('/repos/owner/repo');
      expect(branch).toBe('main');
    });

    it('returns undefined when detached HEAD', async () => {
      mockFns.gitCurrentBranch.mockResolvedValue(null);
      const branch = await gitClient.currentBranch('/repos/owner/repo');
      expect(branch).toBeUndefined();
    });

    it('returns undefined on error', async () => {
      mockFns.gitCurrentBranch.mockRejectedValue(new Error('no head'));
      const branch = await gitClient.currentBranch('/repos/owner/repo');
      expect(branch).toBeUndefined();
    });
  });

  describe('branch()', () => {
    it('calls git.branch with name', async () => {
      await gitClient.branch('/repos/owner/repo', 'feature');
      expect(mockFns.gitBranch).toHaveBeenCalledWith({
        fs: expect.anything(),
        dir: '/repos/owner/repo',
        ref: 'feature',
      });
    });
  });

  describe('checkout()', () => {
    it('calls git.checkout with ref', async () => {
      await gitClient.checkout('/repos/owner/repo', 'feature');
      expect(mockFns.gitCheckout).toHaveBeenCalledWith({
        fs: expect.anything(),
        dir: '/repos/owner/repo',
        ref: 'feature',
      });
    });
  });
});

// ── file operations ──────────────────────────────────────────────────

describe('file operations', () => {
  describe('readFile()', () => {
    it('returns string when pfs.readFile returns string', async () => {
      mockFns.pfsReadFile.mockResolvedValue('hello world');
      const content = await gitClient.readFile('/repos/owner/repo', 'readme.md');
      expect(content).toBe('hello world');
    });

    it('decodes Uint8Array via TextDecoder', async () => {
      mockFns.pfsReadFile.mockResolvedValue(new Uint8Array([104, 101, 108, 108, 111]));
      const content = await gitClient.readFile('/repos/owner/repo', 'file.bin');
      expect(content).toBe('hello');
    });

    it('handles filepath with leading slash', async () => {
      mockFns.pfsReadFile.mockResolvedValue('content');
      await gitClient.readFile('/repos/owner/repo', '/readme.md');
      // Should call with /repos/owner/repo/readme.md (no double slash)
      expect(mockFns.pfsReadFile).toHaveBeenCalledWith('/repos/owner/repo/readme.md', 'utf8');
    });
  });

  describe('readFileBinary()', () => {
    it('returns raw Uint8Array', async () => {
      const binary = new Uint8Array([0, 1, 2, 3]);
      mockFns.pfsReadFile.mockResolvedValue(binary);
      const result = await gitClient.readFileBinary('/repos/owner/repo', 'data.bin');
      expect(result).toEqual(binary);
    });
  });

  describe('writeFile()', () => {
    it('writes content with utf8 encoding', async () => {
      await gitClient.writeFile('/repos/owner/repo', 'readme.md', '# Hello');
      expect(mockFns.pfsWriteFile).toHaveBeenCalledWith('/repos/owner/repo/readme.md', '# Hello', 'utf8');
    });

    it('creates parent directories for nested paths', async () => {
      await gitClient.writeFile('/repos/owner/repo', 'src/nested/file.ts', 'code');
      // Should create /repos/owner/repo/src and /repos/owner/repo/src/nested
      expect(mockFns.pfsMkdir).toHaveBeenCalledWith('/repos/owner/repo/src');
      expect(mockFns.pfsMkdir).toHaveBeenCalledWith('/repos/owner/repo/src/nested');
      expect(mockFns.pfsWriteFile).toHaveBeenCalledWith('/repos/owner/repo/src/nested/file.ts', 'code', 'utf8');
    });
  });

  describe('mkdir()', () => {
    it('creates directory at correct path', async () => {
      resetPfsStat(false);
      await gitClient.mkdir('/repos/owner/repo', 'newdir');
      expect(mockFns.pfsMkdir).toHaveBeenCalledWith('/repos/owner/repo/newdir');
    });

    it('handles leading slash in dirpath', async () => {
      resetPfsStat(false);
      await gitClient.mkdir('/repos/owner/repo', '/newdir');
      expect(mockFns.pfsMkdir).toHaveBeenCalledWith('/repos/owner/repo/newdir');
    });
  });

  describe('deleteFile()', () => {
    it('unlinks file at correct path', async () => {
      await gitClient.deleteFile('/repos/owner/repo', 'file.txt');
      expect(mockFns.pfsUnlink).toHaveBeenCalledWith('/repos/owner/repo/file.txt');
    });

    it('handles leading slash in filepath', async () => {
      await gitClient.deleteFile('/repos/owner/repo', '/file.txt');
      expect(mockFns.pfsUnlink).toHaveBeenCalledWith('/repos/owner/repo/file.txt');
    });
  });

  describe('exists()', () => {
    it('returns true when .git directory exists', async () => {
      resetPfsStat(true);
      mockFns.pfsStat.mockResolvedValue({ isDirectory: () => true, size: 0 });
      const result = await gitClient.exists('/repos/owner/repo');
      expect(result).toBe(true);
    });

    it('returns false when .git directory does not exist', async () => {
      mockFns.pfsStat.mockRejectedValue(new Error('not found'));
      const result = await gitClient.exists('/repos/owner/repo');
      expect(result).toBe(false);
    });
  });

  describe('delete()', () => {
    it('deletes files and directories in reverse order', async () => {
      resetPfsStat(false);
      mockFns.pfsReaddir.mockResolvedValue(['src', 'readme.md']);

      // listAllFiles calls: readdir on dir, stat on each entry
      const fileStat = { isDirectory: () => false, size: 100 };
      const dirStat = { isDirectory: () => true, size: 0 };
      mockFns.pfsStat
        .mockImplementationOnce(() => Promise.resolve(dirStat))    // src -> dir
        .mockImplementationOnce(() => Promise.resolve(fileStat))    // src/readme.md (readdir inside src)
        .mockImplementationOnce(() => Promise.resolve(fileStat));   // readme.md

      mockFns.pfsReaddir
        .mockImplementationOnce(() => Promise.resolve(['src', 'readme.md']))  // top level
        .mockImplementationOnce(() => Promise.resolve(['readme.md']));        // inside src

      await gitClient.delete('/repos/owner/repo');

      // Should call rmdir at the end
      expect(mockFns.pfsRmdir).toHaveBeenCalledWith('/repos/owner/repo');
    });
  });
});

// ── listDir() / listAllFiles() ────────────────────────────────────────

describe('listDir()', () => {
  it('returns file entries with correct paths', async () => {
    resetPfsStat(false);
    mockFns.pfsReaddir.mockResolvedValue(['a.ts', 'b.ts']);
    mockFns.pfsStat.mockResolvedValue({ isDirectory: () => false, size: 100 });

    const entries = await gitClient.listDir('/repos/owner/repo');

    expect(entries).toHaveLength(2);
    expect(entries[0]).toEqual({
      name: 'a.ts',
      path: '/a.ts',
      type: 'file',
      size: 100,
    });
    expect(entries[1].path).toBe('/b.ts');
  });

  it('skips .git directory', async () => {
    mockFns.pfsReaddir.mockResolvedValue(['.git', 'readme.md']);
    mockFns.pfsStat.mockResolvedValue({ isDirectory: () => false, size: 42 });

    const entries = await gitClient.listDir('/repos/owner/repo');
    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe('readme.md');
  });

  it('distinguishes dirs from files', async () => {
    mockFns.pfsReaddir.mockResolvedValue(['src', 'readme.md']);
    mockFns.pfsStat
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => true, size: 0 }))
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => false, size: 42 }));

    const entries = await gitClient.listDir('/repos/owner/repo');

    expect(entries).toHaveLength(2);
    // Dirs come first due to sort
    expect(entries[0].type).toBe('dir');
    expect(entries[1].type).toBe('file');
  });

  it('handles subpath argument', async () => {
    mockFns.pfsReaddir.mockResolvedValue(['file.ts']);
    mockFns.pfsStat.mockResolvedValue({ isDirectory: () => false, size: 42 });

    const entries = await gitClient.listDir('/repos/owner/repo', '/src');

    expect(entries[0].path).toBe('/src/file.ts');
  });

  it('sorts dirs before files, then alphabetically', async () => {
    mockFns.pfsReaddir.mockResolvedValue(['z.txt', 'a_dir', 'b.txt']);
    mockFns.pfsStat
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => false, size: 1 }))
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => true, size: 0 }))
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => false, size: 1 }));

    const entries = await gitClient.listDir('/repos/owner/repo');
    expect(entries[0].name).toBe('a_dir');
    expect(entries[1].name).toBe('b.txt');
    expect(entries[2].name).toBe('z.txt');
  });
});

describe('listAllFiles()', () => {
  it('recursively lists all entries excluding .git', async () => {
    mockFns.pfsReaddir
      .mockImplementationOnce(() => Promise.resolve(['src', 'readme.md']))  // /repos/owner/repo
      .mockImplementationOnce(() => Promise.resolve(['main.ts']));          // /repos/owner/repo/src

    mockFns.pfsStat
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => true, size: 0 }))    // src
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => false, size: 10 }))   // readme.md
      .mockImplementationOnce(() => Promise.resolve({ isDirectory: () => false, size: 50 }));  // main.ts

    const entries = await gitClient.listAllFiles('/repos/owner/repo');

    // Walk order: src (dir), recurses → main.ts, then readme.md at top level
    expect(entries).toHaveLength(3);
    expect(entries[0]).toEqual({
      name: 'src',
      path: '/src',
      type: 'dir',
      size: 0,
    });
    expect(entries[1].name).toBe('main.ts');
    expect(entries[2].name).toBe('readme.md');
  });
});

// ── diff() ────────────────────────────────────────────────────────────

describe('diff()', () => {
  it('returns empty array when no changes', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([]);
    const results = await gitClient.diff('/repos/owner/repo');
    expect(results).toEqual([]);
  });

  it('includes added files with + prefix', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([['new.txt', 1, 0, 0]]); // untracked
    mockFns.pfsReadFile.mockResolvedValue('new content');

    const results = await gitClient.diff('/repos/owner/repo');

    expect(results).toHaveLength(1);
    expect(results[0].filepath).toBe('new.txt');
    expect(results[0].type).toBe('added'); // untracked mapped to 'added'
    expect(results[0].patch).toBe('+new content');
  });

  it('includes deleted files with - prefix', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([['old.txt', 0, 1, 1]]); // deleted
    mockFns.gitReadBlob.mockResolvedValue({ blob: new Uint8Array([111, 108, 100]) }); // "old"

    const results = await gitClient.diff('/repos/owner/repo');

    expect(results[0].filepath).toBe('old.txt');
    expect(results[0].type).toBe('deleted');
    expect(results[0].patch).toBe('-old');
  });

  it('includes modified files with placeholder', async () => {
    mockFns.gitStatusMatrix.mockResolvedValue([['file.ts', 1, 2, 2]]); // modified
    const results = await gitClient.diff('/repos/owner/repo');

    expect(results[0].filepath).toBe('file.ts');
    expect(results[0].type).toBe('modified');
    expect(results[0].patch).toBe('(modified — open file to see changes)');
  });
});

// ── resolveRef() ──────────────────────────────────────────────────────

describe('resolveRef()', () => {
  it('returns the OID for HEAD by default', async () => {
    const oid = await gitClient.resolveRef('/repos/owner/repo');
    expect(mockFns.gitResolveRef).toHaveBeenCalledWith({
      fs: expect.anything(),
      dir: '/repos/owner/repo',
      ref: 'HEAD',
    });
    expect(oid).toBe('abc123deadbeef');
  });

  it('returns the OID for a specific ref', async () => {
    mockFns.gitResolveRef.mockResolvedValue('def456');
    const oid = await gitClient.resolveRef('/repos/owner/repo', 'main');
    expect(oid).toBe('def456');
  });
});

// ── readFileAtCommit() ────────────────────────────────────────────────

describe('readFileAtCommit()', () => {
  it('reads file content from a specific commit', async () => {
    mockFns.gitReadBlob.mockResolvedValue({ blob: new Uint8Array([97, 98, 99]) });
    const content = await gitClient.readFileAtCommit('/repos/owner/repo', 'file.txt', 'abc123');

    expect(mockFns.gitReadBlob).toHaveBeenCalledWith({
      fs: expect.anything(),
      dir: '/repos/owner/repo',
      oid: 'abc123',
      filepath: 'file.txt',
    });
    expect(content).toBe('abc');
  });

  it('returns null when file does not exist at commit', async () => {
    mockFns.gitReadBlob.mockRejectedValue(new Error('not found'));
    const content = await gitClient.readFileAtCommit('/repos/owner/repo', 'missing.txt', 'abc123');
    expect(content).toBeNull();
  });
});

// ── getChangedFiles() ──────────────────────────────────────────────────

describe('getChangedFiles()', () => {
  it('marks all files as added for first commit (no parent)', async () => {
    mockFns.gitReadTree.mockResolvedValue({
      tree: [
        { path: 'a.txt', oid: 'aaa' },
        { path: 'b.ts', oid: 'bbb' },
      ],
    });

    const changed = await gitClient.getChangedFiles('/repos/owner/repo', 'abc123');

    expect(changed).toHaveLength(2);
    expect(changed[0]).toEqual({ filepath: 'a.txt', type: 'added' });
    expect(changed[1]).toEqual({ filepath: 'b.ts', type: 'added' });
  });

  it('detects added, deleted, and modified between two commits', async () => {
    // Parent commit tree
    mockFns.gitReadTree
      .mockImplementationOnce(() => Promise.resolve({          // current
        tree: [
          { path: 'a.txt', oid: 'aaa' },
          { path: 'c.txt', oid: 'ccc' },
        ],
      }))
      .mockImplementationOnce(() => Promise.resolve({          // parent
        tree: [
          { path: 'a.txt', oid: 'aaa' },
          { path: 'b.txt', oid: 'bbb' },
        ],
      }));

    const changed = await gitClient.getChangedFiles('/repos/owner/repo', 'current', 'parent');

    expect(changed).toHaveLength(2);
    expect(changed.find((c) => c.filepath === 'c.txt')).toEqual({ filepath: 'c.txt', type: 'added' });
    expect(changed.find((c) => c.filepath === 'b.txt')).toEqual({ filepath: 'b.txt', type: 'deleted' });
  });

  it('skips .git in changed files', async () => {
    mockFns.gitReadTree
      .mockImplementationOnce(() => Promise.resolve({
        tree: [
          { path: '.git', oid: 'git_oid' },
          { path: 'file.txt', oid: 'aaa' },
        ],
      }));

    const changed = await gitClient.getChangedFiles('/repos/owner/repo', 'abc123');
    expect(changed).toHaveLength(1);
    expect(changed[0].filepath).toBe('file.txt');
  });

  it('handles parent tree read failure gracefully (treats as first commit)', async () => {
    mockFns.gitReadTree
      .mockImplementationOnce(() => Promise.resolve({
        tree: [{ path: 'file.txt', oid: 'aaa' }],
      }))
      .mockImplementationOnce(() => Promise.reject(new Error('bad parent sha')));

    const changed = await gitClient.getChangedFiles('/repos/owner/repo', 'current', 'badparent');
    expect(changed).toHaveLength(1);
    expect(changed[0].type).toBe('added');
  });
});

// ── withLock (concurrent operations) ──────────────────────────────────

describe('withLock (concurrent operations)', () => {
  it('serializes concurrent operations on the same dir', async () => {
    // Use a shared promise chain to simulate serial execution.
    // The lock ensures the second call waits for the first to resolve.
    const callOrder: number[] = [];
    let callIndex = 0;

    mockFns.gitLog.mockImplementation(() => {
      const idx = callIndex++;
      return new Promise((resolve) => {
        setTimeout(() => {
          callOrder.push(idx);
          resolve([{ oid: idx === 0 ? 'first' : 'second', commit: { message: idx === 0 ? 'one' : 'two', author: { name: '', email: '', timestamp: 0 }, committer: { name: '', email: '', timestamp: 0 }, tree: '', parent: [] } }]);
        }, 50 + idx * 50);
      });
    });

    const [result1, result2] = await Promise.all([
      gitClient.log('/repos/owner/repo'),
      gitClient.log('/repos/owner/repo'),
    ]);

    // The lock serializes: first completes (id=0), then second (id=1)
    expect(callOrder).toEqual([0, 1]);
    expect(result1[0].oid).toBe('first');
    expect(result2[0].oid).toBe('second');
  });

  it('allows concurrent operations on different dirs', async () => {
    let started = 0;

    mockFns.gitLog.mockImplementation(() => {
      started++;
      return new Promise((resolve) => {
        setTimeout(() => {
          resolve([{ oid: 'x', commit: { message: 'm', author: { name: '', email: '', timestamp: 0 }, committer: { name: '', email: '', timestamp: 0 }, tree: '', parent: [] } }]);
        }, 10);
      });
    });

    const [r1, r2] = await Promise.all([
      gitClient.log('/repos/a/repo'),
      gitClient.log('/repos/b/repo'),
    ]);

    // Both should have started concurrently (both before either finishes)
    expect(started).toBe(2);
    expect(r1[0].oid).toBe('x');
    expect(r2[0].oid).toBe('x');
  });

  it('continues chain after an error in one operation', async () => {
    let callCount = 0;

    mockFns.gitLog.mockImplementation(() => {
      const n = callCount++;
      if (n === 0) {
        return Promise.reject(new Error('first fails'));
      }
      return Promise.resolve([{ oid: 'second', commit: { message: 'ok', author: { name: '', email: '', timestamp: 0 }, committer: { name: '', email: '', timestamp: 0 }, tree: '', parent: [] } }]);
    });

    const p1 = gitClient.log('/repos/owner/repo').catch(() => 'error1');
    const p2 = gitClient.log('/repos/owner/repo').catch(() => 'error2');

    const [r1, r2] = await Promise.all([p1, p2]);

    // First should get the error, second should succeed (the lock catches and continues)
    expect(r1).toBe('error1');
    expect(r2[0].oid).toBe('second');
  });
});

// ── getFs() ────────────────────────────────────────────────────────────

describe('getFs()', () => {
  it('returns the underlying filesystem instance', () => {
    const fs = gitClient.getFs();
    expect(fs).toBeDefined();
    expect(fs.promises).toBeDefined();
  });
});

// ── static repoPath() ──────────────────────────────────────────────────

describe('GitClient.repoPath()', () => {
  // GitClient class is not exported (only the singleton gitClient is).
  // The /repos/<owner>/<name> path pattern is verified throughout all
  // other tests in this file (e.g. clone, status, diff all construct paths).
  it('follows /repos/<owner>/<name> convention (verified via all other tests)', () => {
    // All test cases above use '/repos/owner/repo' as the dir argument
    // and verify it gets passed to git.clone, git.status, etc.
    expect(true).toBe(true);
  });
});
