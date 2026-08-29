/**
 * Tests for cloudTxn.ts — the ETH-2 txn client and the delta-manifest
 * builders. Network calls are stubbed on fetch (relative URLs + credentials,
 * same interception style as cloudTasks.test.ts); the builder tests exercise
 * the caps, path rules, base64 tolerance and binary roundtrip directly.
 */

import {
  applyPullManifest,
  base64ToBytes,
  buildPushManifest,
  bytesToBase64,
  CloudTxnError,
  createTxn,
  resolveTxnWorkspace,
  TXN_MAX_FILES,
  TXN_MAX_FILE_BYTES,
  txnFinish,
  txnPull,
  txnPush,
  txnRun,
  txnStatus,
  type TxnManifest,
} from './cloudTxn';

/** JSON Response helper (fresh body per call — Response bodies are one-shot). */
function jsonResponse(body: unknown, init?: { status?: number; headers?: Record<string, string> }) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
}

/** Route table for the platform surface, keyed by `<method> <path>`. */
function routeFetch(routes: Record<string, (body: unknown) => Response>) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : {};
    const handler = routes[`${method} ${url}`];
    if (!handler) return jsonResponse({ error: `no route for ${method} ${url}` }, { status: 404 });
    return handler(body);
  });
}

beforeEach(() => {
  vi.stubGlobal('fetch', routeFetch({}));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('cloudTxn base64 helpers', () => {
  it('roundtrips arbitrary bytes (binary-safe, no chunk boundaries hit)', () => {
    const bytes = new Uint8Array(0x9000);
    for (let i = 0; i < bytes.length; i += 1) bytes[i] = i % 256;
    expect(base64ToBytes(bytesToBase64(bytes))).toEqual(bytes);
  });

  it('roundtrips empty bytes', () => {
    expect(base64ToBytes(bytesToBase64(new Uint8Array(0)))).toEqual(new Uint8Array(0));
  });

  it('returns null for undecodable base64 instead of throwing', () => {
    expect(base64ToBytes('not!!valid~~base64!!!')).toBeNull();
  });
});

describe('buildPushManifest', () => {
  it('encodes file contents and reports sizes', async () => {
    const manifest = await buildPushManifest(() => [{ path: 'src/main.go', content: 'package main\n' }], {
      deletes: ['old.go'],
    });
    expect(manifest.files).toHaveLength(1);
    expect(manifest.files[0].path).toBe('src/main.go');
    expect(manifest.files[0].size).toBe(13);
    expect(manifest.files[0].mode).toBe('0644');
    expect(new TextDecoder().decode(base64ToBytes(manifest.files[0].content_base64) ?? new Uint8Array())).toBe(
      'package main\n',
    );
    expect(manifest.deletes).toEqual(['old.go']);
    expect(manifest.base.client).toBe('wasm');
    expect(manifest.truncated).toBe(false);
    expect(manifest.skipped).toEqual([]);
  });

  it('roundtrips Uint8Array contents byte-for-byte', async () => {
    const bytes = new Uint8Array([0, 1, 2, 250, 251, 252, 255]);
    const manifest = await buildPushManifest(() => [{ path: 'a.bin', content: bytes }]);
    const decoded = base64ToBytes(manifest.files[0].content_base64);
    expect(Array.from(decoded ?? [])).toEqual(Array.from(bytes));
    expect(manifest.files[0].size).toBe(bytes.byteLength);
  });

  it('skips files over the per-file cap with the contract reason', async () => {
    const big = new Uint8Array(TXN_MAX_FILE_BYTES + 1);
    const manifest = await buildPushManifest(() => [
      { path: 'ok.txt', content: 'hi' },
      { path: 'big.bin', content: big },
    ]);
    expect(manifest.files.map((f) => f.path)).toEqual(['ok.txt']);
    expect(manifest.skipped).toEqual([{ path: 'big.bin', reason: 'exceeds_per_file_cap' }]);
    expect(manifest.truncated).toBe(true);
  });

  it('allows a file of exactly the per-file cap', async () => {
    const exact = new Uint8Array(TXN_MAX_FILE_BYTES);
    const manifest = await buildPushManifest(() => [{ path: 'exact.bin', content: exact }]);
    expect(manifest.files).toHaveLength(1);
    expect(manifest.skipped).toEqual([]);
  });

  it('skips entries beyond the file-count cap', async () => {
    const files = Array.from({ length: TXN_MAX_FILES + 2 }, (_, i) => ({ path: `f${i}.txt`, content: 'x' }));
    const manifest = await buildPushManifest(() => files);
    expect(manifest.files).toHaveLength(TXN_MAX_FILES);
    expect(manifest.skipped).toEqual([
      { path: 'f2000.txt', reason: 'exceeds_file_count_cap' },
      { path: 'f2001.txt', reason: 'exceeds_file_count_cap' },
    ]);
  });

  it('skips files that would exceed the total cap', async () => {
    // Same arithmetic as the contract caps but with tiny injectable values:
    // three 1 KiB chunks fill a 3 KiB total exactly; the next entries —
    // however small — tip over. (The real 5 MiB/100 MiB values make this
    // test encode 100 MiB of base64 and blow the runner timeout.)
    const maxFileBytes = 1024;
    const maxTotalBytes = 3 * 1024;
    const chunk = new Uint8Array(maxFileBytes);
    const inputs = Array.from({ length: maxTotalBytes / maxFileBytes }, (_, i) => ({
      path: `part-${i}.bin`,
      content: chunk,
    }));
    inputs.push({ path: 'over.txt', content: 'tiny' });
    inputs.push({ path: 'after.txt', content: 'also tiny' });

    const manifest = await buildPushManifest(() => inputs, { maxFileBytes, maxTotalBytes });
    expect(manifest.files).toHaveLength(3);
    expect(manifest.skipped).toEqual([
      { path: 'over.txt', reason: 'exceeds_total_cap' },
      { path: 'after.txt', reason: 'exceeds_total_cap' },
    ]);
    expect(manifest.truncated).toBe(true);
  });

  it('skips paths that violate the contract path rules', async () => {
    const manifest = await buildPushManifest(
      () => [
        { path: 'ok.txt', content: 'x' },
        { path: '', content: 'x' },
        { path: '/abs.txt', content: 'x' },
        { path: 'a/../b.txt', content: 'x' },
        { path: '.git/config', content: 'x' },
        { path: 'sub/.git/x', content: 'x' },
        { path: 'a//b.txt', content: 'x' },
      ],
      { deletes: ['../escape.go'] },
    );
    expect(manifest.files.map((f) => f.path)).toEqual(['ok.txt']);
    const reasons = Object.fromEntries(manifest.skipped.map((s) => [s.path, s.reason]));
    expect(reasons['']).toBe('empty_path');
    expect(reasons['/abs.txt']).toBe('absolute_path');
    expect(reasons['a/../b.txt']).toBe('path_traversal');
    expect(reasons['.git/config']).toBe('git_path');
    expect(reasons['sub/.git/x']).toBe('git_path');
    expect(reasons['a//b.txt']).toBe('invalid_path');
    expect(reasons['../escape.go']).toBe('path_traversal');
  });
});

describe('applyPullManifest', () => {
  const run = async (manifest: TxnManifest) => {
    const written: Array<{ path: string; content: string | Uint8Array }> = [];
    const deleted: string[] = [];
    const result = await applyPullManifest(manifest, {
      writeFiles: async (files) => {
        written.push(...files);
      },
      deleteFiles: async (paths) => {
        deleted.push(...paths);
      },
    });
    return { result, written, deleted };
  };

  it('decodes files, applies deletes and reports counts', async () => {
    const { result, written, deleted } = await run({
      base: { git_sha: 'abc', client: 'container' },
      files: [{ path: 'out.txt', content_base64: bytesToBase64(new TextEncoder().encode('built')), size: 5 }],
      deletes: ['tmp.log'],
      truncated: false,
      skipped: [],
    });
    expect(result).toEqual({ applied: 1, deleted: 1, skipped: [] });
    expect(written).toHaveLength(1);
    expect(new TextDecoder().decode(written[0].content as Uint8Array)).toBe('built');
    expect(deleted).toEqual(['tmp.log']);
  });

  it('preserves binary bytes exactly (roundtrip)', async () => {
    const bytes = new Uint8Array([0, 17, 200, 255, 128]);
    const { written } = await run({
      base: { git_sha: '', client: 'container' },
      files: [{ path: 'logo.png', content_base64: bytesToBase64(bytes), size: bytes.byteLength }],
      deletes: [],
      truncated: false,
      skipped: [],
    });
    expect(written[0].content).toBeInstanceOf(Uint8Array);
    expect(Array.from(written[0].content as Uint8Array)).toEqual(Array.from(bytes));
  });

  it('skips bad base64 entries instead of failing the whole apply', async () => {
    const { result, written, deleted } = await run({
      base: { git_sha: '', client: 'container' },
      files: [
        { path: 'good.txt', content_base64: bytesToBase64(new TextEncoder().encode('ok')), size: 2 },
        { path: 'bad.bin', content_base64: '%%%not-base64%%%', size: 3 },
      ],
      deletes: ['gone.txt'],
      truncated: true,
      skipped: [],
    });
    expect(written.map((w) => w.path)).toEqual(['good.txt']);
    expect(deleted).toEqual(['gone.txt']);
    expect(result.applied).toBe(1);
    expect(result.skipped).toEqual([{ path: 'bad.bin', reason: 'invalid_base64' }]);
  });

  it('reports deletes as skipped when the bridge cannot delete', async () => {
    const written: unknown[] = [];
    const result = await applyPullManifest(
      {
        base: { git_sha: '', client: 'container' },
        files: [],
        deletes: ['a.txt', 'b.txt'],
        truncated: false,
        skipped: [],
      },
      {
        writeFiles: async (files) => {
          written.push(...files);
        },
      },
    );
    expect(written).toHaveLength(0);
    expect(result).toEqual({
      applied: 0,
      deleted: 0,
      skipped: [
        { path: 'a.txt', reason: 'delete_unsupported' },
        { path: 'b.txt', reason: 'delete_unsupported' },
      ],
    });
  });

  it('skips contract-invalid paths from a pull manifest', async () => {
    const { result, deleted } = await run({
      base: { git_sha: '', client: 'container' },
      files: [{ path: '../escape.txt', content_base64: 'aGk=', size: 2 }],
      deletes: ['/abs.txt'],
      truncated: false,
      skipped: [],
    });
    expect(result.applied).toBe(0);
    expect(deleted).toEqual([]);
    expect(result.skipped.map((s) => s.reason)).toEqual(['path_traversal', 'absolute_path']);
  });

  it('tolerates a malformed manifest (missing arrays)', async () => {
    const result = await applyPullManifest({} as TxnManifest, {
      writeFiles: async () => undefined,
    });
    expect(result).toEqual({ applied: 0, deleted: 0, skipped: [] });
  });
});

describe('resolveTxnWorkspace', () => {
  it('matches an existing workspace by repo_url, ignoring .git and trailing slash', async () => {
    const fetchMock = routeFetch({
      'GET /workspace/fly': () =>
        jsonResponse({
          workspaces: [
            { workspace_id: 'ws-other', repo_url: 'https://github.com/acme/other' },
            { workspace_id: 'ws-mine', repo_url: 'https://github.com/acme/app.git/' },
          ],
        }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const resolved = await resolveTxnWorkspace('https://github.com/acme/app');
    expect(resolved).toEqual({ workspaceId: 'ws-mine', created: false });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('creates a build workspace when the list has no match', async () => {
    const fetchMock = routeFetch({
      'GET /workspace/fly': () => jsonResponse({ workspaces: [] }),
      'POST /workspace/fly': (body) => {
        expect(body).toEqual({ repo_url: 'https://github.com/acme/app', mode: 'build' });
        return jsonResponse({ workspace_id: 'ws-new', status: 'running', repo_url: 'x' }, { status: 201 });
      },
    });
    vi.stubGlobal('fetch', fetchMock);

    expect(await resolveTxnWorkspace('https://github.com/acme/app')).toEqual({
      workspaceId: 'ws-new',
      created: true,
    });
  });

  it('falls through to create when the list request fails', async () => {
    vi.stubGlobal(
      'fetch',
      routeFetch({
        'GET /workspace/fly': () => jsonResponse({ error: 'boom' }, { status: 500 }),
        'POST /workspace/fly': () => jsonResponse({ workspace_id: 'ws-new' }, { status: 201 }),
      }),
    );
    expect(await resolveTxnWorkspace('https://github.com/acme/app')).toEqual({
      workspaceId: 'ws-new',
      created: true,
    });
  });

  it('rejects with the platform error when create fails', async () => {
    vi.stubGlobal(
      'fetch',
      routeFetch({
        'GET /workspace/fly': () => jsonResponse({ workspaces: [] }),
        'POST /workspace/fly': () => jsonResponse({ error: 'Overage spending cap reached.' }, { status: 402 }),
      }),
    );
    await expect(resolveTxnWorkspace('https://github.com/acme/app')).rejects.toThrow('Overage spending cap reached.');
  });

  it('requires a repo URL', async () => {
    await expect(resolveTxnWorkspace('')).rejects.toThrow(TypeError);
    await expect(resolveTxnWorkspace('   ')).rejects.toThrow(TypeError);
  });
});

describe('txn lifecycle client', () => {
  it('opens a txn with an empty body and surfaces txn_id/status', async () => {
    const fetchMock = routeFetch({
      'POST /workspace/fly/ws-1/txn': () =>
        jsonResponse(
          { txn_id: 'txn-9', status: 'push', expires_at: '2030-01-01T00:00:00Z', workspace_id: 'ws-1' },
          {
            status: 201,
          },
        ),
    });
    vi.stubGlobal('fetch', fetchMock);

    expect(await createTxn('ws-1')).toEqual({
      txn_id: 'txn-9',
      status: 'push',
      expires_at: '2030-01-01T00:00:00Z',
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/fly/ws-1/txn');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(JSON.parse(init.body as string)).toEqual({});
  });

  it('rejects a 409 with a CloudTxnError carrying the status', async () => {
    vi.stubGlobal(
      'fetch',
      routeFetch({
        'POST /workspace/fly/ws-1/txn': () => jsonResponse({ error: 'a transaction is already open' }, { status: 409 }),
      }),
    );
    const err = await createTxn('ws-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CloudTxnError);
    expect((err as CloudTxnError).status).toBe(409);
    expect((err as Error).message).toBe('a transaction is already open');
  });

  it('pushes the manifest verbatim to /push', async () => {
    const fetchMock = routeFetch({
      'POST /workspace/fly/ws-1/txn/txn-9/push': () =>
        jsonResponse({ applied: 2, deleted: 1, skipped: [], status: 'ok' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const manifest = await buildPushManifest(() => [{ path: 'a.txt', content: 'a' }]);
    const result = await txnPush('ws-1', 'txn-9', manifest);
    expect(result.applied).toBe(2);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/fly/ws-1/txn/txn-9/push');
    expect(JSON.parse(init.body as string)).toEqual(manifest);
  });

  it('runs a command with the timeout and returns the run result', async () => {
    const fetchMock = routeFetch({
      'POST /workspace/fly/ws-1/txn/txn-9/run': (body) => {
        expect(body).toEqual({ command: 'go build ./...', timeout_seconds: 600 });
        return jsonResponse({
          stdout: 'ok',
          stderr: '',
          exit_code: 0,
          duration_ms: 1200,
          timed_out: false,
          truncated: false,
        });
      },
    });
    vi.stubGlobal('fetch', fetchMock);

    expect(await txnRun('ws-1', 'txn-9', 'go build ./...', 600)).toMatchObject({ exit_code: 0, duration_ms: 1200 });
  });

  it('requires a non-empty command for run', () => {
    // Synchronous validation, so assert on the call rather than a rejection.
    expect(() => txnRun('ws-1', 'txn-9', '   ')).toThrow(TypeError);
  });

  it('pulls a manifest and normalizes missing arrays', async () => {
    vi.stubGlobal(
      'fetch',
      routeFetch({
        'POST /workspace/fly/ws-1/txn/txn-9/pull': () => jsonResponse({ files: null, deletes: null }),
      }),
    );
    const manifest = await txnPull('ws-1', 'txn-9');
    expect(manifest.files).toEqual([]);
    expect(manifest.deletes).toEqual([]);
    expect(manifest.base).toEqual({ git_sha: '', client: 'container' });
  });

  it('gets txn status via GET', async () => {
    const fetchMock = routeFetch({
      'GET /workspace/fly/ws-1/txn/txn-9': () =>
        jsonResponse({ txn_id: 'txn-9', status: 'done', created_at: 't', expires_at: 't', run_result: null }),
    });
    vi.stubGlobal('fetch', fetchMock);

    expect((await txnStatus('ws-1', 'txn-9')).status).toBe('done');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/fly/ws-1/txn/txn-9');
    expect(init.method).toBe('GET');
  });

  it('finishes the txn', async () => {
    const fetchMock = routeFetch({
      'POST /workspace/fly/ws-1/txn/txn-9/finish': () =>
        jsonResponse({ status: 'done', txn_duration_seconds: 42, stop_initiated: true }),
    });
    vi.stubGlobal('fetch', fetchMock);

    expect(await txnFinish('ws-1', 'txn-9')).toEqual({
      status: 'done',
      txn_duration_seconds: 42,
      stop_initiated: true,
    });
  });

  it('URI-encodes ids with reserved characters', async () => {
    const fetchMock = routeFetch({
      'POST /workspace/fly/ws%201/txn/txn%2F9/finish': () => jsonResponse({ status: 'done' }),
    });
    vi.stubGlobal('fetch', fetchMock);
    await txnFinish('ws 1', 'txn/9');
    expect(fetchMock).toHaveBeenCalledWith('/workspace/fly/ws%201/txn/txn%2F9/finish', expect.anything());
  });
});
