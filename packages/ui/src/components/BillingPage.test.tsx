// @ts-nocheck

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import BillingPage from './BillingPage';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../contexts/SproutAdapterContext', () => ({
  useSproutAdapter: vi.fn(() => null),
}));

vi.mock('../utils/log', () => ({
  useLog: () => ({
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    debug: vi.fn(),
    success: vi.fn(),
  }),
}));

const { useSproutAdapter } = await import('../contexts/SproutAdapterContext');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

// /api/billing/status returns the BillingStatus struct from foundry
const BILLING_STATUS = {
  ok: true,
  json: () =>
    Promise.resolve({
      tier: 'pro',
      interval: 'monthly',
      status: 'active',
      current_period_start: '2025-01-01T00:00:00Z',
      current_period_end: '2025-01-31T23:59:59Z',
      next_renewal_at: '2025-01-31T23:59:59Z',
    }),
};

// /api/billing/invoices returns { invoices: [...] } from foundry
const INVOICES = {
  ok: true,
  json: () =>
    Promise.resolve({
      invoices: [
        {
          id: 'in_123',
          amount_due: 2999,
          amount_paid: 2999,
          status: 'paid',
          created: '2025-01-15T00:00:00Z',
          lines: [
            { id: 'li_1', description: 'Pro Plan', amount: 2999 },
          ],
        },
      ],
    }),
};

function mockAdapterFetch(url: string | Request) {
  const path = typeof url === 'string' ? url : url.toString();
  if (path.includes('/api/billing/status')) return BILLING_STATUS;
  if (path.includes('/api/billing/invoices')) return INVOICES;
  if (path.includes('/api/billing/portal')) {
    return {
      ok: true,
      json: () => Promise.resolve({ portal_url: 'https://billing.example.com/portal' }),
    };
  }
  return { ok: false, status: 404, statusText: 'Not Found' };
}

function createMockAdapter(fetchFn: ReturnType<typeof vi.fn>) {
  return {
    name: 'test-adapter',
    fetch: fetchFn,
    getWebSocketURL: vi.fn(),
    requiresBackendHealthCheck: false,
    fileOpsViaAPI: true,
    showOnboarding: false,
    supportsSSH: false,
    supportsInstances: false,
    supportsLocalTerminal: true,
    supportsSettings: true,
  };
}

let mockFetch: ReturnType<typeof vi.fn>;

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

  mockFetch = vi.fn().mockImplementation((url) => Promise.resolve(mockAdapterFetch(url)));
  useSproutAdapter.mockReturnValue(createMockAdapter(mockFetch));
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderSync(props = {}) {
  act(() => {
    root.render(createElement(BillingPage, props));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('BillingPage', () => {
  it('renders without crashing', () => {
    renderSync();
    expect(container.querySelector('.sprout-platform-page')).not.toBeNull();
  });

  it('displays page title and description', () => {
    renderSync();
    expect(container.querySelector('.sprout-platform-page-header')).not.toBeNull();
    expect(container.textContent).toContain('Billing & Usage');
  });

  it('shows loading state on initial render', () => {
    renderSync();
    expect(container.querySelector('.sprout-platform-page-loading')).not.toBeNull();
    expect(container.textContent).toContain('Loading billing information...');
  });

  it('shows local mode error when no adapter is installed', () => {
    useSproutAdapter.mockReturnValue(null);
    renderSync();

    expect(container.querySelector('.sprout-platform-page-error')).not.toBeNull();
    expect(container.textContent).toContain('Not available - running in local mode');
  });

  it('calls adapter.fetch with /api/billing/status on mount', () => {
    renderSync();
    expect(mockFetch).toHaveBeenCalledWith('/api/billing/status');
  });

  it('calls adapter.fetch with /api/billing/invoices on mount', () => {
    renderSync();
    expect(mockFetch).toHaveBeenCalledWith('/api/billing/invoices');
  });

  it('does not show error state on initial render with adapter', () => {
    renderSync();
    expect(container.querySelector('.sprout-platform-page-error')).toBeNull();
  });

  it('does not show empty state on initial render', () => {
    renderSync();
    expect(container.querySelector('.sprout-platform-page-empty')).toBeNull();
  });

  it('accepts injected sproutFetch prop', () => {
    const injectedFetch = vi.fn().mockImplementation((url) => Promise.resolve(mockAdapterFetch(url)));
    useSproutAdapter.mockReturnValue(null);

    renderSync({ sproutFetch: injectedFetch });

    // Should call the injected fetch, not the adapter
    expect(injectedFetch).toHaveBeenCalled();
  });

  it('prefers sproutFetch prop over adapter fetch', () => {
    const injectedFetch = vi.fn().mockImplementation((url) => Promise.resolve(mockAdapterFetch(url)));

    renderSync({ sproutFetch: injectedFetch });

    // The injected fetch should be called instead of the adapter's fetch
    expect(injectedFetch).toHaveBeenCalled();
    // Adapter fetch should NOT be called when sproutFetch is provided
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('shows Manage Plan button after data loads when adapter is available', async () => {
    renderSync();

    // On initial render, billingStatus is null so the Manage Plan button is not shown
    expect(container.textContent).not.toContain('Manage Plan');

    // Flush microtasks to allow fetch to complete
    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    // After billing data loads, the button should appear
    expect(container.textContent).toContain('Manage Plan');
  }, 10000);

  it('displays billing data after successful load', async () => {
    renderSync();

    // Flush microtasks and allow React to settle
    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    expect(container.textContent).toContain('pro');
  }, 10000);

  it('shows error on failed API call', async () => {
    const errorFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    });
    useSproutAdapter.mockReturnValue(createMockAdapter(errorFetch));

    renderSync();

    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    expect(container.textContent).toContain('Failed to fetch billing status');
  }, 10000);

  it('shows Retry button in error state', async () => {
    const errorFetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    });
    useSproutAdapter.mockReturnValue(createMockAdapter(errorFetch));

    renderSync();

    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    const errorEl = container.querySelector('.sprout-platform-page-error');
    expect(errorEl).not.toBeNull();
    const retryBtn = errorEl?.querySelector('.sprout-platform-button');
    expect(retryBtn).not.toBeNull();
    if (retryBtn) {
      expect(retryBtn.textContent).toContain('Retry');
    }
  }, 10000);

  it('displays subscription details when status is active', async () => {
    renderSync();

    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    expect(container.textContent).toContain('Subscription Details');
    expect(container.textContent).toContain('Billing Period');
    expect(container.textContent).toContain('active');
  }, 10000);

  it('displays invoice list from foundry response', async () => {
    renderSync();

    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    expect(container.textContent).toContain('Recent Invoices');
    expect(container.textContent).toContain('in_123');
    expect(container.textContent).toContain('paid');
  }, 10000);

  it('handles free tier with no subscription', async () => {
    const freeFetch = vi.fn().mockImplementation((url) => {
      const path = typeof url === 'string' ? url : url.toString();
      if (path.includes('/api/billing/status')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            tier: 'free',
            interval: 'monthly',
            status: 'none',
            current_period_start: '',
            current_period_end: '',
          }),
        });
      }
      if (path.includes('/api/billing/invoices')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ invoices: [] }),
        });
      }
      return Promise.resolve({ ok: false, status: 404 });
    });
    useSproutAdapter.mockReturnValue(createMockAdapter(freeFetch));

    renderSync();

    await act(async () => {
      await new Promise(r => setTimeout(r, 10));
    });

    expect(container.textContent).toContain('free');
    expect(container.textContent).toContain('Upgrade Your Plan');
  }, 10000);
});
