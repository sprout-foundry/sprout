/**
 * Tests for content-hashed WASM asset resolution in wasmShell.ts.
 *
 * The dist build emits sprout.<hash>.wasm / wasm_exec.<hash>.js plus a
 * wasm-manifest.json; the loader must read that manifest and use the hashed
 * URLs, falling back to the fixed sprout.wasm / wasm_exec.js names when the
 * manifest is absent (older bundle, local dev server).
 *
 * The resolution helpers are pure and exported, so most of this file runs
 * without a DOM. One integration test drives initWasmShell() with a manifest
 * served through the mocked fetch to prove the loader wires it in.
 */

// jsdom does not include indexedDB — define a minimal mock (see wasmShell.test.ts).
if (typeof indexedDB === 'undefined') {
  (globalThis as unknown as Record<string, unknown>).indexedDB = {};
}

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initWasmShell, resetWasmShell, resolveWasmUrls, loadWasmManifest } from './wasmShell';

// ── Pure resolution ─────────────────────────────────────────────────────────

describe('resolveWasmUrls', () => {
  it('uses the fixed names when there is no manifest', () => {
    expect(resolveWasmUrls('/wasm', null)).toEqual({
      wasmUrl: '/wasm/sprout.wasm',
      wasmExecUrl: '/wasm/wasm_exec.js',
    });
    expect(resolveWasmUrls('/wasm', undefined)).toEqual({
      wasmUrl: '/wasm/sprout.wasm',
      wasmExecUrl: '/wasm/wasm_exec.js',
    });
  });

  it('uses the hashed names from the manifest top-level fields', () => {
    expect(resolveWasmUrls('/wasm', { wasm: 'sprout.abcd123456.wasm', wasmExec: 'wasm_exec.9876fedcba.js' })).toEqual({
      wasmUrl: '/wasm/sprout.abcd123456.wasm',
      wasmExecUrl: '/wasm/wasm_exec.9876fedcba.js',
    });
  });

  it('reads hashed names from the files map when top-level fields are absent', () => {
    expect(
      resolveWasmUrls('/webui/wasm', {
        version: 1,
        files: { 'sprout.wasm': 'sprout.0011223344.wasm', 'wasm_exec.js': 'wasm_exec.ffeeddccbb.js' },
      }),
    ).toEqual({
      wasmUrl: '/webui/wasm/sprout.0011223344.wasm',
      wasmExecUrl: '/webui/wasm/wasm_exec.ffeeddccbb.js',
    });
  });

  it('keeps the fixed name for an asset the manifest omits', () => {
    expect(resolveWasmUrls('/wasm', { wasm: 'sprout.abcd123456.wasm' })).toEqual({
      wasmUrl: '/wasm/sprout.abcd123456.wasm',
      wasmExecUrl: '/wasm/wasm_exec.js',
    });
  });
});

// ── Manifest fetching ───────────────────────────────────────────────────────

describe('loadWasmManifest', () => {
  it('parses a manifest served at <base>/wasm-manifest.json', async () => {
    const fetchImpl = vi.fn(async (url: string) => {
      expect(url).toBe('/wasm/wasm-manifest.json');
      return {
        ok: true,
        status: 200,
        text: async () => '{"wasm":"sprout.aaaaaaaaaa.wasm","wasmExec":"wasm_exec.bbbbbbbbbb.js"}',
      } as Response;
    }) as unknown as typeof fetch;

    await expect(loadWasmManifest('/wasm', fetchImpl)).resolves.toEqual({
      wasm: 'sprout.aaaaaaaaaa.wasm',
      wasmExec: 'wasm_exec.bbbbbbbbbb.js',
    });
  });

  it('returns null when the manifest is missing (404)', async () => {
    const fetchImpl = vi.fn(async () => ({ ok: false, status: 404 }) as Response) as unknown as typeof fetch;
    await expect(loadWasmManifest('/wasm', fetchImpl)).resolves.toBeNull();
  });

  it('returns null when the fetch throws', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error('network down');
    }) as unknown as typeof fetch;
    await expect(loadWasmManifest('/wasm', fetchImpl)).resolves.toBeNull();
  });

  it('returns null when the manifest is malformed JSON', async () => {
    const fetchImpl = vi.fn(
      async () =>
        ({
          ok: true,
          status: 200,
          text: async () => 'not json {',
        }) as Response,
    ) as unknown as typeof fetch;
    await expect(loadWasmManifest('/wasm', fetchImpl)).resolves.toBeNull();
  });
});

// ── Loader integration ──────────────────────────────────────────────────────

const _origCreateElement = document.createElement.bind(document);

let mockScript: HTMLScriptElement;
let capturedFetchUrls: string[];

function createSyncResolvingRequest(result: unknown): Record<string, unknown> {
  const req: Record<string, unknown> = { result, error: null, onupgradeneeded: null, onsuccess: null, onerror: null };
  Object.defineProperty(req, 'onsuccess', {
    set(fn) {
      queueMicrotask(() => {
        if (typeof fn === 'function') fn({ target: req } as unknown as Event);
      });
    },
    get() {
      return null;
    },
    configurable: true,
  });
  return req;
}

function createMockIDBDatabase() {
  return {
    objectStoreNames: { contains: () => true },
    createObjectStore: () => {},
    close: vi.fn(),
    transaction: () => {
      const store = { getAll: () => createSyncResolvingRequest([]) };
      return { objectStore: () => store, oncomplete: null, onerror: null };
    },
  };
}

/** Fetch mock that serves a manifest body and a WASM body; records URLs. */
function createManifestFetch(manifestBody: string | null, wasmBody = new ArrayBuffer(0)) {
  return async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    capturedFetchUrls.push(url);
    if (url.endsWith('wasm-manifest.json')) {
      if (manifestBody === null) return { ok: false, status: 404 } as Response;
      return { ok: true, status: 200, text: async () => manifestBody } as Response;
    }
    return {
      ok: true,
      status: 200,
      // wasmShell init validates the content-type of the WASM response.
      headers: { get: (name: string) => (name.toLowerCase() === 'content-type' ? 'application/wasm' : null) },
      arrayBuffer: async () => wasmBody,
      text: async () => '',
    } as Response;
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  resetWasmShell();
  capturedFetchUrls = [];
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(console, 'error').mockImplementation(() => {});

  (globalThis as unknown as Record<string, unknown>).indexedDB = {
    open: () => createSyncResolvingRequest(createMockIDBDatabase()),
  };

  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'script') {
      mockScript = _origCreateElement('script') as unknown as HTMLScriptElement;
      return mockScript;
    }
    return _origCreateElement(tag);
  });

  vi.spyOn(document.head, 'appendChild').mockImplementation((node: Node) => {
    if (node === mockScript) queueMicrotask(() => mockScript.dispatchEvent(new Event('load')));
    return node;
  });

  (window as unknown as Record<string, unknown>).Go = function Go() {
    return {
      run: () => {
        (window as unknown as Record<string, unknown>).SproutWasm = {
          init: () => '',
          executeCommand: () => '{"stdout":"","stderr":"","exitCode":0}',
          autoComplete: () => '{"completions":[]}',
          getCwd: () => '/home/user',
          changeDir: () => '{"cwd":"/home/user"}',
          writeFile: () => '',
          readFile: () => '{"content":""}',
          listDir: () => '{"entries":[]}',
          deleteFile: () => '',
          getHistory: () => '[]',
          getEnv: () => '{}',
        };
      },
      importObject: {},
    };
  };

  vi.spyOn(WebAssembly, 'instantiate').mockImplementation(async () => ({
    instance: {} as WebAssembly.Instance,
    module: {} as WebAssembly.Module,
  }));
});

afterEach(() => {
  resetWasmShell();
  vi.restoreAllMocks();
  delete (window as unknown as Record<string, unknown>).SproutWasm;
  delete (window as unknown as Record<string, unknown>).__sproutStore;
  delete (window as unknown as Record<string, unknown>).Go;
});

describe('initWasmShell — content-hashed WASM assets', () => {
  it('uses the hashed URLs from the manifest when it is present', async () => {
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(
      '{"version":1,"files":{"sprout.wasm":"sprout.abc1234567.wasm","wasm_exec.js":"wasm_exec.def7654321.js"}}',
    );

    await initWasmShell();

    expect(mockScript.getAttribute('src')).toBe('/wasm/wasm_exec.def7654321.js');
    expect(capturedFetchUrls).toContain('/wasm/sprout.abc1234567.wasm');
    expect(capturedFetchUrls).toContain('/wasm/wasm-manifest.json');
  });

  it('falls back to the fixed names when the manifest is absent', async () => {
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(null);

    await initWasmShell();

    expect(mockScript.getAttribute('src')).toBe('/wasm/wasm_exec.js');
    expect(capturedFetchUrls).toContain('/wasm/sprout.wasm');
  });

  it('honours an explicit wasmUrl override and still reads the manifest for wasm_exec', async () => {
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(
      '{"wasm":"sprout.abc1234567.wasm","wasmExec":"wasm_exec.def7654321.js"}',
    );

    await initWasmShell({ wasmUrl: '/custom/sprout.wasm' });

    expect(capturedFetchUrls).toContain('/custom/sprout.wasm');
    expect(mockScript.getAttribute('src')).toBe('/wasm/wasm_exec.def7654321.js');
  });
});
