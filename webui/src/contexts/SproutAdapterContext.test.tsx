// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { SproutAdapterProvider, useSproutAdapter, useSproutFetch } from './SproutAdapterContext';
import type { APIAdapter } from '../services/apiAdapter';

// Mock the apiAdapter module to control singleton state in tests
vi.mock('../services/apiAdapter', () => {
  let mockAdapter: APIAdapter | null = null;

  return {
    getAdapter: () => mockAdapter,
    installAdapter: (adapter: APIAdapter | null) => {
      mockAdapter = adapter;
    },
    hasAdapter: () => mockAdapter !== null,
    requiresBackendHealthCheck: () => mockAdapter?.requiresBackendHealthCheck === true,
    __esModule: true,
  };
});

// Mock the clientSession module for the clientFetch tests
vi.mock('../services/clientSession', () => {
  const actual = vi.importActual('../services/clientSession');
  return {
    ...actual,
    clientFetch: vi.fn(),
    getWebUIClientId: vi.fn(() => 'test-client-id'),
    resolveWebUIClientId: vi.fn(() => Promise.resolve('test-client-id')),
    WEBUI_CLIENT_ID_HEADER: 'X-Sprout-Client-ID',
    __esModule: true,
  };
});

// Import after mocking to get the mocked functions
import { installAdapter, getAdapter } from '../services/apiAdapter';
import { clientFetch, resolveWebUIClientId, WEBUI_CLIENT_ID_HEADER } from '../services/clientSession';

// ---------------------------------------------------------------------------
// Mock Adapter Helper
// ---------------------------------------------------------------------------

function createMockAdapter(overrides: Partial<APIAdapter> = {}): APIAdapter {
  const mockFetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ success: true }),
  } as Response);

  return {
    name: 'TestAdapter',
    fetch: mockFetch,
    getWebSocketURL: vi.fn().mockReturnValue(null),
    requiresBackendHealthCheck: false,
    fileOpsViaAPI: true,
    showOnboarding: true,
    supportsSSH: false,
    supportsInstances: false,
    supportsLocalTerminal: true,
    supportsSettings: true,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestAdapterContext: APIAdapter | null | undefined;
let latestFetchFn: ((input: RequestInfo | URL, init?: RequestInit) => Promise<Response>) | undefined;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  latestAdapterContext = undefined;
  latestFetchFn = undefined;

  // Reset adapter state for each test
  installAdapter(null);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Test component that consumes the SproutAdapterContext and stores
 * a reference to the latest hook return values for inspection in tests.
 */
function TestConsumer() {
  latestAdapterContext = useSproutAdapter();
  latestFetchFn = useSproutFetch();
  return createElement('div', { 'data-testid': 'consumer' });
}

/**
 * Mounts the SproutAdapterProvider with a TestConsumer child.
 */
function renderProvider() {
  act(() => {
    // @ts-expect-error — createElement accepts children as rest args, but TS overloads don't reflect this
    root.render(createElement(SproutAdapterProvider, {}, createElement(TestConsumer)));
  });
}

/** Shorthand to get the current adapter context value from the latest render. */
const adapterCtx = () => latestAdapterContext;

/** Shorthand to get the current fetch function from the latest render. */
const fetchFn = () => latestFetchFn;

/**
 * Asserts that the adapter is non-null and returns it.
 * Throws a clear error if the adapter is null/undefined.
 */
function requireCtx(): APIAdapter {
  const v = latestAdapterContext;
  if (v === null || v === undefined) {
    throw new Error('Expected adapter to be non-null in test');
  }
  return v;
}

// ---------------------------------------------------------------------------
// Tests: useSproutAdapter hook
// ---------------------------------------------------------------------------

describe('useSproutAdapter', () => {
  it('throws an error when used outside of SproutAdapterProvider', () => {
    // Suppress the expected console.error from React when the component throws
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    // Render TestConsumer WITHOUT the provider — the hook should throw
    expect(() => {
      act(() => {
        root.render(createElement(TestConsumer));
      });
    }).toThrow('useSproutAdapter must be used within SproutAdapterProvider');

    consoleSpy.mockRestore();
  });

  it('returns null when no adapter is installed', () => {
    expect(getAdapter()).toBeNull();

    renderProvider();

    expect(adapterCtx()).toBeDefined();
    expect(adapterCtx()).toBeNull();
  });

  it('returns the adapter when one is installed via singleton', () => {
    const adapter = createMockAdapter({ name: 'InstalledAdapter' });
    installAdapter(adapter);

    renderProvider();

    expect(adapterCtx()).toBeDefined();
    expect(adapterCtx()).toBe(adapter);
    expect(requireCtx().name).toBe('InstalledAdapter');
  });

  it('returns the same adapter reference across rerenders', () => {
    const adapter = createMockAdapter({ name: 'StableAdapter' });
    installAdapter(adapter);

    renderProvider();

    const firstResult = adapterCtx();

    // Rerender with the same provider
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutAdapterProvider, {}, createElement(TestConsumer)));
    });

    const secondResult = adapterCtx();

    expect(secondResult).toBe(firstResult);
    expect(secondResult).toBe(adapter);
  });
});

// ---------------------------------------------------------------------------
// Tests: useSproutFetch hook
// ---------------------------------------------------------------------------

describe('useSproutFetch', () => {
  it('throws an error when used outside of SproutAdapterProvider', () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    function FetchConsumer() {
      latestFetchFn = useSproutFetch();
      return createElement('div');
    }

    expect(() => {
      act(() => {
        root.render(createElement(FetchConsumer));
      });
    }).toThrow('useSproutAdapter must be used within SproutAdapterProvider');

    consoleSpy.mockRestore();
  });

  it('returns a fetch function', () => {
    renderProvider();

    expect(fetchFn()).toBeDefined();
    expect(typeof fetchFn()).toBe('function');
  });

  it('falls back to clientFetch when no adapter is installed', async () => {
    (clientFetch as vi.Mock).mockResolvedValue({ ok: true } as Response);
    renderProvider();

    const fn = fetchFn();
    expect(fn).toBeDefined();

    // Call the fetch function
    await fn('/api/test');

    expect(clientFetch).toHaveBeenCalledWith('/api/test', expect.anything());
  });

  it('delegates to adapter.fetch when adapter is installed', async () => {
    const adapter = createMockAdapter({ name: 'CloudAdapter' });
    const adapterFetchSpy = vi.spyOn(adapter, 'fetch');
    installAdapter(adapter);

    renderProvider();

    const fn = fetchFn();
    expect(fn).toBeDefined();

    // Call the fetch function
    await fn('/api/cloud/test', { method: 'POST' });

    expect(adapterFetchSpy).toHaveBeenCalledWith(
      '/api/cloud/test',
      expect.objectContaining({
        method: 'POST',
      }),
    );

    adapterFetchSpy.mockRestore();
  });

  it('always sets the X-Sprout-Client-ID header', async () => {
    const adapter = createMockAdapter({ name: 'HeaderAdapter' });
    const capturedHeaders: Headers[] = [];
    adapter.fetch = vi.fn().mockImplementation(async (input, init) => {
      capturedHeaders.push(init?.headers as Headers);
      return { ok: true } as Response;
    });
    installAdapter(adapter);

    renderProvider();

    const fn = fetchFn();
    await fn('/api/test');

    expect(capturedHeaders).toHaveLength(1);
    // Just verify the header is set (it should be non-null)
    expect(capturedHeaders[0]!.get(WEBUI_CLIENT_ID_HEADER)).toBeDefined();
    expect(capturedHeaders[0]!.get(WEBUI_CLIENT_ID_HEADER)).not.toBeNull();
  });

  it('merges custom headers with client ID header when no adapter', async () => {
    (clientFetch as vi.Mock).mockResolvedValue({ ok: true } as Response);

    renderProvider();

    const fn = fetchFn();
    await fn('/api/test', {
      headers: {
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      },
    });

    expect(clientFetch).toHaveBeenCalledWith(
      '/api/test',
      expect.objectContaining({
        headers: expect.any(Headers),
      }),
    );

    const capturedHeaders = (clientFetch as vi.Mock).mock.calls[0]![1]!.headers as Headers;
    expect(capturedHeaders.get('Content-Type')).toBe('application/json');
    expect(capturedHeaders.get('X-Custom-Header')).toBe('custom-value');
    // Just verify client ID header is set
    expect(capturedHeaders.get(WEBUI_CLIENT_ID_HEADER)).toBeDefined();
    expect(capturedHeaders.get(WEBUI_CLIENT_ID_HEADER)).not.toBeNull();
  });

  it('merges custom headers with client ID header when adapter is installed', async () => {
    const capturedHeaders: Headers[] = [];
    const adapter = createMockAdapter({ name: 'MergeHeaderAdapter' });
    adapter.fetch = vi.fn().mockImplementation(async (input, init) => {
      capturedHeaders.push(init?.headers as Headers);
      return { ok: true } as Response;
    });
    installAdapter(adapter);

    renderProvider();

    const fn = fetchFn();
    await fn('/api/test', {
      headers: {
        'Content-Type': 'application/json',
        'X-Custom-Header': 'custom-value',
      },
    });

    expect(capturedHeaders).toHaveLength(1);
    const merged = capturedHeaders[0]!;
    expect(merged.get('Content-Type')).toBe('application/json');
    expect(merged.get('X-Custom-Header')).toBe('custom-value');
    // Just verify client ID header is set
    expect(merged.get(WEBUI_CLIENT_ID_HEADER)).toBeDefined();
    expect(merged.get(WEBUI_CLIENT_ID_HEADER)).not.toBeNull();
  });

  it('propagates fetch errors correctly', async () => {
    const adapter = createMockAdapter({ name: 'ErrorAdapter' });
    adapter.fetch = vi.fn().mockRejectedValue(new Error('Network error'));
    installAdapter(adapter);

    renderProvider();

    const fn = fetchFn();
    await expect(fn('/api/test')).rejects.toThrow('Network error');
  });
});

// ---------------------------------------------------------------------------
// Tests: SproutAdapterProvider
// ---------------------------------------------------------------------------

describe('SproutAdapterProvider', () => {
  it('renders children correctly', () => {
    renderProvider();

    expect(container.querySelector('[data-testid="consumer"]')).not.toBeNull();
  });

  it('provides null adapter when getAdapter() returns null', () => {
    expect(getAdapter()).toBeNull();

    renderProvider();

    expect(adapterCtx()).toBeNull();
  });

  it('provides the adapter from getAdapter() singleton', () => {
    const adapter = createMockAdapter({ name: 'SingletonAdapter' });
    installAdapter(adapter);

    renderProvider();

    expect(adapterCtx()).toBe(adapter);
    expect(requireCtx().name).toBe('SingletonAdapter');
  });

  it('context value is stable across rerenders with same adapter', () => {
    const adapter = createMockAdapter({ name: 'StableAdapter' });
    installAdapter(adapter);

    renderProvider();

    const firstAdapter = adapterCtx();

    // Rerender
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutAdapterProvider, {}, createElement(TestConsumer)));
    });

    const secondAdapter = adapterCtx();

    expect(secondAdapter).toBe(firstAdapter);
  });

  it('fetch function is stable across rerenders', () => {
    const adapter = createMockAdapter({ name: 'FetchStableAdapter' });
    installAdapter(adapter);

    renderProvider();

    const firstFetch = fetchFn();

    // Rerender
    act(() => {
      // @ts-expect-error — createElement accepts children as rest args
      root.render(createElement(SproutAdapterProvider, {}, createElement(TestConsumer)));
    });

    const secondFetch = fetchFn();

    // Fetch functions should have reference stability (useCallback)
    expect(secondFetch).toBe(firstFetch);
  });

  // NOTE: The provider does NOT subscribe to singleton changes — it reads
  // getAdapter() during render. This test only works because we explicitly
  // trigger a re-render after installAdapter(). In production, the adapter is
  // installed once before React renders, so this is not a concern.
  it('picks up adapter changes on re-render', () => {
    const firstAdapter = createMockAdapter({ name: 'FirstAdapter' });
    const secondAdapter = createMockAdapter({ name: 'SecondAdapter' });

    installAdapter(firstAdapter);
    renderProvider();

    expect(adapterCtx()).toBe(firstAdapter);

    // Install a different adapter and trigger re-render to pick up the change
    act(() => {
      installAdapter(secondAdapter);
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(SproutAdapterProvider, {}, createElement(TestConsumer)),
      );
    });

    expect(adapterCtx()).toBe(secondAdapter);
  });

  it('handles adapter with all optional fields', () => {
    const navItems = [{ id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 1 }];

    const adapter = createMockAdapter({
      name: 'FullFieldsAdapter',
      platformNavItems: navItems,
    });
    installAdapter(adapter);

    renderProvider();

    expect(adapterCtx()).toBe(adapter);
    expect(requireCtx().platformNavItems).toEqual(navItems);
  });
});
