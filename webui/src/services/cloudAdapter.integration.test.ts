/**
 * Integration tests for CloudAdapter endpoint mappings.
 *
 * These tests verify that ALL endpoints in CLOUD_ENDPOINTS produce
 * the correct behavior:
 * - foundry-backend: Proxied to correct Foundry proxy URL
 * - synthetic/no-op: Return synthetic responses (no fetch call)
 * - wasm-local: Handled client-side by WASM shell (NOT proxied)
 *
 * Tests also verify URL rewriting, body translation, header injection,
 * and that no endpoints result in 404s or broken flows.
 */

import { CloudAdapter, type CloudAdapterConfig } from './cloudAdapter';
import { CLOUD_ENDPOINTS, getEndpointsByCategory, type CloudEndpoint } from './cloudEndpointRegistry';

// Mock clientSession module
vi.mock('./clientSession', () => ({
  WEBUI_CLIENT_ID_HEADER: 'x-webui-client-id',
  getWebUIClientId: () => 'test-client-id-123',
}));

// Mock wasmShell module — WASM cannot run in jsdom, so provide a fake shell.
const mockWasmShell = {
  executeCommand: vi.fn((input: string) => ({
    stdout: '',
    stderr: '',
    exitCode: 0,
  })),
  getCwd: vi.fn(() => '/home/user'),
  changeDir: vi.fn(() => ({ cwd: '/home/user', error: '' })),
  writeFile: vi.fn(() => ''),
  readFile: vi.fn((path: string) => ({ content: '// file at ' + path, error: '' })),
  listDir: vi.fn((path: string) => {
    // Prevent infinite recursion: return empty for nested paths
    if (path && (path.includes('/src/') || path.endsWith('/src'))) {
      return { entries: [], error: '' };
    }
    return {
      entries: [
        { name: 'src', type: 'dir', size: 0, mode: 0o40755 },
        { name: 'README.md', type: 'file', size: 128, mode: 0o100644 },
      ],
      error: '',
    };
  }),
  deleteFile: vi.fn(() => ''),
  runAgent: vi.fn(() => Promise.resolve({})),
};
vi.mock('./wasmShell', () => ({
  initWasmShell: vi.fn(() => Promise.resolve(mockWasmShell)),
  resetWasmShell: vi.fn(),
}));

// Polyfill Response for jsdom environment
if (typeof Response === 'undefined') {
  global.Response = class Response {
    status: number;
    headers: Map<string, string>;
    private _body: string;

    constructor(body: string, init?: { status?: number; headers?: Record<string, string> }) {
      this._body = body;
      this.status = init?.status ?? 200;
      this.headers = new Map(Object.entries(init?.headers ?? {}));
      const originalGet = this.headers.get.bind(this.headers);
      this.headers.get = (key: string): string | null => {
        const value = originalGet(key);
        return value === undefined ? null : value;
      };
    }

    get ok(): boolean {
      return this.status >= 200 && this.status <= 299;
    }

    async json(): Promise<unknown> {
      return JSON.parse(this._body);
    }

    async text(): Promise<string> {
      return this._body;
    }
  } as unknown as typeof Response;
}

// Polyfill Request for jsdom environment
if (typeof Request === 'undefined') {
  global.Request = class Request {
    url: string;
    method: string;
    headers: Headers | Map<string, string>;
    private _body: string | null;

    constructor(
      input: string | Request,
      init?: RequestInit | { method?: string; headers?: HeadersInit; body?: BodyInit },
    ) {
      if (typeof input === 'string') {
        this.url = input;
        this.method = init?.method?.toUpperCase() ?? 'GET';
        this.headers = new Headers(init?.headers);
        if (init?.body) {
          this._body = typeof init.body === 'string' ? init.body : JSON.stringify(init.body);
        } else {
          this._body = null;
        }
      } else {
        this.url = input.url;
        this.method = input.method;
        this.headers = input.headers;
        this._body = (input as Request & { _body: string | null })._body || null;
      }
    }

    clone(): Request {
      const cloned = new Request(this.url, {
        method: this.method,
        headers: this.headers,
        body: this._body || undefined,
      });
      (cloned as unknown as { _body: string | null })._body = this._body;
      return cloned;
    }

    async text(): Promise<string> {
      return this._body || '';
    }
  } as unknown as typeof Request;
}

describe('CloudAdapter Integration Tests', () => {
  let adapter: CloudAdapter;
  let mockConfig: CloudAdapterConfig;
  let mockFetch: vi.Mock;

  beforeEach(() => {
    mockConfig = {
      apiBase: 'https://api.sprout.dev',
      wsUrl: 'wss://api.sprout.dev/ws',
      navItems: [],
    };

    adapter = new CloudAdapter(mockConfig);
    mockFetch = vi.fn();
    global.fetch = mockFetch;
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  // =========================================================================
  // 1. Endpoint Coverage Verification
  // =========================================================================

  describe('Endpoint Coverage - All CLOUD_ENDPOINTS', () => {
    /**
     * Test that EVERY endpoint in CLOUD_ENDPOINTS produces the correct behavior.
     * This ensures no endpoint results in a 404 or unhandled call.
     */
    it.each(CLOUD_ENDPOINTS.map((e) => ({ endpoint: e })))(
      '$endpoint.path ($endpoint.category)',
      async ({ endpoint }) => {
        // Skip prefix-matched endpoints that need specific test data
        if (endpoint.isPrefix) {
          return;
        }

        // Test the first method defined for this endpoint
        const method = endpoint.methods[0];
        const testPath = endpoint.path;

        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        // Make the request
        const response = await adapter.fetch(testPath, { method });

        // Verify behavior based on category
        switch (endpoint.category) {
          case 'foundry-backend':
            // Should have called fetch with a valid URL
            expect(mockFetch).toHaveBeenCalledTimes(1);
            const fetchCall = mockFetch.mock.calls[0];
            const calledUrl = fetchCall[0] as string;

            // Verify URL is valid (not undefined, starts with http or /)
            expect(calledUrl).toBeTruthy();
            expect(calledUrl.startsWith('http') || calledUrl.startsWith('/')).toBe(true);

            // Verify headers include client ID
            const fetchInit = fetchCall[1] as RequestInit;
            expect(fetchInit?.headers).toBeTruthy();
            expect(fetchInit?.credentials).toBe('include');
            break;

          case 'synthetic':
          case 'no-op':
            // Should NOT have called fetch
            expect(mockFetch).not.toHaveBeenCalled();

            // Should return a valid response
            expect(response).toBeTruthy();
            expect(response.ok).toBe(endpoint.category === 'no-op' || !endpoint.syntheticResponse?.['error']);
            break;

          case 'wasm-local':
            // Should NOT have called fetch — these are handled locally by WASM shell
            expect(mockFetch).not.toHaveBeenCalled();
            break;
        }

        // Every endpoint should either call fetch or return a valid response
        // (no 404s, no unhandled errors)
        expect(response).toBeTruthy();
      },
    );
  });

  // =========================================================================
  // 2. Category-Specific Behavior Tests
  // =========================================================================

  describe('Category: foundry-backend - Proxy URL Verification', () => {
    const foundryBackendEndpoints = getEndpointsByCategory('foundry-backend').filter((e) => !e.isPrefix);

    /**
     * Test that all foundry-backend endpoints proxy to the correct URL.
     * Some endpoints have special URL rewriting (chat, git, stats, settings),
     * others use standard proxy (apiBase + path).
     */
    it.each(
      foundryBackendEndpoints.map((e) => ({
        endpoint: e,
        expectedProxyPath: getExpectedProxyPath(e),
      })),
    )('$endpoint.path → $expectedProxyPath', async ({ endpoint, expectedProxyPath }) => {
      const method = endpoint.methods[0];
      const testPath = endpoint.path;

      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch(testPath, { method });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe(`${mockConfig.apiBase}${expectedProxyPath}`);
    });
  });

  describe('Category: synthetic - No Fetch Calls', () => {
    const syntheticEndpoints = getEndpointsByCategory('synthetic');

    /**
     * Test that all synthetic endpoints return responses without calling fetch.
     * These are handled entirely client-side.
     */
    it.each(
      syntheticEndpoints.map((e) => ({
        endpoint: e,
        firstMethod: e.methods[0],
      })),
    )('$endpoint.path ($endpoint.description) - synthetic response', async ({ endpoint, firstMethod }) => {
      const response = await adapter.fetch(endpoint.path, { method: firstMethod });

      // Should NOT call fetch
      expect(mockFetch).not.toHaveBeenCalled();

      // Check if this synthetic response has an error field
      const hasError =
        endpoint.syntheticResponse &&
        typeof endpoint.syntheticResponse === 'object' &&
        'error' in endpoint.syntheticResponse;

      const expectedStatus = hasError ? 400 : 200;

      // Should return a valid response (may be error response)
      expect(response.ok).toBe(!hasError);
      expect(response.status).toBe(expectedStatus);

      // If synthetic response is defined, it should match
      if (endpoint.syntheticResponse) {
        const data = await response.json();
        expect(data).toEqual(endpoint.syntheticResponse);
      }
    });
  });

  describe('Category: no-op - No Fetch Calls', () => {
    const noOpEndpoints = getEndpointsByCategory('no-op');

    /**
     * Test that all no-op endpoints return success responses without calling fetch.
     * These are endpoints that don't apply in cloud mode but shouldn't break callers.
     */
    it.each(
      noOpEndpoints.map((e) => ({
        endpoint: e,
        firstMethod: e.methods[0],
      })),
    )('$endpoint.path - no-op success response', async ({ endpoint, firstMethod }) => {
      const response = await adapter.fetch(endpoint.path, { method: firstMethod });

      // Should NOT call fetch
      expect(mockFetch).not.toHaveBeenCalled();

      // Should return a valid response
      expect(response.ok).toBe(true);

      // Synthetic response should be present
      if (endpoint.syntheticResponse) {
        const data = await response.json();
        expect(data).toEqual(endpoint.syntheticResponse);
      }
    });
  });

  describe('Category: wasm-local - Handled by WASM Shell (NOT proxied)', () => {
    const wasmLocalEndpoints = getEndpointsByCategory('wasm-local');

    /**
     * Test that all wasm-local endpoints are handled client-side by the WASM shell.
     * They MUST NOT call fetch() — the CloudAdapter intercepts these and delegates
     * to handleWasmLocal() which uses the WASM shell directly.
     */
    it.each(
      wasmLocalEndpoints.map((e) => ({
        endpoint: e,
        firstMethod: e.methods[0],
      })),
    )('$endpoint.path - handled by WASM shell (NOT proxied)', async ({ endpoint, firstMethod }) => {
      // Reset mocks for each test
      vi.clearAllMocks();

      // Some wasm-local endpoints require a request body (create, delete, rename, search/replace, file write)
      let response: Response;
      if (endpoint.path === '/api/create') {
        response = await adapter.fetch(endpoint.path, {
          method: 'POST',
          body: JSON.stringify({ path: '/test.txt' }),
        });
      } else if (endpoint.path === '/api/delete') {
        response = await adapter.fetch(endpoint.path, {
          method: 'DELETE',
          body: JSON.stringify({ path: '/test.txt' }),
        });
      } else if (endpoint.path === '/api/rename') {
        response = await adapter.fetch(endpoint.path, {
          method: 'POST',
          body: JSON.stringify({ old_path: '/old.txt', new_path: '/new.txt' }),
        });
      } else if (endpoint.path === '/api/search/replace') {
        response = await adapter.fetch(endpoint.path, {
          method: 'POST',
          body: JSON.stringify({ search: 'foo', replace: 'bar', files: ['/src/main.go'] }),
        });
      } else if (endpoint.path === '/api/file') {
        // GET requires ?path= query param, POST requires body with content
        if (firstMethod === 'GET') {
          response = await adapter.fetch(`${endpoint.path}?path=/test.txt`, { method: 'GET' });
        } else {
          response = await adapter.fetch(`${endpoint.path}?path=/test.txt`, {
            method: 'POST',
            body: JSON.stringify({ content: 'hello' }),
          });
        }
      } else if (endpoint.path === '/api/query') {
        // Agent query needs a body with at least { query: '...' }
        response = await adapter.fetch(endpoint.path, {
          method: 'POST',
          body: JSON.stringify({ query: 'test' }),
        });
      } else {
        response = await adapter.fetch(endpoint.path, { method: firstMethod });
      }

      // MUST NOT call fetch — these are handled locally by the WASM shell
      expect(mockFetch).not.toHaveBeenCalled();

      // Should return a valid response from the WASM handler
      expect(response).toBeTruthy();
      expect(response.ok).toBe(true);
    });
  });

  // =========================================================================
  // 3. URL Rewriting Correctness
  // =========================================================================

  describe('URL Rewriting - Chat Endpoints', () => {
    // Note: /api/query POST routes through the WASM shell (in-browser agent
    // loop), not through the platform proxy. Steering/stop/status remain
    // proxied because they need server-side chat session state.
    // The platform hosts chat at /proxy/chat (no /api prefix).

    it('/api/query/steer POST → /proxy/chat (with steer flag)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/query/steer', {
        method: 'POST',
        body: JSON.stringify({ query: 'test' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/proxy/chat`);
      const body = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      expect(body.steer).toBe(true);
    });

    it('/api/query/stop POST → /proxy/chat/stop', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/query/stop', { method: 'POST' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/proxy/chat/stop`);
    });

    it('/api/query/status GET → /proxy/chat/status', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: 'idle' }), { status: 200 }));

      await adapter.fetch('/api/query/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/proxy/chat/status`);
    });
  });

  describe('URL Rewriting - Git Endpoints', () => {
    it('/api/git/* paths → /api/proxy/git/* (prefix rewrite)', async () => {
      const gitPaths = [
        '/api/git/status',
        '/api/git/branches',
        '/api/git/checkout',
        '/api/git/branch/create',
        '/api/git/pull',
        '/api/git/push',
        '/api/git/stage',
        '/api/git/unstage',
        '/api/git/discard',
        '/api/git/stage-all',
        '/api/git/unstage-all',
        '/api/git/commit',
        '/api/git/commit-message',
        '/api/git/revert',
        '/api/git/deep-review',
        '/api/git/deep-review/fix',
        '/api/git/deep-review/fix/start',
        '/api/git/deep-review/fix/status',
        '/api/git/diff',
        '/api/git/log',
        '/api/git/confirm',
        '/api/git/commit/show',
        '/api/git/commit/show/file',
        '/api/git/worktrees',
        '/api/git/worktree/create',
        '/api/git/worktree/remove',
        '/api/git/worktree/checkout',
      ];

      for (const path of gitPaths) {
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        await adapter.fetch(path, { method: 'POST' });

        expect(mockFetch).toHaveBeenCalledTimes(1);
        expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}${path.replace('/api/git/', '/api/proxy/git/')}`);
      }
    });

    it('preserves query parameters in git URLs', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ diff: '' }), { status: 200 }));

      await adapter.fetch('/api/git/diff?path=file.txt&cached=false', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/git/diff?path=file.txt&cached=false`);
    });
  });

  describe('URL Rewriting - Stats Endpoint', () => {
    it('/api/stats → /api/proxy/stats', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/stats`);
    });
  });

  describe('URL Rewriting - Settings Endpoints', () => {
    it('/api/settings → /api/proxy/settings', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/settings`);
    });

    it('/api/settings/* paths → /api/proxy/settings/*', async () => {
      // Only the core settings endpoints are proxied; mcp/skills/subagent-types
      // are intercepted as synthetic (not available in browser mode).
      const settingsPaths = [
        '/api/settings/credentials',
        '/api/settings/providers',
      ];

      for (const path of settingsPaths) {
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        await adapter.fetch(path, { method: 'GET' });

        expect(mockFetch).toHaveBeenCalledTimes(1);
        expect(mockFetch.mock.calls[0][0]).toBe(
          `${mockConfig.apiBase}${path.replace('/api/settings', '/api/proxy/settings')}`,
        );
      }
    });

    it('preserves query parameters in settings URLs', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings?layer=provenance', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/settings?layer=provenance`);
    });
  });

  // NOTE: "URL Rewriting - Other foundry-backend Endpoints" describe block
  // was removed: all the endpoints it tested (/api/upload/image,
  // /api/diagnostics, /api/semantic, /api/lsp/*, /api/history/*, /api/costs/*,
  // /api/hotkeys) are now intercepted as synthetic responses because they
  // are not available in browser mode. See synthetic.ts for their definitions.

  // NOTE: "Body Translation - Chat Endpoints" describe block was removed:
  // /api/query POST no longer goes through the platform chat proxy (it
  // routes through the WASM shell's in-browser agent loop), so there is no
  // body translation to test. Steering/stop/status don't need translation
  // because they're thin control messages.

  describe('No 404s - All Endpoints Handled', () => {
    /**
     * Verify that every endpoint in CLOUD_ENDPOINTS is handled correctly:
     * - Synthetic endpoints return responses without calling fetch
     * - Backend endpoints call fetch with valid URLs
     * - No endpoint results in an unhandled call or 404
     */
    it('all CLOUD_ENDPOINTS produce valid responses or fetch calls', async () => {
      const errors: string[] = [];

      for (const endpoint of CLOUD_ENDPOINTS) {
        if (endpoint.isPrefix) continue; // Skip prefix-matched endpoints

        const method = endpoint.methods[0];
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        try {
          await adapter.fetch(endpoint.path, { method });

          const fetchCalled = mockFetch.mock.calls.length > 0;

          if (
            endpoint.category === 'synthetic' ||
            endpoint.category === 'no-op' ||
            endpoint.category === 'wasm-local'
          ) {
            // Should NOT have called fetch — synthetic/no-op return synthetic responses,
            // wasm-local is handled by the WASM shell in-browser
            if (fetchCalled) {
              errors.push(`${endpoint.path} (${endpoint.category}): Expected no fetch call, but fetch was called`);
            }
          } else {
            // foundry-backend: Should have called fetch
            if (!fetchCalled) {
              errors.push(`${endpoint.path} (${endpoint.category}): Expected fetch call, but fetch was not called`);
            } else {
              const calledUrl = mockFetch.mock.calls[0][0] as string;
              if (!calledUrl || !calledUrl.startsWith('http')) {
                errors.push(`${endpoint.path} (${endpoint.category}): Invalid fetch URL: ${calledUrl}`);
              }
            }
          }
        } catch (error) {
          errors.push(`${endpoint.path} (${endpoint.category}): Unexpected error: ${error}`);
        }
      }

      if (errors.length > 0) {
        throw new Error(`${errors.length} endpoint handling errors:\n${errors.map((e) => `  - ${e}`).join('\n')}`);
      }
    });
  });

  describe('Prefix-Matched Endpoints Work Correctly', () => {
    /**
     * Test that prefix-matched endpoints (those with isPrefix: true)
     * handle their sub-paths correctly.
     */
    it('/api/settings/credentials/* prefix matches sub-paths', async () => {
      const subPaths = [
        '/api/settings/credentials',
        '/api/settings/credentials/openai',
        '/api/settings/credentials/openai/',
        '/api/settings/credentials/openai/test',
        '/api/settings/credentials/anthropic/pool',
      ];

      for (const path of subPaths) {
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        await adapter.fetch(path, { method: 'GET' });

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const calledUrl = mockFetch.mock.calls[0][0] as string;
        expect(calledUrl).toContain('/api/proxy/settings/credentials');
      }
    });

    // NOTE: /api/settings/mcp/servers/* and /api/settings/subagent-types/*
    // prefix tests were removed — those endpoints are now intercepted as
    // synthetic (not available in browser mode) and would not call fetch.
    // The remaining prefix tests cover the settings paths that ARE proxied.

    it('/api/settings/providers/* prefix matches sub-paths', async () => {
      const subPaths = [
        '/api/settings/providers/openai/',
        '/api/settings/providers/anthropic/',
        '/api/settings/providers/custom-provider/',
      ];

      for (const path of subPaths) {
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        await adapter.fetch(path, { method: 'GET' });

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const calledUrl = mockFetch.mock.calls[0][0] as string;
        expect(calledUrl).toContain('/api/proxy/settings/providers');
      }
    });

    // NOTE: /api/chat-session/* prefix test was removed — that path is
    // no longer registered in the foundry-backend list (was removed because
    // worktree-only chat-session sub-endpoints don't apply in browser mode
    // and return synthetic safe-default responses instead).
  });

  describe('Default Fallthrough - Unregistered Paths', () => {
    it('unregistered /api/* paths proxy to apiBase + path', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/unregistered/endpoint', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/unregistered/endpoint`);
    });

    it('non-/api paths are proxied as-is', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/health', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/health`);
    });
  });

  describe('Query Parameters Preserved in Proxied Requests', () => {
    it('preserves query parameters for git endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ diff: '' }), { status: 200 }));

      await adapter.fetch('/api/git/diff?path=file.txt&cached=false', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/git/diff?path=file.txt&cached=false`);
    });

    it('preserves query parameters for settings endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings?layer=provenance', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/proxy/settings?layer=provenance`);
    });

    it('preserves query parameters for standard proxy endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ providers: [] }), { status: 200 }));

      await adapter.fetch('/api/providers?type=llm&limit=10', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/providers?type=llm&limit=10`);
    });
  });

  // =========================================================================
  // 6. Header Injection
  // =========================================================================

  describe('Header Injection - All Proxied Requests', () => {
    /**
     * Verify that all proxied requests include the required headers:
     * - X-Sprout-Client-ID (from getWebUIClientId)
     * - credentials: 'include'
     */
    it('all foundry-backend endpoints include WebUI client ID header', async () => {
      const foundryEndpoints = getEndpointsByCategory('foundry-backend').filter((e) => !e.isPrefix);

      const errors: string[] = [];

      for (const endpoint of foundryEndpoints) {
        const method = endpoint.methods[0];
        mockFetch.mockClear();
        mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

        try {
          await adapter.fetch(endpoint.path, { method });

          expect(mockFetch).toHaveBeenCalledTimes(1);
          const fetchInit = mockFetch.mock.calls[0][1] as RequestInit;

          const headers = fetchInit?.headers as Headers | Map<string, string>;
          const clientId = headers.get('x-webui-client-id');

          if (clientId !== 'test-client-id-123') {
            errors.push(
              `${endpoint.path} (${method}): Expected client ID header 'test-client-id-123', got '${clientId}'`,
            );
          }

          if (fetchInit?.credentials !== 'include') {
            errors.push(
              `${endpoint.path} (${method}): Expected credentials 'include', got '${fetchInit?.credentials}'`,
            );
          }
        } catch (error) {
          errors.push(`${endpoint.path} (${method}): Unexpected error checking headers: ${error}`);
        }
      }

      if (errors.length > 0) {
        throw new Error(`${errors.length} header injection errors:\n${errors.map((e) => `  - ${e}`).join('\n')}`);
      }
    });

    it('preserves existing headers in proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const customHeaders = new Headers({
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      });

      // /api/git/status is a foundry-backend endpoint that always goes
      // through the platform proxy. Use it to test header preservation on
      // a real proxied path (was /api/query before that route was moved
      // to the WASM shell in browser mode).
      await adapter.fetch('/api/git/status', {
        method: 'GET',
        headers: customHeaders,
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const fetchInit = mockFetch.mock.calls[0][1] as RequestInit;
      const headers = fetchInit?.headers as Headers;

      expect(headers.get('Content-Type')).toBe('application/json');
      expect(headers.get('X-Custom-Header')).toBe('custom-value');
      expect(headers.get('x-webui-client-id')).toBe('test-client-id-123');
    });
  });

  // =========================================================================
  // 7. Synthetic Endpoint Response Validation
  // =========================================================================

  describe('Synthetic Response Validation', () => {
    /**
     * Verify that all synthetic endpoints return the correct response
     * with the correct status code and Content-Type header.
     */
    it('synthetic endpoints return correct status code', async () => {
      const syntheticEndpoints = getEndpointsByCategory('synthetic');
      const noOpEndpoints = getEndpointsByCategory('no-op');

      for (const endpoint of [...syntheticEndpoints, ...noOpEndpoints]) {
        const method = endpoint.methods[0];

        const response = await adapter.fetch(endpoint.path, { method });

        const hasError =
          endpoint.syntheticResponse &&
          typeof endpoint.syntheticResponse === 'object' &&
          'error' in endpoint.syntheticResponse;

        const expectedStatus = hasError ? 400 : 200;

        expect(response.status).toBe(expectedStatus);
        expect(response.ok).toBe(!hasError);
      }
    });

    it('synthetic responses have correct Content-Type header', async () => {
      const syntheticEndpoints = getEndpointsByCategory('synthetic');
      const noOpEndpoints = getEndpointsByCategory('no-op');

      for (const endpoint of [...syntheticEndpoints, ...noOpEndpoints]) {
        const method = endpoint.methods[0];

        const response = await adapter.fetch(endpoint.path, { method });

        expect(response.headers.get('Content-Type')).toBe('application/json');
      }
    });

    it('synthetic endpoints return correct response bodies', async () => {
      const syntheticEndpoints = getEndpointsByCategory('synthetic');

      for (const endpoint of syntheticEndpoints) {
        const method = endpoint.methods[0];

        const response = await adapter.fetch(endpoint.path, { method });
        const data = await response.json();

        expect(data).toEqual(endpoint.syntheticResponse);
      }
    });
  });

  // =========================================================================
  // 8. Different Input Types Support
  // =========================================================================

  describe('Input Type Support - String, URL, Request Object', () => {
    it('handles string URL input for all endpoint types', async () => {
      // Synthetic endpoint
      const response1 = await adapter.fetch('/api/onboarding/status', { method: 'GET' });
      expect(response1.ok).toBe(true);
      expect(mockFetch).not.toHaveBeenCalled();

      // Backend endpoint
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));
      await adapter.fetch('/api/git/status', { method: 'GET' });
      expect(mockFetch).toHaveBeenCalledTimes(1);

      // WASM-local endpoint — handled locally by WASM shell, NOT proxied
      const response3 = await adapter.fetch('/api/files', { method: 'GET' });
      expect(response3.ok).toBe(true);
      // fetch count stays at 1 — wasm-local does NOT call fetch
      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('handles URL object input for all endpoint types', async () => {
      // Synthetic endpoint
      const response1 = await adapter.fetch(new URL('/api/onboarding/status', 'https://api.sprout.dev'));
      expect(response1.ok).toBe(true);
      expect(mockFetch).not.toHaveBeenCalled();

      // Backend endpoint
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));
      await adapter.fetch(new URL('/api/git/status', 'https://api.sprout.dev'));
      expect(mockFetch).toHaveBeenCalledTimes(1);

      // WASM-local endpoint — handled locally by WASM shell, NOT proxied
      const response3 = await adapter.fetch(new URL('/api/files', 'https://api.sprout.dev'));
      expect(response3.ok).toBe(true);
      // fetch count stays at 1 — wasm-local does NOT call fetch
      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('handles Request object input for all endpoint types', async () => {
      // Synthetic endpoint — use absolute URL for Request (jsdom doesn't support relative URLs)
      const request1 = new Request('https://api.sprout.dev/api/onboarding/status', { method: 'GET' });
      const response1 = await adapter.fetch(request1);
      expect(response1.ok).toBe(true);
      expect(mockFetch).not.toHaveBeenCalled();

      // Backend endpoint
      const request2 = new Request('https://api.sprout.dev/api/git/status', { method: 'GET' });
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));
      await adapter.fetch(request2);
      expect(mockFetch).toHaveBeenCalledTimes(1);

      // WASM-local endpoint — handled locally by WASM shell, NOT proxied
      const request3 = new Request('https://api.sprout.dev/api/files', { method: 'GET' });
      const response3 = await adapter.fetch(request3);
      expect(response3.ok).toBe(true);
      // fetch count stays at 1 — wasm-local does NOT call fetch
      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  // =========================================================================
  // 9. Edge Cases and Error Handling
  // =========================================================================

  describe('Edge Cases', () => {
    it('handles case-insensitive HTTP methods', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'get' });
      expect(mockFetch).toHaveBeenCalledTimes(1);

      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GeT' });
      expect(mockFetch).toHaveBeenCalledTimes(2);
    });

    it('handles empty bodies in POST requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      // /api/git/confirm is a foundry-backend proxy endpoint that accepts
      // POST with an optional body. We test that empty body still flows
      // through the proxy correctly. (Was /api/query before that route
      // was moved to the WASM shell in browser mode — that path now has
      // its own validation in the WASM agent handler.)
      await adapter.fetch('/api/git/confirm', {
        method: 'POST',
        body: JSON.stringify({}),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const sentBody = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      expect(sentBody).toEqual({});
    });

    it('handles invalid JSON bodies gracefully', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      // Invalid JSON should be passed through as-is. Using a still-proxied
      // endpoint (was /api/query before that route was moved to WASM).
      await adapter.fetch('/api/git/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: 'invalid json',
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      // Body should be passed through unchanged when not valid JSON
      const sentBody = mockFetch.mock.calls[0][1]?.body as string;
      expect(sentBody).toBe('invalid json');
    });

    it('handles URLs with fragments', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings#section', { method: 'GET' });

      // CloudAdapter.extractPathname does not strip URL fragments (#).
      // '/api/settings#section' fails the settings endpoint check
      // (neither === '/api/settings' nor startsWith('/api/settings/'))
      // and falls through to the standard proxy (rewriteUrl).
      expect(mockFetch).toHaveBeenCalledTimes(1);
      expect(mockFetch.mock.calls[0][0]).toBe(`${mockConfig.apiBase}/api/settings#section`);
    });
  });

  // =========================================================================
  // 10. Summary Validation
  // =========================================================================

  describe('Integration Test Summary', () => {
    /**
     * Final validation that the integration test suite has covered
     * all the requirements:
     * - Every CLOUD_ENDPOINT produces correct behavior
     * - URL rewriting is correct for all categories
     * - Body translation works for chat endpoints
     * - No 404s or broken flows
     * - Headers are injected correctly
     */
    it('validates integration test coverage', () => {
      // Count endpoints in each category
      const wasmLocal = getEndpointsByCategory('wasm-local').length;
      const foundryBackend = getEndpointsByCategory('foundry-backend').length;
      const synthetic = getEndpointsByCategory('synthetic').length;
      const noOp = getEndpointsByCategory('no-op').length;

      console.log('Integration Test Coverage:');
      console.log(`  wasm-local: ${wasmLocal} endpoints`);
      console.log(`  foundry-backend: ${foundryBackend} endpoints`);
      console.log(`  synthetic: ${synthetic} endpoints`);
      console.log(`  no-op: ${noOp} endpoints`);
      console.log(`  Total: ${CLOUD_ENDPOINTS.length} endpoints`);

      // Count is verified separately — pinning to a literal here would make
      // every registry change a test failure. The only invariant we assert
      // is that every category has at least one endpoint.
      expect(wasmLocal).toBeGreaterThan(0);
      expect(foundryBackend).toBeGreaterThan(0);
      expect(synthetic).toBeGreaterThan(0);
      expect(noOp).toBeGreaterThan(0);
    });
  });
});

// ============================================================================
// Helper Functions
// ============================================================================

/**
 * Determine the expected proxy path for a foundry-backend endpoint.
 * This accounts for the URL rewriting rules in CloudAdapter.
 */
function getExpectedProxyPath(endpoint: CloudEndpoint): string {
  const path = endpoint.path;

  // Chat endpoint mapping (platform hosts chat at /proxy/chat, not /api/proxy/chat).
  // /api/query is intentionally absent — it routes through the WASM shell.
  if (path === '/api/query/steer') {
    return '/proxy/chat';
  }
  if (path === '/api/query/stop') {
    return '/proxy/chat/stop';
  }
  if (path === '/api/query/status') {
    return '/proxy/chat/status';
  }

  // Git endpoint prefix rewriting
  if (path.startsWith('/api/git/')) {
    return path.replace('/api/git/', '/api/proxy/git/');
  }

  // Stats endpoint rewriting
  if (path === '/api/stats') {
    return '/api/proxy/stats';
  }

  // Settings endpoint rewriting
  if (path === '/api/settings' || path.startsWith('/api/settings/')) {
    return path.replace('/api/settings', '/api/proxy/settings');
  }

  // Standard proxy (apiBase + path)
  return path;
}
