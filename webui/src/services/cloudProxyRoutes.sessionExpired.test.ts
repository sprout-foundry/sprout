/**
 * Tests for session-expiry (401) handling in cloudProxyRoutes.ts
 *
 * Verifies that 401 responses from the Foundry backend trigger the
 * session-expired CustomEvent and deferred redirect, and that the
 * module-level guard ensures only one event + one redirect fires
 * regardless of how many 401s arrive concurrently.
 *
 * Also verifies that translateAndProxyChat and the CloudAdapter
 * catch-all proxy pipe 401 responses through handleFoundryAuthError.
 */

// ── Imports ──────────────────────────────────────────────────────────

import {
  SESSION_EXPIRED_EVENT,
  handleFoundryAuthError,
  translateAndProxyChat,
  _resetSessionExpiredGuardForTest,
} from './cloudProxyRoutes';

// ── window.location mock ─────────────────────────────────────────────
// In jsdom, assigning window.location.href to a same-page URL throws
// "Not implemented: navigation". We replace window.location with a mock
// that has a spyable setter for href (matching the bootstrapAdapter.test.ts
// and appStatePersistence.test.ts patterns).

const originalLocation = window.location;
const originalFetch = global.fetch;
let hrefSetter: ReturnType<typeof vi.fn>;
let hrefValue: string;

function mockWindowLocation(pathname: string, search: string) {
  hrefValue = `https://app.test.sprout.dev${pathname}${search}`;
  hrefSetter = vi.fn((value: string) => {
    hrefValue = value;
  });
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: {
      pathname,
      search,
      get href(): string {
        return hrefValue;
      },
      set href(v: string) {
        hrefSetter(v);
      },
      reload: vi.fn(),
    },
  });
}

function restoreWindowLocation() {
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: originalLocation,
  });
}

// ── Event capture ────────────────────────────────────────────────────

const capturedEvents: CustomEvent[] = [];
const eventHandler = (e: Event) => capturedEvents.push(e as CustomEvent);

// ── Shared setup / teardown ──────────────────────────────────────────

function setupSessionExpiredMocks() {
  _resetSessionExpiredGuardForTest();
  vi.useFakeTimers();
  mockWindowLocation('/repo/demos', '?tab=chat&x=1');
  capturedEvents.length = 0;
  window.addEventListener(SESSION_EXPIRED_EVENT, eventHandler);
}

function teardownSessionExpiredMocks() {
  window.removeEventListener(SESSION_EXPIRED_EVENT, eventHandler);
  vi.useRealTimers();
  global.fetch = originalFetch;
  restoreWindowLocation();
}

// ── Tests: handleFoundryAuthError — 401 → event + deferred redirect ─

describe('handleFoundryAuthError — 401 → event + deferred redirect', () => {
  beforeEach(() => {
    setupSessionExpiredMocks();
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it('returns the same response instance (passthrough)', () => {
    const response = new Response(null, { status: 401 });
    const result = handleFoundryAuthError(response);
    expect(result).toBe(response);
  });

  it('dispatches sprout:session-expired CustomEvent with { status: 401 }', () => {
    handleFoundryAuthError(new Response(null, { status: 401 }));
    expect(capturedEvents).toHaveLength(1);
    expect(capturedEvents[0].type).toBe(SESSION_EXPIRED_EVENT);
    expect(capturedEvents[0].detail).toEqual({ status: 401 });
  });

  it('does NOT redirect before 750ms', () => {
    handleFoundryAuthError(new Response(null, { status: 401 }));
    vi.advanceTimersByTime(749);
    expect(hrefSetter).not.toHaveBeenCalled();
  });

  it('redirects to /login?return_to=… after 750ms with encoded pathname+search', () => {
    handleFoundryAuthError(new Response(null, { status: 401 }));
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
    expect(hrefSetter).toHaveBeenCalledWith('/login?return_to=' + encodeURIComponent('/repo/demos?tab=chat&x=1'));
  });
});

// ── Tests: handleFoundryAuthError — non-401 passthrough ──────────────

describe('handleFoundryAuthError — non-401 passthrough', () => {
  beforeEach(() => {
    setupSessionExpiredMocks();
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it.each([200, 403, 500])('returns the same response instance for status %d', (status) => {
    const response = new Response(null, { status });
    const result = handleFoundryAuthError(response);
    expect(result).toBe(response);
  });

  it('does NOT dispatch sprout:session-expired for non-401 status', () => {
    handleFoundryAuthError(new Response(null, { status: 200 }));
    expect(capturedEvents).toHaveLength(0);
  });

  it('does NOT schedule redirect for non-401 status even after advancing timers', () => {
    handleFoundryAuthError(new Response(null, { status: 500 }));
    vi.advanceTimersByTime(750);
    expect(hrefSetter).not.toHaveBeenCalled();
  });
});

// ── Tests: handleFoundryAuthError — concurrent 401s ─────────────────

describe('handleFoundryAuthError — concurrent 401s → single event + single redirect', () => {
  beforeEach(() => {
    setupSessionExpiredMocks();
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it('5 concurrent 401s fire exactly one event and one redirect', () => {
    for (let i = 0; i < 5; i++) {
      handleFoundryAuthError(new Response(null, { status: 401 }));
    }
    expect(capturedEvents).toHaveLength(1);
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
  });
});

// ── Tests: handleFoundryAuthError — guard reset ──────────────────────

describe('handleFoundryAuthError — guard reset allows re-fire', () => {
  beforeEach(() => {
    setupSessionExpiredMocks();
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it('fires the flow again after _resetSessionExpiredGuardForTest', () => {
    // First 401
    handleFoundryAuthError(new Response(null, { status: 401 }));
    expect(capturedEvents).toHaveLength(1);

    // Let the first timer fire, then clear mocks
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
    hrefSetter.mockClear();

    // Reset guard
    _resetSessionExpiredGuardForTest();
    capturedEvents.length = 0;

    // Second 401 — should fire again
    handleFoundryAuthError(new Response(null, { status: 401 }));
    expect(capturedEvents).toHaveLength(1);
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
  });
});

// ── Tests: translateAndProxyChat — pipes 401 through ─────────────────

describe('translateAndProxyChat — pipes 401 through handleFoundryAuthError', () => {
  beforeEach(() => {
    setupSessionExpiredMocks();
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it('returns the 401 response and fires the session-expired event', async () => {
    const response = new Response(null, { status: 401 });
    global.fetch = vi.fn().mockResolvedValue(response);

    const result = await translateAndProxyChat(
      'https://api.sprout.dev',
      '/api/query',
      '/proxy/chat',
      'POST',
      'x-webui-client-id',
      'test-client-id',
      { method: 'POST', body: JSON.stringify({ query: 'hi' }) },
    );

    // Passthrough: same response instance
    expect(result).toBe(response);

    // Event dispatched
    expect(capturedEvents).toHaveLength(1);
    expect(capturedEvents[0].detail).toEqual({ status: 401 });

    // Deferred redirect
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
    expect(hrefSetter).toHaveBeenCalledWith('/login?return_to=' + encodeURIComponent('/repo/demos?tab=chat&x=1'));

    // Fetch was called with the Foundry proxy URL
    expect(global.fetch).toHaveBeenCalledWith(
      'https://api.sprout.dev/proxy/chat',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
      }),
    );
  });
});

// ── CloudAdapter catch-all proxy integration ─────────────────────────
// Mock the same modules as cloudAdapter.test.ts so CloudAdapter can be
// instantiated without WASM / IndexedDB / browser-git dependencies.

vi.mock('./clientSession', () => ({
  WEBUI_CLIENT_ID_HEADER: 'x-webui-client-id',
  getWebUIClientId: () => 'test-client-id-123',
}));

const mockWasmShell = {
  executeCommand: vi.fn(() => ({ stdout: '', stderr: '', exitCode: 0 })),
  getCwd: vi.fn(() => '/home/user'),
  changeDir: vi.fn(() => ({ cwd: '/home/user', error: '' })),
  writeFile: vi.fn(() => ''),
  readFile: vi.fn(() => ({ content: '', error: '' })),
  listDir: vi.fn(() => ({ entries: [], error: '' })),
  deleteFile: vi.fn(() => ''),
  runAgent: vi.fn(() => Promise.resolve({})),
  steerAgent: vi.fn(() => ({ steered: true })),
  stopAgent: vi.fn(() => {}),
};
vi.mock('./wasmShell', () => ({
  initWasmShell: vi.fn(() => Promise.resolve(mockWasmShell)),
  resetWasmShell: vi.fn(),
}));

vi.mock('./cloudWasmHandlers', async (importOriginal) => {
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

vi.mock('./browserGitHandler', () => ({
  handleBrowserGitRequest: vi.fn(
    async () =>
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
  ),
}));

import { CloudAdapter, type CloudAdapterConfig } from './cloudAdapter';

describe('CloudAdapter — catch-all proxy pipes 401 through handleFoundryAuthError', () => {
  let adapter: CloudAdapter;

  beforeEach(() => {
    setupSessionExpiredMocks();
    adapter = new CloudAdapter({
      apiBase: 'https://api.sprout.dev',
      wsUrl: 'wss://api.sprout.dev/ws',
    });
  });

  afterEach(() => {
    teardownSessionExpiredMocks();
  });

  it('catch-all proxy returns a 401 response and fires the session-expired event', async () => {
    const response = new Response(null, { status: 401 });
    global.fetch = vi.fn().mockResolvedValue(response);

    // /api/health-check is unregistered → falls through to the catch-all proxy
    const result = await adapter.fetch('/api/health-check', { method: 'GET' });

    // Passthrough: same response instance
    expect(result).toBe(response);

    // Event dispatched
    expect(capturedEvents).toHaveLength(1);
    expect(capturedEvents[0].detail).toEqual({ status: 401 });

    // Deferred redirect
    vi.advanceTimersByTime(750);
    expect(hrefSetter).toHaveBeenCalledTimes(1);
    expect(hrefSetter).toHaveBeenCalledWith('/login?return_to=' + encodeURIComponent('/repo/demos?tab=chat&x=1'));

    // Fetch was called with the rewritten URL on the Foundry backend
    expect(global.fetch).toHaveBeenCalledWith(
      'https://api.sprout.dev/api/health-check',
      expect.objectContaining({
        credentials: 'include',
      }),
    );
  });
});
