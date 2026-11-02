import { afterEach, describe, expect, it, vi } from 'vitest';

import { memoryFileStore } from './files';
import { loadSproutWasm, resetSproutWasmLoader, resolveWasmAssetUrls, runtimeEnv } from './runtime';
import { summarizeArgs, toAbsolutePath, toWorkspacePath } from './events';

type Globals = Record<string, unknown>;
const g = globalThis as unknown as Globals;

afterEach(() => {
  resetSproutWasmLoader();
  delete g.Go;
  delete g.SproutWasm;
  delete g.__sproutStore;
  vi.restoreAllMocks();
});

/** An empty, valid WebAssembly module. */
const EMPTY_WASM = new Uint8Array([0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00]);

function fakeFetch(manifest: unknown, wasmType = 'application/octet-stream') {
  return vi.fn(async (url: string) => {
    if (url.endsWith('/wasm-manifest.json')) {
      return manifest ? new Response(JSON.stringify(manifest)) : new Response('', { status: 404 });
    }
    return new Response(EMPTY_WASM, { headers: { 'content-type': wasmType } });
  }) as unknown as typeof fetch & ReturnType<typeof vi.fn>;
}

describe('resolveWasmAssetUrls', () => {
  it('uses the manifest names, else the fixed names', () => {
    expect(resolveWasmAssetUrls('/w/', { wasm: 'sprout.abc.wasm', wasmExec: 'wasm_exec.def.js' })).toEqual({
      wasmUrl: '/w/sprout.abc.wasm',
      wasmExecUrl: '/w/wasm_exec.def.js',
    });
    expect(resolveWasmAssetUrls('/w', null)).toEqual({ wasmUrl: '/w/sprout.wasm', wasmExecUrl: '/w/wasm_exec.js' });
  });
});

describe('runtimeEnv', () => {
  it('turns the remote catalogs off unless asked for', () => {
    expect(runtimeEnv({})).toEqual({ SPROUT_MODEL_REGISTRY_URL: 'off', SPROUT_PROVIDER_REGISTRY_URL: 'off' });
    expect(runtimeEnv({ remoteRegistries: true, env: { DEBUG: '1' } })).toEqual({ DEBUG: '1' });
  });
});

describe('loadSproutWasm', () => {
  it('restores the store, starts the module and initializes it once', async () => {
    const init = vi.fn(() => '');
    let restoredAtInit = '';
    let envAtRun: Record<string, string> = {};
    g.Go = class {
      importObject = {};
      env: Record<string, string> = {};
      run() {
        envAtRun = this.env;
        restoredAtInit = (g.__sproutStore as { listFiles(): string }).listFiles();
        g.SproutWasm = { init };
      }
    };
    const store = memoryFileStore([{ path: '/workspace/a.txt', content: 'A' }]);
    const fetchImpl = fakeFetch({ wasm: 'sprout.123.wasm', wasmExec: 'wasm_exec.456.js' });

    const first = loadSproutWasm({ wasmBase: '/wasm', store, fetch: fetchImpl });
    const second = loadSproutWasm({ wasmBase: '/wasm', store, fetch: fetchImpl });
    expect(second).toBe(first);
    const bridge = await first;

    expect(bridge).toBe(g.SproutWasm);
    expect(envAtRun).toMatchObject({ SPROUT_MODEL_REGISTRY_URL: 'off', SPROUT_PROVIDER_REGISTRY_URL: 'off' });
    expect(init).toHaveBeenCalledTimes(1);
    expect(JSON.parse(restoredAtInit)).toEqual([{ path: '/workspace/a.txt', content: 'A', modTime: 0 }]);
    expect(fetchImpl.mock.calls.map((c) => c[0])).toEqual(['/wasm/wasm-manifest.json', '/wasm/sprout.123.wasm']);

    // Writes the module mirrors land in the host's store.
    const mirror = g.__sproutStore as { saveFile(p: string, c: string): void; deleteFile(p: string): void };
    mirror.saveFile('/workspace/b.txt', 'B');
    mirror.deleteFile('/workspace/a.txt');
    expect([...store.files]).toEqual([['/workspace/b.txt', 'B']]);
  });

  it('reuses a module already running on the page', async () => {
    const existing = { init: vi.fn() };
    g.SproutWasm = existing;
    expect(await loadSproutWasm({ wasmBase: '/wasm' })).toBe(existing);
    expect(existing.init).not.toHaveBeenCalled();
  });

  it('names a misrouted asset', async () => {
    g.Go = class {
      importObject = {};
      run() {}
    };
    await expect(loadSproutWasm({ wasmBase: '/wasm', fetch: fakeFetch(null, 'text/html') })).rejects.toThrow(
      /text\/html, not application\/wasm/,
    );
  });

  it('asks for the Go runtime outside a browser', async () => {
    await expect(loadSproutWasm({ wasmBase: '/wasm', fetch: fakeFetch(null) })).rejects.toThrow(/wasm_exec\.js/);
  });
});

describe('event helpers', () => {
  it('converts between workspace and absolute paths', () => {
    expect(toWorkspacePath('/workspace/design/a.html', '/workspace')).toBe('design/a.html');
    expect(toWorkspacePath('./x', '/workspace')).toBe('x');
    expect(toAbsolutePath('design/a.html', '/workspace')).toBe('/workspace/design/a.html');
    expect(toAbsolutePath('.', '/workspace')).toBe('/workspace');
    expect(toAbsolutePath('/etc/x', '/workspace')).toBe('/etc/x');
  });

  it('summarizes the primary argument', () => {
    expect(summarizeArgs({ content: 'x', path: 'a.html' }, '')).toBe('a.html');
    expect(summarizeArgs({ command: 'ls   -la\n' }, '')).toBe('ls -la');
    expect(summarizeArgs(undefined, '{"path": "big')).toBe('{"path": "big');
    expect(summarizeArgs({ count: 3 }, '')).toBe('');
  });
});

describe('mockup events', () => {
  const ctx = { workspaceRoot: '/workspace', readFile: () => null };

  it('maps the streamed mockup phases', async () => {
    const { mapAgentEvent } = await import('./events');
    expect(mapAgentEvent({ type: 'mockup_start', data: { path: '/workspace/design/mockup.html' } }, ctx)).toMatchObject({
      type: 'mockup',
      phase: 'start',
      path: 'design/mockup.html',
    });
    expect(mapAgentEvent({ type: 'mockup_chunk', data: { chunk: '<main>' } }, ctx)).toMatchObject({
      phase: 'chunk',
      chunk: '<main>',
    });
    expect(mapAgentEvent({ type: 'mockup_done', data: { html: '<main></main>', extra: 1 } }, ctx)).toMatchObject({
      phase: 'done',
      html: '<main></main>',
      data: { html: '<main></main>', extra: 1 },
    });
    expect(mapAgentEvent({ type: 'mockup_error', data: { message: 'model failed' } }, ctx)).toMatchObject({
      phase: 'error',
      error: 'model failed',
    });
  });

  it('passes other tool events through untranslated', async () => {
    const { mapAgentEvent } = await import('./events');
    expect(mapAgentEvent({ type: 'render_progress', data: { pct: 50 } }, ctx)).toEqual({
      type: 'raw',
      name: 'render_progress',
      data: { pct: 50 },
    });
  });
});
