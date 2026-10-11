/**
 * The workspace file routes against the real WASM binary: a repo imported
 * through CloudAdapter.restoreRepo is listed, read and searched by the file
 * API at every root spelling, and the editor and the agent's shell see each
 * other's writes.
 *
 * Runs when the binary has been built into public/wasm (scripts/build-wasm.sh
 * or `make build-wasm`); skipped otherwise. The binary runs in its own VM
 * context with the bundled wasm_exec.js, whose in-memory filesystem is the
 * one the browser uses.
 */

import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import vm from 'node:vm';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { CloudAdapter } from './cloudAdapter';
import { createWasmShellBridge } from './wasmShell';
import type { SproutWasmAPI, WasmShell } from './wasmShell';

const WASM_DIR = join(process.cwd(), 'public', 'wasm');
const haveBinary = existsSync(join(WASM_DIR, 'sprout.wasm')) && existsSync(join(WASM_DIR, 'wasm_exec.js'));

const mocks = vi.hoisted(() => ({ shell: null as unknown }));

vi.mock('./wasmShell', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  initWasmShell: vi.fn(() => Promise.resolve(mocks.shell)),
}));
vi.mock('./repoImportCache', () => ({
  loadRepoImport: vi.fn(() => Promise.resolve(null)),
  saveRepoImport: vi.fn(() => Promise.resolve()),
  setLastRepo: vi.fn(() => Promise.resolve()),
  getLastRepo: vi.fn(() => Promise.resolve(null)),
}));

/** Start the binary in a fresh VM context; its network calls fail fast. */
async function startBinary(): Promise<SproutWasmAPI> {
  const ctx: Record<string, unknown> = {
    console: { log() {}, error() {}, warn() {}, info() {}, debug() {} },
    TextEncoder,
    TextDecoder,
    crypto: globalThis.crypto,
    performance,
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    queueMicrotask,
    WebAssembly,
    Headers,
    Request,
    Response,
    AbortController,
    URL,
    fetch: () => Promise.reject(new Error('offline')),
  };
  ctx.globalThis = ctx;
  ctx.window = ctx;
  ctx.self = ctx;
  vm.createContext(ctx);
  vm.runInContext(readFileSync(join(WASM_DIR, 'wasm_exec.js'), 'utf8'), ctx);
  const Go = ctx.Go as new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): void };
  const go = new Go();
  const module = await WebAssembly.compile(readFileSync(join(WASM_DIR, 'sprout.wasm')));
  const instance = await WebAssembly.instantiate(module, go.importObject);
  go.run(instance);
  const wasm = ctx.SproutWasm as SproutWasmAPI;
  wasm.init();
  return wasm;
}

const REPO_FILES = [
  { path: 'README.md', content: '# hello\n' },
  { path: 'src/main.ts', content: 'export const answer = 42;\n' },
  { path: 'src/lib/util.ts', content: 'export const noop = () => {};\n' },
];
const ROOT_SPELLINGS = ['', '.', './', '/workspace', '/workspace/', 'workspace', 'workspace/'];

describe.skipIf(!haveBinary)('workspace file routes on the WASM binary', () => {
  let shell: WasmShell;
  let adapter: CloudAdapter;

  beforeAll(async () => {
    const wasm = await startBinary();
    window.SproutWasm = wasm;
    shell = createWasmShellBridge(wasm);
    mocks.shell = shell;
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify({ repo: 'octocat/hello', files: REPO_FILES }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    adapter = new CloudAdapter({ apiBase: 'https://platform.test', wsUrl: 'wss://platform.test/ws', navItems: [] });
    expect(await adapter.restoreRepo('https://github.com/octocat/hello')).toMatchObject({ success: true });
  }, 120_000);

  afterAll(() => {
    vi.unstubAllGlobals();
    delete window.SproutWasm;
  });

  const list = async (path: string) => {
    const res = await adapter.fetch(`/api/files?path=${encodeURIComponent(path)}`);
    return ((await res.json()).files as Array<{ name: string }>).map((f) => f.name).sort();
  };
  const read = async (path: string) => {
    const res = await adapter.fetch(`/api/file?path=${encodeURIComponent(path)}`);
    return { status: res.status, text: await res.text() };
  };

  it('reports the root the binary uses', async () => {
    expect(shell.getWorkspaceRoot()).toBe('/workspace');
    expect(await (await adapter.fetch('/api/workspace')).json()).toMatchObject({ workspace_root: '/workspace' });
  });

  it.each(ROOT_SPELLINGS)('lists the imported files at the root spelled %j', async (spelling) => {
    expect(await list(spelling)).toEqual(['README.md', 'src']);
  });

  it('reads imported files by relative and absolute path', async () => {
    expect(await read('src/main.ts')).toEqual({ status: 200, text: 'export const answer = 42;\n' });
    expect(await read('/workspace/src/main.ts')).toEqual({ status: 200, text: 'export const answer = 42;\n' });
  });

  it("round-trips between the editor and the agent's shell", async () => {
    const save = await adapter.fetch(`/api/file?path=${encodeURIComponent('src/lib/util.ts')}`, {
      method: 'POST',
      body: JSON.stringify({ content: 'export const noop = 1;\n' }),
    });
    expect(save.status).toBe(200);
    expect(shell.executeCommand('cat src/lib/util.ts').stdout).toBe('export const noop = 1;\n');

    expect(shell.executeCommand('echo from-agent > AGENT.md').exitCode).toBe(0);
    expect(await list('.')).toEqual(['AGENT.md', 'README.md', 'src']);
    expect(await read('AGENT.md')).toEqual({ status: 200, text: 'from-agent\n' });
  });

  it('keeps tree operations and search in the workspace', async () => {
    const post = (url: string, body: unknown) => adapter.fetch(url, { method: 'POST', body: JSON.stringify(body) });
    expect((await post('/api/create', { path: 'todo.txt' })).status).toBe(200);
    expect((await post('/api/rename', { old_path: 'todo.txt', new_path: 'src/todo.txt' })).status).toBe(200);
    expect(await list('src')).toContain('todo.txt');
    expect((await post('/api/delete', { path: 'src/todo.txt' })).status).toBe(200);
    expect(await list('src')).not.toContain('todo.txt');
    expect(shell.executeCommand('ls /').stdout.split(/\s+/)).not.toContain('todo.txt');

    const search = await (await adapter.fetch('/api/search?query=answer')).json();
    expect(search.results.map((r: { file: string }) => r.file)).toEqual(['src/main.ts']);
    const hit = await read(search.results[0].file);
    expect(hit.status).toBe(200);
  });
});
