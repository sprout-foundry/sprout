/**
 * Tests for CloudAdapter
 */

import { WEBUI_CLIENT_ID_HEADER, getWebUIClientId } from './clientSession';
import { CloudAdapter, type CloudAdapterConfig } from './cloudAdapter';

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

// Mock cloudWasmHandlers — we test handleWasmLocal directly via the adapter,
// but the adapter imports jsonError from this module.
vi.mock('./cloudWasmHandlers', async (importOriginal): Promise<typeof import('./cloudWasmHandlers')> => {
  const actual = await importOriginal();
  return {
    ...actual,
    jsonError: vi.fn(
      (message: string, status: number) =>
        new Response(JSON.stringify({ error: message, message }), {
          status,
          headers: { 'Content-Type': 'application/json' },
        }),
    ),
  };
});

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

// Polyfill Request for jsdom environment
// Unconditionally override to support relative URLs (e.g., '/api/stats')
// since native Node.js Request requires absolute URLs.
global.Request = class Request {
  url: string;
  method: string;
  headers: Headers | Map<string, string>;
  private _body: string | null;

  constructor(input: string | Request, init?: RequestInit | { method?: string }) {
    if (typeof input === 'string') {
      // Store relative URLs as-is — the CloudAdapter reads this.url directly
      this.url = input;
      this.method = init?.method ?? 'GET';
      this.headers = new Headers(init?.headers);
      // Store body for testing
      if (init?.body) {
        this._body = typeof init.body === 'string' ? init.body : JSON.stringify(init.body);
      } else {
        this._body = null;
      }
    } else {
      this.url = input.url;
      this.method = input.method;
      this.headers = input.headers;
      // Copy body from existing Request
      this._body = (input as Request & { _body: string | null })._body || null;
    }
  }

  // Support body cloning and reading for chat endpoint tests
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

describe('CloudAdapter', () => {
  let adapter: CloudAdapter;
  let mockConfig: CloudAdapterConfig;
  let mockFetch: vi.Mock;

  beforeEach(() => {
    // Setup mock config
    mockConfig = {
      apiBase: 'https://api.sprout.dev',
      wsUrl: 'wss://api.sprout.dev/ws',
      navItems: [
        { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'tasks', order: 1 },
        { id: 'billing', label: 'Billing', href: '/billing', icon: 'billing', order: 2 },
      ],
    };

    // Create adapter instance
    adapter = new CloudAdapter(mockConfig);

    // Mock global fetch
    mockFetch = vi.fn();
    global.fetch = mockFetch;
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe('constructor and properties', () => {
    it('should have correct name', () => {
      expect(adapter.name).toBe('foundry-cloud');
    });

    it('should require backend health check', () => {
      expect(adapter.requiresBackendHealthCheck).toBe(true);
    });

    it('should indicate file ops are not via API', () => {
      expect(adapter.fileOpsViaAPI).toBe(false);
    });

    it('should not show onboarding', () => {
      expect(adapter.showOnboarding).toBe(false);
    });

    it('should not support SSH (cloud has no access to local network)', () => {
      expect(adapter.supportsSSH).toBe(false);
      expect(adapter.supportsInstances).toBe(true);
    });

    it('should not support local terminal (it is WASM-backed)', () => {
      // supportsLocalTerminal is intentionally false — the terminal in cloud mode
      // is provided by the WASM shell, not a local PTY.
      expect(adapter.supportsLocalTerminal).toBe(false);
    });

    it('should support settings (BYOK settings in cloud mode)', () => {
      // supportsSettings is true — users can configure their own API keys (BYOK)
      // through the settings panel in cloud mode.
      expect(adapter.supportsSettings).toBe(true);
    });

    it('should store platform nav items', () => {
      expect(adapter.platformNavItems).toEqual(mockConfig.navItems);
    });

    it('should return WebSocket URL', () => {
      expect(adapter.getWebSocketURL()).toBe(mockConfig.wsUrl);
    });

    it('should return null WebSocket URL if not configured', () => {
      const configWithoutWs: CloudAdapterConfig = {
        apiBase: 'https://api.sprout.dev',
        wsUrl: '',
      };
      const adapterWithoutWs = new CloudAdapter(configWithoutWs);
      expect(adapterWithoutWs.getWebSocketURL()).toBe('');
    });
  });

  describe('fetch - synthetic endpoint interception', () => {
    it('should return synthetic response for onboarding status', async () => {
      const response = await adapter.fetch('/api/onboarding/status', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ setup_required: false, onboarding_complete: true, providers: [] });

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for onboarding complete', async () => {
      const response = await adapter.fetch('/api/onboarding/complete', {
        method: 'POST',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ message: 'ok' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for instances list', async () => {
      const response = await adapter.fetch('/api/instances', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({
        instances: [],
      });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for SSH hosts', async () => {
      const response = await adapter.fetch('/api/instances/ssh-hosts', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ hosts: [] });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic error response for SSH open', async () => {
      const response = await adapter.fetch('/api/instances/ssh-open', {
        method: 'POST',
      });

      expect(response.status).toBe(400);
      const data = await response.json();
      expect(data).toEqual({ error: 'SSH not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for SSH sessions', async () => {
      const response = await adapter.fetch('/api/instances/ssh-sessions', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ sessions: [] });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic error response for SSH browse', async () => {
      const response = await adapter.fetch('/api/instances/ssh-browse', {
        method: 'POST',
      });

      expect(response.status).toBe(400);
      const data = await response.json();
      expect(data).toEqual({ error: 'SSH not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic success response for SSH close', async () => {
      const response = await adapter.fetch('/api/instances/ssh-close', {
        method: 'POST',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ message: 'ok' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic error response for instance select', async () => {
      const response = await adapter.fetch('/api/instances/select', {
        method: 'POST',
      });

      expect(response.status).toBe(400);
      const data = await response.json();
      expect(data).toEqual({ error: 'Instance management not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic error response for support bundle', async () => {
      const response = await adapter.fetch('/api/support-bundle', {
        method: 'GET',
      });

      expect(response.status).toBe(400);
      const data = await response.json();
      expect(data).toEqual({ error: 'Support bundles not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should set correct Content-Type header for synthetic responses', async () => {
      const response = await adapter.fetch('/api/onboarding/status', {
        method: 'GET',
      });

      expect(response.headers.get('Content-Type')).toBe('application/json');
    });
  });

  const workspaceSyntheticResponse = {
    message: 'ok',
    workspace_root: '/home/user',
    daemon_root: '/home/user',
  };

  describe('fetch - workspace endpoint synthetic response', () => {
    it('should return synthetic response for GET /api/workspace', async () => {
      const response = await adapter.fetch('/api/workspace', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual(workspaceSyntheticResponse);

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for POST /api/workspace', async () => {
      const response = await adapter.fetch('/api/workspace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ workspace_root: '/new/path' }),
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual(workspaceSyntheticResponse);

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should set correct Content-Type header for workspace synthetic response', async () => {
      const response = await adapter.fetch('/api/workspace', {
        method: 'GET',
      });

      expect(response.headers.get('Content-Type')).toBe('application/json');
    });

    it('should return synthetic workspace response when URL object is used', async () => {
      const response = await adapter.fetch(new URL('/api/workspace', 'https://api.sprout.dev'));

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual(workspaceSyntheticResponse);

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic workspace response when Request object is used', async () => {
      const request = new Request('/api/workspace', { method: 'GET' });
      const response = await adapter.fetch(request);

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual(workspaceSyntheticResponse);

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });
  });

  describe('fetch - WASM-local endpoint handling (file CRUD, terminal, search)', () => {
    // These endpoints MUST be handled by the WASM shell in the browser,
    // NOT proxied to the Foundry backend. The CloudAdapter intercepts them
    // and delegates to handleWasmLocal() which uses the WASM shell.

    beforeEach(() => {
      // Reset WASM shell mocks before each test
      vi.clearAllMocks();
    });

    it('should handle GET /api/files locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/files', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      // Should return file list from WASM shell
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('files');
      // The WASM handler returns 'success' for the file list endpoint,
      // not 'ok' — different from synthetic responses which use 'ok'.
      expect(data).toHaveProperty('message', 'success');
    });

    it('should handle GET /api/browse locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/browse?path=/home/user', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      // Should return directory entries from WASM shell
      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('files');
    });

    it('should handle POST /api/create locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: '/test.txt' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('message', 'ok');
    });

    it('should handle POST /api/delete locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: '/test.txt' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle DELETE /api/delete locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/delete', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: '/test.txt' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle POST /api/file (write) locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/file?path=/newfile.txt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: 'hello world' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle POST /api/file/check-modified locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/file/check-modified', {
        method: 'POST',
        body: JSON.stringify({ files: ['/path/to/file.txt'] }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/workspace/browse locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/workspace/browse?path=/home/user', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle POST /api/rename locally via WASM shell (NOT proxied)', async () => {
      mockWasmShell.executeCommand.mockReturnValueOnce({
        stdout: '',
        stderr: '',
        exitCode: 0,
      });

      const response = await adapter.fetch('/api/rename', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ old_path: '/old.txt', new_path: '/new.txt' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/search locally via WASM shell (NOT proxied)', async () => {
      mockWasmShell.executeCommand.mockReturnValueOnce({
        stdout: '/home/user/src/main.go:10:package main',
        stderr: '',
        exitCode: 0,
      });

      const response = await adapter.fetch('/api/search?query=package&case_sensitive=true', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('results');
      expect(data).toHaveProperty('total_matches');
    });

    it('should handle POST /api/search/replace locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/search/replace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ search: 'foo', replace: 'bar', files: ['/src/main.go'] }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/terminal/sessions locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/terminal/sessions', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('active_count', 0);
    });

    it('should handle GET /api/terminal/shells locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/terminal/shells', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toHaveProperty('shells');
    });

    it('should handle GET /api/terminal/history locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/terminal/history', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle POST /api/terminal/history locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/terminal/history', {
        method: 'POST',
        body: JSON.stringify({ command: 'ls -la' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/file locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/file?path=/README.md', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/file/check-modified locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/file/check-modified', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle GET /api/files/prettier-config locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/files/prettier-config', { method: 'GET' });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should handle POST /api/file/consent locally via WASM shell (NOT proxied)', async () => {
      const response = await adapter.fetch('/api/file/consent', {
        method: 'POST',
        body: JSON.stringify({ path: '/test.txt' }),
      });

      // MUST NOT proxy to backend
      expect(mockFetch).not.toHaveBeenCalled();

      expect(response.ok).toBe(true);
    });

    it('should fall through to server proxy when WASM shell init fails (graceful fallback)', async () => {
      // Simulate WASM shell init failure
      const { initWasmShell } = await import('./wasmShell');
      const originalInit = initWasmShell as vi.Mock;
      originalInit.mockImplementationOnce(() => Promise.reject(new Error('WASM not available')));

      // Force a new adapter so it re-initializes WASM
      const freshAdapter = new CloudAdapter(mockConfig);

      // Mock the proxy fetch to simulate the server safety-net response
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ files: [], handled_by: 'wasm-shell' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const response = await freshAdapter.fetch('/api/files', { method: 'GET' });

      // Should NOT return 503 — instead falls through to proxy
      expect(response.ok).toBe(true);

      // Should have proxied to the server safety-net handler
      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/files');

      // Restore mock
      originalInit.mockImplementation(() => Promise.resolve(mockWasmShell));
    });

    it('should fall through to server proxy for wasm-local endpoint when WASM shell is unavailable', async () => {
      // Simulate WASM shell init failure for a terminal endpoint
      const { initWasmShell } = await import('./wasmShell');
      const originalInit = initWasmShell as vi.Mock;
      originalInit.mockImplementationOnce(() => Promise.reject(new Error('WASM load error')));

      const freshAdapter = new CloudAdapter(mockConfig);

      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ entries: [], handled_by: 'wasm-shell' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const response = await freshAdapter.fetch('/api/terminal/history', { method: 'GET' });

      expect(response.ok).toBe(true);
      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/terminal/history');

      // Restore mock
      originalInit.mockImplementation(() => Promise.resolve(mockWasmShell));
    });
  });

  describe('fetch - Foundry backend proxying', () => {
    it('should proxy git status endpoint to Foundry proxy', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: {} }), { status: 200 }));

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/status');
    });

    it('should proxy stats endpoint to Foundry proxy', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
    });

    it('should proxy settings endpoint to Foundry (deprecated - now handled by settings proxy)', async () => {
      // This test is deprecated since /api/settings is now rewritten to /api/proxy/settings
      // See the "fetch - settings endpoint translation" describe block for the actual behavior
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Now rewritten to /api/proxy/settings
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
    });

    it('should proxy chat-sessions endpoint to Foundry', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ sessions: [] }), { status: 200 }));

      await adapter.fetch('/api/chat-sessions', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/chat-sessions');
    });

    it('should preserve body from Request object for standard backend proxy', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const request = new Request('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ theme: 'dark' }),
      });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Now /api/settings is rewritten to /api/proxy/settings
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ theme: 'dark' });
    });
  });

  describe('fetch - header handling', () => {
    it('should add WebUI client ID header to proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should include credentials for auth', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.credentials).toBe('include');
    });

    it('should route POST /api/query through WASM shell (runAgent) and NOT call fetch', async () => {
      // POST /api/query now runs the agent loop in-browser via the WASM shell.
      // The WASM binary handles auth + LLM calls via /proxy/chat, so the
      // CloudAdapter must NOT proxy /api/query through fetch — it must invoke
      // shell.runAgent with the parsed query/provider/model.

      const customHeaders = new Headers({
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      });

      await adapter.fetch('/api/query', {
        method: 'POST',
        headers: customHeaders,
        body: JSON.stringify({
          query: 'test',
          provider: 'anthropic',
          model: 'claude-3',
        }),
      });

      // The agent runs in WASM — global.fetch must NOT be called for /api/query.
      expect(mockFetch).not.toHaveBeenCalled();

      // The agent must have been invoked with the parsed fields.
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      const runAgentCall = mockWasmShell.runAgent.mock.calls[0];
      expect(runAgentCall[0]).toBe('platform'); // provider (defaults to platform in cloud)
      expect(runAgentCall[1]).toBe(''); // model (empty — handled by platform)
      expect(runAgentCall[2]).toBe('test'); // query
      expect(typeof runAgentCall[3]).toBe('function'); // onEvent callback
    });

    it('should NOT add headers to synthetic responses', async () => {
      const response = await adapter.fetch('/api/onboarding/status', {
        method: 'GET',
      });

      expect(mockFetch).not.toHaveBeenCalled();
      expect(response.headers.get('x-webui-client-id')).toBeNull();
    });
  });

  describe('fetch - URL rewriting', () => {
    it('should rewrite relative URLs to apiBase', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/test');
    });

    it('should NOT rewrite absolute URLs', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('https://example.com/api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://example.com/api/test');
    });

    it('should NOT rewrite URLs without leading slash', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('api/test');
    });
  });

  describe('fetch - different input types', () => {
    it('should handle string URL input', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats');

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle URL object input', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch(new URL('/api/stats', 'https://api.sprout.dev'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should intercept synthetic responses when URL object input is used', async () => {
      const response = await adapter.fetch(new URL('/api/onboarding/status', 'https://api.sprout.dev'));

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ setup_required: false, onboarding_complete: true, providers: [] });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should intercept synthetic responses when URL object with query params is used', async () => {
      const response = await adapter.fetch(new URL('/api/instances?foo=bar', 'https://api.sprout.dev'));

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ instances: [] });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should intercept synthetic responses when Request object input is used', async () => {
      const request = new Request('/api/onboarding/status', { method: 'GET' });
      const response = await adapter.fetch(request);

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ setup_required: false, onboarding_complete: true, providers: [] });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should NOT intercept synthetic response for non-api URL object', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch(new URL('/health', 'https://api.sprout.dev'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle Request object input', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const request = new Request('/api/stats', { method: 'GET' });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('fetch - case insensitivity', () => {
    it('should handle lowercase HTTP methods', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'get' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle mixed case HTTP methods', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GeT' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('fetch - query parameter handling', () => {
    it('should strip query parameters when classifying endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings?layer=provenance', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Now /api/settings is rewritten to /api/proxy/settings
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings?layer=provenance');
    });

    it('should preserve query parameters in proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      // Use a Foundry-backend endpoint (not WASM-local) to test query param preservation
      await adapter.fetch('/api/chat-sessions?limit=10', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toContain('limit=10');
    });
  });

  describe('interception priority', () => {
    it('should intercept synthetic responses before URL rewriting', async () => {
      const response = await adapter.fetch('/api/onboarding/status', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should NOT intercept unknown endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/unknown/endpoint', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/unknown/endpoint');
    });
  });

  describe('fetch - chat endpoint translation', () => {
    // Architectural note: POST /api/query no longer proxies to the Foundry chat
    // backend. The WASM shell's runAgent() executes the full agent loop in-browser.
    // /api/query/steer, /api/query/stop, /api/query/status still proxy to the
    // platform's /proxy/chat* paths (see CHAT_ENDPOINT_MAP in cloudProxyRoutes.ts).

    it('should route POST /api/query through WASM shell (NOT to chat proxy)', async () => {
      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).not.toHaveBeenCalled();
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      const [provider, model, query, onEvent] = mockWasmShell.runAgent.mock.calls[0];
      expect(provider).toBe('platform');
      expect(model).toBe('');
      expect(query).toBe('hello');
      expect(typeof onEvent).toBe('function');
    });

    // DELETED: "should translate POST /api/query body from webui format to Foundry format"
    // — body format translation (webui {query} → Foundry {messages, stream}) was
    // performed by the dumb chat proxy. With WASM runAgent, the WASM binary expects
    // the native webui format directly. There is no translation step to verify.

    it('should return 200 OK immediately when routing /api/query to WASM shell', async () => {
      // The WASM agent runs asynchronously — the HTTP response is fire-and-forget.
      // Events stream back via the agentEventDispatcher callback passed to runAgent.
      const response = await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toMatchObject({ status: 'processing' });
    });

    it('should translate POST /api/query/steer URL to /proxy/chat with steer flag', async () => {
      // /api/query/steer still proxies to the chat backend (the WASM shell does
      // not implement steering in-browser — that's a server-side concern).
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/query/steer', {
        method: 'POST',
        body: JSON.stringify({ query: 'adjust tone' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Platform hosts chat at /proxy/chat (not /api/proxy/chat) — see
      // SP-CLOUD-4 and CHAT_ENDPOINT_MAP in cloudProxyRoutes.ts.
      expect(call[0]).toBe('https://api.sprout.dev/proxy/chat');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody.steer).toBe(true);
    });

    it('should translate POST /api/query/stop URL to /proxy/chat/stop', async () => {
      // /api/query/stop still proxies to the chat backend.
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/query/stop', {
        method: 'POST',
        body: JSON.stringify({ chat_id: 'chat-123' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/proxy/chat/stop');
      // Body should be passed through unchanged (no translation for stop)
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ chat_id: 'chat-123' });
    });

    it('should translate GET /api/query/status URL to /proxy/chat/status', async () => {
      // /api/query/status still proxies to the chat backend.
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: 'idle' }), { status: 200 }));

      await adapter.fetch('/api/query/status', {
        method: 'GET',
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/proxy/chat/status');
    });

    // DELETED: "should preserve chat_id in translated body"
    // — chat_id was a field on the Foundry-format body produced by translation.
    // With WASM runAgent, the WASM binary owns chat_id (via its event dispatcher
    // callback); the webui does not need to forward it in the request body.

    it('should default to platform provider in cloud mode (regardless of user input)', async () => {
      // In cloud mode, the WASM agent always uses the platform provider config.
      // User-supplied provider/model are ignored — the platform owns the LLM.
      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'test', provider: 'anthropic', model: 'claude-3' }),
      });

      expect(mockFetch).not.toHaveBeenCalled();
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      const [provider, model] = mockWasmShell.runAgent.mock.calls[0];
      expect(provider).toBe('platform');
      expect(model).toBe('');
    });

    // DELETED: "should preserve workspace_root and system_prompt if present"
    // — workspace_root and system_prompt were fields on the Foundry-format body
    // produced by translation. The WASM shell reads workspace_root from its
    // virtual filesystem root and system_prompt from .config/sprout/. There is
    // no HTTP-level forwarding — those are WASM-side concerns, not adapter
    // concerns.

    it('should NOT translate non-chat endpoints (stats uses stats proxy)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Stats now uses stats proxy with URL rewriting
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
    });

    // DELETED: "should include WebUI client ID header in translated requests"
    // — the WebUI client ID is for the Foundry HTTP proxy. WASM runAgent runs
    // in-browser and has no HTTP request to label. (The shell's own platform
    // provider config carries the identity it needs.)

    // DELETED: "should include credentials in translated requests"
    // — credentials are for cross-origin HTTP auth with the Foundry proxy.
    // WASM runAgent runs in-browser; auth is handled by the shell's
    // provider config (which writes cookies/storage directly).

    // DELETED: "should set Content-Type to application/json for chat requests"
    // — Content-Type is an HTTP request framing concern. The WASM shell receives
    // the parsed body via runAgent(provider, model, query, callback) — no HTTP
    // framing is involved.

    it('should reject empty query with 400 (WASM shell validates)', async () => {
      // The WASM handler returns a 400 error response when the query is empty.
      // (Previously the Foundry proxy would validate and accept an empty query;
      // with WASM the validation happens client-side in handleWasmAgentQuery.)
      const response = await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: '' }),
      });

      expect(response.status).toBe(400);
      // runAgent must NOT have been called for an empty query.
      expect(mockWasmShell.runAgent).not.toHaveBeenCalled();
    });

    it('should handle /api/query with URL query parameters (stripped before routing)', async () => {
      // The CloudAdapter strips query params from the pathname before classifying
      // the endpoint. So /api/query?chat_id=abc is still routed to WASM runAgent.
      await adapter.fetch('/api/query?chat_id=abc', {
        method: 'POST',
        body: JSON.stringify({ query: 'test' }),
      });

      expect(mockFetch).not.toHaveBeenCalled();
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      expect(mockWasmShell.runAgent.mock.calls[0][2]).toBe('test');
    });

    // DELETED: "should translate body when Request object is used for chat endpoint"
    // — body translation is gone with the WASM agent. The handler now reads
    // the body as JSON via input.clone().text() and parses it natively. The
    // equivalent test below verifies that runAgent still receives the query
    // when the caller passes a Request object.

    // DELETED: "should translate body with steer flag when Request object is used"
    // — steer flag is set server-side by the chat proxy on POST /api/query/steer
    // (see CHAT_ENDPOINT_MAP). This test was specifically about POST /api/query
    // body translation, which is gone. The proxy behavior is covered by the
    // "translate POST /api/query/steer URL to /proxy/chat with steer flag" test.

    it('should invoke runAgent from a Request object body', async () => {
      // The WASM agent must receive the body of a Request object too, not just
      // a string init.body. Replaces the deleted "translate body when Request
      // object is used" test — the new behavior is: parse the body and pass
      // the query to runAgent.
      const request = new Request('/api/query', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: 'test from request object' }),
      });
      await adapter.fetch(request);

      expect(mockFetch).not.toHaveBeenCalled();
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      expect(mockWasmShell.runAgent.mock.calls[0][2]).toBe('test from request object');
    });
  });

  describe('fetch - git endpoint translation', () => {
    it('should translate GET /api/git/status to /api/proxy/git/status', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: {} }), { status: 200 }));

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/status');
    });

    it('should translate POST /api/git/stage to /api/proxy/git/stage', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/stage', {
        method: 'POST',
        body: JSON.stringify({ files: ['foo.txt'] }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/stage');
    });

    it('should translate POST /api/git/commit to /api/proxy/git/commit', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/commit', {
        method: 'POST',
        body: JSON.stringify({ message: 'test commit' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/commit');
    });

    it('should translate POST /api/git/branch/create to /api/proxy/git/branch/create', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/branch/create', {
        method: 'POST',
        body: JSON.stringify({ name: 'new-branch' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/branch/create');
    });

    it('should preserve query parameters for git diff', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ diff: '' }), { status: 200 }));

      await adapter.fetch('/api/git/diff?path=foo.txt&cached=false', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/diff?path=foo.txt&cached=false');
    });

    it('should preserve query parameters for git log', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ commits: [] }), { status: 200 }));

      await adapter.fetch('/api/git/log?limit=10&offset=0', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/log?limit=10&offset=0');
    });

    it('should translate nested git deep-review paths', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ items: [] }), { status: 200 }));

      await adapter.fetch('/api/git/deep-review/fix/start', {
        method: 'POST',
        body: JSON.stringify({ review_id: '123' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/deep-review/fix/start');
    });

    it('should translate git deep-review/fix with POST', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/deep-review/fix', {
        method: 'POST',
        body: JSON.stringify({ fixes: [] }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/deep-review/fix');
    });

    it('should translate git deep-review/fix/status with GET', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: 'running' }), { status: 200 }));

      await adapter.fetch('/api/git/deep-review/fix/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/deep-review/fix/status');
    });

    it('should include WebUI client ID header in git requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should include credentials in git requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.credentials).toBe('include');
    });

    it('should pass through POST body unchanged for git endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const requestBody = {
        files: ['foo.txt', 'bar.txt'],
        message: 'test commit',
        author: 'Test User',
      };

      await adapter.fetch('/api/git/commit', {
        method: 'POST',
        body: JSON.stringify(requestBody),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual(requestBody);
    });

    it('should NOT affect non-git endpoints (stats uses stats proxy)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Should use stats proxy, not git proxy
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
      expect(call[0]).not.toContain('/api/proxy/git');
    });

    it('should NOT affect settings endpoints (they use settings proxy)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Settings endpoints are handled by settings proxy, not git proxy
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
    });

    it('should NOT affect chat endpoints (routes through WASM shell, not git proxy)', async () => {
      // POST /api/query no longer goes through the chat proxy — it routes
      // through the WASM shell's runAgent. The "git proxy" route is for git
      // operations only; chat has its own WASM-based routing.
      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'test' }),
      });

      // Chat endpoint must NOT be proxied (no fetch call).
      expect(mockFetch).not.toHaveBeenCalled();

      // Chat endpoint MUST route through WASM shell's runAgent.
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      expect(mockWasmShell.runAgent.mock.calls[0][2]).toBe('test');
    });

    it('should preserve existing headers in git requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const customHeaders = new Headers({
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      });

      await adapter.fetch('/api/git/status', {
        method: 'GET',
        headers: customHeaders,
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('Content-Type')).toBe('application/json');
      expect(call[1]?.headers?.get('X-Custom-Header')).toBe('custom-value');
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should translate git checkout', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/git/checkout', {
        method: 'POST',
        body: JSON.stringify({ ref: 'main' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/checkout');
    });

    it('should translate git worktree endpoints', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ worktrees: [] }), { status: 200 }));

      await adapter.fetch('/api/git/worktree/create', {
        method: 'POST',
        body: JSON.stringify({ path: '/tmp/worktree' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/worktree/create');
    });

    it('should translate git endpoint with Request object', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: {} }), { status: 200 }));

      const request = new Request('/api/git/status', { method: 'GET' });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/status');
    });

    it('should translate git endpoint with absolute URL string to Foundry backend', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: {} }), { status: 200 }));

      await adapter.fetch('https://other-host.example.com/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Should proxy to Foundry backend, NOT the other-host URL
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/status');
      expect(call[0]).not.toContain('other-host.example.com');
    });

    it('should translate git endpoint with URL object to Foundry backend', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ diff: '' }), { status: 200 }));

      await adapter.fetch(new URL('/api/git/diff?path=foo.txt', 'https://other-host.example.com'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Should proxy to Foundry backend with path and query params preserved
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/diff?path=foo.txt');
      expect(call[0]).not.toContain('other-host.example.com');
    });

    it('should preserve body when Request object with POST is used for git endpoint', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      const request = new Request('/api/git/commit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'test commit', files: ['a.ts'] }),
      });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/commit');
      // Body from Request object should be preserved
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ message: 'test commit', files: ['a.ts'] });
    });
  });

  describe('fetch - settings endpoint translation', () => {
    it('should translate GET /api/settings to /api/proxy/settings', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
    });

    it('should translate GET /api/settings with query params and preserve them', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings?layer=provenance', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings?layer=provenance');
    });

    it('should translate PUT /api/settings with body', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ theme: 'dark' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ theme: 'dark' });
    });

    it('should translate GET /api/settings/credentials to /api/proxy/settings/credentials', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ credentials: [] }), { status: 200 }));

      await adapter.fetch('/api/settings/credentials', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/credentials');
    });

    it('should translate PUT /api/settings/credentials/openai/ with body', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings/credentials/openai/', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: 'sk-...' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/credentials/openai/');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ api_key: 'sk-...' });
    });

    it('should translate DELETE /api/settings/credentials/openai/', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings/credentials/openai/', {
        method: 'DELETE',
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/credentials/openai/');
    });

    it('should translate POST /api/settings/credentials/openai/test with body', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ valid: true }), { status: 200 }));

      await adapter.fetch('/api/settings/credentials/openai/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: 'sk-...' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/credentials/openai/test');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ api_key: 'sk-...' });
    });

    it('should translate GET /api/settings/providers to /api/proxy/settings/providers', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ providers: [] }), { status: 200 }));

      await adapter.fetch('/api/settings/providers', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/providers');
    });

    it('should translate PUT /api/settings/providers/openai/ with body', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ success: true }), { status: 200 }));

      await adapter.fetch('/api/settings/providers/openai/', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: 'claude-3' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings/providers/openai/');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ model: 'claude-3' });
    });

    // NOTE: Settings sub-endpoint proxy tests for /api/settings/mcp/*,
    // /api/settings/skills, /api/settings/subagent-types/*, and /api/hotkeys*
    // were removed. Those endpoints are now intercepted as synthetic responses
    // (not available in browser mode) instead of being proxied to the platform
    // backend. See cloudEndpointRegistry/endpoints/synthetic.ts for their
    // definitions.
  });

  describe('fetch - stats endpoint translation', () => {
    it('should translate GET /api/stats to /api/proxy/stats', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
    });

    it('should include WebUI client ID header in stats requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should include credentials in stats requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.credentials).toBe('include');
    });

    it('should translate stats endpoint with Request object', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      const request = new Request('/api/stats', { method: 'GET' });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
    });

    it('should translate stats endpoint with absolute URL string to Foundry backend', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch('https://other-host.example.com/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Should proxy to Foundry backend, NOT the other-host URL
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
      expect(call[0]).not.toContain('other-host.example.com');
    });

    it('should translate stats endpoint with URL object to Foundry backend', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ stats: {} }), { status: 200 }));

      await adapter.fetch(new URL('/api/stats', 'https://other-host.example.com'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      // Should proxy to Foundry backend
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/stats');
      expect(call[0]).not.toContain('other-host.example.com');
    });

    it('should NOT affect chat endpoints (routes through WASM shell, not stats proxy)', async () => {
      // POST /api/query no longer goes through the chat/stats proxy — it
      // routes through the WASM shell's runAgent. The stats proxy is for
      // stats operations only; chat has its own WASM-based routing.
      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'test' }),
      });

      // Chat endpoint must NOT be proxied (no fetch call).
      expect(mockFetch).not.toHaveBeenCalled();

      // Chat endpoint MUST route through WASM shell's runAgent.
      expect(mockWasmShell.runAgent).toHaveBeenCalledTimes(1);
      expect(mockWasmShell.runAgent.mock.calls[0][2]).toBe('test');
    });

    it('should NOT affect git endpoints (should still use git proxy)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ status: {} }), { status: 200 }));

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/git/status');
    });

    it('should NOT affect settings endpoints (should still use settings proxy)', async () => {
      mockFetch.mockResolvedValueOnce(new Response(JSON.stringify({ settings: {} }), { status: 200 }));

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/settings');
    });
  });

  describe('error handling', () => {
    it('should propagate network errors from proxied requests', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      await expect(adapter.fetch('/api/stats', { method: 'GET' })).rejects.toThrow('Network error');
    });

    it('should propagate 404 errors from proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(new Response('Not found', { status: 404 }));

      const response = await adapter.fetch('/api/stats', { method: 'GET' });
      expect(response.ok).toBe(false);
      expect(response.status).toBe(404);
    });
  });
});
