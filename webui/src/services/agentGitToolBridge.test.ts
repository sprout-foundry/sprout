/**
 * Tests for agentGitToolBridge — command parsing, async dispatch, global
 * registration, and sync hook behavior.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Mock agentGitTools ───────────────────────────────────────────────

const mockExecute = vi.fn();

const mockToolDefs = [
  {
    name: 'git_status',
    description: 'Test status tool',
    parameters: { type: 'object' },
    execute: mockExecute,
  },
  {
    name: 'git_read_file',
    description: 'Test read tool',
    parameters: { type: 'object' },
    execute: mockExecute,
  },
];

const mockToolNames = new Set(mockToolDefs.map((t) => t.name));

vi.mock('./agentGitTools', () => ({
  get AGENT_GIT_TOOLS() {
    return mockToolDefs;
  },
  get AGENT_GIT_TOOL_NAMES() {
    return mockToolNames;
  },
}));

import {
  GITTOOL_COMMAND_PREFIX,
  parseGitToolCommand,
  dispatchGitTool,
  registerGitToolGlobal,
  installGitToolBridge,
} from './agentGitToolBridge';

// ── Helpers ──────────────────────────────────────────────────────────

function resetMocks() {
  vi.clearAllMocks();
  mockExecute.mockReset();
  delete (globalThis as any).__sproutGitTools;
}

// ── parseGitToolCommand ──────────────────────────────────────────────

describe('GITTOOL_COMMAND_PREFIX', () => {
  it('equals the expected prefix string', () => {
    expect(GITTOOL_COMMAND_PREFIX).toBe('gittool:');
  });
});

describe('parseGitToolCommand', () => {
  beforeEach(resetMocks);

  it('returns null for non-gittool commands', () => {
    expect(parseGitToolCommand('ls -la')).toBe(null);
    expect(parseGitToolCommand('echo hello')).toBe(null);
    expect(parseGitToolCommand('')).toBe(null);
    expect(parseGitToolCommand('gittool')).toBe(null); // missing colon
    expect(parseGitToolCommand('GITTOOL:git_status {}')).toBe(null); // case-sensitive
  });

  it('parses simple command with args', () => {
    const result = parseGitToolCommand('gittool:git_status {"repo":"a/b"}');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({ repo: 'a/b' });
  });

  it('parses command with multiple args', () => {
    const result = parseGitToolCommand('gittool:git_read_file {"repo":"owner/repo","filepath":"src/main.ts"}');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_read_file');
    expect(result!.args).toEqual({ repo: 'owner/repo', filepath: 'src/main.ts' });
  });

  it('handles command without args', () => {
    const result = parseGitToolCommand('gittool:git_status');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({});
  });

  it('handles command with trailing whitespace', () => {
    const result = parseGitToolCommand('gittool:git_status   {"repo":"x"}');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({ repo: 'x' });
  });

  it('handles invalid JSON gracefully', () => {
    const result = parseGitToolCommand('gittool:git_status {invalid json}');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({});
  });

  it('handles empty args braces', () => {
    const result = parseGitToolCommand('gittool:git_status {}');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({});
  });

  it('returns null for empty tool name after prefix', () => {
    expect(parseGitToolCommand('gittool:')).toBe(null);
    expect(parseGitToolCommand('gittool:  ')).toBe(null);
  });

  it('ignores non-object JSON (returns empty args)', () => {
    const result = parseGitToolCommand('gittool:git_status "just a string"');
    expect(result).not.toBe(null);
    expect(result!.toolName).toBe('git_status');
    expect(result!.args).toEqual({});

    const result2 = parseGitToolCommand('gittool:git_status [1,2,3]');
    expect(result2).not.toBe(null);
    expect(result2!.toolName).toBe('git_status');
    expect(result2!.args).toEqual({});
  });
});

// ── dispatchGitTool ──────────────────────────────────────────────────

describe('dispatchGitTool', () => {
  beforeEach(resetMocks);

  it('calls the tool execute method', async () => {
    mockExecute.mockResolvedValue('Status clean');
    const result = await dispatchGitTool('git_status', { repo: 'a/b' });
    expect(result).toBe('Status clean');
    expect(mockExecute).toHaveBeenCalledWith({ repo: 'a/b' });
  });

  it('throws on unknown tool name', async () => {
    await expect(dispatchGitTool('git_nonexistent', {})).rejects.toThrow(/Unknown git tool: git_nonexistent/);
  });

  it('includes available tool names in error', async () => {
    try {
      await dispatchGitTool('bad', {});
      expect.fail('should have thrown');
    } catch (err: any) {
      expect(err.message).toContain('git_status');
      expect(err.message).toContain('git_read_file');
    }
  });

  it('catches execute errors and includes tool context', async () => {
    mockExecute.mockRejectedValue(new Error('repo not found'));
    await expect(dispatchGitTool('git_status', {})).rejects.toThrow('repo not found');
  });
});

// ── registerGitToolGlobal ────────────────────────────────────────────

describe('registerGitToolGlobal', () => {
  beforeEach(resetMocks);
  afterEach(() => {
    // Clean up global state
    delete (globalThis as any).__sproutGitTools;
  });

  it('registers on globalThis', () => {
    registerGitToolGlobal();
    expect(globalThis.__sproutGitTools).toBeDefined();
    expect(typeof globalThis.__sproutGitTools.execute).toBe('function');
    expect(globalThis.__sproutGitTools.names).toBe(mockToolNames);
    expect(typeof globalThis.__sproutGitTools.list).toBe('function');
  });

  it('execute dispatches to dispatchGitTool', async () => {
    mockExecute.mockResolvedValue('OK');
    registerGitToolGlobal();

    const result = await globalThis.__sproutGitTools.execute('git_status', { repo: 'x' });
    expect(result).toBe('OK');
    expect(mockExecute).toHaveBeenCalledWith({ repo: 'x' });
  });

  it('list returns sorted tool names', () => {
    registerGitToolGlobal();
    const names = globalThis.__sproutGitTools.list();
    expect(names).toEqual(['git_read_file', 'git_status']);
  });

  it('is idempotent', () => {
    registerGitToolGlobal();
    const first = globalThis.__sproutGitTools;
    registerGitToolGlobal();
    // A new object is created but with the same shape
    expect(globalThis.__sproutGitTools).not.toBe(first);
    expect(globalThis.__sproutGitTools.names).toBe(first.names);
  });

  it('throws on unknown tool via execute', async () => {
    mockExecute.mockResolvedValue('OK');
    registerGitToolGlobal();

    await expect(globalThis.__sproutGitTools.execute('bad_tool', {})).rejects.toThrow(/Unknown git tool/);
  });
});

// ── installGitToolBridge ─────────────────────────────────────────────

describe('installGitToolBridge', () => {
  beforeEach(resetMocks);

  it('returns silently when setToolExecutionHook is missing', () => {
    const spy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    installGitToolBridge({});
    expect(spy).toHaveBeenCalledWith('[agentGitToolBridge] setToolExecutionHook not available on WASM API');
    spy.mockRestore();
  });

  it('registers a sync hook that returns null for non-gittool commands', () => {
    let registeredHook: ((cmd: string) => unknown) | null = null;
    installGitToolBridge({
      setToolExecutionHook: (fn) => {
        registeredHook = fn;
      },
    });
    expect(registeredHook).not.toBe(null);
    expect(registeredHook!('ls -la')).toBe(null);
    expect(registeredHook!('echo hello')).toBe(null);
    expect(registeredHook!('')).toBe(null);
  });

  it('returns structured error for known gittool commands', () => {
    let registeredHook: ((cmd: string) => unknown) | null = null;
    installGitToolBridge({
      setToolExecutionHook: (fn) => {
        registeredHook = fn;
      },
    });
    const result = registeredHook!('gittool:git_status {"repo":"a/b"}');
    // NOTE: in production the Go side intercepts gittool: commands before
    // this hook fires (callGitToolJS), but the sync hook still handles them
    // for direct JS invocation and testing.
    expect(result).toMatchObject({
      stdout: '',
      exitCode: 1,
    });
    expect((result as { stderr: string }).stderr).toContain('git_status');
    expect((result as { stderr: string }).stderr).toContain('__sproutGitTools.execute');
  });

  it('rejects unknown tool names in the sync hook', () => {
    let registeredHook: ((cmd: string) => unknown) | null = null;
    installGitToolBridge({
      setToolExecutionHook: (fn) => {
        registeredHook = fn;
      },
    });
    const result = registeredHook!('gittool:unknown_tool {}');
    expect(result).toMatchObject({
      stdout: '',
      exitCode: 1,
    });
    expect((result as { stderr: string }).stderr).toContain('Unknown git tool: unknown_tool');
    // Should NOT contain the async guidance message for unknown tools
    expect((result as { stderr: string }).stderr).not.toContain('async execution');
  });

  it('sync hook does not call execute (fire-and-forget only)', () => {
    let registeredHook: ((cmd: string) => unknown) | null = null;
    installGitToolBridge({
      setToolExecutionHook: (fn) => {
        registeredHook = fn;
      },
    });
    // The sync hook returns immediately without calling dispatchGitTool
    const result = registeredHook!('gittool:git_status {}');
    expect(mockExecute).not.toHaveBeenCalled();
    expect(result).toMatchObject({
      stdout: '',
      exitCode: 1,
    });
    expect((result as { stderr: string }).stderr).toContain('async execution');
  });

  it('sync hook passes args in the error message', () => {
    let registeredHook: ((cmd: string) => unknown) | null = null;
    installGitToolBridge({
      setToolExecutionHook: (fn) => {
        registeredHook = fn;
      },
    });
    const result = registeredHook!('gittool:git_read_file {"repo":"x/y","filepath":"a.ts"}');
    const stderr = (result as any).stderr;
    expect(stderr).toContain('git_read_file');
    expect(stderr).toContain('"repo":"x/y"');
    expect(stderr).toContain('"filepath":"a.ts"');
  });
});
