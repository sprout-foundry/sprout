/**
 * The WASM asset seam (SP-160 §160e).
 *
 * The package emits its WASM assets content-hashed under `dist/wasm/` and
 * declares `./wasm/` as a subpath export. A host serves that directory at a URL
 * it owns, so it tells the loader the base: through `WasmAssetsProvider` /
 * `SproutProviders`'s `wasmBase` prop, or directly via `setActiveWasmBase`.
 * These tests prove:
 *   - the context/accessor normalize and resolve a host base,
 *   - the loader resolves the manifest and the assets against the host base
 *     (so the hashed names the package emitted are the URLs it fetches),
 *   - with no host base the location probe is unchanged (both shipped builds).
 */

// jsdom does not include indexedDB — define a minimal mock (see wasmShell.test.ts).
if (typeof indexedDB === 'undefined') {
  (globalThis as unknown as Record<string, unknown>).indexedDB = {};
}

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  WasmAssetsProvider,
  setActiveWasmBase,
  getActiveWasmBase,
  normalizeWasmBase,
  resolveActiveWasmBase,
  useWasmAssets,
} from './WasmAssetsContext';
import { initWasmShell, resetWasmShell } from '../services/wasmShell';

// ── base normalization / resolution ─────────────────────────────────────────

describe('WASM asset base normalization', () => {
  it('trims a trailing slash and treats an empty/blank base as default', () => {
    expect(normalizeWasmBase('/assets/wasm/')).toBe('/assets/wasm');
    expect(normalizeWasmBase('/assets/wasm///')).toBe('/assets/wasm');
    expect(normalizeWasmBase('')).toBeNull();
    expect(normalizeWasmBase('   ')).toBeNull();
    expect(normalizeWasmBase(null)).toBeNull();
    expect(normalizeWasmBase(undefined)).toBeNull();
  });

  it('an explicit base wins over the active host base; the host base is the fallback', () => {
    setActiveWasmBase('/host/wasm');
    try {
      expect(resolveActiveWasmBase('/explicit/wasm')).toBe('/explicit/wasm');
      expect(resolveActiveWasmBase()).toBe('/host/wasm');
      expect(resolveActiveWasmBase(null)).toBe('/host/wasm');
    } finally {
      setActiveWasmBase(null);
    }
  });

  it('reports no active base until one is set', () => {
    setActiveWasmBase(null);
    expect(getActiveWasmBase()).toBeNull();
    expect(resolveActiveWasmBase()).toBeNull();
  });
});

// ── the provider + the module accessor the loader reads ─────────────────────

function BaseProbe(): JSX.Element {
  const { wasmBase } = useWasmAssets();
  return <span data-testid="probe">{String(wasmBase)}</span>;
}

describe('WasmAssetsProvider', () => {
  beforeEach(() => {
    setActiveWasmBase(null);
  });
  afterEach(() => {
    setActiveWasmBase(null);
  });

  it('publishes the host base to the context and the loader accessor', () => {
    render(
      <WasmAssetsProvider wasmBase="/host/sprout-wasm/">
        <BaseProbe />
      </WasmAssetsProvider>,
    );
    expect(screen.getByTestId('probe').textContent).toBe('/host/sprout-wasm');
    expect(getActiveWasmBase()).toBe('/host/sprout-wasm');
  });

  it('defaults to null (the loader probe) when no base is supplied', () => {
    render(
      <WasmAssetsProvider>
        <BaseProbe />
      </WasmAssetsProvider>,
    );
    expect(screen.getByTestId('probe').textContent).toBe('null');
    expect(getActiveWasmBase()).toBeNull();
  });
});

// ── the loader resolves URLs against the host base ──────────────────────────

let mockScript: HTMLScriptElement;
let capturedFetchUrls: string[];

const _origCreateElement = document.createElement.bind(document);

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

/** Fetch mock serving a manifest body and a WASM body, recording every URL. */
function createManifestFetch(manifestBody: string | null) {
  // The production loader reads the Content-Type of the .wasm response to guard
  // against a misrouted (non-wasm) asset, so the mock Response must expose a
  // headers.get() that answers the lookup (an empty type exercises the buffered
  // compile path — the one the WebAssembly.instantiate mock drives).
  const emptyHeaders = { get: () => '' };
  return async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    capturedFetchUrls.push(url);
    if (url.endsWith('wasm-manifest.json')) {
      if (manifestBody === null) return { ok: false, status: 404, headers: emptyHeaders } as Response;
      return { ok: true, status: 200, headers: emptyHeaders, text: async () => manifestBody } as Response;
    }
    return {
      ok: true,
      status: 200,
      headers: emptyHeaders,
      arrayBuffer: async () => new ArrayBuffer(0),
      text: async () => '',
    } as Response;
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  resetWasmShell();
  setActiveWasmBase(null);
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
  setActiveWasmBase(null);
  vi.restoreAllMocks();
  delete (window as unknown as Record<string, unknown>).SproutWasm;
  delete (window as unknown as Record<string, unknown>).__sproutStore;
  delete (window as unknown as Record<string, unknown>).Go;
});

describe('initWasmShell — the host WASM base', () => {
  it('resolves the manifest and hashed assets against the active host base', async () => {
    setActiveWasmBase('/host/sprout-wasm');
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(
      '{"version":1,"files":{"sprout.wasm":"sprout.abc1234567.wasm","wasm_exec.js":"wasm_exec.def7654321.js"}}',
    );

    await initWasmShell();

    expect(capturedFetchUrls).toContain('/host/sprout-wasm/wasm-manifest.json');
    expect(capturedFetchUrls).toContain('/host/sprout-wasm/sprout.abc1234567.wasm');
    expect(mockScript.getAttribute('src')).toBe('/host/sprout-wasm/wasm_exec.def7654321.js');
  });

  it('honours the wasmBase config override when a host passes it directly', async () => {
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(
      '{"wasm":"sprout.0011223344.wasm","wasmExec":"wasm_exec.ffeeddccbb.js"}',
    );

    await initWasmShell({ wasmBase: '/explicit/wasm/' });

    expect(capturedFetchUrls).toContain('/explicit/wasm/wasm-manifest.json');
    expect(capturedFetchUrls).toContain('/explicit/wasm/sprout.0011223344.wasm');
    expect(mockScript.getAttribute('src')).toBe('/explicit/wasm/wasm_exec.ffeeddccbb.js');
  });

  it('falls back to the fixed names under the host base when the manifest is absent', async () => {
    setActiveWasmBase('/host/sprout-wasm');
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(null);

    await initWasmShell();

    expect(capturedFetchUrls).toContain('/host/sprout-wasm/sprout.wasm');
    expect(mockScript.getAttribute('src')).toBe('/host/sprout-wasm/wasm_exec.js');
  });

  it('keeps the location-probe default when no host base is set', async () => {
    (window as unknown as Record<string, unknown>).fetch = createManifestFetch(null);

    await initWasmShell();

    expect(capturedFetchUrls).toContain('/wasm/sprout.wasm');
    expect(mockScript.getAttribute('src')).toBe('/wasm/wasm_exec.js');
  });
});
