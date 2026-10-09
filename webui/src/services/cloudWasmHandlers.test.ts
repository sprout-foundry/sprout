// @vitest-environment jsdom

import { describe, it, expect, beforeEach, vi } from 'vitest';
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
    readFileBytes: () => ({ error: 'not found' }),
    saveImage: () => ({ error: 'not implemented' }),
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

describe('handleWasmFile — POST /api/file (save)', () => {
  // BufferManagerContext clears a tab's unsaved flag only when the save
  // response is {message: 'File saved successfully'} or {success: true} —
  // the daemon's contract. Anything else leaves the tab dirty and the 30s
  // autosave rewrites it forever.
  it('answers with the save contract the buffer manager checks for', async () => {
    const writes: Array<[string, string]> = [];
    const shell = createMockShell({
      writeFile: (p: string, c: string) => {
        writes.push([p, c]);
        return '';
      },
    });
    const res = handleWasmLocal(
      shell,
      '/api/file',
      'POST',
      '/api/file?path=/src/main.go',
      JSON.stringify({ content: 'package main\n' }),
    );
    expect(res.status).toBe(200);
    const body = JSON.parse(await res.text());
    expect(body.success).toBe(true);
    expect(body.message).toBe('File saved successfully');
    expect(writes).toEqual([['/src/main.go', 'package main\n']]);
  });

  it('reports a failed write as an error', async () => {
    const shell = createMockShell({ writeFile: () => 'disk full' });
    const res = handleWasmLocal(shell, '/api/file', 'POST', '/api/file?path=/a.txt', JSON.stringify({ content: 'x' }));
    expect(res.status).toBe(500);
  });
});

describe('listAllVfsFiles', () => {
  it('lists files in subfolders and leaves .git out', async () => {
    const { listAllVfsFiles } = await import('./cloudWasmHandlers');
    const tree: Record<string, WasmDirEntry[]> = {
      '/home/user': [
        { name: 'README.md', type: 'file', size: 1 },
        { name: 'api', type: 'dir', size: 0 },
        { name: '.git', type: 'dir', size: 0 },
      ],
      '/home/user/api': [{ name: 'form.go', type: 'file', size: 1 }],
      '/home/user/.git': [{ name: 'HEAD', type: 'file', size: 1 }],
    };
    const shell = createMockShell({
      listDir: (dir: string) => ({ entries: tree[dir] ?? [] }),
      readFile: (path: string) => ({ content: `content of ${path}` }),
    });

    const files = await listAllVfsFiles(shell);

    expect(files.map((f) => f.path).sort()).toEqual(['README.md', 'api/form.go']);
    expect(files.find((f) => f.path === 'api/form.go')?.content).toBe('content of /home/user/api/form.go');
  });
});

describe('listAllVfsFiles with the workspace at the filesystem root', () => {
  it('leaves the agent home and scratch space out of the repository files', async () => {
    const { listAllVfsFiles } = await import('./cloudWasmHandlers');
    const tree: Record<string, WasmDirEntry[]> = {
      '/': [
        { name: 'go.mod', type: 'file', size: 1 },
        { name: 'home', type: 'dir', size: 0 },
        { name: 'tmp', type: 'dir', size: 0 },
      ],
      '/tmp': [{ name: 'scratch.txt', type: 'file', size: 1 }],
      '/home': [{ name: 'user', type: 'dir', size: 0 }],
      '/home/user': [{ name: '.config', type: 'dir', size: 0 }],
      '/home/user/.config': [{ name: 'platform.json', type: 'file', size: 1 }],
    };
    const shell = createMockShell({
      getCwd: () => '/',
      listDir: (dir: string) => ({ entries: tree[dir] ?? [] }),
      readFile: () => ({ content: 'x' }),
    });

    const files = await listAllVfsFiles(shell);

    expect(files.map((f) => f.path)).toEqual(['go.mod']);
  });
});

describe('handleWasmFileList with the workspace at the filesystem root', () => {
  const tree: Record<string, WasmDirEntry[]> = {
    '/': [
      { name: 'go.mod', type: 'file', size: 1 },
      { name: 'home', type: 'dir', size: 0 },
      { name: 'tmp', type: 'dir', size: 0 },
    ],
    '/tmp': [{ name: 'sprout', type: 'dir', size: 0 }],
    '/home': [{ name: 'user', type: 'dir', size: 0 }],
    '/home/user': [{ name: '.config', type: 'dir', size: 0 }],
  };
  const listing = async (cwd: string, path: string) => {
    const shell = createMockShell({ getCwd: () => cwd, listDir: (dir: string) => ({ entries: tree[dir] ?? [] }) });
    const res = handleWasmLocal(shell, '/api/files', 'GET', `/api/files?path=${encodeURIComponent(path)}`);
    return (JSON.parse(await res.text()).files as Array<{ name: string }>).map((f) => f.name);
  };

  it('leaves the agent home and scratch space out of the tree', async () => {
    expect(await listing('/', '/')).toEqual(['go.mod']);
  });

  it('keeps a folder that holds more than the agent home', async () => {
    tree['/home'] = [
      { name: 'user', type: 'dir', size: 0 },
      { name: 'shared', type: 'dir', size: 0 },
    ];
    expect(await listing('/', '/')).toEqual(['go.mod', 'home']);
    expect(await listing('/', '/home')).toEqual(['shared']);
    tree['/home'] = [{ name: 'user', type: 'dir', size: 0 }];
  });

  it('shows it when the workspace is inside it', async () => {
    expect(await listing('/home/user', '/home/user')).toEqual(['.config']);
  });
});

describe('file mutations announce file_changed like the daemon', () => {
  it('reports writes, creates, deletes and renames', async () => {
    const { setAgentEventDispatcher } = await import('./cloudWasmHandlers');
    const events: Array<{ type: string; data: { file_path: string; action: string } }> = [];
    setAgentEventDispatcher((e) => events.push(e as (typeof events)[number]));
    const shell = createMockShell();

    handleWasmLocal(shell, '/api/file', 'POST', '/api/file?path=/w/a.ts', JSON.stringify({ content: 'x' }));
    handleWasmLocal(shell, '/api/create', 'POST', '/api/create', JSON.stringify({ path: '/w/b.ts' }));
    handleWasmLocal(shell, '/api/delete', 'POST', '/api/delete', JSON.stringify({ path: '/w/c.ts' }));
    handleWasmLocal(
      shell,
      '/api/rename',
      'POST',
      '/api/rename',
      JSON.stringify({ old_path: '/w/d.ts', new_path: '/w/e.ts' }),
    );
    handleWasmLocal(shell, '/api/file', 'GET', '/api/file?path=/w/a.ts');
    await Promise.resolve();
    setAgentEventDispatcher(null);

    expect(events.map((e) => `${e.type} ${e.data.action} ${e.data.file_path}`)).toEqual([
      'file_changed write /w/a.ts',
      'file_changed created /w/b.ts',
      'file_changed deleted /w/c.ts',
      'file_changed deleted /w/d.ts',
      'file_changed created /w/e.ts',
    ]);
  });
});

describe('/api/query in the browser — a stop is not a failure', () => {
  function runningShell() {
    let reject: (err: Error) => void = () => {};
    const shell = createMockShell({
      runAgent: () =>
        new Promise((_, r) => {
          reject = r;
        }),
      stopAgent: () => reject(new Error('process query: query interrupted: context canceled')),
    });
    return { shell, fail: (msg: string) => reject(new Error(msg)) };
  }
  const settle = () => new Promise((r) => setTimeout(r, 0));

  async function eventsFor(run: (shell: WasmShell, fail: (msg: string) => void) => void) {
    const { setAgentEventDispatcher } = await import('./cloudWasmHandlers');
    const events: Array<{ type: string; data: Record<string, unknown> }> = [];
    setAgentEventDispatcher((e) => events.push(e as (typeof events)[number]));
    const { shell, fail } = runningShell();
    handleWasmLocal(shell, '/api/query', 'POST', '/api/query', JSON.stringify({ query: 'hi', chat_id: 'c1' }));
    await settle();
    run(shell, fail);
    await settle();
    setAgentEventDispatcher(null);
    return events.filter((e) => e.type === 'query_completed' || e.type === 'error');
  }

  it('ends a stopped run as interrupted, with no error', async () => {
    const events = await eventsFor((shell) => {
      handleWasmLocal(shell, '/api/query/stop', 'POST', '/api/query/stop?chat_id=c1');
    });
    expect(events).toEqual([
      { type: 'query_completed', data: { query: 'hi', response: '', status: 'interrupted', chat_id: 'c1' } },
    ]);
  });

  it('still reports a run that fails on its own', async () => {
    const events = await eventsFor((_shell, fail) => fail('upstream exploded'));
    expect(events.map((e) => e.type)).toEqual(['error']);
  });
});

describe('/api/query in the browser — the host session store is appended by the event handler', () => {
  const settle = () => new Promise((r) => setTimeout(r, 0));

  it('does not POST turns itself; the shared event-handler seam owns that', async () => {
    // The append lives in useWebSocketEventHandler (above the per-chat filter),
    // so it covers both backends and background chats uniformly. The WASM query
    // handler must not append a second time.
    const { setActiveHost } = await import('../host/accessor');
    const { headlessHost } = await import('../host/HostProvider');
    const base = headlessHost();
    setActiveHost({
      ...base,
      transport: { apiBaseURL: 'https://host.test/backend', wsURL: '', authMode: 'bearer' },
      capabilities: { ...base.capabilities, chat: true, chatSessions: true },
    });

    const fetchSpy = vi.fn(async () => new Response('{}', { status: 200 }));
    vi.stubGlobal('fetch', fetchSpy);
    try {
      const shell = createMockShell({ runAgent: async () => ({ response: 'a', provider: 'p', model: 'm' }) });
      handleWasmLocal(shell, '/api/query', 'POST', '/api/query', JSON.stringify({ query: 'q', chat_id: 'c-host' }));
      await settle();
      await settle();
      expect(fetchSpy.mock.calls.some((c) => String(c[0]).includes('/api/chat-sessions/turn'))).toBe(false);
    } finally {
      vi.unstubAllGlobals();
      setActiveHost(headlessHost());
    }
  });

  it('dispatches query_completed carrying the query, so the append seam sees the answer', async () => {
    // The append seam reads `eventData.query`; the daemon's query_completed
    // carries it, so the in-browser agent's must too, or the answer would
    // never reach a host store for the WASM backend.
    const { setAgentEventDispatcher } = await import('./cloudWasmHandlers');
    const events: Array<{ type: string; data: Record<string, unknown> }> = [];
    setAgentEventDispatcher((event) => events.push(event as { type: string; data: Record<string, unknown> }));
    try {
      const shell = createMockShell({ runAgent: async () => ({ response: '4', provider: 'p', model: 'm' }) });
      handleWasmLocal(
        shell,
        '/api/query',
        'POST',
        '/api/query',
        JSON.stringify({ query: 'what is 2+2?', chat_id: 'c1' }),
      );
      await settle();
      await settle();
      const completed = events.find((e) => e.type === 'query_completed');
      expect(completed?.data.query).toBe('what is 2+2?');
      expect(completed?.data.response).toBe('4');
    } finally {
      setAgentEventDispatcher(null);
    }
  });
});

describe('/api/query in the browser — one run per chat', () => {
  it('rejects a second submit for a running chat with query_in_progress', async () => {
    const { setChatRunning } = await import('./cloudChatSessions');
    const shell = createMockShell();
    setChatRunning('c-busy', true);
    try {
      const res = handleWasmLocal(
        shell,
        '/api/query',
        'POST',
        '/api/query',
        JSON.stringify({ query: 'steer me', chat_id: 'c-busy' }),
      );
      expect(res.status).toBe(409);
      const body = JSON.parse(await res.text());
      expect(body.code).toBe('query_in_progress');
    } finally {
      setChatRunning('c-busy', false);
    }
  });

  it('accepts a submit for an idle chat', () => {
    const shell = createMockShell();
    const res = handleWasmLocal(
      shell,
      '/api/query',
      'POST',
      '/api/query',
      JSON.stringify({ query: 'hi', chat_id: 'c-idle' }),
    );
    expect(res.status).toBe(200);
  });

  it('always accepts /clear, even while the chat is running', async () => {
    const { setChatRunning } = await import('./cloudChatSessions');
    const shell = createMockShell();
    setChatRunning('c-clear', true);
    try {
      const res = handleWasmLocal(
        shell,
        '/api/query',
        'POST',
        '/api/query',
        JSON.stringify({ query: '/clear', chat_id: 'c-clear' }),
      );
      expect(res.status).toBe(200);
      const body = JSON.parse(await res.text());
      expect(body.message).toBe('Conversation cleared');
    } finally {
      setChatRunning('c-clear', false);
    }
  });
});

describe('handleWasmLocal — /api/search', () => {
  it('reports matches with workspace-relative paths', async () => {
    let ran = '';
    const shell = createMockShell({
      getWorkspaceRoot: () => '/workspace',
      executeCommand: (cmd: string) => {
        ran = cmd;
        return { stdout: '/workspace/src/a.go:3:func Device() {}\n', stderr: '', exitCode: 0 };
      },
    });

    const res = handleWasmLocal(shell, '/api/search', 'GET', 'http://x/api/search?query=Device', undefined);
    const body = await res.json();

    expect(ran).toContain("'/workspace'");
    expect(body.results.map((r: { file: string }) => r.file)).toEqual(['src/a.go']);
  });
});

import { handleWasmImageUpload, uploadBodyBytes } from './cloudWasmBinary';

describe('handleWasmLocal — binary files', () => {
  it('serves images byte-exact with their MIME type', async () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0xff, 0x00]);
    const shell = createMockShell({
      readFile: () => ({ content: 'mangled' }),
      readFileBytes: (path: string) => (path.endsWith('design/brand/logo.png') ? { bytes: png } : { error: 'missing' }),
    });
    const res = handleWasmLocal(shell, '/api/file', 'GET', '/api/file?path=design/brand/logo.png');
    expect(res.status).toBe(200);
    expect(res.headers.get('Content-Type')).toBe('image/png');
    expect(new Uint8Array(await res.arrayBuffer())).toEqual(png);
  });

  it('keeps text files on the text path', async () => {
    const shell = createMockShell({
      readFile: () => ({ content: 'body {}' }),
      readFileBytes: () => ({ error: 'should not be called' }),
    });
    const res = handleWasmLocal(shell, '/api/file', 'GET', '/api/file?path=design/generated/tokens.css');
    expect(res.headers.get('Content-Type')).toContain('text/plain');
    expect(await res.text()).toBe('body {}');
  });
});

describe('image upload in browser mode', () => {
  it('reads the image field of a FormData body as bytes', async () => {
    const form = new FormData();
    form.append('image', new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' }));
    expect(await uploadBodyBytes(form)).toEqual(new Uint8Array([1, 2, 3]));
    expect(await uploadBodyBytes('text')).toBeNull();
  });

  it('stores the image through the shell and replies like the daemon', async () => {
    const shell = createMockShell({
      saveImage: () => ({ path: '/workspace/.sprout/images/paste.png', filename: 'paste.png' }),
    });
    const res = handleWasmImageUpload(shell, new Uint8Array([1]));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ path: '/workspace/.sprout/images/paste.png', filename: 'paste.png' });
  });

  it('reports a rejected image as a 400', async () => {
    const shell = createMockShell({ saveImage: () => ({ error: 'Not a recognized image format' }) });
    const res = handleWasmImageUpload(shell, new Uint8Array([1]));
    expect(res.status).toBe(400);
  });
});

describe('the in-browser agent provider config follows the host backend', () => {
  const queryBody = JSON.stringify({ query: 'hello' });

  it('points the provider at the host model endpoint for a wasm backend', async () => {
    const { setActiveHost } = await import('../host/accessor');
    const { headlessHost } = await import('../host/HostProvider');
    setActiveHost({
      ...headlessHost(),
      transport: {
        apiBaseURL: '',
        wsURL: '',
        authMode: 'none',
        agent: { kind: 'wasm', modelEndpoint: 'https://models.host.test/v1/chat' },
      },
    });
    const written: Array<{ path: string; content: string }> = [];
    const shell = createMockShell({
      writeFile: (path: string, content: string) => {
        written.push({ path, content });
        return '';
      },
    });

    handleWasmLocal(shell, '/api/query', 'POST', '/api/query', queryBody);
    // The query handler writes the provider config synchronously before it
    // fires the (async) agent loop.
    const config = written.find((w) => w.path.endsWith('/providers/platform.json'));
    expect(config).toBeDefined();
    expect(JSON.parse(config!.content).endpoint).toBe('https://models.host.test/v1/chat');
    setActiveHost(headlessHost());
  });

  it('points the provider at the daemon API base for a daemon backend', async () => {
    const { setActiveHost } = await import('../host/accessor');
    const { headlessHost } = await import('../host/HostProvider');
    setActiveHost({
      ...headlessHost(),
      transport: {
        apiBaseURL: 'https://daemon.test',
        wsURL: 'wss://daemon.test/ws',
        authMode: 'none',
        agent: { kind: 'daemon', apiBaseURL: 'https://daemon.test', wsURL: 'wss://daemon.test/ws' },
      },
    });
    const written: Array<{ path: string; content: string }> = [];
    const shell = createMockShell({
      writeFile: (path: string, content: string) => {
        written.push({ path, content });
        return '';
      },
    });

    handleWasmLocal(shell, '/api/query', 'POST', '/api/query', queryBody);
    const config = written.find((w) => w.path.endsWith('/providers/platform.json'));
    expect(JSON.parse(config!.content).endpoint).toBe('https://daemon.test/api/proxy/chat');
    setActiveHost(headlessHost());
  });

  it('falls back to the platform proxy path when the host names no endpoint', async () => {
    const { setActiveHost } = await import('../host/accessor');
    const { headlessHost } = await import('../host/HostProvider');
    setActiveHost({ ...headlessHost() });
    const written: Array<{ path: string; content: string }> = [];
    const shell = createMockShell({
      writeFile: (path: string, content: string) => {
        written.push({ path, content });
        return '';
      },
    });

    handleWasmLocal(shell, '/api/query', 'POST', '/api/query', queryBody);
    const config = written.find((w) => w.path.endsWith('/providers/platform.json'));
    const origin = window.location.origin;
    expect(JSON.parse(config!.content).endpoint).toBe(`${origin}/proxy/chat`);
    setActiveHost(headlessHost());
  });
});
