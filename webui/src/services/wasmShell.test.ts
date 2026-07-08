/**
 * Tests for configurable WASM paths in wasmShell.ts
 *
 * Verifies that `initWasmShell()` uses the correct URLs for the WASM binary
 * and wasm_exec.js script based on the config parameter:
 *   - Default paths when no config is provided
 *   - Custom paths when explicitly configured
 *   - Mixed config (only one path overridden)
 *   - Error messages include the actual URL when fetch fails
 */

// jsdom 16.x does not include indexedDB — define a minimal mock.
if (typeof indexedDB === 'undefined') {
  (globalThis as unknown as Record<string, unknown>).indexedDB = {};
}

import { initWasmShell, resetWasmShell } from './wasmShell';

// Save original createElement before mocking
const _origCreateElement = document.createElement.bind(document);

// ── Mock state ──────────────────────────────────────────────────────────────

let mockScript: HTMLScriptElement;
let capturedFetchUrls: string[];

/**
 * Create a mock IDBRequest that fires onsuccess synchronously after the
 * caller assigns the onsuccess handler.
 */
function createSyncResolvingRequest(result: unknown): Record<string, unknown> {
  const req: Record<string, unknown> = {
    result,
    error: null,
    onupgradeneeded: null,
    onsuccess: null,
    onerror: null,
  };

  // Intercept onsuccess assignment: fire it immediately when set
  Object.defineProperty(req, 'onsuccess', {
    set(fn) {
      // Fire on the next microtask so the caller finishes setting up first
      queueMicrotask(() => {
        if (typeof fn === 'function') {
          fn({ target: req } as unknown as Event);
        }
      });
    },
    get() {
      return null; // always return null since we fire and forget
    },
    configurable: true,
  });

  return req;
}

/**
 * Create a mock IDBDatabase with a transaction → objectStore → getAll chain
 * that resolves immediately.
 */
function createMockIDBDatabase() {
  const db: Record<string, unknown> = {
    objectStoreNames: { contains: () => true },
    createObjectStore: () => {},
    close: vi.fn(),
    transaction: () => {
      const store = {
        getAll: () => createSyncResolvingRequest([]),
      };
      const tx: Record<string, unknown> = {
        objectStore: () => store,
        oncomplete: null,
        onerror: null,
      };
      return tx;
    },
  };
  return db;
}

/**
 * Get the raw src attribute value (not the resolved absolute URL).
 * jsdom's HTMLScriptElement.src resolves relative URLs to absolute,
 * but getAttribute('src') returns the original assigned value.
 */
function getScriptSrc(): string {
  return mockScript.getAttribute('src') || '';
}

function createMockFetch(responseOverrides?: Map<string, Partial<Response>>) {
  return async (input: RequestInfo | URL, _init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    capturedFetchUrls.push(url);

    const override = responseOverrides?.get(url);
    if (override) {
      return override as Response;
    }

    return {
      ok: true,
      status: 200,
      arrayBuffer: async () => new ArrayBuffer(0),
    } as Response;
  };
}

// ── Test lifecycle ──────────────────────────────────────────────────────────

beforeEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
  resetWasmShell();

  capturedFetchUrls = [];

  // Suppress expected console output
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(console, 'error').mockImplementation(() => {});

  // ── IndexedDB mock ──────────────────────────────────────────────────────
  const mockDB = createMockIDBDatabase();
  (globalThis as unknown as Record<string, unknown>).indexedDB = {
    open: () => createSyncResolvingRequest(mockDB),
  };

  // ── document.createElement mock ──────────────────────────────────────────
  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'script') {
      // Create a real script element so dispatchEvent works properly
      mockScript = _origCreateElement('script') as unknown as HTMLScriptElement;
      return mockScript;
    }
    return _origCreateElement(tag);
  });

  // ── document.head.appendChild mock ───────────────────────────────────────
  // The module calls appendChild, then sets script.onload/onerror, then awaits.
  // We need to dispatch 'load' *after* onload is assigned.
  // Sequence in module:
  //   document.head.appendChild(script);  // sync
  //   await new Promise((resolve, reject) => {
  //     script.onload = () => resolve();  // sync, assigned immediately after appendChild
  //     script.onerror = () => reject();
  //   });
  // So by the time we hit the next microtask, onload is already set.
  vi.spyOn(document.head, 'appendChild').mockImplementation((node: Node) => {
    if (node === mockScript) {
      queueMicrotask(() => {
        mockScript.dispatchEvent(new Event('load'));
      });
    }
    return node;
  });

  // ── window.Go mock ───────────────────────────────────────────────────────
  (window as unknown as Record<string, unknown>).Go = function Go() {
    return {
      run: () => {
        (window as unknown as Record<string, unknown>).SproutWasm = {
          init: (_cfg?: string) => '',
          executeCommand: (_input: string) => '{"stdout":"","stderr":"","exitCode":0}',
          autoComplete: (_input: string) => '{"completions":[]}',
          getCwd: () => '/home/user',
          changeDir: (_dir: string) => '{"cwd":"/home/user"}',
          writeFile: (_path: string, _content: string) => '',
          readFile: (_path: string) => '{"content":""}',
          listDir: (_path: string) => '{"entries":[]}',
          deleteFile: (_path: string) => '',
          getHistory: () => '[]',
          getEnv: () => '{}',
        };
      },
      importObject: {},
    };
  };

  // ── fetch mock ───────────────────────────────────────────────────────────
  (window as unknown as Record<string, unknown>).fetch = createMockFetch();

  // ── WebAssembly.instantiate mock ─────────────────────────────────────────
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

// ── Tests ───────────────────────────────────────────────────────────────────

describe('initWasmShell — configurable paths', () => {
  describe('default paths', () => {
    it('uses /webui/wasm/wasm_exec.js as script src when no config is provided', async () => {
      await initWasmShell();

      expect(getScriptSrc()).toBe('/webui/wasm/wasm_exec.js');
    });

    it('fetches /webui/wasm/sprout.wasm when no config is provided', async () => {
      await initWasmShell();

      expect(capturedFetchUrls).toContain('/webui/wasm/sprout.wasm');
    });

    it('uses default paths when an empty config object is passed', async () => {
      await initWasmShell({});

      expect(getScriptSrc()).toBe('/webui/wasm/wasm_exec.js');
      expect(capturedFetchUrls).toContain('/webui/wasm/sprout.wasm');
    });
  });

  describe('custom paths', () => {
    it('uses custom wasmExecUrl for script src', async () => {
      await initWasmShell({ wasmExecUrl: '/custom/path/wasm_exec.js' });

      expect(getScriptSrc()).toBe('/custom/path/wasm_exec.js');
    });

    it('uses custom wasmUrl for fetch', async () => {
      await initWasmShell({ wasmUrl: '/custom/path/sprout.wasm' });

      expect(capturedFetchUrls).toContain('/custom/path/sprout.wasm');
    });

    it('uses both custom paths when both are provided', async () => {
      await initWasmShell({
        wasmUrl: 'https://cdn.example.com/sprout.wasm',
        wasmExecUrl: 'https://cdn.example.com/wasm_exec.js',
      });

      expect(getScriptSrc()).toBe('https://cdn.example.com/wasm_exec.js');
      expect(capturedFetchUrls).toContain('https://cdn.example.com/sprout.wasm');
    });

    it('does not use default paths when custom paths are provided', async () => {
      await initWasmShell({
        wasmUrl: '/custom/sprout.wasm',
        wasmExecUrl: '/custom/wasm_exec.js',
      });

      expect(capturedFetchUrls).not.toContain('/webui/wasm/sprout.wasm');
      expect(getScriptSrc()).not.toBe('/webui/wasm/wasm_exec.js');
    });
  });

  describe('mixed config (only one path overridden)', () => {
    it('uses default wasmExecUrl when only wasmUrl is provided', async () => {
      await initWasmShell({ wasmUrl: '/custom/sprout.wasm' });

      expect(getScriptSrc()).toBe('/webui/wasm/wasm_exec.js'); // default
      expect(capturedFetchUrls).toContain('/custom/sprout.wasm'); // overridden
    });

    it('uses default wasmUrl when only wasmExecUrl is provided', async () => {
      await initWasmShell({ wasmExecUrl: '/custom/wasm_exec.js' });

      expect(getScriptSrc()).toBe('/custom/wasm_exec.js'); // overridden
      expect(capturedFetchUrls).toContain('/webui/wasm/sprout.wasm'); // default
    });
  });

  describe('error message includes actual URL', () => {
    it('includes the custom wasmUrl in the error when fetch fails', async () => {
      const customUrl = '/broken/path/sprout.wasm';

      (window as unknown as Record<string, unknown>).fetch = createMockFetch(
        new Map([
          [
            customUrl,
            {
              ok: false,
              status: 404,
              arrayBuffer: async () => new ArrayBuffer(0),
            } as unknown as Response,
          ],
        ]),
      );

      await expect(initWasmShell({ wasmUrl: customUrl })).rejects.toThrow(customUrl);
    });

    it('includes the default wasmUrl in the error when fetch fails with no config', async () => {
      (window as unknown as Record<string, unknown>).fetch = createMockFetch(
        new Map([
          [
            '/webui/wasm/sprout.wasm',
            {
              ok: false,
              status: 500,
              arrayBuffer: async () => new ArrayBuffer(0),
            } as unknown as Response,
          ],
        ]),
      );

      await expect(initWasmShell()).rejects.toThrow('/webui/wasm/sprout.wasm');
    });

    it('includes the HTTP status code in the error message', async () => {
      (window as unknown as Record<string, unknown>).fetch = createMockFetch(
        new Map([
          [
            '/webui/wasm/sprout.wasm',
            {
              ok: false,
              status: 403,
              arrayBuffer: async () => new ArrayBuffer(0),
            } as unknown as Response,
          ],
        ]),
      );

      await expect(initWasmShell()).rejects.toThrow('403');
    });

    it('error includes the custom URL, not the default, when custom URL fails', async () => {
      const customUrl = 'https://example.com/broken.wasm';

      (window as unknown as Record<string, unknown>).fetch = createMockFetch(
        new Map([
          [
            customUrl,
            {
              ok: false,
              status: 404,
              arrayBuffer: async () => new ArrayBuffer(0),
            } as unknown as Response,
          ],
        ]),
      );

      try {
        await initWasmShell({ wasmUrl: customUrl });
        fail('Expected an error to be thrown');
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        expect(message).toContain(customUrl);
        expect(message).not.toContain('/webui/wasm/sprout.wasm');
      }
    });
  });

  describe('wasmExecUrl script loading error', () => {
    it('rejects when the script fails to load', async () => {
      // Override appendChild to dispatch an error event instead of load
      (document.head.appendChild as vi.Mock).mockImplementation((node: Node) => {
        if (node === mockScript) {
          queueMicrotask(() => {
            mockScript.dispatchEvent(new Event('error'));
          });
        }
        return node;
      });

      await expect(initWasmShell()).rejects.toThrow('Failed to load wasm_exec.js from /webui/wasm/wasm_exec.js');
    });

    it('includes custom wasmExecUrl in the error when script load fails', async () => {
      const customUrl = '/custom/broken/wasm_exec.js';

      (document.head.appendChild as vi.Mock).mockImplementation((node: Node) => {
        if (node === mockScript) {
          queueMicrotask(() => {
            mockScript.dispatchEvent(new Event('error'));
          });
        }
        return node;
      });

      await expect(initWasmShell({ wasmExecUrl: customUrl })).rejects.toThrow(
        `Failed to load wasm_exec.js from ${customUrl}`,
      );
    });
  });

  describe('singleton behavior', () => {
    it('returns the same instance on second call without re-fetching', async () => {
      const shell1 = await initWasmShell();

      capturedFetchUrls = [];

      const shell2 = await initWasmShell();

      expect(capturedFetchUrls).toHaveLength(0);
      expect(shell2).toBe(shell1);
    });

    it('creates a new instance after resetWasmShell()', async () => {
      const shell1 = await initWasmShell();

      resetWasmShell();

      const shell2 = await initWasmShell();

      expect(shell2).not.toBe(shell1);
    });
  });
});
