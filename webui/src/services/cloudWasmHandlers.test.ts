// @vitest-environment jsdom

import { describe, it, expect, beforeEach } from 'vitest';
import { handleWasmLocal, trackFileWrite } from './cloudWasmHandlers';
import type { WasmDirEntry, WasmShell } from './wasmShell';

function createMockShell(overrides: Partial<WasmShell> = {}): WasmShell {
  return {
    executeCommand: () => ({ stdout: '', stderr: '', exitCode: 0 }),
    autoComplete: () => ({ completions: [] }),
    getCwd: () => '/home/user',
    changeDir: () => ({ cwd: '/home/user' }),
    writeFile: () => '',
    readFile: () => ({ content: '' }),
    listDir: () => ({ entries: [] }),
    deleteFile: () => '',
    runAgent: async () => ({ response: '', provider: '', model: '' }),
    clearConversation: () => {},
    stopAgent: () => {},
    wasm: globalThis as WasmShell['wasm'],
    ...overrides,
  };
}

describe('handleWasmLocal — /api/ask-user/response', () => {
  let shell: WasmShell;

  beforeEach(() => {
    shell = createMockShell();
  });

  it('returns 400 when request body is missing', async () => {
    const res = handleWasmLocal(shell, '/api/ask-user/response', 'POST', '/api/ask-user/response');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Missing request body');
  });

  it('returns 400 when request body is not valid JSON', async () => {
    const res = handleWasmLocal(shell, '/api/ask-user/response', 'POST', '/api/ask-user/response', 'not json');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Invalid JSON body');
  });

  it('returns 400 when request_id is missing from JSON body', async () => {
    const bodyStr = JSON.stringify({ response: 'hello' });
    const res = handleWasmLocal(shell, '/api/ask-user/response', 'POST', '/api/ask-user/response', bodyStr);
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('request_id is required');
  });

  it('returns 501 when respondToAskUser is not available on the shell', async () => {
    const shellWithoutMethod = createMockShell({ respondToAskUser: undefined });
    const bodyStr = JSON.stringify({ request_id: 'abc123', response: 'hello' });
    const res = handleWasmLocal(
      shellWithoutMethod,
      '/api/ask-user/response',
      'POST',
      '/api/ask-user/response',
      bodyStr,
    );
    expect(res.status).toBe(501);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('respondToAskUser not available (WASM binary too old)');
  });

  it('returns 200 with { delivered: true } when respondToAskUser succeeds', async () => {
    const shellWithMethod = createMockShell({
      respondToAskUser: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({ request_id: 'abc123', response: 'hello' });
    const res = handleWasmLocal(shellWithMethod, '/api/ask-user/response', 'POST', '/api/ask-user/response', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body.delivered).toBe(true);
  });

  it('returns 404 when respondToAskUser returns delivered false (unknown/expired request)', async () => {
    const shellWithMethod = createMockShell({
      respondToAskUser: () => ({ delivered: false }),
    });
    const bodyStr = JSON.stringify({ request_id: 'abc123', response: 'hello' });
    const res = handleWasmLocal(shellWithMethod, '/api/ask-user/response', 'POST', '/api/ask-user/response', bodyStr);
    expect(res.status).toBe(404);
    const body = JSON.parse(await res.text());
    expect(body.error).toContain('not found or already expired');
  });
});

// ---------------------------------------------------------------------------
// handleWasmEditDecision (INT-2/cloud edit-approval wiring)
// ---------------------------------------------------------------------------

import { handleWasmEditDecision, handleWasmShellApprovalDecision } from './cloudWasmHandlers';

describe('handleWasmEditDecision — /api/edits/{id}/decision', () => {
  let shell: WasmShell;

  beforeEach(() => {
    shell = createMockShell();
  });

  it('returns 400 when request body is missing', async () => {
    const res = handleWasmEditDecision(shell, 'edit_42');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Missing request body');
  });

  it('returns 400 when request body is not valid JSON', async () => {
    const res = handleWasmEditDecision(shell, 'edit_42', 'not json');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Invalid JSON body');
  });

  it('returns 501 when respondToEditDecision is not available on the shell', async () => {
    const shellWithoutMethod = createMockShell({ respondToEditDecision: undefined });
    const bodyStr = JSON.stringify({ accepted_hunks: ['hunk-0'], rejected: false });
    const res = handleWasmEditDecision(shellWithoutMethod, 'edit_42', bodyStr);
    expect(res.status).toBe(501);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('respondToEditDecision not available (WASM binary too old)');
  });

  it('returns 404 when delivered is false (unknown/expired request)', async () => {
    const shellWithMethod = createMockShell({
      respondToEditDecision: () => ({ delivered: false }),
    });
    const bodyStr = JSON.stringify({ accepted_hunks: ['hunk-0'], rejected: false });
    const res = handleWasmEditDecision(shellWithMethod, 'edit_gone', bodyStr);
    expect(res.status).toBe(404);
    const body = JSON.parse(await res.text());
    expect(body.error).toContain('not found or already expired');
  });

  it('returns 200 with correct response when delivered is true', async () => {
    const shellWithMethod = createMockShell({
      respondToEditDecision: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({ accepted_hunks: ['hunk-0', 'hunk-1'], rejected: false });
    const res = handleWasmEditDecision(shellWithMethod, 'edit_42', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body).toEqual({
      edit_id: 'edit_42',
      decided: true,
      accepted: 2,
      rejected: false,
    });
  });

  it('returns 200 with rejected=true when user rejects', async () => {
    const shellWithMethod = createMockShell({
      respondToEditDecision: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({ accepted_hunks: [], rejected: true });
    const res = handleWasmEditDecision(shellWithMethod, 'edit_99', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body).toEqual({
      edit_id: 'edit_99',
      decided: true,
      accepted: 0,
      rejected: true,
    });
  });

  it('defaults accepted_hunks to [] and rejected to false when omitted', async () => {
    const shellWithMethod = createMockShell({
      respondToEditDecision: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({});
    const res = handleWasmEditDecision(shellWithMethod, 'edit_1', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body).toEqual({
      edit_id: 'edit_1',
      decided: true,
      accepted: 0,
      rejected: false,
    });
  });
});

// ---------------------------------------------------------------------------
// handleWasmShellApprovalDecision (INT-5/cloud shell-approval wiring)
// ---------------------------------------------------------------------------

describe('handleWasmShellApprovalDecision — /api/shell-approvals/{id}/decision', () => {
  let shell: WasmShell;

  beforeEach(() => {
    shell = createMockShell();
  });

  it('returns 400 when request body is missing', async () => {
    const res = handleWasmShellApprovalDecision(shell, 'shell_42');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Missing request body');
  });

  it('returns 400 when request body is not valid JSON', async () => {
    const res = handleWasmShellApprovalDecision(shell, 'shell_42', 'not json');
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('Invalid JSON body');
  });

  it('returns 400 when decisions is missing from JSON body', async () => {
    const bodyStr = JSON.stringify({ request_id: 'shell_42' });
    const res = handleWasmShellApprovalDecision(shell, 'shell_42', bodyStr);
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('decisions map required');
  });

  it('returns 400 when decisions is an array (not an object)', async () => {
    const bodyStr = JSON.stringify({ request_id: 'shell_42', decisions: [true, false] });
    const res = handleWasmShellApprovalDecision(shell, 'shell_42', bodyStr);
    expect(res.status).toBe(400);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('decisions map required');
  });

  it('returns 501 when respondToShellApproval is not available on the shell', async () => {
    const shellWithoutMethod = createMockShell({ respondToShellApproval: undefined });
    const bodyStr = JSON.stringify({ request_id: 'shell_42', decisions: { part_1: true } });
    const res = handleWasmShellApprovalDecision(shellWithoutMethod, 'shell_42', bodyStr);
    expect(res.status).toBe(501);
    const body = JSON.parse(await res.text());
    expect(body.error).toBe('respondToShellApproval not available (WASM binary too old)');
  });

  it('returns 410 when delivered is false (unknown/expired request)', async () => {
    const shellWithMethod = createMockShell({
      respondToShellApproval: () => ({ delivered: false }),
    });
    const bodyStr = JSON.stringify({ request_id: 'shell_gone', decisions: { part_1: true } });
    const res = handleWasmShellApprovalDecision(shellWithMethod, 'shell_gone', bodyStr);
    expect(res.status).toBe(410);
    const body = JSON.parse(await res.text());
    expect(body.error).toContain('not delivered');
  });

  it('returns 200 with correct response when delivered is true', async () => {
    const shellWithMethod = createMockShell({
      respondToShellApproval: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({ request_id: 'shell_42', decisions: { part_1: true, part_2: false } });
    const res = handleWasmShellApprovalDecision(shellWithMethod, 'shell_42', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body).toEqual({
      ok: true,
      request_id: 'shell_42',
      delivered: true,
    });
  });

  it('returns 200 with all parts rejected', async () => {
    const shellWithMethod = createMockShell({
      respondToShellApproval: () => ({ delivered: true }),
    });
    const bodyStr = JSON.stringify({ request_id: 'shell_99', decisions: { part_1: false, part_2: false } });
    const res = handleWasmShellApprovalDecision(shellWithMethod, 'shell_99', bodyStr);
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body).toEqual({
      ok: true,
      request_id: 'shell_99',
      delivered: true,
    });
  });
});

// ---------------------------------------------------------------------------
// handleWasmFileList — /api/files single-level shape (folder preservation)
// ---------------------------------------------------------------------------

describe('handleWasmFileList — /api/files returns single-level listings', () => {
  function dirShell(cwd: string, dirs: Record<string, WasmDirEntry[]>): WasmShell {
    return {
      ...({} as WasmShell),
      executeCommand: () => ({ stdout: '', stderr: '', exitCode: 0 }),
      autoComplete: () => ({ completions: [] }),
      getCwd: () => cwd,
      changeDir: () => ({ cwd }),
      writeFile: () => '',
      readFile: () => ({ content: '' }),
      listDir: (p: string) => {
        const entries = dirs[p];
        if (!entries) return { entries: [], error: `no such directory: ${p}` };
        return { entries };
      },
      deleteFile: () => '',
      runAgent: async () => ({ response: '', provider: '', model: '' }),
      clearConversation: () => {},
      stopAgent: () => {},
    } as unknown as WasmShell;
  }

  it('lists immediate children with is_dir flags and excludes .git (no recursion)', async () => {
    const shell = dirShell('/work', {
      '/work': [
        { name: '.git', type: 'dir', size: 0, mode: 0 },
        { name: 'api', type: 'dir', size: 0, mode: 0 },
        { name: 'LICENSE', type: 'file', size: 10, mode: 0 },
      ],
      '/work/api': [{ name: 'access_token.go', type: 'file', size: 100, mode: 0 }],
    });
    const res = handleWasmLocal(shell, '/api/files', 'GET', '/api/files?path=%2Fwork');
    const body = JSON.parse(await res.text());
    const names = (body.files as Array<{ name: string }>).map((f) => f.name);
    expect(names).toContain('api');
    expect(names).toContain('LICENSE');
    expect(names).not.toContain('.git', '.git directory must be hidden');
    expect(names).not.toContain('access_token.go', 'must be single-level, not recursive');
    const api = (body.files as Array<Record<string, unknown>>).find((f) => f.name === 'api');
    expect(api?.is_dir).toBe(true);
    expect(api?.path).toBe('/work/api');
    expect(api?.relative).toBe('api');
  });

  it("child fetch for an expanded directory returns that directory's files", async () => {
    const shell = dirShell('/work', {
      '/work/api': [{ name: 'access_token.go', type: 'file', size: 100, mode: 0 }],
    });
    const res = handleWasmLocal(shell, '/api/files', 'GET', '/api/files?path=%2Fwork%2Fapi');
    const body = JSON.parse(await res.text());
    const files = body.files as Array<{ name: string; is_dir: boolean; path: string }>;
    expect(files).toHaveLength(1);
    expect(files[0].name).toBe('access_token.go');
    expect(files[0].is_dir).toBe(false);
    expect(files[0].path).toBe('/work/api/access_token.go');
  });

  it('falls back to the manifest with implicit directory entries when listDir fails', async () => {
    trackFileWrite('/mfl-a/api/access_token.go');
    trackFileWrite('/mfl-a/device/poller.go');
    trackFileWrite('/mfl-a/LICENSE');
    const shell = dirShell('/mfl-a', {}); // listDir errors for everything
    const res = handleWasmLocal(shell, '/api/files', 'GET', '/api/files?path=%2Fmfl-a');
    const body = JSON.parse(await res.text());
    const files = body.files as Array<{ name: string; is_dir: boolean; path: string }>;
    const byName = new Map(files.map((f) => [f.name, f]));
    expect(byName.get('api')?.is_dir).toBe(true, 'nested files imply a directory entry');
    expect(byName.get('api')?.path).toBe('/mfl-a/api');
    expect(byName.get('device')?.is_dir).toBe(true);
    expect(byName.get('LICENSE')?.is_dir).toBe(false);
    expect(byName.get('LICENSE')?.path).toBe('/mfl-a/LICENSE');
    // directories sort before files
    expect(files.findIndex((f) => f.name === 'api')).toBeLessThan(files.findIndex((f) => f.name === 'LICENSE'));
  });

  it('CWD mismatch: files written elsewhere are listed from the manifest root', async () => {
    trackFileWrite('/mfl-b/api/x.go');
    const shell = dirShell('/other', {}); // listDir('/other') errors, nothing under /other
    const res = handleWasmLocal(shell, '/api/files', 'GET', '/api/files?path=%2Fother');
    const body = JSON.parse(await res.text());
    const files = body.files as Array<{ name: string; is_dir: boolean; path: string }>;
    const mflb = files.find((f) => f.name === 'mfl-b');
    (expect(mflb).toBeDefined(), 'manifest fallback must surface files written to a different root');
    expect(mflb?.is_dir).toBe(true);
    expect(mflb?.path).toBe('/mfl-b');
  });
});
