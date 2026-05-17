import { CreditCard, ExternalLink } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { useSproutAdapter } from '../contexts/SproutAdapterContext';
import { useLog } from '../utils/log';
import './BillingPage.css';

// ---------------------------------------------------------------------------
// Types matching foundry /api/billing/* responses
// ---------------------------------------------------------------------------

/** Response from GET /api/billing/status (foundry BillingStatus struct) */
interface BillingStatusResponse {
  tier: string;
  interval: string;
  status: string;
  current_period_start: string;
  current_period_end: string;
  next_renewal_at?: string;
  cancel_at?: string;
  canceled_at?: string;
}

interface InvoiceLineItem {
  id: string;
  description: string;
  amount: number;
  proration?: boolean;
  proration_record_id?: string;
}

interface Invoice {
  id: string;
  amount_due: number;
  amount_paid: number;
  status: string;
  created: string;
  lines: InvoiceLineItem[];
  proration_total?: number;
}

/**
 * Signature matching both the adapter's fetch and the webui's useSproutFetch.
 */
type FetchFn = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface BillingPageProps {
  /**
   * Optional fetch callback. When supplied, BillingPage uses this for all HTTP
   * calls instead of looking up the @sprout/ui SproutAdapterContext.
   *
   * This is the integration point for consumers (e.g. the webui) that need to
   * inject their own context-aware fetch.
   *
   * When omitted, BillingPage falls back to the @sprout/ui SproutAdapterContext.
   */
  sproutFetch?: FetchFn;
}

// ---------------------------------------------------------------------------
// Constants — endpoints on foundry
// ---------------------------------------------------------------------------

const STATUS_ENDPOINT = '/api/billing/status';
const INVOICES_ENDPOINT = '/api/billing/invoices';
const PORTAL_ENDPOINT = '/api/billing/portal';

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

const BillingPage: React.FC<BillingPageProps> = ({ sproutFetch }) => {
  const log = useLog();

  // Always call the hook unconditionally (Rules of Hooks).
  const adapter = useSproutAdapter();

  // Prefer the injected sproutFetch when provided; fall back to the adapter.
  const doFetch: FetchFn | undefined = sproutFetch ?? adapter?.fetch;
  const available = !!doFetch;

  const [billingStatus, setBillingStatus] = useState<BillingStatusResponse | null>(null);
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Portal / manage subscription state
  const [portalLoading, setPortalLoading] = useState(false);

  // Stable refs so the initial fetch effect only runs once.
  const doFetchRef = React.useRef(doFetch);
  const availableRef = React.useRef(available);
  doFetchRef.current = doFetch;
  availableRef.current = available;

  // ------------------------------------------------------------------
  // Shared fetch helpers (used by both the mount effect and retry)
  // ------------------------------------------------------------------

  const loadBillingStatus = useCallback(async () => {
    const f = doFetchRef.current;
    const avail = availableRef.current;
    if (!avail || !f) {
      setError('Not available - running in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const response = await f(STATUS_ENDPOINT);
      if (!response.ok) {
        throw new Error(`Failed to fetch billing status: ${response.status} ${response.statusText}`);
      }
      const data = await response.json();
      setBillingStatus({
        tier: data?.tier ?? 'free',
        interval: data?.interval ?? 'monthly',
        status: data?.status ?? 'none',
        current_period_start: data?.current_period_start ?? '',
        current_period_end: data?.current_period_end ?? '',
        next_renewal_at: data?.next_renewal_at ?? undefined,
        cancel_at: data?.cancel_at ?? undefined,
        canceled_at: data?.canceled_at ?? undefined,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load billing information';
      setError(message);
      log.error(message, { title: 'Billing Page Error' });
    } finally {
      setLoading(false);
    }
  }, [log]);

  const loadInvoices = useCallback(async () => {
    const f = doFetchRef.current;
    const avail = availableRef.current;
    if (!avail || !f) return;
    try {
      const response = await f(INVOICES_ENDPOINT);
      if (!response.ok) return;
      const data = await response.json();
      // Foundry returns { invoices: [...] }
      const invoiceList = Array.isArray(data) ? data : (data?.invoices ?? []);
      setInvoices(Array.isArray(invoiceList) ? invoiceList : []);
    } catch {
      // Non-critical
    }
  }, []);

  // ------------------------------------------------------------------
  // Initial data fetch — runs once on mount
  // ------------------------------------------------------------------

  useEffect(() => {
    // Fire both requests concurrently. Each load function handles
    // its own errors internally.
    Promise.all([loadBillingStatus(), loadInvoices()]).catch(() => {});

    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ------------------------------------------------------------------
  // Manage subscription portal
  // ------------------------------------------------------------------

  const handleOpenPortal = useCallback(async () => {
    const f = doFetchRef.current;
    const avail = availableRef.current;
    if (!avail || !f) return;
    setPortalLoading(true);
    try {
      const response = await f(PORTAL_ENDPOINT, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ return_url: typeof window !== 'undefined' ? window.location.href : '/' }),
      });
      if (!response.ok) {
        throw new Error(`Failed to create portal session: ${response.status}`);
      }
      const data = await response.json();
      // Foundry returns { portal_url: "..." }
      const url = data?.portal_url ?? data?.url;
      if (url && typeof window !== 'undefined') {
        window.open(url, '_blank', 'noopener,noreferrer');
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to open billing portal';
      log.error(message, { title: 'Portal Error' });
    } finally {
      setPortalLoading(false);
    }
  }, [log]);

  // ------------------------------------------------------------------
  // Formatting helpers
  // ------------------------------------------------------------------

  const formatCurrency = (amount: number) =>
    new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(amount);
  const formatDate = (dateString: string) => {
    if (!dateString) return '—';
    const date = new Date(dateString);
    if (isNaN(date.getTime())) return '—';
    return date.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
  };

  // ------------------------------------------------------------------
  // Derived state
  // ------------------------------------------------------------------

  const hasSubscription = billingStatus?.status !== 'none' && billingStatus?.status !== '';

  // ------------------------------------------------------------------
  // Render
  // ------------------------------------------------------------------

  return (
    <div className="sprout-platform-page">
      <div className="sprout-platform-page-header">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <h2>Billing & Usage</h2>
            <p>View your current plan, subscription status, and invoice history.</p>
          </div>
          {available && billingStatus && (
            <button
              className="sprout-platform-button sprout-platform-button-secondary sprout-platform-button-sm"
              onClick={handleOpenPortal}
              disabled={portalLoading}
              style={{ display: 'flex', alignItems: 'center', gap: '6px', opacity: portalLoading ? 0.6 : 1 }}
            >
              <ExternalLink size={14} />
              {portalLoading ? 'Loading...' : 'Manage Plan'}
            </button>
          )}
        </div>
      </div>

      {/* Loading State */}
      {loading && <div className="sprout-platform-page-loading">Loading billing information...</div>}

      {/* Error State */}
      {error && (
        <div className="sprout-platform-page-error">
          <h3>Error loading billing</h3>
          <p>{error}</p>
          <button
            className="sprout-platform-button sprout-platform-button-secondary sprout-platform-button-sm"
            onClick={loadBillingStatus}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      )}

      {/* Empty state (no data) */}
      {!loading && !error && !billingStatus && (
        <div className="sprout-platform-page-empty">
          <div className="sprout-platform-page-empty-icon">
            <CreditCard size={48} />
          </div>
          <h3>No billing information</h3>
          <p>Billing data is not available for your account.</p>
        </div>
      )}

      {/* Main billing content */}
      {!loading && !error && billingStatus && (
        <>
          {/* Current Plan */}
          <div className="sprout-platform-card">
            <div className="sprout-platform-card-header">
              <h3 className="sprout-platform-card-title">Current Plan</h3>
              <span className="sprout-platform-status-badge running">{billingStatus.tier.toUpperCase()}</span>
            </div>
            <div className="sprout-platform-card-body">
              You are on the <strong>{billingStatus.tier}</strong> plan
              {billingStatus.interval !== 'monthly' && <span> ({billingStatus.interval} billing)</span>}
              .
              {hasSubscription && (
                <span> Status: <strong>{billingStatus.status}</strong>.</span>
              )}
              {hasSubscription && (
                <span> Usage resets on {formatDate(billingStatus.current_period_end)}.</span>
              )}
            </div>
          </div>

          {/* Subscription Details */}
          {hasSubscription && (
            <div className="sprout-platform-card">
              <div className="sprout-platform-card-header">
                <h3 className="sprout-platform-card-title">Subscription Details</h3>
              </div>
              <div className="sprout-platform-card-body">
                <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '8px 16px', fontSize: '14px' }}>
                  <span style={{ color: 'var(--text-secondary)' }}>Billing Period</span>
                  <span>{formatDate(billingStatus.current_period_start)} - {formatDate(billingStatus.current_period_end)}</span>

                  {billingStatus.next_renewal_at && (
                    <>
                      <span style={{ color: 'var(--text-secondary)' }}>Next Renewal</span>
                      <span>{formatDate(billingStatus.next_renewal_at)}</span>
                    </>
                  )}

                  {billingStatus.cancel_at && (
                    <>
                      <span style={{ color: 'var(--text-secondary)' }}>Cancellation Date</span>
                      <span style={{ color: '#ef4444' }}>{formatDate(billingStatus.cancel_at)}</span>
                    </>
                  )}

                  {billingStatus.canceled_at && (
                    <>
                      <span style={{ color: 'var(--text-secondary)' }}>Cancelled At</span>
                      <span style={{ color: '#ef4444' }}>{formatDate(billingStatus.canceled_at)}</span>
                    </>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Upgrade prompt for free tier */}
          {!hasSubscription && billingStatus.tier === 'free' && (
            <div className="sprout-platform-card">
              <div className="sprout-platform-card-header">
                <h3 className="sprout-platform-card-title">Upgrade Your Plan</h3>
              </div>
              <div className="sprout-platform-card-body">
                <p>Upgrade to Pro or Team to unlock unlimited tasks and higher usage limits.</p>
              </div>
            </div>
          )}

          {/* Invoice History */}
          {invoices.length > 0 ? (
            <div className="sprout-platform-card" style={{ marginTop: '24px' }}>
              <div className="sprout-platform-card-header">
                <h3 className="sprout-platform-card-title">Recent Invoices</h3>
              </div>
              <div className="sprout-platform-list">
                {invoices.map((invoice) => {
                  const lineItems = Array.isArray(invoice.lines)
                    ? invoice.lines
                    : (invoice.lines as { data?: InvoiceLineItem[] } | undefined)?.data ?? [];
                  const lineCount = lineItems.length;
                  return (
                    <div key={invoice.id} className="sprout-platform-list-item">
                      <div className="sprout-platform-list-item-icon">
                        <CreditCard size={20} />
                      </div>
                      <div className="sprout-platform-list-item-content">
                        <div className="sprout-platform-list-item-title">
                          Invoice {invoice.id.slice(0, 8)}
                        </div>
                        <div className="sprout-platform-list-item-subtitle">
                          {lineCount} line item{lineCount !== 1 ? 's' : ''}
                        </div>
                      </div>
                      <div className="sprout-platform-list-item-meta">
                        <span className={`sprout-platform-status-badge ${invoice.status === 'paid' ? 'completed' : invoice.status === 'open' ? 'pending' : ''}`}>
                          {invoice.status}
                        </span>
                        <div className="sprout-platform-list-item-time">
                          <div style={{ fontWeight: '500', color: 'var(--text-primary)' }}>
                            {formatCurrency(invoice.amount_due)}
                          </div>
                          <div>{formatDate(invoice.created)}</div>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : (
            !loading && hasSubscription && (
              <div className="sprout-platform-card" style={{ marginTop: '24px' }}>
                <div className="sprout-platform-card-body">
                  <p style={{ textAlign: 'center', color: 'var(--text-secondary)' }}>No invoices found.</p>
                </div>
              </div>
            )
          )}
        </>
      )}
    </div>
  );
};

export default BillingPage;
