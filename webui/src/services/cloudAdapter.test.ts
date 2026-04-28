/**
 * Tests for CloudAdapter
 */

import { CloudAdapter, type CloudAdapterConfig } from './cloudAdapter';
import { WEBUI_CLIENT_ID_HEADER, getWebUIClientId } from './clientSession';

// Mock clientSession module
jest.mock('./clientSession', () => ({
  WEBUI_CLIENT_ID_HEADER: 'x-webui-client-id',
  getWebUIClientId: () => 'test-client-id-123',
}));

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
if (typeof Request === 'undefined') {
  global.Request = class Request {
    url: string;
    method: string;
    headers: Headers | Map<string, string>;
    private _body: string | null;

    constructor(input: string | Request, init?: RequestInit | { method?: string }) {
      if (typeof input === 'string') {
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
}

describe('CloudAdapter', () => {
  let adapter: CloudAdapter;
  let mockConfig: CloudAdapterConfig;
  let mockFetch: jest.Mock;

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
    mockFetch = jest.fn();
    global.fetch = mockFetch;
  });

  afterEach(() => {
    jest.clearAllMocks();
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

    it('should support SSH and instances', () => {
      expect(adapter.supportsSSH).toBe(true);
      expect(adapter.supportsInstances).toBe(true);
    });

    it('should not support local terminal or settings', () => {
      expect(adapter.supportsLocalTerminal).toBe(false);
      expect(adapter.supportsSettings).toBe(false);
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
      expect(data).toEqual({ setup_required: false });

      // Should NOT call the actual fetch
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for onboarding complete', async () => {
      const response = await adapter.fetch('/api/onboarding/complete', {
        method: 'POST',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ success: true });

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
        current_pid: 0,
        active_host_pid: 0,
        active_host_port: 0,
        desired_host_pid: 0,
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
      expect(data).toEqual({ error: 'Not available in cloud mode' });

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
      expect(data).toEqual({ error: 'Not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic success response for SSH close', async () => {
      const response = await adapter.fetch('/api/instances/ssh-close', {
        method: 'POST',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ success: true });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic success response for instance select', async () => {
      const response = await adapter.fetch('/api/instances/select', {
        method: 'POST',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ success: true });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should return synthetic response for support bundle', async () => {
      const response = await adapter.fetch('/api/support-bundle', {
        method: 'GET',
      });

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ message: 'Not available in cloud mode' });

      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should set correct Content-Type header for synthetic responses', async () => {
      const response = await adapter.fetch('/api/onboarding/status', {
        method: 'GET',
      });

      expect(response.headers.get('Content-Type')).toBe('application/json');
    });
  });

  describe('fetch - WASM-local endpoint passthrough', () => {
    it('should NOT intercept WASM-local file endpoints', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ files: [] }), { status: 200 })
      );

      await adapter.fetch('/api/files', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toContain('/api/files');
    });

    it('should NOT intercept WASM-local terminal endpoints', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ history: [] }), { status: 200 })
      );

      await adapter.fetch('/api/terminal/history', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toContain('/api/terminal/history');
    });

    it('should NOT intercept WASM-local create endpoint', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: '/test.txt' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should NOT intercept WASM-local browse endpoint', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ files: [] }), { status: 200 })
      );

      await adapter.fetch('/api/browse', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should NOT intercept WASM-local search/replace endpoint', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/search/replace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pattern: 'foo', replacement: 'bar' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('fetch - Foundry backend proxying', () => {
    it('should proxy git status endpoint to Foundry', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ status: {} }), { status: 200 })
      );

      await adapter.fetch('/api/git/status', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/git/status');
    });

    it('should proxy stats endpoint to Foundry', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ stats: {} }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/stats');
    });

    it('should proxy settings endpoint to Foundry', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ settings: {} }), { status: 200 })
      );

      await adapter.fetch('/api/settings', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/settings');
    });

    it('should proxy chat-sessions endpoint to Foundry', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ sessions: [] }), { status: 200 })
      );

      await adapter.fetch('/api/chat-sessions', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/chat-sessions');
    });
  });

  describe('fetch - header handling', () => {
    it('should add WebUI client ID header to proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should include credentials for auth', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.credentials).toBe('include');
    });

    it('should preserve existing headers', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      const customHeaders = new Headers({
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      });

      await adapter.fetch('/api/query', {
        method: 'POST',
        headers: customHeaders,
        body: JSON.stringify({ query: 'test' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('Content-Type')).toBe('application/json');
      expect(call[1]?.headers?.get('X-Custom-Header')).toBe('custom-value');
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
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
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/test');
    });

    it('should NOT rewrite absolute URLs', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('https://example.com/api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://example.com/api/test');
    });

    it('should NOT rewrite URLs without leading slash', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('api/test', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('api/test');
    });
  });

  describe('fetch - different input types', () => {
    it('should handle string URL input', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/stats');

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle URL object input', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch(new URL('/api/stats', 'https://api.sprout.dev'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should intercept synthetic responses when URL object input is used', async () => {
      const response = await adapter.fetch(new URL('/api/onboarding/status', 'https://api.sprout.dev'));

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ setup_required: false });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should intercept synthetic responses when URL object with query params is used', async () => {
      const response = await adapter.fetch(new URL('/api/instances?foo=bar', 'https://api.sprout.dev'));

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({
        instances: [],
        current_pid: 0,
        active_host_pid: 0,
        active_host_port: 0,
        desired_host_pid: 0,
      });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should intercept synthetic responses when Request object input is used', async () => {
      const request = new Request('/api/onboarding/status', { method: 'GET' });
      const response = await adapter.fetch(request);

      expect(response.ok).toBe(true);
      const data = await response.json();
      expect(data).toEqual({ setup_required: false });
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('should NOT intercept synthetic response for non-api URL object', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch(new URL('/health', 'https://api.sprout.dev'));

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle Request object input', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      const request = new Request('/api/stats', { method: 'GET' });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('fetch - case insensitivity', () => {
    it('should handle lowercase HTTP methods', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'get' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('should handle mixed case HTTP methods', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'GeT' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
    });
  });

  describe('fetch - query parameter handling', () => {
    it('should strip query parameters when classifying endpoints', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/settings?layer=provenance', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/settings?layer=provenance');
    });

    it('should preserve query parameters in proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ history: [] }), { status: 200 })
      );

      await adapter.fetch('/api/terminal/history?session_id=abc123', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toContain('session_id=abc123');
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
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/unknown/endpoint', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/unknown/endpoint');
    });
  });

  describe('fetch - chat endpoint translation', () => {
    it('should translate POST /api/query URL to /api/proxy/chat', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat');
    });

    it('should translate POST /api/query body from webui format to Foundry format', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({
        messages: [{ role: 'user', content: 'hello' }],
        stream: true,
      });
    });

    it('should translate POST /api/query/steer URL to /api/proxy/chat with steer flag', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query/steer', {
        method: 'POST',
        body: JSON.stringify({ query: 'adjust tone' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody.steer).toBe(true);
    });

    it('should translate POST /api/query/stop URL to /api/proxy/chat/stop', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query/stop', {
        method: 'POST',
        body: JSON.stringify({ chat_id: 'chat-123' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat/stop');
      // Body should be passed through unchanged (no translation for stop)
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({ chat_id: 'chat-123' });
    });

    it('should translate GET /api/query/status URL to /api/proxy/chat/status', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ status: 'idle' }), { status: 200 })
      );

      await adapter.fetch('/api/query/status', {
        method: 'GET',
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat/status');
    });

    it('should preserve chat_id in translated body', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'test', chat_id: 'chat-123' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const sentBody = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      expect(sentBody.chat_id).toBe('chat-123');
    });

    it('should preserve provider and model if present', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'test', provider: 'anthropic', model: 'claude-3' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const sentBody = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      expect(sentBody.provider).toBe('anthropic');
      expect(sentBody.model).toBe('claude-3');
    });

    it('should preserve workspace_root and system_prompt if present', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({
          query: 'test',
          workspace_root: '/home/user/project',
          system_prompt: 'You are helpful.',
        }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const sentBody = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      expect(sentBody.workspace_root).toBe('/home/user/project');
      expect(sentBody.system_prompt).toBe('You are helpful.');
    });

    it('should NOT translate non-chat endpoints', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ stats: {} }), { status: 200 })
      );

      await adapter.fetch('/api/stats', { method: 'GET' });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/stats');
    });

    it('should include WebUI client ID header in translated requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('x-webui-client-id')).toBe('test-client-id-123');
    });

    it('should include credentials in translated requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.credentials).toBe('include');
    });

    it('should set Content-Type to application/json for chat requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: 'hello' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[1]?.headers?.get('Content-Type')).toBe('application/json');
    });

    it('should pass through empty query for backend validation', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query', {
        method: 'POST',
        body: JSON.stringify({ query: '' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const sentBody = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
      // Adapter constructs messages array with empty content (Foundry backend validates)
      expect(sentBody.messages).toEqual([{ role: 'user', content: '' }]);
    });

    it('should handle query with query parameters in URL', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      await adapter.fetch('/api/query?chat_id=abc', {
        method: 'POST',
        body: JSON.stringify({ query: 'test' }),
      });

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat');
    });

    it('should translate body when Request object is used for chat endpoint', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      const request = new Request('/api/query', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: 'test from request object' }),
      });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({
        messages: [{ role: 'user', content: 'test from request object' }],
        stream: true,
      });
    });

    it('should translate body with steer flag when Request object is used', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response(JSON.stringify({ success: true }), { status: 200 })
      );

      const request = new Request('/api/query/steer', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: 'adjust tone' }),
      });
      await adapter.fetch(request);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const call = mockFetch.mock.calls[0];
      expect(call[0]).toBe('https://api.sprout.dev/api/proxy/chat');
      const sentBody = JSON.parse(call[1]?.body as string);
      expect(sentBody).toEqual({
        messages: [{ role: 'user', content: 'adjust tone' }],
        stream: true,
        steer: true,
      });
    });
  });

  describe('error handling', () => {
    it('should propagate network errors from proxied requests', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      await expect(adapter.fetch('/api/stats', { method: 'GET' })).rejects.toThrow('Network error');
    });

    it('should propagate 404 errors from proxied requests', async () => {
      mockFetch.mockResolvedValueOnce(
        new Response('Not found', { status: 404 })
      );

      const response = await adapter.fetch('/api/stats', { method: 'GET' });
      expect(response.ok).toBe(false);
      expect(response.status).toBe(404);
    });
  });
});
