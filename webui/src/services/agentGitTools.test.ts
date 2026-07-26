/**
 * Tests for agentGitTools — tool definitions and their execute handlers.
 *
 * Tests the tool formatting, error handling, repo validation, and localStorage
 * token lookup logic against a mocked gitClient singleton.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// ── Mocks ────────────────────────────────────────────────────────────

const mockGitClient = {
  clone: vi.fn().mockResolvedValue(undefined),
  status: vi.fn().mockResolvedValue([]),
  diff: vi.fn().mockResolvedValue([]),
  log: vi.fn().mockResolvedValue([]),
  listBranches: vi.fn().mockResolvedValue([]),
  currentBranch: vi.fn().mockResolvedValue('main'),
  branch: vi.fn().mockResolvedValue(undefined),
  checkout: vi.fn().mockResolvedValue(undefined),
  readFile: vi.fn().mockResolvedValue(''),
  writeFile: vi.fn().mockResolvedValue(undefined),
  listAllFiles: vi.fn().mockResolvedValue([]),
  add: vi.fn().mockResolvedValue(undefined),
  commit: vi.fn().mockResolvedValue('abcdef1234567890'),
  push: vi.fn().mockResolvedValue(undefined),
  pull: vi.fn().mockResolvedValue(undefined),
};

vi.mock('./gitClient', () => ({
  get gitClient() {
    return mockGitClient;
  },
}));

// Mock localStorage
const mockLocalStorage = {
  getItem: vi.fn().mockReturnValue(null),
  setItem: vi.fn(),
  removeItem: vi.fn(),
  clear: vi.fn(),
  get length() { return 0; },
  key: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();

  // Reset gitClient mocks
  mockGitClient.status.mockResolvedValue([]);
  mockGitClient.diff.mockResolvedValue([]);
  mockGitClient.log.mockResolvedValue([]);
  mockGitClient.listBranches.mockResolvedValue([]);
  mockGitClient.currentBranch.mockResolvedValue('main');
  mockGitClient.branch.mockResolvedValue(undefined);
  mockGitClient.checkout.mockResolvedValue(undefined);
  mockGitClient.readFile.mockResolvedValue('');
  mockGitClient.writeFile.mockResolvedValue(undefined);
  mockGitClient.listAllFiles.mockResolvedValue([]);
  mockGitClient.add.mockResolvedValue(undefined);
  mockGitClient.commit.mockResolvedValue('abcdef1234567890');
  mockGitClient.push.mockResolvedValue(undefined);
  mockGitClient.pull.mockResolvedValue(undefined);

  // Reset localStorage
  mockLocalStorage.getItem.mockReturnValue(null);
});

// Make localStorage available globally
Object.defineProperty(global, 'localStorage', {
  value: mockLocalStorage,
  writable: true,
  configurable: true,
});

// ── Imports ──────────────────────────────────────────────────────────

import { AGENT_GIT_TOOLS, AGENT_GIT_TOOL_NAMES, AgentGitToolDefinition } from './agentGitTools';

// ── Helpers ──────────────────────────────────────────────────────────

function findTool(name: string): AgentGitToolDefinition {
  const tool = AGENT_GIT_TOOLS.find((t) => t.name === name);
  if (!tool) throw new Error(`Tool ${name} not found`);
  return tool;
}

// ── Structure tests ──────────────────────────────────────────────────

describe('AGENT_GIT_TOOLS structure', () => {
  it('has 13 tool definitions', () => {
    expect(AGENT_GIT_TOOLS).toHaveLength(13);
  });

  it('each tool has required fields', () => {
    for (const tool of AGENT_GIT_TOOLS) {
      expect(tool.name).toBeDefined();
      expect(typeof tool.name).toBe('string');
      expect(tool.description).toBeDefined();
      expect(typeof tool.description).toBe('string');
      expect(tool.parameters).toBeDefined();
      expect(typeof tool.parameters).toBe('object');
      expect(tool.execute).toBeDefined();
      expect(typeof tool.execute).toBe('function');
    }
  });

  it('all tool names are unique', () => {
    const names = AGENT_GIT_TOOLS.map((t) => t.name);
    const unique = new Set(names);
    expect(unique.size).toBe(names.length);
  });

  it('AGENT_GIT_TOOL_NAMES matches all tool names', () => {
    expect(AGENT_GIT_TOOL_NAMES.size).toBe(13);
    for (const tool of AGENT_GIT_TOOLS) {
      expect(AGENT_GIT_TOOL_NAMES.has(tool.name)).toBe(true);
    }
  });

  it('contains all expected tool names', () => {
    const expected = [
      'git_status',
      'git_diff',
      'git_log',
      'git_branch_list',
      'git_read_file',
      'git_write_file',
      'git_list_files',
      'git_add',
      'git_commit',
      'git_push',
      'git_pull',
      'git_create_branch',
      'git_checkout',
    ];
    for (const name of expected) {
      expect(AGENT_GIT_TOOL_NAMES.has(name)).toBe(true);
    }
  });
});

// ── resolveRepoDir validation (tested via tool error paths) ───────────

describe('resolveRepoDir validation', () => {
  it('rejects missing repo argument', async () => {
    const result = await findTool('git_status').execute({});
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
  });

  it('rejects empty repo string', async () => {
    const result = await findTool('git_status').execute({ repo: '' });
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
  });

  it('rejects non-string repo', async () => {
    const result = await findTool('git_status').execute({ repo: 123 });
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
  });

  it('rejects repo without slash', async () => {
    const result = await findTool('git_status').execute({ repo: 'foo' });
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
    expect(result).toContain('must be "owner/name"');
  });

  it('rejects repo with only owner', async () => {
    const result = await findTool('git_status').execute({ repo: 'owner/' });
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
  });

  it('rejects repo with only name', async () => {
    const result = await findTool('git_status').execute({ repo: '/name' });
    expect(result).toContain('git_status error:');
    expect(result).toContain('Invalid repo');
  });
});

// ── Per-tool happy path ──────────────────────────────────────────────

describe('git_status', () => {
  it('returns clean message when no changes', async () => {
    mockGitClient.status.mockResolvedValue([]);
    const result = await findTool('git_status').execute({ repo: 'owner/repo' });
    expect(result).toBe('No changes detected. Working tree is clean.');
  });

  it('formats status entries with type and filepath', async () => {
    mockGitClient.status.mockResolvedValue([
      { filepath: 'a.ts', type: 'modified' },
      { filepath: 'b.ts', type: 'added' },
    ]);
    const result = await findTool('git_status').execute({ repo: 'owner/repo' });
    expect(result).toContain('Status for owner/repo');
    expect(result).toContain('modified  ');
    expect(result).toContain('a.ts');
    expect(result).toContain('added     ');
    expect(result).toContain('b.ts');
  });
});

describe('git_diff', () => {
  it('returns no changes message when empty', async () => {
    mockGitClient.diff.mockResolvedValue([]);
    const result = await findTool('git_diff').execute({ repo: 'owner/repo' });
    expect(result).toBe('No changes detected.');
  });

  it('formats diff blocks with filepath and patch', async () => {
    mockGitClient.diff.mockResolvedValue([
      { filepath: 'a.ts', type: 'modified', patch: '+new code' },
    ]);
    const result = await findTool('git_diff').execute({ repo: 'owner/repo' });
    expect(result).toContain('Diff for owner/repo');
    expect(result).toContain('--- a.ts (modified)');
    expect(result).toContain('+new code');
  });
});

describe('git_log', () => {
  it('returns no commits message when empty', async () => {
    mockGitClient.log.mockResolvedValue([]);
    const result = await findTool('git_log').execute({ repo: 'owner/repo' });
    expect(result).toBe('No commits found in this repository.');
  });

  it('formats log entries with SHA, date, author, and message', async () => {
    mockGitClient.log.mockResolvedValue([
      {
        oid: 'abcdef1234567890abcdef1234567890',
        commit: {
          message: 'feat: add login',
          author: { name: 'Alice', email: 'alice@example.com', timestamp: 1700000000 },
          committer: { name: 'Alice', email: 'alice@example.com', timestamp: 1700000000 },
          tree: 'tree123',
          parent: ['parent123'],
        },
      },
    ]);
    const result = await findTool('git_log').execute({ repo: 'owner/repo' });
    expect(result).toContain('Log for owner/repo (1 commit(s))');
    expect(result).toContain('abcdef12');
    expect(result).toContain('Alice');
    expect(result).toContain('alice@example.com');
    expect(result).toContain('feat: add login');
  });

  it('uses default depth of 50 when not specified', async () => {
    mockGitClient.log.mockResolvedValue([]);
    await findTool('git_log').execute({ repo: 'owner/repo' });
    expect(mockGitClient.log).toHaveBeenCalledWith('/repos/owner/repo', { depth: 50 });
  });

  it('passes depth from args when provided', async () => {
    mockGitClient.log.mockResolvedValue([]);
    await findTool('git_log').execute({ repo: 'owner/repo', depth: 10 });
    expect(mockGitClient.log).toHaveBeenCalledWith('/repos/owner/repo', { depth: 10 });
  });
});

describe('git_branch_list', () => {
  it('returns no branches message when empty', async () => {
    mockGitClient.listBranches.mockResolvedValue([]);
    const result = await findTool('git_branch_list').execute({ repo: 'owner/repo' });
    expect(result).toBe('No branches found.');
  });

  it('marks current branch with *', async () => {
    mockGitClient.listBranches.mockResolvedValue(['main', 'feature']);
    mockGitClient.currentBranch.mockResolvedValue('main');
    const result = await findTool('git_branch_list').execute({ repo: 'owner/repo' });
    expect(result).toContain('Branches for owner/repo (HEAD -> main)');
    expect(result).toContain('  * main');
    expect(result).toContain('    feature');
  });

  it('shows detached HEAD when no current branch', async () => {
    mockGitClient.listBranches.mockResolvedValue(['main']);
    mockGitClient.currentBranch.mockResolvedValue(undefined);
    const result = await findTool('git_branch_list').execute({ repo: 'owner/repo' });
    expect(result).toContain('detached HEAD');
  });
});

describe('git_read_file', () => {
  it('returns file content', async () => {
    mockGitClient.readFile.mockResolvedValue('const x = 1;');
    const result = await findTool('git_read_file').execute({
      repo: 'owner/repo',
      filepath: 'src/main.ts',
    });
    expect(result).toBe('const x = 1;');
    expect(mockGitClient.readFile).toHaveBeenCalledWith('/repos/owner/repo', 'src/main.ts');
  });
});

describe('git_write_file', () => {
  it('writes file and returns confirmation', async () => {
    const result = await findTool('git_write_file').execute({
      repo: 'owner/repo',
      filepath: 'src/main.ts',
      content: 'const x = 2;',
    });
    expect(result).toBe('Wrote src/main.ts in owner/repo');
    expect(mockGitClient.writeFile).toHaveBeenCalledWith(
      '/repos/owner/repo',
      'src/main.ts',
      'const x = 2;',
    );
  });
});

describe('git_list_files', () => {
  it('returns no files message when empty', async () => {
    mockGitClient.listAllFiles.mockResolvedValue([]);
    const result = await findTool('git_list_files').execute({ repo: 'owner/repo' });
    expect(result).toBe('No files found in this repository.');
  });

  it('formats entries with paths and trailing slash for dirs', async () => {
    mockGitClient.listAllFiles.mockResolvedValue([
      { name: 'src', path: '/src', type: 'dir', size: 0 },
      { name: 'readme.md', path: '/readme.md', type: 'file', size: 100 },
    ]);
    const result = await findTool('git_list_files').execute({ repo: 'owner/repo' });
    expect(result).toContain('Files in owner/repo (2 entries)');
    expect(result).toContain('  /src/');
    expect(result).toContain('  /readme.md');
  });
});

describe('git_add', () => {
  it('stages single file with filepath', async () => {
    const result = await findTool('git_add').execute({
      repo: 'owner/repo',
      filepath: 'src/main.ts',
    });
    expect(result).toBe('Staged src/main.ts in owner/repo');
    expect(mockGitClient.add).toHaveBeenCalledWith('/repos/owner/repo', 'src/main.ts');
  });

  it('stages all changes when filepath omitted', async () => {
    const result = await findTool('git_add').execute({ repo: 'owner/repo' });
    expect(result).toBe('Staged all changes in owner/repo');
    expect(mockGitClient.add).toHaveBeenCalledWith('/repos/owner/repo', undefined);
  });
});

describe('git_commit', () => {
  it('returns short SHA and message', async () => {
    mockGitClient.commit.mockResolvedValue('abcdef1234567890abcdef1234567890');
    const result = await findTool('git_commit').execute({
      repo: 'owner/repo',
      message: 'feat: add login',
    });
    expect(result).toContain('Committed abcdef12');
    expect(result).toContain('in owner/repo');
    expect(result).toContain('feat: add login');
  });
});

describe('git_push', () => {
  it('returns error when no token in localStorage', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);
    const result = await findTool('git_push').execute({ repo: 'owner/repo' });
    expect(result).toBe('No GitHub token found. The user must authenticate first.');
    expect(mockGitClient.push).not.toHaveBeenCalled();
  });

  it('pushes with token from localStorage', async () => {
    mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
    const result = await findTool('git_push').execute({ repo: 'owner/repo' });
    expect(result).toBe('Pushed to owner/repo');
    expect(mockGitClient.push).toHaveBeenCalledWith('/repos/owner/repo', {
      token: 'ghp_mytoken',
      branch: undefined,
    });
  });

  it('passes branch when provided', async () => {
    mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
    const result = await findTool('git_push').execute({
      repo: 'owner/repo',
      branch: 'feature',
    });
    expect(result).toBe('Pushed to owner/repo');
    expect(mockGitClient.push).toHaveBeenCalledWith('/repos/owner/repo', {
      token: 'ghp_mytoken',
      branch: 'feature',
    });
  });
});

describe('git_pull', () => {
  it('returns error when no token in localStorage', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);
    const result = await findTool('git_pull').execute({ repo: 'owner/repo' });
    expect(result).toBe('No GitHub token found. The user must authenticate first.');
    expect(mockGitClient.pull).not.toHaveBeenCalled();
  });

  it('pulls with token from localStorage', async () => {
    mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
    const result = await findTool('git_pull').execute({ repo: 'owner/repo' });
    expect(result).toBe('Pulled from owner/repo');
    expect(mockGitClient.pull).toHaveBeenCalledWith('/repos/owner/repo', {
      token: 'ghp_mytoken',
      branch: undefined,
    });
  });

  it('pulls with explicit branch', async () => {
    mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
    const result = await findTool('git_pull').execute({
      repo: 'owner/repo',
      branch: 'dev',
    });
    expect(result).toBe('Pulled from owner/repo');
    expect(mockGitClient.pull).toHaveBeenCalledWith('/repos/owner/repo', {
      token: 'ghp_mytoken',
      branch: 'dev',
    });
  });
});

describe('git_create_branch', () => {
  it('creates branch and returns confirmation', async () => {
    const result = await findTool('git_create_branch').execute({
      repo: 'owner/repo',
      name: 'feature',
    });
    expect(result).toBe('Created branch "feature" in owner/repo');
    expect(mockGitClient.branch).toHaveBeenCalledWith('/repos/owner/repo', 'feature');
  });
});

describe('git_checkout', () => {
  it('checks out ref and returns confirmation', async () => {
    const result = await findTool('git_checkout').execute({
      repo: 'owner/repo',
      ref: 'feature',
    });
    expect(result).toBe('Checked out "feature" in owner/repo');
    expect(mockGitClient.checkout).toHaveBeenCalledWith('/repos/owner/repo', 'feature');
  });
});

// ── Path traversal guards ────────────────────────────────────────────

describe('path traversal guards', () => {
  it('git_read_file rejects path traversal', async () => {
    const result = await findTool('git_read_file').execute({
      repo: 'owner/repo',
      filepath: '../secret',
    });
    expect(result).toContain('git_read_file error:');
    expect(result).toContain('path traversal');
    expect(mockGitClient.readFile).not.toHaveBeenCalled();
  });

  it('git_read_file rejects absolute paths', async () => {
    const result = await findTool('git_read_file').execute({
      repo: 'owner/repo',
      filepath: '/etc/passwd',
    });
    expect(result).toContain('git_read_file error:');
    expect(result).toContain('path traversal');
    expect(mockGitClient.readFile).not.toHaveBeenCalled();
  });

  it('git_write_file rejects path traversal', async () => {
    const result = await findTool('git_write_file').execute({
      repo: 'owner/repo',
      filepath: '../secret.txt',
      content: 'malicious',
    });
    expect(result).toContain('git_write_file error:');
    expect(result).toContain('path traversal');
    expect(mockGitClient.writeFile).not.toHaveBeenCalled();
  });

  it('git_write_file rejects absolute paths', async () => {
    const result = await findTool('git_write_file').execute({
      repo: 'owner/repo',
      filepath: '/etc/passwd',
      content: 'malicious',
    });
    expect(result).toContain('git_write_file error:');
    expect(result).toContain('path traversal');
    expect(mockGitClient.writeFile).not.toHaveBeenCalled();
  });

  it('git_add rejects path traversal in filepath', async () => {
    const result = await findTool('git_add').execute({
      repo: 'owner/repo',
      filepath: '../secret',
    });
    expect(result).toContain('git_add error:');
    expect(result).toContain('path traversal');
  });
});

// ── Runtime type validation ──────────────────────────────────────────

describe('runtime type validation', () => {
  it('git_read_file rejects non-string filepath', async () => {
    const result = await findTool('git_read_file').execute({
      repo: 'owner/repo',
      filepath: 123,
    });
    expect(result).toContain('git_read_file error:');
    expect(result).toContain('filepath must be a string');
    expect(mockGitClient.readFile).not.toHaveBeenCalled();
  });

  it('git_write_file rejects non-string content', async () => {
    const result = await findTool('git_write_file').execute({
      repo: 'owner/repo',
      filepath: 'src/main.ts',
      content: 123,
    });
    expect(result).toContain('git_write_file error:');
    expect(result).toContain('content must be a string');
    expect(mockGitClient.writeFile).not.toHaveBeenCalled();
  });

  it('git_commit rejects non-string message', async () => {
    const result = await findTool('git_commit').execute({
      repo: 'owner/repo',
      message: 123,
    });
    expect(result).toContain('git_commit error:');
    expect(result).toContain('message must be a string');
    expect(mockGitClient.commit).not.toHaveBeenCalled();
  });

  it('git_create_branch rejects non-string name', async () => {
    const result = await findTool('git_create_branch').execute({
      repo: 'owner/repo',
      name: 123,
    });
    expect(result).toContain('git_create_branch error:');
    expect(result).toContain('name must be a string');
    expect(mockGitClient.branch).not.toHaveBeenCalled();
  });

  it('git_checkout rejects non-string ref', async () => {
    const result = await findTool('git_checkout').execute({
      repo: 'owner/repo',
      ref: 123,
    });
    expect(result).toContain('git_checkout error:');
    expect(result).toContain('ref must be a string');
    expect(mockGitClient.checkout).not.toHaveBeenCalled();
  });
});

// ── git_add null handling ────────────────────────────────────────────

describe('git_add null handling', () => {
  it('stages all when filepath is null', async () => {
    const result = await findTool('git_add').execute({
      repo: 'owner/repo',
      filepath: null as unknown as string,
    });
    expect(result).toBe('Staged all changes in owner/repo');
    expect(mockGitClient.add).toHaveBeenCalledWith('/repos/owner/repo', undefined);
  });
});

// ── Error handling ───────────────────────────────────────────────────

describe('error handling', () => {
  it('git_status returns error string on failure', async () => {
    mockGitClient.status.mockRejectedValue(new Error('repo not found'));
    const result = await findTool('git_status').execute({ repo: 'owner/repo' });
    expect(result).toBe('git_status error: repo not found');
  });

  it('git_diff returns error string on failure', async () => {
    mockGitClient.diff.mockRejectedValue(new Error('bad state'));
    const result = await findTool('git_diff').execute({ repo: 'owner/repo' });
    expect(result).toBe('git_diff error: bad state');
  });

  it('git_read_file returns error string on failure', async () => {
    mockGitClient.readFile.mockRejectedValue(new Error('file not found'));
    const result = await findTool('git_read_file').execute({
      repo: 'owner/repo',
      filepath: 'missing.txt',
    });
    expect(result).toBe('git_read_file error: file not found');
  });

  it('git_commit returns error string on failure', async () => {
    mockGitClient.commit.mockRejectedValue(new Error('nothing to commit'));
    const result = await findTool('git_commit').execute({
      repo: 'owner/repo',
      message: 'test',
    });
    expect(result).toBe('git_commit error: nothing to commit');
  });

  it('git_push returns error string on push failure', async () => {
    mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
    mockGitClient.push.mockRejectedValue(new Error('remote rejected'));
    const result = await findTool('git_push').execute({ repo: 'owner/repo' });
    expect(result).toBe('git_push error: remote rejected');
  });

  it('git_checkout returns error string on failure', async () => {
    mockGitClient.checkout.mockRejectedValue(new Error('branch not found'));
    const result = await findTool('git_checkout').execute({
      repo: 'owner/repo',
      ref: 'nonexistent',
    });
    expect(result).toBe('git_checkout error: branch not found');
  });

  it('tools never throw — errors are returned as strings', async () => {
    // Verify that execute handlers wrap errors and return strings,
    // so the agent loop never crashes from a tool call.
    for (const tool of AGENT_GIT_TOOLS) {
      // Spy on each method to reject, then restore after.
      const spies: ReturnType<typeof vi.spyOn>[] = [];
      for (const key of Object.keys(mockGitClient)) {
        spies.push(vi.spyOn(mockGitClient as any, key as keyof typeof mockGitClient).mockRejectedValue(new Error('simulated error')));
      }

      // push/pull check localStorage *before* calling gitClient,
      // so with no token they return their pre-auth message (not an error string).
      // Set a token so execution reaches the actual gitClient call that throws.
      if (tool.name === 'git_push' || tool.name === 'git_pull') {
        mockLocalStorage.getItem.mockReturnValue('ghp_mytoken');
      } else {
        mockLocalStorage.getItem.mockReturnValue(null);
      }

      try {
        const result = await tool.execute({
          repo: 'owner/repo',
          filepath: 'file.txt',
          content: 'content',
          message: 'msg',
          name: 'branch',
          ref: 'main',
          branch: 'main',
          depth: 5,
        });
        // Should always return a string, never throw
        expect(typeof result).toBe('string');
        expect(result).toContain(tool.name);
        expect(result).toContain('error:');
      } finally {
        // Restore all spies
        for (const spy of spies) {
          spy.mockRestore();
        }
        // Re-apply resolved mocks for next test
        mockGitClient.status.mockResolvedValue([]);
        mockGitClient.diff.mockResolvedValue([]);
        mockGitClient.log.mockResolvedValue([]);
        mockGitClient.listBranches.mockResolvedValue([]);
        mockGitClient.currentBranch.mockResolvedValue('main');
        mockGitClient.branch.mockResolvedValue(undefined);
        mockGitClient.checkout.mockResolvedValue(undefined);
        mockGitClient.readFile.mockResolvedValue('');
        mockGitClient.writeFile.mockResolvedValue(undefined);
        mockGitClient.listAllFiles.mockResolvedValue([]);
        mockGitClient.add.mockResolvedValue(undefined);
        mockGitClient.commit.mockResolvedValue('abcdef1234567890');
        mockGitClient.push.mockResolvedValue(undefined);
        mockGitClient.pull.mockResolvedValue(undefined);
      }
    }
  });
});
