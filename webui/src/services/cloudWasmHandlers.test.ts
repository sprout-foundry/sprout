// @vitest-environment jsdom

import { describe, it, expect, beforeEach } from 'vitest';
import { handleWasmLocal } from './cloudWasmHandlers';
import type { WasmShell } from './wasmShell';

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
