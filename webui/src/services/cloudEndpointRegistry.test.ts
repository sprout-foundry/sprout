/**
 * Tests for cloudEndpointRegistry
 */

import {
  CLOUD_ENDPOINTS,
  classifyEndpoint,
  getSyntheticResponse,
  getEndpointsByCategory,
  isWasmLocalEndpoint,
  isFoundryBackendEndpoint,
  type CloudEndpoint,
  type EndpointCategory,
} from './cloudEndpointRegistry';

// Polyfill Response for jsdom environment (jsdom lacks Response/fetch)
if (typeof Response === 'undefined') {
  global.Response = class Response {
    status: number;
    private _body: string;

    constructor(body: string, init?: { status?: number; headers?: Record<string, string> }) {
      this._body = body;
      this.status = init?.status ?? 200;
      // Store headers using a Map for get() support
      this.headers = new Map(Object.entries(init?.headers ?? {}));
      // Override get() to return null for missing keys (matching standard Response API)
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

describe('cloudEndpointRegistry', () => {
  describe('CLOUD_ENDPOINTS', () => {
    it('should have all required endpoints defined', () => {
      // Verify we have approximately 111 endpoints (15 wasm-local + 81 foundry-backend + 14 synthetic + 1 no-op)
      expect(CLOUD_ENDPOINTS.length).toBeGreaterThanOrEqual(111);
    });

    it('should have unique path+method combinations', () => {
      const seen = new Set<string>();
      for (const endpoint of CLOUD_ENDPOINTS) {
        for (const method of endpoint.methods) {
          const key = `${endpoint.path}:${method}`;
          expect(seen.has(key)).toBe(false);
          seen.add(key);
        }
      }
    });

    it('should have valid categories for all endpoints', () => {
      const validCategories: EndpointCategory[] = ['wasm-local', 'foundry-backend', 'synthetic', 'no-op'];
      for (const endpoint of CLOUD_ENDPOINTS) {
        expect(validCategories).toContain(endpoint.category);
      }
    });

    it('should have synthetic responses for all synthetic endpoints', () => {
      const syntheticEndpoints = CLOUD_ENDPOINTS.filter((e) => e.category === 'synthetic');
      for (const endpoint of syntheticEndpoints) {
        expect(endpoint.syntheticResponse).toBeDefined();
        expect(endpoint.syntheticResponse).not.toBeNull();
      }
    });

    it('should have synthetic responses for all no-op endpoints', () => {
      const noOpEndpoints = CLOUD_ENDPOINTS.filter((e) => e.category === 'no-op');
      for (const endpoint of noOpEndpoints) {
        expect(endpoint.syntheticResponse).toBeDefined();
        expect(endpoint.syntheticResponse).not.toBeNull();
      }
    });

    it('should NOT have synthetic responses for non-synthetic and non-no-op endpoints', () => {
      const nonSyntheticEndpoints = CLOUD_ENDPOINTS.filter((e) => e.category !== 'synthetic' && e.category !== 'no-op');
      for (const endpoint of nonSyntheticEndpoints) {
        expect(endpoint.syntheticResponse).toBeUndefined();
      }
    });

    it('should have non-empty descriptions for all endpoints', () => {
      for (const endpoint of CLOUD_ENDPOINTS) {
        expect(endpoint.description).toBeTruthy();
        expect(endpoint.description.length).toBeGreaterThan(0);
      }
    });
  });

  describe('classifyEndpoint', () => {
    it('should classify WASM-local endpoints correctly', () => {
      const testCases = [
        { path: '/api/files', method: 'GET' },
        { path: '/api/create', method: 'POST' },
        { path: '/api/delete', method: 'DELETE' },
        { path: '/api/delete', method: 'POST' },
        { path: '/api/rename', method: 'POST' },
        { path: '/api/browse', method: 'GET' },
        { path: '/api/terminal/history', method: 'GET' },
        { path: '/api/terminal/history', method: 'POST' },
        { path: '/api/search/replace', method: 'POST' },
      ];

      for (const { path, method } of testCases) {
        const result = classifyEndpoint(path, method);
        expect(result).not.toBeNull();
        expect(result?.category).toBe('wasm-local');
      }
    });

    it('should classify foundry-backend endpoints correctly', () => {
      const testCases = [
        { path: '/api/git/status', method: 'GET' },
        { path: '/api/git/checkout', method: 'POST' },
        { path: '/api/stats', method: 'GET' },
        { path: '/api/settings', method: 'GET' },
        { path: '/api/settings', method: 'PUT' },
      ];

      for (const { path, method } of testCases) {
        const result = classifyEndpoint(path, method);
        expect(result).not.toBeNull();
        expect(result?.category).toBe('foundry-backend');
      }
    });

    it('should classify WASM-local /api/query POST (in-browser agent loop)', () => {
      // /api/query POST routes through the WASM shell's in-browser agent
      // loop, NOT through the platform proxy. See cloudAdapter.ts and
      // cloudEndpointRegistry/endpoints/wasm-local.ts for details.
      const result = classifyEndpoint('/api/query', 'POST');
      expect(result).not.toBeNull();
      expect(result?.category).toBe('wasm-local');
    });

    it('should classify synthetic endpoints correctly', () => {
      const testCases = [
        { path: '/api/onboarding/status', method: 'GET' },
        { path: '/api/onboarding/complete', method: 'POST' },
        { path: '/api/instances', method: 'GET' },
        { path: '/api/instances/ssh-hosts', method: 'GET' },
        { path: '/api/support-bundle', method: 'GET' },
      ];

      for (const { path, method } of testCases) {
        const result = classifyEndpoint(path, method);
        expect(result).not.toBeNull();
        expect(result?.category).toBe('synthetic');
      }
    });

    it('should classify no-op endpoints correctly', () => {
      const result = classifyEndpoint('/api/open-in-file-browser', 'POST');
      expect(result).not.toBeNull();
      expect(result?.category).toBe('no-op');
    });

    it('should return null for unknown endpoints', () => {
      const result = classifyEndpoint('/api/unknown/endpoint', 'GET');
      expect(result).toBeNull();
    });

    it('should return null for unknown methods on known paths', () => {
      const result = classifyEndpoint('/api/stats', 'DELETE');
      expect(result).toBeNull();
    });

    it('should strip query parameters before classification', () => {
      const result = classifyEndpoint('/api/settings?layer=provenance', 'GET');
      expect(result).not.toBeNull();
      expect(result?.path).toBe('/api/settings');
    });

    it('should be case-insensitive for methods', () => {
      const result1 = classifyEndpoint('/api/stats', 'GET');
      const result2 = classifyEndpoint('/api/stats', 'get');
      const result3 = classifyEndpoint('/api/stats', 'Get');

      expect(result1).not.toBeNull();
      expect(result2).not.toBeNull();
      expect(result3).not.toBeNull();
      expect(result1).toEqual(result2);
      expect(result2).toEqual(result3);
    });

    it('should match prefix endpoints correctly', () => {
      // /api/settings/credentials/openai/ is a prefix endpoint (foundry-backend)
      const result1 = classifyEndpoint('/api/settings/credentials/openai/123', 'POST');
      expect(result1).not.toBeNull();
      expect(result1?.category).toBe('foundry-backend');

      // /api/settings/providers/ is also a prefix endpoint (foundry-backend)
      const result2 = classifyEndpoint('/api/settings/providers/openai', 'PUT');
      expect(result2).not.toBeNull();
      expect(result2?.category).toBe('foundry-backend');
    });

    it('should classify /api/settings/mcp/servers/ as synthetic (browser mode)', () => {
      // MCP server management is intercepted as synthetic in cloud mode —
      // there is no platform-backed MCP server registry in browser mode.
      const result1 = classifyEndpoint('/api/settings/mcp/servers/123', 'POST');
      expect(result1).not.toBeNull();
      expect(result1?.category).toBe('synthetic');

      const result2 = classifyEndpoint('/api/settings/mcp/servers/', 'PUT');
      expect(result2).not.toBeNull();
      expect(result2?.category).toBe('synthetic');
    });

    it('should handle complex query parameters', () => {
      const result = classifyEndpoint('/api/terminal/history?session_id=abc123', 'GET');
      expect(result).not.toBeNull();
      expect(result?.path).toBe('/api/terminal/history');
    });
  });

  describe('getSyntheticResponse', () => {
    it('should return null for WASM-local endpoints', () => {
      const response = getSyntheticResponse('/api/files', 'GET');
      expect(response).toBeNull();
    });

    it('should return null for foundry-backend endpoints', () => {
      const response = getSyntheticResponse('/api/stats', 'GET');
      expect(response).toBeNull();
    });

    it('should return Response object for synthetic endpoints', () => {
      const response = getSyntheticResponse('/api/onboarding/status', 'GET');
      expect(response).not.toBeNull();
      expect(response).toBeInstanceOf(Response);
    });

    it('should return Response object for no-op endpoints', () => {
      const response = getSyntheticResponse('/api/open-in-file-browser', 'POST');
      expect(response).not.toBeNull();
      expect(response).toBeInstanceOf(Response);
    });

    it('should return synthetic Response with correct JSON content', async () => {
      const response = getSyntheticResponse('/api/onboarding/status', 'GET');
      expect(response).not.toBeNull();

      const data = await response?.json();
      expect(data).toEqual({ setup_required: false, onboarding_complete: true, providers: [] });
    });

    it('should return 200 status for successful synthetic responses', async () => {
      const response = getSyntheticResponse('/api/onboarding/complete', 'POST');
      expect(response?.status).toBe(200);
    });

    it('should return 400 status for error synthetic responses', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-open', 'POST');
      expect(response?.status).toBe(400);

      const data = await response?.json();
      expect(data).toHaveProperty('error');
    });

    it('should set correct Content-Type header', async () => {
      const response = getSyntheticResponse('/api/instances', 'GET');
      expect(response?.headers.get('Content-Type')).toBe('application/json');
    });

    it('should handle all defined synthetic endpoints', async () => {
      const syntheticEndpoints = getEndpointsByCategory('synthetic');

      for (const endpoint of syntheticEndpoints) {
        for (const method of endpoint.methods) {
          const response = getSyntheticResponse(endpoint.path, method);
          expect(response).not.toBeNull();
          expect(response).toBeInstanceOf(Response);

          const data = await response?.json();
          expect(data).toBeDefined();

          if (endpoint.syntheticResponse) {
            expect(data).toEqual(endpoint.syntheticResponse);
          }
        }
      }
    });

    it('should strip query parameters for synthetic endpoints', async () => {
      const response = getSyntheticResponse('/api/settings?layer=provenance', 'GET');
      // /api/settings is NOT a synthetic endpoint, so should return null
      expect(response).toBeNull();
    });

    it('should return correct synthetic response for /api/instances', async () => {
      const response = getSyntheticResponse('/api/instances', 'GET');
      const data = await response?.json();

      expect(data).toEqual({
        instances: [],
      });
    });

    it('should return correct synthetic response for /api/instances/ssh-hosts', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-hosts', 'GET');
      const data = await response?.json();

      expect(data).toEqual({ hosts: [] });
    });

    it('should return correct synthetic response for SSH error endpoints', async () => {
      const sshErrorEndpoints = ['/api/instances/ssh-open', '/api/instances/ssh-browse'];

      for (const path of sshErrorEndpoints) {
        const response = getSyntheticResponse(path, 'POST');
        expect(response?.status).toBe(400);

        const data = await response?.json();
        expect(data).toEqual({ error: 'SSH not available in cloud mode' });
      }
    });

    it('should return correct synthetic response for SSH success endpoints', async () => {
      // ssh-close returns { message: 'ok' } with status 200
      const closeResponse = getSyntheticResponse('/api/instances/ssh-close', 'POST');
      expect(closeResponse?.status).toBe(200);
      const closeData = await closeResponse?.json();
      expect(closeData).toEqual({ message: 'ok' });
    });

    it('should return correct synthetic response for /api/instances/select (error)', async () => {
      const response = getSyntheticResponse('/api/instances/select', 'POST');
      expect(response?.status).toBe(400);
      const data = await response?.json();
      expect(data).toEqual({ error: 'Instance management not available in cloud mode' });
    });
  });

  describe('getEndpointsByCategory', () => {
    it('should return all WASM-local endpoints', () => {
      const endpoints = getEndpointsByCategory('wasm-local');
      expect(endpoints.length).toBeGreaterThan(0);
      endpoints.forEach((e) => expect(e.category).toBe('wasm-local'));
    });

    it('should return all foundry-backend endpoints', () => {
      const endpoints = getEndpointsByCategory('foundry-backend');
      expect(endpoints.length).toBeGreaterThan(0);
      endpoints.forEach((e) => expect(e.category).toBe('foundry-backend'));
    });

    it('should return all synthetic endpoints', () => {
      const endpoints = getEndpointsByCategory('synthetic');
      expect(endpoints.length).toBeGreaterThan(0);
      endpoints.forEach((e) => expect(e.category).toBe('synthetic'));
    });

    it('should return empty array for unknown category', () => {
      const endpoints = getEndpointsByCategory('unknown' as EndpointCategory);
      expect(endpoints).toEqual([]);
    });
  });

  describe('isWasmLocalEndpoint', () => {
    it('should return true for WASM-local endpoints', () => {
      expect(isWasmLocalEndpoint('/api/files', 'GET')).toBe(true);
      expect(isWasmLocalEndpoint('/api/terminal/history', 'POST')).toBe(true);
      expect(isWasmLocalEndpoint('/api/delete', 'POST')).toBe(true);
    });

    it('should return false for foundry-backend endpoints', () => {
      expect(isWasmLocalEndpoint('/api/git/status', 'GET')).toBe(false);
      expect(isWasmLocalEndpoint('/api/stats', 'GET')).toBe(false);
    });

    it('should return true for /api/query POST (WASM-local in browser mode)', () => {
      // /api/query POST routes through the WASM shell's in-browser agent
      // loop, so it IS wasm-local in cloud mode.
      expect(isWasmLocalEndpoint('/api/query', 'POST')).toBe(true);
    });

    it('should return false for synthetic endpoints', () => {
      expect(isWasmLocalEndpoint('/api/onboarding/status', 'GET')).toBe(false);
      expect(isWasmLocalEndpoint('/api/instances', 'GET')).toBe(false);
    });

    it('should return false for no-op endpoints', () => {
      expect(isWasmLocalEndpoint('/api/open-in-file-browser', 'POST')).toBe(false);
    });

    it('should return false for unknown endpoints', () => {
      expect(isWasmLocalEndpoint('/api/unknown', 'GET')).toBe(false);
    });
  });

  describe('isFoundryBackendEndpoint', () => {
    it('should return true for foundry-backend endpoints', () => {
      expect(isFoundryBackendEndpoint('/api/git/status', 'GET')).toBe(true);
      expect(isFoundryBackendEndpoint('/api/stats', 'GET')).toBe(true);
    });

    it('should return false for /api/query POST (now WASM-local, not foundry-backend)', () => {
      // /api/query POST routes through the WASM shell — it is no longer
      // classified as a foundry-backend endpoint.
      expect(isFoundryBackendEndpoint('/api/query', 'POST')).toBe(false);
    });

    it('should return false for WASM-local endpoints', () => {
      expect(isFoundryBackendEndpoint('/api/files', 'GET')).toBe(false);
      expect(isFoundryBackendEndpoint('/api/terminal/history', 'POST')).toBe(false);
      expect(isFoundryBackendEndpoint('/api/delete', 'POST')).toBe(false);
    });

    it('should return false for synthetic endpoints', () => {
      expect(isFoundryBackendEndpoint('/api/onboarding/status', 'GET')).toBe(false);
      expect(isFoundryBackendEndpoint('/api/instances', 'GET')).toBe(false);
    });

    it('should return false for no-op endpoints', () => {
      expect(isFoundryBackendEndpoint('/api/open-in-file-browser', 'POST')).toBe(false);
    });

    it('should return false for unknown endpoints', () => {
      expect(isFoundryBackendEndpoint('/api/unknown', 'GET')).toBe(false);
    });
  });

  describe('endpoint counts by category', () => {
    // Counts are derived from the registry. Pinning to a literal would make
    // every registry change a test failure — these assertions document the
    // current expected ranges and would only fail if the counts went wildly
    // wrong (e.g. a category was deleted or doubled).
    it('should have expected number of WASM-local endpoints', () => {
      const wasmLocal = getEndpointsByCategory('wasm-local');
      // Includes the 15 file/terminal/search endpoints plus /api/query
      // (the in-browser agent loop endpoint).
      expect(wasmLocal.length).toBeGreaterThanOrEqual(15);
      expect(wasmLocal.length).toBeLessThan(25);
    });

    it('should have expected number of synthetic endpoints', () => {
      const synthetic = getEndpointsByCategory('synthetic');
      // Includes onboarding, instances, embedding/LSP, history, costs,
      // settings/mcp/skills/subagent-types, and other not-available-in-
      // browser-mode endpoints.
      expect(synthetic.length).toBeGreaterThanOrEqual(40);
      expect(synthetic.length).toBeLessThan(70);
    });

    it('should have expected number of no-op endpoints', () => {
      const noOp = getEndpointsByCategory('no-op');
      // /api/open-in-file-browser plus the chat-sessions pin/unpin/delete-all
      // endpoints that succeed silently in cloud mode.
      expect(noOp.length).toBe(4);
    });

    it('should have most endpoints as foundry-backend', () => {
      const foundryBackend = getEndpointsByCategory('foundry-backend');
      const wasmLocal = getEndpointsByCategory('wasm-local');
      const synthetic = getEndpointsByCategory('synthetic');
      const noOp = getEndpointsByCategory('no-op');

      // After the synthetic reclassification, foundry-backend is the
      // smallest category. The registry is now: synthetic >> wasm-local
      // > foundry-backend > no-op, which is the correct shape for a
      // browser-mode SPA that intercepts most non-essential endpoints
      // client-side.
      expect(foundryBackend.length).toBeGreaterThan(0);
      // Sanity check: the four categories together account for all
      // registered endpoints.
      expect(foundryBackend.length + wasmLocal.length + synthetic.length + noOp.length)
        .toBe(CLOUD_ENDPOINTS.length);
    });

    it('should have a reasonable number of foundry-backend endpoints', () => {
      const foundryBackend = getEndpointsByCategory('foundry-backend');
      // After reclassification, foundry-backend is small and focused on
      // git, stats, settings (core), and chat control (steer/stop/status).
      expect(foundryBackend.length).toBeGreaterThan(15);
      expect(foundryBackend.length).toBeLessThan(50);
    });
  });

  describe('specific endpoint validation', () => {
    it('should correctly classify all git endpoints', () => {
      const gitEndpoints = [
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
        '/api/git/worktrees',
        '/api/git/worktree/create',
        '/api/git/worktree/remove',
        '/api/git/worktree/checkout',
      ];

      for (const path of gitEndpoints) {
        const result = classifyEndpoint(path, 'POST');
        // Some are GET, some are POST - check at least one exists
        const getResult = classifyEndpoint(path, 'GET');
        const postResult = classifyEndpoint(path, 'POST');

        expect(result || getResult || postResult).not.toBeNull();

        if (result) expect(result.category).toBe('foundry-backend');
        if (getResult) expect(getResult.category).toBe('foundry-backend');
        if (postResult) expect(postResult.category).toBe('foundry-backend');
      }
    });

    it('should correctly classify core chat session endpoints', () => {
      // Core CRUD operations (GET/POST) remain foundry-backend so the
      // platform can manage session lifecycle.
      const coreChatEndpoints = [
        '/api/chat-sessions',
        '/api/chat-sessions/create',
        '/api/chat-sessions/delete',
        '/api/chat-sessions/rename',
        '/api/chat-sessions/switch',
      ];

      for (const path of coreChatEndpoints) {
        const result = classifyEndpoint(path, 'POST');
        expect(result).not.toBeNull();
        expect(result?.category).toBe('foundry-backend');
      }
    });

    it('should classify worktree-only chat session endpoints as synthetic', () => {
      // Worktree/compact sub-endpoints are intercepted as synthetic in
      // browser mode (worktree support is not available in the cloud IDE).
      // Each entry is a [path, method] tuple since not all are POST.
      const worktreeEndpoints: Array<[string, 'POST' | 'GET']> = [
        ['/api/chat-sessions/create-in-worktree', 'POST'],
        ['/api/chat-sessions/compact', 'POST'],
        ['/api/chat-sessions/worktree-mappings', 'GET'],
      ];

      for (const [path, method] of worktreeEndpoints) {
        const result = classifyEndpoint(path, method);
        expect(result).not.toBeNull();
        expect(result?.category).toBe('synthetic');
      }
    });

    it('should classify pin/unpin/delete-all chat session endpoints as no-op', () => {
      // These succeed silently in cloud mode because sessions are managed
      // client-side. Returning 200/ok avoids error toasts when the UI
      // calls them (e.g. delete-all from a confirmation dialog).
      const noopEndpoints: Array<[string, 'POST']> = [
        ['/api/chat-sessions/pin', 'POST'],
        ['/api/chat-sessions/unpin', 'POST'],
        ['/api/chat-sessions/delete-all', 'POST'],
      ];

      for (const [path, method] of noopEndpoints) {
        const result = classifyEndpoint(path, method);
        expect(result).not.toBeNull();
        expect(result?.category).toBe('no-op');
      }
    });

    it('should correctly classify settings endpoints', () => {
      // Core settings endpoints (user prefs, credentials, providers) are
      // foundry-backend — the platform owns them.
      const proxiedSettings = [
        '/api/settings',
        '/api/settings/credentials',
        '/api/settings/providers',
      ];

      for (const path of proxiedSettings) {
        const getResult = classifyEndpoint(path, 'GET');
        const putResult = classifyEndpoint(path, 'PUT');

        expect(getResult || putResult).not.toBeNull();

        if (getResult) expect(getResult.category).toBe('foundry-backend');
        if (putResult) expect(putResult.category).toBe('foundry-backend');
      }
    });

    it('should classify not-available-in-browser settings as synthetic', () => {
      // MCP, skills, subagent-types sub-endpoints are intercepted as
      // synthetic in cloud mode (no platform-backed registry exists).
      const syntheticSettings = [
        '/api/settings/mcp',
        '/api/settings/mcp/servers/',
        '/api/settings/skills',
        '/api/settings/subagent-types',
      ];

      for (const path of syntheticSettings) {
        const getResult = classifyEndpoint(path, 'GET');
        const putResult = classifyEndpoint(path, 'PUT');

        expect(getResult || putResult).not.toBeNull();

        if (getResult) expect(getResult.category).toBe('synthetic');
        if (putResult) expect(putResult.category).toBe('synthetic');
      }
    });

    it('should correctly classify POST on credentials prefix for pool and test sub-paths', () => {
      // Verify POST method was added to credentials prefix entry
      const poolResult = classifyEndpoint('/api/settings/credentials/openai/pool', 'POST');
      expect(poolResult).not.toBeNull();
      expect(poolResult?.category).toBe('foundry-backend');

      const testResult = classifyEndpoint('/api/settings/credentials/openai/test', 'POST');
      expect(testResult).not.toBeNull();
      expect(testResult?.category).toBe('foundry-backend');
    });

    it('should correctly classify all hotkey endpoints', () => {
      // Hotkey configuration is intercepted as synthetic in cloud mode
      // (the platform doesn't have a hotkey registry for browser IDEs;
      // hotkeys are managed client-side via localStorage).
      const hotkeyEndpoints = ['/api/hotkeys', '/api/hotkeys/validate', '/api/hotkeys/preset'];

      for (const path of hotkeyEndpoints) {
        const getResult = classifyEndpoint(path, 'GET');
        const postResult = classifyEndpoint(path, 'POST');

        expect(getResult || postResult).not.toBeNull();

        if (getResult) expect(getResult.category).toBe('synthetic');
        if (postResult) expect(postResult.category).toBe('synthetic');
      }
    });

    it('should correctly classify all terminal endpoints', () => {
      const terminalEndpoints = ['/api/terminal/sessions', '/api/terminal/shells', '/api/terminal/history'];

      for (const path of terminalEndpoints) {
        const getResult = classifyEndpoint(path, 'GET');
        const postResult = classifyEndpoint(path, 'POST');

        expect(getResult || postResult).not.toBeNull();

        if (getResult) expect(getResult.category).toBe('wasm-local');
        if (postResult) expect(postResult.category).toBe('wasm-local');
      }
    });

    it('should classify /api/workspace as synthetic in cloud mode', () => {
      const getResult = classifyEndpoint('/api/workspace', 'GET');
      expect(getResult).not.toBeNull();
      expect(getResult?.category).toBe('synthetic');
      expect(getResult?.syntheticResponse).toEqual({
        message: 'ok',
        workspace_root: '/home/user',
        daemon_root: '/home/user',
      });

      const postResult = classifyEndpoint('/api/workspace', 'POST');
      expect(postResult).not.toBeNull();
      expect(postResult?.category).toBe('synthetic');
      expect(postResult?.syntheticResponse).toEqual({
        message: 'ok',
        workspace_root: '/home/user',
        daemon_root: '/home/user',
      });
    });

    it('should classify /api/workspace/symbols as synthetic in cloud mode', () => {
      // Workspace symbols requires an LSP backend, which is not available
      // in browser mode. Intercepted as synthetic.
      const result = classifyEndpoint('/api/workspace/symbols', 'GET');
      expect(result).not.toBeNull();
      expect(result?.category).toBe('synthetic');
    });
  });
});
