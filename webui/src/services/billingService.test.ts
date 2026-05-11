/**
 * Tests for billingService
 *
 * Tests getBillingStatus, getInvoices, getProrationRecords,
 * processRefund, getRefunds, getDunningReport, createBillingPortalSession.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Mocks (before imports) ──────────────────────────────────────────

vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(),
  installAdapter: vi.fn(),
  hasAdapter: vi.fn(),
  requiresBackendHealthCheck: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── Imports ──────────────────────────────────────────────────────────

import {
  getBillingStatus,
  getInvoices,
  getProrationRecords,
  processRefund,
  getRefunds,
  getDunningReport,
  createBillingPortalSession,
} from './billingService';
import { getAdapter, installAdapter } from '../services/apiAdapter';

// ── Helpers ──────────────────────────────────────────────────────────

const makeOkResponse = (data: any) => ({
  ok: true,
  json: () => Promise.resolve(data),
  status: 200,
});

const makeErrorResponse = (status: number) => ({
  ok: false,
  status,
});

interface MockAdapter {
  fetch: ReturnType<typeof vi.fn>;
  name: string;
}

function setupMockAdapter(fetchFn?: (url: string, options?: any) => Promise<any>): MockAdapter {
  const mockAdapter = {
    fetch: vi.fn().mockImplementation(fetchFn || (() => Promise.resolve(makeOkResponse({})))),
    name: 'mock',
  };
  (getAdapter as ReturnType<typeof vi.fn>).mockReturnValue(mockAdapter);
  return mockAdapter;
}

function setupNoAdapter() {
  (getAdapter as ReturnType<typeof vi.fn>).mockReturnValue(null);
}

// ── getBillingStatus ─────────────────────────────────────────────────

describe('getBillingStatus', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns null when no adapter installed', async () => {
    setupNoAdapter();
    const result = await getBillingStatus();
    expect(result).toBeNull();
  });

  it('returns billing status on success', async () => {
    const mockStatus = {
      tier: 'pro',
      status: 'active',
      proration_credits: 50,
      proration_charges: 100,
      dunning_status: 'none' as const,
    };
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockStatus)));
    const result = await getBillingStatus();
    expect(result).toEqual(mockStatus);
  });

  it('returns null on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(500)));
    const result = await getBillingStatus();
    expect(result).toBeNull();
  });

  it('returns null on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await getBillingStatus();
    expect(result).toBeNull();
  });

  it('calls adapter.fetch with correct URL', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse({ tier: 'free', status: 'active' })));
    await getBillingStatus();
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/billing/status');
  });
});

// ── getInvoices ──────────────────────────────────────────────────────

describe('getInvoices', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns [] when no adapter installed', async () => {
    setupNoAdapter();
    const result = await getInvoices();
    expect(result).toEqual([]);
  });

  it('returns invoices on success', async () => {
    const mockInvoices = [
      {
        id: 'inv-1',
        amount_due: 1000,
        amount_paid: 1000,
        status: 'paid',
        created: '2024-01-01',
        lines: [{ id: 'line-1', description: 'Pro plan', amount: 1000 }],
      },
    ];
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockInvoices)));
    const result = await getInvoices();
    expect(result).toEqual(mockInvoices);
  });

  it('returns [] on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(403)));
    const result = await getInvoices();
    expect(result).toEqual([]);
  });

  it('returns [] on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await getInvoices();
    expect(result).toEqual([]);
  });

  it('URL includes limit param when provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getInvoices(10);
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/billing/invoices?limit=10');
  });

  it('URL without limit param when not provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getInvoices();
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/billing/invoices');
  });
});

// ── getProrationRecords ──────────────────────────────────────────────

describe('getProrationRecords', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns [] when no adapter installed', async () => {
    setupNoAdapter();
    const result = await getProrationRecords();
    expect(result).toEqual([]);
  });

  it('returns proration records on success', async () => {
    const mockRecords = [
      {
        id: 'pr-1',
        user_id: 'user-1',
        subscription_id: 'sub-1',
        amount_cents: 500,
        type: 'credit' as const,
        old_price_id: 'price-old',
        new_price_id: 'price-new',
        created_at: '2024-01-01',
      },
    ];
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockRecords)));
    const result = await getProrationRecords();
    expect(result).toEqual(mockRecords);
  });

  it('returns [] on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(500)));
    const result = await getProrationRecords();
    expect(result).toEqual([]);
  });

  it('returns [] on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await getProrationRecords();
    expect(result).toEqual([]);
  });

  it('URL includes limit param when provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getProrationRecords(20);
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/billing/prorations?limit=20');
  });

  it('URL without limit param when not provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getProrationRecords();
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/billing/prorations');
  });
});

// ── processRefund ────────────────────────────────────────────────────

describe('processRefund', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns null when no adapter installed', async () => {
    setupNoAdapter();
    const result = await processRefund({ charge_id: 'ch-1', reason: 'duplicate', user_id: 'user-1' });
    expect(result).toBeNull();
  });

  it('returns refund on success', async () => {
    const mockRefund = {
      id: 'ref-1',
      charge_id: 'ch-1',
      user_id: 'user-1',
      amount: 500,
      reason: 'duplicate',
      status: 'succeeded',
      refund_type: 'full' as const,
      created_at: '2024-01-01',
    };
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockRefund)));
    const result = await processRefund({
      charge_id: 'ch-1',
      reason: 'duplicate',
      user_id: 'user-1',
    });
    expect(result).toEqual(mockRefund);
  });

  it('throws error on HTTP error with status code (unlike other functions)', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(400)));
    await expect(processRefund({ charge_id: 'ch-1', reason: 'duplicate', user_id: 'user-1' })).rejects.toThrow(
      'Status: 400',
    );
  });

  it('throws error on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    await expect(processRefund({ charge_id: 'ch-1', reason: 'duplicate', user_id: 'user-1' })).rejects.toThrow(
      'Network error',
    );
  });

  it('sends correct POST body with request', async () => {
    const mockAdapter = setupMockAdapter((url, options) => {
      expect(url).toBe('/api/admin/billing/refunds');
      expect(options?.method).toBe('POST');
      expect(options?.headers?.['Content-Type']).toBe('application/json');
      const body = JSON.parse(options?.body);
      expect(body.charge_id).toBe('ch-1');
      expect(body.reason).toBe('customer error');
      expect(body.user_id).toBe('user-1');
      expect(body.amount).toBe(250);
      return Promise.resolve(
        makeOkResponse({
          id: 'ref-1',
          charge_id: 'ch-1',
          amount: 250,
          reason: 'customer error',
          status: 'succeeded',
          refund_type: 'partial',
          created_at: '2024-01-01',
        }),
      );
    });
    await processRefund({
      charge_id: 'ch-1',
      amount: 250,
      reason: 'customer error',
      user_id: 'user-1',
    });
    expect(mockAdapter.fetch).toHaveBeenCalled();
  });

  it('sends POST without optional amount', async () => {
    const mockAdapter = setupMockAdapter((url, options) => {
      const body = JSON.parse(options?.body);
      expect(body.charge_id).toBe('ch-1');
      expect(body.amount).toBeUndefined();
      return Promise.resolve(
        makeOkResponse({
          id: 'ref-1',
          charge_id: 'ch-1',
          amount: 500,
          reason: 'duplicate',
          status: 'succeeded',
          refund_type: 'full',
          created_at: '2024-01-01',
        }),
      );
    });
    await processRefund({
      charge_id: 'ch-1',
      reason: 'duplicate',
      user_id: 'user-1',
    });
    expect(mockAdapter.fetch).toHaveBeenCalled();
  });
});

// ── getRefunds ───────────────────────────────────────────────────────

describe('getRefunds', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns [] when no adapter installed', async () => {
    setupNoAdapter();
    const result = await getRefunds();
    expect(result).toEqual([]);
  });

  it('returns refunds on success', async () => {
    const mockRefunds = [
      {
        id: 'ref-1',
        charge_id: 'ch-1',
        amount: 500,
        reason: 'duplicate',
        status: 'succeeded',
        refund_type: 'full' as const,
        created_at: '2024-01-01',
      },
    ];
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockRefunds)));
    const result = await getRefunds();
    expect(result).toEqual(mockRefunds);
  });

  it('returns [] on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(403)));
    const result = await getRefunds();
    expect(result).toEqual([]);
  });

  it('returns [] on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await getRefunds();
    expect(result).toEqual([]);
  });

  it('URL includes limit param when provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getRefunds(5);
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/admin/billing/refunds?limit=5');
  });

  it('URL without limit param when not provided', async () => {
    const mockAdapter = setupMockAdapter(() => Promise.resolve(makeOkResponse([])));
    await getRefunds();
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/admin/billing/refunds');
  });
});

// ── getDunningReport ─────────────────────────────────────────────────

describe('getDunningReport', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns null when no adapter installed', async () => {
    setupNoAdapter();
    const result = await getDunningReport();
    expect(result).toBeNull();
  });

  it('returns dunning report on success', async () => {
    const mockReport = {
      total_failed_payments: 10,
      recovered_payments: 8,
      suspended_subscriptions: 2,
      recovery_rate: 0.8,
    };
    setupMockAdapter(() => Promise.resolve(makeOkResponse(mockReport)));
    const result = await getDunningReport();
    expect(result).toEqual(mockReport);
  });

  it('returns null on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(500)));
    const result = await getDunningReport();
    expect(result).toBeNull();
  });

  it('returns null on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await getDunningReport();
    expect(result).toBeNull();
  });

  it('calls adapter.fetch with correct URL', async () => {
    const mockAdapter = setupMockAdapter(() =>
      Promise.resolve(
        makeOkResponse({
          total_failed_payments: 0,
          recovered_payments: 0,
          suspended_subscriptions: 0,
          recovery_rate: 0,
        }),
      ),
    );
    await getDunningReport();
    expect(mockAdapter.fetch).toHaveBeenCalledWith('/api/admin/billing/dunning/report');
  });
});

// ── createBillingPortalSession ───────────────────────────────────────

describe('createBillingPortalSession', () => {
  afterEach(() => {
    (getAdapter as ReturnType<typeof vi.fn>).mockReset();
  });

  it('returns null when no adapter installed', async () => {
    setupNoAdapter();
    const result = await createBillingPortalSession('http://app.example.com');
    expect(result).toBeNull();
  });

  it('returns URL string on success', async () => {
    setupMockAdapter(() => Promise.resolve(makeOkResponse({ url: 'https://billing.stripe.com/session-123' })));
    const result = await createBillingPortalSession('http://app.example.com');
    expect(result).toBe('https://billing.stripe.com/session-123');
  });

  it('returns null on HTTP error', async () => {
    setupMockAdapter(() => Promise.resolve(makeErrorResponse(500)));
    const result = await createBillingPortalSession('http://app.example.com');
    expect(result).toBeNull();
  });

  it('returns null on fetch exception', async () => {
    setupMockAdapter(() => Promise.reject(new Error('Network error')));
    const result = await createBillingPortalSession('http://app.example.com');
    expect(result).toBeNull();
  });

  it('sends correct POST body with return_url', async () => {
    const mockAdapter = setupMockAdapter((url, options) => {
      expect(url).toBe('/api/billing/portal');
      expect(options?.method).toBe('POST');
      expect(options?.headers?.['Content-Type']).toBe('application/json');
      const body = JSON.parse(options?.body);
      expect(body.return_url).toBe('https://myapp.com/settings');
      return Promise.resolve(makeOkResponse({ url: 'https://portal.example.com/abc' }));
    });
    await createBillingPortalSession('https://myapp.com/settings');
    expect(mockAdapter.fetch).toHaveBeenCalled();
  });

  it('extracts data.url from response (not full response)', async () => {
    setupMockAdapter(() =>
      Promise.resolve(
        makeOkResponse({
          url: 'https://portal.example.com/abc',
          id: 'portal-session-123',
          expires_at: 1234567890,
        }),
      ),
    );
    const result = await createBillingPortalSession('http://app.example.com');
    // Should return just the url field, not the full object
    expect(result).toBe('https://portal.example.com/abc');
    expect(typeof result).toBe('string');
  });
});
